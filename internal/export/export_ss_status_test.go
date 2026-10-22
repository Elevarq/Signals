package export

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"testing"

	"github.com/elevarq/signals/internal/db"
	"github.com/elevarq/signals/internal/pgqueries"
	"github.com/elevarq/signals/internal/singlestore"
)

// #534 / A-08: a target-scoped SingleStore export's collector_status must draw
// never-run / coverage entries from the SingleStore collector catalog — NOT the
// PostgreSQL catalog. Before the fix a SingleStore export claimed every
// PostgreSQL collector "never_run", fabricating false coverage gaps.
func TestExport_SSStatusUsesSSCatalogNotPG(t *testing.T) {
	store := newExportStore(t)
	tgt, err := store.UpsertTarget("ss", "ss.internal", 3306, "app", "svc", "disable", "none", "", true)
	if err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}
	const snapID = "snap-ss"
	if err := store.InsertSnapshot(db.Snapshot{
		ID: snapID, TargetID: tgt, CollectedAt: "2026-10-21T00:00:00Z",
		PGVersion: "8.5.22", Engine: "singlestore", Payload: json.RawMessage(`{}`), SizeBytes: 2,
	}); err != nil {
		t.Fatalf("InsertSnapshot: %v", err)
	}
	// One SingleStore collector actually ran; the rest of the SS catalog is
	// "never_run". No PostgreSQL collector ran.
	payload, compressed, sz, err := db.EncodeNDJSON([]map[string]any{{"memsql_version": "8.5.22"}})
	if err != nil {
		t.Fatalf("EncodeNDJSON: %v", err)
	}
	if err := store.InsertQueryRunBatch(
		[]db.QueryRun{{
			ID: "run-id", TargetID: tgt, SnapshotID: snapID, QueryID: "singlestore_identity_v1",
			CollectedAt: "2026-10-21T00:00:00Z", PGVersion: "8.5.22", DurationMS: 1,
			RowCount: 1, Status: "success", CreatedAt: "2026-10-21T00:00:00Z",
		}},
		[]db.QueryResult{{RunID: "run-id", Payload: payload, Compressed: compressed, SizeBytes: sz}},
	); err != nil {
		t.Fatalf("InsertQueryRunBatch: %v", err)
	}

	var buf bytes.Buffer
	if err := NewBuilder(store, "inst").WriteTo(&buf, Options{TargetID: tgt}); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	// Read collector_status.json from the ZIP.
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("zip reader: %v", err)
	}
	var statusRaw []byte
	for _, f := range zr.File {
		if f.Name == "collector_status.json" {
			rc, oerr := f.Open()
			if oerr != nil {
				t.Fatalf("open collector_status.json: %v", oerr)
			}
			statusRaw, _ = io.ReadAll(rc)
			_ = rc.Close()
		}
	}
	if statusRaw == nil {
		t.Fatal("collector_status.json missing from export")
	}
	var status struct {
		Collectors []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"collectors"`
	}
	if err := json.Unmarshal(statusRaw, &status); err != nil {
		t.Fatalf("decode collector_status.json: %v", err)
	}

	ssIDs := map[string]bool{}
	for _, c := range singlestore.Catalog() {
		ssIDs[c.ID] = true
	}
	pgIDs := map[string]bool{}
	for _, q := range pgqueries.All() {
		pgIDs[q.ID] = true
	}

	sawSSNeverRun := false
	for _, c := range status.Collectors {
		if pgIDs[c.ID] && !ssIDs[c.ID] {
			t.Errorf("#534 A-08 gap: SingleStore export leaked PostgreSQL collector %q (status=%q)", c.ID, c.Status)
		}
		if ssIDs[c.ID] && c.Status == "never_run" {
			sawSSNeverRun = true
		}
	}
	if !sawSSNeverRun {
		t.Error("expected at least one SingleStore catalog collector reported never_run")
	}
}
