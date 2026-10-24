package collector

import (
	"strings"
	"testing"
)

// Tests derive from specifications/singlestore-live-qualification.acceptance.md.
// These are the DETERMINISTIC verifier-logic coverage (not producer evidence):
// they prove the qualification fails loudly on bad input and passes on
// legitimate input, with synthetic projected structs.

// goodSnapshot builds a full-grant snapshot where every expected collector
// succeeded with reconciled result evidence.
func goodSnapshot(id string, expected []string) QualSnapshot {
	runs := make([]QualRun, 0, len(expected))
	for i, q := range expected {
		runs = append(runs, QualRun{
			QueryID: q, Status: "success", RowCount: i, SnapshotID: id,
			HasResult: true, PersistedRows: i,
		})
	}
	return QualSnapshot{ID: id, Engine: "singlestore", Version: "9.1.1", Runs: runs}
}

// TC-SLQ-01 (logic leg): a good full-grant snapshot passes.
func TestVerifyCadenceSnapshot_GoodPasses(t *testing.T) {
	exp := SingleStoreExpectedCollectors()
	if err := VerifyCadenceSnapshot(goodSnapshot("snap-1", exp), exp, "9."); err != nil {
		t.Fatalf("good snapshot should pass, got: %v", err)
	}
}

// TC-SLQ-02 (SLQ-R004): an injected failed/skipped collector fails loudly — the
// verifier must NOT pass on distinct query ids.
func TestVerifyCadenceSnapshot_InjectedNonSuccessFailsLoudly(t *testing.T) {
	exp := SingleStoreExpectedCollectors()
	for _, bad := range []string{"failed", "skipped"} {
		s := goodSnapshot("snap-1", exp)
		s.Runs[3].Status = bad
		s.Runs[3].Reason = "permission_denied"
		err := VerifyCadenceSnapshot(s, exp, "9.")
		if err == nil {
			t.Fatalf("status=%q must fail the qualification, got pass", bad)
		}
		if !strings.Contains(err.Error(), s.Runs[3].QueryID) {
			t.Errorf("error must name the offending collector %q: %v", s.Runs[3].QueryID, err)
		}
	}
}

// TC-SLQ-03 (SLQ-R003): a legitimate zero-row success passes (0 == 0).
func TestVerifyCadenceSnapshot_ZeroRowSuccessPasses(t *testing.T) {
	exp := SingleStoreExpectedCollectors()
	s := goodSnapshot("snap-1", exp)
	s.Runs[0].RowCount = 0
	s.Runs[0].PersistedRows = 0
	s.Runs[0].HasResult = true
	if err := VerifyCadenceSnapshot(s, exp, "9."); err != nil {
		t.Fatalf("legitimate zero-row success must pass, got: %v", err)
	}
}

// TC-SLQ-04 (SLQ-R003): a success run with no result row, or a row-count
// mismatch, fails.
func TestVerifyCadenceSnapshot_MissingOrMismatchedResultFails(t *testing.T) {
	exp := SingleStoreExpectedCollectors()

	noResult := goodSnapshot("snap-1", exp)
	noResult.Runs[2].HasResult = false
	if err := VerifyCadenceSnapshot(noResult, exp, "9."); err == nil {
		t.Error("a success run with no query_results row must fail")
	}

	mismatch := goodSnapshot("snap-1", exp)
	mismatch.Runs[2].RowCount = 5
	mismatch.Runs[2].PersistedRows = 4
	if err := VerifyCadenceSnapshot(mismatch, exp, "9."); err == nil {
		t.Error("a row-count mismatch must fail")
	}
}

// SLQ-R002: a snapshot_id mismatch and a wrong engine/version fail.
func TestVerifyCadenceSnapshot_IdentityViolationsFail(t *testing.T) {
	exp := SingleStoreExpectedCollectors()

	wrongEngine := goodSnapshot("snap-1", exp)
	wrongEngine.Engine = "postgresql"
	if err := VerifyCadenceSnapshot(wrongEngine, exp, "9."); err == nil {
		t.Error("wrong engine must fail")
	}

	badSnapID := goodSnapshot("snap-1", exp)
	badSnapID.Runs[1].SnapshotID = "other-snap"
	if err := VerifyCadenceSnapshot(badSnapID, exp, "9."); err == nil {
		t.Error("snapshot_id mismatch must fail")
	}

	missing := goodSnapshot("snap-1", exp)
	missing.Runs = missing.Runs[:len(missing.Runs)-1] // drop one expected collector
	if err := VerifyCadenceSnapshot(missing, exp, "9."); err == nil {
		t.Error("a missing expected collector must fail")
	}
}

func goodExport(expected []string, s QualSnapshot) QualExport {
	z := QualExport{
		Engine: "singlestore", EngineVersion: "9.1.1",
		ExportedRows:  map[string]int{},
		ManifestNames: map[string]string{},
		MemberNames: []string{
			"metadata.json", "collector_status.json", "snapshots.ndjson",
			"query_catalog.json", "query_runs.ndjson", "query_results.ndjson", "manifest.json",
		},
	}
	for _, id := range expected {
		z.CollectorStatusIDs = append(z.CollectorStatusIDs, id)
		z.QueryCatalogIDs = append(z.QueryCatalogIDs, id)
	}
	for _, r := range s.Runs {
		if r.Status == "success" {
			z.ExportedRows[r.QueryID] = r.PersistedRows
		}
	}
	for _, m := range z.MemberNames {
		if m != "manifest.json" {
			z.ManifestNames[m] = "deadbeef"
		}
	}
	return z
}

// TC-SLQ-06 (logic leg): a reconciled export passes.
func TestVerifyExportReconciliation_GoodPasses(t *testing.T) {
	exp := SingleStoreExpectedCollectors()
	s := goodSnapshot("snap-1", exp)
	if err := VerifyExportReconciliation(s, goodExport(exp, s), exp, "9."); err != nil {
		t.Fatalf("reconciled export should pass, got: %v", err)
	}
}

// TC-SLQ-07 (SLQ-R007): a count mismatch, a foreign PostgreSQL collector, and a
// skipped run emitted as a payload each fail.
func TestVerifyExportReconciliation_ViolationsFail(t *testing.T) {
	exp := SingleStoreExpectedCollectors()

	s := goodSnapshot("snap-1", exp)
	z := goodExport(exp, s)
	z.ExportedRows[exp[0]] = s.Runs[0].PersistedRows + 1 // exported != persisted
	if err := VerifyExportReconciliation(s, z, exp, "9."); err == nil {
		t.Error("an exported/persisted row-count mismatch must fail")
	}

	s2 := goodSnapshot("snap-1", exp)
	z2 := goodExport(exp, s2)
	z2.QueryCatalogIDs = append(z2.QueryCatalogIDs, "pg_settings_v1") // foreign PG collector
	if err := VerifyExportReconciliation(s2, z2, exp, "9."); err == nil {
		t.Error("a foreign PostgreSQL collector in query_catalog must fail")
	}

	s3 := goodSnapshot("snap-1", exp)
	s3.Runs[0].Status = "skipped"
	z3 := goodExport(exp, s3)
	z3.ExportedRows[exp[0]] = 0 // skipped run must NOT have an exported payload
	if err := VerifyExportReconciliation(s3, z3, exp, "9."); err == nil {
		t.Error("a skipped run emitted as an exported payload must fail")
	}
}

// TC-SLQ-08 (SLQ-R008): a redaction breach fails.
func TestVerifyExportReconciliation_RedactionBreachFails(t *testing.T) {
	exp := SingleStoreExpectedCollectors()
	s := goodSnapshot("snap-1", exp)

	zt := goodExport(exp, s)
	zt.QueryTextPresent = true
	if err := VerifyExportReconciliation(s, zt, exp, "9."); err == nil {
		t.Error("a QUERY_TEXT leak must fail")
	}

	zi := goodExport(exp, s)
	zi.ProcessInfoPresent = true
	if err := VerifyExportReconciliation(s, zi, exp, "9."); err == nil {
		t.Error("a processlist INFO leak must fail")
	}
}

// TC-SLQ-09 (SLQ-R009): a member absent from the manifest, or a manifest entry
// absent from the ZIP, fails.
func TestVerifyExportReconciliation_ManifestCoverageFails(t *testing.T) {
	exp := SingleStoreExpectedCollectors()
	s := goodSnapshot("snap-1", exp)

	missing := goodExport(exp, s)
	delete(missing.ManifestNames, "query_results.ndjson") // member not in manifest
	if err := VerifyExportReconciliation(s, missing, exp, "9."); err == nil {
		t.Error("a ZIP member absent from the manifest must fail")
	}

	extra := goodExport(exp, s)
	extra.ManifestNames["ghost.json"] = "deadbeef" // manifest entry not in ZIP
	if err := VerifyExportReconciliation(s, extra, exp, "9."); err == nil {
		t.Error("a manifest entry absent from the ZIP must fail")
	}
}
