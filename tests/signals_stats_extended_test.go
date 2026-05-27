package tests

import (
	"strings"
	"testing"

	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// ---------------------------------------------------------------------------
// pg_stats_extended_v1 collector — high-sensitivity histogram stats
//
// Per the spec (specifications/collectors/pg_stats_extended_v1.md), this
// collector emits sampled-value columns from pg_stats (most_common_vals,
// most_common_freqs, histogram_bounds). It is HIGH-sensitivity by
// construction — output contains real customer data — so it ships
// disabled by default and runs only when the operator opts in via
// HighSensitivityEnabled.
// ---------------------------------------------------------------------------

// --- registration ---

func TestStatsExtendedCollectorRegistered(t *testing.T) {
	q := pgqueries.ByID("pg_stats_extended_v1")
	if q == nil {
		t.Fatal("pg_stats_extended_v1 is not registered")
	}
	if q.Category != "schema" {
		t.Errorf("category: got %q, want %q", q.Category, "schema")
	}
}

// --- linter ---

func TestStatsExtendedCollectorPassesLinter(t *testing.T) {
	q := pgqueries.ByID("pg_stats_extended_v1")
	if q == nil {
		t.Fatal("pg_stats_extended_v1 not registered")
	}
	if err := pgqueries.LintQuery(q.SQL); err != nil {
		t.Errorf("pg_stats_extended_v1 failed linter: %v", err)
	}
}

// --- cadence ---

func TestStatsExtendedCollectorCadence(t *testing.T) {
	q := pgqueries.ByID("pg_stats_extended_v1")
	if q == nil {
		t.Fatal("pg_stats_extended_v1 not registered")
	}
	if q.Cadence != pgqueries.CadenceDaily {
		t.Errorf("cadence: got %v, want CadenceDaily (24h)", q.Cadence)
	}
}

// --- retention is short (sampled values must not persist) ---

func TestStatsExtendedCollectorRetentionShort(t *testing.T) {
	q := pgqueries.ByID("pg_stats_extended_v1")
	if q == nil {
		t.Fatal("pg_stats_extended_v1 not registered")
	}
	if q.RetentionClass != pgqueries.RetentionShort {
		t.Errorf("RetentionClass: got %v, want RetentionShort (sensitive samples)",
			q.RetentionClass)
	}
}

// --- HighSensitivity flag set (gated off by default) ---

func TestStatsExtendedCollectorIsHighSensitivity(t *testing.T) {
	q := pgqueries.ByID("pg_stats_extended_v1")
	if q == nil {
		t.Fatal("pg_stats_extended_v1 not registered")
	}
	if !q.HighSensitivity {
		t.Error("pg_stats_extended_v1 must be HighSensitivity (emits real customer data)")
	}
}

// --- schema filter ---

func TestStatsExtendedCollectorExcludesSystemSchemas(t *testing.T) {
	q := pgqueries.ByID("pg_stats_extended_v1")
	if q == nil {
		t.Fatal("pg_stats_extended_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	for _, schema := range []string{"pg_catalog", "information_schema", "pg_toast", "pg_temp_"} {
		if !strings.Contains(sql, schema) {
			t.Errorf("pg_stats_extended_v1 must filter out %q", schema)
		}
	}
}

// --- deterministic ordering ---

func TestStatsExtendedCollectorHasOrderBy(t *testing.T) {
	q := pgqueries.ByID("pg_stats_extended_v1")
	if q == nil {
		t.Fatal("pg_stats_extended_v1 not registered")
	}
	if !strings.Contains(strings.ToUpper(q.SQL), "ORDER BY") {
		t.Error("pg_stats_extended_v1 must have ORDER BY for deterministic output")
	}
}

// --- explicit column list (no SELECT *) ---

func TestStatsExtendedCollectorNoSelectStar(t *testing.T) {
	q := pgqueries.ByID("pg_stats_extended_v1")
	if q == nil {
		t.Fatal("pg_stats_extended_v1 not registered")
	}
	if strings.Contains(q.SQL, "SELECT *") || strings.Contains(q.SQL, "select *") {
		t.Error("pg_stats_extended_v1 must not use SELECT *")
	}
}

// --- required output columns ---

func TestStatsExtendedCollectorOutputColumns(t *testing.T) {
	q := pgqueries.ByID("pg_stats_extended_v1")
	if q == nil {
		t.Fatal("pg_stats_extended_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	required := []string{
		"schemaname", "tablename", "attname",
		"most_common_vals", "most_common_freqs", "histogram_bounds",
	}
	for _, col := range required {
		if !strings.Contains(sql, col) {
			t.Errorf("pg_stats_extended_v1 must include column %q", col)
		}
	}
}

// --- excluded columns (array-only / disproportionate volume) ---

func TestStatsExtendedCollectorExcludesArrayColumns(t *testing.T) {
	q := pgqueries.ByID("pg_stats_extended_v1")
	if q == nil {
		t.Fatal("pg_stats_extended_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	excluded := []string{
		"most_common_elems",
		"most_common_elem_freqs",
		"elem_count_histogram",
	}
	for _, col := range excluded {
		if strings.Contains(sql, col) {
			t.Errorf("pg_stats_extended_v1 must NOT include %q (array-type only / disproportionate volume)",
				col)
		}
	}
}

// --- result kind ---

func TestStatsExtendedCollectorResultKind(t *testing.T) {
	q := pgqueries.ByID("pg_stats_extended_v1")
	if q == nil {
		t.Fatal("pg_stats_extended_v1 not registered")
	}
	if q.ResultKind != pgqueries.ResultRowset {
		t.Errorf("ResultKind: got %q, want rowset", q.ResultKind)
	}
}

// --- gating: filtered out when HighSensitivity disabled ---

func TestStatsExtendedCollectorGatedOffByDefault(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion:         18,
		Extensions:             []string{},
		HighSensitivityEnabled: false, // default
	})
	for _, q := range filtered {
		if q.ID == "pg_stats_extended_v1" {
			t.Error("pg_stats_extended_v1 must NOT appear in Filter when HighSensitivityEnabled=false")
		}
	}
}

// --- gating: included when HighSensitivity enabled ---

func TestStatsExtendedCollectorIncludedWhenEnabled(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion:         18,
		Extensions:             []string{},
		HighSensitivityEnabled: true,
	})
	found := false
	for _, q := range filtered {
		if q.ID == "pg_stats_extended_v1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("pg_stats_extended_v1 must appear in Filter when HighSensitivityEnabled=true")
	}
}

// --- gating reason: appears under config_disabled when off ---

func TestStatsExtendedCollectorGatingReasonIsConfigDisabled(t *testing.T) {
	gated := pgqueries.GatedIDsByReason(pgqueries.FilterParams{
		PGMajorVersion:         18,
		Extensions:             []string{},
		HighSensitivityEnabled: false,
	})
	ids := gated[pgqueries.GateReasonConfigDisabled]
	found := false
	for _, id := range ids {
		if id == "pg_stats_extended_v1" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("pg_stats_extended_v1 must appear under %q in GatedIDsByReason; got %v",
			pgqueries.GateReasonConfigDisabled, ids)
	}
}

// --- minimum PG version: works on PG14+ ---

func TestStatsExtendedCollectorIncludedOnPG14(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion:         14,
		Extensions:             []string{},
		HighSensitivityEnabled: true, // required to surface a HighSensitivity collector
	})
	for _, q := range filtered {
		if q.ID == "pg_stats_extended_v1" {
			return
		}
	}
	t.Error("pg_stats_extended_v1 must be included on PG 14")
}
