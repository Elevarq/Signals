# Multi-Engine Report Contract (producer side)

## Status

ACTIVE

## Purpose

Define — and make verifiable on the **producer side** — the `signals-snapshot.v1`
export contract as the five 2.0 engines on `poc/singlestore` actually emit it:
PostgreSQL, SingleStore, MySQL, MariaDB, ClickHouse. This is the umbrella contract
for Elevarq/Signals #530. It owns the cross-cutting shape (members, metadata,
per-collector evidence, enums, redaction, manifest) that every engine's export
must satisfy; the engine-specific mechanics live in `engine-keyed-reporting.md`
(engine tagging + no cross-engine leak) and each engine's collector spec
(redaction columns).

Signals stays read-only and local-first. The engine fields are **additive**: the
PostgreSQL export is byte-compatible with 1.x `signals-snapshot.v1`; nothing here
changes historical PostgreSQL report semantics.

## Scope

- **Producer obligations (verifiable here, CI-gated):** the shape every engine's
  `export.Builder` output satisfies — §"The contract" + rules MERC-R001..R007,
  plus the single-engine-scope invariant MERC-R009 (#622).
  Asserted by the cross-engine conformance test over the real `WriteTo` path.
- **Consumer obligations (owned by Analyzer, the OPEN acceptance):** how a
  consumer treats a new `engine` value, additive fields, and the
  empty/failed/skipped distinction — MERC-R008 + §"Consumer obligations". These
  are confirmed by the Analyzer owners (the issue stays open until they do);
  Signals cannot close them unilaterally.

Out of scope: Analyzer rules/Replay, PostgreSQL report semantics (unchanged), and
the normalized workload envelope (that is `workload-measurement-model.md` / #533).

## The contract (what every engine emits today)

A snapshot export is a ZIP with the same members for every engine:

| Member | Content |
|--------|---------|
| `metadata.json` | schema + engine identity (`engine` always present; `engine_version` non-PostgreSQL-only) + collection context (see below) |
| `collector_status.json` | per-collector status/freshness; ONLY the snapshot engine's collectors |
| `snapshots.ndjson` | one line per exported snapshot |
| `query_catalog.json` | the engine's collector catalog; ONLY the snapshot engine's collectors |
| `query_runs.ndjson` | one line per collector run (status/reason/row_count/duration) |
| `query_results.ndjson` | joinable per-run payload (successful runs) |
| `manifest.json` | `{manifest_version, algorithm:"sha256", files:[{name, sha256}]}` over every other member |

`metadata.json` fields (confirmed against `export.writeMetadata`):

- `schema_version` — always `"signals-snapshot.v1"`, every engine (unchanged).
- `engine` — **always present**; `postgresql | singlestore | mysql | mariadb |
  clickhouse`. An empty/legacy snapshot normalizes to `postgresql` (EID-R004 /
  INV-EID-01).
- `engine_version` — present **only** for non-PostgreSQL engines (the collected
  server version). PostgreSQL does **not** gain this field; its version stays in
  `pg_version` inside `snapshots.ndjson` / `query_runs.ndjson` — so a PostgreSQL
  export gains no new metadata field and stays byte-compatible with 1.x re: its
  version carrier (`engine-keyed-reporting.md` INV-MKR-02 / MKR-R008).
- plus the existing `collector_version`, `collector_commit`, `collected_at` /
  `generated_at`, `instance_id`, `signals_version`, `collection_status`,
  `run_scope`, `snapshot_count`. `pg_version` is NOT a `metadata.json` field — it
  is carried per-row in `snapshots.ndjson` / `query_runs.ndjson`.

Per-collector evidence (`query_runs.ndjson`): `status` ∈ `success | failed |
skipped`; a non-`success` run carries a canonical `reason`; a `success` run
carries `row_count` and a joinable `query_results` payload (0 rows is a legitimate
success).

## Rules (producer — verifiable)

- **MERC-R001** | Every engine's export ZIP contains all seven members above.
- **MERC-R002** | `metadata.json.schema_version == "signals-snapshot.v1"` for
  every engine; the engine fields are additive and never remove/alter a v1 field.
- **MERC-R003** | `metadata.engine` is always present and engine-correct
  (empty/legacy → `postgresql`). A non-PostgreSQL snapshot additionally sets a
  non-empty `metadata.engine_version`; a PostgreSQL snapshot does NOT gain
  `engine_version` (its version stays in `pg_version` within
  `snapshots.ndjson`/`query_runs.ndjson`) — byte-compat per
  `engine-keyed-reporting.md` INV-MKR-02.
- **MERC-R004** | `collector_status.json` and `query_catalog.json` contain ONLY
  collectors belonging to the snapshot's engine catalog — no cross-engine leak
  (`engine-keyed-reporting.md` MKR-R001; per-engine catalogs are disjoint).
- **MERC-R005** | Every `query_runs` row's `status` ∈ `{success, failed,
  skipped}`; a `success` run has a `row_count` and a joinable `query_results`
  payload; a non-`success` run carries a `reason` and emits no successful payload.
- **MERC-R006** | `manifest.json` lists every other member with a non-empty
  `sha256` (algorithm `sha256`) and lists no member absent from the ZIP.
- **MERC-R007** | Default (non-high-sensitivity) exports carry no secret payload:
  query/sample text (`QUERY_TEXT`, `INFO`, `DIGEST_TEXT`, `trx_query`, ClickHouse
  `query`), locked-key data (`LOCK_DATA`/`lock_data`), and secret config
  (`CONFIG_JSON`) are redacted per each engine's collector spec.

- **MERC-R009** (#622) | A report must truthfully represent ONE engine. An
  export whose resolved scope spans more than one engine is **rejected** with a
  clear, actionable error (`ErrHeterogeneousScope`) that names the engines and
  the required scope — it never emits one archive mixing engines under a single
  engine identity. Explicit `TargetID` / `SnapshotID` scopes are inherently
  single-target (one engine) and always pass; a homogeneous unscoped scope
  (including a legacy PostgreSQL-only / single-target install) passes unchanged.
  This Signals-side invariant is enforced independently of the Analyzer sign-off
  (a report cannot lie about its engine identity regardless of consumer state).

## Rules (consumer — Analyzer, OPEN sign-off)

- **MERC-R008** | Because `schema_version` is unchanged and the engine fields are
  additive, a consumer MUST forward-ignore an unknown `engine` value and unknown
  additive fields (ignore-with-notice) rather than hard-fail, and MUST keep
  ingesting historical PostgreSQL exports identically. Confirmed by the Analyzer
  owners — this is the acceptance gate #530 holds open.

## Invariants

- **INV-MERC-01** | `schema_version` never changes for the additive engine
  fields; a PostgreSQL export gains no new metadata field (`engine_version` is
  non-PostgreSQL-only) and keeps `pg_version` as its version carrier.
- **INV-MERC-02** | No export leaks a collector from another engine's catalog.
- **INV-MERC-03** | No default export contains raw credentials, secret config, or
  query/sample text.
- **INV-MERC-04** | No exported archive contains snapshots/runs/results from more
  than one engine — the archive's declared `engine` describes ALL included
  evidence (#622).

## Failure conditions

| Trigger | Response |
|---------|----------|
| A member missing from the ZIP or from `manifest.json` | Contract violation (conformance test fails). |
| A non-PostgreSQL snapshot missing `engine`/`engine_version`, or a PostgreSQL snapshot carrying them | Contract violation. |
| A collector from a foreign engine in `collector_status`/`query_catalog` | Contract violation (cross-engine leak). |
| A secret-bearing column present in a default export | Contract violation (redaction breach). |
| An unscoped export whose scope spans >1 engine | Rejected with `ErrHeterogeneousScope` naming the engines + required scope (#622); never a mixed artifact. |

## Consumer obligations (owned by Analyzer — the OPEN acceptance of #530)

The following are the consumer-side acceptance criteria #530 keeps open until the
Analyzer owners confirm. Signals provides a five-engine conformance corpus (real
`export.Builder` ZIPs, one per engine, reproducible via each engine's
live-qualification harness or the conformance test's corpus mode) so Analyzer can
verify its ingest path against the real shape:

1. Historical PostgreSQL `signals-snapshot.v1` ingestion remains identical.
2. A snapshot with a new `engine` value ingests (ignore-with-notice), not a hard
   fail (MERC-R008).
3. Additive metadata fields (e.g. `engine_version`) are forward-ignorable.
4. The consumer distinguishes `success`-with-0-rows (collected, nothing to
   report) from `skipped` (capability/permission degrade) from `failed`.
5. Engine-native workload evidence is consumed per `workload-measurement-model.md`
   (#533); query identities are never equated across engines.

## Traceability

Acceptance: `specifications/multi-engine-report-contract.acceptance.md`.
Producer rules MERC-R001..R007 + R009 + INV-MERC-01..04 are CI-gated by the cross-engine
conformance test (`internal/export/report_contract_test.go`). MERC-R008 + the
consumer obligations are the open Analyzer sign-off. Builds on
`engine-keyed-reporting.md` (MKR) and each engine's collector spec (redaction).
