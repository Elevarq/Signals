# Compatibility & support matrix

Tracking: [#139](https://github.com/Elevarq/Arq-Signals/issues/139).

This page documents the PostgreSQL versions, managed-database
providers, extensions, permissions, and network requirements
Elevarq Signals supports for the v1.0 release. The matrix
distinguishes **supported** (tested + green in CI), **best-effort**
(works, monitored by support, no SLA), **unsupported** (known
gaps), and **planned** (on the roadmap, not yet shipped).

The runtime `arqctl doctor` command (and the `--json` form for
automation) reports compatibility gaps against the live target.
See [§ Operator preflight](#operator-preflight) below.

## PostgreSQL major versions

| Major | Status | Notes |
|---|---|---|
| 14 | **Supported** | Catalog mappings, regression tests, release smoke. |
| 15 | **Supported** | Same as 14. |
| 16 | **Supported** | Same as 14. |
| 17 | **Supported** | Includes the `pg_stat_progress_vacuum` PG-17 column additions (caught pre-Beta; see `CHANGELOG.md` 0.5.0). |
| 18 | **Supported** | Latest tested major; release smoke runs against PG 18 too. |
| 19+ | **Experimental** | `IsExperimentalMajor()` accepts PG 19 with a warning so operators on rolling early-adopter clusters aren't blocked. Collectors that depend on PG-19-only columns are gated; the rest run unchanged. |
| 12, 13 | **Unsupported** | EOL upstream. No catalog mapping. Collector refuses to start with `unsupported_pg_major`. |
| ≤ 11 | **Unsupported** | Out of scope. |

Source of truth: `internal/pgqueries/discovery.go::SupportedMajors`.

## Managed-database providers

| Provider | Status | Notes |
|---|---|---|
| Self-hosted PostgreSQL | **Supported** | The reference deployment. |
| AWS RDS for PostgreSQL | **Supported** | Standard read permissions; `pg_monitor` role required for some signal collectors. The `rds_superuser` role is NOT required. |
| AWS RDS Aurora PostgreSQL-compatible | **Best-effort** | Aurora's catalog implements PG-compatible system views; common collectors work. The `pg_stat_statements_info.dealloc` column (used by `pgss_capacity_v1`) is present in Aurora. Aurora-Serverless's elastic-IOPS surface is invisible to the collector — operator-declared values in the analyzer's TargetContext are the right path (Elevarq/Arq-Workbench#242). |
| Google Cloud SQL for PostgreSQL | **Supported** | Standard read permissions. The `cloudsqlsuperuser` role is sufficient; the collector does NOT require superuser. |
| AlloyDB | **Best-effort** | PG-compatible catalog. Storage / IOPS abstraction is invisible to the collector; same TargetContext path applies. |
| Azure Database for PostgreSQL — Flexible Server | **Supported** | Standard read permissions; `azure_pg_admin` is NOT required. |
| Azure Database for PostgreSQL — Single Server | **Unsupported** | Retired upstream. |
| Crunchy Bridge / Supabase / Render / Neon | **Best-effort** | PG-compatible; community-reported working. No automated CI coverage. |
| Citus / CockroachDB / YugabyteDB | **Unsupported** | Distributed SQL forks; catalog and pg_stat semantics diverge. |

## PostgreSQL extensions

The collector reads catalogs / functions that are part of vanilla
PostgreSQL OR managed-provider standard images. Some collectors
have OPTIONAL upgrades when an extension is installed.

| Extension | Required? | Used by | Behaviour when missing |
|---|---|---|---|
| `pg_stat_statements` | **Recommended** | `pgss_capacity_v1`, `pgss_top_statements_v1`, planner-corpus drivers | Workload-shape collectors skip; informational warning. |
| `pgstattuple` | Optional | `pg_class_storage_v1` bloat upgrade | Falls back to `pg_class.relpages`-only estimate. |
| `hypopg` | Not used at collection time | (Analyzer-side index advice) | N/A — collector doesn't reach for hypopg. |
| `pg_partman` / `pg_repack` / etc. | Not used | — | Collector doesn't depend on management extensions. |

Extension presence is captured by the `pg_extensions_v1`
collector; the analyzer consumes it to gate rules.

## Permissions

Elevarq Signals is designed to run as a **non-superuser** role.

### Minimum read permissions

| Need | Grant |
|---|---|
| Connect | `GRANT CONNECT ON DATABASE <db> TO arq_monitor;` |
| Read public schema | `GRANT USAGE ON SCHEMA public TO arq_monitor;` |
| Read all tables | `GRANT SELECT ON ALL TABLES IN SCHEMA public TO arq_monitor;` + `ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO arq_monitor;` |
| Read pg_stat_statements | Grant membership in `pg_monitor` (or `pg_read_all_stats` + `pg_read_server_files` on PG 14+). |

### Roles by environment

| Environment | Recommended role |
|---|---|
| Self-hosted | `pg_monitor` membership + `SELECT` on application schemas. |
| RDS PostgreSQL | `pg_monitor` (available since PG 10). |
| RDS Aurora | `rds_pg_monitor` (the Aurora equivalent). |
| Cloud SQL | `cloudsqlsuperuser` is sufficient (overkill but the typical Cloud SQL operator role). |
| AlloyDB | `pg_monitor` plus the AlloyDB-specific read role if present. |
| Azure Flex | Membership in the `azure_pg_admin_role` group or the dedicated read role. |

The `arqctl doctor` command's `role_safe` check refuses to run
as superuser by default (override with `ARQ_SIGNALS_ALLOW_UNSAFE_ROLE=1`
for evaluation only).

## Network requirements

- **Inbound to Postgres**: 5432/tcp (or operator-configured port)
  from the Signals collector. No additional ports.
- **Outbound from Signals**: NONE in v1. The collector is a
  pull-only service against the target Postgres. Snapshots are
  written to a local SQLite store; export is operator-driven.
- **TLS**: `sslmode=verify-full` recommended in production;
  `verify-ca` acceptable; `prefer` emits a warning (target identity
  not verified). `disable` only with
  `ARQ_ALLOW_INSECURE_PG_TLS=1` and never in `env=prod`.

## Container / Kubernetes compatibility

| Surface | Status |
|---|---|
| Container image (multi-arch: `linux/amd64`, `linux/arm64`) | **Supported**. Distroless base. Non-root UID 65532. |
| Docker / Docker Compose | **Supported** for local development. See `examples/docker-compose.yml`. |
| Kubernetes ≥ 1.27 | **Supported**. Helm chart at `deploy/helm/arq-signals/`. Production profile rules in [`docs/install/kubernetes-production.md`](../install/kubernetes-production.md) (filed by #140). |
| Kubernetes < 1.27 | **Best-effort**. Chart should render but no CI coverage. |
| Helm 3 | **Supported**. |
| Helm 2 | **Unsupported**. |
| OpenShift | **Best-effort**. Chart uses standard PSP-free patterns; SCC mapping is operator-side. |

## Architecture

| Arch | Status |
|---|---|
| `linux/amd64` | **Supported**. |
| `linux/arm64` | **Supported**. Verified on Apple Silicon dev machines + AWS Graviton. |
| Other | **Unsupported**. No CI coverage. |

## Operator preflight

The `arqctl doctor` command runs read-only checks against the
configured target and reports compatibility gaps in the closed
check-id schema (`config_valid`, `target_reachable`, `role_safe`,
`collector_prerequisites`, `snapshot_freshness`, `store_writable`).

Run before deployment:

```sh
arqctl doctor --config config.yaml
```

JSON output for automation:

```sh
arqctl doctor --config config.yaml --json
```

A failing check produces a `status: "fail"` entry with a closed
reason string + the next operator action. Non-zero exit code
signals at least one failed check.

## Footnotes

- **Sales / SE quick-answer**: if the prospect's stack is in the
  "Supported" column for PG version + managed provider + Kubernetes
  version, Elevarq Signals fits. Best-effort answers should be flagged
  for a brief technical exchange before commitment.
- **Unsupported is final**: distributed forks (Citus, Cockroach,
  Yugabyte) and Single-Server Azure are out of scope for v1.0 by
  design, not by gap.
- **Planned** entries land in this matrix as **Supported** or
  **Best-effort** when CI coverage exists.
