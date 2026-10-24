package export

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/elevarq/signals/internal/db"
	"github.com/elevarq/signals/internal/pgqueries"
	"github.com/elevarq/signals/internal/singlestore"
)

// Tests derive from specifications/engine-keyed-reporting.acceptance.md.
//
// These assert the engine-keyed reporting seam (MKR-R001/R002/R008 and the
// PostgreSQL byte-identical invariant INV-MKR-02) at the export-builder
// boundary, through the public WriteTo path only.

func openReportTestDB(t *testing.T) *db.DB {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "x.db"), false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return store
}

// seedEngineRun inserts one snapshot (carrying engine) with one successful
// query_run (query_id == collector id) + a trivial payload, for targetID.
func seedEngineRun(t *testing.T, store *db.DB, snapID string, targetID int64, engine, queryID, collectedAt, version string) {
	t.Helper()
	snap := db.Snapshot{
		ID: snapID, TargetID: targetID, CollectedAt: collectedAt,
		PGVersion: version, Engine: engine,
		Payload: json.RawMessage(`{}`), SizeBytes: 2,
	}
	run := db.QueryRun{
		ID: "run-" + snapID + "-" + queryID, TargetID: targetID, SnapshotID: snapID,
		QueryID: queryID, CollectedAt: collectedAt, PGVersion: version,
		CreatedAt: collectedAt, Status: "success", RowCount: 1,
	}
	results := []db.QueryResult{{RunID: run.ID, Payload: []byte("{\"k\":\"v\"}\n"), SizeBytes: 8}}
	if err := store.InsertCollectionAtomic(snap, []db.QueryRun{run}, results); err != nil {
		t.Fatalf("InsertCollectionAtomic %s/%s: %v", snapID, queryID, err)
	}
}

func exportJSONFile(t *testing.T, store *db.DB, opts Options, name string) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	if err := NewBuilder(store, "inst").WriteTo(&buf, opts); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		rc, _ := f.Open()
		defer rc.Close()
		var out map[string]any
		if err := json.NewDecoder(rc).Decode(&out); err != nil {
			t.Fatalf("decode %s: %v", name, err)
		}
		return out
	}
	t.Fatalf("%s not present in export", name)
	return nil
}

func collectorIDs(t *testing.T, file map[string]any) []string {
	t.Helper()
	cols, _ := file["collectors"].([]any)
	ids := make([]string, 0, len(cols))
	for _, c := range cols {
		m, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if id, _ := m["id"].(string); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func singleStoreCatalogIDSet() map[string]bool {
	set := map[string]bool{}
	for _, c := range singlestore.Catalog() {
		set[c.ID] = true
	}
	return set
}

// TC-MKR-01 (MKR-R001, INV-MKR-01): a SingleStore export's collector_status.json
// contains only SingleStore collectors — no PostgreSQL collector leaks in as a
// never_run entry.
func TestSingleStoreExportCollectorStatusHasNoPostgreSQLLeak(t *testing.T) {
	store := openReportTestDB(t)
	targetID, err := store.UpsertTarget("ss", "h", 3306, "db", "u", "disable", "none", "", true)
	if err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}
	// Two SingleStore collectors actually ran.
	seedEngineRun(t, store, "ss-snap-1", targetID, "singlestore", "singlestore_identity_v1", "2026-10-23T00:00:00Z", "9.1.1")
	seedEngineRun(t, store, "ss-snap-2", targetID, "singlestore", "singlestore_aggregators_v1", "2026-10-23T00:00:01Z", "9.1.1")

	file := exportJSONFile(t, store, Options{TargetID: targetID}, "collector_status.json")
	ssSet := singleStoreCatalogIDSet()
	for _, id := range collectorIDs(t, file) {
		if pgqueries.ByID(id) != nil {
			t.Errorf("PostgreSQL collector %q leaked into a SingleStore export's collector_status.json", id)
		}
		if !ssSet[id] {
			t.Errorf("non-SingleStore collector %q present in a SingleStore export", id)
		}
	}
}

// TC-MKR-02 (MKR-R002, INV-MKR-01): a SingleStore export's query_catalog.json
// excludes PostgreSQL rows even when the (mixed) store holds both.
func TestSingleStoreExportQueryCatalogExcludesPostgreSQL(t *testing.T) {
	store := openReportTestDB(t)
	targetID, err := store.UpsertTarget("ss", "h", 3306, "db", "u", "disable", "none", "", true)
	if err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}
	// Mixed catalog: one real PostgreSQL row, one SingleStore row.
	pgID := pgqueries.All()[0].ID
	if err := store.UpsertQueryCatalog(db.QueryCatalogRow{QueryID: pgID, Category: "config", ResultKind: "rowset", RetentionClass: "medium", RegisteredAt: "2026-10-23T00:00:00Z"}); err != nil {
		t.Fatalf("UpsertQueryCatalog pg: %v", err)
	}
	if err := store.UpsertQueryCatalog(db.QueryCatalogRow{QueryID: "singlestore_identity_v1", Category: "server", ResultKind: "rowset", RetentionClass: "medium", RegisteredAt: "2026-10-23T00:00:00Z"}); err != nil {
		t.Fatalf("UpsertQueryCatalog ss: %v", err)
	}
	seedEngineRun(t, store, "ss-snap-1", targetID, "singlestore", "singlestore_identity_v1", "2026-10-23T00:00:00Z", "9.1.1")

	var buf bytes.Buffer
	if err := NewBuilder(store, "inst").WriteTo(&buf, Options{TargetID: targetID}); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	var rows []map[string]any
	for _, f := range zr.File {
		if f.Name != "query_catalog.json" {
			continue
		}
		rc, _ := f.Open()
		if err := json.NewDecoder(rc).Decode(&rows); err != nil {
			rc.Close()
			t.Fatalf("decode query_catalog.json: %v", err)
		}
		rc.Close()
	}
	if len(rows) == 0 {
		t.Fatal("query_catalog.json has no rows")
	}
	for _, r := range rows {
		id, _ := r["QueryID"].(string)
		if pgqueries.ByID(id) != nil {
			t.Errorf("PostgreSQL catalog row %q leaked into a SingleStore export's query_catalog.json", id)
		}
	}
}

// TC-MKR-03 (MKR-R001, INV-MKR-02): a PostgreSQL export still enumerates every
// pgqueries collector (those with runs + the rest as never_run). The seam must
// not drop any PostgreSQL collector.
func TestPostgreSQLExportCollectorStatusUnchanged(t *testing.T) {
	store := openReportTestDB(t)
	targetID, err := store.UpsertTarget("pg", "h", 5432, "postgres", "u", "disable", "none", "", true)
	if err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}
	all := pgqueries.All()
	// Run the first two PostgreSQL collectors; the rest must appear as never_run.
	seedEngineRun(t, store, "pg-snap-1", targetID, "postgresql", all[0].ID, "2026-10-23T00:00:00Z", "PostgreSQL 18.0")
	seedEngineRun(t, store, "pg-snap-2", targetID, "postgresql", all[1].ID, "2026-10-23T00:00:01Z", "PostgreSQL 18.0")

	file := exportJSONFile(t, store, Options{TargetID: targetID}, "collector_status.json")
	present := map[string]bool{}
	for _, id := range collectorIDs(t, file) {
		present[id] = true
	}
	for _, q := range all {
		if !present[q.ID] {
			t.Errorf("PostgreSQL collector %q missing from collector_status.json (seam dropped it)", q.ID)
		}
	}
}

// TC-MKR-04 (MKR-R002, INV-MKR-02): a PostgreSQL export retains every stored
// PostgreSQL catalog row (the engine filter is a no-op for PostgreSQL).
func TestPostgreSQLExportQueryCatalogRetained(t *testing.T) {
	store := openReportTestDB(t)
	targetID, err := store.UpsertTarget("pg", "h", 5432, "postgres", "u", "disable", "none", "", true)
	if err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}
	for _, q := range pgqueries.All() {
		if err := store.UpsertQueryCatalog(db.QueryCatalogRow{QueryID: q.ID, Category: q.Category, ResultKind: string(q.ResultKind), RetentionClass: string(q.RetentionClass), RegisteredAt: "2026-10-23T00:00:00Z"}); err != nil {
			t.Fatalf("UpsertQueryCatalog %s: %v", q.ID, err)
		}
	}
	seedEngineRun(t, store, "pg-snap-1", targetID, "postgresql", pgqueries.All()[0].ID, "2026-10-23T00:00:00Z", "PostgreSQL 18.0")

	var buf bytes.Buffer
	if err := NewBuilder(store, "inst").WriteTo(&buf, Options{TargetID: targetID}); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	var rows []map[string]any
	for _, f := range zr.File {
		if f.Name != "query_catalog.json" {
			continue
		}
		rc, _ := f.Open()
		_ = json.NewDecoder(rc).Decode(&rows)
		rc.Close()
	}
	if len(rows) != len(pgqueries.All()) {
		t.Errorf("query_catalog.json has %d rows, want %d (PostgreSQL catalog must be retained whole)", len(rows), len(pgqueries.All()))
	}
}

// TC-MKR-12 (MKR-R008, INV-MKR-02): a SingleStore export's metadata.json carries
// engine_version; a PostgreSQL export does not (byte-identical).
func TestMetadataEngineVersionOnlyForNonPostgreSQL(t *testing.T) {
	store := openReportTestDB(t)
	ssTarget, err := store.UpsertTarget("ss", "h", 3306, "db", "u", "disable", "none", "", true)
	if err != nil {
		t.Fatalf("UpsertTarget ss: %v", err)
	}
	pgTarget, err := store.UpsertTarget("pg", "h2", 5432, "postgres", "u", "disable", "none", "", true)
	if err != nil {
		t.Fatalf("UpsertTarget pg: %v", err)
	}
	seedEngineRun(t, store, "ss-snap-1", ssTarget, "singlestore", "singlestore_identity_v1", "2026-10-23T00:00:00Z", "9.1.1")
	seedEngineRun(t, store, "pg-snap-1", pgTarget, "postgresql", pgqueries.All()[0].ID, "2026-10-23T00:00:00Z", "PostgreSQL 18.0")

	ssMeta := exportJSONFile(t, store, Options{TargetID: ssTarget}, "metadata.json")
	if ssMeta["engine_version"] != "9.1.1" {
		t.Errorf("SingleStore metadata.json engine_version = %v, want 9.1.1", ssMeta["engine_version"])
	}
	pgMeta := exportJSONFile(t, store, Options{TargetID: pgTarget}, "metadata.json")
	if _, present := pgMeta["engine_version"]; present {
		t.Errorf("PostgreSQL metadata.json must not carry engine_version, got %v", pgMeta["engine_version"])
	}
}
