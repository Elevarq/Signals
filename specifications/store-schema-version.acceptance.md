# Acceptance Tests: Store Schema Version

## Feature

`specifications/store-schema-version.md` (Elevarq/Signals#502)

## Test harness

The store-schema-version behavior is verified at the store layer with
deterministic, table-driven Go tests in `internal/db` (package `db`, normal
`go test -race` job) — no live PostgreSQL, no cloud resource. Each case
constructs a store in a known schema state (fresh / legacy / current / older /
newer / failing-migration / interrupted) and asserts the `Migrate` behavior
and the non-destructiveness of a refusal.

The newer-store cases (TC-SCHEMA-05, TC-SCHEMA-08) are the red-before-green
pins for #502: on the base binary `Migrate` silently succeeds against a store
with an unknown ledger row (the proven defect), so these fail RED until the
guard is implemented. The remaining cases pin the preserved forward-migration
behavior (fresh / legacy / current / older / failure / interrupted) so the
guard cannot regress it.

## Test Cases

### TC-SCHEMA-01: Fresh store migrates forward and is usable

**Rule:** R-SCHEMA-06 / B1 — Normal

**Given:**
- An empty database file.

**When:**
- `Open` + `Migrate`.

**Then:**
- `Migrate` returns nil; the `schema_migrations` ledger records exactly the
  binary's embedded migration filenames; the store is usable (e.g.
  `EnsureInstanceID` succeeds).

**Test:** `TestSchemaVersion_FreshStore`

---

### TC-SCHEMA-02: Legacy unversioned store is safely adopted

**Rule:** R-SCHEMA-05 / B2 — Boundary

**Scenario:** A store that predates the ledger — tables present, no
`schema_migrations` table (and the empty-ledger variant) — must not be
mistaken for "newer".

**Given:**
- A store with application tables but no `schema_migrations` table; and
  separately, a store with the table but zero rows.

**When:**
- `Migrate`.

**Then:**
- `Migrate` returns nil (no `*SchemaNewerError`); the forward path adopts the
  store; the store is usable.

**Test:** `TestSchemaVersion_LegacyUnversionedAdopted`

---

### TC-SCHEMA-03: Current store reopens idempotently

**Rule:** R-SCHEMA-02 / B3 — Normal

**Given:**
- A store already migrated to exactly the binary's embedded set.

**When:**
- `Migrate` is run a second time.

**Then:**
- `Migrate` returns nil; no migration is re-applied; the ledger is unchanged;
  the guard passes; the store is usable.

**Test:** `TestSchemaVersion_CurrentStoreIdempotent`

---

### TC-SCHEMA-04: Older store requiring forward migration migrates forward

**Rule:** R-SCHEMA-06 / B4 — Normal

**Scenario:** A store whose ledger is a strict prefix of the embedded set (an
earlier release's store) opened by the current binary.

**Given:**
- A store whose `schema_migrations` contains only the first N−1 of the N
  embedded migrations (the rest not yet applied).

**When:**
- `Migrate`.

**Then:**
- The guard passes (every recorded filename is known); the missing migrations
  are applied forward in order; the ledger ends equal to the embedded set; the
  store is usable.

**Test:** `TestSchemaVersion_OlderStoreMigratesForward`

---

### TC-SCHEMA-05: Newer store opened by an older binary is refused (typed error), no mutation

**Rule:** R-SCHEMA-03 / R-SCHEMA-04 / INV-SCHEMA-01 / INV-SCHEMA-02 / B5 —
Failure (red-gate)

**Scenario:** The proven defect. A store whose ledger records a migration the
binary does not ship (it was migrated by a newer binary). The older binary
must refuse, loudly and non-destructively.

**Given:**
- A fully-migrated current store, then a synthetic future ledger row inserted
  for a filename not in the embedded set (e.g. `999_from_the_future.sql`).

**When:**
- `Migrate`.

**Then:**
- `Migrate` returns a non-nil error that is a `*SchemaNewerError` and matches
  `errors.Is(err, ErrSchemaNewer)`; the message names the store's recorded max
  (`999_from_the_future.sql`) and the binary's max known migration, and
  carries operator recovery guidance (forward-only / restore-from-backup).
- The store is **unchanged**: the future ledger row is still present, every
  embedded migration row is still present, no table was recreated/truncated,
  and no new ledger row was written.

**Test:** `TestSchemaVersion_NewerStoreRefused`

---

### TC-SCHEMA-06: Migration failure propagates and does not advance the ledger

**Rule:** R-SCHEMA-06 / INV-SCHEMA-04 / B6 — Failure

**Scenario:** The existing atomic `applyMigration` guarantee, pinned so the
guard work does not weaken it.

**Given:**
- A store and a migration whose DDL is invalid, applied via
  `ApplyMigrationSQL`.

**When:**
- The invalid migration is applied.

**Then:**
- An error is returned; the invalid migration's ledger row is **not** present
  (DDL + ledger insert rolled back together); a subsequent valid apply of the
  same filename succeeds from a clean state.

**Test:** `TestSchemaVersion_MigrationFailureNotAdvanced`

---

### TC-SCHEMA-07: Interrupted migration is resumable on rerun

**Rule:** INV-SCHEMA-04 / B7 — Boundary

**Scenario:** A `Migrate` sequence that committed some but not all migrations
(simulated by seeding the ledger with only a prefix of the embedded set, as
if a crash had occurred mid-sequence) resumes on rerun.

**Given:**
- A store whose ledger holds a strict prefix of the embedded set (the rest
  "interrupted").

**When:**
- `Migrate` is run.

**Then:**
- Already-committed migrations are skipped (their rows persist, unchanged
  `applied_at`); the remaining migrations are applied forward; the ledger
  reaches the embedded set; the store is usable.

**Test:** `TestSchemaVersion_InterruptedMigrationResumes`

---

### TC-SCHEMA-08: After a refusal, a reopen by the older binary still refuses idempotently and remains non-destructive

**Rule:** R-SCHEMA-03 / R-SCHEMA-04 / INV-SCHEMA-02 / B5 — Failure/Idempotent
(red-gate)

**Scenario:** The refusal is stable — reopening the newer store with the older
binary refuses again, with the store still untouched (a refusal is never a
one-shot that later silently proceeds).

**Given:**
- The newer store from TC-SCHEMA-05 (future ledger row present).

**When:**
- `Migrate` is called twice in succession on the same (reopened) store.

**Then:**
- Both calls return `*SchemaNewerError` (`errors.Is(err, ErrSchemaNewer)`);
  the store's ledger and tables are identical before and after both calls (no
  mutation on either call).

**Test:** `TestSchemaVersion_NewerStoreRefusalIdempotentNonDestructive`

---

### TC-SCHEMA-09: Production ledger reconciles to POC/2.0 (cross-branch equivalence)

**Rule:** R-SCHEMA-08 / INV-SCHEMA-06 — Normal + Boundary + Failure (red-gate,
cross-branch upgrade)

**Scenario:** A real production (1.x) store whose ledger ends at
`007_cycle_outcome_last_error.sql` is opened by the POC/2.0 binary, whose
embedded `008_cycle_outcome_last_error.sql` is the same DDL renumbered after
`007` was taken for snapshot-engine identity. The upgrade must adopt the store,
apply the genuinely-new `007_snapshot_engine.sql`, and record `008` ledger-only
(no duplicate `ADD COLUMN`), preserving state — while still refusing a genuinely
newer migration.

**Given:**
- A store built from the REAL released DDL: shared migrations `001..006` applied
  normally, then the cycle-outcome DDL applied under the production filename
  `007_cycle_outcome_last_error.sql` (byte-identical to embedded `008`), and
  `007_snapshot_engine.sql` NOT applied. Pre-upgrade meta is set.

**Then (normal + boundary):**
- `Migrate` succeeds (no `*SchemaNewerError`, no duplicate-column error).
- `snapshots.engine` exists afterwards (new `007_snapshot_engine.sql` applied).
- `target_cycle_outcomes.reason_code` exists exactly once (reconcile did not
  re-run the DDL).
- The ledger contains all of `007_cycle_outcome_last_error.sql` (preserved),
  `007_snapshot_engine.sql`, and `008_cycle_outcome_last_error.sql`.
- Pre-upgrade meta survives; the store is usable (`EnsureInstanceID`).
- A second `Migrate` is a no-op (idempotent re-run; ledger unchanged).

**Then (failure):**
- The same store with an additional genuinely-newer `999_from_the_future.sql`
  ledger row is still refused with `*SchemaNewerError` — the equivalence map is a
  closed enumerated set and never widens acceptance.

**Test:** `TestSchemaVersion_ProductionLedgerReconciles`,
`TestSchemaVersion_ReconcileIdempotent`,
`TestSchemaVersion_EquivalenceDoesNotAcceptGenuinelyNewer`
