package export

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/elevarq/signals/internal/db"
)

// Tests derive from specifications/engine-identity.acceptance.md.

// TC-EID-06 (EID-R004, INV-EID-02): a PostgreSQL export's metadata.json carries
// an explicit engine field. The field is additive; the rest of the export is
// unchanged (guarded by the M0 PostgreSQL lock + golden).
func TestExportMetadataCarriesEngine(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "x.db"), false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	targetID, err := store.UpsertTarget("t", "h", 5432, "postgres", "u", "disable", "none", "", true)
	if err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}
	if err := store.InsertSnapshot(db.Snapshot{
		ID: "snap-1", TargetID: targetID, CollectedAt: "2026-10-23T00:00:00Z",
		PGVersion: "16.0", Engine: "postgresql",
		Payload: json.RawMessage(`{}`), SizeBytes: 2,
	}); err != nil {
		t.Fatalf("InsertSnapshot: %v", err)
	}

	var buf bytes.Buffer
	if err := NewBuilder(store, "inst").WriteTo(&buf, Options{TargetID: targetID}); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	for _, f := range zr.File {
		if f.Name != "metadata.json" {
			continue
		}
		rc, _ := f.Open()
		var meta map[string]any
		if err := json.NewDecoder(rc).Decode(&meta); err != nil {
			rc.Close()
			t.Fatalf("decode metadata.json: %v", err)
		}
		rc.Close()
		if meta["engine"] != "postgresql" {
			t.Fatalf("metadata.json engine = %v, want postgresql", meta["engine"])
		}
		return
	}
	t.Fatal("metadata.json not present in export")
}
