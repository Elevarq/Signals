# pg_version_v1 — Collector Specification

## Purpose

Minimal scalar capture of PostgreSQL's `version()` output. Used for
coarse version-based dispatch when the richer `server_identity_v1`
output is not yet available.

## Catalog source

- `version()`

## Query

```
SELECT version()
```

## Output

Scalar `text` — the full version banner.

## Invariants

- Exactly one row per sample.
- Read-only, passes linter.

## Failure Conditions

- None under normal operation. `version()` is built in and requires
  no privilege.

## Configuration

- Category: server
- Cadence: 6h (Cadence6h)
- Retention: RetentionLong
- Min PG version: 10
- Requires extension: none
- Semantics: snapshot
- Enabled by default: yes

## Sensitivity

Low.

## Relationship to other collectors

`server_identity_v1` is the richer equivalent (version + uptime +
database context + size). `pg_version_v1` is retained for its
minimal cost and for bootstrap use before `server_identity_v1`
has executed.

## Analyzer requirements unblocked

- Version-dependent detector logic when only the banner is needed.
- Ingestion-layer major-version parsing for evidence normalization.
