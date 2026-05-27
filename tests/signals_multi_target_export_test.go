package tests

import (
	"testing"

	"github.com/elevarq/arq-signals/internal/collector"
	"github.com/elevarq/arq-signals/internal/db"
)

// --- MTE-R001: Target-filtered query runs ---

func TestGetQueryRunsByTargetFiltersCorrectly(t *testing.T) {
	store := openTestDB(t)

	t1 := mteInsertTarget(t, store, "target-a")
	t2 := mteInsertTarget(t, store, "target-b")

	mteInsertRun(t, store, "run-a1", t1, "pg_stat_user_tables_v1", "2026-03-16T10:00:00Z", 0, 10, "")
	mteInsertRun(t, store, "run-a2", t1, "pg_constraints_v1", "2026-03-16T10:00:01Z", 0, 5, "")
	mteInsertRun(t, store, "run-b1", t2, "pg_stat_user_tables_v1", "2026-03-16T10:00:00Z", 0, 20, "")

	runs, err := store.GetQueryRunsByTarget(t1, "", "")
	if err != nil {
		t.Fatalf("GetQueryRunsByTarget: %v", err)
	}

	if len(runs) != 2 {
		t.Errorf("expected 2 runs for target-a, got %d", len(runs))
	}
	for _, r := range runs {
		if r.TargetID != t1 {
			t.Errorf("run %s has target_id=%d, want %d", r.ID, r.TargetID, t1)
		}
	}
}

func TestGetQueryRunsByTargetRespectsTimeRange(t *testing.T) {
	store := openTestDB(t)
	tid := mteInsertTarget(t, store, "target-a")

	mteInsertRun(t, store, "run-1", tid, "a_v1", "2026-03-16T08:00:00Z", 0, 1, "")
	mteInsertRun(t, store, "run-2", tid, "b_v1", "2026-03-16T12:00:00Z", 0, 1, "")
	mteInsertRun(t, store, "run-3", tid, "c_v1", "2026-03-16T16:00:00Z", 0, 1, "")

	runs, _ := store.GetQueryRunsByTarget(tid, "2026-03-16T10:00:00Z", "2026-03-16T14:00:00Z")
	if len(runs) != 1 {
		t.Errorf("expected 1 run in time range, got %d", len(runs))
	}
}

// --- MTE-R004, MTE-R005, MTE-R006: Target-scoped collector status ---

func TestCollectorStatusFileWithTargetName(t *testing.T) {
	file := collector.CollectorStatusFile{
		SchemaVersion: "1",
		TargetName:    "prod-db",
		CollectedAt:   "2026-03-16T14:30:00Z",
		Collectors: []collector.CollectorStatus{
			{ID: "pg_stat_user_tables_v1", Status: "success", Attempted: true, RowCount: 47},
		},
	}
	if file.TargetName != "prod-db" {
		t.Errorf("target_name: got %q", file.TargetName)
	}
}

func TestDifferentTargetsHaveDifferentStatuses(t *testing.T) {
	fileA := collector.CollectorStatusFile{
		TargetName: "pg17-db",
		Collectors: []collector.CollectorStatus{
			{ID: "pg_functions_v1", Status: "success", Attempted: true, RowCount: 30},
		},
	}
	fileB := collector.CollectorStatusFile{
		TargetName: "pg10-db",
		Collectors: []collector.CollectorStatus{
			{ID: "pg_functions_v1", Status: "skipped", Reason: "version_unsupported"},
		},
	}
	if fileA.Collectors[0].Status == fileB.Collectors[0].Status {
		t.Error("same collector must have different statuses across targets")
	}
}

// --- MTE-R006: Build status from query runs ---

func TestBuildCollectorStatusFromRuns(t *testing.T) {
	runs := []db.QueryRun{
		{QueryID: "pg_stat_user_tables_v1", RowCount: 47, DurationMS: 12, Error: "", CollectedAt: "2026-03-16T14:30:01Z"},
		{QueryID: "pg_constraints_v1", RowCount: 83, DurationMS: 45, Error: "", CollectedAt: "2026-03-16T14:30:02Z"},
		{QueryID: "vacuum_health_v1", RowCount: 0, DurationMS: 3, Error: "permission denied", CollectedAt: "2026-03-16T14:30:03Z"},
	}

	statuses := collector.BuildStatusFromRuns(runs)

	if len(statuses) != 3 {
		t.Fatalf("expected 3 statuses, got %d", len(statuses))
	}

	byID := make(map[string]collector.CollectorStatus)
	for _, s := range statuses {
		byID[s.ID] = s
	}

	s := byID["pg_stat_user_tables_v1"]
	if s.Status != "success" || s.RowCount != 47 {
		t.Errorf("tables: %+v", s)
	}

	s = byID["vacuum_health_v1"]
	if s.Status != "failed" || s.Reason != "permission_denied" {
		t.Errorf("vacuum: %+v", s)
	}
}

func TestBuildCollectorStatusPermissionDenied(t *testing.T) {
	runs := []db.QueryRun{
		{QueryID: "login_roles_v1", Error: "ERROR: permission denied (SQLSTATE 42501)", CollectedAt: "2026-03-16T14:30:00Z"},
	}
	statuses := collector.BuildStatusFromRuns(runs)
	if statuses[0].Reason != "permission_denied" {
		t.Errorf("reason: got %q", statuses[0].Reason)
	}
}

func TestBuildCollectorStatusTimeout(t *testing.T) {
	runs := []db.QueryRun{
		{QueryID: "pg_stats_v1", Error: "context deadline exceeded", CollectedAt: "2026-03-16T14:30:00Z"},
	}
	statuses := collector.BuildStatusFromRuns(runs)
	if statuses[0].Reason != "timeout" {
		t.Errorf("reason: got %q", statuses[0].Reason)
	}
}

// --- GetTargetName ---

func TestGetTargetName(t *testing.T) {
	store := openTestDB(t)
	tid := mteInsertTarget(t, store, "my-prod-db")

	name, err := store.GetTargetName(tid)
	if err != nil {
		t.Fatalf("GetTargetName: %v", err)
	}
	if name != "my-prod-db" {
		t.Errorf("name: got %q, want %q", name, "my-prod-db")
	}
}

// --- Helpers (prefixed to avoid collision with signals_export_test.go) ---

func mteInsertTarget(t *testing.T, store *db.DB, name string) int64 {
	t.Helper()
	id, err := store.UpsertTarget(name, "localhost", 5432, "testdb", "testuser", "disable", "NONE", "", true)
	if err != nil {
		t.Fatalf("upsert target %s: %v", name, err)
	}
	return id
}

func mteInsertRun(t *testing.T, store *db.DB, runID string, targetID int64, queryID, collectedAt string, durationMS, rowCount int, errMsg string) {
	t.Helper()
	run := db.QueryRun{
		ID: runID, TargetID: targetID, SnapshotID: "snap-" + runID,
		QueryID: queryID, CollectedAt: collectedAt, PGVersion: "17.2",
		DurationMS: durationMS, RowCount: rowCount, Error: errMsg, CreatedAt: collectedAt,
	}
	err := store.InsertQueryRunBatch([]db.QueryRun{run}, nil)
	if err != nil {
		t.Fatalf("insert run %s: %v", runID, err)
	}
}
