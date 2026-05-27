# Acceptance Tests: pg_stat_activity_summary_v1

## Feature

`specifications/collectors/pg_stat_activity_summary_v1.md`

## Test Cases

### TC-ACT-01: Single aggregated row with no query text

**Rule:** Normal + privacy invariant

**Scenario:** Target has a mix of active, idle, and
idle-in-transaction sessions.

**Given:**
- At least 10 backends in assorted states.

**When:**
- A collection cycle runs.

**Then:**
- Exactly one row emitted.
- No column in the row contains query text, user names, client
  addresses, or session PIDs.
- `by_backend_type` and `by_wait_event_type` are JSON objects.

**Expected Result:** Pass when the row is aggregated and contains no
per-session identifiers.

---

### TC-ACT-02: Long-running transaction age reflects wall clock

**Rule:** Normal

**Scenario:** A transaction has been open for > 10 minutes.

**Given:**
- One backend with `BEGIN` issued > 10 minutes ago.

**When:**
- A collection cycle runs.

**Then:**
- `oldest_xact_age_seconds ≥ 600`.
- `long_idle_in_txn_count` reflects the state (≥ 1 if the session
  is idle in transaction).

**Expected Result:** Pass when age reporting is accurate to within
one sample interval.

---

### TC-ACT-03: Collector's own backend is excluded from counts

**Rule:** Scope filter

**Scenario:** The collector's session does not self-count.

**Given:**
- Only the collector backend is connected.

**When:**
- A collection cycle runs.

**Then:**
- `total_backends = 0` (client backends only).
- `by_backend_type` may still include 1 for `"client backend"` if
  the implementation exposes the collector backend there — in
  which case the test confirms that only the collector's own PID
  is excluded from the client-backend *state* counts
  (`active_count`, `idle_count`, etc.).

**Expected Result:** Pass when the state counts do not include the
collector's own session.

---

### TC-ACT-04: JSON aggregations never NULL

**Rule:** Invariant

**Scenario:** Even under minimal activity, the JSON fields are
present.

**Given:**
- Target with only the collector backend (minimal activity).

**When:**
- A collection cycle runs.

**Then:**
- `by_backend_type` is a JSON object (possibly with a single
  entry).
- `by_wait_event_type` is a JSON object (possibly `{}`).
- Neither field is NULL or the literal string `"null"`.

**Expected Result:** Pass when both JSON fields are populated objects.
