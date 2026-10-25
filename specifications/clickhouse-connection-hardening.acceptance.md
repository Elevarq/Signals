# Acceptance Tests: clickhouse-connection-hardening

## Feature

`specifications/clickhouse-connection-hardening.md` — the ClickHouse native TLS
path (#558). Verify crypto is covered hermetically by `internal/dbtls`; these
cases cover the ClickHouse sslmode→`*tls.Config` mapping, fail-closed, and the
session wiring.

## Test Cases

### TC-CCH-01: disable/empty sslmode is plaintext (boundary)

**Rule:** CCH-R004

**Given:** a `ConnConfig` with `TLSMode` `disable` or empty. **When:**
`resolveTLS` runs. **Then:** it returns a nil `*tls.Config` (plaintext) and no
error. **Expected:** Pass — explicit opt-out only.

---

### TC-CCH-02: require applies TLS without certificate verification (normal)

**Rule:** CCH-R003

**Given:** `TLSMode=require`. **When:** `resolveTLS` runs. **Then:** it returns a
non-nil `*tls.Config` (encryption applied) with no error. **Expected:** Pass —
TLS-required is never plaintext.

---

### TC-CCH-03: verify-full verifies chain + hostname (normal)

**Rule:** CCH-R001

**Given:** `TLSMode=verify-full` + a valid CA file. **When:** `resolveTLS` runs.
**Then:** a non-nil `*tls.Config` with `ServerName` set to the target host.
**Expected:** Pass.

---

### TC-CCH-04: verify-ca verifies chain, not hostname (normal)

**Rule:** CCH-R002

**Given:** `TLSMode=verify-ca` + a valid CA file. **When:** `resolveTLS` runs.
**Then:** a non-nil `*tls.Config` with no `ServerName` and a custom
`VerifyPeerCertificate` (chain checked, hostname skipped). **Expected:** Pass.
(End-to-end verify behavior is proven hermetically in `internal/dbtls`.)

---

### TC-CCH-05: verify mode fails closed on bad CA (failure)

**Rule:** CCH-R003, INV-CCH-01

**Given:** `TLSMode=verify-full` (or `verify-ca`) with a missing/unparseable CA.
**When:** `clickhouse.Open` runs. **Then:** it returns an error before dialing;
no handle; the connection never falls back to plaintext. **Expected:** Pass —
fail closed.

---

### TC-CCH-06: unknown sslmode is a hard error (failure)

**Rule:** CCH-R003

**Given:** an unrecognized `TLSMode`. **When:** `resolveTLS` runs. **Then:** a
hard error (never a silent plaintext). **Expected:** Pass.

---

### TC-CCH-07: session passes sslmode + CA into the connector (normal)

**Rule:** CCH-R005

**Given:** a ClickHouse target with `SSLMode` + `SSLRootCertFile`. **When:**
`openClickHouse` builds the `ConnConfig`. **Then:** `TLSMode == tgt.SSLMode` and
`TLSRootCAFile == tgt.SSLRootCertFile` — the configuration reaches the driver (the
F1 fix). **Expected:** Pass.
