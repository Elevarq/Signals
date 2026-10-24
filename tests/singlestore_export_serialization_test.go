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

// TC-SLQ-10 (SLQ-R006, SLQ-R007, SLQ-R009): the DETERMINISTIC serialization leg
// of the live-qualification coverage receipt. A SingleStore snapshot is
// persisted via InsertCollectionAtomic and exported through the PRODUCTION
// exporter (export.Builder), then reconciled by the shared verifier
// (collector.VerifyExportReconciliation).
//
// This is serialization/verifier coverage, explicitly NOT producer evidence:
// the live producer evidence (real cmd/signals daemon + live SingleStore) is the
// //go:build singlestore_live test. See
// specifications/singlestore-live-qualification.md "Coverage receipt".
func TestSingleStoreExportSerializationReconciles(t *testing.T) {
	store := openTestDB(t)
	targetID, err := store.UpsertTarget("ss-dev", "127.0.0.1", 3306, "signals_fixture", "signals_ro", "disable", "none", "", true)
	if err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}

	// Persist one engine=singlestore snapshot whose runs mirror the real
	// producer shape: one successful run per catalog collector, redacted
	// sensitive columns, a per-run payload whose decoded row count equals the
	// run's row_count.
	cat := singlestore.Catalog()
	snapID := "01TESTSNAPSHOTSINGLESTORE01"
	snap := db.Snapshot{
		ID: snapID, TargetID: targetID, CollectedAt: "2026-10-24T00:00:00Z",
		PGVersion: "9.1.1", Engine: "singlestore",
		Payload: json.RawMessage(`{"engine":"singlestore"}`), SizeBytes: 24,
	}
	var runs []db.QueryRun
	var results []db.QueryResult
	for i, c := range cat {
		runID := snapID + "-" + c.ID
		// Row count varies per collector; one legitimate zero-row collector.
		rows := make([]map[string]any, 0, i)
		for r := 0; r < i; r++ {
			row := map[string]any{"k": r}
			// Redacted sensitive columns persist as nil (mirrors the producer).
			if len(c.SensitiveColumns) > 0 {
				for _, col := range c.SensitiveColumns {
					row[col] = nil
				}
			}
			rows = append(rows, row)
		}
		payload, compressed, size, err := db.EncodeNDJSON(rows)
		if err != nil {
			t.Fatalf("EncodeNDJSON %s: %v", c.ID, err)
		}
		runs = append(runs, db.QueryRun{
			ID: runID, TargetID: targetID, SnapshotID: snapID,
			QueryID: c.ID, CollectedAt: "2026-10-24T00:00:00Z", PGVersion: "9.1.1",
			RowCount: i, CreatedAt: "2026-10-24T00:00:00Z", Status: "success",
		})
		results = append(results, db.QueryResult{RunID: runID, Payload: payload, Compressed: compressed, SizeBytes: size})

		// Register the collector in the catalog table (as the SingleStore session
		// does). Also register a foreign PostgreSQL row to prove the M3 engine
		// filter keeps it out of the SingleStore export.
		if err := store.UpsertQueryCatalog(db.QueryCatalogRow{QueryID: c.ID, Category: c.Category, ResultKind: "rowset", RetentionClass: "medium", RegisteredAt: "2026-10-24T00:00:00Z"}); err != nil {
			t.Fatalf("UpsertQueryCatalog %s: %v", c.ID, err)
		}
	}
	if err := store.UpsertQueryCatalog(db.QueryCatalogRow{QueryID: "pg_settings_v1", Category: "config", ResultKind: "rowset", RetentionClass: "medium", RegisteredAt: "2026-10-24T00:00:00Z"}); err != nil {
		t.Fatalf("UpsertQueryCatalog pg: %v", err)
	}
	if err := store.InsertCollectionAtomic(snap, runs, results); err != nil {
		t.Fatalf("InsertCollectionAtomic: %v", err)
	}

	// Export the exact snapshot through the production exporter to a file.
	var buf bytes.Buffer
	if err := export.NewBuilder(store, "inst").WriteTo(&buf, export.Options{SnapshotID: snapID}); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	zipPath := filepath.Join(t.TempDir(), "snapshot.zip")
	if err := os.WriteFile(zipPath, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write zip: %v", err)
	}

	// Reconcile via the shared verifier.
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
	if qs.ID == "" {
		t.Fatalf("snapshot %s not found in store projection", snapID)
	}
	z, err := collector.ReadQualExport(zipPath)
	if err != nil {
		t.Fatalf("ReadQualExport: %v", err)
	}
	expected := collector.SingleStoreExpectedCollectors()
	if err := collector.VerifyCadenceSnapshot(qs, expected, "9."); err != nil {
		t.Fatalf("cadence verify (serialization leg): %v", err)
	}
	if err := collector.VerifyExportReconciliation(qs, z, expected, "9."); err != nil {
		t.Fatalf("export reconciliation (serialization leg): %v", err)
	}
	// The foreign PostgreSQL catalog row must have been filtered out (M3 seam).
	for _, id := range z.QueryCatalogIDs {
		if id == "pg_settings_v1" {
			t.Errorf("foreign PostgreSQL row leaked into the SingleStore export query_catalog.json")
		}
	}
}
