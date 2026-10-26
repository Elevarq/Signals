//go:build mysql_live || mariadb_live

package collector

// Live structural-query-text qualification (#647,
// specifications/structural-query-text.md, INV-SQT-01 / TC-SQT-08 / TC-SQT-10).
//
// The PRODUCER-EVIDENCE leg for the structural capability: it verifies the REAL
// export ZIPs produced by the normal cmd/signals daemon against a live pinned
// MySQL/MariaDB after the adversarial workload ran — one export with the
// structural opt-in ON, one with it OFF (control). Gated behind the engine live
// build tag AND the STRUCTURAL_QUAL_* environment that
// scripts/{mysql,mariadb}-live-qual.sh set after driving the real daemon +
// exporter. Each engine/version runs it independently (SQT-R009).
//
// Run via:  bash scripts/mysql-live-qual.sh all   (or mariadb-live-qual.sh)

import (
	"os"
	"testing"

	"github.com/elevarq/signals/internal/singlestore"
)

func TestLiveStructuralQueryText(t *testing.T) {
	engine := os.Getenv("STRUCTURAL_QUAL_ENGINE")
	structuralZip := os.Getenv("STRUCTURAL_QUAL_STRUCTURAL_ZIP")
	controlZip := os.Getenv("STRUCTURAL_QUAL_CONTROL_ZIP")
	if engine == "" || structuralZip == "" || controlZip == "" {
		t.Skip("STRUCTURAL_QUAL_* not set — run via scripts/{mysql,mariadb}-live-qual.sh")
	}

	if err := VerifyStructuralQueryText(structuralZip, controlZip, engine); err != nil {
		t.Fatalf("%v", err)
	}

	// Record the largest server-produced DIGEST_TEXT length (informational; the
	// user wants this measurement to inform a later, evidence-based cap
	// decision — it never gates qualification).
	if maxBytes, err := StructuralQualMaxObservedDigestBytes(structuralZip, engine); err == nil {
		t.Logf("structural-query qualification OK for %s: INV-SQT-01 held (no literal leakage), "+
			"redaction preserved, truncation proven; max observed server DIGEST_TEXT = %d bytes (Signals cap %d)",
			engine, maxBytes, singlestore.StructuralQueryTextMaxBytes)
	}
}
