package tests

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/elevarq/arq-signals/internal/db"
	"github.com/elevarq/arq-signals/internal/export"
	"github.com/elevarq/arq-signals/internal/pgqueries"
	"github.com/elevarq/arq-signals/snapshot"
)

// openTestDB creates a temp SQLite DB, runs migrations, and returns it.
// The DB is closed automatically when the test finishes.
func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := db.Open(dbPath, false)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	if err := store.Migrate(); err != nil {
		_ = store.Close()
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// buildExportZIP creates an export builder, writes a ZIP to a buffer, and returns
// the buffer plus the zip reader for inspection.
func buildExportZIP(t *testing.T, store *db.DB) (*bytes.Buffer, *zip.Reader) {
	t.Helper()
	builder := export.NewBuilder(store, "test-instance-id")
	var buf bytes.Buffer
	if err := builder.WriteTo(&buf, export.Options{}); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	return &buf, zr
}

// TestExportMetadataSchemaVersion verifies metadata.json contains schema_version == "signals-snapshot.v1".
// Traces: ARQ-SIGNALS-R005 / TC-SIG-008
func TestExportMetadataSchemaVersion(t *testing.T) {
	store := openTestDB(t)
	_, zr := buildExportZIP(t, store)

	meta := readZipFileJSON(t, zr, "metadata.json")
	sv, ok := meta["schema_version"].(string)
	if !ok {
		t.Fatal("metadata.json missing schema_version field")
	}
	if sv != snapshot.SchemaVersion {
		t.Errorf("schema_version = %q, want %q", sv, snapshot.SchemaVersion)
	}
}

// TestExportMetadataCollectorFields verifies metadata.json contains collector_version
// and collector_commit fields (even if default values).
// Traces: ARQ-SIGNALS-R005 / TC-SIG-008
func TestExportMetadataCollectorFields(t *testing.T) {
	store := openTestDB(t)
	_, zr := buildExportZIP(t, store)

	meta := readZipFileJSON(t, zr, "metadata.json")

	if _, ok := meta["collector_version"]; !ok {
		t.Error("metadata.json missing collector_version field")
	}
	if _, ok := meta["collector_commit"]; !ok {
		t.Error("metadata.json missing collector_commit field")
	}
}

// TestExportMetadataCollectedAtRFC3339 verifies collected_at is a valid RFC3339 timestamp.
// Traces: ARQ-SIGNALS-R005 / TC-SIG-008
func TestExportMetadataCollectedAtRFC3339(t *testing.T) {
	store := openTestDB(t)
	_, zr := buildExportZIP(t, store)

	meta := readZipFileJSON(t, zr, "metadata.json")

	raw, ok := meta["collected_at"].(string)
	if !ok {
		t.Fatal("metadata.json missing collected_at field")
	}
	if _, err := time.Parse(time.RFC3339, raw); err != nil {
		t.Errorf("collected_at %q is not valid RFC3339: %v", raw, err)
	}
}

// TestExportMetadataInstanceID verifies instance_id is present and matches what we set.
// Traces: ARQ-SIGNALS-R005 / TC-SIG-008
func TestExportMetadataInstanceID(t *testing.T) {
	store := openTestDB(t)
	_, zr := buildExportZIP(t, store)

	meta := readZipFileJSON(t, zr, "metadata.json")

	iid, ok := meta["instance_id"].(string)
	if !ok {
		t.Fatal("metadata.json missing instance_id field")
	}
	if iid != "test-instance-id" {
		t.Errorf("instance_id = %q, want %q", iid, "test-instance-id")
	}
}

// TestExportZIPContainsRequiredFiles verifies the ZIP contains the required files for
// the Elevarq Signals export format.
// Traces: ARQ-SIGNALS-R006 / TC-SIG-009
func TestExportZIPContainsRequiredFiles(t *testing.T) {
	store := openTestDB(t)

	// Insert seed data: a target, a snapshot, a query run and result.
	seedExportData(t, store)

	_, zr := buildExportZIP(t, store)

	required := []string{
		"metadata.json",
		"collector_status.json",
		"query_catalog.json",
		"query_runs.ndjson",
		"query_results.ndjson",
		"snapshots.ndjson",
	}

	fileSet := make(map[string]bool, len(zr.File))
	for _, f := range zr.File {
		fileSet[f.Name] = true
	}

	for _, name := range required {
		if !fileSet[name] {
			t.Errorf("ZIP missing required file: %s", name)
		}
	}
}

// TestExportZIPOmitsAnalyzerFiles verifies the ZIP does NOT contain files
// that belong to the analyzer product (arq-analyzer).
// Traces: ARQ-SIGNALS-R006 / TC-SIG-009
func TestExportZIPOmitsAnalyzerFiles(t *testing.T) {
	store := openTestDB(t)
	seedExportData(t, store)
	_, zr := buildExportZIP(t, store)

	forbidden := []string{
		"stats_snapshots",
		"requirement_catalog",
		"reports",
		"environment_profiles",
		"license_status",
	}

	for _, f := range zr.File {
		for _, kw := range forbidden {
			if f.Name == kw || f.Name == kw+".json" || f.Name == kw+".ndjson" {
				t.Errorf("ZIP contains forbidden file: %s (analyzer-only content)", f.Name)
			}
		}
	}
}

// seedExportData inserts a minimal target + snapshot + query run + result into the DB.
func seedExportData(t *testing.T, store *db.DB) {
	t.Helper()

	now := time.Now().UTC().Format(time.RFC3339)

	// Insert target.
	targetID, err := store.UpsertTarget("test-pg", "localhost", 5432, "testdb", "arq", "disable", "NONE", "", true)
	if err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}

	// Sync at least one query catalog entry.
	for _, q := range pgqueries.All() {
		if err := store.UpsertQueryCatalog(db.QueryCatalogRow{
			QueryID:        q.ID,
			Category:       q.Category,
			ResultKind:     string(q.ResultKind),
			RetentionClass: string(q.RetentionClass),
			RegisteredAt:   now,
		}); err != nil {
			t.Fatalf("UpsertQueryCatalog(%s): %v", q.ID, err)
		}
	}

	// Insert snapshot.
	snap := db.Snapshot{
		ID:          "snap-001",
		TargetID:    targetID,
		CollectedAt: now,
		PGVersion:   "PostgreSQL 16.2",
		Payload:     json.RawMessage(`{"version":"PostgreSQL 16.2"}`),
		SizeBytes:   42,
	}
	if err := store.InsertSnapshot(snap); err != nil {
		t.Fatalf("InsertSnapshot: %v", err)
	}

	// Insert a query run.
	rows := []map[string]any{{"setting": "shared_buffers", "value": "128MB"}}
	payload, compressed, sizeBytes, err := db.EncodeNDJSON(rows)
	if err != nil {
		t.Fatalf("EncodeNDJSON: %v", err)
	}

	run := db.QueryRun{
		ID:          "run-001",
		TargetID:    targetID,
		SnapshotID:  "snap-001",
		QueryID:     "pg_settings_v1",
		CollectedAt: now,
		PGVersion:   "PostgreSQL 16.2",
		DurationMS:  5,
		RowCount:    1,
		Error:       "",
		CreatedAt:   now,
	}
	result := db.QueryResult{
		RunID:      "run-001",
		Payload:    payload,
		Compressed: compressed,
		SizeBytes:  sizeBytes,
	}
	if err := store.InsertQueryRunBatch([]db.QueryRun{run}, []db.QueryResult{result}); err != nil {
		t.Fatalf("InsertQueryRunBatch: %v", err)
	}
}

// readZipFileJSON reads a named file from the ZIP and decodes it as JSON.
func readZipFileJSON(t *testing.T, zr *zip.Reader, name string) map[string]any {
	t.Helper()
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("open %s in ZIP: %v", name, err)
			}
			defer rc.Close()
			var m map[string]any
			if err := json.NewDecoder(rc).Decode(&m); err != nil {
				t.Fatalf("decode %s: %v", name, err)
			}
			return m
		}
	}
	t.Fatalf("file %s not found in ZIP", name)
	return nil
}
