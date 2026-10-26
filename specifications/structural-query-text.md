# Structural Query Text (bounded normalized DIGEST_TEXT)

## Status

ACTIVE

The behavioral contract is implemented and unit-tested. Per-engine/version
*enablement* of the capability remains gated by SQT-R009 (live INV-SQT-01
qualification across the MySQL/MariaDB matrix); until a line passes, it stays in
the **unsupported** state. The capability's secure default is OFF, so landing
the mechanism ahead of that qualification changes no existing output.

## Purpose

Let an operator **opt in** to collecting the **server-normalized** statement
digest text (`DIGEST_TEXT`) for the MySQL-wire family (MySQL, MariaDB,
SingleStore) as a bounded, high-sensitivity **structural-query** signal, so a
downstream consumer (Elevarq Analyzer, #15) has the per-digest query *shape*
needed to derive safe column-level index candidates and an EXPLAIN workload —
the analogue of PostgreSQL's normalized `pg_stat_statements.query`.

This spec owns only the **gating, bounding, truncation-signalling, capability
state, and redaction interaction** for the `DIGEST_TEXT` column of the existing
digest collectors (`mysql_statement_digests_v1` and the SingleStore
equivalent). It adds **no new collector and no new SQL**: the digest collectors,
their SQL, and the engine-neutral persistence/export are unchanged. It changes
only *whether and how* one already-collected, already-redacted column survives
to disk and export, plus the structured state that lets a consumer interpret it
honestly.

Separation of concerns is explicit and preserved: **Signals collects, Analyzer
interprets the SQL, Replay validates.** Signals performs a simple deterministic
top-N selection (the digest collector's existing `ORDER BY` latency, `LIMIT`);
it does not rank, diagnose, or interpret the text.

## Safety basis

`performance_schema.events_statements_summary_by_digest.DIGEST_TEXT` is
**normalized by the database server before Signals reads it**: literal values
are collapsed to `?`, lists of literals to `?, ...`, and whitespace/comments
canonicalised. Therefore collecting `DIGEST_TEXT` verbatim collects a query
*shape* and **does not intentionally expose runtime literal values** — the
property that makes this safe to surface where raw SQL text (`INFO`,
`trx_query`) and row data (`LOCK_DATA`) are not. This spec treats that
server-side normalization as a **verifiable guarantee**, re-asserted
adversarially across every supported engine line (INV-SQT-01), never assumed.

## Sensitivity classification

Normalized `DIGEST_TEXT` is **sensitive structural query evidence**, not
harmless metadata:

- runtime **literal values are not intentionally exposed** (server-normalized to
  `?`);
- database / schema / table / column **identifiers ARE exposed** (they are the
  structural content the consumer needs);
- therefore it remains **sensitive metadata requiring explicit opt-in**, and is
  never described or handled as non-sensitive.

## Interfaces

### Inputs

| Input | Type | Constraints |
|-------|------|-------------|
| `collect_normalized_query_text` (config `yaml`) | bool | Default **false** (secure-default-OFF). The sole switch that lets `DIGEST_TEXT` survive. **Independent** of `high_sensitivity_collectors_enabled`: enabling either MUST NOT implicitly enable the other. |
| `WithStructuralQueryText(enabled bool)` | CollectorOption | Construction-time wiring of the above; layered on top of `highSensitivityEnabled`, exactly as `collectArrayRangeHistograms` (#128), and independent of it. |

The per-row cap is a **fixed constant** `structuralQueryTextMaxBytes = 4096` for
the POC — deliberately **not** a configuration knob (do not over-engineer
configurability; the cap may be revisited later from empirical evidence). It
becomes configurable only if the existing config architecture makes that
essentially free; it does not today, so it stays a constant.

### Affected collectors

`mysql_statement_digests_v1` (MySQL, MariaDB) and the SingleStore digest
collector — the only collectors whose `SensitiveColumns` includes the
server-normalized `DIGEST_TEXT`.

### Outputs (per digest row)

| Field | Meaning |
|-------|---------|
| `DIGEST_TEXT` | `null` (default / not opted in / null at source), else the server-normalized shape, at most `structuralQueryTextMaxBytes` bytes. |
| `digest_text_truncated` | `true` when the retained `DIGEST_TEXT` was cut to the cap, else `false`. Machine-visible truncation signal (SQT-R003/R007). Absent/omitted only when structural collection is disabled. |

## Capability state (must never collapse into "successful empty")

The producer records, per snapshot (provenance/metadata) and resolvable per
digest collector, a state a consumer can act on. These five states MUST remain
distinguishable downstream — an unsupported engine, a safety failure, and a
genuinely idle workload MUST NOT all look like the same empty success:

1. **disabled** — `collect_normalized_query_text=false`; `DIGEST_TEXT` null by
   design (poc.1-compatible).
2. **enabled, evidence available** — opt-in on, ≥1 digest row carries retained
   `DIGEST_TEXT`.
3. **enabled, no qualifying evidence** — opt-in on, collector ran, but no hot
   digest rows (empty perf_schema / idle workload). A legitimate success, not a
   failure, and distinct from (1) and (4).
4. **unsupported / unavailable** — the engine/version is not on the qualified
   normalization matrix, or Performance Schema / the digest consumer is off;
   the capability does not run and says so.
5. **normalization safety failure** — INV-SQT-01 qualification failed for this
   engine/version; the capability is withheld (not shipped) for it.

## Behaviors

- **Given** `collect_normalized_query_text=false` (or unset), **when** the digest
  collector runs, **then** every row's `DIGEST_TEXT` is `null` and the snapshot
  state is **disabled** (output byte-identical to v2.0.0-poc.1).
- **Given** `collect_normalized_query_text=true`, **when** the digest collector
  runs with hot rows, **then** each row's `DIGEST_TEXT` survives (truncated on a
  UTF-8 boundary to the cap, with `digest_text_truncated` set accordingly), and
  every HighSensitivity column on *other* collectors (`trx_query`, `LOCK_DATA`,
  `INFO`) remains redacted to `null`.
- **Given** `collect_normalized_query_text=true` but no hot digest rows,
  **then** the state is **enabled, no qualifying evidence** — not conflated with
  disabled.
- **Given** a digest row whose `DIGEST_TEXT` is `null` at source, **then** it
  stays `null` with `digest_text_truncated=false` (nothing fabricated).

## Rules

- **SQT-R001** | `DIGEST_TEXT` is redacted to `null` whenever
  `collect_normalized_query_text` is false, **regardless of**
  `highSensitivityEnabled`. The structural opt-in is the sole gate that lets
  `DIGEST_TEXT` survive.
- **SQT-R002** | The opt-in un-redacts **only** `DIGEST_TEXT`. `trx_query`,
  `LOCK_DATA`, `INFO`, and every other `SensitiveColumns` entry on every
  collector stay governed by `highSensitivityEnabled` and are never affected by
  this opt-in. This is not a blanket high-sensitivity un-redaction mechanism.
- **SQT-R003** | When retained, `DIGEST_TEXT` is truncated to
  `structuralQueryTextMaxBytes` bytes on a valid UTF-8 boundary; a truncated
  value carries a trailing `…` marker **and** sets `digest_text_truncated=true`.
  The structured flag is authoritative; the marker is a human convenience.
- **SQT-R004** | Signals performs no parsing, normalization, literal extraction,
  or reconstruction of `DIGEST_TEXT`; it retains the server's bytes verbatim
  (modulo SQT-R003 truncation). It never reads the literal-bearing
  `INFO`/`trx_query`/`SQL_TEXT` columns to enrich it.
- **SQT-R005** | Selection is the digest collector's existing deterministic
  top-N (`ORDER BY SUM_TIMER_WAIT DESC LIMIT 200`); this spec adds no ranking or
  re-ordering.
- **SQT-R006** | The capability state (the five states above) and
  `collect_normalized_query_text` are recorded in snapshot provenance/metadata so
  a consumer distinguishes disabled / evidence / no-evidence / unsupported /
  safety-failure — never collapsing them into an empty success.
- **SQT-R007** | A truncated `DIGEST_TEXT` MUST never be presentable as complete
  structural evidence. `digest_text_truncated` travels with the value through
  persistence and export. The consumer contract (Analyzer, #15) is **fail
  closed**: when truncation prevents safely establishing the table / predicate /
  join / order structure, the index-advice path yields `NOT_ASSESSED`, never a
  guess. (Producer obligation here: emit the flag faithfully; the consumer
  obligation is recorded for #15.)
- **SQT-R008** | MySQL and MariaDB remain **distinct engine/collector
  contracts** (separate identities, separate qualification legs). No separate
  MariaDB/EITS structural-query *path* is introduced merely because MariaDB is a
  distinct engine: where live qualification proves identical Performance Schema
  `DIGEST_TEXT` semantics, the same implementation is reused. MariaDB-specific
  behaviour is introduced **only** when the live supported-version matrix proves
  it is required.
- **SQT-R009** | The capability is shipped per engine/version **only** after
  that line passes INV-SQT-01 qualification. A line that fails is left in the
  **unsupported** state (SQT state 4/5), not shipped with a weakened check.

## Invariants

- **INV-SQT-01** | No runtime literal value ever appears in a retained
  `DIGEST_TEXT`: for every supported engine line the server-normalized digest
  contains `?` placeholders, never the original literals. Runtime leakage is a
  **hard failure**; qualified empirically across the matrix; a failing line is
  withheld (SQT-R009), never shipped with a relaxed assertion.
- **INV-SQT-02** | Secure default: with no configuration, Signals' output is
  byte-for-byte unchanged from v2.0.0-poc.1 (`DIGEST_TEXT` null, state
  **disabled**).
- **INV-SQT-03** | Producer fidelity: for the opted-in field, **collected ==
  persisted == exported** — the retained value (and its `digest_text_truncated`
  flag) is identical on the collector, on disk, and in the export ZIP; no stage
  drops, re-truncates, or mutates it.
- **INV-SQT-04** | The opt-in never widens any other column's exposure
  (SQT-R002 holds catalog-wide).
- **INV-SQT-05** | The five capability states never collapse: an unsupported
  engine, a normalization-safety failure, and an idle-but-enabled workload are
  each distinguishable downstream and none is reported as a plain empty success.

## Failure conditions

| Trigger | System response |
|---------|-----------------|
| INV-SQT-01 qualification fails for an engine/version | Capability withheld for that line (state **normalization safety failure**); not shipped. Hard stop for that line (SQT-R009). |
| Engine/version not on the qualified matrix | State **unsupported/unavailable**; capability does not run; distinct from an empty success. |
| Performance Schema off / digest consumer disabled | Digest collector already empty; opt-in is a no-op → state **unsupported/unavailable** or **enabled, no qualifying evidence** as applicable. Legitimate, not an error. |
| Source `DIGEST_TEXT` is `null` | Retained as `null`, `digest_text_truncated=false`; never fabricated. |

## Constraints

- **Adversarial normalization matrix** — INV-SQT-01 verified live against
  **MySQL 8.0, 8.4, 9.7** and **MariaDB 10.11, 11.4, 11.8, 12.3**, using
  statements that embed string, numeric, `IN (...)` list, and comment literals;
  the retained `DIGEST_TEXT` must contain `?` placeholders and none of the
  injected literals on every line, or the line is withheld (SQT-R009).
- **Bound** — top-200 by latency (unchanged) × fixed `structuralQueryTextMaxBytes`
  = 4096 bytes per row.
- **Least exposure** — only `DIGEST_TEXT`; only when explicitly enabled.
- **No invented divergence** — MariaDB reuses the MySQL implementation unless the
  matrix proves otherwise (SQT-R008).

## NFR overrides

- **Security** — secure-default-OFF; narrow single-column opt-in independent of
  the generic high-sensitivity switch; literals not intentionally exposed
  (INV-SQT-01); identifiers are exposed ⟹ sensitive, explicit opt-in required;
  no global un-redaction (SQT-R002/INV-SQT-04).
- **Determinism** — identical input + config ⟹ identical retained text and
  truncation flag (verbatim bytes, deterministic UTF-8-boundary truncation).
- **Compatibility** — default behaviour byte-identical to v2.0.0-poc.1
  (INV-SQT-02); additive field + config only.
- **Honest absence** — downstream never confuses disabled / no-evidence /
  unsupported / safety-failure (INV-SQT-05); query-specific recommendations that
  require this evidence become `NOT_ASSESSED` rather than guessed when it is
  absent (consumer contract, #15).

## Traceability

specification (this file) → acceptance cases
(`structural-query-text.acceptance.md`) → tests → implementation. Resolves the
producer-evidence gap for Elevarq/Analyzer#15. Ships in Signals **v2.0.0-poc.2**.
