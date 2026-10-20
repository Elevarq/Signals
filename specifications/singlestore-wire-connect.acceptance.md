# Acceptance Tests: singlestore-wire-connect

## Feature

`specifications/singlestore-wire-connect.md` — shared MySQL-wire connector +
engine detection (delegates identity to `engineid`).

## Test Cases

### TC-WIRE-01: BuildDSN defaults (normal)

**Rule:** WIRE-R001, WIRE-R002, WIRE-R005

**Given:** `ConnConfig{Host: "db.local", User: "signals_ro", Password: "s3cret"}`.

**When:** `BuildDSN(cfg)` runs.

**Then:** the DSN addresses `tcp(db.local:3306)` (default port), parses back
with `InterpolateParams == false` (the injection-safe default; `FormatDSN`
omits default-valued params, so this is verified by parsing, not substring),
contains `tls=preferred` (default), and carries the `signals_ro` user.

**Expected Result:** Pass when defaults and the injection-safe param are present.

---

### TC-WIRE-02: BuildDSN explicit port + TLS (boundary)

**Rule:** WIRE-R001, WIRE-R005

**Given:** `ConnConfig{Host: "ss.example", Port: 3307, User: "u", TLS: "skip-verify"}`.

**When:** `BuildDSN(cfg)` runs.

**Then:** the DSN addresses `tcp(ss.example:3307)` and contains `tls=skip-verify`.

**Expected Result:** Pass when explicit port and TLS key are honored.

---

### TC-WIRE-03: Identify SingleStore via probe (normal)

**Rule:** WIRE-R003, INV-WIRE-03 (→ SS-ID-R001)

**Given:** a fake `ScalarQuery` returning `"8.5.22"` for `@@memsql_version`,
`"SingleStore"` for `@@version_comment`, `"5.7.32"` for `VERSION()`.

**When:** `Identify(ctx, fake)` runs.

**Then:** engine == `singlestore`, version == `8.5.22`, error is nil.

**Expected Result:** Pass when the live probe resolves SingleStore.

---

### TC-WIRE-04: Identify MySQL, @@memsql_version rejected (boundary)

**Rule:** WIRE-R003, INV-WIRE-03 (→ SS-ID-R003)

**Given:** a fake `ScalarQuery` that **errors** on `@@memsql_version` (unknown
variable), returns `"MySQL Community Server - GPL"` for `@@version_comment`
and `"8.0.36"` for `VERSION()`.

**When:** `Identify(ctx, fake)` runs.

**Then:** the `@@memsql_version` error is tolerated; engine == `mysql`;
engine != `singlestore`; error is nil.

**Expected Result:** Pass when a rejected `@@memsql_version` does not fail
detection and the server resolves to MySQL.

---

### TC-WIRE-05: Identify indeterminate (invalid — fail closed)

**Rule:** INV-WIRE-03 (→ SS-ID-R004)

**Given:** a fake `ScalarQuery` that errors on `@@memsql_version` and
`@@version_comment` and returns `"5.7.32"` for `VERSION()` (bare, no vendor
signal).

**When:** `Identify(ctx, fake)` runs.

**Then:** a non-nil error (`engineid.ErrIndeterminate`) is returned; engine is empty.

**Expected Result:** Pass when an unrecognizable server fails closed.

---

### TC-WIRE-06: Redacted DSN hides the password (security)

**Rule:** INV-WIRE-02

**Given:** `ConnConfig{Host: "h", User: "u", Password: "s3cret"}`.

**When:** `Redacted(cfg)` runs.

**Then:** the returned string does **not** contain `s3cret`.

**Expected Result:** Pass when the password never appears in the loggable DSN.

---

### TC-WIRE-07: Live SingleStore detection (integration — build-tag guarded)

**Rule:** WIRE-R003, WIRE-R004, INV-WIRE-03

**Given:** a reachable SingleStore instance described by `SINGLESTORE_TEST_DSN`
(build tag `singlestore_integration`; skipped when unset).

**When:** the connector opens it and `Identify` runs against the live session.

**Then:** engine == `singlestore` and a non-empty version is returned, using
only `SELECT` statements.

**Expected Result:** Pass against the SingleStore Dev Image (#555); skipped
when no instance is configured.
