package singlestore

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/elevarq/signals/snapshot"
)

// Tests derive from specifications/singlestore-snapshot-assembly.acceptance.md.

func sampleResults() []Result {
	return []Result{
		{CollectorID: "singlestore_identity_v1", Category: "server", Rows: []map[string]any{{"memsql_version": "8.5.22"}}},
		{CollectorID: "singlestore_aggregators_v1", Category: "topology", Rows: []map[string]any{{"HOST": "10.0.0.1"}}},
		{CollectorID: "singlestore_plancache_v1", Category: "workload", Rows: []map[string]any{{"PLAN_ID": "p1"}}},
	}
}

func buildInfo() BuildInfo {
	return BuildInfo{
		CollectorVersion: "poc-singlestore",
		CollectedAt:      "2026-10-19T00:00:00Z",
		Target:           snapshot.TargetInfo{Name: "ss", Host: "h", Port: 3306, DBName: "d"},
	}
}

func TestBuildSnapshotDeclaresSingleStore(t *testing.T) {
	// TC-SSA-01
	s := BuildSnapshot(buildInfo(), "singlestore", "8.5.22", sampleResults())
	b, err := s.MetadataJSON()
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}
	js := string(b)
	if !strings.Contains(js, `"engine": "singlestore"`) || !strings.Contains(js, `"engine_version": "8.5.22"`) {
		t.Fatalf("metadata must declare singlestore + version: %s", js)
	}
	if !strings.Contains(js, `"schema_version": "signals-snapshot.v1"`) {
		t.Fatalf("schema_version must be v1: %s", js)
	}
	if strings.Contains(js, `"pg_version": "`) && !strings.Contains(js, `"pg_version": ""`) {
		// pg_version is not omitempty, so it may appear empty; it must not be populated.
		t.Fatalf("pg_version must be empty for singlestore: %s", js)
	}
}

func TestQueryResultsNDJSONOnePerCollectorInOrder(t *testing.T) {
	// TC-SSA-02
	s := BuildSnapshot(buildInfo(), "singlestore", "8.5.22", sampleResults())
	b, err := s.QueryResultsNDJSON()
	if err != nil {
		t.Fatalf("ndjson: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 ndjson lines, got %d", len(lines))
	}
	wantOrder := []string{"singlestore_identity_v1", "singlestore_aggregators_v1", "singlestore_plancache_v1"}
	for i, ln := range lines {
		var row ndjsonRow
		if err := json.Unmarshal([]byte(ln), &row); err != nil {
			t.Fatalf("line %d invalid json: %v", i, err)
		}
		if row.CollectorID != wantOrder[i] {
			t.Fatalf("line %d collector=%q, want %q", i, row.CollectorID, wantOrder[i])
		}
	}
}

func TestWriteZIPManifestIntegrity(t *testing.T) {
	// TC-SSA-03
	s := BuildSnapshot(buildInfo(), "singlestore", "8.5.22", sampleResults())
	var buf bytes.Buffer
	if err := s.WriteZIP(&buf); err != nil {
		t.Fatalf("zip: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("read zip: %v", err)
	}
	contents := map[string][]byte{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		data, _ := io.ReadAll(rc)
		rc.Close()
		contents[f.Name] = data
	}
	for _, want := range []string{"metadata.json", "query_results.ndjson", "manifest.json"} {
		if _, ok := contents[want]; !ok {
			t.Fatalf("zip missing %s", want)
		}
	}
	var manifest map[string]string
	if err := json.Unmarshal(contents["manifest.json"], &manifest); err != nil {
		t.Fatalf("manifest json: %v", err)
	}
	for _, name := range []string{"metadata.json", "query_results.ndjson"} {
		sum := sha256.Sum256(contents[name])
		if manifest[name] != hex.EncodeToString(sum[:]) {
			t.Fatalf("INV-SSA-02: manifest hash mismatch for %s", name)
		}
	}
}

func TestBuildSnapshotPostgreSQLLeavesEngineEmpty(t *testing.T) {
	// TC-SSA-04
	s := BuildSnapshot(buildInfo(), "postgresql", "18.2", sampleResults())
	b, _ := s.MetadataJSON()
	if strings.Contains(string(b), `"engine"`) {
		t.Fatalf("postgresql must not emit an engine key: %s", string(b))
	}
}
