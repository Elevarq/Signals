package db

import (
	"errors"
	"path/filepath"
	"sort"
	"testing"
)

// Tests for Elevarq/Signals#502 — explicit store schema version +
// forward-only / no-downgrade / fail-closed-on-newer-store policy.
// Spec: specifications/store-schema-version.md
// Acceptance: specifications/store-schema-version.acceptance.md
//
// The two newer-store cases (TC-SCHEMA-05 / TC-SCHEMA-08) are the
// red-before-green pins: on the base binary Migrate() silently succeeds
// against a store carrying an unknown ledger row (the proven defect). The
// remaining cases pin the preserved forward-migration behavior so the guard
// cannot regress it.

// openMigrated opens a fresh store at a temp path and migrates it to the
// binary's embedded set.
func openMigrated(t *testing.T) *DB {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "schema.db"), false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(); err != nil {
		t.Fatalf("Migrate (setup): %v", err)
	}
	return store
}

// ledgerFilenames reads the recorded migration filenames from the store,
// sorted.
func ledgerFilenames(t *testing.T, d *DB) []string {
	t.Helper()
	rows, err := d.SQL().Query("SELECT filename FROM schema_migrations ORDER BY filename")
	if err != nil {
		t.Fatalf("query ledger: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			t.Fatalf("scan ledger: %v", err)
		}
		out = append(out, f)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TC-SCHEMA-01: fresh store migrates forward and is usable.
func TestSchemaVersion_FreshStore(t *testing.T) {
	store := openMigrated(t)

	want := append([]string(nil), MigrationFilenames()...)
	sort.Strings(want)
	got := ledgerFilenames(t, store)
	if !equalStrings(got, want) {
		t.Fatalf("ledger = %v, want embedded set %v", got, want)
	}
	if _, err := store.EnsureInstanceID(); err != nil {
		t.Fatalf("store not usable: %v", err)
	}
}

// TC-SCHEMA-02: legacy unversioned store (no ledger table; and empty ledger)
// is adopted, not refused.
func TestSchemaVersion_LegacyUnversionedAdopted(t *testing.T) {
	// Variant A: tables exist but no schema_migrations table at all.
	t.Run("no_ledger_table", func(t *testing.T) {
		store, err := Open(filepath.Join(t.TempDir(), "legacy-a.db"), false)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		t.Cleanup(func() { _ = store.Close() })
		// Simulate a pre-ledger store: a real table, no schema_migrations.
		if _, err := store.SQL().Exec(`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT)`); err != nil {
			t.Fatalf("seed legacy table: %v", err)
		}
		if err := store.Migrate(); err != nil {
			t.Fatalf("Migrate on legacy (no ledger table) must adopt, got: %v", err)
		}
		if _, err := store.EnsureInstanceID(); err != nil {
			t.Fatalf("store not usable after adoption: %v", err)
		}
	})

	// Variant B: schema_migrations table exists but is empty.
	t.Run("empty_ledger", func(t *testing.T) {
		store, err := Open(filepath.Join(t.TempDir(), "legacy-b.db"), false)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		t.Cleanup(func() { _ = store.Close() })
		if _, err := store.SQL().Exec(`CREATE TABLE schema_migrations (
			filename TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
			t.Fatalf("seed empty ledger: %v", err)
		}
		if err := store.Migrate(); err != nil {
			t.Fatalf("Migrate on empty ledger must adopt, got: %v", err)
		}
		if _, err := store.EnsureInstanceID(); err != nil {
			t.Fatalf("store not usable after adoption: %v", err)
		}
	})
}

// TC-SCHEMA-03: current store reopens idempotently.
func TestSchemaVersion_CurrentStoreIdempotent(t *testing.T) {
	store := openMigrated(t)
	before := ledgerFilenames(t, store)

	if err := store.Migrate(); err != nil {
		t.Fatalf("second Migrate on current store: %v", err)
	}
	after := ledgerFilenames(t, store)
	if !equalStrings(before, after) {
		t.Fatalf("ledger changed on reopen: before=%v after=%v", before, after)
	}
	if _, err := store.EnsureInstanceID(); err != nil {
		t.Fatalf("store not usable: %v", err)
	}
}

// TC-SCHEMA-04: older store (strict prefix of embedded set) migrates forward.
func TestSchemaVersion_OlderStoreMigratesForward(t *testing.T) {
	embedded := append([]string(nil), MigrationFilenames()...)
	sort.Strings(embedded)
	if len(embedded) < 2 {
		t.Skipf("need >=2 embedded migrations to model an older store, have %d", len(embedded))
	}

	store, err := Open(filepath.Join(t.TempDir(), "older.db"), false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	// Create the ledger and apply only the first migration via the real
	// atomic path, leaving the rest "unapplied" (an earlier release's store).
	if _, err := store.SQL().Exec(`CREATE TABLE schema_migrations (
		filename TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatalf("seed ledger: %v", err)
	}
	first := embedded[0]
	data, err := migrationsFS.ReadFile(migrationsDir + "/" + first)
	if err != nil {
		t.Fatalf("read %s: %v", first, err)
	}
	if err := store.ApplyMigrationSQL(first, string(data)); err != nil {
		t.Fatalf("apply first migration: %v", err)
	}

	if err := store.Migrate(); err != nil {
		t.Fatalf("Migrate forward on older store: %v", err)
	}

	got := ledgerFilenames(t, store)
	if !equalStrings(got, embedded) {
		t.Fatalf("ledger = %v, want full embedded set %v", got, embedded)
	}
	if _, err := store.EnsureInstanceID(); err != nil {
		t.Fatalf("store not usable: %v", err)
	}
}

// TC-SCHEMA-05: newer store opened by an older binary is refused with a typed
// error and no mutation. RED on the base binary.
func TestSchemaVersion_NewerStoreRefused(t *testing.T) {
	store := openMigrated(t)

	const futureMigration = "999_from_the_future.sql"
	if _, err := store.SQL().Exec(
		"INSERT INTO schema_migrations (filename, applied_at) VALUES (?, ?)",
		futureMigration, "2099-01-01T00:00:00Z",
	); err != nil {
		t.Fatalf("seed future ledger row: %v", err)
	}
	before := ledgerFilenames(t, store)

	err := store.Migrate()
	if err == nil {
		t.Fatalf("Migrate accepted a newer store (unknown ledger row %q) — must refuse", futureMigration)
	}
	if !errors.Is(err, ErrSchemaNewer) {
		t.Fatalf("Migrate error is not ErrSchemaNewer: %v", err)
	}
	var sne *SchemaNewerError
	if !errors.As(err, &sne) {
		t.Fatalf("Migrate error is not *SchemaNewerError: %v", err)
	}
	// The message must name the offending/stored max and the binary's max.
	if sne.StoredMax != futureMigration {
		t.Errorf("SchemaNewerError.StoredMax = %q, want %q", sne.StoredMax, futureMigration)
	}
	embedded := append([]string(nil), MigrationFilenames()...)
	sort.Strings(embedded)
	wantBinMax := embedded[len(embedded)-1]
	if sne.BinaryMax != wantBinMax {
		t.Errorf("SchemaNewerError.BinaryMax = %q, want %q", sne.BinaryMax, wantBinMax)
	}

	// Non-destructive: the ledger is exactly as seeded (future row still
	// present, nothing added/removed).
	after := ledgerFilenames(t, store)
	if !equalStrings(before, after) {
		t.Fatalf("store mutated on refusal: before=%v after=%v", before, after)
	}
	// The future row specifically must still be present.
	var cnt int
	if err := store.SQL().QueryRow(
		"SELECT COUNT(*) FROM schema_migrations WHERE filename = ?", futureMigration,
	).Scan(&cnt); err != nil {
		t.Fatalf("recount future row: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("future ledger row count = %d, want 1 (store must be untouched)", cnt)
	}
}

// TC-SCHEMA-06: a migration DDL failure propagates and does not advance the
// ledger (the existing atomic applyMigration guarantee).
func TestSchemaVersion_MigrationFailureNotAdvanced(t *testing.T) {
	store := openMigrated(t)

	const bad = "900_bad_ddl.sql"
	err := store.ApplyMigrationSQL(bad, "THIS IS NOT VALID SQL;")
	if err == nil {
		t.Fatalf("ApplyMigrationSQL accepted invalid DDL")
	}
	var cnt int
	if err := store.SQL().QueryRow(
		"SELECT COUNT(*) FROM schema_migrations WHERE filename = ?", bad,
	).Scan(&cnt); err != nil {
		t.Fatalf("count bad ledger row: %v", err)
	}
	if cnt != 0 {
		t.Fatalf("failed migration advanced the ledger: count=%d, want 0", cnt)
	}

	// A subsequent valid apply of the same filename succeeds from a clean
	// state.
	if err := store.ApplyMigrationSQL(bad, "CREATE TABLE schema_probe_900 (x INTEGER);"); err != nil {
		t.Fatalf("retry valid apply: %v", err)
	}
	if err := store.SQL().QueryRow(
		"SELECT COUNT(*) FROM schema_migrations WHERE filename = ?", bad,
	).Scan(&cnt); err != nil {
		t.Fatalf("recount bad ledger row: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("retry did not record ledger row: count=%d, want 1", cnt)
	}
}

// TC-SCHEMA-07: interrupted migration (ledger holds only a prefix) resumes on
// rerun; committed rows are untouched.
func TestSchemaVersion_InterruptedMigrationResumes(t *testing.T) {
	embedded := append([]string(nil), MigrationFilenames()...)
	sort.Strings(embedded)
	if len(embedded) < 2 {
		t.Skipf("need >=2 embedded migrations, have %d", len(embedded))
	}

	store, err := Open(filepath.Join(t.TempDir(), "interrupted.db"), false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.SQL().Exec(`CREATE TABLE schema_migrations (
		filename TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatalf("seed ledger: %v", err)
	}

	// Apply the first migration for real (committed). The rest are
	// "interrupted" — not yet applied.
	first := embedded[0]
	data, err := migrationsFS.ReadFile(migrationsDir + "/" + first)
	if err != nil {
		t.Fatalf("read %s: %v", first, err)
	}
	if err := store.ApplyMigrationSQL(first, string(data)); err != nil {
		t.Fatalf("apply first: %v", err)
	}
	var firstApplied string
	if err := store.SQL().QueryRow(
		"SELECT applied_at FROM schema_migrations WHERE filename = ?", first,
	).Scan(&firstApplied); err != nil {
		t.Fatalf("read first applied_at: %v", err)
	}

	// Rerun resumes.
	if err := store.Migrate(); err != nil {
		t.Fatalf("resume Migrate: %v", err)
	}
	got := ledgerFilenames(t, store)
	if !equalStrings(got, embedded) {
		t.Fatalf("ledger = %v, want full embedded set %v", got, embedded)
	}
	// Committed row untouched (same applied_at).
	var firstAppliedAfter string
	if err := store.SQL().QueryRow(
		"SELECT applied_at FROM schema_migrations WHERE filename = ?", first,
	).Scan(&firstAppliedAfter); err != nil {
		t.Fatalf("reread first applied_at: %v", err)
	}
	if firstAppliedAfter != firstApplied {
		t.Fatalf("committed migration row rewritten on resume: %q -> %q", firstApplied, firstAppliedAfter)
	}
	if _, err := store.EnsureInstanceID(); err != nil {
		t.Fatalf("store not usable after resume: %v", err)
	}
}

// TC-SCHEMA-08: after a refusal, reopening the newer store refuses again and
// remains non-destructive. RED on the base binary.
func TestSchemaVersion_NewerStoreRefusalIdempotentNonDestructive(t *testing.T) {
	store := openMigrated(t)

	const futureMigration = "999_from_the_future.sql"
	if _, err := store.SQL().Exec(
		"INSERT INTO schema_migrations (filename, applied_at) VALUES (?, ?)",
		futureMigration, "2099-01-01T00:00:00Z",
	); err != nil {
		t.Fatalf("seed future ledger row: %v", err)
	}
	before := ledgerFilenames(t, store)

	for i := 0; i < 2; i++ {
		err := store.Migrate()
		if !errors.Is(err, ErrSchemaNewer) {
			t.Fatalf("call %d: Migrate did not refuse with ErrSchemaNewer: %v", i, err)
		}
		after := ledgerFilenames(t, store)
		if !equalStrings(before, after) {
			t.Fatalf("call %d mutated store: before=%v after=%v", i, before, after)
		}
	}
}
