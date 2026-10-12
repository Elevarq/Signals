# Acceptance Tests: Populated vPREV→vNEXT Upgrade Gate

## Feature

`specifications/populated-upgrade-gate.md` (Elevarq/Signals#504)

## Test harness

The upgrade state semantics are deployment-independent (they live in
`internal/db`), so the primary proof is a single **deep Go lifecycle test**
in `tests/signals_populated_upgrade_test.go` (package `tests`, normal
`go test -race` job — no live PostgreSQL, no cloud resource). It builds a
populated store at an **earlier embedded migration state** (vPREV) via the
real `internal/db` API + `ApplyMigrationSQL` over a strict prefix of
`MigrationFilenames()`, performs the supported upgrade through the real
mechanism (`db.Open` → `store.Migrate()` forward), and asserts the before/
after invariants.

The mapping to supported deploy modes is **structural / template-level**,
reusing the #501 durability guards in `tests/data_durability_test.go`; this
feature adds the container/compose durable-`/data` mapping
(`TestPopulatedUpgrade_ContainerDataPathDurable`). No deep lifecycle proof is
re-run per cloud (R-UPG-05).

The deep lifecycle test is the **upgrade gate**; it runs under the existing
CI `test` gate (`go test -count=1 -race ./...`) alongside the retained
fresh-install / schema-version / durability-continuity tests (R-UPG-06 /
R-UPG-07).

## Test Cases

### TC-UPG-01: The vPREV fixture is a genuine strict-prefix (older) store

**Rule:** R-UPG-01 / B1 — Boundary

**Given:**
- A store built by applying only a strict prefix of `MigrationFilenames()`
  (`[:k]`, `k < len`) via `ApplyMigrationSQL`, then populated.

**When:**
- Its `schema_migrations` ledger is read before upgrade.

**Then:**
- The ledger equals the prefix: it is a strict subset of the embedded set
  (≥1 embedded migration missing) and contains no filename unknown to the
  binary — an authentic older-release store, not fresh and not newer.

**Test:** `TestPopulatedUpgrade_StateSurvivesVPrevToVNext` (pre-upgrade
assertion block).

---

### TC-UPG-02: Instance identity survives the upgrade unchanged

**Rule:** R-UPG-03 / R-UPG-04 / B2 / INV-UPG-01 — Normal

**Given:**
- The vPREV store with `meta.instance_id` set before upgrade.

**When:**
- `db.Open` + `store.Migrate()` forward (the real upgrade mechanism).

**Then:**
- `EnsureInstanceID` returns the same `instance_id` recorded before the
  upgrade — not a regenerated or default identity.

**Test:** `TestPopulatedUpgrade_StateSurvivesVPrevToVNext`.

---

### TC-UPG-03: Collection history is preserved across the upgrade

**Rule:** R-UPG-04 / B3 / INV-UPG-01 — Normal

**Given:**
- The vPREV store populated with N snapshots and their `query_runs` /
  `query_results`.

**When:**
- The upgrade completes.

**Then:**
- `CountSnapshots`, the `query_runs` count for the target, and the
  `query_results` payload are all the pre-upgrade values (no row dropped,
  truncated, or recreated).

**Test:** `TestPopulatedUpgrade_StateSurvivesVPrevToVNext`.

---

### TC-UPG-04: Watermarks survive AND the next collection continues from them

**Rule:** R-UPG-04 / B4 / INV-UPG-01 — Normal (continue-not-restart)

**Given:**
- The vPREV store whose `query_runs` establish a per-collector last-run
  watermark `W`.

**When:**
- The upgrade completes, then a subsequent collection is persisted.

**Then:**
- `GetLastRunTimes` returns `W` for that collector immediately after the
  upgrade (unchanged); and the subsequent collection's `collected_at` is
  strictly after `W` and becomes the new watermark — the next cycle continues
  incrementally rather than restarting history from zero.

**Test:** `TestPopulatedUpgrade_StateSurvivesVPrevToVNext`.

---

### TC-UPG-05: Schema migrated to current; ledger advanced correctly

**Rule:** R-UPG-04 / B5 / INV-UPG-02 — Normal

**Given:**
- The strict-prefix vPREV ledger (prefix rows carry a known `applied_at`).

**When:**
- The upgrade completes.

**Then:**
- The `schema_migrations` ledger equals the full `MigrationFilenames()` set;
  the previously-missing suffix migrations are now recorded; and each prefix
  migration row's `applied_at` is unchanged (not rewritten) — forward-only,
  advanced by exactly the missing migrations.

**Test:** `TestPopulatedUpgrade_StateSurvivesVPrevToVNext`.

---

### TC-UPG-06: A subsequent collection / suffix-capability insert succeeds after upgrade

**Rule:** R-UPG-04 / B6 — Normal

**Given:**
- The upgraded store at the current schema.

**When:**
- A new `InsertCollectionAtomic` is persisted and a capability introduced by
  a suffix migration is used (`UpsertCircuitState`, added by the circuit-
  state migration that was part of the applied suffix).

**Then:**
- Both succeed and are observable — the upgraded store is fully usable at the
  current schema, including the tables the upgrade added.

**Test:** `TestPopulatedUpgrade_StateSurvivesVPrevToVNext`.

---

### TC-UPG-07: No credential in the store; no empty-store init over populated data

**Rule:** R-UPG-04 / B7 / INV-UPG-03 — Failure (safety) / Invalid-guard

**Given:**
- The populated vPREV store whose target carries only a `secret_type` /
  `secret_ref` reference.

**When:**
- The upgrade completes.

**Then:**
- No credential-like meta key (`password`, `api_token`, `token`, `secret`,
  `db_password`) is present; the target's persisted secret is the reference,
  with no password/token substring; and the post-upgrade identity / history /
  watermark are the pre-upgrade values (the store was NOT re-initialized
  empty over the populated data).

**Test:** `TestPopulatedUpgrade_StateSurvivesVPrevToVNext`.

---

### TC-UPG-08: Supported deploy modes carry populated-upgrade-safe `/data`

**Rule:** R-UPG-05 / B8 / INV-DUR-01 — Structural

**Scenario:** Because the state proof (TC-UPG-02..07) is
deployment-independent, each supported mode only needs a durable `/data` for
that proof to apply. The Helm PVC default and cloud durable-volume retain are
already guarded by #501; this case adds the container/compose mapping.

**Given:**
- The container/compose deploy guidance (`deploy/docker-compose*.yml` /
  `examples/` / the deploy README) that runs the collector with `/data`.

**When:**
- The structural guard parses it.

**Then:**
- The container/compose `/data` is a declared named/durable volume (not an
  anonymous/ephemeral mount), and the guard references the populated-upgrade
  rule so a regression to an ephemeral `/data` fails CI. The #501 Helm-PVC and
  cloud-volume-retain guards remain in force (asserted by the retained
  `tests/data_durability_test.go`).

**Test:** `TestPopulatedUpgrade_ContainerDataPathDurable`.

---

### TC-UPG-09: Fresh-install coverage is retained

**Rule:** R-UPG-06 / INV-UPG-04 — Regression (additive)

**Given:**
- The existing fresh-install / schema-version / durability-continuity tests.

**When:**
- The upgrade gate is added.

**Then:**
- Those tests are unmodified and still pass under the same `test` gate — the
  upgrade gate is additive, not a replacement. (Verified by the full
  `go test -race ./...` run staying green with the pre-existing tests
  untouched.)

**Test:** existing `TestSchemaVersion_FreshStore`,
`TestDataDurability_StoreSurvivesInstanceReplacement`,
`TestDataDurability_*` (retained, unmodified).
