package export

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/elevarq/signals/internal/db"
)

// ECD-R007 (#532): the common exporter declares engine/engine_version in
// metadata.json, sourced from the persisted snapshot's engine identity, so a
// consumer (Analyzer) dispatches on engine instead of assuming PostgreSQL. A
// SingleStore snapshot exports engine=singlestore; a PostgreSQL snapshot
// exports engine=postgresql.
func TestExportMetadataDeclaresEngine(t *testing.T) {
	cases := []struct {
		name, engine, version string
	}{
		{"singlestore", "singlestore", "8.5.22"},
		{"postgresql", "postgresql", "17"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, err := db.Open(filepath.Join(t.TempDir(), "x.db"), false)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			t.Cleanup(func() { _ = store.Close() })
			if err := store.Migrate(); err != nil {
				t.Fatalf("Migrate: %v", err)
			}
			tid, err := store.UpsertTarget("t", "h", 3306, "d", "u", "require", "", "", true)
			if err != nil {
				t.Fatalf("UpsertTarget: %v", err)
			}
			now := time.Now().UTC().Format(time.RFC3339)
			payload, compressed, size, err := db.EncodeNDJSON([]map[string]any{{"k": "v"}})
			if err != nil {
				t.Fatalf("EncodeNDJSON: %v", err)
			}
			if err := store.UpsertQueryCatalog(db.QueryCatalogRow{QueryID: "c1", Category: "server", ResultKind: "rowset", RetentionClass: "medium", RegisteredAt: now}); err != nil {
				t.Fatalf("UpsertQueryCatalog: %v", err)
			}
			snap := db.Snapshot{ID: "snap-1", TargetID: tid, CollectedAt: now, PGVersion: tc.version, Engine: tc.engine, Payload: json.RawMessage(`{}`), SizeBytes: 2}
			runs := []db.QueryRun{{ID: "run-1", TargetID: tid, SnapshotID: snap.ID, QueryID: "c1", CollectedAt: now, PGVersion: tc.version, RowCount: 1, CreatedAt: now, Status: "success"}}
			results := []db.QueryResult{{RunID: "run-1", Payload: payload, Compressed: compressed, SizeBytes: size}}
			if err := store.InsertCollectionAtomic(snap, runs, results); err != nil {
				t.Fatalf("InsertCollectionAtomic: %v", err)
			}

			var buf bytes.Buffer
			b := NewBuilder(store, "test-instance")
			if err := b.WriteTo(&buf, Options{TargetID: tid}); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			meta := zipMetadata(t, buf.Bytes())
			if meta["engine"] != tc.engine {
				t.Errorf("metadata.json engine = %v, want %q", meta["engine"], tc.engine)
			}
			if meta["engine_version"] != tc.version {
				t.Errorf("metadata.json engine_version = %v, want %q", meta["engine_version"], tc.version)
			}
		})
	}
}

func zipMetadata(t *testing.T, zipBytes []byte) map[string]any {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	for _, f := range zr.File {
		if f.Name != "metadata.json" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open metadata.json: %v", err)
		}
		defer rc.Close()
		raw, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return m
	}
	t.Fatalf("metadata.json not found")
	return nil
}
