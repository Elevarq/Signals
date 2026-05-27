package tests

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/elevarq/arq-signals/internal/db"
	"github.com/elevarq/arq-signals/internal/export"
)

// buildExportZIPWithBuilder runs an arbitrary configured Builder to a
// buffer and returns the parsed reader. Variant of buildExportZIP that
// lets the caller toggle SetExportPerCollectorFiles before WriteTo.
func buildExportZIPWithBuilder(t *testing.T, b *export.Builder) *zip.Reader {
	t.Helper()
	var buf bytes.Buffer
	if err := b.WriteTo(&buf, export.Options{}); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	return zr
}

// listPerCollectorFiles returns the names of files inside per-collector/.
func listPerCollectorFiles(zr *zip.Reader) []string {
	var out []string
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "per-collector/") {
			out = append(out, f.Name)
		}
	}
	return out
}

// readZipFile returns the raw bytes of a named ZIP entry.
func readZipFile(t *testing.T, zr *zip.Reader, name string) []byte {
	t.Helper()
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("open %s: %v", name, err)
			}
			defer rc.Close()
			data, err := io.ReadAll(rc)
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			return data
		}
	}
	t.Fatalf("entry %q not in ZIP", name)
	return nil
}

// ---------------------------------------------------------------------------
// R080: Per-collector export view
// ---------------------------------------------------------------------------

// TestPerCollectorExportOffByDefault verifies the safe default: no
// per-collector/ directory unless the operator explicitly opts in.
// Traces: ARQ-SIGNALS-R080
func TestPerCollectorExportOffByDefault(t *testing.T) {
	store := openTestDB(t)
	seedExportData(t, store)

	builder := export.NewBuilder(store, "test-instance-id")
	zr := buildExportZIPWithBuilder(t, builder)

	if names := listPerCollectorFiles(zr); len(names) != 0 {
		t.Errorf("per-collector files leaked into default export: %v", names)
	}
}

// TestPerCollectorExportEnabled verifies that with the flag on, every
// collector that produced a query_run for this scope gets exactly one
// per-collector/<query_id>.json file.
// Traces: ARQ-SIGNALS-R080
func TestPerCollectorExportEnabled(t *testing.T) {
	store := openTestDB(t)
	seedExportData(t, store)

	builder := export.NewBuilder(store, "test-instance-id")
	builder.SetExportPerCollectorFiles(true)
	zr := buildExportZIPWithBuilder(t, builder)

	names := listPerCollectorFiles(zr)
	if len(names) != 1 {
		t.Fatalf("expected 1 per-collector file (one collector ran), got %d: %v", len(names), names)
	}
	if names[0] != "per-collector/pg_settings_v1.json" {
		t.Errorf("unexpected file name: %q", names[0])
	}

	// Content shape: latest run metadata + payload rows.
	raw := readZipFile(t, zr, names[0])
	var entry map[string]any
	if err := json.Unmarshal(raw, &entry); err != nil {
		t.Fatalf("decode per-collector entry: %v", err)
	}
	if got, want := entry["query_id"], "pg_settings_v1"; got != want {
		t.Errorf("query_id = %v, want %q", got, want)
	}
	if got, want := entry["status"], "success"; got != want {
		t.Errorf("status = %v, want %q", got, want)
	}
	rows, ok := entry["rows"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("rows missing or wrong shape: %v", entry["rows"])
	}
	row := rows[0].(map[string]any)
	if row["setting"] != "shared_buffers" || row["value"] != "128MB" {
		t.Errorf("row payload mismatch: %v", row)
	}
}

// TestPerCollectorExportSkippedRunHasStub verifies that a skipped run
// (R075 high-sensitivity gate) still produces a per-collector entry —
// auditors browsing per-collector/ should see the gate is active. The
// entry carries status=skipped and reason=config_disabled but no
// payload rows.
// Traces: ARQ-SIGNALS-R080 / ARQ-SIGNALS-R075
func TestPerCollectorExportSkippedRunHasStub(t *testing.T) {
	store := openTestDB(t)

	// Minimal fixture: one target plus a single skipped run for a
	// high-sensitivity collector.
	now := time.Now().UTC().Format(time.RFC3339)
	targetID, err := store.UpsertTarget("test-pg", "h", 5432, "d", "u", "disable", "NONE", "", true)
	if err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}
	skipRun := db.QueryRun{
		ID:          "run-skip-1",
		TargetID:    targetID,
		SnapshotID:  "snap-skip",
		QueryID:     "pg_views_definitions_v1",
		CollectedAt: now,
		PGVersion:   "16",
		CreatedAt:   now,
		Status:      "skipped",
		Reason:      "config_disabled",
	}
	if err := store.InsertQueryRunBatch([]db.QueryRun{skipRun}, nil); err != nil {
		t.Fatalf("InsertQueryRunBatch: %v", err)
	}

	builder := export.NewBuilder(store, "test-instance-id")
	builder.SetExportPerCollectorFiles(true)
	zr := buildExportZIPWithBuilder(t, builder)

	raw := readZipFile(t, zr, "per-collector/pg_views_definitions_v1.json")
	var entry map[string]any
	if err := json.Unmarshal(raw, &entry); err != nil {
		t.Fatalf("decode skipped entry: %v", err)
	}
	if entry["status"] != "skipped" {
		t.Errorf("status = %v, want skipped", entry["status"])
	}
	if entry["reason"] != "config_disabled" {
		t.Errorf("reason = %v, want config_disabled", entry["reason"])
	}
	if _, has := entry["rows"]; has {
		t.Error("skipped run should not carry a row payload")
	}
}

// TestPerCollectorExportLatestRunWins verifies the latest-run-wins
// rule: when the export covers multiple cycles for the same collector,
// the per-collector file reflects the most recent run only.
// Traces: ARQ-SIGNALS-R080
func TestPerCollectorExportLatestRunWins(t *testing.T) {
	store := openTestDB(t)
	targetID, err := store.UpsertTarget("test-pg", "h", 5432, "d", "u", "disable", "NONE", "", true)
	if err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}

	// Older run.
	oldRows := []map[string]any{{"value": "old"}}
	oldPayload, oldComp, oldSize, _ := db.EncodeNDJSON(oldRows)
	older := db.QueryRun{
		ID: "run-old", TargetID: targetID, SnapshotID: "s1",
		QueryID: "demo_v1", CollectedAt: "2026-04-01T00:00:00Z",
		PGVersion: "16", DurationMS: 1, RowCount: 1, Status: "success",
		CreatedAt: "2026-04-01T00:00:00Z",
	}
	// Newer run for the same collector.
	newRows := []map[string]any{{"value": "new"}}
	newPayload, newComp, newSize, _ := db.EncodeNDJSON(newRows)
	newer := db.QueryRun{
		ID: "run-new", TargetID: targetID, SnapshotID: "s2",
		QueryID: "demo_v1", CollectedAt: "2026-04-15T00:00:00Z",
		PGVersion: "16", DurationMS: 2, RowCount: 1, Status: "success",
		CreatedAt: "2026-04-15T00:00:00Z",
	}
	results := []db.QueryResult{
		{RunID: older.ID, Payload: oldPayload, Compressed: oldComp, SizeBytes: oldSize},
		{RunID: newer.ID, Payload: newPayload, Compressed: newComp, SizeBytes: newSize},
	}
	if err := store.InsertQueryRunBatch([]db.QueryRun{older, newer}, results); err != nil {
		t.Fatalf("InsertQueryRunBatch: %v", err)
	}

	builder := export.NewBuilder(store, "test-instance-id")
	builder.SetExportPerCollectorFiles(true)
	zr := buildExportZIPWithBuilder(t, builder)

	raw := readZipFile(t, zr, "per-collector/demo_v1.json")
	var entry map[string]any
	if err := json.Unmarshal(raw, &entry); err != nil {
		t.Fatalf("decode entry: %v", err)
	}
	if entry["collected_at"] != "2026-04-15T00:00:00Z" {
		t.Errorf("expected newest collected_at, got %v", entry["collected_at"])
	}
	rows := entry["rows"].([]any)
	if rows[0].(map[string]any)["value"] != "new" {
		t.Errorf("payload mismatch: expected new, got %v", rows[0])
	}
}
