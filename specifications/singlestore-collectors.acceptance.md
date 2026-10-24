# Acceptance Tests: singlestore-collectors

## Feature

`specifications/singlestore-collectors.md` — validated SingleStore collector
catalog + runner, read-only, redaction-aware.

## Test Cases

### TC-SSC-01: Collect runs the catalog and captures rows (normal)

**Rule:** SSC-R002

**Given:** a fake `QueryRunner` returning one row for the identity and
aggregators collectors.

**When:** `Collect(ctx, fake, cat, false)` runs.

**Then:** a `Result` exists per catalog entry; the identity and aggregators
results carry their rows; no `Err` is set for those.

**Expected Result:** Pass when every collector produces a result.

---

### TC-SSC-02: A failing collector does not abort the others (boundary)

**Rule:** SSC-R002

**Given:** a fake `QueryRunner` that errors for one collector and returns rows
for the rest.

**When:** `Collect` runs.

**Then:** the failing collector's `Result.Err` is non-empty; the other
collectors still return their rows.

**Expected Result:** Pass when partial collection is captured, not fatal.

---

### TC-SSC-03: Sensitive query text is redacted on opt-out (invariant)

**Rule:** SSC-R003, INV-SSC-02

**Given:** a fake `QueryRunner` returning a plancache row with a non-nil
`QUERY_TEXT`.

**When:** `Collect(ctx, fake, cat, true)` runs with redaction enabled.

**Then:** the plancache result's `QUERY_TEXT` is nil in every row; other
columns (e.g. `PLAN_ID`) survive.

**Expected Result:** Pass when the high-sensitivity column is redacted.

---

### TC-SSC-04: Catalog shape (structural)

**Rule:** SSC-R001, INV-SSC-02

**Given:** `Catalog()`.

**When:** inspected.

**Then:** every entry's SQL begins with `SELECT`; `singlestore_plancache_v1`
has `HighSensitivity=true` and lists `QUERY_TEXT` in `SensitiveColumns`.

**Expected Result:** Pass when the catalog is read-only and the plancache
collector is correctly flagged.

---

### TC-SSC-LIVE: Live catalog validation (integration — build-tag guarded)

**Rule:** SSC-R001, SSC-R002

**Given:** a reachable SingleStore instance (`SINGLESTORE_TEST_DSN`, tag
`singlestore_integration`; skipped when unset).

**When:** the real `Catalog()` runs via `NewRunner` against the live instance.

**Then:** no collector returns a fatal error; `singlestore_identity_v1`
reports a non-empty `@@memsql_version`.

**Expected Result:** Pass against the SingleStore workspace; skipped when no
instance is configured. This is how the catalog SQL/columns were validated.
