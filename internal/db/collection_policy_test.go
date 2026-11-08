package db

import (
	"path/filepath"
	"testing"
)

// #729 / CTP: migration 009 adds the per-snapshot collection-time policy
// columns but, unlike 008's source-identity backfill, it MUST NOT invent a
// value for pre-#729 rows — their collection-time policy is genuinely unknown
// and the honest representation is NULL. This test upgrades a pre-009 store and
// asserts the legacy row's policy columns read back as NULL (tri-state nil).
func TestMigration009_DoesNotInventLegacyPolicy(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "p.db"), false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	// Apply every migration BEFORE 009 (a pre-#729 store).
	if _, err := store.SQL().Exec(`CREATE TABLE schema_migrations (
		filename TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatalf("seed ledger: %v", err)
	}
	for _, name := range MigrationFilenames() {
		if name == "010_snapshot_collection_policy.sql" {
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

	tid, err := store.UpsertTarget("app", "h", 5432, "d", "u", "require", "NONE", "", true)
	if err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}
	// Insert with the pre-009 column set (no collect_* columns).
	if _, err := store.SQL().Exec(
		"INSERT INTO snapshots (id, target_id, collected_at, pg_version, payload, size_bytes, source_host, source_port, source_db, source_user) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"pre729", tid, "2026-11-07T10:00:00Z", "PostgreSQL 16", "{}", 2, "h", 5432, "d", "u",
	); err != nil {
		t.Fatalf("insert pre-729 snapshot: %v", err)
	}

	// Upgrade: migration 009 adds the columns (nullable, no backfill).
	if err := store.Migrate(); err != nil {
		t.Fatalf("Migrate (apply 009): %v", err)
	}

	s, err := store.GetSnapshotByID("pre729")
	if err != nil {
		t.Fatalf("GetSnapshotByID: %v", err)
	}
	if s == nil {
		t.Fatal("legacy snapshot not found after upgrade")
	}
	if s.CollectUnsafeMode != nil {
		t.Errorf("legacy CollectUnsafeMode must be nil (unknown), got %v", *s.CollectUnsafeMode)
	}
	if s.CollectHighSensitivity != nil {
		t.Errorf("legacy CollectHighSensitivity must be nil (unknown), got %v", *s.CollectHighSensitivity)
	}
}

// #729 / CTP: a snapshot inserted with a concrete collection-time policy reads
// back with that exact tri-state value (not unknown).
func TestInsertSnapshot_PersistsCollectionPolicy(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "r.db"), false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	tid, err := store.UpsertTarget("app", "h", 5432, "d", "u", "require", "NONE", "", true)
	if err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}

	unsafe, highSens := false, true
	if err := store.InsertSnapshot(Snapshot{
		ID: "s1", TargetID: tid, CollectedAt: "2026-11-07T10:00:00Z",
		PGVersion: "PostgreSQL 16", Payload: []byte("{}"), SizeBytes: 2,
		CollectUnsafeMode: &unsafe, CollectHighSensitivity: &highSens,
	}); err != nil {
		t.Fatalf("InsertSnapshot: %v", err)
	}

	s, err := store.GetSnapshotByID("s1")
	if err != nil || s == nil {
		t.Fatalf("GetSnapshotByID: %v (nil=%v)", err, s == nil)
	}
	if s.CollectUnsafeMode == nil || *s.CollectUnsafeMode != false {
		t.Errorf("CollectUnsafeMode = %v, want false", s.CollectUnsafeMode)
	}
	if s.CollectHighSensitivity == nil || *s.CollectHighSensitivity != true {
		t.Errorf("CollectHighSensitivity = %v, want true", s.CollectHighSensitivity)
	}
}
