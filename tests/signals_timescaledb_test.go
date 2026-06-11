package tests

import (
	"strings"
	"testing"

	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// ---------------------------------------------------------------------------
// timescaledb_family_v1 — TimescaleDB / Tiger Data collector family (R114).
//
// STDD failing-first slice for issue #73: these tests encode the
// specification and MUST fail until the implementation slice registers
// the family. Gated on the 'timescaledb' extension via the standard
// RequiresExtension filter — plain PostgreSQL targets skip the whole
// family with reason=extension_missing (EA-R001, INV-SIGNALS-24).
//
// Specification: specifications/collectors/timescaledb_family_v1.md
// Acceptance:    specifications/collectors/timescaledb_family_v1.acceptance.md
// Design note:   docs/timescaledb-collectors-design.md
// ---------------------------------------------------------------------------

// timescaleDBFamilyIDs is the closed list of R114 family members.
var timescaleDBFamilyIDs = []string{
	"timescaledb_extension_v1",
	"timescaledb_hypertables_v1",
	"timescaledb_dimensions_v1",
	"timescaledb_chunks_v1",
	"timescaledb_chunk_summary_v1",
	"timescaledb_hypertable_sizes_v1",
	"timescaledb_compression_settings_v1",
	"timescaledb_compression_stats_v1",
	"timescaledb_continuous_aggregates_v1",
	"timescaledb_jobs_v1",
	"timescaledb_job_stats_v1",
	"timescaledb_job_errors_v1",
}

// TC-TSDB-13: registration + category.
func TestTimescaleDBFamilyRegistered(t *testing.T) {
	for _, id := range timescaleDBFamilyIDs {
		q := pgqueries.ByID(id)
		if q == nil {
			t.Errorf("%s is not registered", id)
			continue
		}
		if q.Category != "timescaledb" {
			t.Errorf("%s category: got %q, want %q", id, q.Category, "timescaledb")
		}
	}
}

// TC-TSDB-13: extension gate + PG floor.
func TestTimescaleDBFamilyRequiresExtension(t *testing.T) {
	for _, id := range timescaleDBFamilyIDs {
		q := pgqueries.ByID(id)
		if q == nil {
			t.Errorf("%s not registered", id)
			continue
		}
		if q.RequiresExtension != "timescaledb" {
			t.Errorf("%s RequiresExtension: got %q, want %q", id, q.RequiresExtension, "timescaledb")
		}
		if q.MinPGVersion != 14 {
			t.Errorf("%s MinPGVersion: got %d, want 14", id, q.MinPGVersion)
		}
	}
}

// TC-TSDB-13: read-only linter (R002/R013). The linter independently
// blocks every mutating TimescaleDB API (add_job, compress_chunk,
// drop_chunks, ...) via the SELECT/WITH-only rule and keyword denylist.
func TestTimescaleDBFamilyPassesLinter(t *testing.T) {
	for _, id := range timescaleDBFamilyIDs {
		q := pgqueries.ByID(id)
		if q == nil {
			t.Errorf("%s not registered", id)
			continue
		}
		if err := pgqueries.LintQuery(q.SQL); err != nil {
			t.Errorf("%s failed linter: %v", id, err)
		}
	}
}

// TC-TSDB-01 / TC-TSDB-13: eligibility strictly follows extension
// presence (R014/R081 filter).
func TestTimescaleDBFamilyEligibilityFollowsExtension(t *testing.T) {
	with := filteredIDSet(pgqueries.FilterParams{
		PGMajorVersion:         17,
		Extensions:             []string{"timescaledb"},
		HighSensitivityEnabled: true,
	})
	without := filteredIDSet(pgqueries.FilterParams{
		PGMajorVersion:         17,
		Extensions:             nil,
		HighSensitivityEnabled: true,
	})
	for _, id := range timescaleDBFamilyIDs {
		if !with[id] {
			t.Errorf("%s missing from Filter output with timescaledb installed", id)
		}
		if without[id] {
			t.Errorf("%s eligible without the timescaledb extension", id)
		}
	}
}

// TC-TSDB-01: skipped members are accounted for, never silently absent
// (EA-R001, INV-SIGNALS-24).
func TestTimescaleDBFamilyGatedAsExtensionMissing(t *testing.T) {
	gated := pgqueries.GatedIDsByReason(pgqueries.FilterParams{
		PGMajorVersion:         17,
		Extensions:             nil,
		HighSensitivityEnabled: true,
	})
	missing := make(map[string]bool)
	for _, id := range gated[pgqueries.GateReasonExtensionMissing] {
		missing[id] = true
	}
	for _, id := range timescaleDBFamilyIDs {
		if !missing[id] {
			t.Errorf("%s not reported under reason=%s when timescaledb is absent",
				id, pgqueries.GateReasonExtensionMissing)
		}
	}
}

// TC-TSDB-07 / TC-TSDB-10: the two text-bearing members are
// high-sensitivity redact-path collectors (R075 revised); everything
// else is low sensitivity.
func TestTimescaleDBFamilySensitivity(t *testing.T) {
	redactPath := map[string][]string{
		"timescaledb_continuous_aggregates_v1": {"view_definition"},
		"timescaledb_job_errors_v1":            {"err_message"},
	}
	for _, id := range timescaleDBFamilyIDs {
		q := pgqueries.ByID(id)
		if q == nil {
			t.Errorf("%s not registered", id)
			continue
		}
		want, sensitive := redactPath[id]
		if sensitive {
			if !q.HighSensitivity {
				t.Errorf("%s: HighSensitivity = false, want true (redact path)", id)
			}
			if len(q.SensitiveColumns) != len(want) || q.SensitiveColumns[0] != want[0] {
				t.Errorf("%s SensitiveColumns: got %v, want %v", id, q.SensitiveColumns, want)
			}
			continue
		}
		if q.HighSensitivity {
			t.Errorf("%s: HighSensitivity = true, want false", id)
		}
		if len(q.SensitiveColumns) != 0 {
			t.Errorf("%s SensitiveColumns: got %v, want none", id, q.SensitiveColumns)
		}
	}
}

// TC-TSDB-15: chunk rows are bounded; the per-hypertable summary stays
// complete so truncation is always detectable.
func TestTimescaleDBChunksBounded(t *testing.T) {
	q := pgqueries.ByID("timescaledb_chunks_v1")
	if q == nil {
		t.Fatal("timescaledb_chunks_v1 not registered")
	}
	if !strings.Contains(strings.ToUpper(q.SQL), "LIMIT 5000") {
		t.Error("timescaledb_chunks_v1 SQL does not bound its output at LIMIT 5000")
	}
	if pgqueries.ByID("timescaledb_chunk_summary_v1") == nil {
		t.Error("timescaledb_chunk_summary_v1 (the unbounded-safe summary) not registered")
	}
}

// Bounded output, job-errors variant: the backing table is
// per-execution (a crash-looping job accumulates rows far faster than
// the monthly retention job prunes), so the rowset is capped
// newest-first.
func TestTimescaleDBJobErrorsBounded(t *testing.T) {
	q := pgqueries.ByID("timescaledb_job_errors_v1")
	if q == nil {
		t.Fatal("timescaledb_job_errors_v1 not registered")
	}
	if !strings.Contains(strings.ToUpper(q.SQL), "LIMIT 1000") {
		t.Error("timescaledb_job_errors_v1 SQL does not bound its output at LIMIT 1000")
	}
}

// TC-TSDB-13: spec-mandated cadence and retention per member.
func TestTimescaleDBFamilyCadenceAndRetention(t *testing.T) {
	expect := map[string]struct {
		cadence   pgqueries.Cadence
		retention pgqueries.RetentionClass
	}{
		"timescaledb_extension_v1":             {pgqueries.Cadence6h, pgqueries.RetentionLong},
		"timescaledb_hypertables_v1":           {pgqueries.Cadence6h, pgqueries.RetentionMedium},
		"timescaledb_dimensions_v1":            {pgqueries.CadenceDaily, pgqueries.RetentionMedium},
		"timescaledb_chunks_v1":                {pgqueries.Cadence6h, pgqueries.RetentionMedium},
		"timescaledb_chunk_summary_v1":         {pgqueries.Cadence1h, pgqueries.RetentionMedium},
		"timescaledb_hypertable_sizes_v1":      {pgqueries.Cadence1h, pgqueries.RetentionMedium},
		"timescaledb_compression_settings_v1":  {pgqueries.CadenceDaily, pgqueries.RetentionMedium},
		"timescaledb_compression_stats_v1":     {pgqueries.Cadence1h, pgqueries.RetentionMedium},
		"timescaledb_continuous_aggregates_v1": {pgqueries.Cadence6h, pgqueries.RetentionMedium},
		"timescaledb_jobs_v1":                  {pgqueries.Cadence1h, pgqueries.RetentionMedium},
		"timescaledb_job_stats_v1":             {pgqueries.Cadence15m, pgqueries.RetentionShort},
		"timescaledb_job_errors_v1":            {pgqueries.Cadence1h, pgqueries.RetentionMedium},
	}
	for id, want := range expect {
		q := pgqueries.ByID(id)
		if q == nil {
			t.Errorf("%s not registered", id)
			continue
		}
		if q.Cadence != want.cadence {
			t.Errorf("%s cadence: got %s, want %s", id, q.Cadence, want.cadence)
		}
		if q.RetentionClass != want.retention {
			t.Errorf("%s retention: got %s, want %s", id, q.RetentionClass, want.retention)
		}
	}
}

// TC-TSDB-02: the detection collector is the capability surface — it
// must probe by existence (to_regclass), read the extension row, and
// surface the edition GUC. Feature detection, not version tables.
func TestTimescaleDBDetectionUsesFeatureProbes(t *testing.T) {
	q := pgqueries.ByID("timescaledb_extension_v1")
	if q == nil {
		t.Fatal("timescaledb_extension_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	for _, needle := range []string{"pg_extension", "to_regclass", "timescaledb.license"} {
		if !strings.Contains(sql, needle) {
			t.Errorf("timescaledb_extension_v1 SQL does not reference %q", needle)
		}
	}
}

// filteredIDSet returns the set of query IDs eligible under p.
func filteredIDSet(p pgqueries.FilterParams) map[string]bool {
	out := make(map[string]bool)
	for _, q := range pgqueries.Filter(p) {
		out[q.ID] = true
	}
	return out
}
