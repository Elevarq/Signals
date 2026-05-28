// Tests for the latest-run-per-collector default export scope (R084
// revised), the run_scope metadata marker (R086), and collector
// freshness metadata (R107). Added for issue #5.
//
// Spec: features/arq-signals/specification.md
//   ARQ-SIGNALS-R084 (default scope = latest run per collector per
//                     active target)
//   ARQ-SIGNALS-R086 (metadata run_scope marker)
//   ARQ-SIGNALS-R107 (collector freshness: collected_at/cadence/freshness)
// Spec: features/arq-signals/acceptance-tests.md
//   TC-SIG-121 (mixed-cadence completeness)
//   TC-SIG-122 (run_scope marker)
//   TC-SIG-123 (freshness metadata)

package tests

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/elevarq/arq-signals/internal/db"
	"github.com/elevarq/arq-signals/internal/export"
	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// seedRun inserts one snapshot carrying a single collector run (and,
// for success runs, a trivial result payload) for the given target.
func seedRun(t *testing.T, store *db.DB, snapID string, targetID int64, queryID, collectedAt, status string) {
	t.Helper()
	snap := db.Snapshot{
		ID:          snapID,
		TargetID:    targetID,
		CollectedAt: collectedAt,
		PGVersion:   "PostgreSQL 18.0",
		Payload:     json.RawMessage(`{}`),
	}
	run := db.QueryRun{
		ID:          "run-" + snapID + "-" + queryID,
		TargetID:    targetID,
		SnapshotID:  snapID,
		QueryID:     queryID,
		CollectedAt: collectedAt,
		PGVersion:   "PostgreSQL 18.0",
		CreatedAt:   collectedAt,
		Status:      status,
	}
	var results []db.QueryResult
	if status == "success" {
		results = []db.QueryResult{{
			RunID:     run.ID,
			Payload:   []byte("{\"k\":\"v\"}\n"),
			SizeBytes: 8,
		}}
	}
	if err := store.InsertCollectionAtomic(snap, []db.QueryRun{run}, results); err != nil {
		t.Fatalf("InsertCollectionAtomic %s/%s: %v", snapID, queryID, err)
	}
}

// collectorStatusByID parses collector_status.json from the ZIP and
// returns the entries keyed by collector id (last wins — adequate for
// the single-target fixtures here).
func collectorStatusByID(t *testing.T, store *db.DB, opts export.Options) map[string]map[string]any {
	t.Helper()
	_, zr, err := buildExportZIPWithOpts(t, store, opts)
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	raw := readZipFileJSON(t, zr, "collector_status.json")
	out := map[string]map[string]any{}
	cols, _ := raw["collectors"].([]any)
	for _, c := range cols {
		m, ok := c.(map[string]any)
		if !ok {
			continue
		}
		id, _ := m["id"].(string)
		out[id] = m
	}
	return out
}

// TC-SIG-121 — default export includes a lower-cadence collector whose
// latest run lives in an older snapshot than the newest cycle.
func TestExportDefaultScopeIncludesLowerCadenceCollector(t *testing.T) {
	store := openTestDB(t)
	if _, err := store.UpsertTarget("target-A", "host-a", 5432, "postgres", "arq", "disable", "NONE", "", true); err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}

	// S1 (older): both a 5m and a 24h collector ran.
	seedRun(t, store, "snap-1", 1, "cadence_5m_v1", "2026-04-25T10:00:00Z", "success")
	seedRun(t, store, "snap-1b", 1, "cadence_24h_v1", "2026-04-25T10:00:00Z", "success")
	// S2 (newer): only the 5m collector was due.
	seedRun(t, store, "snap-2", 1, "cadence_5m_v1", "2026-04-25T10:05:00Z", "success")

	_, zr, err := buildExportZIPWithOpts(t, store, export.Options{})
	if err != nil {
		t.Fatalf("WriteTo (default): %v", err)
	}

	// query_runs.ndjson must contain the latest run of EACH collector:
	// cadence_5m_v1 from snap-2 and cadence_24h_v1 from snap-1b.
	runSnap := map[string]string{} // query_id -> snapshot_id
	qids := readZipNDJSONField(t, zr, "query_runs.ndjson", "query_id")
	sids := readZipNDJSONField(t, zr, "query_runs.ndjson", "snapshot_id")
	if len(qids) != len(sids) {
		t.Fatalf("query_runs field length mismatch: %d ids vs %d snaps", len(qids), len(sids))
	}
	for i := range qids {
		runSnap[qids[i]] = sids[i]
	}

	if _, ok := runSnap["cadence_24h_v1"]; !ok {
		t.Errorf("default export dropped cadence_24h_v1 — this is the R084 completeness regression (issue #5). runs=%v", runSnap)
	}
	if got := runSnap["cadence_5m_v1"]; got != "snap-2" {
		t.Errorf("cadence_5m_v1 latest run snapshot = %q, want snap-2", got)
	}
	if got := runSnap["cadence_24h_v1"]; got != "snap-1b" {
		t.Errorf("cadence_24h_v1 latest run snapshot = %q, want snap-1b", got)
	}

	// snapshots.ndjson must reference both contributing snapshots.
	snaps := map[string]bool{}
	for _, id := range readZipNDJSONField(t, zr, "snapshots.ndjson", "id") {
		snaps[id] = true
	}
	if !snaps["snap-1b"] || !snaps["snap-2"] {
		t.Errorf("snapshots.ndjson must contain snap-1b and snap-2; got %v", snaps)
	}

	meta := readZipFileJSON(t, zr, "metadata.json")
	if got := int(meta["snapshot_count"].(float64)); got != 2 {
		t.Errorf("snapshot_count = %d, want 2", got)
	}
}

// TC-SIG-122 — run_scope metadata marker.
func TestExportRunScopeMarker(t *testing.T) {
	store := openTestDB(t)
	if _, err := store.UpsertTarget("target-A", "host-a", 5432, "postgres", "arq", "disable", "NONE", "", true); err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}
	seedRun(t, store, "snap-1", 1, "cadence_5m_v1", "2026-04-25T10:00:00Z", "success")

	cases := []struct {
		name string
		opts export.Options
		want string
	}{
		{"default", export.Options{}, "latest-per-collector"},
		{"all", export.Options{All: true}, "snapshot"},
		{"snapshot-id", export.Options{SnapshotID: "snap-1"}, "snapshot"},
		{"since-until", export.Options{Since: "2026-04-25T00:00:00Z", Until: "2026-04-26T00:00:00Z"}, "snapshot"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, zr, err := buildExportZIPWithOpts(t, store, tc.opts)
			if err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			meta := readZipFileJSON(t, zr, "metadata.json")
			if got, _ := meta["run_scope"].(string); got != tc.want {
				t.Errorf("run_scope = %q, want %q", got, tc.want)
			}
		})
	}
}

// TC-SIG-123 — collector freshness metadata (fresh / stale / never_run).
func TestExportCollectorFreshness(t *testing.T) {
	store := openTestDB(t)
	if _, err := store.UpsertTarget("target-A", "host-a", 5432, "postgres", "arq", "disable", "NONE", "", true); err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}

	fresh5m := pickByCadence(t, pgqueries.Cadence5m)
	staleDaily := pickByCadence(t, pgqueries.CadenceDaily)
	neverRun := pickAnotherCollector(t, fresh5m, staleDaily)

	now := time.Now().UTC()
	seedRun(t, store, "snap-fresh", 1, fresh5m, now.Add(-30*time.Second).Format(time.RFC3339), "success")
	seedRun(t, store, "snap-stale", 1, staleDaily, now.Add(-72*time.Hour).Format(time.RFC3339), "success")

	// never_run enumeration is target-scoped (the flat instance-level
	// status file has no target attribution), so scope to the target.
	byID := collectorStatusByID(t, store, export.Options{TargetID: 1})

	if e := byID[fresh5m]; e == nil {
		t.Errorf("%s missing from collector_status", fresh5m)
	} else {
		if e["freshness"] != "fresh" {
			t.Errorf("%s freshness = %v, want fresh", fresh5m, e["freshness"])
		}
		if e["cadence"] != "5m" {
			t.Errorf("%s cadence = %v, want 5m", fresh5m, e["cadence"])
		}
	}

	if e := byID[staleDaily]; e == nil {
		t.Errorf("%s missing from collector_status", staleDaily)
	} else if e["freshness"] != "stale" {
		t.Errorf("%s freshness = %v, want stale (72h > 2x24h)", staleDaily, e["freshness"])
	}

	if e := byID[neverRun]; e == nil {
		t.Errorf("eligible-but-never-run collector %s should appear as a never_run entry, not be absent", neverRun)
	} else if e["freshness"] != "never_run" {
		t.Errorf("%s freshness = %v, want never_run", neverRun, e["freshness"])
	}
}

func pickByCadence(t *testing.T, cad pgqueries.Cadence) string {
	t.Helper()
	for _, q := range pgqueries.All() {
		if q.Cadence == cad {
			return q.ID
		}
	}
	t.Fatalf("no registered collector with cadence %s", cad)
	return ""
}

func pickAnotherCollector(t *testing.T, exclude ...string) string {
	t.Helper()
	ex := map[string]bool{}
	for _, e := range exclude {
		ex[e] = true
	}
	for _, q := range pgqueries.All() {
		if !ex[q.ID] {
			return q.ID
		}
	}
	t.Fatalf("no spare registered collector to use as never_run")
	return ""
}
