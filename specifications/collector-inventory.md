# Canonical Collector Inventory — Behavioral Specification

Spec version: 1.0
Status: ACTIVE
Issue: [Elevarq/Signals#252](https://github.com/Elevarq/Signals/issues/252)

## Purpose

The wire contract for `collector_name` values is the query registry
(`internal/pgqueries`, one `Register(QueryDef)` per collector) — not
the `specifications/collectors/*.md` filenames. The two diverged:
family specs (`pg_stat_progress_family_v1.md` covers six registered
IDs, `timescaledb_family_v1.md` covers twelve) and a handful of
spec-less IDs mean a filename walk under-counts and mis-names the
registry. Downstream consumers that need the closed collector-name
enum (the Arq-Workbench bundled catalogue generator, the Analyzer's
WPS-R008 producer-side gate and its freshness checks) cannot import
`internal/pgqueries` cross-module, and had no mechanical view of the
registry at all — which is how the bundled catalogue silently went
stale at 64 of 99 names
([Elevarq/Analyzer#1378](https://github.com/Elevarq/Analyzer/issues/1378),
[Elevarq/Workbench#765](https://github.com/Elevarq/Workbench/issues/765)).

This spec adds a canonical, machine-readable **collector inventory**:
a committed JSON artifact generated from the registry, kept in sync
by a CI gate, consumable by out-of-module tooling as a plain file at
any git ref.

## Interfaces

### Inputs

- The process-global query registry populated by `init()`-time
  `pgqueries.Register` calls: each entry's `ID` and `Category`.

### Outputs

- `specifications/collectors/collector-inventory.json`, committed to
  the repository:

```json
{
  "collectors": [
    { "category": "server", "name": "bgwriter_stats_v1" },
    ...
  ],
  "contract_version": 1
}
```

- A regeneration command: `go run ./cmd/gen-collector-inventory`
  (writes the file in place from the repo root).

## Rules

- **R119** — The committed inventory lists exactly the registered
  collector IDs: one entry per `pgqueries.Register` call, `name` =
  `QueryDef.ID`, `category` = `QueryDef.Category`, no additions, no
  omissions.
- **R120** — The encoding is canonical and byte-stable: entries
  sorted ascending by `name`, JSON object keys sorted, two-space
  indentation, trailing newline, no volatile fields (no timestamps,
  no git refs — the enclosing commit already identifies the source).
  Regenerating from an unchanged registry is a byte-identical no-op.
- **R121** — CI enforces registry↔inventory sync: a Go test
  regenerates the inventory in memory and fails when the committed
  file differs, naming the missing/extra collector IDs. Registering,
  renaming, or removing a collector without regenerating the
  inventory fails CI.
- **R122** — `contract_version` identifies the schema of this file
  (currently `1`). Adding fields or changing the encoding is a
  deliberate spec amendment that bumps it.

## Invariants

- **INV-CINV-01** — Inventory `name` values and registry IDs are
  set-equal and equally sized at every commit that passes CI.
- **INV-CINV-02** — The artifact is generated offline: no database
  access, no network, no environment dependence.

## Failure conditions

| Trigger | Response |
|---|---|
| Registry changed, inventory not regenerated | R121 CI test fails, listing the drifted names |
| Committed inventory malformed / hand-edited into non-canonical bytes | R121 CI test fails on byte comparison |
| Generator run outside a repo checkout (output path missing) | Command exits non-zero with the attempted path |

## Non-functional requirements

- **Determinism** — byte-identical output for an unchanged registry
  (R120); safe under `SOURCE_DATE_EPOCH`-style reproducible builds
  because no timestamp exists to vary.
- **Compatibility** — consumers parse a stable shape gated by
  `contract_version` (R122); the file is fetchable as a raw blob at
  any ref without building Signals.
