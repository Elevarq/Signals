# pg_triggers_v1 — Collector Specification

## Purpose

Trigger inventory for all user-schema triggers. Provides trigger
identity, tgtype bitmask (encoding timing and events), function
reference, and enabled status. Internal triggers (constraint-generated)
are excluded. Definition text is available in definition mode but
excluded by default.

## Catalog source

- pg_trigger (trigger metadata and tgtype bitmask)
- pg_class (table identity)
- pg_namespace (schema names for table and function)
- pg_proc (trigger function identity)

## Definition modes

| Mode | Output | Default |
|---|---|---|
| inventory | identity + tgtype + function + enabled | yes |
| definition | adds: triggerdef from pg_get_triggerdef() | no |
| hash_only | adds: definition_hash (SHA-256, computed by Arq Signals runtime) | no |

## Output columns (inventory mode)

| Column | Type | Description |
|---|---|---|
| schemaname | text | Schema of the table the trigger is on |
| relname | text | Table name |
| tgname | text | Trigger name |
| tgtype | int | Bitmask encoding timing and events (see below) |
| tg_funcschema | text | Schema of the trigger function |
| tg_funcname | text | Trigger function name |
| tg_enabled | char | O=origin, D=disabled, R=replica, A=always |

## Output columns (definition mode, adds)

| Column | Type | Description |
|---|---|---|
| triggerdef | text | Full trigger definition from pg_get_triggerdef() |

## tgtype bitmask

The tgtype column is an integer bitmask emitted as-is by the
collector. The analyzer decodes timing and events:

- bit 0 (1): ROW level (vs STATEMENT)
- bit 1 (2): BEFORE
- bit 2 (4): INSERT
- bit 3 (8): DELETE
- bit 4 (16): UPDATE
- bit 5 (32): TRUNCATE
- bit 6 (64): INSTEAD OF

Timing: BEFORE if bit 1 set, INSTEAD OF if bit 6 set, else AFTER.

Design note: the bitmask is emitted as an integer rather than decoded
to string literals in SQL because the linter blocks event keyword
strings (INSERT, UPDATE, DELETE, TRUNCATE) as dangerous words.
Decoding happens in the analyzer.

## Filter

- Excludes internal triggers (tgisinternal = true)
- Standard schema filter on table schema

## Invariants

- Deterministic ordering: ORDER BY schemaname, relname, tgname
- Empty result serializes as []
- Stable output column order (explicit SELECT, no SELECT *)
- Read-only query, passes linter

## Configuration

- Category: schema
- Cadence: 24h (CadenceDaily)
- Retention: RetentionMedium
- Min PG version: 10
- Enabled by default: yes (inventory mode)

## Sensitivity

Low (inventory mode — trigger names and function references).
Moderate (definition mode — trigger logic structure).

## Analyzer use cases

- Disabled trigger audit
- Trigger-induced overhead analysis
- Schema documentation
