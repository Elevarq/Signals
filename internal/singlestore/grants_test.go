package singlestore

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// Tests derive from specifications/singlestore-grants.acceptance.md.

func TestOnlyClusterStatusIsElevated(t *testing.T) {
	// TC-SSG-01 / INV-SSG-01: exactly cluster_status requires CLUSTER; all
	// others run under the baseline SELECT+PROCESS role.
	for _, c := range Catalog() {
		if c.ID == "singlestore_cluster_status_v1" {
			if c.ElevatedPrivilege != "CLUSTER" {
				t.Fatalf("cluster_status must require CLUSTER, got %q", c.ElevatedPrivilege)
			}
			continue
		}
		if c.ElevatedPrivilege != "" {
			t.Fatalf("SSG: collector %s unexpectedly elevated (%q); baseline is SELECT+PROCESS", c.ID, c.ElevatedPrivilege)
		}
	}
}

func TestGoldenFixtureReplaysIntoSnapshot(t *testing.T) {
	// TC-SSG-02 / #555: the committed golden fixture (a real, redacted capture)
	// replays into a valid engine=singlestore snapshot — CI coverage with no
	// live DB.
	b, err := os.ReadFile("testdata/singlestore_collect_fixture.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var results []Result
	if err := json.Unmarshal(b, &results); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	if len(results) < 8 {
		t.Fatalf("fixture should carry the full catalog, got %d", len(results))
	}

	// #577 fidelity: the version comes from the fixture's OWN identity row (the
	// real captured @@memsql_version), never a hardcoded literal — so the
	// replay cannot silently drift from the producer that generated the fixture.
	ver := ""
	for _, r := range results {
		if r.CollectorID == "singlestore_identity_v1" && len(r.Rows) == 1 {
			if v, ok := r.Rows[0]["memsql_version"].(string); ok {
				ver = v
			}
		}
	}
	if ver == "" {
		t.Fatalf("fixture identity row missing a captured memsql_version")
	}

	snap := BuildSnapshot(BuildInfo{CollectorVersion: "fixture", CollectedAt: "2026-10-20T00:00:00Z"}, "singlestore", ver, results)
	meta, err := snap.MetadataJSON()
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}
	if !strings.Contains(string(meta), `"engine": "singlestore"`) {
		t.Fatalf("snapshot must declare singlestore: %s", meta)
	}
	nd, err := snap.QueryResultsNDJSON()
	if err != nil {
		t.Fatalf("ndjson: %v", err)
	}
	lines := strings.Count(strings.TrimRight(string(nd), "\n"), "\n") + 1
	if lines != len(results) {
		t.Fatalf("ndjson lines %d != collectors %d", lines, len(results))
	}

	// Redaction held in the committed fixture (no SQL text on disk).
	for _, r := range results {
		switch r.CollectorID {
		case "singlestore_plancache_v1":
			for _, row := range r.Rows {
				if v, ok := row["QUERY_TEXT"]; ok && v != nil {
					t.Fatalf("fixture leaked QUERY_TEXT: %v", v)
				}
			}
		case "singlestore_processlist_v1":
			for _, row := range r.Rows {
				if v, ok := row["INFO"]; ok && v != nil {
					t.Fatalf("fixture leaked processlist INFO: %v", v)
				}
			}
		}
	}
}
