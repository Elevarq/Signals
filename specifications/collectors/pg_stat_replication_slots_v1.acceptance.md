# Acceptance Tests: pg_stat_replication_slots_v1

## Feature

`specifications/collectors/pg_stat_replication_slots_v1.md`

## Test Cases

### TC-RSLOTS-01: Empty rowset on instance with no logical slots

**Rule:** Normal — empty path

**Scenario:** Collector runs against a PostgreSQL instance that has
no logical replication slots configured. This is the common case
for the majority of installations.

**Given:**
- A PostgreSQL primary on PG 14+ with no entries in
  `pg_replication_slots`.
- The signals read-only role can `SELECT` from
  `pg_stat_replication_slots`.

**When:**
- A collection cycle runs.

**Then:**
- The collector executes without error.
- Zero rows are emitted (empty rowset is the success state).
- Collector `status = success`.

**Expected Result:** Pass when an empty rowset returns and the
collector reports success — not a failure or skip.

---

### TC-RSLOTS-02: One row per logical slot on a cluster with active logical replication

**Rule:** Normal — populated path

**Scenario:** A primary has two logical replication slots configured
(e.g. one for a CDC pipeline, one for an audit shipper).

**Given:**
- `pg_replication_slots` lists two `logical`-type slots.
- Both slots have been used (decoding has run at least once since
  cluster boot, so `total_txns > 0` on at least one).

**When:**
- A collection cycle runs.

**Then:**
- Exactly two rows are emitted.
- Each row carries a non-empty `slot_name`.
- Cumulative counter columns (`spill_txns`, `spill_count`,
  `spill_bytes`, `stream_txns`, `stream_count`, `stream_bytes`,
  `total_txns`, `total_bytes`) are non-negative `bigint` values.
- Output is ordered deterministically by `slot_name`.
- Collector `status = success`.

**Expected Result:** Pass when one row per logical slot is emitted
with cumulative counters intact.

---

### TC-RSLOTS-03: stats_reset NULL on a fresh cluster

**Rule:** Failure condition — degrade-gracefully invariant

**Scenario:** A cluster that has just been initialised has a slot
created but the stats subsystem has not been reset since boot.

**Given:**
- `pg_stat_replication_slots.stats_reset IS NULL` for the slot.

**When:**
- A collection cycle runs.

**Then:**
- The collector emits the row.
- The `stats_reset` column carries SQL NULL (the collector does not
  substitute a sentinel or error out).
- Collector `status = success`.

**Expected Result:** Pass when NULL `stats_reset` is propagated
unchanged.

---

### TC-RSLOTS-04: Collector excluded on PG < 14

**Rule:** Failure condition — FC-01

**Scenario:** Catalog filter runs on an instance reporting
`server_version_num < 140000`.

**Given:**
- The target's reported `PGMajorVersion = 13`.

**When:**
- `pgqueries.Filter(...)` is asked which collectors apply.

**Then:**
- `pg_stat_replication_slots_v1` is **not** in the returned set.
- The collector's status in `collector_status.json` is `skipped`
  with `reason = version_unsupported` per existing EA-R001 / R081
  dispatch.

**Expected Result:** Pass when the version gate keeps the collector
out of the catalog on PG 13 and below, with the standard skipped
reason surfaced.

---

### TC-RSLOTS-05: Cumulative counters survive consecutive cycles

**Rule:** Invariant — monotonic between resets

**Scenario:** Two consecutive cycles against the same cluster with
ongoing logical-decoding traffic.

**Given:**
- `pg_stat_replication_slots.stats_reset` does not change between
  the two cycles (no operator reset, no cluster restart).
- Decoding traffic flows between the cycles.

**When:**
- Two collection cycles run.

**Then:**
- For every slot present in both cycles:
  `total_txns_cycle2 >= total_txns_cycle1`,
  `total_bytes_cycle2 >= total_bytes_cycle1`,
  and the same monotonic relation holds for `spill_*` and
  `stream_*` counters.
- The slot's `slot_name` is byte-identical across cycles.

**Expected Result:** Pass when the across-cycle invariant holds —
analyzer-side delta computation is safe.
