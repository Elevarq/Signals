# Arq Signals v0.2.0 — Expanded Diagnostics and Server Survival Pack

## What is Arq Signals

Arq Signals is an open-source, read-only PostgreSQL diagnostic
collector. It connects to your databases, runs approved SQL queries,
and produces portable snapshots of diagnostic data. No AI, no cloud,
no write operations.

## What changed since v0.1.0

### New collectors (17 added, 29 total)

**Diagnostic Pack 1** (9 collectors):
- `server_identity_v1` — version, uptime, database context
- `extension_inventory_v1` — installed extensions with versions
- `bgwriter_stats_v1` — checkpoint/bgwriter health (SELECT * for PG 17+)
- `long_running_txns_v1` — transactions older than 5 minutes
- `blocking_locks_v1` — lock-blocking chains with wait durations
- `login_roles_v1` — login roles with privilege flags (no password hashes)
- `connection_utilization_v1` — connection counts vs max_connections
- `planner_stats_staleness_v1` — estimate drift and stale statistics
- `pgss_reset_check_v1` — pg_stat_statements reset timestamp (PG 14+)

**Server Survival Pack** (8 collectors):
- `replication_slots_risk_v1` — stale slots retaining WAL (disk exhaustion risk)
- `replication_status_v1` — replica lag and sync state
- `checkpointer_stats_v1` — PG 17+ checkpoint statistics
- `vacuum_health_v1` — dead tuple pressure, overdue vacuum, XID freeze age
- `idle_in_txn_offenders_v1` — backends holding open transactions
- `database_sizes_v1` — all database sizes for growth monitoring
- `largest_relations_v1` — top 30 relations by disk size
- `temp_io_pressure_v1` — per-database temp file usage

### Cross-version compatibility

- `pg_stat_statements` uses dynamic column capture (`SELECT *`) to
  adapt across PostgreSQL 14–18 where column names changed
- `pg_stat_bgwriter` uses `SELECT *` for PG 17+ compatibility
- `pg_stat_checkpointer` collected on PG 17+, gracefully skipped on
  earlier versions
- Savepoint-based query isolation prevents one failing query from
  aborting the entire collection cycle

### Bug fixes

- NULL payload on zero-row query results
- Transaction commit error handling (blocks downstream persistence)
- `round()` type compatibility for planner statistics

### STDD expansion

- 56 numbered requirements (was 26)
- 135 automated tests (was 94)
- Specification covers diagnostics, server survival, version-aware
  behavior, and graceful absence handling

## Validated PostgreSQL versions

Smoke-tested against:
- PostgreSQL 14.20 (port 54314)
- PostgreSQL 15.15 (port 54315)
- PostgreSQL 16.11 (port 54316)
- PostgreSQL 17.7 (port 54317)
- PostgreSQL 18.1 (port 54318)

All 29 collectors execute without error on PG 18. Replication and
checkpointer collectors behave gracefully on versions where those
features are absent or structured differently.

## Recommended role model

Create a dedicated monitoring role — do not use the `postgres`
superuser:

```sql
CREATE ROLE arq_signals LOGIN;
GRANT pg_monitor TO arq_signals;
```

The safety model blocks superuser, replication, and bypassrls roles
by default. An explicit override (`ARQ_SIGNALS_ALLOW_UNSAFE_ROLE=true`)
is available for lab/dev use and is recorded in export metadata.

## Trust model

- All queries execute inside `READ ONLY` transactions
- Three-layer read-only enforcement (linter + session + transaction)
- Role safety validation blocks unsafe roles before any query runs
- Session timeouts via `SET LOCAL` inside the collection transaction
- Credentials never stored, exported, or logged
- Unsafe overrides explicitly recorded in snapshot metadata
- BSD-3-Clause license — fully open source
