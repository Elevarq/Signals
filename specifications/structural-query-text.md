# Structural Query Text (bounded normalized DIGEST_TEXT)

## Status

DRAFT

## Purpose

Let an operator **opt in** to collecting the **server-normalized** statement
digest text (`DIGEST_TEXT`) for the MySQL-wire family (MySQL, MariaDB,
SingleStore) as a bounded, high-sensitivity structural-query signal, so a
downstream consumer (Elevarq Analyzer, #15) has the per-digest query *shape*
needed to derive safe column-level index candidates and an EXPLAIN workload —
the analogue of PostgreSQL's normalized `pg_stat_statements.query`.

This spec owns only the **gating, bounding, and redaction interaction** for the
`DIGEST_TEXT` column of the existing digest collectors
(`mysql_statement_digests_v1` and the SingleStore equivalent). It adds **no new
collector, no new SQL, no new code path** — the digest collectors, their SQL,
and the engine-neutral persistence/export are unchanged. It changes only
*whether and how* one already-collected, already-redacted column survives to
disk and export.

Separation of concerns is explicit and preserved: **Signals collects, Analyzer
interprets the SQL, Replay validates.** Signals performs a simple deterministic
top-N selection (the digest collector's existing `ORDER BY` latency, `LIMIT`);
it does not rank, diagnose, or interpret the text.

## Safety basis

`performance_schema.events_statements_summary_by_digest.DIGEST_TEXT` is
**normalized by the database server before Signals reads it**: literal values
are collapsed to `?`, lists of literals to `?, ...`, and whitespace/comments
canonicalised. Therefore collecting `DIGEST_TEXT` verbatim collects a query
*shape* and **never collects literal values** — the property that makes this
safe to surface where raw SQL text (`INFO`, `trx_query`) and row data
(`LOCK_DATA`) are not. This spec treats that server-side normalization as a
**verifiable guarantee**, re-asserted by adversarial tests across every
supported engine line (see Constraints), not as an assumption.

## Interfaces

### Inputs

| Input | Type | Constraints |
|-------|------|-------------|
| `collect_normalized_query_text` (config `yaml`) | bool | Default **false**. Secure-default-OFF: absent/false ⟹ no structural query text is collected. |
| `WithStructuralQueryText(enabled bool)` | CollectorOption | Construction-time wiring of the above; layered on top of `highSensitivityEnabled`, exactly as `collectArrayRangeHistograms` (#128). |
| `structural_query_text_max_bytes` (config `yaml`) | int | Default **4096**. Per-row cap on the retained `DIGEST_TEXT`. Validator rejects non-positive at startup. |

### Affected collectors

`mysql_statement_digests_v1` (MySQL, MariaDB) and the SingleStore digest
collector — the only collectors whose `SensitiveColumns` includes the
server-normalized `DIGEST_TEXT`.

### Outputs

Per digest row: `DIGEST_TEXT` is either `null` (default / not opted in) or a
UTF-8 string of at most `structural_query_text_max_bytes` bytes carrying the
server-normalized statement shape. All other row columns are unchanged.

## Behaviors

- **Given** `collect_normalized_query_text=false` (or unset), **when** the
  digest collector runs, **then** every row's `DIGEST_TEXT` is `null` in the
  persisted and exported payload (behaviour identical to v2.0.0-poc.1).
- **Given** `collect_normalized_query_text=true`, **when** the digest collector
  runs, **then** each row's `DIGEST_TEXT` survives, truncated to
  `structural_query_text_max_bytes` on a UTF-8 boundary, and every other
  HighSensitivity column on *other* collectors (`trx_query`, `LOCK_DATA`,
  `INFO`) remains redacted to `null`.
- **Given** `collect_normalized_query_text=true` and a digest row whose
  `DIGEST_TEXT` is already `null` at the source (e.g. the NULL-digest overflow
  row), **when** the collector runs, **then** the value stays `null` (nothing
  is fabricated).

## Rules

- **SQT-R001** | `DIGEST_TEXT` is redacted to `null` whenever
  `collect_normalized_query_text` is false, **regardless of**
  `highSensitivityEnabled`. The structural opt-in is the *sole* gate that lets
  `DIGEST_TEXT` survive.
- **SQT-R002** | The opt-in un-redacts **only** `DIGEST_TEXT`. `trx_query`,
  `LOCK_DATA`, `INFO`, and every other `SensitiveColumns` entry on every
  collector stay governed by `highSensitivityEnabled` and are never affected by
  this opt-in. (No global un-redaction.)
- **SQT-R003** | When retained, `DIGEST_TEXT` is truncated to
  `structural_query_text_max_bytes` bytes on a valid UTF-8 boundary; a
  truncated value carries a trailing ellipsis marker `…` so truncation is
  observable and never mistaken for a complete statement.
- **SQT-R004** | Signals performs no parsing, normalization, literal
  extraction, or reconstruction of `DIGEST_TEXT`; it retains the server's
  bytes verbatim (modulo SQT-R003 truncation). It never reads the
  literal-bearing `INFO`/`trx_query`/`SQL_TEXT` columns to enrich it.
- **SQT-R005** | Selection is the digest collector's existing deterministic
  top-N (`ORDER BY SUM_TIMER_WAIT DESC LIMIT 200`); this spec adds no ranking
  or re-ordering.
- **SQT-R006** | The opt-in state is recorded in the snapshot
  provenance/metadata (`collect_normalized_query_text: true|false`) so a
  consumer can tell a genuinely-empty workload from a redacted one.

## Invariants

- **INV-SQT-01** | No literal value ever appears in a retained `DIGEST_TEXT`:
  for every supported engine line, the server-normalized digest contains `?`
  placeholders, never the original literals. (Re-asserted adversarially — a
  violation on any line is a hard stop, never a weakened test.)
- **INV-SQT-02** | Secure default: with no configuration, Signals' output is
  byte-for-byte unchanged from v2.0.0-poc.1 (`DIGEST_TEXT` null).
- **INV-SQT-03** | Producer fidelity: for the opted-in field, **collected ==
  persisted == exported** — the value the collector retained is the value on
  disk is the value in the export ZIP; no stage drops, truncates differently,
  or mutates it beyond SQT-R003.
- **INV-SQT-04** | The opt-in never widens any other column's exposure
  (SQT-R002 holds across all collectors, asserted catalog-wide).

## Failure conditions

| Trigger | System response |
|---------|-----------------|
| `structural_query_text_max_bytes <= 0` | Startup config validation error; Signals does not start. |
| Performance Schema off / digest consumer disabled | Digest collector is already empty; opt-in is a no-op (no rows → no text). Legitimate success, not an error. |
| Source `DIGEST_TEXT` is `null` | Retained as `null` (SQT-R001 behaviour three); never fabricated. |

## Constraints

- **Adversarial normalization matrix** — INV-SQT-01 is verified live against
  **MySQL 8.0, 8.4, 9.7** and **MariaDB 10.11, 11.4, 11.8, 12.3**, using
  statements that embed string, numeric, `IN (...)` list, and comment literals;
  the retained `DIGEST_TEXT` must contain `?` placeholders and none of the
  injected literals on every line.
- **Bound** — top-200 by latency (unchanged) × `structural_query_text_max_bytes`
  (default 4096) per row.
- **Least exposure** — only `DIGEST_TEXT`; only when explicitly enabled.

## NFR overrides

- **Security** — secure-default-OFF; narrow single-column opt-in; literals never
  collected (INV-SQT-01); no global un-redaction (SQT-R002/INV-SQT-04).
- **Determinism** — identical input + config ⟹ identical retained text
  (verbatim bytes, deterministic truncation).
- **Compatibility** — default behaviour is byte-identical to v2.0.0-poc.1
  (INV-SQT-02); additive config only.

## Traceability

specification (this file) → acceptance cases
(`structural-query-text.acceptance.md`) → tests → implementation. Resolves the
producer-evidence gap for Elevarq/Analyzer#15. Ships in Signals **v2.0.0-poc.2**.
