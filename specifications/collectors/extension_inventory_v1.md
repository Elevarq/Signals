# extension_inventory_v1 — Collector Specification

## Purpose

Installed-extension inventory with version information. Used for
(a) operational-readiness reporting, (b) detector feature-gating
(presence of `pg_stat_statements`, `vector`, `pgstattuple`, etc.),
and (c) platform fingerprinting (some hyperscalers install
vendor-specific extensions).

## Catalog source

- `pg_available_extensions` — joined view exposing both installed
  and default versions.

## Output columns

| Column | Type | Description |
|---|---|---|
| name | text | Extension name |
| default_version | text | Version offered by the server's package |
| installed_version | text | Currently installed version (non-NULL) |
| comment | text | Extension description |

## Scope filter

- `installed_version IS NOT NULL` — only installed extensions.
- Available-but-not-installed extensions are out of scope.

## Invariants

- Deterministic ordering: `ORDER BY name`.
- Stable output column order.
- Read-only, passes linter.

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
