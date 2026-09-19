# extension_inventory_v1 — Collector Specification

## Purpose

Extension inventory with version information, covering both
**installed** extensions and those that are **available but not
installed** on this server. Used for (a) operational-readiness
reporting, (b) detector feature-gating (presence of
`pg_stat_statements`, `vector`, `pgstattuple`, etc.), and
(c) platform fingerprinting (some hyperscalers install
vendor-specific extensions, and the *set of available* extensions
is itself a platform signal).

Emitting available-but-not-installed extensions lets a downstream
consumer distinguish "available to install" from "unavailable on
this managed platform" — data already present in the catalog that
was previously discarded at collection. Evidence only: the
collector performs no diagnosis (that is the Analyzer's job).

## Catalog source

- `pg_available_extensions` — joined view exposing both installed
  and default versions.

## Output columns

| Column | Type | Description |
|---|---|---|
| name | text | Extension name |
| default_version | text | Version offered by the server's package |
| installed_version | text | Currently installed version, or **NULL** when the extension is available but not installed |
| comment | text | Extension description |

## Scope filter

- No installed-only filter. Every row of `pg_available_extensions`
  visible to the collecting role is emitted — installed extensions
  (`installed_version` non-NULL) **and** available-but-not-installed
  ones (`installed_version` NULL).
- A NULL `installed_version` is the canonical "available to install
  but not created" signal; consumers MUST NOT treat it as a
  collector failure.
- Platform visibility still bounds the set: on managed platforms
  `pg_available_extensions` only exposes what the platform permits
  (see FC-01), so absence of a row means "not available to this
  role on this platform", not "not installed".

## Invariants

- Deterministic ordering: `ORDER BY name`.
- Stable output column order.
- Read-only, passes linter.

## Acceptance cases

- **AC-01 (normal — installed extension).** An extension created on
  the server (e.g. `plpgsql`, always present) appears with a
  non-NULL `installed_version`.
- **AC-02 (available-but-not-installed).** An extension that is
  available on the server but not created (e.g. a contrib module
  present in `pg_available_extensions` but never `CREATE EXTENSION`d)
  appears in the evidence with `installed_version` NULL. This is the
  behavior added for #415 — such rows were previously filtered out.
- **AC-03 (definition — no installed-only filter).** The registered
  query does not restrict to `installed_version IS NOT NULL`; it
  emits the full role-visible `pg_available_extensions` set.
- **AC-04 (platform visibility).** Only extensions visible to the
  collecting role on the platform are emitted; a row's absence means
  "not available to this role" (FC-01), not "installed vs not".

## Failure Conditions

- FC-01: On managed platforms where `pg_available_extensions` has
  restricted visibility, rows may be omitted; the analyzer should
  not treat "extension missing from this output" as authoritative
  for detector gating — cross-reference with `collector_status.json`
  entries for collectors that declared `RequiresExtension`
  (per `specifications/extension-absent-emission.md`).
- FC-02: Permission denied → standard collector error path.

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

## Analyzer requirements unblocked

- Operational-readiness reporting.
- `server_identity_v1` cross-reference for platform fingerprint
  signals (vendor extensions).
- Detector feature-gating — `EvidenceCompleteness` can preemptively
  explain why an extension-gated collector was skipped (the
  primary signal is `collector_status.json`; this inventory
  provides the broader picture of what's installed).

## Known gap

The current SQL does not emit `update_available`,
`superuser_only_to_install`, `relocatable`, or `requires`. A
follow-up change can extend the SELECT list if detectors need
them — no blocker today.
