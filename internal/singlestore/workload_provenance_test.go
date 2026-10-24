package singlestore

import (
	"strings"
	"testing"

	"github.com/elevarq/signals/snapshot"
)

// Tests derive from specifications/workload-measurement-provenance.acceptance.md.

func TestWorkloadCollectorsAreRawAndLossless(t *testing.T) {
	// TC-WMP-01 / INV-WMP-01: workload collectors emit raw cumulative counters
	// with no in-collector delta or lossy aggregation.
	workload := map[string]bool{
		"singlestore_plancache_v1":  true,
		"singlestore_activities_v1": true,
	}
	banned := []string{" LAG(", " GROUP BY ", " SUM(", " OVER ("}
	sawCounters := map[string]bool{}
	for _, c := range Catalog() {
		if !workload[c.ID] {
			continue
		}
		up := strings.ToUpper(c.SQL)
		for _, b := range banned {
			if strings.Contains(up, b) {
				t.Fatalf("WMP-R001: workload collector %s must not compute deltas/aggregates in-collector (found %q)", c.ID, strings.TrimSpace(b))
			}
		}
		// Each must select at least one known raw cumulative counter.
		for _, counter := range []string{"COMMITS", "EXECUTION_TIME", "ROWCOUNT", "CPU_TIME", "RUN_COUNT", "ELAPSED_TIME_MS"} {
			if strings.Contains(up, counter) {
				sawCounters[c.ID] = true
			}
		}
	}
	for id := range workload {
		if !sawCounters[id] {
			t.Fatalf("workload collector %s selects no recognizable raw cumulative counter", id)
		}
	}
}

func TestSnapshotCarriesProvenance(t *testing.T) {
	// TC-WMP-02 / INV-WMP-02
	results := []Result{
		{CollectorID: "singlestore_plancache_v1", Category: "workload", Rows: []map[string]any{{"COMMITS": 5}}},
	}
	snap := BuildSnapshot(BuildInfo{
		CollectorVersion: "poc",
		CollectedAt:      "2026-10-20T00:00:00Z",
		Target:           snapshot.TargetInfo{Name: "prov-test", Host: "h", Port: 3306},
	}, "singlestore", "8.5.22", results)

	meta, err := snap.MetadataJSON()
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}
	for _, want := range []string{`"engine": "singlestore"`, `"engine_version": "8.5.22"`, `"collected_at": "2026-10-20T00:00:00Z"`, `"name": "prov-test"`} {
		if !strings.Contains(string(meta), want) {
			t.Fatalf("provenance missing %q in: %s", want, meta)
		}
	}
	nd, err := snap.QueryResultsNDJSON()
	if err != nil {
		t.Fatalf("ndjson: %v", err)
	}
	if !strings.Contains(string(nd), `"collector_id":"singlestore_plancache_v1"`) || !strings.Contains(string(nd), `"category":"workload"`) {
		t.Fatalf("result provenance (collector_id/category) missing: %s", nd)
	}
}
