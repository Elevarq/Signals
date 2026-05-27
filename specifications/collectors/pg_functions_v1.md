# pg_functions_v1 — Collector Specification

## Purpose

Function and procedure inventory for all user-schema routines.
Provides identity, signature, return type, language, volatility, and
security properties. Function bodies are excluded by default (high
sensitivity) and available only in definition mode.

## Catalog source

- pg_proc (function/procedure metadata)
- pg_namespace (schema name)
- pg_language (language name)
- pg_get_function_identity_arguments() (argument signature)
- pg_get_function_result() (return type)

## Definition modes

| Mode | Output | Default |
|---|---|---|
| inventory | identity + signature + properties | yes |
| definition | adds: body (prosrc text) | no |
| hash_only | adds: body_hash (SHA-256, computed by Arq Signals runtime) | no |

## Output columns (inventory mode)

| Column | Type | Description |
|---|---|---|
| schemaname | text | Schema containing the function |
| proname | text | Function/procedure name |
| identity_args | text | Argument signature from pg_get_function_identity_arguments() |
| return_type | text | Return type from pg_get_function_result() |
| language | text | Language name (plpgsql, sql, c, etc.) |
| volatility | char | i=immutable, s=stable, v=volatile |
| security_definer | bool | true if SECURITY DEFINER |
| is_strict | bool | true if RETURNS NULL ON NULL INPUT |
| prokind | char | f=function, p=procedure, a=aggregate, w=window |

## Output columns (definition mode, adds)

| Column | Type | Description |
|---|---|---|
| body | text | Function body from pg_proc.prosrc |

## Minimum PostgreSQL version

Requires PG 11+. The `prokind` column was introduced in PG 11,
replacing the PG 10 booleans `proisagg` and `proiswindow`. On PG 10
this collector is skipped (graceful degradation via MinPGVersion).

## Schema filter

Excludes pg_catalog, information_schema, pg_toast, pg_temp_%,
pg_toast_temp_%.

## Invariants

- Deterministic ordering: ORDER BY schemaname, proname, identity_args
- Empty result serializes as []
- Stable output column order (explicit SELECT, no SELECT *)
- Read-only query, passes linter

## Configuration

- Category: schema
- Cadence: 24h (CadenceDaily)
- Retention: RetentionMedium
- Min PG version: 11
- Enabled by default: yes (inventory mode)

## Sensitivity

Low (inventory mode — function names, signatures, and properties).
High (definition mode — function bodies may contain credentials,
business logic, or PII handling). Definition mode requires explicit
configuration.

## Analyzer use cases

- Security definer audit
- Language distribution analysis
- Trigger function inventory (cross-reference with pg_triggers_v1)
- Schema documentation
