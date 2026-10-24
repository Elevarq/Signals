# Acceptance Tests: singlestore-connection-hardening

## Feature

`specifications/singlestore-connection-hardening.md` — SingleStore TLS CA
verification (#585), per-collector query timeout (#586), and environment engine
selection (#587), with PostgreSQL behavior unchanged.

## Test Cases

### TC-SCH-01: verify-full trusts the configured CA (normal)

**Rule:** SCH-R001

**Given:** a local TLS listener presenting a cert for `localhost` signed by a
temporary test CA, and `BuildTLSConfig(caFile, "localhost", true)`.

**When:** a TLS client dials the listener with that config.

**Then:** the handshake succeeds.

**Expected Result:** Pass when the trusted-CA + matching-hostname handshake
completes.

---

### TC-SCH-02: verify-full rejects an untrusted CA and a hostname mismatch (failure)

**Rule:** SCH-R001, SCH-R003

**Given:** the same listener, but (a) a `BuildTLSConfig` built from a DIFFERENT
(untrusted) CA, and (b) `BuildTLSConfig(caFile, "wrong.example", true)`.

**When:** a client dials in each case.

**Then:** both handshakes fail; neither downgrades to plaintext.

**Expected Result:** Pass when both verified dials fail.

---

### TC-SCH-03: verify-ca verifies the chain but ignores hostname (boundary)

**Rule:** SCH-R002

**Given:** the listener (cert for `localhost`) and
`BuildTLSConfig(caFile, "wrong.example", false)`.

**When:** a client dials.

**Then:** the handshake SUCCEEDS (chain trusted; hostname not checked); but with
an untrusted CA it still FAILS.

**Expected Result:** Pass when verify-ca accepts a hostname mismatch under a
trusted CA and rejects an untrusted CA.

---

### TC-SCH-04: bad CA material fails closed (failure)

**Rule:** SCH-R003

**Given:** a non-existent CA path, and a file containing no PEM certificate.

**When:** `BuildTLSConfig` is called (and `ConnConfig.Open`-level resolution).

**Then:** an error is returned; no `*tls.Config` / no usable handle; never a
plaintext/unverified fallback.

**Expected Result:** Pass when both inputs error.

---

### TC-SCH-05: ConnConfig maps sslmode + CA onto the wire TLS fields (normal)

**Rule:** SCH-R001, SCH-R002

**Given:** `TargetConfig`s with `sslmode` disable/require/verify-ca/verify-full
and an `sslrootcert_file`.

**When:** `singleStoreConnConfig` builds the `ConnConfig`.

**Then:** verify-ca/verify-full set `TLSRootCAFile` to the CA path;
`TLSVerifyHostname` is true only for verify-full; disable→`false`,
require→`true`, default→`preferred`.

**Expected Result:** Pass when the mapping matches.

---

### TC-SCH-06: per-collector query timeout cancels at query_timeout (failure)

**Rule:** SCH-R004, SCH-R007

**Given:** a bounded runner wrapping a fake runner that blocks until its context
is canceled, with `query_timeout = 10ms`.

**When:** the bounded runner runs a query.

**Then:** it returns a deadline/timeout error within a small multiple of 10ms,
and `classifyRunError` maps that error to reason `timeout`.

**Expected Result:** Pass when the bound is enforced and classified as timeout.

---

### TC-SCH-07: later collectors run after a timed-out collector (normal)

**Rule:** SCH-R004

**Given:** a catalog where the first collector times out and the rest return
promptly, run through the bounded runner under a live target context.

**When:** `singlestore.Collect` runs the catalog.

**Then:** the first result carries the timeout error; subsequent results carry
their rows — the cycle is not aborted.

**Expected Result:** Pass when a single collector timeout does not abort the
cycle.

---

### TC-SCH-08: failed SingleStore run carries a canonical reason (normal)

**Rule:** SCH-R007

**Given:** SingleStore results: one with a timeout error (non-elevated), one with
a generic error, one elevated with a permission-denied error.

**When:** `mapSingleStoreCollection` maps them.

**Then:** the timeout run is `failed`/`timeout`; the generic run is
`failed`/`execution_error`; the elevated run is `skipped`/`permission_denied`.

**Expected Result:** Pass when reasons are canonical and the elevated degrade is
preserved.

---

### TC-SCH-09: env SIGNALS_TARGET_ENGINE sets the engine; omission is PostgreSQL (boundary)

**Rule:** SCH-R006

**Given:** `SIGNALS_TARGET_HOST` set, with (a) `SIGNALS_TARGET_ENGINE=singlestore`
and (b) `SIGNALS_TARGET_ENGINE` unset.

**When:** the env config is loaded.

**Then:** (a) the env target's `EffectiveEngine()` is `singlestore` and the
default port is 3306 when `SIGNALS_TARGET_PORT` is unset; (b) `EffectiveEngine()`
is `postgresql` and the default port is 5432.

**Expected Result:** Pass when the env engine parity + engine-aware port hold.

---

### TC-SCH-10: documentation agrees with config (normal)

**Rule:** SCH-R006

**Given:** `examples/singlestore-daemon.yaml`.

**When:** its env-config guidance is read.

**Then:** it does not claim the env path cannot set the engine; it documents an
env-only SingleStore target with `SIGNALS_TARGET_ENGINE=singlestore` and an
explicit SingleStore port; and it states that omitting the engine is PostgreSQL
compatibility.

**Expected Result:** Pass (doc review) when the example matches the behavior.
