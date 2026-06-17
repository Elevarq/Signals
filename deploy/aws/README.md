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
apply/deploy, on the collector instance:

```bash
# the collector container should be running and collecting
docker logs signals 2>&1 | grep -iE "collector|snapshot|connected"
# trigger an export to confirm a successful passwordless connection
docker exec signals signalsctl export --output /data/snapshot.zip
```

A healthy run connects with **no password in config**, mints a token from the
instance role, and collects at least one snapshot. If the connection is
rejected for `rds_iam`, re-check Step 1 and that the EC2 role's
`rds-db:connect` resource ARN matches the instance `DbiResourceId` + DB user.

## Security notes

- **No secrets** in any input or on disk — the token is minted from the EC2
  instance role at connect time and never persisted.
- **`rds-db:connect` is scoped** to exactly `db_user` on this one instance
  (`arn:aws:rds-db:<region>:<acct>:dbuser:<DbiResourceId>/<db_user>`).
- TLS is **`verify-full`** against the RDS global CA bundle (fetched to
  `/etc/signals/rds-ca.pem`).
- IMDSv2 is enforced (`http_tokens = required`); the root volume is encrypted;
  the API listener binds to `127.0.0.1` only.

## Reusing the identity elsewhere

If you run the collector on ECS or EKS instead of the bundled EC2 instance,
take the `collector_role_arn` output (Terraform) / `CollectorRoleArn` output
(CloudFormation) and attach it as the task role / IRSA role; the
`rds-db:connect` policy is identical. For Kubernetes, see the Helm chart
(`deploy/helm/signals/`) and #114.
