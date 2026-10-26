# Acceptance Tests: structural-query-text

## Feature

`specifications/structural-query-text.md` — the opt-in, bounded, secure-default-
OFF retention of the server-normalized `DIGEST_TEXT` for the MySQL-wire family.

## Test Cases

### TC-SQT-01: secure default — DIGEST_TEXT null when not opted in (normal)

**Rule:** SQT-R001, INV-SQT-02

**Given:** a digest collector with rows whose source `DIGEST_TEXT` is a
normalized statement, and `collect_normalized_query_text=false`.

**When:** redaction runs.

**Then:** every row's `DIGEST_TEXT` is `null`, regardless of
`highSensitivityEnabled` (both true and false).

**Expected Result:** Pass when output is byte-identical to the
poc.1 redacted shape.

---

### TC-SQT-02: opt-in retains normalized text, bounded (normal)

**Rule:** SQT-R001, SQT-R003, INV-SQT-03

**Given:** `collect_normalized_query_text=true`,
`structural_query_text_max_bytes=4096`, and a row whose normalized
`DIGEST_TEXT` is `SELECT * FROM orders WHERE id = ?`.

**When:** redaction runs and the row is persisted and exported.

**Then:** the retained/persisted/exported `DIGEST_TEXT` equals the source bytes
verbatim (collected == persisted == exported).

**Expected Result:** Pass when the exported value matches the collected value.

---

### TC-SQT-03: opt-in does NOT un-redact other sensitive columns (boundary)

**Rule:** SQT-R002, INV-SQT-04

**Given:** `collect_normalized_query_text=true`, `highSensitivityEnabled=false`,
and rows from `mysql_innodb_transactions_v1` (`trx_query`) and
`mysql_data_locks_v1` (`LOCK_DATA`).

**When:** redaction runs.

**Then:** `trx_query` and `LOCK_DATA` are still `null`; only the digest
collector's `DIGEST_TEXT` survives.

**Expected Result:** Pass when no literal-bearing / row-data column is widened.

---

### TC-SQT-04: truncation on a UTF-8 boundary with marker (boundary)

**Rule:** SQT-R003

**Given:** `collect_normalized_query_text=true`,
`structural_query_text_max_bytes=16`, and a normalized `DIGEST_TEXT` longer than
16 bytes containing a multi-byte UTF-8 rune straddling the cap.

**When:** redaction runs.

**Then:** the retained value is valid UTF-8, ≤16 bytes of content, and ends with
the `…` truncation marker (no partial rune, no silent full-length pass-through).

**Expected Result:** Pass when the value is valid UTF-8, bounded, and marked.

---

### TC-SQT-05: null source digest stays null under opt-in (boundary)

**Rule:** SQT-R001 (behaviour three)

**Given:** `collect_normalized_query_text=true` and the NULL-digest overflow row
(source `DIGEST_TEXT` is `null`).

**When:** redaction runs.

**Then:** `DIGEST_TEXT` stays `null` (nothing fabricated).

**Expected Result:** Pass when a null source is preserved as null.

---

### TC-SQT-06: non-positive max_bytes is rejected at startup (invalid)

**Rule:** Failure conditions

**Given:** `structural_query_text_max_bytes=0` (or negative).

**When:** config validation runs at startup.

**Then:** startup fails with a validation error; the collector does not start.

**Expected Result:** Pass when startup is refused.

---

### TC-SQT-07: no literal leakage across the engine matrix (failure / adversarial)

**Rule:** INV-SQT-01

**Given:** `collect_normalized_query_text=true` and a live server on each of
MySQL 8.0/8.4/9.7 and MariaDB 10.11/11.4/11.8/12.3, after executing statements
embedding string (`'secret'`), numeric (`42`), `IN (1,2,3)` list, and
`/* comment */` literals.

**When:** the digest collector runs and the retained `DIGEST_TEXT` is inspected.

**Then:** the retained text contains `?` placeholders and **none** of the
injected literal values (`secret`, `42`, `1`, `2`, `3`) on any line.

**Expected Result:** Pass only when every engine line is literal-free; any
occurrence of an injected literal is a hard FAIL (normalization guarantee
broken → stop, do not weaken the test).

---

### TC-SQT-08: opt-in state recorded in provenance (normal)

**Rule:** SQT-R006

**Given:** a snapshot taken with `collect_normalized_query_text` true, then
false.

**When:** the snapshot metadata/provenance is read.

**Then:** it records `collect_normalized_query_text: true` and `false`
respectively, so a consumer distinguishes an empty workload from a redacted one.

**Expected Result:** Pass when provenance reflects the opt-in state.
