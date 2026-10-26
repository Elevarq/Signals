# Acceptance Tests: structural-query-text

## Feature

`specifications/structural-query-text.md` — the opt-in, bounded, secure-default-
OFF retention of the server-normalized `DIGEST_TEXT` for MySQL and MariaDB
(SingleStore excluded — SQT-R010), with machine-visible truncation and
non-collapsing capability state.

## Test Cases

### TC-SQT-01: secure default — DIGEST_TEXT null when not opted in (normal)

**Rule:** SQT-R001, INV-SQT-02

**Given:** a digest collector with rows whose source `DIGEST_TEXT` is a
normalized statement, and `collect_normalized_query_text=false`.

**When:** redaction runs.

**Then:** every row's `DIGEST_TEXT` is `null` regardless of
`highSensitivityEnabled` (both true and false), and the snapshot state is
**disabled**.

**Expected Result:** Pass when output is byte-identical to the poc.1 redacted
shape.

---

### TC-SQT-02: opt-in retains normalized text, bounded, fidelity preserved (normal)

**Rule:** SQT-R001, SQT-R003, INV-SQT-03

**Given:** `collect_normalized_query_text=true` and a row whose normalized
`DIGEST_TEXT` is `SELECT * FROM orders WHERE id = ?` (under the 4096-byte cap).

**When:** redaction runs and the row is persisted and exported.

**Then:** the retained/persisted/exported `DIGEST_TEXT` equals the source bytes
verbatim and `digest_text_truncated=false` at every stage (collected ==
persisted == exported).

**Expected Result:** Pass when the exported value and flag match the collected
ones.

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

### TC-SQT-04: switches are independent (boundary)

**Rule:** SQT-R001, SQT-R002 (independence)

**Given:** `high_sensitivity_collectors_enabled=true` but
`collect_normalized_query_text=false`.

**When:** the digest collector runs.

**Then:** `DIGEST_TEXT` is still `null` — enabling the generic high-sensitivity
switch does not implicitly enable structural query text (and vice versa).

**Expected Result:** Pass when neither switch implies the other.

---

### TC-SQT-05: truncation is machine-visible and UTF-8-safe (boundary)

**Rule:** SQT-R003, SQT-R007

**Given:** `collect_normalized_query_text=true` and a normalized `DIGEST_TEXT`
longer than the cap, containing a multi-byte UTF-8 rune straddling the cap
boundary (test uses a small fixed cap injected for the unit, real cap 4096).

**When:** redaction runs.

**Then:** the retained value is valid UTF-8, within the cap, ends with the `…`
marker, and `digest_text_truncated=true`; the flag (not the marker) is the
authoritative signal and survives persist+export.

**Expected Result:** Pass when the value is valid UTF-8, bounded, marked, and the
structured flag is true end-to-end.

---

### TC-SQT-06: null source digest stays null under opt-in (boundary)

**Rule:** SQT-R001, SQT-R003

**Given:** `collect_normalized_query_text=true` and the NULL-digest overflow row
(source `DIGEST_TEXT` is `null`).

**When:** redaction runs.

**Then:** `DIGEST_TEXT` stays `null` and `digest_text_truncated=false` (nothing
fabricated).

**Expected Result:** Pass when a null source is preserved as null, untruncated.

---

### TC-SQT-07: capability states do not collapse (invalid/degenerate inputs)

**Rule:** SQT-R006, INV-SQT-05

**Given:** four snapshots: (a) opt-in off; (b) opt-in on with hot rows; (c)
opt-in on with zero digest rows; (d) engine/version off the qualified matrix.

**When:** snapshot provenance/metadata is read.

**Then:** the states resolve distinctly to **disabled**, **enabled/evidence**,
**enabled/no-evidence**, **unsupported** — none reported as a plain empty
success.

**Expected Result:** Pass when all four states are distinguishable.

---

### TC-SQT-08: no literal leakage across the engine matrix (failure / adversarial)

**Rule:** INV-SQT-01, SQT-R009

**Given:** `collect_normalized_query_text=true` and a live server on each of
MySQL 8.0/8.4/9.7 and MariaDB 10.11/11.4/11.8/12.3, after executing statements
embedding string (`'secret'`), numeric (`42`), `IN (1,2,3)` list, and
`/* comment */` literals.

**When:** the digest collector runs and the retained `DIGEST_TEXT` is inspected.

**Then:** the retained text contains `?` placeholders and **none** of the
injected literal values (`secret`, `42`, `1`, `2`, `3`) on any line.

**Expected Result:** Pass only when every engine line is literal-free; any
occurrence of an injected literal is a hard FAIL — that line is withheld
(unsupported), never shipped with a weakened test.

---

### TC-SQT-09: MariaDB reuses the MySQL path unless the matrix proves otherwise (normal)

**Rule:** SQT-R008

**Given:** MariaDB and MySQL digest collectors with identical server-normalized
`DIGEST_TEXT` semantics (as proven by TC-SQT-08).

**When:** the structural-query retention runs for each.

**Then:** both use the same retention/truncation implementation while keeping
distinct engine identities and separate qualification legs; no MariaDB-specific
structural path exists absent a matrix-proven need.

**Expected Result:** Pass when MariaDB exercises the shared implementation and no
divergent path was introduced without justification.

---

### TC-SQT-10: SingleStore is not auto-enabled by the shared path (boundary / security)

**Rule:** SQT-R010

**Given:** the SingleStore plancache collector `singlestore_plancache_v1`
(`QUERY_TEXT` from `MV_PLANCACHE`) and `collect_normalized_query_text=true`.

**When:** redaction and bounding run.

**Then:** `QUERY_TEXT` is NOT in the structural registry, stays governed by the
generic high-sensitivity switch (redacted to `null` when it is off), and no
`digest_text_truncated` flag is stamped. MySQL-wire compatibility does not
enable SingleStore; that requires independent SingleStore qualification.

**Expected Result:** Pass when SingleStore structural evidence is not surfaced
via the MySQL/MariaDB opt-in.
