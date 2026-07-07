package tests

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/elevarq/signals/internal/db"
	"github.com/elevarq/signals/internal/pgqueries"
)

// seedRunStatusData inserts a target, catalog, one snapshot, and the given
// runs (with a result payload for every status="success" run, since the
// export fails a success run without its result partner).
func seedRunStatusData(t *testing.T, store *db.DB, runs []db.QueryRun) {
	t.Helper()

	now := time.Now().UTC().Format(time.RFC3339)

	targetID, err := store.UpsertTarget("test-pg", "localhost", 5432, "testdb", "arq", "disable", "NONE", "", true)
	if err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}

	for _, q := range pgqueries.All() {
		if err := store.UpsertQueryCatalog(db.QueryCatalogRow{
			QueryID:        q.ID,
			Category:       q.Category,
			ResultKind:     string(q.ResultKind),
			RetentionClass: string(q.RetentionClass),
			RegisteredAt:   now,
		}); err != nil {
			t.Fatalf("UpsertQueryCatalog(%s): %v", q.ID, err)
		}
	}

	if err := store.InsertSnapshot(db.Snapshot{
		ID:          "snap-001",
		TargetID:    targetID,
		CollectedAt: now,
		PGVersion:   "PostgreSQL 18.4",
		Payload:     json.RawMessage(`{"version":"PostgreSQL 18.4"}`),
		SizeBytes:   42,
	}); err != nil {
		t.Fatalf("InsertSnapshot: %v", err)
	}

	var results []db.QueryResult
	for i := range runs {
		runs[i].TargetID = targetID
		runs[i].SnapshotID = "snap-001"
		runs[i].CollectedAt = now
		runs[i].CreatedAt = now
		runs[i].PGVersion = "PostgreSQL 18.4"
		if runs[i].Status == "success" {
			payload, compressed, sizeBytes, err := db.EncodeNDJSON([]map[string]any{{"k": "v"}})
			if err != nil {
				t.Fatalf("EncodeNDJSON: %v", err)
			}
			results = append(results, db.QueryResult{
				RunID:      runs[i].ID,
				Payload:    payload,
				Compressed: compressed,
				SizeBytes:  sizeBytes,
			})
		}
	}
	if err := store.InsertQueryRunBatch(runs, results); err != nil {
		t.Fatalf("InsertQueryRunBatch: %v", err)
	}
}

// runRowsByID reads query_runs.ndjson from a fresh export of the store and
// indexes the rows by run id.
func runRowsByID(t *testing.T, store *db.DB) map[string]map[string]any {
	t.Helper()
	_, zr := buildExportZIP(t, store)
	rows := readZipNDJSONRows(t, zr, "query_runs.ndjson")
	byID := make(map[string]map[string]any, len(rows))
	for _, row := range rows {
		id, _ := row["id"].(string)
		byID[id] = row
	}
	return byID
}

// TestExportQueryRunsCarryStatusAndReason verifies a success run's export row
// carries status/reason alongside the nine pre-existing fields.
// Traces: R118 / TC-EQRS-01
func TestExportQueryRunsCarryStatusAndReason(t *testing.T) {
	store := openTestDB(t)
	seedRunStatusData(t, store, []db.QueryRun{{
		ID:       "run-ok",
		QueryID:  "pg_settings_v1",
		RowCount: 1,
		Status:   "success",
	}})

	row, ok := runRowsByID(t, store)["run-ok"]
	if !ok {
		t.Fatal("run-ok not found in query_runs.ndjson")
	}

	for _, field := range []string{
		"id", "target_id", "snapshot_id", "query_id", "collected_at",
		"pg_version", "duration_ms", "row_count", "error", "status", "reason",
	} {
		if _, ok := row[field]; !ok {
			t.Errorf("query_runs.ndjson row missing field %q", field)
		}
	}
	if got := row["status"]; got != "success" {
		t.Errorf("status = %v, want %q", got, "success")
	}
	if got := row["reason"]; got != "" {
		t.Errorf("reason = %v, want empty", got)
	}
}

// TestExportQueryRunsOwnerOnlySkipDistinguishable verifies an R116
// owner-only skip and a genuine permission failure are distinguishable from
// their export rows alone, with the skip's driver error text preserved.
// Traces: R118 / TC-EQRS-02, TC-EQRS-03
func TestExportQueryRunsOwnerOnlySkipDistinguishable(t *testing.T) {
	const skipErr = "ERROR: permission denied for table pg_statistic_ext_data (SQLSTATE 42501)"

	store := openTestDB(t)
	seedRunStatusData(t, store, []db.QueryRun{
		{
			ID:      "run-skip",
			QueryID: "pg_statistic_ext_data_v1",
			Error:   skipErr,
			Status:  "skipped",
			Reason:  "privilege_owner_only",
		},
		{
			ID:      "run-fail",
			QueryID: "pg_stat_statements_v1",
			Error:   "ERROR: permission denied for view pg_stat_statements (SQLSTATE 42501)",
			Status:  "failed",
			Reason:  "permission_denied",
		},
	})

	byID := runRowsByID(t, store)

	skip, ok := byID["run-skip"]
	if !ok {
		t.Fatal("run-skip not found in query_runs.ndjson")
	}
	if got := skip["status"]; got != "skipped" {
		t.Errorf("skip status = %v, want %q", got, "skipped")
	}
	if got := skip["reason"]; got != "privilege_owner_only" {
		t.Errorf("skip reason = %v, want %q", got, "privilege_owner_only")
	}
	if got := skip["error"]; got != skipErr {
		t.Errorf("skip error = %v, want it preserved verbatim", got)
	}

	fail, ok := byID["run-fail"]
	if !ok {
		t.Fatal("run-fail not found in query_runs.ndjson")
	}
	if got := fail["status"]; got != "failed" {
		t.Errorf("fail status = %v, want %q", got, "failed")
	}
	if got := fail["reason"]; got != "permission_denied" {
		t.Errorf("fail reason = %v, want %q", got, "permission_denied")
	}

	if skip["status"] == fail["status"] {
		t.Error("owner-only skip and genuine failure export the same status — INV-01 violated")
	}
}

// TestExportQueryRunsUnknownStatusVerbatim verifies an out-of-set persisted
// status is exported verbatim, never coerced.
// Traces: FC-01 / TC-EQRS-04
func TestExportQueryRunsUnknownStatusVerbatim(t *testing.T) {
	store := openTestDB(t)
	seedRunStatusData(t, store, []db.QueryRun{{
		ID:      "run-odd",
		QueryID: "pg_settings_v1",
		Status:  "quarantined",
	}})

	row, ok := runRowsByID(t, store)["run-odd"]
	if !ok {
		t.Fatal("run-odd not found in query_runs.ndjson")
	}
	if got := row["status"]; got != "quarantined" {
		t.Errorf("status = %v, want %q emitted verbatim", got, "quarantined")
	}
}
