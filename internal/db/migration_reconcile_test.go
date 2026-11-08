package db

import (
	"errors"
	"path/filepath"
	"sort"
	"testing"
)

// #710 / F03 / R-SCHEMA-08: a real PRODUCTION store (ledger ends at
// 007_cycle_outcome_last_error.sql) must upgrade to POC/2.0 — whose embedded
// 008_cycle_outcome_last_error.sql is the SAME DDL renumbered after 007 was
// taken for snapshot-engine identity. The binary must (a) adopt the store, not
// refuse it as newer, (b) apply the genuinely-new 007_snapshot_engine.sql, and
// (c) reconcile 008 ledger-only without re-running the duplicate ADD COLUMN.

// seedProductionStore builds a faithful production end-state store using the
// REAL released DDL: shared migrations 001..006 applied normally, then the
// cycle-outcome DDL applied under the PRODUCTION filename
// 007_cycle_outcome_last_error.sql (its DDL is byte-identical to the embedded
// 008). It deliberately does NOT apply 007_snapshot_engine.sql (production never
// shipped it), so `snapshots.engine` is absent — exactly a 1.x store.
func seedProductionStore(t *testing.T) *DB {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "prod.db"), false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if _, err := store.SQL().Exec(`CREATE TABLE schema_migrations (
		filename TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatalf("seed ledger: %v", err)
	}

	// Shared prefix 001..006 (identical on both release lines).
	for _, name := range []string{
		"001_initial.sql",
		"002_target_metadata.sql",
		"003_query_results.sql",
		"004_query_run_status.sql",
		"005_target_cycle_outcomes.sql",
		"006_circuit_state.sql",
	} {
		data, err := migrationsFS.ReadFile(migrationsDir + "/" + name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if err := store.ApplyMigrationSQL(name, string(data)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}

	// Production's 007 == the embedded 008 DDL, recorded under the PRODUCTION
	// filename. This reproduces the real 1.x ledger using the real DDL.
	ddl, err := migrationsFS.ReadFile(migrationsDir + "/008_cycle_outcome_last_error.sql")
	if err != nil {
		t.Fatalf("read embedded 008 DDL: %v", err)
	}
	if err := store.ApplyMigrationSQL("007_cycle_outcome_last_error.sql", string(ddl)); err != nil {
		t.Fatalf("apply production 007: %v", err)
	}
	return store
}

func columnExists(t *testing.T, store *DB, table, column string) bool {
	t.Helper()
	rows, err := store.SQL().Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatalf("table_info(%s): %v", table, err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan table_info: %v", err)
		}
		if name == column {
			n++
		}
	}
	if n > 1 {
		t.Errorf("column %s.%s present %d times — duplicate ADD COLUMN", table, column, n)
	}
	return n == 1
}

// TC-SCHEMA-08a: the production ledger is ADOPTED and reconciled, not refused.
func TestSchemaVersion_ProductionLedgerReconciles(t *testing.T) {
	store := seedProductionStore(t)

	// Pre-upgrade state that must survive the reconcile.
	if err := store.SetMeta("instance_marker", "prod-store-123"); err != nil {
		t.Fatalf("SetMeta: %v", err)
	}
	// Production schema: cycle-outcome columns present, engine column absent.
	if !columnExists(t, store, "target_cycle_outcomes", "reason_code") {
		t.Fatal("precondition: production store missing reason_code (007 DDL not applied)")
	}
	if columnExists(t, store, "snapshots", "engine") {
		t.Fatal("precondition: production store already has snapshots.engine (should be absent pre-upgrade)")
	}

	// The POC/2.0 upgrade must succeed (no SchemaNewerError, no duplicate column).
	if err := store.Migrate(); err != nil {
		t.Fatalf("POC Migrate on a production store must reconcile and succeed, got: %v", err)
	}

	// The new migration applied; the reconciled one did NOT re-run its DDL.
	if !columnExists(t, store, "snapshots", "engine") {
		t.Error("007_snapshot_engine.sql did not apply — snapshots.engine missing after upgrade")
	}
	if !columnExists(t, store, "target_cycle_outcomes", "reason_code") {
		t.Error("reason_code missing/duplicated after reconcile")
	}

	// Ledger converged to the embedded identity AND preserved the historical row.
	ledger := ledgerFilenames(t, store)
	lset := map[string]bool{}
	for _, f := range ledger {
		lset[f] = true
	}
	for _, want := range []string{
		"007_cycle_outcome_last_error.sql", // preserved production row (non-destructive)
		"007_snapshot_engine.sql",          // genuinely new, applied
		"008_cycle_outcome_last_error.sql", // reconciled ledger-only
	} {
		if !lset[want] {
			t.Errorf("ledger missing %q after reconcile; got %v", want, ledger)
		}
	}

	// Data preserved.
	if v, _ := store.GetMeta("instance_marker"); v != "prod-store-123" {
		t.Errorf("pre-upgrade meta lost: got %q, want prod-store-123", v)
	}
	if _, err := store.EnsureInstanceID(); err != nil {
		t.Errorf("store not usable after upgrade: %v", err)
	}
}

// TC-SCHEMA-08b: the reconcile is idempotent and non-destructive on re-run
// (interrupted-upgrade re-entry safety).
func TestSchemaVersion_ReconcileIdempotent(t *testing.T) {
	store := seedProductionStore(t)
	if err := store.Migrate(); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	before := ledgerFilenames(t, store)

	if err := store.Migrate(); err != nil {
		t.Fatalf("second Migrate (idempotent re-run): %v", err)
	}
	after := ledgerFilenames(t, store)

	sort.Strings(before)
	sort.Strings(after)
	if !equalStrings(before, after) {
		t.Fatalf("ledger changed on reconcile re-run: before=%v after=%v", before, after)
	}
}

// TC-SCHEMA-08c: the equivalence map does NOT widen acceptance — a genuinely
// newer/unknown migration alongside a recognized equivalent is still refused.
func TestSchemaVersion_EquivalenceDoesNotAcceptGenuinelyNewer(t *testing.T) {
	store := seedProductionStore(t)
	if _, err := store.SQL().Exec(
		"INSERT INTO schema_migrations (filename, applied_at) VALUES (?, ?)",
		"999_from_the_future.sql", "2099-01-01T00:00:00Z",
	); err != nil {
		t.Fatalf("seed future row: %v", err)
	}

	err := store.Migrate()
	if err == nil {
		t.Fatal("Migrate accepted a store with a genuinely-newer migration — the equivalence map must not widen acceptance (R-SCHEMA-08 / INV-SCHEMA-06)")
	}
	if !errors.Is(err, ErrSchemaNewer) {
		t.Fatalf("want ErrSchemaNewer, got: %v", err)
	}
}
