# Acceptance Tests: singlestore-engine-identity

## Feature

`specifications/singlestore-engine-identity.md` — pure engine-identity
resolution for MySQL-wire servers (SingleStore vs MySQL), fail-closed.

## Test Cases

### TC-SSID-01: SingleStore via `@@memsql_version` (normal)

**Rule:** SS-ID-R001, INV-SSID-01, INV-SSID-02

**Scenario:** A SingleStore server exposes `@@memsql_version`.

**Given:**
- `Probe{MemsqlVersion: "8.5.22", VersionComment: "SingleStore", Version: "5.7.32"}`.

**When:**
- `Resolve(probe)` runs.

**Then:**
- engine == `singlestore`.
- version == `8.5.22` (the `@@memsql_version`, not the MySQL-compat `5.7.32`).
- error is nil.

**Expected Result:** Pass when the SingleStore-specific version wins.

---

### TC-SSID-02: MySQL, no SingleStore signal (boundary — must not say singlestore)

**Rule:** SS-ID-R005, INV-SSID-01

**Scenario:** A stock MySQL 8.0 server.

**Given:**
- `Probe{MemsqlVersion: "", VersionComment: "MySQL Community Server - GPL", Version: "8.0.36"}`.

**When:**
- `Resolve(probe)` runs.

**Then:**
- engine == `mysql`.
- engine != `singlestore`.
- engine != `mariadb`.
- error is nil.

**Expected Result:** Pass when a MySQL-compatible version with no
SingleStore signal resolves to `mysql` and never `singlestore`/`mariadb`.

---

### TC-SSID-07: MariaDB, no SingleStore signal (boundary — distinct from MySQL)

**Rule:** SS-ID-R003, INV-SSID-01

**Scenario:** A stock MariaDB server.

**Given:**
- `Probe{MemsqlVersion: "", VersionComment: "mariadb.org binary distribution", Version: "10.11.6-MariaDB"}`.

**When:**
- `Resolve(probe)` runs.

**Then:**
- engine == `mariadb`.
- engine != `mysql`.
- engine != `singlestore`.
- error is nil.

**Expected Result:** Pass when a MariaDB server resolves to the distinct
`mariadb` engine, never folding into `mysql` or `singlestore`.

---

### TC-SSID-03: SingleStore via version_comment only (boundary)

**Rule:** SS-ID-R002, INV-SSID-01

**Scenario:** A SingleStore server where `@@memsql_version` was not read,
but `@@version_comment` names SingleStore.

**Given:**
- `Probe{MemsqlVersion: "", VersionComment: "SingleStore Managed Service", Version: "5.7.32"}`.

**When:**
- `Resolve(probe)` runs.

**Then:**
- engine == `singlestore`.
- error is nil.

**Expected Result:** Pass when the SingleStore comment is honored even
without `@@memsql_version`.

---

### TC-SSID-04: Indeterminate probe (invalid — fail closed)

**Rule:** SS-ID-R004, INV-SSID-03

**Scenario:** A bare version string with no recognizable vendor signal.

**Given:**
- `Probe{MemsqlVersion: "", VersionComment: "", Version: "5.7.32"}`.

**When:**
- `Resolve(probe)` runs.

**Then:**
- a non-nil error is returned.
- engine is empty (no guess).

**Expected Result:** Pass when an unrecognizable server is an error, never
a guessed engine.

---

### TC-SSID-05: Empty probe (failure — unreachable / no variables read)

**Rule:** SS-ID-R004, INV-SSID-03

**Scenario:** None of the probe variables could be read.

**Given:**
- `Probe{}` (all fields empty).

**When:**
- `Resolve(probe)` runs.

**Then:**
- a non-nil error is returned (`indeterminate engine identity`).
- engine is empty.

**Expected Result:** Pass when an empty probe fails closed.

---

### TC-SSID-06: memsql_version wins over a MySQL-looking comment (invariant)

**Rule:** SS-ID-R001, INV-SSID-02

**Scenario:** A SingleStore server that also reports a MySQL-ish
`VERSION()`; `@@memsql_version` is present.

**Given:**
- `Probe{MemsqlVersion: "8.1.0", VersionComment: "MySQL Community Server - GPL", Version: "5.7.32"}`.

**When:**
- `Resolve(probe)` runs.

**Then:**
- engine == `singlestore` (never `mysql`, per INV-SSID-02).
- version == `8.1.0`.

**Expected Result:** Pass when `@@memsql_version` overrides a misleading
MySQL comment.
