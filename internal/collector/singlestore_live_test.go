//go:build singlestore_live

package collector

// Live SingleStore qualification (specifications/singlestore-live-qualification.md).
//
// This is the PRODUCER-EVIDENCE leg: it verifies the artifacts produced by the
// NORMAL cmd/signals daemon against a REAL pinned SingleStore Dev Image. It is
// opt-in / local — gated behind the `singlestore_live` build tag AND the
// SINGLESTORE_QUAL_* environment, which scripts/singlestore-live-qual.sh sets
// after driving the real daemon + exporter. It never runs in the default CI
// lane and never calls singlestore.Collect() directly.
//
// Run via:  bash scripts/singlestore-live-qual.sh all

import (
	"os"
	"strconv"
	"testing"

	"github.com/elevarq/signals/internal/db"
)

func openStore(t *testing.T, path string) *db.DB {
	t.Helper()
	store, err := db.Open(path, false)
	if err != nil {
		t.Fatalf("open store %s: %v", path, err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// TestSingleStoreLiveCadenceQualification (#583, SLQ-R002..R005): over the
// cadence store written by the real daemon across multiple cycles + a restart,
// every snapshot passes VerifyCadenceSnapshot (per-collector status='success'
// with reconciled evidence), ids are unique (no overwrite), and the restart
// appended.
func TestSingleStoreLiveCadenceQualification(t *testing.T) {
	dbPath := os.Getenv("SINGLESTORE_QUAL_CADENCE_DB")
	if dbPath == "" {
		t.Skip("SINGLESTORE_QUAL_CADENCE_DB not set — run via scripts/singlestore-live-qual.sh")
	}
	store := openStore(t, dbPath)
	snaps, err := ReadQualSnapshots(store)
	if err != nil {
		t.Fatalf("ReadQualSnapshots: %v", err)
	}

	minSnaps := 3
	if v := os.Getenv("SINGLESTORE_QUAL_CADENCE_MIN"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			minSnaps = n
		}
	}
	if len(snaps) < minSnaps {
		t.Fatalf("expected >= %d scheduled snapshots, got %d", minSnaps, len(snaps))
	}

	seen := map[string]bool{}
	expected := SingleStoreExpectedCollectors()
	for _, s := range snaps {
		if seen[s.ID] {
			t.Errorf("duplicate snapshot id %s (overwrite!)", s.ID)
		}
		seen[s.ID] = true
		if err := VerifyCadenceSnapshot(s, expected, "singlestore", "9."); err != nil {
			t.Errorf("snapshot %s failed cadence qualification: %v", s.ID, err)
		}
	}

	// Restart-append: the orchestrator records the count BEFORE the restart;
	// the accumulated total must exceed it (append, no overwrite).
	if v := os.Getenv("SINGLESTORE_QUAL_CADENCE_BEFORE"); v != "" {
		before, err := strconv.Atoi(v)
		if err == nil && len(snaps) <= before {
			t.Errorf("restart did not append: before=%d after=%d", before, len(snaps))
		}
	}
	t.Logf("cadence qualification OK: %d independent snapshots, all collectors status=success", len(snaps))
}

// TestSingleStoreLiveExportReconciliation (#584, SLQ-R006..R009): one exact live
// snapshot, exported through the production exporter, reconciles
// collected==persisted==exported for every collector, carries truthful
// engine/version metadata, no foreign PostgreSQL collectors, held redaction, and
// a manifest covering every member.
func TestSingleStoreLiveExportReconciliation(t *testing.T) {
	dbPath := os.Getenv("SINGLESTORE_QUAL_EXPORT_DB")
	zipPath := os.Getenv("SINGLESTORE_QUAL_EXPORT_ZIP")
	if dbPath == "" || zipPath == "" {
		t.Skip("SINGLESTORE_QUAL_EXPORT_DB/ZIP not set — run via scripts/singlestore-live-qual.sh")
	}
	store := openStore(t, dbPath)
	snaps, err := ReadQualSnapshots(store)
	if err != nil {
		t.Fatalf("ReadQualSnapshots: %v", err)
	}
	if len(snaps) == 0 {
		t.Fatal("export store has no snapshots")
	}

	// The export store holds exactly one forced collection; the exact snapshot
	// is snaps[0] (optionally pinned by SINGLESTORE_QUAL_EXPORT_SNAPSHOT).
	target := snaps[0]
	if want := os.Getenv("SINGLESTORE_QUAL_EXPORT_SNAPSHOT"); want != "" {
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
	expected := SingleStoreExpectedCollectors()
	if err := VerifyCadenceSnapshot(target, expected, "singlestore", "9."); err != nil {
		t.Fatalf("exact snapshot %s failed status/evidence checks: %v", target.ID, err)
	}
	if err := VerifyExportReconciliation(target, z, expected, "9."); err != nil {
		t.Fatalf("export reconciliation for %s failed: %v", target.ID, err)
	}
	t.Logf("export reconciliation OK for snapshot %s: collected==persisted==exported, redaction held, manifest complete", target.ID)

	// Semantic qualification (specifications/semantic-collector-qualification.md):
	// a value assertion against the REAL production export is a required lifecycle
	// step — status + shape reconciliation are not sufficient (SCQ-INV-1).
	for _, c := range SemanticQualRegistry()["singlestore"] {
		if v := c.EffectiveVerifier(); v != nil {
			if err := v(z); err != nil {
				t.Fatalf("semantic qualification failed for %s: %v", c.CollectorID, err)
			}
			t.Logf("semantic contract validated for %s (%s): emitted values are faithful", c.CollectorID, c.Level)
		}
	}
}
