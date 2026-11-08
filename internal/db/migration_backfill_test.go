package db

import (
	"path/filepath"
	"testing"
)

// #716 / F10: migration 008 adds the immutable per-snapshot source-identity
// columns AND backfills pre-#716 rows from the then-current targets row (the
// only identity available for historical snapshots), so existing evidence
// gains a stable identity on upgrade.
func TestMigration008_BackfillsSourceIdentityFromTargets(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "b.db"), false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	// Apply every migration BEFORE 008 (a pre-#716 store), then insert a
	// snapshot with the old column set (no source_* columns yet).
	if _, err := store.SQL().Exec(`CREATE TABLE schema_migrations (
		filename TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatalf("seed ledger: %v", err)
	}
	for _, name := range MigrationFilenames() {
		if name == "008_snapshot_source_identity.sql" {
			break
		}
		data, rerr := migrationsFS.ReadFile(migrationsDir + "/" + name)
		if rerr != nil {
			t.Fatalf("read %s: %v", name, rerr)
		}
		if aerr := store.ApplyMigrationSQL(name, string(data)); aerr != nil {
			t.Fatalf("apply %s: %v", name, aerr)
		}
	}

	tid, err := store.UpsertTarget("app", "bf-host", 6432, "bfdb", "bfuser", "require", "NONE", "", true)
	if err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}
	if _, err := store.SQL().Exec(
		"INSERT INTO snapshots (id, target_id, collected_at, pg_version, payload, size_bytes) VALUES (?, ?, ?, ?, ?, ?)",
		"pre716", tid, "2026-11-07T10:00:00Z", "PostgreSQL 16", "{}", 2,
	); err != nil {
		t.Fatalf("insert pre-716 snapshot: %v", err)
	}

	// Upgrade: migration 008 adds the columns and backfills the existing row.
	if err := store.Migrate(); err != nil {
		t.Fatalf("Migrate (apply 008): %v", err)
	}

	var host, dbname, user string
	var port int
	if err := store.SQL().QueryRow(
		"SELECT source_host, source_port, source_db, source_user FROM snapshots WHERE id = ?", "pre716",
	).Scan(&host, &port, &dbname, &user); err != nil {
		t.Fatalf("read backfilled identity: %v", err)
	}
	if host != "bf-host" || port != 6432 || dbname != "bfdb" || user != "bfuser" {
		t.Errorf("backfill = %s:%d/%s@%s, want bf-host:6432/bfdb@bfuser (from the targets row)", host, port, dbname, user)
	}
}
