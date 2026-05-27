# pg_sequences_v1 — Collector Specification

## Purpose

Sequence inventory and health for all user-schema sequences. Provides
identity, configuration, and current value for sequence exhaustion
detection and identity column health monitoring.

## Catalog source

- pg_sequences (system view, PG 10+)

## Output columns

| Column | Type | Description |
|---|---|---|
| schemaname | text | Schema containing the sequence |
| sequencename | text | Sequence name |
| data_type | text | Sequence data type (smallint, integer, bigint) |
| start_value | bigint | Initial value |
| min_value | bigint | Minimum allowed value |
| max_value | bigint | Maximum allowed value |
| increment_by | bigint | Step size |
| cycle | bool | true if sequence wraps on exhaustion |
| last_value | bigint | Last value returned (null if never used) |

## Schema filter

Excludes pg_catalog, information_schema, pg_toast, pg_temp_%,
pg_toast_temp_%.

## Invariants

- Deterministic ordering: ORDER BY schemaname, sequencename
- Empty result serializes as []
- Stable output column order (explicit SELECT, no SELECT *)
- Read-only query, passes linter

## Configuration

- Category: schema
- Cadence: 24h (CadenceDaily)
- Retention: RetentionMedium
- Min PG version: 10
- Enabled by default: yes

## Sensitivity

Low. Sequence configuration is structural metadata. last_value
reveals the current counter but not what it is used for.

## Analyzer use cases

- Sequence exhaustion detection (approaching max_value)
- Identity column health monitoring
- Schema documentation
