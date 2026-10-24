# SingleStore Support Matrix and Limits (POC)

## Status

ACTIVE

## Purpose

Document what Signals supports when collecting from SingleStore, the evidence it
captures, the required role, and the known limits (Elevarq/Signals #557). Based
on validation against a live SingleStore S-00 workspace (US East 1, SingleStore
engine version reported via `@@memsql_version`).

## Connectivity

- **Protocol:** MySQL wire (`go-sql-driver/mysql`), TLS. SingleStore is
  positively identified via `@@memsql_version` / `@@version_comment`; it is
  never reported as MySQL (see `singlestore-engine-identity.md`).
- **Declaration, not inference:** a target opts in with `engine: singlestore`
  in config; identity is validated, never guessed.

## Evidence captured (10 collectors)

| Category | Collector | Source |
|----------|-----------|--------|
| server | `singlestore_identity_v1` | `@@memsql_version`, `@@version_comment`, `VERSION()` |
| topology | `singlestore_aggregators_v1` | `information_schema.AGGREGATORS` |
| topology | `singlestore_leaves_v1` | `information_schema.LEAVES` |
| topology | `singlestore_cluster_status_v1` | `information_schema.MV_CLUSTER_STATUS` (needs `CLUSTER`) |
| schema | `singlestore_table_statistics_v1` | `information_schema.TABLE_STATISTICS` |
| workload | `singlestore_plancache_v1` | `information_schema.MV_PLANCACHE` (query text redactable) |
| config | `singlestore_global_variables_v1` | `information_schema.MV_GLOBAL_VARIABLES` |
| resources | `singlestore_activities_v1` | `information_schema.MV_ACTIVITIES` |
| activity | `singlestore_processlist_v1` | `information_schema.MV_PROCESSLIST` (INFO redactable) |
| resources | `singlestore_distributed_partitions_v1` | `information_schema.DISTRIBUTED_PARTITIONS` |

## Required role (least privilege)

Baseline `GRANT SELECT, PROCESS ON *.*` (read-only) covers 9/10 collectors;
`cluster_status` additionally needs `GRANT CLUSTER`. Full detail in
`singlestore-grants.md`. No admin/superuser; no write privilege.

## Known limits

- **`cluster_status` requires `CLUSTER`.** Without it that one collector
  degrades to a captured skip; topology is still covered by aggregators +
  leaves at the baseline grant.
- **`TABLE_STATISTICS` is empty on a database with no user tables** — expected;
  it populates once tables exist.
- **Managed (Helios) vs self-managed:** validated on managed Helios. Some
  node-level views may differ or be restricted on managed deployments; the
  self-managed matrix is confirmed as instances become available.
- **Multi-node production qualification (#556) is pending** — validation so far
  is a single S-00 workspace; a multi-node cluster run is required before
  claiming production multi-node support.
- **POC scope:** this matrix reflects the POC collector slice, not the full
  intended coverage of every #526 sub-issue.

## Traceability

- Elevarq/Signals #557 (support matrix), building on #548–#555 and
  `singlestore-grants.md`.
