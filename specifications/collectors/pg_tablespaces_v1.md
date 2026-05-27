# pg_tablespaces_v1 — Collector Specification

## Purpose

Tablespace inventory with per-tablespace GUC overrides
(`seq_page_cost`, `random_page_cost`, `effective_io_concurrency`,
`maintenance_io_concurrency`). On self-hosted PostgreSQL this
provides the primary surface for placement-aware cost advice; on
hyperscalers (RDS, Aurora, Cloud SQL, AlloyDB, Azure) only the
default tablespace is expected, and the output confirms that
absence so the analyzer can rule out placement-based recommendations
up front.

## Catalog source

- `pg_tablespace`
- `pg_tablespace_size()`
- `pg_options_to_table(spcoptions)` to expand `spcoptions`

## Output columns

One row per tablespace.

| Column | Type | Description |
|---|---|---|
| spcname | text | Tablespace name |
| spcowner_oid | oid | Owner role OID |
| spcoptions_raw | text[] | `pg_tablespace.spcoptions` verbatim |
| seq_page_cost | real | From `spcoptions`, NULL if unset |
| random_page_cost | real | From `spcoptions`, NULL if unset |
| effective_io_concurrency | int | From `spcoptions`, NULL if unset |
| maintenance_io_concurrency | int | From `spcoptions`, NULL if unset |
| size_bytes | bigint | `pg_tablespace_size(oid)` — NULL if function errors |

## Scope filter

All tablespaces.

## Invariants

- Deterministic ordering: `ORDER BY spcname`.
- Stable output column order.
- Read-only, passes linter.
- Even on hyperscalers, `pg_default` and `pg_global` are emitted —
  the analyzer uses their presence-only state to confirm the
  absence of custom tablespaces.

## Failure Conditions

- FC-01: `pg_tablespace_size()` fails for a tablespace the
  monitoring role cannot stat → `size_bytes` NULL; row still
  emitted.
- FC-02: Permission denied on `pg_tablespace` → standard collector
  error path.

## Configuration

- Category: configuration
- Cadence: 24h (CadenceDaily)
- Retention: RetentionMedium
- Min PG version: 10
- Requires extension: none
- Semantics: snapshot
- Enabled by default: yes

## Sensitivity

Low.

## Analyzer requirements unblocked

- `io-cost-calibration` — tablespace-level cost priors where present;
  on hyperscalers, confirms the single-calibration model is the
  right one.
- `object-parameter-drift` — detects per-tablespace costs that
  contradict cluster-level intent.
