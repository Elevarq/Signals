# SingleStore Least-Privilege Grants (POC)

## Status

ACTIVE

## Purpose

Define the minimal SingleStore privileges the Signals collectors require, so
collection runs as a locked-down read-only role — never admin (Elevarq/Signals
#554, the grants half; query-text handling is in `singlestore-collectors.md`).
The grant boundary was determined **empirically against a live SingleStore
workspace**, not guessed.

## Empirical findings (live S-00 workspace)

Running the catalog as a restricted `signals_ro` user under an escalating grant
ladder (baseline-vs-elevated re-confirmed live for the #618 collectors against
SingleStore 9.1.1):

| Grant | Collectors that succeed |
|-------|-------------------------|
| `GRANT SELECT, PROCESS ON *.*` | the baseline set (13): identity, aggregators, leaves, table_statistics, global_variables, distributed_partitions, plancache, activities, processlist, **pipelines, sysinfo_memory, sysinfo_disk, nodes**. `PROCESS` gates the live-workload / sysinfo `MV_*` views. |
| `+ GRANT CLUSTER ON *.*` | the above **+ cluster_status + mv_events** (15) — `MV_CLUSTER_STATUS` and `MV_EVENTS` both need `CLUSTER` (`MV_EVENTS` returns `ERROR 1227 … need … CLUSTER` under the baseline role). |

## Decision

- **Baseline monitoring role = `GRANT SELECT, PROCESS ON *.*`** — read-only,
  covers 13/15 collectors including all workload, ingest, and per-node resource
  evidence. `PROCESS` is a read-only monitoring privilege (no data mutation),
  appropriate for a diagnostic collector.
- **`CLUSTER` is elevated and optional.** Exactly two collectors need it —
  `singlestore_cluster_status_v1` and `singlestore_mv_events_v1` — each marked
  `ElevatedPrivilege: "CLUSTER"`. Topology is already covered at the baseline by
  `aggregators` + `leaves` + `nodes`, so the elevated pair is supplementary —
  not required for the POC; it degrades to a per-collector skip under the
  baseline role.

## Cross-user / sensitive exposure (what each grant makes visible)

Source-permission-first: the database grant is the primary boundary; Signals-side
redaction is defense in depth. `PROCESS` broadens the live-workload `MV_*` views
from the current user's own rows to **every** session/query on the cluster; the
collectors that then carry cross-user SQL text are redacted by default:

| Collector | Sensitive field | What it exposes | Default handling |
|-----------|-----------------|-----------------|------------------|
| `singlestore_plancache_v1` | `QUERY_TEXT` | the plan-cache SQL text across all accounts | **HighSensitivity → redacted** |
| `singlestore_processlist_v1` | `INFO` | the live SQL text of every session | **HighSensitivity → redacted** |

Without `PROCESS`, `MV_PLANCACHE` / `MV_PROCESSLIST` return the current user's own
rows only (a current-user-only state, not a denial). The `CLUSTER`-elevated
collectors (`cluster_status`, `mv_events`) degrade to a per-collector skip under
the baseline role (SSG-R003). Non-SQL evidence (topology, schema sizing, resource
counters, node capacity, pipeline names) is native evidence; pipeline
`CONFIG_JSON` (connection credentials) is also HighSensitivity-redacted (#618).

## Rules

| ID | Rule |
|----|------|
| **SSG-R001** | The documented baseline role is `GRANT SELECT, PROCESS ON *.*` to a dedicated read-only user; no broader grant is required for 13/15 collectors. |
| **SSG-R002** | A collector needing a privilege beyond the baseline MUST declare it in `Collector.ElevatedPrivilege` (currently `cluster_status` and `mv_events` → `CLUSTER`). |
| **SSG-R003** | A permission-denied error from an `ElevatedPrivilege` collector run under the baseline role is an EXPECTED degrade — captured as a per-collector skip (`Result.Err`), never a fatal failure (collection stays partial, not failed). |
| **SSG-R004** | Every collector that can surface cross-user SQL (`QUERY_TEXT`, `INFO`) or connection credentials (`CONFIG_JSON`) is `HighSensitivity` and redacted by default (defense in depth); the DB grant (`PROCESS` for cross-session visibility) is the primary boundary. Without `PROCESS`, the live-workload views are current-user-only, not denied. |

## Invariants

| ID | Invariant |
|----|-----------|
| **INV-SSG-01** | No collector requires more than `SELECT`+`PROCESS` except those with a non-empty `ElevatedPrivilege`. |
| **INV-SSG-02** | Collection never requires admin/superuser; the role is read-only (no write privilege is ever granted or needed). |

## Recommended role (operator-facing)

```sql
CREATE USER 'signals_ro'@'%' IDENTIFIED BY '<password>';
GRANT SELECT, PROCESS ON *.* TO 'signals_ro'@'%';
-- Optional, only for cluster_status / replication-state collection:
-- GRANT CLUSTER ON *.* TO 'signals_ro'@'%';
```

## Non-functional requirements

- Safety: read-only posture preserved (INV-SSG-02); `PROCESS`/`CLUSTER` are
  read/monitoring privileges, not write.
- Correctness: the grant ladder is evidence-based (live workspace), not assumed.

## Traceability

- Parent: Elevarq/Signals #554 (least-privilege + query-text). Pairs with
  `singlestore-collectors.md` (SSC-R004 query-text redaction).
- Acceptance: `singlestore-grants.acceptance.md`.
