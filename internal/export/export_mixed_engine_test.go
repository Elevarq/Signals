package export

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/elevarq/signals/internal/db"
)

// #529 / A-09: engine identity is scoped to ONE collection. An export whose
// scope mixes DISTINCT engines must be REFUSED (no first-snapshot-wins
// stamping); a single-engine scope — including PostgreSQL history that spans a
// legacy pre-engine snapshot and a post-engine snapshot — must succeed.

func newExportStore(t *testing.T) *db.DB {
	t.Helper()
	store, err := db.Open(t.TempDir()+"/signals.db", false)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	if err := store.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return store
}

func TestExport_MixedEngineScopeRefused(t *testing.T) {
	store := newExportStore(t)
	pgTarget, err := store.UpsertTarget("pg", "pg.internal", 5432, "app", "svc", "disable", "none", "", false)
	if err != nil {
		t.Fatalf("UpsertTarget pg: %v", err)
	}
	ssTarget, err := store.UpsertTarget("ss", "ss.internal", 3306, "app", "svc", "disable", "none", "", true)
	if err != nil {
		t.Fatalf("UpsertTarget ss: %v", err)
	}
	if err := store.InsertSnapshot(db.Snapshot{
		ID: "snap-pg", TargetID: pgTarget, CollectedAt: "2026-10-21T00:00:00Z",
		PGVersion: "17.2", Engine: "postgresql", Payload: json.RawMessage(`{}`), SizeBytes: 2,
	}); err != nil {
		t.Fatalf("InsertSnapshot pg: %v", err)
	}
	if err := store.InsertSnapshot(db.Snapshot{
		ID: "snap-ss", TargetID: ssTarget, CollectedAt: "2026-10-21T00:00:00Z",
		PGVersion: "8.5.22", Engine: "singlestore", Payload: json.RawMessage(`{}`), SizeBytes: 2,
	}); err != nil {
		t.Fatalf("InsertSnapshot ss: %v", err)
	}

	// Options{} → all targets → scope spans both engines.
	var buf bytes.Buffer
	err = NewBuilder(store, "inst").WriteTo(&buf, Options{All: true})
	if err == nil {
		t.Fatalf("#529 A-09 gap: a mixed-engine export scope must be refused, got success")
	}
	if !strings.Contains(err.Error(), "mixed-engine") {
		t.Errorf("error = %v, want a mixed-engine refusal", err)
	}
}

func TestExport_SingleEnginePGHistoryWithLegacySnapshotAccepted(t *testing.T) {
	store := newExportStore(t)
	tgt, err := store.UpsertTarget("pg", "pg.internal", 5432, "app", "svc", "disable", "none", "", false)
	if err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}
	// Legacy pre-engine snapshot (Engine == "" → postgresql) + an explicit
	// postgresql snapshot: the SAME engine, so NOT mixed.
	if err := store.InsertSnapshot(db.Snapshot{
		ID: "snap-legacy", TargetID: tgt, CollectedAt: "2026-10-20T00:00:00Z",
		PGVersion: "16.1", Engine: "", Payload: json.RawMessage(`{}`), SizeBytes: 2,
	}); err != nil {
		t.Fatalf("InsertSnapshot legacy: %v", err)
	}
	if err := store.InsertSnapshot(db.Snapshot{
		ID: "snap-modern", TargetID: tgt, CollectedAt: "2026-10-21T00:00:00Z",
		PGVersion: "17.2", Engine: "postgresql", Payload: json.RawMessage(`{}`), SizeBytes: 2,
	}); err != nil {
		t.Fatalf("InsertSnapshot modern: %v", err)
	}

	var buf bytes.Buffer
	if err := NewBuilder(store, "inst").WriteTo(&buf, Options{All: true, TargetID: tgt}); err != nil {
		t.Fatalf("single-engine PG history must export, got: %v", err)
	}
	_, meta := inspectConformanceZIP(t, buf.Bytes())
	if meta["engine"] != "postgresql" {
		t.Errorf("metadata engine = %q, want postgresql", meta["engine"])
	}
}
