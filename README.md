# Elevarq Signals

Elevarq Signals is a read-only PostgreSQL diagnostic collector. It runs on
your infrastructure, collects statistics from your databases, and
packages them as portable snapshots. No data leaves your machine. No AI.
No cloud. Just structured evidence from the views PostgreSQL already
exposes.

From [Elevarq](https://elevarq.com) — PostgreSQL tools for engineering teams.

[![CI](https://github.com/elevarq/arq-signals/actions/workflows/ci.yml/badge.svg)](https://github.com/elevarq/arq-signals/actions/workflows/ci.yml)
[![License: BSD-3-Clause](https://img.shields.io/badge/License-BSD--3--Clause-blue.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/elevarq/arq-signals)](https://goreportcard.com/report/github.com/elevarq/arq-signals)
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/13020/badge)](https://www.bestpractices.dev/projects/13020)

> **Read-only by design** — three independent enforcement layers prevent
> any write operations. Unsafe roles (superuser, replication) are blocked
> before collection starts.
> [Every SQL query is in the source.](internal/pgqueries/catalog.go)
>
> **No cloud, no phone-home** — all data stays on your machine. No
> telemetry, no analytics, no external network calls.
>
> **No AI inside** — Elevarq Signals is a pure data collector. No language
> models, no scoring, no recommendations. What you collect is what you get.
>
> **Built for restricted environments** — runs airgapped, as a non-root
> container, with no internet dependency. Suitable for networks where
> third-party monitoring agents are not permitted.

---

## Try it in 2 minutes

```bash
git clone https://github.com/elevarq/arq-signals.git
cd arq-signals
docker compose -f examples/docker-compose.yml up -d
```

This starts Elevarq Signals alongside PostgreSQL 16 with a pre-configured
monitoring role. Collection begins automatically.

```bash
# Trigger an immediate collection
curl -X POST http://localhost:8081/collect/now \
  -H "Authorization: Bearer dev-local-only-replace-in-prod-32chars"

# Download your first snapshot
curl -o snapshot.zip http://localhost:8081/export \
  -H "Authorization: Bearer dev-local-only-replace-in-prod-32chars"

# Inspect the contents
unzip -l snapshot.zip
```

Your snapshot contains raw PostgreSQL statistics in structured JSON —
nothing more. See [`examples/snapshot-example/`](examples/snapshot-example/)
for what the output looks like.

---

## Why Elevarq Signals exists

Every PostgreSQL instance exposes diagnostic data through built-in
statistics views. But collecting this data consistently, safely, and in
a format you can actually use takes tooling that most teams end up
building themselves.

Elevarq Signals handles the collection part so you don't have to. It
connects with a read-only role, runs approved SQL queries on a schedule,
and writes structured results to local storage. When you need the data
elsewhere, it packages everything as a portable ZIP snapshot.

The project is open source because we think data collection should be
transparent. You can read every SQL query Elevarq Signals will run. You can
audit the binary. You own the output.

## What Elevarq Signals does

- Connects to one or more PostgreSQL instances (14+)
- Runs 99 read-only diagnostic collectors covering:
  - Server configuration, identity, and cluster fingerprint
  - Session activity and connection pressure
  - Table, index, and I/O statistics (incl. `pg_stat_io` /
    `pg_stat_wal`)
  - Schema metadata: columns, constraints, indexes, sequences,
    triggers, views, materialised views, partitions, functions
  - Storage placement: tablespaces, per-relation storage, per-
    attribute storage
  - Query intelligence (via `pg_stat_statements`, self-filtered
    to exclude Signals' own probe queries and scoped to the
    connected database)
  - Transaction wraparound risk and prepared-transaction age
  - Vacuum and autovacuum health
  - **In-flight operation progress** — six `pg_stat_progress_*`
    collectors covering vacuum, analyze, create_index, cluster,
    basebackup, copy
  - **Index hygiene** — derived findings for unused, invalid,
    redundant, and duplicate indexes
  - **Bloat estimation** — statistical table and index bloat
    without `pgstattuple` (runs on managed PG)
  - Replication: physical (`pg_stat_replication`), slot risk
    (`pg_replication_slots`), and logical-slot health
    (`pg_stat_replication_slots`)
  - Checkpoint, background writer, and checkpointer pressure
  - Storage growth, largest relations, temp I/O pressure
  - Per-role / per-database configuration overrides
  - Foreign data wrappers and partition topology
  - Vector / pgvector column inventory
  - Role capabilities and login-role surface
- Stores results locally in SQLite as structured NDJSON
- Schedules collection with configurable cadences (5m to 7d per query)
- Packages snapshots as portable ZIP archives
- Exposes a local HTTP API for triggering collection, pausing /
  resuming targets, and reloading configuration
- Provides a CLI (`signalsctl`) for operations, including pre-flight
  diagnostics (`signalsctl doctor`) and classified connection-test
  (`signalsctl connect test`)
- Per-target sensitivity profiles, per-class retention, and a
  per-target circuit breaker for operator safety during incidents

## Specification & Test-Driven Development (STDD)

Elevarq Signals is developed using STDD — a methodology where the
specification and tests define the system, and code is a replaceable
artifact that must satisfy both.

The repository contains:

- **Formal specification** — 104 numbered requirements covering
  collection, safety, configuration, API, persistence, and diagnostics
  ([specification.md](features/arq-signals/specification.md))
- **Acceptance tests** — 240+ test cases derived directly from the
  specification (per-collector acceptance files under
  [`specifications/collectors/`](specifications/collectors/),
  plus the cross-cutting
  [`acceptance-tests.md`](features/arq-signals/acceptance-tests.md))
- **Traceability matrix** — every requirement mapped to executable
  tests with evidence classification (behavioral, structural, or
  integration)
  ([traceability.md](features/arq-signals/traceability.md))
- **Language-neutral contracts** — API and configuration schemas
  defined as appendices, independent of the Go implementation
  ([Appendix A](features/arq-signals/appendix-a-api-contract.md),
  [Appendix B](features/arq-signals/appendix-b-configuration-schema.md))

This approach matters for a tool that connects to production databases.
Every safety guarantee — read-only enforcement, role validation,
credential handling — is formally specified, tested, and traceable.
You can verify the claims without reading the implementation.

## Why DBAs trust Elevarq Signals

- All PostgreSQL queries execute inside `READ ONLY` transactions,
  enforced at three independent layers
- Role safety validation blocks superuser, replication, and bypassrls
  roles before any query runs
- Defensive session timeouts (`statement_timeout`, `lock_timeout`,
  `idle_in_transaction_session_timeout`) prevent runaway queries
- The collector never performs write operations on PostgreSQL — this is
  enforced by static SQL linting, session configuration, and
  transaction access mode
- Credentials are never stored in snapshots, export metadata, API
  responses, or log output
- If an unsafe role override is used, it is explicitly recorded in
  export metadata with the specific bypassed checks
- The entire safety model is formally specified and covered by
  800+ automated tests across the module

For the full safety model, see
[docs/runtime-safety-model.md](docs/runtime-safety-model.md).

## Examples

| Example | Description |
|---------|-------------|
| [Local safe role](examples/local-safe-role/) | Recommended production setup with `signals` monitoring role |
| [Local superuser override](examples/local-superuser-override/) | Dev/test setup with postgres superuser (unsafe override) |
| [Docker](examples/docker/) | Container build, run, and export workflow |
| [Docker Compose](examples/docker-compose.yml) | Quick start with PostgreSQL 16 |
| [Helm](examples/helm/) | Kubernetes deployment with the starter Helm chart |
| [Snapshot inspection](examples/snapshot-inspection/) | How to inspect and understand export output |
| [Snapshot example](examples/snapshot-example/) | Static reference snapshot for offline review |

## Supported PostgreSQL versions

Elevarq Signals has first-class catalog support for **PostgreSQL 14, 15,
16, 17, and 18**. Each major has its own catalog file
(`internal/pgqueries/catalog_pgN.go`) that carries the SQL needed
when a `pg_stat_*` view's column shape differs from the version-
agnostic default. Logical collector IDs (e.g. `pg_stat_io_v1`) stay
stable across majors — only the SQL underneath changes.

A per-cycle discovery probe runs first on each target and returns
the server's `version`, `server_version_num`, installed extensions,
current database, and current user. Catalog selection is driven by
that probe, not by configured assumption. Version-specific collectors
(e.g. `checkpointer_stats_v1` on PG 17+) and extension-gated
collectors (e.g. `pg_stat_statements_v1`) are included or skipped
automatically.

**PostgreSQL 19** is treated as **experimental**: the daemon falls
back to the highest supported catalog (PG 18) and logs a startup
warning so the experimental status is visible. PostgreSQL versions
below 14 are out of scope.

The full **compatibility & support matrix** — including managed-
provider coverage (RDS / Cloud SQL / AlloyDB / Aurora / Azure
Flex), extension dependencies, permission grants per environment,
Kubernetes-version support, and the in/out-of-scope statements
for distributed-SQL forks — lives at
[`docs/compatibility/support-matrix.md`](docs/compatibility/support-matrix.md).
Sales and solutions engineers can answer prospect compatibility
questions against that page without reading source.

## Security model

Elevarq Signals is local-first by design:

- **No data egress.** The daemon writes snapshots only to local
  storage (a SQLite file under `database.path`) and serves them via
  the HTTP API on the operator's listener. There is no outbound
  telemetry, no LLM call, no analytics ping. The repository's
  boundary tests assert this at every build.
- **Read-only PostgreSQL access.** Three layers: a static SQL linter
  rejects DDL/DML at registration; the session is set to
  `default_transaction_read_only=on`; every collector query runs
  inside a `BEGIN ... READ ONLY` transaction. Roles with `rolsuper`,
  `rolreplication`, or `rolbypassrls` are refused.
- **No secrets in artifacts.** Passwords, API tokens, and DSNs never
  appear in logs, exports, or the metrics endpoint. A central
  audit-event denylist filters secret-shaped attribute keys before
  any slog record is emitted (R078).
- **Operator-controlled sensitivity.** The Prometheus `/metrics`
  endpoint (R079) and the per-collector export view (R080) are
  opt-in and have no effect unless explicitly enabled in
  `signals.yaml`. The high-sensitivity collector pack (R075, revised
  2026-05) runs **by default** (collect-everything default); operators
  who prefer privacy over diagnostic richness opt **out** with
  `signals.high_sensitivity_collectors_enabled: false`, which redacts
  the listed `SensitiveColumns` for collectors with non-sensitive
  diagnostic columns (the live `pg_stat_activity` collectors) and
  skips collectors whose row is itself the sensitive payload (DDL
  definitions, sampled-value stats, RLS policies, rewrite rules). The
  effective state is recorded in
  `metadata.json.high_sensitivity_collectors_enabled` for auditors.

## Verifying a release

Releases are published as multi-arch container images at
`ghcr.io/elevarq/signals` with cosign keyless signatures, an
SPDX SBOM (OCI attestation **and** downloadable file), and a SLSA
build provenance attestation (`mode=max`).

Quick signature verification:

```bash
cosign verify ghcr.io/elevarq/signals:<VERSION> \
  --certificate-identity-regexp='github.com/Elevarq/(Arq-Signals|signals)/.github/workflows/release.yml@' \
  --certificate-oidc-issuer='https://token.actions.githubusercontent.com'
```

Inspect the SBOM (registry attestation):

```bash
cosign download sbom ghcr.io/elevarq/signals:<VERSION> > sbom.spdx.json
```

Confirm the image is multi-arch:

```bash
docker buildx imagetools inspect ghcr.io/elevarq/signals:<VERSION>
```

Full operator checklist (provenance, Trivy re-scan, OCI labels, etc.):
[`docs/release-verification.md`](docs/release-verification.md).

## Installation

### Docker Compose (recommended for trying)

```bash
git clone https://github.com/elevarq/arq-signals.git
cd arq-signals
docker compose -f examples/docker-compose.yml up -d
```

### Docker (bring your own PostgreSQL)

```bash
docker run -d --name signals \
  -e SIGNALS_TARGET_HOST=host.docker.internal \
  -e SIGNALS_TARGET_USER=signals \
  -e SIGNALS_TARGET_DBNAME=postgres \
  -e SIGNALS_TARGET_PASSWORD_ENV=PG_PASSWORD \
  -e PG_PASSWORD=your_password \
  -e SIGNALS_ALLOW_INSECURE_PG_TLS=true \
  -e SIGNALS_ENV=dev \
  -v signals-data:/data \
  -p 8081:8081 \
  ghcr.io/elevarq/signals:latest
```

### Build from source

```bash
git clone https://github.com/elevarq/arq-signals.git
cd arq-signals
make build    # produces bin/signals and bin/signalsctl
./bin/signals --config signals.yaml
```

See [`examples/signals.yaml`](examples/signals.yaml) for a complete
annotated configuration file.

### Recommended PostgreSQL role

Elevarq Signals is designed to run using a dedicated monitoring role, not
the PostgreSQL superuser. For production use, create a role such as
`signals` and grant the `pg_monitor` predefined role:

```sql
CREATE ROLE signals LOGIN;
GRANT pg_monitor TO signals;
GRANT CONNECT ON DATABASE your_database TO signals;

-- Optional: enable query-level statistics
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
```

The default `postgres` role is a superuser and will be rejected by the
safety model unless the operator explicitly enables unsafe override
mode (`SIGNALS_ALLOW_UNSAFE_ROLE=true`). This behavior is
intentional — it prevents accidental execution with elevated
privileges in production.

For a full discussion of the role posture — including what additional
access (if any) the high-sensitivity collector pack needs and how to
audit the role — see [`docs/postgres-role.md`](docs/postgres-role.md).

### Optional: Prometheus metrics

Elevarq Signals can expose an opt-in `/metrics` endpoint with operational
counters and gauges (collection outcomes, export outcomes, sqlite
persistence health, high-sensitivity gate state). The endpoint never
publishes collected PostgreSQL data. Off by default; enable with
`signals.metrics_enabled: true`. See
[`docs/prometheus.md`](docs/prometheus.md) for the safety scope
guarantees and
[`docs/metrics-consumer-guide.md`](docs/metrics-consumer-guide.md)
for the full metric inventory, scrape configuration, and
recommended alerting rules.

## Using Elevarq Signals

### Trigger a collection

```bash
# Via CLI
signalsctl collect now

# Via API
curl -X POST http://localhost:8081/collect/now \
  -H "Authorization: Bearer $SIGNALS_API_TOKEN"
```

### Export snapshots

```bash
# Via CLI
signalsctl export --output snapshot.zip

# Via API
curl -o snapshot.zip http://localhost:8081/export \
  -H "Authorization: Bearer $SIGNALS_API_TOKEN"
```

### Pre-flight diagnostics

```bash
# Check config, store path, target reachability, role safety,
# collector prerequisites, and snapshot freshness in one pass.
signalsctl doctor

# Test one connection (or an ad-hoc DSN) with classified
# failure reasons: ok / dns / tcp / tls / auth / startup / role /
# password_resolve / config.
signalsctl connect test prod-db
signalsctl connect test --dsn "host=db.example.com port=5432 dbname=app user=monitor sslmode=require password_env=APP_DB_PW"
```

### Pause / resume a target during an incident

```bash
# Stop collecting from a target without taking the daemon down.
# State is in-memory; daemon restart resumes all targets.
signalsctl collect pause --target=prod-db --reason="investigating incident #4321"

# Bring it back.
signalsctl collect resume --target=prod-db
```

The daemon also auto-pauses (state `open`) a target that fails
3 consecutive collection cycles and auto-recovers after a
cooldown (default 5 minutes). See R097 in
[`features/arq-signals/specification.md`](features/arq-signals/specification.md)
for the full state-machine spec.

### Reload configuration without restart

```bash
# SIGHUP path
kill -HUP $(pidof signals)

# HTTP path
curl -X POST http://localhost:8081/reload \
  -H "Authorization: Bearer $SIGNALS_API_TOKEN"
```

v1 reload scope is the target list — add / remove / modify
connection params or `collectors.profile`. `poll_interval`,
retention, and circuit thresholds remain set-at-construction
(documented as future scope).

### Check status

```bash
signalsctl status
```

## Snapshot format

Elevarq Signals produces snapshots in the `signals-snapshot.v1` format:

```
snapshot.zip
├── metadata.json          # collector version, timestamp, PG version
├── query_catalog.json     # which queries were executed
├── query_runs.ndjson      # execution metadata (timing, row counts, errors)
├── query_results.ndjson   # the actual data (one JSON object per row)
└── snapshots.ndjson       # legacy combined format
```

Example `metadata.json`:

```json
{
  "schema_version": "signals-snapshot.v1",
  "collector_version": "0.1.0",
  "collector_commit": "abc1234",
  "collected_at": "2026-03-14T10:30:00Z",
  "instance_id": "a1b2c3d4e5f6"
}
```

Example `query_results.ndjson` (one line per query):

```json
{"run_id":"01JD...","payload":[{"name":"max_connections","setting":"100","unit":"","source":"configuration file"},{"name":"shared_buffers","setting":"16384","unit":"8kB","source":"configuration file"}]}
```

The format is versioned. Breaking changes will bump `schema_version`.

A complete example snapshot is available at
[`examples/snapshot-example/`](examples/snapshot-example/) — you can
inspect exactly what Elevarq Signals collects without running it.

## Collected signals

Elevarq Signals includes 99 read-only collectors. Grouped by domain:

- **Baseline & runtime** — server config, sessions, databases,
  tables, indexes, table / index I/O, query stats
  (`pg_stat_statements`)
- **Schema model** — columns, constraints, indexes, partitions,
  sequences, schemas, triggers, views, materialised views,
  functions, planner stats, extended statistics, vector columns
- **Definitions** — view, materialised-view, function, and
  trigger definitions (DDL bodies)
- **Storage placement** — tablespaces, per-relation storage,
  per-attribute storage
- **In-flight operations** — six `pg_stat_progress_*` collectors
  (vacuum, analyze, create_index, cluster, basebackup, copy)
- **Index hygiene** — derived findings: unused, invalid,
  redundant, duplicate
- **Bloat estimation** — statistical table-bloat and index-bloat
  estimates without `pgstattuple`
- **Wraparound risk** — XID age at database / relation level,
  freeze blockers, prepared-transaction age
- **Vacuum / checkpointer / bgwriter** — autovacuum health,
  checkpointer stats (PG 17+), bgwriter pressure
- **Replication** — `pg_stat_replication`, `pg_replication_slots`,
  `pg_stat_replication_slots` (logical slot health)
- **Operational pressure** — connection utilisation, blocking
  locks, long-running transactions, idle-in-transaction
  offenders, temp I/O, lock summary
- **Identity & configuration** — server identity, cluster
  identity (network fingerprint), extension inventory, role
  capabilities, login roles, per-role / per-database GUC
  overrides
- **Foreign data wrappers** — wrappers, servers, user mappings,
  foreign tables

Collectors requiring unavailable extensions or unsupported PostgreSQL
versions are silently skipped and surface with a `reason` in
`collector_status.json`. Replication collectors return empty
results on standalone instances.

See [docs/collectors.md](docs/collectors.md) for the full inventory
with query IDs, PostgreSQL sources, and cadences. Every query is
visible in [`internal/pgqueries/`](internal/pgqueries/).

## API

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/health` | No | Liveness probe, always 200 |
| `GET` | `/status` | Bearer | Collector status, targets, last collection |
| `POST` | `/collect/now` | Bearer | Trigger immediate collection (optional JSON body to narrow targets) |
| `POST` | `/collect/pause` | Bearer | Pause a target's collection circuit (R097) |
| `POST` | `/collect/resume` | Bearer | Resume a paused target |
| `POST` | `/reload` | Bearer | Re-read config file, swap target list (R100). Same as `SIGHUP` |
| `GET` | `/export` | Bearer | Download snapshot ZIP |

Set `SIGNALS_API_TOKEN` to configure the bearer token. If unset, a
random token is generated at startup and logged (fingerprint only;
the value is never logged).

### `POST /collect/now` examples

The body is optional. An empty / missing body keeps the historical
"collect every enabled target" behaviour. When present, the body
may carry an optional `targets` subset, an optional `request_id`
correlation identifier, an optional `reason` label, and an optional
`force` flag that bypasses R091's `min_snapshot_interval`.

```bash
# 1. No body — collect every enabled target.
curl -s -X POST http://127.0.0.1:8081/collect/now \
  -H "Authorization: Bearer ${SIGNALS_API_TOKEN}"

# 2. Narrow to a subset of configured targets.
curl -s -X POST http://127.0.0.1:8081/collect/now \
  -H "Authorization: Bearer ${SIGNALS_API_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"targets":["prod-main"]}'

# 3. Caller-supplied correlation id and reason.
curl -s -X POST http://127.0.0.1:8081/collect/now \
  -H "Authorization: Bearer ${SIGNALS_API_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
        "targets":    ["prod-main", "prod-reporting"],
        "request_id": "scheduled_run_2026_04_25",
        "reason":     "automated_cycle"
      }'

# 4. Force this cycle to bypass min_snapshot_interval (R091).
#    Does NOT bypass a paused circuit (R097) — resume the target first.
curl -s -X POST http://127.0.0.1:8081/collect/now \
  -H "Authorization: Bearer ${SIGNALS_API_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"targets":["prod-main"], "force": true}'
```

A successful response (HTTP 202):

```json
{
  "status": "collection triggered",
  "request_id": "scheduled_run_2026_04_25",
  "accepted_targets": ["prod-main", "prod-reporting"]
}
```

A rejection (HTTP 400) — invalid target name:

```json
{
  "error": "one or more targets cannot be collected",
  "accepted_targets": ["prod-main"],
  "rejected_targets": [
    {"name": "does-not-exist", "reason": "unknown_target"}
  ]
}
```

The cycle is **not triggered** when any target was rejected.

For the full request schema, validation rules, and audit-trace
behaviour, see [`docs/control-plane.md`](docs/control-plane.md).

## Control plane support

`POST /collect/now` accepts an optional JSON body that lets a caller
narrow the cycle to a configured + enabled subset of targets. The
configured target list in `signals.yaml` is the **authoritative
ceiling** — no caller can introduce a database name that wasn't
already configured.

Two optional correlation fields ride along with the request:

- `request_id` (regex `^[A-Za-z0-9_-]{1,32}$`) — caller-supplied
  correlation identifier. When absent, Elevarq Signals generates a ULID.
- `reason` (regex `^[A-Za-z0-9_-]{1,64}$`) — short tag-style label
  surfaced in audit events.

Every accepted request produces a deterministic audit trace keyed
by `request_id`:

```
collect_now_requested  →  collection_started  →  collection_completed   (per target)
```

Validation failures emit `collect_now_rejected`; requests that
queue but can't run (channel full, or cycle overlap) emit
`collect_now_dropped`. See "Audit guarantees" below.

Operators who want the commercial Elevarq control plane to drive this
endpoint additionally enable Mode B authentication — see the next
section. The endpoint itself works in both modes.

Reference: [`docs/control-plane.md`](docs/control-plane.md).

## Authentication modes

Elevarq Signals supports two modes, configured by `signals.mode` in
`signals.yaml` (default `standalone`).

### Standalone mode (default)

A single bearer token (`api.token`) authorises every request.
Matched-token audit events carry `actor=local_operator`. This is
the only mode every open-source deployment needs to know about.

### Managed mode (`mode: managed`)

Adds a second bearer token, the **Elevarq control-plane token**,
distinct from `api.token`. The matched token determines the audit
identity:

| Bearer matched | `actor` |
|---|---|
| `api.token` | `local_operator` |
| `control_plane_token` | `control_plane` |

The actor is sourced from *which token matched* — it is **never**
inferred from request shape. A caller holding only `api.token`
cannot acquire the `control_plane` identity by adding a
`request_id` or any other body field.

The control-plane token is supplied via file (preferred) or
environment-variable indirection:

```yaml
signals:
  mode: managed
  control_plane_token_file: /etc/signals/control-plane.token
  # or:
  # control_plane_token_env: SIGNALS_CONTROL_PLANE_TOKEN
```

The file is re-read on every authentication attempt so rotation is
a single file-write — no daemon restart required. Token length
floor is 32 characters; the two tokens must be distinct (constant-
time check at startup).

Mode B has **no licence-validation surface in Elevarq Signals.** The
collector remains open source; the commercial value lives in the
Elevarq control plane's analysis layer, not in obscured collector
behaviour. See `docs/authentication.md` for the full Mode B model,
rotation behaviour, and security posture.

Reference: [`docs/authentication.md`](docs/authentication.md).

## Audit guarantees

Elevarq Signals emits structured slog records keyed
`audit_event=<name>` for every operationally significant lifecycle
moment. The contract:

**No silent request loss.** Every accepted `/collect/now` request
reaches a terminal outcome for its `request_id` along exactly one
of three branches:

| Branch | Terminal records | When |
|---|---|---|
| **rejected** | one `collect_now_rejected` | validation failed; cycle never queued |
| **dropped** | one `collect_now_dropped` | queued but cycle never ran (channel full, or cycle overlap) |
| **ran** | one `collection_started` **per target** + one `collection_completed` **per target** | cycle ran |

The "ran" branch is per-target: a request that narrows to two
targets emits two started/completed pairs sharing the same
`request_id`; a request that omits `targets` emits one pair per
enabled target. There is no aggregate "cycle complete" record. If
a `request_id` appears on `collect_now_requested` but the audit
log shows no records on any of the three branches, that's a bug.

**Token values never logged.** A centralised denylist filter in
`internal/safety/audit.go` rejects audit attributes whose key
contains `password`, `secret`, `api_token`, `token`, `dsn`,
`connection_string`, `payload`, or `query_result`. A small
hand-curated allow-list overrides the substring match for keys
that carry only metadata about a configured value (booleans /
fingerprints), never the secret value itself — as of today the
allow-list has exactly one entry, the boolean
`control_plane_token_configured` on the `mode_configured`
startup event.

**Correlation by request_id.** When a caller supplies (or the
daemon generates) a `request_id`, that value is propagated through
to every per-target `collection_started` / `collection_completed`
audit record so the full sequence is greppable as one trail.

For the full event catalogue, attribute schemas, and the
secret-handling proof points, see
[`docs/audit-model.md`](docs/audit-model.md).

## Security and data handling

### Read-only enforcement (three layers)

1. **Static linting** — every SQL query is validated at startup. DDL
   (`CREATE`, `ALTER`, `DROP`), DML (`INSERT`, `UPDATE`, `DELETE`), and
   dangerous functions (`pg_terminate_backend`, `pg_sleep`) cause the
   process to abort immediately.
2. **Session-level** — all connections set
   `default_transaction_read_only=on`.
3. **Per-query** — each query runs inside `BEGIN ... READ ONLY`.

### Role safety validation (fail-closed)

Before collecting from any target, Elevarq Signals validates the connected
role's safety posture. Collection is **blocked** if the role has:

- Superuser privileges (`rolsuper=true`)
- Replication privileges (`rolreplication=true`)
- Bypass RLS privileges (`rolbypassrls=true`)

This is enforced by default with no configuration needed. Use a
dedicated monitoring role with `pg_monitor` for safe collection.
See [docs/runtime-safety-model.md](docs/runtime-safety-model.md) for
details.

### Credentials

- Passwords are read from file or environment variable at connection
  time
- Passwords are never cached in memory beyond a single connection
  attempt
- Passwords are never written to SQLite
- Passwords never appear in snapshots or exports
- Password rotation is supported (re-read on each new connection)

### API tokens

- Both bearer tokens (the local `api.token` and the optional
  Mode B `control_plane_token`) are compared in constant time
  via `crypto/subtle`.
- Token values **never appear** in audit logs, metrics, error
  messages, or HTTP responses. The auto-generated `api.token` logs
  only its SHA-256 fingerprint at startup.
- Audit-attribute filtering is centralised: a denylist on attribute
  key names (`password`, `secret`, `api_token`, `token`, `dsn`,
  `connection_string`, `payload`, `query_result`) drops any record
  whose key contains a denylisted substring before it leaves the
  process. A small hand-curated allow-list permits a single
  configuration-status boolean
  (`control_plane_token_configured`) on the `mode_configured`
  startup event — never a token value.
- The control-plane token (when configured) is re-read from file
  on every authentication attempt. Rotation is a single file-write;
  no daemon restart is required. See
  [`docs/authentication.md`](docs/authentication.md).

### Network

- Elevarq Signals makes **no outbound network connections** except to your
  PostgreSQL targets
- No telemetry, no analytics, no phone-home
- The HTTP API binds to loopback by default (`127.0.0.1:8081`)

### Container hardening

When deployed via Docker, Elevarq Signals runs as a non-root user
(UID 10001) on a minimal Alpine 3.21 base. The image contains
BusyBox (used by the `wget`-based healthcheck and `tini` init) and
no Bash, sh or other full shell beyond BusyBox's `ash` applet.
For deployments that require a shell-free runtime, build against
a distroless base — the binary is statically linked and CGO-free
so it runs without glibc.

## Configuration reference

Elevarq Signals reads configuration from (in order):
1. `--config` flag
2. `/etc/signals/signals.yaml`
3. `./signals.yaml`

Environment variables override file-based config. See
[`examples/signals.yaml`](examples/signals.yaml) for a complete
annotated example.

| Environment variable | Description | Default |
|---------------------|-------------|---------|
| `SIGNALS_ENV` | Environment: dev, lab, prod | dev |
| `SIGNALS_ALLOW_INSECURE_PG_TLS` | Allow weak TLS in non-prod | false |
| `SIGNALS_ALLOW_UNSAFE_ROLE` | Allow unsafe role attributes (lab/dev only) | false |
| `SIGNALS_TARGET_HOST` | PostgreSQL host | -- |
| `SIGNALS_TARGET_PORT` | PostgreSQL port | 5432 |
| `SIGNALS_TARGET_DBNAME` | Database name | postgres |
| `SIGNALS_TARGET_USER` | Username | -- |
| `SIGNALS_TARGET_NAME` | Target name | default |
| `SIGNALS_TARGET_PASSWORD_FILE` | Path to password file | -- |
| `SIGNALS_TARGET_PASSWORD_ENV` | Env var containing the password | -- |
| `SIGNALS_TARGET_PGPASS_FILE` | Path to pgpass file | -- |
| `SIGNALS_TARGET_SSLMODE` | TLS mode | -- |
| `SIGNALS_POLL_INTERVAL` | Collection interval | 5m |
| `SIGNALS_RETENTION_DAYS` | Days to retain data | 30 |
| `SIGNALS_LOG_LEVEL` | Log level: debug, info, warn, error | info |
| `SIGNALS_LOG_JSON` | JSON log format | false |
| `SIGNALS_MAX_CONCURRENT_TARGETS` | Max parallel targets | 4 |
| `SIGNALS_TARGET_TIMEOUT` | Per-target timeout | 60s |
| `SIGNALS_QUERY_TIMEOUT` | Per-query timeout | 10s |
| `SIGNALS_LISTEN_ADDR` | API listen address | 127.0.0.1:8081 |
| `SIGNALS_DB_PATH` | SQLite database path | /data/signals.db |
| `SIGNALS_WRITE_TIMEOUT` | API write timeout | 180s |
| `SIGNALS_API_TOKEN` | Bearer token for API auth | auto-generated |

## Architecture and scope

Elevarq Signals is the open-source collection layer of the Elevarq platform.
It is a complete, standalone tool — not a crippled free tier.

```
┌───────────────────┐
│  Elevarq Signals  │  Collects diagnostic signals from PostgreSQL.
│   (open source)   │  Produces portable snapshots. This repository.
└─────────┬─────────┘
          │ snapshot (ZIP / NDJSON)
          ▼
┌───────────────────┐
│  Elevarq Analyzer │  Analyzes signals. Scores health. Generates
│     (private)     │  findings and recommendations.
└─────────┬─────────┘
          │ findings
          ▼
┌───────────────────┐
│ Elevarq Workbench │  Presents results to engineers.
│     (private)     │  Interactive UI for DBA workflows.
└───────────────────┘
```

The snapshot format (`signals-snapshot.v1`) is the stable contract between
layers. Each layer is independently deployable and separately
maintained.

**Elevarq Signals is fully usable on its own.** You do not need Elevarq
Analyzer or Elevarq Workbench to collect, export, or inspect your PostgreSQL
diagnostics. Many teams use Elevarq Signals purely for data collection,
feeding the snapshots into their own scripts, dashboards, or analysis
workflows.

### What stays out of Elevarq Signals — by design

The boundary between Signals and the rest of the platform is
intentional, not accidental:

| Capability | Where it lives | Why not in Signals |
|-----------|---------------|-------------------|
| Database analysis | Elevarq Analyzer | Interpretation is a separate concern from evidence collection |
| Health scoring | Elevarq Analyzer | Scoring requires domain judgment that evolves independently |
| AI / LLM | Elevarq Analyzer | Language models are not needed for safe data collection |
| Recommendations | Elevarq Analyzer | Remediation advice requires analysis context |
| Cloud services | None | No component phones home or uploads data |
| Telemetry | None | No usage tracking exists anywhere in the platform |

This separation keeps the collector small, auditable, and safe to run
in restricted environments where third-party analysis tools may not be
permitted.

## Project status

Elevarq Signals v0.8.0 — the collection engine, safety model, and
snapshot format are stable and tested (800+ automated tests, 104
STDD requirements). Smoke-tested against PostgreSQL 14, 15, 16,
17, and 18. Released container images are published to GHCR and
Docker Hub with SBOM (SPDX) and SLSA provenance, plus a cosign-
signed SPDX attestation bound to the workflow OIDC identity
(verify with `cosign verify-attestation --type spdx …` per
`docs/release-verification.md`).

**Roadmap:**

- Kubernetes deployment examples
- Community-contributed collectors
- `bloat_exact_v1` / `index_bloat_exact_v1` — `pgstattuple`-gated
  precision variants of the existing statistical bloat collectors

## Development methodology

This project follows
[STDD — Specification & Test-Driven Development](https://github.com/fheikens/stdd).
Specifications and tests define correct behavior. Implementation is
written to satisfy those rules. The development policy is defined in
[CLAUDE.md](CLAUDE.md).

## Contributing

We welcome contributions. See [CONTRIBUTING.md](CONTRIBUTING.md) for
guidelines and [GOVERNANCE.md](GOVERNANCE.md) for project governance.

**In scope:** new collectors, bug fixes, performance, documentation.
**Out of scope:** analysis, scoring, AI (those belong in a downstream
analyzer).

## Project resources

- [Collector inventory](docs/collectors.md) — all 99 collectors with sources and cadences
- [Database connections](docs/database-connections.md) — per-cloud `auth_method` recipes (RDS IAM, Entra, Cloud SQL IAM, secret stores) + grants
- [Runtime safety model](docs/runtime-safety-model.md) — read-only enforcement details
- [Adoption guide](docs/adoption-guide.md) — production deployment guidance
- [FAQ](docs/faq.md) — common questions
- [Changelog](CHANGELOG.md) — release history
- [Security policy](SECURITY.md) — vulnerability reporting
- [Citation](CITATION.cff) — how to cite this project

## Related

- [Elevarq](https://elevarq.com) — PostgreSQL tools for engineering teams
- [Elevarq Analyzer](https://elevarq.com/products/arq) — commercial PostgreSQL intelligence
  platform; Elevarq Signals is its open-source collection layer
- [pgAgroal Container](https://github.com/Elevarq/pgAgroal) — production-ready
  container distribution of pgagroal, a high-performance PostgreSQL connection
  pooler

## License

BSD-3-Clause. See [LICENSE](LICENSE).

Free to use, modify, and distribute for any purpose, including
commercial use.
