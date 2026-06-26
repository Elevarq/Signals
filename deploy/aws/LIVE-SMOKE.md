# AWS `aws_rds_iam` live smoke — operator runbook

End-to-end validation of the passwordless AWS onboarding path
([`deploy/aws/terraform`](terraform/), `auth_method: aws_rds_iam`). It is
**operator-gated** — it provisions real infrastructure and is **not** part
of default CI (mirrors the provider live smokes #94/#95/#96). Tracks
[Elevarq/Signals#201](https://github.com/Elevarq/Signals/issues/201).

The workflow ([`.github/workflows/aws-rds-iam-live-smoke.yml`](../../.github/workflows/aws-rds-iam-live-smoke.yml))
does, in one job:

1. `terraform apply` the module against your IAM-auth RDS instance.
2. Transiently attach `AmazonSSMManagedInstanceCore` to the collector's
   IAM role so the **loopback-only** API can be reached via SSM (the
   production template grants no SSM access — this is added only for the
   smoke and detached at teardown).
3. SSM-exec on the instance: force a collection, then
   `signalsctl status` / `export`, and assert a passwordless snapshot
   (`SNAPSHOT_OK`).
4. **Always** detach the transient policy and `terraform destroy`.

This is the deploy-level smoke. The connection-level smoke (token mint +
`verify-full` connect against an already-running RDS) is the
`integration`-tagged Go test `internal/collector/credprovider_live_test.go`
(`SIGNALS_INTEGRATION_LIVE=1`).

## Prerequisites (one-time)

1. **An IAM-auth-enabled RDS / Aurora PostgreSQL instance.** Enable
   *IAM database authentication* on the instance (Elevarq's shared
   instances have it **disabled** — use a throwaway test instance).
2. **The one-time DB grant**, run once as a privileged role (see
   [`README.md`](README.md) Step 1):
   ```sql
   CREATE ROLE signals LOGIN;
   GRANT rds_iam TO signals;
   GRANT pg_monitor TO signals;
   ```
3. **Network**: a subnet that routes to the instance, and a security
   group that allows egress to the DB port. The collector needs outbound
   HTTPS to pull the image (`ghcr.io`) and the RDS CA bundle, and SSM
   endpoints reachable (NAT or SSM VPC endpoints).
4. **A GitHub→AWS OIDC role**, exposed as the repo variable
   **`AWS_LIVE_SMOKE_ROLE_ARN`** (Settings → Secrets and variables →
   Actions → Variables). The role must:
   - Trust this repo's GitHub OIDC identity
     (`token.actions.githubusercontent.com`, `sub` scoped to
     `repo:Elevarq/Signals:*` — tighten to `ref`/`environment` if you
     prefer).
   - Be allowed to apply/destroy the module:
     `iam:CreateRole`, `iam:DeleteRole`, `iam:PutRolePolicy`,
     `iam:DeleteRolePolicy`, `iam:GetRole*`,
     `iam:CreateInstanceProfile`, `iam:DeleteInstanceProfile`,
     `iam:AddRoleToInstanceProfile`, `iam:RemoveRoleFromInstanceProfile`,
     `iam:PassRole`, `iam:TagRole`,
     `ec2:RunInstances`, `ec2:TerminateInstances`,
     `ec2:Describe*`, `ec2:CreateTags`, `ssm:GetParameters`.
   - Be allowed to drive the verification:
     `iam:AttachRolePolicy`, `iam:DetachRolePolicy` (scoped to the
     collector role / the `AmazonSSMManagedInstanceCore` ARN),
     `ssm:SendCommand`, `ssm:GetCommandInvocation`,
     `ssm:DescribeInstanceInformation`.

   Keep this role least-privilege and dedicated to the smoke. No
   long-lived AWS keys are used — auth is GitHub OIDC.

## Running it

GitHub → **Actions** → **AWS RDS IAM Live Smoke** → **Run workflow**, then
fill the inputs:

| Input | Example | Notes |
|-------|---------|-------|
| `region` | `us-east-1` | region of the RDS instance |
| `db_host` | `mydb.abc123.us-east-1.rds.amazonaws.com` | RDS endpoint |
| `db_name` | `appdb` | database to connect to |
| `db_resource_id` | `db-ABCDEFGH00000000000000` | the **DbiResourceId**, not the DB id |
| `subnet_id` | `subnet-0123…` | must route to the DB |
| `security_group_ids` | `["sg-0123…"]` | **JSON array** string |
| `db_user` | `signals` | role granted `rds_iam` + `pg_monitor` |
| `name_prefix` | `signals-livesmoke` | prefixes the throwaway resources |
| `image_uri` | `ghcr.io/elevarq/signals:1.0.0` | pinned tag |

A green run means: the collector minted an RDS IAM token from its instance
role, connected `verify-full` with **no password on disk**, and produced
at least one snapshot — then everything was destroyed.

## Cleanup / failure modes

- The teardown step runs with `if: always()`, so a normal failure still
  detaches the transient SSM policy and destroys the stack.
- **If you cancel the job** between apply and teardown, resources can
  leak. Recover by re-running with the same inputs (Terraform will
  reconcile) or manually: detach `AmazonSSMManagedInstanceCore` from the
  `<name_prefix>-collector` role, then terminate the instance and delete
  the role / instance-profile (all tagged
  `app.kubernetes.io/name=signals`, `managed-by=terraform`).
- Cost: one `t3.small` + a 20 GiB encrypted EBS volume for the run's
  duration only.

## Azure / GCP variants

The Azure (`azure_entra`) and GCP (`gcp_cloudsql_iam`) deploy paths follow
the same shape and are tracked alongside #201. They are **not yet
automated** — porting this workflow (swap the cloud auth action and the
verify path) is the remaining follow-up for full multi-cloud live
coverage.
