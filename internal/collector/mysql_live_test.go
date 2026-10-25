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

// mysqlQualVersionPrefix is the expected engine_version prefix for the live
// qualification, so the one harness qualifies every pinned MySQL line (#625/#547):
// "8." for 8.0/8.4, "9." for 9.7 LTS. Set via MYSQL_QUAL_VERSION_PREFIX per CI
// matrix leg; defaults to "8." for backward compatibility.
func mysqlQualVersionPrefix() string {
	if p := os.Getenv("MYSQL_QUAL_VERSION_PREFIX"); p != "" {
		return p
	}
	return "8."
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
		if err := VerifyCadenceSnapshot(s, expected, "mysql", mysqlQualVersionPrefix()); err != nil {
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
	if err := VerifyCadenceSnapshot(target, expected, "mysql", mysqlQualVersionPrefix()); err != nil {
		t.Fatalf("exact snapshot %s failed status/evidence checks: %v", target.ID, err)
	}
	if err := VerifyExportReconciliation(target, z, expected, mysqlQualVersionPrefix()); err != nil {
		t.Fatalf("export reconciliation for %s failed: %v", target.ID, err)
	}
	t.Logf("export reconciliation OK for snapshot %s: collected==persisted==exported, redaction held, manifest complete", target.ID)
}

// TestMySQLLiveReplayTier1Evidence (#538, MYC-R009): the columns +
// index-inventory evidence for the seeded signals_fixture.replay_fixture table
// survives collection -> persistence -> production export with its semantics
// intact — column order/type/nullability and index identity/uniqueness/composite
// ordering — so Analyzer Replay can reconstruct the table without inventing it.
func TestMySQLLiveReplayTier1Evidence(t *testing.T) {
	zipPath := os.Getenv("MYSQL_QUAL_EXPORT_ZIP")
	if zipPath == "" {
		t.Skip("MYSQL_QUAL_EXPORT_ZIP not set — run via scripts/mysql-live-qual.sh")
	}
	z, err := ReadQualExport(zipPath)
	if err != nil {
		t.Fatalf("ReadQualExport: %v", err)
	}
	if err := VerifyReplayTier1Evidence(z, "mysql"); err != nil {
		t.Fatalf("replay Tier-1 evidence qualification failed: %v", err)
	}
	t.Log("replay Tier-1 evidence OK: columns + index inventory semantics survived collection->persistence->export")
}
