# Helm Chart Multiple Targets — Specification

## Status

ACTIVE

## Purpose

The Signals Helm chart renders a single PostgreSQL target (`.Values.target`)
into the mounted `signals.yaml` ConfigMap. The collector engine already
supports monitoring many databases from one daemon (`signals.yaml`
`targets:` list; `max_concurrent_targets`), but the chart cannot express
it, so a Helm/EKS (including AWS Marketplace) install can point Signals at
only one database.

This specification adds an optional `.Values.targets` list so one chart
install can monitor multiple databases, without changing the behaviour of
existing single-target installs.

## Interfaces

Input: `.Values.targets` — an optional array. Each item mirrors the
`.Values.target` field surface:

| values key | rendered `signals.yaml` key | notes |
|------------|-----------------------------|-------|
| `name` | `name` | required, identifies the target |
| `host` | `host` | required, enables the entry |
| `port` | `port` | default 5432 |
| `dbname` | `dbname` | default `postgres` |
| `user` | `user` | default `signals` |
| `sslmode` | `sslmode` | default `prefer` |
| `sslRootCertFile` | `sslrootcert_file` | path only |
| `authMethod` | `auth_method` | `aws_rds_iam`/`azure_entra`/`gcp_cloudsql_iam`/`secret_store`/`mtls` |
| `region` | `region` | |
| `azureClientId` | `azure_client_id` | |
| `gcpImpersonateServiceAccount` | `gcp_impersonate_service_account` | |
| `secretRef` | `secret_ref` | reference, not a secret |
| `sslCertFile` | `sslcert` | mtls path |
| `sslKeyFile` | `sslkey` | mtls path |
| `sslKeyPassphraseFile` | `sslkey_passphrase_file` | mtls path |
| `passwordFile` | `password_file` | path to a mounted password file |
| `passwordEnv` | `password_env` | name of an env var (user-provided via `extraEnv`) |

Output: the `signals.yaml` ConfigMap `targets:` list.

## Rules

- HMT-R001: When `.Values.targets` is a non-empty list, the ConfigMap MUST
  render one `targets:` entry per item, in list order.
- HMT-R002: For each item, required fields (`name`, `host`, `port`,
  `dbname`, `user`, `sslmode`) MUST always render; every optional field
  above MUST render only when set, mapped to the `signals.yaml` key in the
  table.
- HMT-R003: When both `.Values.target.host` and `.Values.targets` are set,
  the single `default` target renders first, followed by the list entries.
- HMT-R006: Password authentication for list items uses `passwordFile` /
  `passwordEnv` only. The chart-managed `target.passwordSecretName` ->
  `PG_PASSWORD` Secret injection remains scoped to the single `.Values.target`
  block (one managed password env per install); multi-target password auth
  supplies its own env/volume via `extraEnv` / `extraVolumes`.
- HMT-R007: `values.schema.json` MUST declare `targets` as an array of typed
  target objects (`name` + `host` required per item; `mtls` requires
  `sslCertFile` + `sslKeyFile`) so the Amazon EKS add-on configuration schema
  exposes the multi-target surface (parity with
  [`marketplace-eks-addon-delivery`](marketplace-eks-addon-delivery.md)).

## Invariants

- INV-HMT-01: **Back-compat / byte stability.** When `.Values.targets` is
  empty or unset and `.Values.target.host` is set, the rendered `signals.yaml`
  is byte-for-byte identical to the pre-change single-target output (one
  `default` entry).
- INV-HMT-02: No credential contents ever enter values or the ConfigMap —
  only filesystem paths, cloud references, and env-var names (same guarantee
  as the single `target` block).

## Failure conditions

- A `targets` item missing `name` or `host`, or an `mtls` item missing
  `sslCertFile`/`sslKeyFile`, MUST fail `helm template` schema validation
  (fail-closed) rather than render an unusable target.

## Constraints

- HMT-R005: When neither `.Values.target.host` nor `.Values.targets` is set,
  no `targets:` key is emitted (unchanged).
- The list is additive to the existing chart surface; no existing value
  changes meaning.

## Backward compatibility

- Existing installs that set only `.Values.target` are unaffected
  (INV-HMT-01; HMT-R005 preserves the empty case). `.Values.targets` defaults
  to `[]`.
