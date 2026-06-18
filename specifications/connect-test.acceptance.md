# Acceptance Tests: `signalsctl connect test`

## Feature

`specifications/connect-test.md`

## Test Cases

### TC-CONN-01: OK on a reachable, safe target

**Rule:** Normal — happy path

**Scenario:** Operator runs against a healthy target whose role is
not superuser / replication / bypassrls.

**Given:**
- A target reachable on `host:port`.
- The configured role passes `collector.ValidateRoleSafety`.

**When:**
- `signalsctl connect test <target>` is run.

**Then:**
- Output contains a single line beginning with `OK`.
- The line names `host`, `port`, `dbname`, `username`, and PG version.
- Exit code is `0`.

---

### TC-CONN-02: TCP category on connection refused

**Rule:** Failure condition — TCP layer

**Scenario:** The target's host:port is reachable at DNS level but
nothing is listening (firewall drop or instance down).

**Given:**
- Target configured at `127.0.0.1:9` (discard port — refused).

**When:**
- `signalsctl connect test <target>` is run.

**Then:**
- Output line begins with `FAIL`.
- The classification token is `tcp`.
- Detail mentions "refused" or the underlying syscall error.
- Exit code is `1`.

---

### TC-CONN-03: Auth category on PG SQLSTATE 28P01

**Rule:** Failure condition — auth

**Scenario:** Target reachable, password is wrong.

**Given:**
- A target reachable on TCP whose configured password is invalid.

**When:**
- `signalsctl connect test <target>` is run.

**Then:**
- Output line begins with `FAIL`.
- The classification token is `auth`.
- Detail mentions `SQLSTATE 28P01` or `authentication failed`.
- Detail does NOT contain the password value (INV-CONN-02).
- Exit code is `1`.

---

### TC-CONN-04: password_resolve category when env var is unset

**Rule:** Failure condition — password resolution

**Scenario:** Target's config references `password_env` but the
environment variable is not set when the CLI runs.

**Given:**
- Target config carries `password_env: SIGNALS_DOES_NOT_EXIST`.
- The env var is not set.

**When:**
- `signalsctl connect test <target>` is run.

**Then:**
- Output line begins with `FAIL`.
- The classification token is `password_resolve`.
- Detail names the env var so the operator knows what to set.
- No TCP connection is attempted (no entry in dial log).
- Exit code is `1`.

---

### TC-CONN-05: --dsn ad-hoc mode bypasses config

**Rule:** Normal — ad-hoc usage

**Scenario:** Operator wants to test a connection before adding it
to config.

**Given:**
- A reachable target.
- No mention of it in `signals.yaml`.

**When:**
- `signalsctl connect test --dsn "host=localhost port=5432 dbname=postgres user=monitor sslmode=disable"` is run.

**Then:**
- The connection is attempted.
- Exit code reflects the outcome (0 on OK, 1 on any failure).
- The result is NOT persisted anywhere (read-only tool).

---

### TC-CONN-06: --dsn + <target-name> is a usage error

**Rule:** Failure condition — FC-CONN-01

**Scenario:** Operator supplies both modes by accident.

**Given:**
- Both a positional `<target-name>` and `--dsn` on the command line.

**When:**
- `signalsctl connect test foo --dsn "host=... port=..."` is run.

**Then:**
- No connection is attempted.
- Exit code is `2`.
- Stderr explains the conflict.

---

### TC-CONN-07: JSON mode emits the documented wire shape

**Rule:** Invariant — output contract

**Scenario:** A CI gate consumes `--json` output.

**Given:**
- Any valid target (the exact health state does not matter).

**When:**
- `signalsctl connect test --json [<target>]` is run.

**Then:**
- Output is a single JSON object parseable by `encoding/json`.
- Required keys: `schema_version`, `generated_at`, `attempts` (array), `summary` (object with integer `ok`, `fail`).
- Each `attempts[i]` carries `target`, `category` (lowercase enum),
  `detail`, `host`, `port`, `dbname`, `username`.
- `category` is one of `ok | dns | tcp | tls | auth | startup | role | password_resolve | config`.

---

### TC-CONN-08: Classification deterministic across runs

**Rule:** Invariant — INV-CONN-03

**Scenario:** Same input twice → same category.

**Given:**
- A target that produces a specific failure (e.g. TCP refused).

**When:**
- The command is run twice back to back.

**Then:**
- The `category` field is identical in both runs.
- `detail` may differ in elapsed-time figures but the classification
  is byte-stable.

---

### TC-CONN-09: Multi-target output order pinned to config order

**Rule:** Invariant — INV-CONN-04

**Scenario:** Operator runs `signalsctl connect test` (no args, no
`--dsn`) against a config with three targets `alpha`, `bravo`,
`charlie`.

**Given:**
- Targets declared in that order in `signals.yaml`.

**When:**
- The command is run.

**Then:**
- Output lines appear in `alpha`, `bravo`, `charlie` order regardless
  of which connection finishes first.
- JSON `attempts[]` array preserves the same order.

---

### TC-CONN-10: Password value never appears in any output channel

**Rule:** Invariant — INV-CONN-02

**Scenario:** A target's password contains a recognisable sentinel.

**Given:**
- Config with `password_env: SIGNALS_TEST_SENTINEL`.
- The env var set to `SENTINEL-conntest-leak-probe-789`.

**When:**
- `signalsctl connect test <target>` is run against an unreachable port
  (forces the dial-failure path that exercises error formatting).

**Then:**
- The sentinel value does NOT appear in stdout.
- The sentinel value does NOT appear in stderr.
- The sentinel value does NOT appear in the `--json` output.
