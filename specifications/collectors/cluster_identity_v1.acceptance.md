# Acceptance Tests: cluster_identity_v1

## Feature

`specifications/collectors/cluster_identity_v1.md`

## Test Cases

### TC-CLUSTERID-01: Single row emitted on a normal TCP connection

**Rule:** Normal

**Scenario:** Collector runs against a primary cluster reached over
TCP with the standard signals read-only role.

**Given:**
- A PostgreSQL primary reachable on `127.0.0.1:5432`.
- Connection via TCP (not unix socket).

**When:**
- A collection cycle runs.

**Then:**
- Exactly one row is emitted.
- `inet_server_addr` is non-NULL and resolves to the bound address.
- `inet_server_port` equals the server's bound port.
- `is_in_recovery = false`.
- `server_timezone` is non-empty.
- `last_wal_receive_lsn IS NULL` and `last_wal_replay_lsn IS NULL`
  (primary).
- `postmaster_start_time` is non-NULL and ≤ `now()`.
- Collector `status = success`.

**Expected Result:** Pass when the row is emitted on a primary.

---

### TC-CLUSTERID-02: Unix-socket connection yields NULL inet fields

**Rule:** Failure condition — FC-01

**Scenario:** The collector connects via unix socket
(e.g. `host=/var/run/postgresql`), which is a common operator
configuration on bare-metal deployments.

**Given:**
- A PostgreSQL primary listening on a unix socket.
- Connection string targets the socket path, not a TCP host.

**When:**
- A collection cycle runs.

**Then:**
- `inet_server_addr IS NULL`.
- `inet_server_port IS NULL`.
- The collector does not error.
- Collector `status = success`.
- All other fields (`is_in_recovery`, `server_timezone`,
  `postmaster_start_time`) are populated normally.

**Expected Result:** Pass when unix-socket connections produce NULL
network fields and a success status.

---

### TC-CLUSTERID-03: Standby cluster identified

**Rule:** Normal — replica path

**Scenario:** Collector runs against a streaming replica.

**Given:**
- A PostgreSQL standby with streaming replication configured against
  a primary.
- The replica has received and replayed at least one WAL record from
  the primary.

**When:**
- A collection cycle runs.

**Then:**
- `is_in_recovery = true`.
- `last_wal_receive_lsn` is a non-NULL `pg_lsn`.
- `last_wal_replay_lsn` is a non-NULL `pg_lsn`.
- `last_wal_replay_lsn <= last_wal_receive_lsn` (replay never leads
  receive).
- Collector `status = success`.

**Expected Result:** Pass when the collector emits the expected
replica fingerprint.

---

### TC-CLUSTERID-04: cluster_name unset coalesced to NULL

**Rule:** Normal — coalescing rule

**Scenario:** Operator has not configured `cluster_name` GUC (default).

**Given:**
- `current_setting('cluster_name')` returns the empty string `''`.

**When:**
- A collection cycle runs.

**Then:**
- `cluster_name IS NULL` (not an empty string).

**Expected Result:** Pass when the empty-string default is coalesced
to NULL for cleaner downstream handling.

---

### TC-CLUSTERID-05: Deterministic single-row output

**Rule:** Invariant

**Scenario:** Two consecutive cycles against an unchanged cluster.

**Given:**
- No restart, role change, or GUC change between cycles.

**When:**
- Two collection cycles run with no intervening side effects.

**Then:**
- Both cycles emit exactly one row.
- All columns except `last_wal_receive_lsn` and `last_wal_replay_lsn`
  are byte-identical between the two rows.
  (The two LSN fields advance on a primary with active write
  traffic; on a fully idle cluster they may also be byte-identical.)
- `postmaster_start_time` is identical (no restart).

**Expected Result:** Pass when the deterministic invariants hold.
