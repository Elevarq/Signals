package tests

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/elevarq/signals/internal/pgqueries"
)

// ---------------------------------------------------------------------------
// index_health_summary_v2 — decision-grade index-health contract.
//
// Specification: specifications/collectors/index_health_summary_v2.md
// Acceptance:    specifications/collectors/index_health_summary_v2.acceptance.md
// ---------------------------------------------------------------------------

func v2(t *testing.T) *pgqueries.QueryDef {
	t.Helper()
	q := pgqueries.ByID("index_health_summary_v2")
	if q == nil {
		t.Fatal("index_health_summary_v2 is not registered")
	}
	return q
}

// TC-IHV2-01.
func TestIndexHealthV2Registered(t *testing.T) {
	q := v2(t)
	if q.Category != "indexes" {
		t.Errorf("category: got %q, want %q", q.Category, "indexes")
	}
	if q.Cadence != pgqueries.Cadence6h {
		t.Errorf("cadence: got %v, want Cadence6h", q.Cadence)
	}
	if q.RetentionClass != pgqueries.RetentionMedium {
		t.Errorf("retention: got %q, want RetentionMedium", q.RetentionClass)
	}
	if q.Timeout != 30*time.Second {
		t.Errorf("timeout: got %v, want 30s", q.Timeout)
	}
	if q.ResultKind != pgqueries.ResultRowset {
		t.Errorf("ResultKind: got %q, want rowset", q.ResultKind)
	}
}

// TC-IHV2-02: SQL passes the linter (incl. the CREATE/REINDEX keyword traps).
func TestIndexHealthV2PassesLinter(t *testing.T) {
	q := v2(t)
	if err := pgqueries.LintQuery(q.SQL); err != nil {
		t.Errorf("index_health_summary_v2 failed linter: %v", err)
	}
}

// TC-IHV2-03.
func TestIndexHealthV2ExcludesSystemSchemas(t *testing.T) {
	q := v2(t)
	for _, s := range []string{"'pg_catalog'", "'information_schema'", "'pg_toast'", `'pg\_temp\_%'`, `'pg\_toast\_temp\_%'`} {
		if !strings.Contains(q.SQL, s) {
			t.Errorf("SQL must exclude system schema token %s", s)
		}
	}
}

// TC-IHV2-04.
func TestIndexHealthV2OutputColumns(t *testing.T) {
	q := v2(t)
	sql := strings.ToLower(q.SQL)
	required := []string{
		"schemaname", "tablename", "indexname", "index_oid", "table_oid",
		"size_bytes", "idx_scan", "idx_tup_read", "idx_tup_fetch",
		"is_valid", "is_ready", "is_live", "is_primary", "is_unique",
		"is_exclusion", "is_immediate", "is_replica_identity",
		"is_constraint_backed", "constraint_type", "build_state",
		"access_method", "relation_kind", "is_partitioned",
		"key_column_count", "include_column_count",
		"structure_version", "structure_fingerprint",
		"exact_duplicate_of", "prefix_candidate_of", "prefix_candidate_basis",
	}
	for _, col := range required {
		if !strings.Contains(sql, col) {
			t.Errorf("SQL must reference column %q", col)
		}
	}
}

// TC-IHV2-05.
func TestIndexHealthV2NoSelectStar(t *testing.T) {
	q := v2(t)
	if strings.Contains(strings.ToLower(q.SQL), "select *") {
		t.Error("index_health_summary_v2 must not use SELECT *")
	}
}

// TC-IHV2-06.
func TestIndexHealthV2DeterministicOrder(t *testing.T) {
	q := v2(t)
	if !strings.Contains(q.SQL, "ORDER BY m.schemaname, m.tablename, m.indexname") {
		t.Error("outer SELECT must ORDER BY m.schemaname, m.tablename, m.indexname")
	}
}

// TC-IHV2-07: no safety synthesis on usage counters or state booleans.
func TestIndexHealthV2NoSafetySynthesis(t *testing.T) {
	q := v2(t)
	lc := strings.ToLower(q.SQL)
	// Usage counters must pass through raw (NULL preserved) — never COALESCEd.
	for _, bad := range []string{"coalesce(s.idx_scan", "coalesce(m.idx_scan", "coalesce(s.idx_tup", "coalesce(m.idx_tup"} {
		if strings.Contains(lc, bad) {
			t.Errorf("usage counter must not be coerced: found %q", bad)
		}
	}
	// No state boolean coerced to false.
	if strings.Contains(lc, ", false)") {
		t.Error("no state boolean may be COALESCE(..., false)")
	}
	// Raw passthrough present.
	if !strings.Contains(lc, "s.idx_scan") {
		t.Error("idx_scan must be emitted raw from pg_stat_user_indexes")
	}
}

// TC-IHV2-08.
func TestIndexHealthV2ConstraintCodes(t *testing.T) {
	q := v2(t)
	if !strings.Contains(q.SQL, "conindid") {
		t.Error("must join pg_constraint on conindid for constraint backing")
	}
	for _, code := range []string{"'primary'", "'unique'", "'exclusion'", "'other'"} {
		if !strings.Contains(q.SQL, code) {
			t.Errorf("controlled constraint-type code %s must be present", code)
		}
	}
}

// TC-IHV2-09.
func TestIndexHealthV2BuildStateCodes(t *testing.T) {
	q := v2(t)
	if !strings.Contains(q.SQL, "pg_stat_progress_create_index") {
		t.Error("must reference pg_stat_progress_create_index for live build state")
	}
	for _, code := range []string{"'active_build'", "'active_reindex'", "'invalid_residue'", "'not_ready_residue'", "'ready'"} {
		if !strings.Contains(q.SQL, code) {
			t.Errorf("controlled build-state code %s must be present", code)
		}
	}
}

// TC-IHV2-10: versioned fingerprint derived in-DB; duplicate keyed on fingerprint.
func TestIndexHealthV2FingerprintDuplicates(t *testing.T) {
	q := v2(t)
	if !strings.Contains(q.SQL, "structure_version") {
		t.Error("must emit structure_version")
	}
	if !strings.Contains(q.SQL, "md5(") || !strings.Contains(q.SQL, "pg_get_indexdef(") {
		t.Error("structure_fingerprint must be md5 over a pg_get_indexdef normalization")
	}
	if !strings.Contains(q.SQL, "m2.structure_fingerprint = m.structure_fingerprint") {
		t.Error("exact_duplicate_of must be selected by fingerprint equality, not key columns")
	}
}

// TC-IHV2-11: prefix relationship is a labelled candidate, never a verdict.
func TestIndexHealthV2PrefixIsCandidate(t *testing.T) {
	q := v2(t)
	if !strings.Contains(q.SQL, "'key_column_left_prefix'") {
		t.Error("prefix_candidate_basis literal 'key_column_left_prefix' must be present")
	}
	if strings.Contains(strings.ToLower(q.SQL), "'redundant'") {
		t.Error("v2 must not emit a 'redundant' verdict; the prefix relationship is a candidate only")
	}
}

// TC-IHV2-12.
func TestIndexHealthV2IncludedOnAllMajors(t *testing.T) {
	for _, major := range []int{14, 15, 16, 17, 18} {
		found := false
		for _, q := range pgqueries.Filter(pgqueries.FilterParams{PGMajorVersion: major, Extensions: []string{}}) {
			if q.ID == "index_health_summary_v2" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("index_health_summary_v2 must be included on PG %d", major)
		}
	}
}

// TC-IHV2-13: v1 compatibility preserved.
func TestIndexHealthV2V1StillRegistered(t *testing.T) {
	q := pgqueries.ByID("index_health_summary_v1")
	if q == nil {
		t.Fatal("index_health_summary_v1 must remain registered (v2 is additive)")
	}
	if q.Category != "indexes" || q.Cadence != pgqueries.Cadence6h ||
		q.RetentionClass != pgqueries.RetentionMedium || q.Timeout != 30*time.Second ||
		q.ResultKind != pgqueries.ResultRowset {
		t.Error("v1 configuration must be unchanged")
	}
}

// TC-IHV2-15: concurrent-DDL capability evidence (#294).
func TestIndexHealthV2PartitioningEvidence(t *testing.T) {
	q := v2(t)
	// Derived from pg_class.relkind, not coerced.
	if !strings.Contains(q.SQL, "ic.relkind = 'I'") {
		t.Error("is_partitioned must derive from pg_class.relkind = 'I'")
	}
	for _, lit := range []string{"'index'", "'partitioned_index'"} {
		if !strings.Contains(q.SQL, lit) {
			t.Errorf("controlled relation_kind literal %s must be present", lit)
		}
	}
	if strings.Contains(strings.ToLower(q.SQL), "coalesce(ic.relkind") {
		t.Error("relation_kind/is_partitioned must not be COALESCEd to a safe default")
	}
}

// TC-IHV2-14: inventory carries v2.
func TestIndexHealthV2InInventory(t *testing.T) {
	b, err := os.ReadFile("../specifications/collectors/collector-inventory.json")
	if err != nil {
		t.Fatalf("read inventory: %v", err)
	}
	if !strings.Contains(string(b), "index_health_summary_v2") {
		t.Error("collector-inventory.json must list index_health_summary_v2")
	}
}
