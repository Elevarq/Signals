package tests

import (
	"context"
	"testing"
	"time"

	"github.com/elevarq/arq-signals/internal/collector"
	"github.com/elevarq/arq-signals/internal/config"
	"github.com/elevarq/arq-signals/internal/db"
)

// ---------------------------------------------------------------------------
// R031: Initial forced collection (TC-SIG-037)
// ---------------------------------------------------------------------------

// TestInitialCollectionIsForced verifies that the collector's Run method
// attempts collection immediately on startup, not waiting for the first
// poll interval tick.
func TestInitialCollectionIsForced(t *testing.T) {
	store := openTestDB(t)
	targets := []config.TargetConfig{
		{Name: "unreachable", Host: "192.0.2.1", Port: 59999, DBName: "x", User: "x", Enabled: true},
	}

	coll := collector.New(store, targets, 24*time.Hour, 30,
		collector.WithTargetTimeout(500*time.Millisecond),
		collector.WithQueryTimeout(500*time.Millisecond),
	)

	done := make(chan struct{})
	go func() {
		defer close(done)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		coll.Run(ctx)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("collector did not complete within 5s — initial cycle may not have fired")
	}

	// The initial cycle should have attempted to connect to the
	// unreachable target and logged a collect_error event.
	var eventCount int
	_ = store.SQL().QueryRow("SELECT COUNT(*) FROM events WHERE event_type = 'collect_error'").Scan(&eventCount)
	if eventCount == 0 {
		t.Error("no collect_error event found — initial forced cycle may not have executed")
	}
}

// ---------------------------------------------------------------------------
// R032: Overlap prevention (TC-SIG-038)
// ---------------------------------------------------------------------------

// TestOverlapPreventionCollectNow verifies that rapid CollectNow calls
// do not block and are deduplicated (buffered channel of size 1).
func TestOverlapPreventionCollectNow(t *testing.T) {
	store := openTestDB(t)
	coll := collector.New(store, nil, time.Hour, 30)

	// CollectNow uses a buffered channel of size 1. Calling it twice
	// rapidly should not block — the second send is silently dropped.
	coll.CollectNow(collector.CollectRequest{})
	coll.CollectNow(collector.CollectRequest{})
	// If this test hangs, the overlap protection is broken.
}

// ---------------------------------------------------------------------------
// R033: Concurrent multi-target collection (TC-SIG-039)
// ---------------------------------------------------------------------------

// TestConcurrentMultiTargetCollection verifies that the collector
// processes multiple targets and a failure on one does not block others.
func TestConcurrentMultiTargetCollection(t *testing.T) {
	store := openTestDB(t)

	targets := []config.TargetConfig{
		{Name: "t1", Host: "192.0.2.1", Port: 59999, DBName: "x", User: "x", Enabled: true},
		{Name: "t2", Host: "192.0.2.2", Port: 59999, DBName: "x", User: "x", Enabled: true},
		{Name: "t3", Host: "192.0.2.3", Port: 59999, DBName: "x", User: "x", Enabled: true},
	}

	coll := collector.New(store, targets, 24*time.Hour, 30,
		collector.WithMaxConcurrentTargets(2),
		collector.WithTargetTimeout(500*time.Millisecond),
		collector.WithQueryTimeout(500*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	coll.Run(ctx)

	// All 3 targets should have been attempted.
	var errorCount int
	_ = store.SQL().QueryRow("SELECT COUNT(*) FROM events WHERE event_type = 'collect_error'").Scan(&errorCount)
	if errorCount < 3 {
		t.Errorf("expected at least 3 collect_error events (one per target), got %d", errorCount)
	}
}

// ---------------------------------------------------------------------------
// R036: Persistence guarantees (TC-SIG-043)
// ---------------------------------------------------------------------------

// TestMigrationCreatesExpectedTables verifies that opening a fresh
// database and running Migrate creates all required tables.
func TestMigrationCreatesExpectedTables(t *testing.T) {
	store := openTestDB(t)

	tables := []string{"meta", "targets", "snapshots", "events",
		"query_catalog", "query_runs", "query_results", "schema_migrations"}

	for _, table := range tables {
		var count int
		err := store.SQL().QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count)
		if err != nil {
			t.Errorf("table %q should exist after migration: %v", table, err)
		}
	}
}

// TestInstanceIDStableAcrossRestarts verifies that EnsureInstanceID
// returns the same value on repeated calls.
func TestInstanceIDStableAcrossRestarts(t *testing.T) {
	store := openTestDB(t)

	id1, err := store.EnsureInstanceID()
	if err != nil {
		t.Fatalf("first EnsureInstanceID: %v", err)
	}
	if id1 == "" {
		t.Fatal("instance ID should not be empty")
	}

	id2, err := store.EnsureInstanceID()
	if err != nil {
		t.Fatalf("second EnsureInstanceID: %v", err)
	}

	if id1 != id2 {
		t.Errorf("instance ID should be stable: got %q then %q", id1, id2)
	}
}

// TestRetentionCleanup verifies that retention-based deletion removes
// old data while preserving recent data.
func TestRetentionCleanup(t *testing.T) {
	store := openTestDB(t)

	_ = store.InsertSnapshot(db.Snapshot{
		ID: "old-snap", TargetID: 0, CollectedAt: "2020-01-01T00:00:00Z",
		PGVersion: "16", Payload: []byte("{}"), SizeBytes: 2,
	})
	_ = store.InsertSnapshot(db.Snapshot{
		ID: "new-snap", TargetID: 0, CollectedAt: "2099-01-01T00:00:00Z",
		PGVersion: "16", Payload: []byte("{}"), SizeBytes: 2,
	})

	deleted, err := store.DeleteSnapshotsOlderThan("2025-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("DeleteSnapshotsOlderThan: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 deleted, got %d", deleted)
	}

	count, _ := store.CountSnapshots()
	if count != 1 {
		t.Errorf("expected 1 remaining snapshot, got %d", count)
	}
}

// TestAtomicBatchInsert verifies that InsertQueryRunBatch writes runs
// and results atomically.
func TestAtomicBatchInsert(t *testing.T) {
	store := openTestDB(t)

	targetID, err := store.UpsertTarget("test", "localhost", 5432, "db", "user", "prefer", "NONE", "", true)
	if err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}

	runs := []db.QueryRun{
		{ID: "run-1", TargetID: targetID, SnapshotID: "snap-1", QueryID: "q1",
			CollectedAt: "2026-01-01T00:00:00Z", PGVersion: "16", CreatedAt: "2026-01-01T00:00:00Z"},
		{ID: "run-2", TargetID: targetID, SnapshotID: "snap-1", QueryID: "q2",
			CollectedAt: "2026-01-01T00:00:00Z", PGVersion: "16", CreatedAt: "2026-01-01T00:00:00Z"},
	}
	results := []db.QueryResult{
		{RunID: "run-1", Payload: []byte(`{"a":1}`), Compressed: false, SizeBytes: 7},
	}

	if err := store.InsertQueryRunBatch(runs, results); err != nil {
		t.Fatalf("InsertQueryRunBatch: %v", err)
	}

	allRuns, err := store.GetAllQueryRuns("", "")
	if err != nil {
		t.Fatalf("GetAllQueryRuns: %v", err)
	}
	if len(allRuns) != 2 {
		t.Errorf("expected 2 runs, got %d", len(allRuns))
	}

	res, err := store.GetQueryResultByRunID("run-1")
	if err != nil {
		t.Fatalf("GetQueryResultByRunID: %v", err)
	}
	if res == nil {
		t.Error("expected result for run-1, got nil")
	}
}

// TestInsertCollectionAtomicCommitsAll verifies the happy path: snapshot,
// runs, and results all land in storage from a single InsertCollectionAtomic
// call.
// Traces: ARQ-SIGNALS-R077
func TestInsertCollectionAtomicCommitsAll(t *testing.T) {
	store := openTestDB(t)
	targetID, err := store.UpsertTarget("test", "localhost", 5432, "db", "user", "prefer", "NONE", "", true)
	if err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}

	snap := db.Snapshot{
		ID: "snap-atomic", TargetID: targetID, CollectedAt: "2026-04-24T00:00:00Z",
		PGVersion: "16", Payload: []byte(`{"version":"16"}`), SizeBytes: 16,
	}
	runs := []db.QueryRun{
		{ID: "run-a", TargetID: targetID, SnapshotID: snap.ID, QueryID: "q1",
			CollectedAt: snap.CollectedAt, PGVersion: "16", CreatedAt: snap.CollectedAt, Status: "success"},
	}
	results := []db.QueryResult{
		{RunID: "run-a", Payload: []byte(`{"k":1}`), SizeBytes: 7},
	}

	if err := store.InsertCollectionAtomic(snap, runs, results); err != nil {
		t.Fatalf("InsertCollectionAtomic: %v", err)
	}

	if n, _ := store.CountSnapshots(); n != 1 {
		t.Errorf("snapshot count = %d, want 1", n)
	}
	allRuns, _ := store.GetAllQueryRuns("", "")
	if len(allRuns) != 1 {
		t.Errorf("run count = %d, want 1", len(allRuns))
	}
	if res, _ := store.GetQueryResultByRunID("run-a"); res == nil {
		t.Error("result for run-a not persisted")
	}
}

// TestInsertCollectionAtomicRollsBackOnRunFailure verifies that if any
// query_run insert fails (here: two runs share the same primary key),
// nothing — not even the snapshot — is left behind.
// Traces: ARQ-SIGNALS-R077
func TestInsertCollectionAtomicRollsBackOnRunFailure(t *testing.T) {
	store := openTestDB(t)
	targetID, err := store.UpsertTarget("test", "localhost", 5432, "db", "user", "prefer", "NONE", "", true)
	if err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}

	snap := db.Snapshot{
		ID: "snap-rollback", TargetID: targetID, CollectedAt: "2026-04-24T00:00:00Z",
		PGVersion: "16", Payload: []byte(`{}`), SizeBytes: 2,
	}
	// Two runs share the same ID — the second insert violates the PK
	// constraint on query_runs(id) and aborts the transaction.
	badRuns := []db.QueryRun{
		{ID: "run-dup", TargetID: targetID, SnapshotID: snap.ID, QueryID: "q1",
			CollectedAt: snap.CollectedAt, PGVersion: "16", CreatedAt: snap.CollectedAt},
		{ID: "run-dup", TargetID: targetID, SnapshotID: snap.ID, QueryID: "q2",
			CollectedAt: snap.CollectedAt, PGVersion: "16", CreatedAt: snap.CollectedAt},
	}

	err = store.InsertCollectionAtomic(snap, badRuns, nil)
	if err == nil {
		t.Fatal("expected error from duplicate run ID, got nil")
	}

	// Critical: the snapshot must NOT be present, otherwise readers would
	// observe a snapshot with no runs (the partial state R077 forbids).
	if n, _ := store.CountSnapshots(); n != 0 {
		t.Errorf("snapshot count = %d after rollback, want 0", n)
	}
	if runs, _ := store.GetAllQueryRuns("", ""); len(runs) != 0 {
		t.Errorf("run count = %d after rollback, want 0", len(runs))
	}
}
