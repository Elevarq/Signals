package tests

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/elevarq/signals/internal/collector"
	"github.com/elevarq/signals/internal/db"
	"github.com/elevarq/signals/internal/export"
	"github.com/elevarq/signals/internal/singlestore"
)

// Deterministic serialization proof for a MariaDB engine snapshot: persist via
// InsertCollectionAtomic + export through the PRODUCTION exporter, then reconcile
// with the shared verifier. Proves the engine-keyed reporting seam (M3) + the
// CatalogFor registry hold for engine=mariadb — metadata engine/version, no
// foreign PostgreSQL collector, collected==persisted==exported. This is
// serialization coverage (not live producer evidence; the MariaDB live
// qualification is a follow-up PR).
func TestMariaDBExportSerializationReconciles(t *testing.T) {
	store := openTestDB(t)
	targetID, err := store.UpsertTarget("mariadb-dev", "127.0.0.1", 3306, "app", "signals_ro", "disable", "none", "", true)
	if err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}

	cat := singlestore.CatalogFor("mariadb")
	if len(cat) == 0 {
		t.Fatal("CatalogFor(mariadb) is empty")
	}
	snapID := "01TESTSNAPSHOTMARIADB0000001"
	snap := db.Snapshot{
		ID: snapID, TargetID: targetID, CollectedAt: "2026-10-24T00:00:00Z",
		PGVersion: "11.4.13-MariaDB", Engine: "mariadb",
		Payload: json.RawMessage(`{"engine":"mariadb"}`), SizeBytes: 18,
	}
	var runs []db.QueryRun
	var results []db.QueryResult
	for i, c := range cat {
		runID := snapID + "-" + c.ID
		rows := make([]map[string]any, 0, i)
		for r := 0; r < i; r++ {
			row := map[string]any{"k": r}
			for _, col := range c.SensitiveColumns {
				row[col] = nil // redacted, as the producer persists it
			}
			rows = append(rows, row)
		}
		payload, compressed, size, err := db.EncodeNDJSON(rows)
		if err != nil {
			t.Fatalf("EncodeNDJSON %s: %v", c.ID, err)
		}
		runs = append(runs, db.QueryRun{
			ID: runID, TargetID: targetID, SnapshotID: snapID,
			QueryID: c.ID, CollectedAt: "2026-10-24T00:00:00Z", PGVersion: "11.4.13-MariaDB",
			RowCount: i, CreatedAt: "2026-10-24T00:00:00Z", Status: "success",
		})
		results = append(results, db.QueryResult{RunID: runID, Payload: payload, Compressed: compressed, SizeBytes: size})
		if err := store.UpsertQueryCatalog(db.QueryCatalogRow{QueryID: c.ID, Category: c.Category, ResultKind: "rowset", RetentionClass: "medium", RegisteredAt: "2026-10-24T00:00:00Z"}); err != nil {
			t.Fatalf("UpsertQueryCatalog %s: %v", c.ID, err)
		}
	}
	// A foreign PostgreSQL catalog row the engine filter must exclude.
	if err := store.UpsertQueryCatalog(db.QueryCatalogRow{QueryID: "pg_settings_v1", Category: "config", ResultKind: "rowset", RetentionClass: "medium", RegisteredAt: "2026-10-24T00:00:00Z"}); err != nil {
		t.Fatalf("UpsertQueryCatalog pg: %v", err)
	}
	if err := store.InsertCollectionAtomic(snap, runs, results); err != nil {
		t.Fatalf("InsertCollectionAtomic: %v", err)
	}

	var buf bytes.Buffer
	if err := export.NewBuilder(store, "inst").WriteTo(&buf, export.Options{SnapshotID: snapID}); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	zipPath := filepath.Join(t.TempDir(), "snapshot.zip")
	if err := os.WriteFile(zipPath, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write zip: %v", err)
	}

	snaps, err := collector.ReadQualSnapshots(store)
	if err != nil {
		t.Fatalf("ReadQualSnapshots: %v", err)
	}
	var qs collector.QualSnapshot
	for _, s := range snaps {
		if s.ID == snapID {
			qs = s
		}
	}
	z, err := collector.ReadQualExport(zipPath)
	if err != nil {
		t.Fatalf("ReadQualExport: %v", err)
	}
	expected := make([]string, 0, len(cat))
	for _, c := range cat {
		expected = append(expected, c.ID)
	}
	if err := collector.VerifyExportReconciliation(qs, z, expected, "11."); err != nil {
		t.Fatalf("mariadb export reconciliation failed: %v", err)
	}
	for _, id := range z.QueryCatalogIDs {
		if id == "pg_settings_v1" {
			t.Error("foreign PostgreSQL row leaked into the mariadb export query_catalog.json")
		}
	}
}
