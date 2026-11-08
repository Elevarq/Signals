package tests

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/elevarq/signals/internal/db"
	"github.com/elevarq/signals/internal/export"
)

// #716 / F10 / R094.1: a snapshot's exported target_identity must reflect the
// identity it was COLLECTED under, not the current (mutable) targets row. Target
// rows are upserted by name and overwrite their connection identity, so reusing
// or retargeting a target name to another database must NOT relabel historical
// evidence as the new database.

func firstNDJSONRow(t *testing.T, zr *zip.Reader, name string) map[string]any {
	t.Helper()
	raw := readZipFile(t, zr, name)
	line := strings.SplitN(strings.TrimSpace(string(raw)), "\n", 2)[0]
	var row map[string]any
	if err := json.Unmarshal([]byte(line), &row); err != nil {
		t.Fatalf("parse first %s row: %v\n%s", name, err, line)
	}
	return row
}

func exportZIP(t *testing.T, store *db.DB, targetID int64) *zip.Reader {
	t.Helper()
	builder := export.NewBuilder(store, "inst")
	var buf bytes.Buffer
	// All + TargetID: the snapshot enters snapshots.ndjson via GetSnapshotsByTarget
	// (no query_runs needed) and TargetID>0 keeps the metadata top-level identity.
	if err := builder.WriteTo(&buf, export.Options{TargetID: targetID, All: true}); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	return zr
}

func TestExport_HistoricalSnapshotKeepsSourceIdentityAfterRetarget(t *testing.T) {
	store := openTestDB(t)

	// Target "app" originally points at olddb on old-host; one snapshot is
	// collected under that identity (source_* captured at collection time).
	tid, err := store.UpsertTarget("app", "old-host", 5432, "olddb", "monitor", "require", "FILE", "/etc/x.pw", true)
	if err != nil {
		t.Fatalf("UpsertTarget (old): %v", err)
	}
	if err := store.InsertSnapshot(db.Snapshot{
		ID: "hist-1", TargetID: tid, CollectedAt: time.Now().UTC().Format(time.RFC3339),
		PGVersion: "PostgreSQL 16", Payload: json.RawMessage(`{}`), SizeBytes: 2,
		SourceHost: "old-host", SourcePort: 5432, SourceDB: "olddb", SourceUser: "monitor",
	}); err != nil {
		t.Fatalf("InsertSnapshot: %v", err)
	}

	// The operator retargets the SAME target name to a different database. This
	// overwrites the mutable targets row (same id, new host/db).
	if _, err := store.UpsertTarget("app", "new-host", 5432, "newdb", "monitor", "require", "FILE", "/etc/x.pw", true); err != nil {
		t.Fatalf("UpsertTarget (retarget): %v", err)
	}

	zr := exportZIP(t, store, tid)

	// Per-snapshot identity in snapshots.ndjson must be the OLD identity.
	row := firstNDJSONRow(t, zr, "snapshots.ndjson")
	ident, _ := row["target_identity"].(map[string]any)
	if ident == nil {
		t.Fatalf("snapshots.ndjson row missing target_identity: %v", row)
	}
	if ident["host"] != "old-host" || ident["dbname"] != "olddb" {
		t.Errorf("historical snapshot was relabeled to the retargeted database: host=%v dbname=%v, want old-host/olddb (#716)", ident["host"], ident["dbname"])
	}

	// The metadata.json top-level block (single-source scope) must also be the
	// OLD identity, not the retargeted current row.
	meta := readZipFileJSON(t, zr, "metadata.json")
	mident, _ := meta["target_identity"].(map[string]any)
	if mident == nil {
		t.Fatalf("metadata.json missing target_identity: %v", meta)
	}
	if mident["host"] != "old-host" || mident["dbname"] != "olddb" {
		t.Errorf("metadata top-level identity was relabeled: host=%v dbname=%v, want old-host/olddb", mident["host"], mident["dbname"])
	}
}

// A snapshot with no captured source identity (pre-#716, pre-backfill / orphan)
// falls back to the current targets row — backward compatibility for old data.
func TestExport_EmptySourceIdentityFallsBackToTargetsRow(t *testing.T) {
	store := openTestDB(t)
	tid, err := store.UpsertTarget("app", "cur-host", 5432, "curdb", "monitor", "require", "FILE", "/etc/x.pw", true)
	if err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}
	// Source identity deliberately left empty (models a pre-#716 snapshot).
	if err := store.InsertSnapshot(db.Snapshot{
		ID: "legacy-1", TargetID: tid, CollectedAt: time.Now().UTC().Format(time.RFC3339),
		PGVersion: "PostgreSQL 16", Payload: json.RawMessage(`{}`), SizeBytes: 2,
	}); err != nil {
		t.Fatalf("InsertSnapshot: %v", err)
	}

	zr := exportZIP(t, store, tid)
	row := firstNDJSONRow(t, zr, "snapshots.ndjson")
	ident, _ := row["target_identity"].(map[string]any)
	if ident == nil {
		t.Fatalf("expected a fallback target_identity from the targets row: %v", row)
	}
	if ident["host"] != "cur-host" || ident["dbname"] != "curdb" {
		t.Errorf("empty-source snapshot did not fall back to the current targets row: host=%v dbname=%v", ident["host"], ident["dbname"])
	}
}
