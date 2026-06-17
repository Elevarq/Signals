# arq-signals Helm chart

Deploys the arq-signals collector. The collector reads a single
PostgreSQL target from the mounted `signals.yaml` ConfigMap. Set
`target.host` to render that target; leave it empty and no target
block is emitted.

> Cloud-side setup (DB roles, IAM bindings, CA bundles) lives in
> [`docs/database-connections.md`](../../../docs/database-connections.md).
> This README covers only the Helm wiring.

## Authentication methods

`target.authMethod` selects how the collector authenticates. The
default (empty) is the password method; the rest are **passwordless** —
the pod's ambient cloud identity mints the credential at connect time.

| `authMethod` | Credential source | Password? |
|---|---|---|
| `""` (default) | Kubernetes Secret → `PG_PASSWORD` | yes |
| `aws_rds_iam` | RDS IAM token from the pod's AWS identity | no |
| `azure_entra` | Entra OAuth2 token from the pod's Azure identity | no |
| `gcp_cloudsql_iam` | Google OAuth2 token from the pod's GCP identity | no |
| `secret_store` | Password fetched from a cloud vault | no |

Invariants enforced by the chart and the collector:

- **FC005** — passwordless methods carry no password source. Setting
  `target.passwordSecretName` together with a non-empty `authMethod` is
  ignored: neither `password_env` nor the `PG_PASSWORD` secret mount is
  rendered.
- **FC006** — cloud methods require `sslmode: verify-full`. In `prod`,
  `verify-ca`/`verify-full` also hard-require `target.sslRootCertFile`
  pointing at a mounted CA bundle (see `extraVolumes` below).

## Password method (default)

```yaml
target:
  host: db.internal
  dbname: appdb
  user: arq_signals
  sslmode: verify-full
  sslRootCertFile: /etc/ssl/db/ca.pem
  passwordSecretName: pg-cred     # Secret holding the password
  passwordSecretKey: password
```

The password is injected as `PG_PASSWORD` via `secretKeyRef`; it never
lands in the ConfigMap or any rendered manifest beyond the Secret name.

## Per-platform passwordless wiring

### AWS — EKS IRSA (`aws_rds_iam`)

Bind the IAM role through the ServiceAccount annotation. IRSA injects
the web-identity token automatically.

```yaml
serviceAccount:
  create: true
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::<account>:role/arq-signals-irsa

target:
  host: mydb.abc123.us-east-1.rds.amazonaws.com
  dbname: appdb
  user: arq_signals
  authMethod: aws_rds_iam
  region: us-east-1                 # optional; inferred from AWS_REGION / IMDS
  sslmode: verify-full
  sslRootCertFile: /etc/ssl/db/rds-global-bundle.pem

extraVolumes:
  - name: db-ca
    secret:
      secretName: rds-ca-bundle
extraVolumeMounts:
  - name: db-ca
    mountPath: /etc/ssl/db
    readOnly: true
```

### AWS — EKS Pod Identity (`aws_rds_iam`)

Pod Identity needs **no** ServiceAccount annotation — the association
between the SA and the IAM role is created out-of-band
(`aws eks create-pod-identity-association`). Keep `serviceAccount.create:
true` so there is a stable SA to associate.

```yaml
serviceAccount:
  create: true
  annotations: {}                   # association is created out-of-band

target:
  host: mydb.abc123.us-east-1.rds.amazonaws.com
  dbname: appdb
  user: arq_signals
  authMethod: aws_rds_iam
  sslmode: verify-full
  sslRootCertFile: /etc/ssl/db/rds-global-bundle.pem
# ... extraVolumes / extraVolumeMounts as above
```

### GCP — GKE Workload Identity (`gcp_cloudsql_iam`)

Bind the Google service account through the ServiceAccount annotation.

```yaml
serviceAccount:
  create: true
  annotations:
    iam.gke.io/gcp-service-account: signals-collector@<project>.iam.gserviceaccount.com

target:
  host: 10.0.0.5                    # private IP or Cloud SQL proxy endpoint
  dbname: appdb
  user: "signals-collector@<project>.iam"   # SA email without .gserviceaccount.com
  authMethod: gcp_cloudsql_iam
  sslmode: verify-full
  sslRootCertFile: /etc/ssl/db/server-ca.pem

extraVolumes:
  - name: db-ca
    secret:
      secretName: cloudsql-ca
extraVolumeMounts:
  - name: db-ca
    mountPath: /etc/ssl/db
    readOnly: true
```

### Azure — AKS Workload Identity (`azure_entra`)

AKS workload identity needs **both** the ServiceAccount annotation and
a pod label so the webhook injects the projected federated token.

```yaml
serviceAccount:
  create: true
  annotations:
    azure.workload.identity/client-id: <client-id>

podLabels:
  azure.workload.identity/use: "true"

target:
  host: myserver.postgres.database.azure.com
  dbname: appdb
  user: arq_signals                 # must equal the Entra principal name
  authMethod: azure_entra
  azureClientId: <client-id>        # optional; user-assigned MI disambiguation
  sslmode: verify-full
  sslRootCertFile: /etc/ssl/db/DigiCertGlobalRootCA.crt.pem

extraVolumes:
  - name: db-ca
    secret:
      secretName: azure-db-ca
extraVolumeMounts:
  - name: db-ca
    mountPath: /etc/ssl/db
    readOnly: true
```

### `secret_store` — password in a cloud vault

Use any of the platform identity bindings above (IRSA / Pod Identity /
GKE WI / AKS WI) to authorize the vault read, then point `secretRef` at
the secret. The backend is inferred from the `secretRef` shape.

```yaml
serviceAccount:
  create: true
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::<account>:role/arq-signals-vault

target:
  host: db.internal
  dbname: appdb
  user: arq_signals
  authMethod: secret_store
  secretRef: arn:aws:secretsmanager:us-east-1:123456789012:secret:prod/arq_signals-AbCdEf
  sslmode: verify-full
  sslRootCertFile: /etc/ssl/db/ca.pem
# ... extraVolumes / extraVolumeMounts for the CA bundle
```

## CA bundles for `verify-full`

`verify-full` needs a CA bundle on disk. Mount it with `extraVolumes` /
`extraVolumeMounts` (from a Secret or ConfigMap — never inline secrets
in values) and set `target.sslRootCertFile` to the mount path. In
`prod`, a cloud method without `sslRootCertFile` fails at startup.
