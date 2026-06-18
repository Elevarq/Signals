# Acceptance Tests: `signalsctl doctor`

## Feature

`specifications/doctor.md`

## Test Cases

### TC-DOC-01: All checks pass on a healthy config

**Rule:** Normal — happy path

**Scenario:** Operator runs `signalsctl doctor` against a valid config
with one reachable target whose role passes safety validation.

**Given:**
- Config file at the configured path is valid YAML and passes
  `ValidateStrict`.
- The configured SQLite store directory exists and is writable.
- The configured target accepts TCP on its declared host:port.
- The target's role is not superuser / replication / bypassrls.

**When:**
- `signalsctl doctor` is run.

**Then:**
- All four checks (C1..C4) report `OK`.
- Exit code is `0`.
- The summary line reports `4 OK, 0 WARN, 0 FAIL`.

**Expected Result:** Pass when a healthy deployment yields all-OK.

---

### TC-DOC-02: Config file missing fails C1 and downgrades dependents

**Rule:** Failure condition — FC-DOC-01

**Scenario:** Operator runs doctor pointing at a config path that does
not exist.

**Given:**
- `--config /nonexistent/path.yaml` is supplied.

**When:**
- `signalsctl doctor --config /nonexistent/path.yaml` is run.

**Then:**
- `C1=FAIL` with a detail mentioning the path.
- `C2`, `C3`, `C4` emit `WARN` with detail "skipped (config_valid failed)".
- Exit code is `1`.

**Expected Result:** Pass when a missing config short-circuits
downstream checks via WARN rather than FAIL, keeping the failure
attributable to the root cause.

---

### TC-DOC-03: Unreachable target fails C3 and downgrades C4

**Rule:** Failure condition — FC-DOC-03

**Scenario:** A target's host:port is unreachable (e.g. firewall
drop, instance down). Other targets remain reachable.

**Given:**
- A config with two enabled targets. One points at a port nothing is
  listening on (e.g. `127.0.0.1:9`). The other is healthy.

**When:**
- `signalsctl doctor` is run.

**Then:**
- `C1=OK`, `C2=OK`.
- `C3` emits two entries: `FAIL` for the unreachable target,
  `OK` for the healthy one.
- `C4` for the unreachable target emits `WARN` with detail
  "skipped (target_reachable failed)".
- `C4` for the healthy target emits `OK` (assuming role is safe).
- Exit code is `1` (one FAIL).

**Expected Result:** Pass when per-target failures are isolated
and dependent checks degrade to WARN.

---

### TC-DOC-04: `--json` produces a parseable JSON object

**Rule:** Invariant — output contract

**Scenario:** A caller (CI gate, monitoring agent) needs machine-
readable output.

**Given:**
- Any valid config; the exact health state does not matter for this
  test.

**When:**
- `signalsctl doctor --json` is run.
- The stdout output is parsed as JSON.

**Then:**
- The output is a single JSON object.
- It has keys: `schema_version`, `generated_at`, `checks`, `summary`.
- `checks` is a non-empty array; each element has `id`, `name`,
  `target`, `status`, `detail`, `duration_ms`.
- `summary` has integer keys `ok`, `warn`, `fail`.
- `status` values are one of `"ok"`, `"warn"`, `"fail"` (lowercase).

**Expected Result:** Pass when the JSON contract holds shape and
field types regardless of underlying check outcomes.

---

### TC-DOC-05: Unknown `--check` name exits with usage error

**Rule:** Failure condition — FC-DOC-05

**Scenario:** Operator misspells a check ID.

**Given:**
- The valid check IDs are C1, C2, C3, C4.

**When:**
- `signalsctl doctor --check=C9` is run.

**Then:**
- The command does not run any check.
- Exit code is `2`.
- Stderr contains a diagnostic listing the supported check IDs.

**Expected Result:** Pass when unknown check names are rejected
at argument parse time, not silently ignored or treated as failures.

---

### TC-DOC-07: C5 reports per-target available / missing collector counts

**Rule:** Normal — C5 happy path

**Scenario:** A reachable target with `pg_stat_statements` installed.

**Given:**
- Target reachable on TCP.
- Role passes role-safety.
- `pg_stat_statements` extension is created in the target database.

**When:**
- `signalsctl doctor` is run.

**Then:**
- C5 emits OK.
- Detail names a non-zero `available` count.
- Detail lists zero entries under `extension_missing` for
  `pg_stat_statements` specifically.

---

### TC-DOC-08: C5 reports `extension_missing` for an absent extension

**Rule:** Normal — C5 missing-extension path

**Scenario:** Target reachable but `pg_stat_statements` is not
installed.

**Given:**
- Target reachable on TCP.
- Role passes role-safety.
- `pg_stat_statements` is **not** installed.

**When:**
- `signalsctl doctor` is run.

**Then:**
- C5 emits WARN.
- Detail names `pg_stat_statements` under the missing extensions
  list.
- Other collectors remain in the `available` count — partial
  coverage is informational.

---

### TC-DOC-09: C5 emits WARN when upstream C4 failed (FC-DOC-06)

**Rule:** Failure condition — dependency

**Scenario:** Target has bad credentials so C4 fails.

**Given:**
- Target reachable on TCP (C3 OK).
- Role validation fails because the connection fails earlier (C4 FAIL).

**When:**
- `signalsctl doctor` is run.

**Then:**
- C5 emits WARN with detail `skipped (target_reachable / role_safe failed)`.
- C5 does NOT FAIL — C4 is already reporting the root cause.

---

### TC-DOC-10: C6 OK when freshness within 2x poll_interval

**Rule:** Normal — C6 happy path

**Scenario:** Daemon has been running and has a recent snapshot
for the target.

**Given:**
- `poll_interval` configured at 60s.
- Daemon's SQLite store has a snapshot for target `prod-db`
  collected 45s ago.

**When:**
- `signalsctl doctor --check=C6` is run.

**Then:**
- C6 emits OK.
- Detail names the age (e.g. `45s ago, poll_interval=60s`).

---

### TC-DOC-11: C6 WARN when freshness exceeds 2x poll_interval

**Rule:** Normal — C6 staleness path

**Scenario:** Target hasn't produced a snapshot in a long time.

**Given:**
- `poll_interval` configured at 60s.
- Daemon's SQLite store has a snapshot for `prod-db`
  collected 5 minutes ago (5× interval).

**When:**
- `signalsctl doctor` is run.

**Then:**
- C6 emits WARN.
- Detail names the age and the threshold breached.

---

### TC-DOC-12: C6 WARN when store unreadable (FC-DOC-07)

**Rule:** Failure condition — pre-daemon

**Scenario:** Operator runs doctor before the daemon has ever
booted. Store file does not exist.

**Given:**
- Configured `database.path` does not point at a valid SQLite file.

**When:**
- `signalsctl doctor --check=C6` is run.

**Then:**
- C6 emits WARN with detail beginning `store unreadable:`.
- C6 does NOT FAIL — pre-daemon runs are a valid case.

---

### TC-DOC-06: Credentials never appear in output

**Rule:** Invariant — INV-DOC-03

**Scenario:** A target's password is resolved during the doctor run.
The password must not appear in stdout, stderr, JSON output, or any
error detail string — even when the connection fails.

**Given:**
- A target configured with `password_env=SIGNALS_TEST_DOCTOR_PASSWORD`
  set to a unique value like `S3kRet-doctor-test`.
- The host is unreachable (forces a failure path that touches
  connection error formatting).

**When:**
- `signalsctl doctor` is run with the env var present.

**Then:**
- The password value `S3kRet-doctor-test` appears nowhere in
  stdout or stderr.
- `RedactDSN` (or equivalent) is used for any connection-string
  echoing.

**Expected Result:** Pass when credential material is absent from
every output channel under all failure modes.
