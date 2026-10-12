# AWS onboarding — passwordless Elevarq Signals on RDS / Aurora

Stands up a least-privilege, **passwordless** Signals collector against Amazon
RDS / Aurora PostgreSQL using `auth_method: aws_rds_iam`. The collector's EC2
IAM identity mints a short-lived RDS IAM token at connect time — **no password
is stored anywhere** (INV001/INV002), and the connection is `verify-full`
(INV003).

Two equivalent implementations:

- [`terraform/`](terraform/) — `terraform apply`
- [`cloudformation/signals-rds-iam.yaml`](cloudformation/signals-rds-iam.yaml) — `aws cloudformation deploy`

Both provision the IAM role + minimal `rds-db:connect` policy (scoped to one DB
user), an instance profile, and an EC2 instance running the collector
pre-wired for `aws_rds_iam`. See
[`docs/database-connections.md`](../../docs/database-connections.md) for the
full `auth_method` reference.

The collector role also carries the AWS-managed
`AmazonSSMManagedInstanceCore` policy, so the verify steps below run over
Systems Manager Session Manager / Run Command — no SSH key pair is
provisioned.

## Prerequisites

- An RDS / Aurora PostgreSQL instance with **IAM database authentication
  enabled**.
- A subnet and security group that route to the instance on its port (the SG
  must allow egress to the DB).
- The instance's **`DbiResourceId`**:

  ```bash
  aws rds describe-db-instances --db-instance-identifier <id> \
    --query 'DBInstances[0].DbiResourceId' --output text
  ```

## Step 1 — Database role (one-time)

Run once against the target database, as a privileged role:

```sql
CREATE ROLE signals LOGIN;     -- no password (passwordless)
GRANT rds_iam TO signals;      -- enables RDS IAM authentication
GRANT pg_monitor TO signals;   -- least-privilege read-only monitoring
```

See [`docs/postgres-role.md`](../../docs/postgres-role.md) for the role
rationale.

## Step 2a — Terraform

```bash
cd terraform
terraform init
terraform apply \
  -var region=us-east-1 \
  -var db_host=mydb.abc123.us-east-1.rds.amazonaws.com \
  -var db_name=appdb \
  -var db_resource_id=db-ABCDEFGH00000000000000 \
  -var subnet_id=subnet-0123456789abcdef0 \
  -var 'security_group_ids=["sg-0123456789abcdef0"]'
```

## Step 2b — CloudFormation

```bash
aws cloudformation deploy \
  --template-file cloudformation/signals-rds-iam.yaml \
  --stack-name signals-rds-iam \
  --capabilities CAPABILITY_IAM \
  --parameter-overrides \
    DbHost=mydb.abc123.us-east-1.rds.amazonaws.com \
    DbName=appdb \
    DbResourceId=db-ABCDEFGH00000000000000 \
    SubnetId=subnet-0123456789abcdef0 \
    SecurityGroupIds=sg-0123456789abcdef0
```

## Verify (live)

Live verification provisions real infrastructure and is operator-gated —
it is not part of default CI (mirrors the provider live smokes, #94). After
apply/deploy, open a shell on the collector instance via SSM Session Manager
(`aws ssm start-session --target <instance-id>`) or SSM Run Command, then:

```bash
# the collector container should be running and collecting
docker logs signals 2>&1 | grep -iE "collector|snapshot|connected"

# the control-plane API requires a bearer token; user-data minted one and
# stored it root-only on the instance, outside the bind-mounted config.
TOKEN="$(sudo cat /root/signals-api-token)"

# confirm collector status over the loopback API
docker exec -e SIGNALS_API_TOKEN="$TOKEN" signals signalsctl status

# force an immediate collection (bypasses min_snapshot_interval), then export
docker exec -e SIGNALS_API_TOKEN="$TOKEN" signals signalsctl collect now --force
docker exec -e SIGNALS_API_TOKEN="$TOKEN" signals signalsctl export --output /data/snapshot.zip
```

A healthy run connects with **no password in config**, mints a token from the
instance role, and collects at least one snapshot. If the connection is
rejected for `rds_iam`, re-check Step 1 and that the EC2 role's
`rds-db:connect` resource ARN matches the instance `DbiResourceId` + DB user.

## Other AWS credential options

These templates are intentionally focused on the passwordless `aws_rds_iam`
path. If IAM database authentication is not enabled on your instance, Signals
also supports secret-store backed password auth (`auth_method: secret_store`);
the backend is selected by the shape of `secret_ref`:

- **AWS Secrets Manager** — `secret_ref:
  arn:aws:secretsmanager:<region>:<account>:secret:<name>`. Grant the
  collector role `secretsmanager:GetSecretValue` on that one secret.
- **AWS Systems Manager Parameter Store** — `secret_ref:
  arn:aws:ssm:<region>:<account>:parameter/<name>`. Grant `ssm:GetParameter`
  (+ `kms:Decrypt` for a `SecureString`).
- If the secret value is a JSON document (e.g. an RDS-managed secret), set
  `secret_json_key: password` to extract the password field; omit it when the
  secret value is the bare password.

Secret-store targets require `sslmode: verify-full` in every environment —
same as the token methods. See
[`docs/database-connections.md`](../../docs/database-connections.md) for the
full configuration examples.

## Security notes

- **No secrets** in any input or on disk — the token is minted from the EC2
  instance role at connect time and never persisted.
- **`rds-db:connect` is scoped** to exactly `db_user` on this one instance
  (`arn:aws:rds-db:<region>:<acct>:dbuser:<DbiResourceId>/<db_user>`).
- TLS is **`verify-full`** against the RDS global CA bundle (fetched to
  `/etc/signals/rds-ca.pem`).
- IMDSv2 is enforced (`http_tokens = required`); the root volume is encrypted;
  the API listener binds to `127.0.0.1` only.
- **No SSH.** Shell access for the verify steps goes through SSM Session
  Manager (`AmazonSSMManagedInstanceCore` on the collector role); no key pair
  exists and no inbound port is open.
- The loopback **control-plane API token** is a separate credential class from
  the (non-existent) DB password: user-data mints a random 32-byte token and
  writes it to `/etc/signals/signals.env` (root-only, `0600`), which the
  container receives via `docker run --env-file` (`SIGNALS_API_TOKEN`, the same
  env path the Helm chart uses) — by reference, so the value is never on the
  docker command line or in the journal (#292). A plain copy of just the token is
  also stored at `/root/signals-api-token` for the operator verify step. The
  world-readable `signals.yaml` stays secret-free.

## Reusing the identity elsewhere

If you run the collector on ECS or EKS instead of the bundled EC2 instance,
take the `collector_role_arn` output (Terraform) / `CollectorRoleArn` output
(CloudFormation) and attach it as the task role / IRSA role; the
`rds-db:connect` policy is identical. For Kubernetes, see the Helm chart
(`deploy/helm/signals/`) and #114.

## Data durability and upgrades (#501)

The collector's entire store — `signals.db` at **`/data`** — holds the
instance identity (`meta.instance_id`), collection history, and the
cadence/incremental watermarks that let a replacement collector resume
instead of re-collecting from zero. That store lives on a **separate,
encrypted EBS data volume** (`aws_ebs_volume.data` in Terraform /
`DataVolume` in CloudFormation), not the instance root disk and not a Docker
named volume.

- **The data volume is independent of the instance.** `user_data_replace_on_change`
  (Terraform) and a replacement stack update (CloudFormation) replace only the
  compute instance; the data volume carries `prevent_destroy = true`
  (Terraform) / `DeletionPolicy: Retain` + `UpdateReplacePolicy: Retain`
  (CloudFormation). Destroying or replacing the instance **does not** destroy
  the volume.
- **Replacement re-attaches, never re-initializes.** The replacement instance
  attaches the same volume at a stable device, and the boot-time mount is
  **format-if-empty**: a populated volume keeps its `signals.db` untouched, so
  identity, history, and watermarks survive the AMI/instance upgrade. A fresh
  volume is formatted exactly once and owned by the non-root container user
  (uid 10001).
- **Credentials stay external.** No DB password or API token is ever written
  onto `/data`; the control-plane token stays root-only in
  `/etc/signals/signals.env`, outside the data volume.
- **Backups.** Snapshot the EBS data volume (not the root disk) as the backup
  of record for collection history; EBS snapshots are independent of the
  instance.

### Transitioning an existing root-disk install (migration)

Earlier revisions of these modules mounted `/data` from a Docker **named
volume on the root disk** (`-v signals-data:/data`). On such an install
`signals.db` sits on the ephemeral root disk and is lost on the next
instance/AMI replacement. To move to the durable volume **without losing
history**, migrate the store by hand before re-applying — **never let the new
instance initialize an empty store over an existing one**:

1. Over SSM Session Manager, **stop the collector** so the store quiesces:
   `sudo docker stop signals`.
2. Copy the live store off the root-disk named volume to a safe path:
   `sudo docker run --rm -v signals-data:/src -v /root/signals-backup:/dst alpine \
   sh -c 'cp -a /src/. /dst/'` (this captures `signals.db` and any WAL/companion
   files).
3. **Verify** the copy before cutover: it must contain `signals.db`, and
   `sqlite3 /root/signals-backup/signals.db 'select value from meta where key="instance_id"; select count(*) from snapshots;'`
   must show the expected identity and a non-zero history count.
4. `terraform apply` (or update the stack) to provision the durable EBS volume
   and re-launch; on the new instance, with the collector **stopped**, copy the
   verified store onto the mounted durable volume:
   `sudo cp -a /root/signals-backup/. /data/ && sudo chown -R 10001:10001 /data`.
5. Re-verify identity + history from `/data/signals.db`, then start the
   collector. Confirm the next cycle collects incrementally (watermarks intact),
   not a full re-collect.

If you cannot complete and verify this copy, **do not cut over** — a fresh
`/data` seeds a new store and resets identity. **Rollback / recovery note:** if
a pre-fix install already lost `/data` on a prior replacement, that history and
identity **cannot be recovered** (the ephemeral root disk discarded it); the
first apply of this fix seeds a fresh store on the durable volume and
collection restarts from that point.
