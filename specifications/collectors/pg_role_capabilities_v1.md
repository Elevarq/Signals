# pg_role_capabilities_v1 — Collector Specification

## Purpose

Record what the monitoring role can actually observe on the target.
This is the "ground truth" companion to the role-safety validation
already performed at collection start: whereas `ValidateRoleSafety`
*gates* collection, this collector *documents* capability for the
analyzer's coverage model so detectors can reason about why a signal
might be incomplete.

## Catalog source

Composite — `pg_has_role`, `current_user`, `session_user`,
`pg_authid` / `pg_roles`, `pg_settings`.

## Output columns

One row.

| Column | Type | Description |
|---|---|---|
| session_user | text | Session role name |
| current_user | text | Effective role name |
| is_superuser | boolean | `rolsuper` |
| is_pg_monitor | boolean | Member of `pg_monitor` |
| is_pg_read_all_stats | boolean | Member of `pg_read_all_stats` (directly or via pg_monitor) |
| is_pg_read_all_settings | boolean | Member of `pg_read_all_settings` |
| is_pg_read_server_files | boolean | Member of `pg_read_server_files` |
| is_pg_signal_backend | boolean | Member of `pg_signal_backend` |
| can_read_all_stats | boolean | Effective — superuser OR pg_read_all_stats |
| can_read_all_settings | boolean | Effective — superuser OR pg_read_all_settings |
| default_transaction_read_only | text | Current session GUC |
| statement_timeout | text | Current session GUC |
| role_attrs | jsonb | `{rolcreaterole, rolcreatedb, rolcanlogin, rolreplication, rolbypassrls, rolconnlimit}` |

## Scope filter

Single-row output.

## Invariants

- Exactly one row per target, per sample.
- Booleans are authoritative (computed from `pg_has_role` membership
  checks, not from role name heuristics).
- `role_attrs` is a JSON object with every listed key present; never
  NULL values for known attrs.

## Failure Conditions

- FC-01: `pg_has_role` returns an error for a built-in role that does
  not exist on older PG versions (e.g. `pg_read_all_settings` on PG
  < 10.5) → emit `false` and record the fact in a JSON side-channel
  field `probe_errors` so the analyzer can distinguish "role doesn't
  exist on this version" from "monitoring user is not a member."

## Configuration

- Category: context
- Cadence: 24h (CadenceDaily)
- Retention: RetentionMedium
- Min PG version: 10
- Requires extension: none
- Semantics: snapshot
- Enabled by default: yes

## Sensitivity

Low. All information is already derivable by the connected role from
catalog queries it is allowed to run.

## Analyzer requirements unblocked

- `EvidenceCompleteness` — detectors can distinguish
  `permission_denied` (runtime, recorded as `failed` in
  `collector_status.json`) from `extension_missing` (gating-time,
  recorded as `skipped`) when producing CoverageNotes.
- Support-bundle generation — explains "why did collector X produce
  no rows" by reference to the recorded capability matrix at the
  time of collection.
