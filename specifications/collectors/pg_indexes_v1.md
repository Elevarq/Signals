# pg_indexes_v1 — Collector Specification

## Purpose

Index definitions for all user-schema indexes. Provides the indexdef
column containing the full CREATE INDEX statement, needed to identify
leading columns for composite index analysis.

## Catalog source

- pg_indexes (system view joining pg_class, pg_index, pg_namespace)

## Output columns

| Column | Type | Description |
|---|---|---|
| schemaname | text | Schema containing the index |
| tablename | text | Table the index is on |
| indexname | text | Index name |
| indexdef | text | Full CREATE INDEX statement |
| tablespace | text | Tablespace name (empty if default) |

## Schema filter

Excludes pg_catalog, information_schema, pg_toast, pg_temp_%,
pg_toast_temp_%.

## Invariants

- Deterministic ordering: ORDER BY schemaname, tablename, indexname
- Empty result serializes as []
- Stable output column order (explicit SELECT, no SELECT *)
- Read-only query, passes linter
- indexdef always starts with "CREATE"

## Configuration

- Category: schema
- Cadence: 24h (CadenceDaily)
- Retention: RetentionMedium
- Min PG version: 10
- Enabled by default: yes

## Sensitivity

Low. Index definitions reveal column choices and partial predicates,
but this is structural metadata visible to any connected role.

## Analyzer requirements unblocked

- FI-R014: FK index suppression — parseLeadingColumn reads indexdef
- The detector reads ev.Raw["pg_indexes_v1"] or ev.Indexes
