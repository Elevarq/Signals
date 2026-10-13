# Snapshot / output-format compatibility & deprecation policy

Tracking: [#503](https://github.com/Elevarq/Signals/issues/503) (part of the
upgrade-lifecycle umbrella [#500](https://github.com/Elevarq/Signals/issues/500)).
Status: **ACTIVE**.
Related: [`support-matrix.md`](support-matrix.md),
[`../metrics-consumer-guide.md`](../metrics-consumer-guide.md),
[`../upgrading.md`](../upgrading.md).

This is the **producer-side** compatibility and deprecation policy for the
Elevarq Signals **export / snapshot format** — the ZIP a Signals collector
emits (via `signalsctl export` and the control-plane `GET /export`) for a
downstream consumer to ingest. It documents how the **already-versioned**
format evolves, how long a version is emitted and supported, and what a
consumer may rely on. It does **not** introduce runtime producer/consumer
negotiation and does **not** change the existing version constants — the format
is already versioned, self-describing, and integrity-checked; what was missing
is this written stability contract.

## The versioned surfaces

A Signals export is a ZIP whose top-level entries are self-describing. Three
independent version identifiers are embedded, each a constant baked into the
collector binary:

| Surface | Identifier (JSON field) | Current value | Source constant |
|---|---|---|---|
| Snapshot format (the export as a whole; `metadata.json`) | `schema_version` | `signals-snapshot.v1` | `snapshot.SchemaVersion` |
| Collector execution status (`collector_status.json`) | `collector_status_schema_version` | `1` | `export.CollectorStatusSchemaVersion` |
| Integrity manifest (`manifest.json`) | `manifest_version` | `1` | `export.ManifestVersion` |

`manifest.json` additionally lists **every other file in the ZIP with its
SHA-256** (`"algorithm": "sha256"`), so an export's integrity is verifiable
offline, independent of transport.

The three identifiers version **independently**: the collector-status schema or
the manifest schema can bump without bumping the overall snapshot format, so a
consumer can pin tooling to a specific status or manifest shape without pinning
the whole format. The rules below apply to each surface on its own.

## Version semantics — how a version evolves

Each surface's version is a single integer major (the `.vN` suffix for the
snapshot format; the bare integer for the other two). The major identifies a
**compatibility generation**, not a release.

- **Additive / backward-compatible changes do NOT bump the version.** Adding a
  new optional field to `metadata.json`, a new file to the ZIP (e.g. a new
  collector's output, a new `per-collector/` view), a new collector, or a new
  optional metadata key is backward-compatible and ships **within** the current
  major. A consumer written for the current major keeps working unchanged.
- **Only a breaking change bumps the major.** Removing or renaming an existing
  field or file, changing the meaning or type of an existing field, or changing
  the ZIP layout in a way that an existing consumer cannot read is breaking and
  **only** such a change advances the major (`signals-snapshot.v1` →
  `signals-snapshot.v2`, `collector_status_schema_version` `1` → `2`, etc.).
- **Forward-compatibility is required of consumers, not of producers.** Because
  additive changes are expected within a major, a consumer **MUST ignore
  unknown fields and unknown files** rather than fail on them. This is the
  contract that lets the producer add within a major without a version bump.

## Emission & support window

This window is a customer-facing promise. It was decided for this policy
(2026-10-12) — there was no prior policy settling it.

- **Signals emits exactly one major of each surface at a time** — the current
  one. There is no runtime negotiation and no option to emit an older major on
  request.
- **When a new major supersedes the previous one, the previous major continues
  to be emitted and supported for two minor release cycles.** Concretely: if a
  new major is introduced in Signals minor `vX.Y`, the superseded major's
  guarantees (it remains readable, documented, and regression-tested) hold
  through at least `vX.(Y+2)`, and emission of the superseded major may be
  removed no earlier than the second minor release after supersession. This
  gives downstream consumers approximately two minor cycles of overlap to
  migrate.
- **This is distinct from software version-line support.** The security-fix
  support window for a software release line is defined separately in
  [`SECURITY.md`](../../SECURITY.md#supported-versions) (the previous minor
  receives Critical fixes for one minor cycle). That governs *security fixes to
  a software line*; the window here governs *emission and support of a format
  major*. They are not the same window and must not be conflated.
- **Supersession and the window are announced in `CHANGELOG.md` and the release
  notes** of the release that introduces the successor major. A superseded
  major is labelled deprecated there; its removal is announced again in the
  release that drops its emission.

## What a consumer may rely on

A downstream consumer of a Signals export may rely on the following for the
lifetime of a given major:

1. **Self-description.** `metadata.json` carries `schema_version`; a consumer
   can read the major and decide how to parse **before** reading any data file.
   `collector_status.json` and `manifest.json` likewise carry their own
   version.
2. **Additive-only within a major.** A consumer written for a major keeps
   working across every emission of that major, provided it **ignores unknown
   fields and files** (see above). New data never breaks an old consumer within
   the same major.
3. **Integrity.** `manifest.json` lists every other ZIP entry with its SHA-256
   under `"algorithm": "sha256"`; a consumer can verify each file offline before
   trusting it. The manifest is always the complete list of the export's other
   files.
4. **No secrets, ever.** An export never contains credentials, connection
   secrets, or `secret_ref` *values* — only diagnostic data and non-sensitive
   target identity (host/port/dbname/username). This is an invariant of the
   collector (INV-SIGNALS-07), not a property of this format version, so it
   holds across all majors.
5. **A migration window.** When a major is superseded, a consumer has the
   emission & support window above (two minor cycles) to migrate to the new
   major before the old one stops being emitted.

A consumer may **not** rely on: the presence of any field or file not defined
by the format major it targets; a specific ordering of files or JSON keys; the
absence of additional files; or a producer emitting a major other than the
current one (there is no negotiation).

## Deprecation process (producer)

1. A breaking change is proposed; it requires a new major on the affected
   surface (and a specification/behaviour change in the collector — out of scope
   here, which is documentation of the policy, not a format change).
2. The release that introduces the new major announces the supersession in
   `CHANGELOG.md` + release notes and labels the previous major **deprecated**.
3. The previous major remains emitted, documented, and regression-tested for the
   emission & support window (two minor cycles).
4. A later release removes emission of the deprecated major and announces the
   removal in `CHANGELOG.md` + release notes.

At every step the producer emits exactly one major; the "overlap" is temporal
(across releases), not simultaneous (within one export).

## Non-goals

- **No runtime producer/consumer negotiation.** Signals does not inspect a
  consumer's capabilities or emit a chosen major on request; it emits the
  current major. Compatibility is the consumer's responsibility, bounded by this
  written contract.
- **No new version fields or constants.** This policy documents the existing
  `schema_version` / `collector_status_schema_version` / `manifest_version`
  surfaces; it adds none.
- **No change to the existing format or constants.** This is a documentation
  artifact; it changes no emitted bytes.

## Cross-repo contract

This is the **producer** side of a producer/consumer format contract. The
**consumer** side — the Analyzer ingestion gate that accepts
`signals-snapshot.v1` — is tracked in Elevarq/Analyzer#3763 and is governed on
its own side. This document is written entirely in Signals' own terms: it
references no private Analyzer or Elevarq internals, and a downstream consumer
needs only this document and the emitted, self-describing version fields to
integrate. Signals is a public, independent product; the contract is expressed
through the versioned, self-describing format, not through any shared internal
code.

## Traceability

The policy's version references are pinned to the live collector constants by
`tests/signals_snapshot_format_policy_test.go`: the guard fails if
`snapshot.SchemaVersion`, `export.CollectorStatusSchemaVersion`, or
`export.ManifestVersion` change without this document being updated, so the
written contract can never silently drift from the emitted format.
</content>
