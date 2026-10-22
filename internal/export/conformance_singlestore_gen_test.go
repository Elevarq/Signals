package export

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"os"
	"testing"

	"github.com/elevarq/signals/internal/db"
)

// TestGenerateSingleStoreConformanceZIP produces a SingleStore snapshot ZIP
// through the SAME production export serialization Signals uses for customer
// snapshots (export.Builder.WriteTo), from controlled SingleStore evidence
// rows. The DB/network boundary is faked (the rows are injected), but the
// resulting artifact is produced by production code and therefore truthfully
// satisfies "this ZIP could have been produced by Signals".
//
// It is the producer half of the cross-repo engine-conformance seam: set
// CONFORMANCE_ZIP_OUT to the downstream (Analyzer) testdata path to refresh
// the committed seam fixture; the test logs the engine metadata, the ZIP's
// internal file list, and the SHA-256 so the provenance is recorded. The
// evidence deterministically triggers the three demo findings
// (rowstore_memory_concentration, expensive_cached_query, long_running_query)
// plus the other POC SingleStore rules.
func TestGenerateSingleStoreConformanceZIP(t *testing.T) {
	store, err := db.Open(t.TempDir()+"/signals.db", false)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	if err := store.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	targetID, err := store.UpsertTarget(
		"ss-conformance", "ss.conformance.internal", 3306, "app",
		"svc", "disable", "none", "", true)
	if err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}

	const (
		snapID  = "snap-ss-conformance-1"
		nowTS   = "2026-10-21T00:00:00Z"
		version = "8.5.22"
	)
	if err := store.InsertSnapshot(db.Snapshot{
		ID: snapID, TargetID: targetID, CollectedAt: nowTS,
		PGVersion: version, Engine: "singlestore",
		Payload: json.RawMessage(`{"engine":"singlestore"}`), SizeBytes: 24,
	}); err != nil {
		t.Fatalf("InsertSnapshot: %v", err)
	}

	// Controlled SingleStore evidence (the raw collector column shapes the
	// SingleStore normalizer reads). Deterministically fires the three demo
	// findings and the other POC rules.
	collectors := map[string][]map[string]any{
		"singlestore_identity_v1": {
			// Real identity collector columns (#530): the production SQL is
			// `SELECT @@memsql_version AS memsql_version, @@version_comment AS
			// version_comment, VERSION() AS version` (internal/singlestore/
			// collectors.go) — NOT a VARIABLE_NAME/VARIABLE_VALUE pair (that is
			// the GLOBAL_VARIABLES collector). The downstream Analyzer evidence
			// contract keys on these real columns (Elevarq/Analyzer#3865).
			{"memsql_version": version, "version_comment": "SingleStore", "version": "5.7.32"},
		},
		"singlestore_table_statistics_v1": {
			{"DATABASE_NAME": "app", "TABLE_NAME": "events", "STORAGE_TYPE": "rowstore", "MEMORY_USE": 800, "ROWS": 20000000},
			{"DATABASE_NAME": "app", "TABLE_NAME": "mid", "STORAGE_TYPE": "rowstore", "MEMORY_USE": 150, "ROWS": 1000},
			{"DATABASE_NAME": "app", "TABLE_NAME": "small", "STORAGE_TYPE": "rowstore", "MEMORY_USE": 50, "ROWS": 1000},
		},
		"singlestore_plancache_v1": {
			{"DATABASE_NAME": "app", "PLAN_ID": "p1", "AVERAGE_EXEC_TIME": 200, "COMMITS": 40},
		},
		"singlestore_processlist_v1": {
			{"NODE_ID": 1, "ID": "42", "COMMAND": "Query", "TIME": 120, "STATE": "executing", "DB": "app", "USER": "svc"},
		},
	}

	order := []string{
		"singlestore_identity_v1",
		"singlestore_table_statistics_v1",
		"singlestore_plancache_v1",
		"singlestore_processlist_v1",
	}
	var runs []db.QueryRun
	var results []db.QueryResult
	for _, coll := range order {
		rows := collectors[coll]
		payload, compressed, sz, err := db.EncodeNDJSON(rows)
		if err != nil {
			t.Fatalf("EncodeNDJSON %s: %v", coll, err)
		}
		runID := "run-" + coll
		runs = append(runs, db.QueryRun{
			ID: runID, TargetID: targetID, SnapshotID: snapID, QueryID: coll,
			CollectedAt: nowTS, PGVersion: version, DurationMS: 1,
			RowCount: len(rows), Status: "success", CreatedAt: nowTS,
		})
		results = append(results, db.QueryResult{
			RunID: runID, Payload: payload, Compressed: compressed, SizeBytes: sz,
		})
	}
	if err := store.InsertQueryRunBatch(runs, results); err != nil {
		t.Fatalf("InsertQueryRunBatch: %v", err)
	}

	// Production export serialization.
	builder := NewBuilder(store, "ss-conformance-instance")
	var buf bytes.Buffer
	if err := builder.WriteTo(&buf, Options{TargetID: targetID}); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	sum := sha256.Sum256(buf.Bytes())
	t.Logf("SingleStore conformance ZIP: %d bytes, sha256=%x", buf.Len(), sum)

	files, meta := inspectConformanceZIP(t, buf.Bytes())
	t.Logf("ZIP files: %v", files)
	t.Logf("metadata engine=%q engine_version=%q schema_version=%q",
		meta["engine"], meta["engine_version"], meta["schema_version"])
	if meta["engine"] != "singlestore" {
		t.Fatalf("metadata engine = %q, want singlestore", meta["engine"])
	}
	if meta["engine_version"] != version {
		t.Fatalf("metadata engine_version = %q, want %s", meta["engine_version"], version)
	}
	for _, need := range []string{"metadata.json", "query_runs.ndjson", "query_results.ndjson"} {
		if !files[need] {
			t.Fatalf("production ZIP missing Analyzer-required file %q (have %v)", need, files)
		}
	}

	if out := os.Getenv("CONFORMANCE_ZIP_OUT"); out != "" {
		if err := os.WriteFile(out, buf.Bytes(), 0o644); err != nil {
			t.Fatalf("write CONFORMANCE_ZIP_OUT: %v", err)
		}
		t.Logf("wrote conformance ZIP to %s", out)
	}
}

func inspectConformanceZIP(t *testing.T, b []byte) (files map[string]bool, meta map[string]string) {
	t.Helper()
	files = map[string]bool{}
	meta = map[string]string{}
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	for _, f := range zr.File {
		files[f.Name] = true
		if f.Name == "metadata.json" {
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("open metadata.json: %v", err)
			}
			var raw map[string]any
			if err := json.NewDecoder(rc).Decode(&raw); err != nil {
				_ = rc.Close()
				t.Fatalf("decode metadata.json: %v", err)
			}
			_ = rc.Close()
			for _, k := range []string{"engine", "engine_version", "schema_version"} {
				if v, ok := raw[k].(string); ok {
					meta[k] = v
				}
			}
		}
	}
	return files, meta
}
