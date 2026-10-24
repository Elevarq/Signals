//go:build mysql_live

package collector

// Live MySQL qualification (specifications/mysql-live-qualification.md).
//
// The PRODUCER-EVIDENCE leg: it verifies artifacts produced by the NORMAL
// cmd/signals daemon against a REAL pinned MySQL (a CI service container or the
// local Dev Image). Gated behind the `mysql_live` build tag AND the MYSQL_QUAL_*
// environment that scripts/mysql-live-qual.sh sets after driving the real daemon
// + exporter. It never calls the collector directly.
//
// Run via:  bash scripts/mysql-live-qual.sh all

import (
	"os"
	"strconv"
	"testing"

	"github.com/elevarq/signals/internal/db"
)

func openMySQLStore(t *testing.T, path string) *db.DB {
	t.Helper()
	store, err := db.Open(path, false)
	if err != nil {
		t.Fatalf("open store %s: %v", path, err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// TestMySQLLiveCadenceQualification (MLQ-R002/R003): over the cadence store
// written by the real daemon across cycles + a restart, every snapshot passes
// VerifyCadenceSnapshot (engine=mysql, every collector status='success' with
// reconciled evidence; replication a legitimate zero-row success), ids unique,
// restart appended.
func TestMySQLLiveCadenceQualification(t *testing.T) {
	dbPath := os.Getenv("MYSQL_QUAL_CADENCE_DB")
	if dbPath == "" {
		t.Skip("MYSQL_QUAL_CADENCE_DB not set — run via scripts/mysql-live-qual.sh")
	}
	store := openMySQLStore(t, dbPath)
	snaps, err := ReadQualSnapshots(store)
	if err != nil {
		t.Fatalf("ReadQualSnapshots: %v", err)
	}

	minSnaps := 3
	if v := os.Getenv("MYSQL_QUAL_CADENCE_MIN"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			minSnaps = n
		}
	}
	if len(snaps) < minSnaps {
		t.Fatalf("expected >= %d scheduled snapshots, got %d", minSnaps, len(snaps))
	}

	seen := map[string]bool{}
	expected := ExpectedCollectorsFor("mysql")
	for _, s := range snaps {
		if seen[s.ID] {
			t.Errorf("duplicate snapshot id %s (overwrite!)", s.ID)
		}
		seen[s.ID] = true
		if err := VerifyCadenceSnapshot(s, expected, "mysql", "8."); err != nil {
			t.Errorf("snapshot %s failed cadence qualification: %v", s.ID, err)
		}
	}

	if v := os.Getenv("MYSQL_QUAL_CADENCE_BEFORE"); v != "" {
		before, err := strconv.Atoi(v)
		if err == nil && len(snaps) <= before {
			t.Errorf("restart did not append: before=%d after=%d", before, len(snaps))
		}
	}
	t.Logf("cadence qualification OK: %d independent snapshots, all collectors status=success", len(snaps))
}

// TestMySQLLiveExportReconciliation (MLQ-R004): one exact live snapshot, exported
// through the production exporter, reconciles collected==persisted==exported for
// every collector, carries truthful engine/version metadata, no foreign
// PostgreSQL collectors, held redaction, and a complete manifest.
func TestMySQLLiveExportReconciliation(t *testing.T) {
	dbPath := os.Getenv("MYSQL_QUAL_EXPORT_DB")
	zipPath := os.Getenv("MYSQL_QUAL_EXPORT_ZIP")
	if dbPath == "" || zipPath == "" {
		t.Skip("MYSQL_QUAL_EXPORT_DB/ZIP not set — run via scripts/mysql-live-qual.sh")
	}
	store := openMySQLStore(t, dbPath)
	snaps, err := ReadQualSnapshots(store)
	if err != nil {
		t.Fatalf("ReadQualSnapshots: %v", err)
	}
	if len(snaps) == 0 {
		t.Fatal("export store has no snapshots")
	}

	target := snaps[0]
	if want := os.Getenv("MYSQL_QUAL_EXPORT_SNAPSHOT"); want != "" {
		found := false
		for _, s := range snaps {
			if s.ID == want {
				target = s
				found = true
			}
		}
		if !found {
			t.Fatalf("pinned snapshot %s not found in export store", want)
		}
	}

	z, err := ReadQualExport(zipPath)
	if err != nil {
		t.Fatalf("ReadQualExport: %v", err)
	}
	expected := ExpectedCollectorsFor("mysql")
	if err := VerifyCadenceSnapshot(target, expected, "mysql", "8."); err != nil {
		t.Fatalf("exact snapshot %s failed status/evidence checks: %v", target.ID, err)
	}
	if err := VerifyExportReconciliation(target, z, expected, "8."); err != nil {
		t.Fatalf("export reconciliation for %s failed: %v", target.ID, err)
	}
	t.Logf("export reconciliation OK for snapshot %s: collected==persisted==exported, redaction held, manifest complete", target.ID)
}
