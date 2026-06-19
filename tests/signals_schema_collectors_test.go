package tests

import (
	"strings"
	"testing"

	"github.com/elevarq/signals/internal/pgqueries"
)

// ---------------------------------------------------------------------------
// Schema Metadata Collectors — Phase 1: pg_constraints_v1, pg_indexes_v1
// ---------------------------------------------------------------------------

var schemaPhase1 = []struct {
	id       string
	category string
}{
	{"pg_constraints_v1", "schema"},
	{"pg_indexes_v1", "schema"},
}

// --- Registration tests ---

func TestSchemaPhase1AllRegistered(t *testing.T) {
	for _, tc := range schemaPhase1 {
		q := pgqueries.ByID(tc.id)
		if q == nil {
			t.Errorf("collector %q is not registered", tc.id)
			continue
		}
		if q.Category != tc.category {
			t.Errorf("collector %q: category=%q, want %q", tc.id, q.Category, tc.category)
		}
	}
}

// --- Linter tests ---

func TestSchemaPhase1AllPassLinter(t *testing.T) {
	for _, tc := range schemaPhase1 {
		q := pgqueries.ByID(tc.id)
		if q == nil {
			t.Errorf("collector %q not registered", tc.id)
			continue
		}
		if err := pgqueries.LintQuery(q.SQL); err != nil {
			t.Errorf("collector %q failed linter: %v", tc.id, err)
		}
	}
}

// --- Cadence tests ---

func TestSchemaPhase1DefaultCadence(t *testing.T) {
	for _, tc := range schemaPhase1 {
		q := pgqueries.ByID(tc.id)
		if q == nil {
			continue
		}
		if q.Cadence != pgqueries.CadenceDaily {
			t.Errorf("collector %q: cadence=%v, want CadenceDaily (24h)", tc.id, q.Cadence)
		}
	}
}

// --- Schema filter tests ---

func TestSchemaPhase1ExcludesSystemSchemas(t *testing.T) {
	systemSchemas := []string{
		"pg_catalog", "information_schema", "pg_toast", "pg_temp_",
	}

	for _, tc := range schemaPhase1 {
		q := pgqueries.ByID(tc.id)
		if q == nil {
			continue
		}
		sql := strings.ToLower(q.SQL)
		for _, schema := range systemSchemas {
			if !strings.Contains(sql, schema) {
				t.Errorf("collector %q SQL must filter out %q", tc.id, schema)
			}
		}
	}
}

// --- Deterministic ordering tests ---

func TestSchemaPhase1HasOrderBy(t *testing.T) {
	for _, tc := range schemaPhase1 {
		q := pgqueries.ByID(tc.id)
		if q == nil {
			continue
		}
		if !containsCI(q.SQL, "ORDER BY") {
			t.Errorf("collector %q must have ORDER BY for deterministic output", tc.id)
		}
	}
}

// --- Output shape: explicit SELECT (no SELECT *) ---

func TestSchemaPhase1NoSelectStar(t *testing.T) {
	for _, tc := range schemaPhase1 {
		q := pgqueries.ByID(tc.id)
		if q == nil {
			continue
		}
		if strings.Contains(q.SQL, "SELECT *") || strings.Contains(q.SQL, "select *") {
			t.Errorf("collector %q must not use SELECT *", tc.id)
		}
	}
}

// --- No duplicate IDs ---

func TestSchemaPhase1NoDuplicateIDs(t *testing.T) {
	seen := make(map[string]bool)
	for _, q := range pgqueries.All() {
		if seen[q.ID] {
			t.Errorf("duplicate collector ID: %q", q.ID)
		}
		seen[q.ID] = true
	}
}

// --- Catalog count increases ---

func TestSchemaPhase1CatalogCount(t *testing.T) {
	all := pgqueries.All()
	// 29 existing + 2 new = 31 minimum
	if len(all) < 31 {
		t.Errorf("catalog has %d collectors, want at least 31 (29 existing + 2 schema)", len(all))
	}
}

// --- pg_constraints_v1 specific ---

func TestConstraintsCollectorOutputColumns(t *testing.T) {
	q := pgqueries.ByID("pg_constraints_v1")
	if q == nil {
		t.Fatal("pg_constraints_v1 not registered")
	}

	sql := strings.ToLower(q.SQL)

	requiredColumns := []string{
		"schemaname", "relname", "conname", "contype", "condef",
		"column_name", "column_position", "relkind", "n_live_tup",
		"confrelname", "confschemaname",
	}

	for _, col := range requiredColumns {
		if !strings.Contains(sql, col) {
			t.Errorf("pg_constraints_v1 must include column %q in output", col)
		}
	}
}

func TestConstraintsCollectorUsesUnnest(t *testing.T) {
	q := pgqueries.ByID("pg_constraints_v1")
	if q == nil {
		t.Fatal("pg_constraints_v1 not registered")
	}
	if !containsCI(q.SQL, "unnest") {
		t.Error("pg_constraints_v1 must use unnest(conkey) for multi-column support")
	}
	if !containsCI(q.SQL, "ordinality") {
		t.Error("pg_constraints_v1 must use WITH ORDINALITY for column_position")
	}
}

func TestConstraintsCollectorOrderByIncludesPosition(t *testing.T) {
	q := pgqueries.ByID("pg_constraints_v1")
	if q == nil {
		t.Fatal("pg_constraints_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	orderIdx := strings.LastIndex(sql, "order by")
	if orderIdx < 0 {
		t.Fatal("missing ORDER BY")
	}
	orderClause := sql[orderIdx:]
	if !strings.Contains(orderClause, "ordinality") && !strings.Contains(orderClause, "column_position") {
		t.Error("ORDER BY must include column_position/ordinality for determinism")
	}
}

func TestConstraintsCollectorResultKind(t *testing.T) {
	q := pgqueries.ByID("pg_constraints_v1")
	if q == nil {
		t.Fatal("pg_constraints_v1 not registered")
	}
	if q.ResultKind != pgqueries.ResultRowset {
		t.Errorf("ResultKind: got %q, want rowset", q.ResultKind)
	}
}

// --- pg_indexes_v1 specific ---

func TestIndexesCollectorOutputColumns(t *testing.T) {
	q := pgqueries.ByID("pg_indexes_v1")
	if q == nil {
		t.Fatal("pg_indexes_v1 not registered")
	}

	sql := strings.ToLower(q.SQL)

	requiredColumns := []string{
		"schemaname", "tablename", "indexname", "indexdef", "tablespace",
	}

	for _, col := range requiredColumns {
		if !strings.Contains(sql, col) {
			t.Errorf("pg_indexes_v1 must include column %q in output", col)
		}
	}
}

func TestIndexesCollectorIncludesIndexdef(t *testing.T) {
	q := pgqueries.ByID("pg_indexes_v1")
	if q == nil {
		t.Fatal("pg_indexes_v1 not registered")
	}
	if !containsCI(q.SQL, "indexdef") {
		t.Error("pg_indexes_v1 must include indexdef for leading-column parsing")
	}
}

func TestIndexesCollectorResultKind(t *testing.T) {
	q := pgqueries.ByID("pg_indexes_v1")
	if q == nil {
		t.Fatal("pg_indexes_v1 not registered")
	}
	if q.ResultKind != pgqueries.ResultRowset {
		t.Errorf("ResultKind: got %q, want rowset", q.ResultKind)
	}
}

func TestIndexesCollectorUsesCoalesce(t *testing.T) {
	// tablespace should use COALESCE to return empty string instead of null
	q := pgqueries.ByID("pg_indexes_v1")
	if q == nil {
		t.Fatal("pg_indexes_v1 not registered")
	}
	if !containsCI(q.SQL, "COALESCE") {
		t.Error("pg_indexes_v1 should COALESCE tablespace to empty string")
	}
}

// ---------------------------------------------------------------------------
// Schema Metadata Collectors — Phase 1 Step 3: pg_stats_v1
// ---------------------------------------------------------------------------

// --- pg_stats_v1 registration ---

func TestStatsCollectorRegistered(t *testing.T) {
	q := pgqueries.ByID("pg_stats_v1")
	if q == nil {
		t.Fatal("pg_stats_v1 is not registered")
	}
	if q.Category != "schema" {
		t.Errorf("category: got %q, want %q", q.Category, "schema")
	}
}

// --- pg_stats_v1 linter ---

func TestStatsCollectorPassesLinter(t *testing.T) {
	q := pgqueries.ByID("pg_stats_v1")
	if q == nil {
		t.Fatal("pg_stats_v1 not registered")
	}
	if err := pgqueries.LintQuery(q.SQL); err != nil {
		t.Errorf("pg_stats_v1 failed linter: %v", err)
	}
}

// --- pg_stats_v1 cadence ---

func TestStatsCollectorCadence(t *testing.T) {
	q := pgqueries.ByID("pg_stats_v1")
	if q == nil {
		t.Fatal("pg_stats_v1 not registered")
	}
	if q.Cadence != pgqueries.CadenceDaily {
		t.Errorf("cadence: got %v, want CadenceDaily (24h)", q.Cadence)
	}
}

// --- pg_stats_v1 schema filter ---

func TestStatsCollectorExcludesSystemSchemas(t *testing.T) {
	q := pgqueries.ByID("pg_stats_v1")
	if q == nil {
		t.Fatal("pg_stats_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	for _, schema := range []string{"pg_catalog", "information_schema", "pg_toast", "pg_temp_"} {
		if !strings.Contains(sql, schema) {
			t.Errorf("pg_stats_v1 must filter out %q", schema)
		}
	}
}

// --- pg_stats_v1 deterministic ordering ---

func TestStatsCollectorHasOrderBy(t *testing.T) {
	q := pgqueries.ByID("pg_stats_v1")
	if q == nil {
		t.Fatal("pg_stats_v1 not registered")
	}
	if !containsCI(q.SQL, "ORDER BY") {
		t.Error("pg_stats_v1 must have ORDER BY for deterministic output")
	}
}

// --- pg_stats_v1 explicit column list ---

func TestStatsCollectorNoSelectStar(t *testing.T) {
	q := pgqueries.ByID("pg_stats_v1")
	if q == nil {
		t.Fatal("pg_stats_v1 not registered")
	}
	if strings.Contains(q.SQL, "SELECT *") || strings.Contains(q.SQL, "select *") {
		t.Error("pg_stats_v1 must not use SELECT *")
	}
}

// --- pg_stats_v1 required output columns ---

func TestStatsCollectorOutputColumns(t *testing.T) {
	q := pgqueries.ByID("pg_stats_v1")
	if q == nil {
		t.Fatal("pg_stats_v1 not registered")
	}

	sql := strings.ToLower(q.SQL)

	required := []string{
		"schemaname", "tablename", "attname",
		"n_distinct", "correlation", "null_frac", "avg_width",
	}

	for _, col := range required {
		if !strings.Contains(sql, col) {
			t.Errorf("pg_stats_v1 must include column %q", col)
		}
	}
}

// --- pg_stats_v1 excluded columns (data samples) ---

func TestStatsCollectorExcludesDataSamples(t *testing.T) {
	q := pgqueries.ByID("pg_stats_v1")
	if q == nil {
		t.Fatal("pg_stats_v1 not registered")
	}

	sql := strings.ToLower(q.SQL)

	excluded := []string{
		"most_common_vals", "most_common_freqs",
		"histogram_bounds", "most_common_elems",
		"most_common_elem_freqs", "elem_count_histogram",
	}

	for _, col := range excluded {
		if strings.Contains(sql, col) {
			t.Errorf("pg_stats_v1 must NOT include %q (contains data samples)", col)
		}
	}
}

// --- pg_stats_v1 result kind ---

func TestStatsCollectorResultKind(t *testing.T) {
	q := pgqueries.ByID("pg_stats_v1")
	if q == nil {
		t.Fatal("pg_stats_v1 not registered")
	}
	if q.ResultKind != pgqueries.ResultRowset {
		t.Errorf("ResultKind: got %q, want rowset", q.ResultKind)
	}
}

// --- pg_stats_v1 included on PG 14 ---

func TestStatsCollectorIncludedOnPG14(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 14,
		Extensions:     []string{},
	})
	found := false
	for _, q := range filtered {
		if q.ID == "pg_stats_v1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("pg_stats_v1 must be included on PG 14")
	}
}

// ---------------------------------------------------------------------------
// Schema Metadata Collectors — Phase 1 Step 4: pg_columns_v1
// ---------------------------------------------------------------------------

// --- pg_columns_v1 registration ---

func TestColumnsCollectorRegistered(t *testing.T) {
	q := pgqueries.ByID("pg_columns_v1")
	if q == nil {
		t.Fatal("pg_columns_v1 is not registered")
	}
	if q.Category != "schema" {
		t.Errorf("category: got %q, want %q", q.Category, "schema")
	}
}

func TestColumnsCollectorPassesLinter(t *testing.T) {
	q := pgqueries.ByID("pg_columns_v1")
	if q == nil {
		t.Fatal("pg_columns_v1 not registered")
	}
	if err := pgqueries.LintQuery(q.SQL); err != nil {
		t.Errorf("pg_columns_v1 failed linter: %v", err)
	}
}

func TestColumnsCollectorCadence(t *testing.T) {
	q := pgqueries.ByID("pg_columns_v1")
	if q == nil {
		t.Fatal("pg_columns_v1 not registered")
	}
	if q.Cadence != pgqueries.CadenceDaily {
		t.Errorf("cadence: got %v, want CadenceDaily (24h)", q.Cadence)
	}
}

func TestColumnsCollectorExcludesSystemSchemas(t *testing.T) {
	q := pgqueries.ByID("pg_columns_v1")
	if q == nil {
		t.Fatal("pg_columns_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	for _, schema := range []string{"pg_catalog", "information_schema", "pg_toast", "pg_temp_"} {
		if !strings.Contains(sql, schema) {
			t.Errorf("pg_columns_v1 must filter out %q", schema)
		}
	}
}

func TestColumnsCollectorHasOrderBy(t *testing.T) {
	q := pgqueries.ByID("pg_columns_v1")
	if q == nil {
		t.Fatal("pg_columns_v1 not registered")
	}
	if !containsCI(q.SQL, "ORDER BY") {
		t.Error("pg_columns_v1 must have ORDER BY for deterministic output")
	}
}

func TestColumnsCollectorNoSelectStar(t *testing.T) {
	q := pgqueries.ByID("pg_columns_v1")
	if q == nil {
		t.Fatal("pg_columns_v1 not registered")
	}
	if strings.Contains(q.SQL, "SELECT *") || strings.Contains(q.SQL, "select *") {
		t.Error("pg_columns_v1 must not use SELECT *")
	}
}

func TestColumnsCollectorOutputColumns(t *testing.T) {
	q := pgqueries.ByID("pg_columns_v1")
	if q == nil {
		t.Fatal("pg_columns_v1 not registered")
	}

	sql := strings.ToLower(q.SQL)

	required := []string{
		"schemaname", "relname", "attname", "attnum",
		"typname", "is_nullable", "has_default", "attlen",
	}

	for _, col := range required {
		if !strings.Contains(sql, col) {
			t.Errorf("pg_columns_v1 must include column %q", col)
		}
	}
}

func TestColumnsCollectorUsesFormatType(t *testing.T) {
	q := pgqueries.ByID("pg_columns_v1")
	if q == nil {
		t.Fatal("pg_columns_v1 not registered")
	}
	if !containsCI(q.SQL, "format_type") {
		t.Error("pg_columns_v1 must use format_type() for human-readable type names")
	}
}

func TestColumnsCollectorUsesPgAttribute(t *testing.T) {
	q := pgqueries.ByID("pg_columns_v1")
	if q == nil {
		t.Fatal("pg_columns_v1 not registered")
	}
	if !containsCI(q.SQL, "pg_attribute") {
		t.Error("pg_columns_v1 must use pg_attribute (PostgreSQL-native catalog)")
	}
}

func TestColumnsCollectorExcludesDefaultText(t *testing.T) {
	q := pgqueries.ByID("pg_columns_v1")
	if q == nil {
		t.Fatal("pg_columns_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	// pg_get_expr would extract the default expression text — must not appear
	if strings.Contains(sql, "pg_get_expr") {
		t.Error("pg_columns_v1 must NOT use pg_get_expr (default expression may contain sensitive values)")
	}
	// column_default from information_schema is also not allowed
	if strings.Contains(sql, "column_default") {
		t.Error("pg_columns_v1 must NOT include column_default text")
	}
}

func TestColumnsCollectorExcludesSystemColumns(t *testing.T) {
	q := pgqueries.ByID("pg_columns_v1")
	if q == nil {
		t.Fatal("pg_columns_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	// Must filter attnum > 0 (system columns like ctid, xmin have attnum <= 0)
	if !strings.Contains(sql, "attnum > 0") {
		t.Error("pg_columns_v1 must filter attnum > 0 to exclude system columns")
	}
}

func TestColumnsCollectorExcludesDroppedColumns(t *testing.T) {
	q := pgqueries.ByID("pg_columns_v1")
	if q == nil {
		t.Fatal("pg_columns_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	if !strings.Contains(sql, "attisdropped") {
		t.Error("pg_columns_v1 must filter out dropped columns")
	}
}

func TestColumnsCollectorResultKind(t *testing.T) {
	q := pgqueries.ByID("pg_columns_v1")
	if q == nil {
		t.Fatal("pg_columns_v1 not registered")
	}
	if q.ResultKind != pgqueries.ResultRowset {
		t.Errorf("ResultKind: got %q, want rowset", q.ResultKind)
	}
}

func TestColumnsCollectorIncludedOnPG14(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 14,
		Extensions:     []string{},
	})
	found := false
	for _, q := range filtered {
		if q.ID == "pg_columns_v1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("pg_columns_v1 must be included on PG 14")
	}
}

// ---------------------------------------------------------------------------
// Schema Metadata Collectors — Phase 2 Step 5: pg_schemas_v1
// ---------------------------------------------------------------------------

func TestSchemasCollectorRegistered(t *testing.T) {
	q := pgqueries.ByID("pg_schemas_v1")
	if q == nil {
		t.Fatal("pg_schemas_v1 is not registered")
	}
	if q.Category != "schema" {
		t.Errorf("category: got %q, want %q", q.Category, "schema")
	}
}

func TestSchemasCollectorPassesLinter(t *testing.T) {
	q := pgqueries.ByID("pg_schemas_v1")
	if q == nil {
		t.Fatal("pg_schemas_v1 not registered")
	}
	if err := pgqueries.LintQuery(q.SQL); err != nil {
		t.Errorf("pg_schemas_v1 failed linter: %v", err)
	}
}

func TestSchemasCollectorCadence(t *testing.T) {
	q := pgqueries.ByID("pg_schemas_v1")
	if q == nil {
		t.Fatal("pg_schemas_v1 not registered")
	}
	if q.Cadence != pgqueries.CadenceDaily {
		t.Errorf("cadence: got %v, want CadenceDaily (24h)", q.Cadence)
	}
}

func TestSchemasCollectorExcludesSystemSchemas(t *testing.T) {
	q := pgqueries.ByID("pg_schemas_v1")
	if q == nil {
		t.Fatal("pg_schemas_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	for _, schema := range []string{"pg_catalog", "information_schema", "pg_toast", "pg_temp_"} {
		if !strings.Contains(sql, schema) {
			t.Errorf("pg_schemas_v1 must filter out %q", schema)
		}
	}
}

func TestSchemasCollectorHasOrderBy(t *testing.T) {
	q := pgqueries.ByID("pg_schemas_v1")
	if q == nil {
		t.Fatal("pg_schemas_v1 not registered")
	}
	if !containsCI(q.SQL, "ORDER BY") {
		t.Error("pg_schemas_v1 must have ORDER BY for deterministic output")
	}
}

func TestSchemasCollectorNoSelectStar(t *testing.T) {
	q := pgqueries.ByID("pg_schemas_v1")
	if q == nil {
		t.Fatal("pg_schemas_v1 not registered")
	}
	if strings.Contains(q.SQL, "SELECT *") || strings.Contains(q.SQL, "select *") {
		t.Error("pg_schemas_v1 must not use SELECT *")
	}
}

func TestSchemasCollectorOutputColumns(t *testing.T) {
	q := pgqueries.ByID("pg_schemas_v1")
	if q == nil {
		t.Fatal("pg_schemas_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)

	for _, col := range []string{"nspname", "nspowner", "is_default"} {
		if !strings.Contains(sql, col) {
			t.Errorf("pg_schemas_v1 must include column %q", col)
		}
	}
}

func TestSchemasCollectorUsesPgNamespace(t *testing.T) {
	q := pgqueries.ByID("pg_schemas_v1")
	if q == nil {
		t.Fatal("pg_schemas_v1 not registered")
	}
	if !containsCI(q.SQL, "pg_namespace") {
		t.Error("pg_schemas_v1 must use pg_namespace")
	}
}

func TestSchemasCollectorJoinsRoles(t *testing.T) {
	q := pgqueries.ByID("pg_schemas_v1")
	if q == nil {
		t.Fatal("pg_schemas_v1 not registered")
	}
	if !containsCI(q.SQL, "pg_roles") {
		t.Error("pg_schemas_v1 must join pg_roles for owner name")
	}
}

func TestSchemasCollectorResultKind(t *testing.T) {
	q := pgqueries.ByID("pg_schemas_v1")
	if q == nil {
		t.Fatal("pg_schemas_v1 not registered")
	}
	if q.ResultKind != pgqueries.ResultRowset {
		t.Errorf("ResultKind: got %q, want rowset", q.ResultKind)
	}
}

func TestSchemasCollectorIncludedOnPG14(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 14,
		Extensions:     []string{},
	})
	found := false
	for _, q := range filtered {
		if q.ID == "pg_schemas_v1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("pg_schemas_v1 must be included on PG 14")
	}
}

// ---------------------------------------------------------------------------
// Schema Metadata Collectors — Phase 2 Step 6: pg_views_v1
// ---------------------------------------------------------------------------

func TestViewsCollectorRegistered(t *testing.T) {
	q := pgqueries.ByID("pg_views_v1")
	if q == nil {
		t.Fatal("pg_views_v1 is not registered")
	}
	if q.Category != "schema" {
		t.Errorf("category: got %q, want %q", q.Category, "schema")
	}
}

func TestViewsCollectorPassesLinter(t *testing.T) {
	q := pgqueries.ByID("pg_views_v1")
	if q == nil {
		t.Fatal("pg_views_v1 not registered")
	}
	if err := pgqueries.LintQuery(q.SQL); err != nil {
		t.Errorf("pg_views_v1 failed linter: %v", err)
	}
}

func TestViewsCollectorCadence(t *testing.T) {
	q := pgqueries.ByID("pg_views_v1")
	if q == nil {
		t.Fatal("pg_views_v1 not registered")
	}
	if q.Cadence != pgqueries.CadenceDaily {
		t.Errorf("cadence: got %v, want CadenceDaily (24h)", q.Cadence)
	}
}

func TestViewsCollectorExcludesSystemSchemas(t *testing.T) {
	q := pgqueries.ByID("pg_views_v1")
	if q == nil {
		t.Fatal("pg_views_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	for _, schema := range []string{"pg_catalog", "information_schema", "pg_toast", "pg_temp_"} {
		if !strings.Contains(sql, schema) {
			t.Errorf("pg_views_v1 must filter out %q", schema)
		}
	}
}

func TestViewsCollectorHasOrderBy(t *testing.T) {
	q := pgqueries.ByID("pg_views_v1")
	if q == nil {
		t.Fatal("pg_views_v1 not registered")
	}
	if !containsCI(q.SQL, "ORDER BY") {
		t.Error("pg_views_v1 must have ORDER BY for deterministic output")
	}
}

func TestViewsCollectorNoSelectStar(t *testing.T) {
	q := pgqueries.ByID("pg_views_v1")
	if q == nil {
		t.Fatal("pg_views_v1 not registered")
	}
	if strings.Contains(q.SQL, "SELECT *") || strings.Contains(q.SQL, "select *") {
		t.Error("pg_views_v1 must not use SELECT *")
	}
}

func TestViewsCollectorInventoryColumns(t *testing.T) {
	q := pgqueries.ByID("pg_views_v1")
	if q == nil {
		t.Fatal("pg_views_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)

	for _, col := range []string{"schemaname", "viewname", "viewowner"} {
		if !strings.Contains(sql, col) {
			t.Errorf("pg_views_v1 must include column %q", col)
		}
	}
}

func TestViewsCollectorInventoryModeNoDefinition(t *testing.T) {
	// Default (inventory) mode must NOT include view definition text
	q := pgqueries.ByID("pg_views_v1")
	if q == nil {
		t.Fatal("pg_views_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)

	if strings.Contains(sql, "pg_get_viewdef") {
		t.Error("inventory mode must not include pg_get_viewdef (definition text)")
	}
	if strings.Contains(sql, "definition") {
		// Check it's not an alias — "definition" as an output column
		// Would appear as "AS definition" in the SQL
		if strings.Contains(sql, "as definition") {
			t.Error("inventory mode must not include a 'definition' output column")
		}
	}
}

func TestViewsCollectorUsesPgViews(t *testing.T) {
	q := pgqueries.ByID("pg_views_v1")
	if q == nil {
		t.Fatal("pg_views_v1 not registered")
	}
	if !containsCI(q.SQL, "pg_views") {
		t.Error("pg_views_v1 must query from pg_views")
	}
}

func TestViewsCollectorResultKind(t *testing.T) {
	q := pgqueries.ByID("pg_views_v1")
	if q == nil {
		t.Fatal("pg_views_v1 not registered")
	}
	if q.ResultKind != pgqueries.ResultRowset {
		t.Errorf("ResultKind: got %q, want rowset", q.ResultKind)
	}
}

func TestViewsCollectorIncludedOnPG14(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 14,
		Extensions:     []string{},
	})
	found := false
	for _, q := range filtered {
		if q.ID == "pg_views_v1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("pg_views_v1 must be included on PG 14")
	}
}

// --- Definition mode collector ---

func TestViewsDefinitionsCollectorRegistered(t *testing.T) {
	q := pgqueries.ByID("pg_views_definitions_v1")
	if q == nil {
		t.Fatal("pg_views_definitions_v1 is not registered")
	}
	if q.Category != "schema" {
		t.Errorf("category: got %q, want %q", q.Category, "schema")
	}
}

func TestViewsDefinitionsCollectorPassesLinter(t *testing.T) {
	q := pgqueries.ByID("pg_views_definitions_v1")
	if q == nil {
		t.Fatal("pg_views_definitions_v1 not registered")
	}
	if err := pgqueries.LintQuery(q.SQL); err != nil {
		t.Errorf("pg_views_definitions_v1 failed linter: %v", err)
	}
}

func TestViewsDefinitionsCollectorIncludesDefinition(t *testing.T) {
	q := pgqueries.ByID("pg_views_definitions_v1")
	if q == nil {
		t.Fatal("pg_views_definitions_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)

	// Must include all inventory columns plus definition
	for _, col := range []string{"schemaname", "viewname", "viewowner", "definition"} {
		if !strings.Contains(sql, col) {
			t.Errorf("pg_views_definitions_v1 must include column %q", col)
		}
	}
}

// ---------------------------------------------------------------------------
// Schema Metadata Collectors — Phase 2 Step 7: pg_matviews_v1
// ---------------------------------------------------------------------------

// --- pg_matviews_v1 inventory mode ---

func TestMatviewsCollectorRegistered(t *testing.T) {
	q := pgqueries.ByID("pg_matviews_v1")
	if q == nil {
		t.Fatal("pg_matviews_v1 is not registered")
	}
	if q.Category != "schema" {
		t.Errorf("category: got %q, want %q", q.Category, "schema")
	}
}

func TestMatviewsCollectorPassesLinter(t *testing.T) {
	q := pgqueries.ByID("pg_matviews_v1")
	if q == nil {
		t.Fatal("pg_matviews_v1 not registered")
	}
	if err := pgqueries.LintQuery(q.SQL); err != nil {
		t.Errorf("pg_matviews_v1 failed linter: %v", err)
	}
}

func TestMatviewsCollectorCadence(t *testing.T) {
	q := pgqueries.ByID("pg_matviews_v1")
	if q == nil {
		t.Fatal("pg_matviews_v1 not registered")
	}
	if q.Cadence != pgqueries.CadenceDaily {
		t.Errorf("cadence: got %v, want CadenceDaily (24h)", q.Cadence)
	}
}

func TestMatviewsCollectorExcludesSystemSchemas(t *testing.T) {
	q := pgqueries.ByID("pg_matviews_v1")
	if q == nil {
		t.Fatal("pg_matviews_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	for _, schema := range []string{"pg_catalog", "information_schema", "pg_toast", "pg_temp_"} {
		if !strings.Contains(sql, schema) {
			t.Errorf("pg_matviews_v1 must filter out %q", schema)
		}
	}
}

func TestMatviewsCollectorHasOrderBy(t *testing.T) {
	q := pgqueries.ByID("pg_matviews_v1")
	if q == nil {
		t.Fatal("pg_matviews_v1 not registered")
	}
	if !containsCI(q.SQL, "ORDER BY") {
		t.Error("pg_matviews_v1 must have ORDER BY for deterministic output")
	}
}

func TestMatviewsCollectorNoSelectStar(t *testing.T) {
	q := pgqueries.ByID("pg_matviews_v1")
	if q == nil {
		t.Fatal("pg_matviews_v1 not registered")
	}
	if strings.Contains(q.SQL, "SELECT *") || strings.Contains(q.SQL, "select *") {
		t.Error("pg_matviews_v1 must not use SELECT *")
	}
}

func TestMatviewsCollectorInventoryColumns(t *testing.T) {
	q := pgqueries.ByID("pg_matviews_v1")
	if q == nil {
		t.Fatal("pg_matviews_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)

	for _, col := range []string{"schemaname", "matviewname", "matviewowner", "ispopulated", "hasindexes"} {
		if !strings.Contains(sql, col) {
			t.Errorf("pg_matviews_v1 must include column %q", col)
		}
	}
}

func TestMatviewsCollectorInventoryModeNoDefinition(t *testing.T) {
	q := pgqueries.ByID("pg_matviews_v1")
	if q == nil {
		t.Fatal("pg_matviews_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)

	if strings.Contains(sql, "pg_get_viewdef") {
		t.Error("inventory mode must not include pg_get_viewdef")
	}
	if strings.Contains(sql, "as definition") {
		t.Error("inventory mode must not include a 'definition' output column")
	}
}

func TestMatviewsCollectorUsesPgMatviews(t *testing.T) {
	q := pgqueries.ByID("pg_matviews_v1")
	if q == nil {
		t.Fatal("pg_matviews_v1 not registered")
	}
	if !containsCI(q.SQL, "pg_matviews") {
		t.Error("pg_matviews_v1 must query from pg_matviews")
	}
}

func TestMatviewsCollectorResultKind(t *testing.T) {
	q := pgqueries.ByID("pg_matviews_v1")
	if q == nil {
		t.Fatal("pg_matviews_v1 not registered")
	}
	if q.ResultKind != pgqueries.ResultRowset {
		t.Errorf("ResultKind: got %q, want rowset", q.ResultKind)
	}
}

func TestMatviewsCollectorIncludedOnPG14(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 14,
		Extensions:     []string{},
	})
	found := false
	for _, q := range filtered {
		if q.ID == "pg_matviews_v1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("pg_matviews_v1 must be included on PG 14")
	}
}

// --- pg_matviews_definitions_v1 definition mode ---

func TestMatviewsDefinitionsCollectorRegistered(t *testing.T) {
	q := pgqueries.ByID("pg_matviews_definitions_v1")
	if q == nil {
		t.Fatal("pg_matviews_definitions_v1 is not registered")
	}
	if q.Category != "schema" {
		t.Errorf("category: got %q, want %q", q.Category, "schema")
	}
}

func TestMatviewsDefinitionsCollectorPassesLinter(t *testing.T) {
	q := pgqueries.ByID("pg_matviews_definitions_v1")
	if q == nil {
		t.Fatal("pg_matviews_definitions_v1 not registered")
	}
	if err := pgqueries.LintQuery(q.SQL); err != nil {
		t.Errorf("pg_matviews_definitions_v1 failed linter: %v", err)
	}
}

func TestMatviewsDefinitionsCollectorIncludesDefinition(t *testing.T) {
	q := pgqueries.ByID("pg_matviews_definitions_v1")
	if q == nil {
		t.Fatal("pg_matviews_definitions_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)

	for _, col := range []string{"schemaname", "matviewname", "matviewowner", "ispopulated", "definition"} {
		if !strings.Contains(sql, col) {
			t.Errorf("pg_matviews_definitions_v1 must include column %q", col)
		}
	}
}

// ---------------------------------------------------------------------------
// Schema Metadata Collectors — Phase 2 Step 8: pg_partitions_v1
// ---------------------------------------------------------------------------

func TestPartitionsCollectorRegistered(t *testing.T) {
	q := pgqueries.ByID("pg_partitions_v1")
	if q == nil {
		t.Fatal("pg_partitions_v1 is not registered")
	}
	if q.Category != "schema" {
		t.Errorf("category: got %q, want %q", q.Category, "schema")
	}
}

func TestPartitionsCollectorPassesLinter(t *testing.T) {
	q := pgqueries.ByID("pg_partitions_v1")
	if q == nil {
		t.Fatal("pg_partitions_v1 not registered")
	}
	if err := pgqueries.LintQuery(q.SQL); err != nil {
		t.Errorf("pg_partitions_v1 failed linter: %v", err)
	}
}

func TestPartitionsCollectorCadence(t *testing.T) {
	q := pgqueries.ByID("pg_partitions_v1")
	if q == nil {
		t.Fatal("pg_partitions_v1 not registered")
	}
	if q.Cadence != pgqueries.CadenceDaily {
		t.Errorf("cadence: got %v, want CadenceDaily (24h)", q.Cadence)
	}
}

func TestPartitionsCollectorExcludesSystemSchemas(t *testing.T) {
	q := pgqueries.ByID("pg_partitions_v1")
	if q == nil {
		t.Fatal("pg_partitions_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	for _, schema := range []string{"pg_catalog", "information_schema", "pg_toast", "pg_temp_"} {
		if !strings.Contains(sql, schema) {
			t.Errorf("pg_partitions_v1 must filter out %q", schema)
		}
	}
}

func TestPartitionsCollectorHasOrderBy(t *testing.T) {
	q := pgqueries.ByID("pg_partitions_v1")
	if q == nil {
		t.Fatal("pg_partitions_v1 not registered")
	}
	if !containsCI(q.SQL, "ORDER BY") {
		t.Error("pg_partitions_v1 must have ORDER BY for deterministic output")
	}
}

func TestPartitionsCollectorNoSelectStar(t *testing.T) {
	q := pgqueries.ByID("pg_partitions_v1")
	if q == nil {
		t.Fatal("pg_partitions_v1 not registered")
	}
	if strings.Contains(q.SQL, "SELECT *") || strings.Contains(q.SQL, "select *") {
		t.Error("pg_partitions_v1 must not use SELECT *")
	}
}

func TestPartitionsCollectorOutputColumns(t *testing.T) {
	q := pgqueries.ByID("pg_partitions_v1")
	if q == nil {
		t.Fatal("pg_partitions_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)

	for _, col := range []string{
		"parent_schema", "parent_name", "partition_strategy",
		"partition_key", "child_schema", "child_name", "child_bounds",
	} {
		if !strings.Contains(sql, col) {
			t.Errorf("pg_partitions_v1 must include column %q", col)
		}
	}
}

func TestPartitionsCollectorUsesPgPartitionedTable(t *testing.T) {
	q := pgqueries.ByID("pg_partitions_v1")
	if q == nil {
		t.Fatal("pg_partitions_v1 not registered")
	}
	if !containsCI(q.SQL, "pg_partitioned_table") {
		t.Error("pg_partitions_v1 must use pg_partitioned_table catalog")
	}
}

func TestPartitionsCollectorUsesPgGetPartkeydef(t *testing.T) {
	q := pgqueries.ByID("pg_partitions_v1")
	if q == nil {
		t.Fatal("pg_partitions_v1 not registered")
	}
	if !containsCI(q.SQL, "pg_get_partkeydef") {
		t.Error("pg_partitions_v1 must use pg_get_partkeydef() for partition key")
	}
}

func TestPartitionsCollectorUsesPgInherits(t *testing.T) {
	q := pgqueries.ByID("pg_partitions_v1")
	if q == nil {
		t.Fatal("pg_partitions_v1 not registered")
	}
	if !containsCI(q.SQL, "pg_inherits") {
		t.Error("pg_partitions_v1 must use pg_inherits for parent-child relationships")
	}
}

func TestPartitionsCollectorResultKind(t *testing.T) {
	q := pgqueries.ByID("pg_partitions_v1")
	if q == nil {
		t.Fatal("pg_partitions_v1 not registered")
	}
	if q.ResultKind != pgqueries.ResultRowset {
		t.Errorf("ResultKind: got %q, want rowset", q.ResultKind)
	}
}

func TestPartitionsCollectorIncludedOnPG14(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 14,
		Extensions:     []string{},
	})
	found := false
	for _, q := range filtered {
		if q.ID == "pg_partitions_v1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("pg_partitions_v1 must be included on PG 14")
	}
}

// ---------------------------------------------------------------------------
// Schema Metadata Collectors — Phase 2 Step 9: pg_triggers_v1
// ---------------------------------------------------------------------------

// --- pg_triggers_v1 inventory mode ---

func TestTriggersCollectorRegistered(t *testing.T) {
	q := pgqueries.ByID("pg_triggers_v1")
	if q == nil {
		t.Fatal("pg_triggers_v1 is not registered")
	}
	if q.Category != "schema" {
		t.Errorf("category: got %q, want %q", q.Category, "schema")
	}
}

func TestTriggersCollectorPassesLinter(t *testing.T) {
	q := pgqueries.ByID("pg_triggers_v1")
	if q == nil {
		t.Fatal("pg_triggers_v1 not registered")
	}
	if err := pgqueries.LintQuery(q.SQL); err != nil {
		t.Errorf("pg_triggers_v1 failed linter: %v", err)
	}
}

func TestTriggersCollectorCadence(t *testing.T) {
	q := pgqueries.ByID("pg_triggers_v1")
	if q == nil {
		t.Fatal("pg_triggers_v1 not registered")
	}
	if q.Cadence != pgqueries.CadenceDaily {
		t.Errorf("cadence: got %v, want CadenceDaily (24h)", q.Cadence)
	}
}

func TestTriggersCollectorExcludesSystemSchemas(t *testing.T) {
	q := pgqueries.ByID("pg_triggers_v1")
	if q == nil {
		t.Fatal("pg_triggers_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	for _, schema := range []string{"pg_catalog", "information_schema", "pg_toast", "pg_temp_"} {
		if !strings.Contains(sql, schema) {
			t.Errorf("pg_triggers_v1 must filter out %q", schema)
		}
	}
}

func TestTriggersCollectorHasOrderBy(t *testing.T) {
	q := pgqueries.ByID("pg_triggers_v1")
	if q == nil {
		t.Fatal("pg_triggers_v1 not registered")
	}
	if !containsCI(q.SQL, "ORDER BY") {
		t.Error("pg_triggers_v1 must have ORDER BY for deterministic output")
	}
}

func TestTriggersCollectorNoSelectStar(t *testing.T) {
	q := pgqueries.ByID("pg_triggers_v1")
	if q == nil {
		t.Fatal("pg_triggers_v1 not registered")
	}
	if strings.Contains(q.SQL, "SELECT *") || strings.Contains(q.SQL, "select *") {
		t.Error("pg_triggers_v1 must not use SELECT *")
	}
}

func TestTriggersCollectorInventoryColumns(t *testing.T) {
	q := pgqueries.ByID("pg_triggers_v1")
	if q == nil {
		t.Fatal("pg_triggers_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)

	for _, col := range []string{
		"schemaname", "relname", "tgname", "tgtype",
		"tg_funcschema", "tg_funcname", "tg_enabled",
	} {
		if !strings.Contains(sql, col) {
			t.Errorf("pg_triggers_v1 must include column %q", col)
		}
	}
}

func TestTriggersCollectorExcludesInternalTriggers(t *testing.T) {
	q := pgqueries.ByID("pg_triggers_v1")
	if q == nil {
		t.Fatal("pg_triggers_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	if !strings.Contains(sql, "tgisinternal") {
		t.Error("pg_triggers_v1 must exclude internal triggers (tgisinternal)")
	}
}

func TestTriggersCollectorEmitsTgtype(t *testing.T) {
	q := pgqueries.ByID("pg_triggers_v1")
	if q == nil {
		t.Fatal("pg_triggers_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	// Must emit tgtype as integer for analyzer-side decoding
	if !strings.Contains(sql, "tgtype") {
		t.Error("pg_triggers_v1 must emit tgtype bitmask")
	}
	if strings.Contains(sql, "pg_get_triggerdef") {
		t.Error("inventory mode must not use pg_get_triggerdef")
	}
}

func TestTriggersCollectorUsesPgTrigger(t *testing.T) {
	q := pgqueries.ByID("pg_triggers_v1")
	if q == nil {
		t.Fatal("pg_triggers_v1 not registered")
	}
	if !containsCI(q.SQL, "pg_trigger") {
		t.Error("pg_triggers_v1 must use pg_trigger catalog")
	}
}

func TestTriggersCollectorResultKind(t *testing.T) {
	q := pgqueries.ByID("pg_triggers_v1")
	if q == nil {
		t.Fatal("pg_triggers_v1 not registered")
	}
	if q.ResultKind != pgqueries.ResultRowset {
		t.Errorf("ResultKind: got %q, want rowset", q.ResultKind)
	}
}

func TestTriggersCollectorIncludedOnPG14(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 14,
		Extensions:     []string{},
	})
	found := false
	for _, q := range filtered {
		if q.ID == "pg_triggers_v1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("pg_triggers_v1 must be included on PG 14")
	}
}

// --- pg_triggers_definitions_v1 definition mode ---

func TestTriggersDefinitionsCollectorRegistered(t *testing.T) {
	q := pgqueries.ByID("pg_triggers_definitions_v1")
	if q == nil {
		t.Fatal("pg_triggers_definitions_v1 is not registered")
	}
	if q.Category != "schema" {
		t.Errorf("category: got %q, want %q", q.Category, "schema")
	}
}

func TestTriggersDefinitionsCollectorPassesLinter(t *testing.T) {
	q := pgqueries.ByID("pg_triggers_definitions_v1")
	if q == nil {
		t.Fatal("pg_triggers_definitions_v1 not registered")
	}
	if err := pgqueries.LintQuery(q.SQL); err != nil {
		t.Errorf("pg_triggers_definitions_v1 failed linter: %v", err)
	}
}

func TestTriggersDefinitionsCollectorIncludesTriggerdef(t *testing.T) {
	q := pgqueries.ByID("pg_triggers_definitions_v1")
	if q == nil {
		t.Fatal("pg_triggers_definitions_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)

	for _, col := range []string{"schemaname", "relname", "tgname", "triggerdef"} {
		if !strings.Contains(sql, col) {
			t.Errorf("pg_triggers_definitions_v1 must include column %q", col)
		}
	}
	if !strings.Contains(sql, "pg_get_triggerdef") {
		t.Error("pg_triggers_definitions_v1 must use pg_get_triggerdef()")
	}
}

// ---------------------------------------------------------------------------
// Schema Metadata Collectors — Phase 2 Step 10: pg_functions_v1
// ---------------------------------------------------------------------------

// --- pg_functions_v1 inventory mode ---

func TestFunctionsCollectorRegistered(t *testing.T) {
	q := pgqueries.ByID("pg_functions_v1")
	if q == nil {
		t.Fatal("pg_functions_v1 is not registered")
	}
	if q.Category != "schema" {
		t.Errorf("category: got %q, want %q", q.Category, "schema")
	}
}

func TestFunctionsCollectorPassesLinter(t *testing.T) {
	q := pgqueries.ByID("pg_functions_v1")
	if q == nil {
		t.Fatal("pg_functions_v1 not registered")
	}
	if err := pgqueries.LintQuery(q.SQL); err != nil {
		t.Errorf("pg_functions_v1 failed linter: %v", err)
	}
}

func TestFunctionsCollectorCadence(t *testing.T) {
	q := pgqueries.ByID("pg_functions_v1")
	if q == nil {
		t.Fatal("pg_functions_v1 not registered")
	}
	if q.Cadence != pgqueries.CadenceDaily {
		t.Errorf("cadence: got %v, want CadenceDaily (24h)", q.Cadence)
	}
}

func TestFunctionsCollectorExcludesSystemSchemas(t *testing.T) {
	q := pgqueries.ByID("pg_functions_v1")
	if q == nil {
		t.Fatal("pg_functions_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	for _, schema := range []string{"pg_catalog", "information_schema", "pg_toast", "pg_temp_"} {
		if !strings.Contains(sql, schema) {
			t.Errorf("pg_functions_v1 must filter out %q", schema)
		}
	}
}

func TestFunctionsCollectorHasOrderBy(t *testing.T) {
	q := pgqueries.ByID("pg_functions_v1")
	if q == nil {
		t.Fatal("pg_functions_v1 not registered")
	}
	if !containsCI(q.SQL, "ORDER BY") {
		t.Error("pg_functions_v1 must have ORDER BY for deterministic output")
	}
}

func TestFunctionsCollectorNoSelectStar(t *testing.T) {
	q := pgqueries.ByID("pg_functions_v1")
	if q == nil {
		t.Fatal("pg_functions_v1 not registered")
	}
	if strings.Contains(q.SQL, "SELECT *") || strings.Contains(q.SQL, "select *") {
		t.Error("pg_functions_v1 must not use SELECT *")
	}
}

func TestFunctionsCollectorInventoryColumns(t *testing.T) {
	q := pgqueries.ByID("pg_functions_v1")
	if q == nil {
		t.Fatal("pg_functions_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)

	for _, col := range []string{
		"schemaname", "proname", "identity_args", "return_type",
		"language", "volatility", "security_definer", "is_strict", "prokind",
	} {
		if !strings.Contains(sql, col) {
			t.Errorf("pg_functions_v1 must include column %q", col)
		}
	}
}

func TestFunctionsCollectorInventoryExcludesBody(t *testing.T) {
	q := pgqueries.ByID("pg_functions_v1")
	if q == nil {
		t.Fatal("pg_functions_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	if strings.Contains(sql, "prosrc") {
		t.Error("inventory mode must not include prosrc (function body)")
	}
}

func TestFunctionsCollectorUsesPgProc(t *testing.T) {
	q := pgqueries.ByID("pg_functions_v1")
	if q == nil {
		t.Fatal("pg_functions_v1 not registered")
	}
	if !containsCI(q.SQL, "pg_proc") {
		t.Error("pg_functions_v1 must use pg_proc catalog")
	}
}

func TestFunctionsCollectorMinPGVersion(t *testing.T) {
	q := pgqueries.ByID("pg_functions_v1")
	if q == nil {
		t.Fatal("pg_functions_v1 not registered")
	}
	if q.MinPGVersion != 11 {
		t.Errorf("MinPGVersion: got %d, want 11 (prokind requires PG 11+)", q.MinPGVersion)
	}
}

func TestFunctionsCollectorExcludedOnPG10(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 10,
		Extensions:     []string{},
	})
	for _, q := range filtered {
		if q.ID == "pg_functions_v1" {
			t.Error("pg_functions_v1 must be excluded on PG 10")
		}
	}
}

func TestFunctionsCollectorIncludedOnPG14(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 14,
		Extensions:     []string{},
	})
	found := false
	for _, q := range filtered {
		if q.ID == "pg_functions_v1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("pg_functions_v1 must be included on PG 14")
	}
}

func TestFunctionsCollectorResultKind(t *testing.T) {
	q := pgqueries.ByID("pg_functions_v1")
	if q == nil {
		t.Fatal("pg_functions_v1 not registered")
	}
	if q.ResultKind != pgqueries.ResultRowset {
		t.Errorf("ResultKind: got %q, want rowset", q.ResultKind)
	}
}

// --- pg_functions_definitions_v1 definition mode ---

func TestFunctionsDefinitionsCollectorRegistered(t *testing.T) {
	q := pgqueries.ByID("pg_functions_definitions_v1")
	if q == nil {
		t.Fatal("pg_functions_definitions_v1 is not registered")
	}
	if q.Category != "schema" {
		t.Errorf("category: got %q, want %q", q.Category, "schema")
	}
}

func TestFunctionsDefinitionsCollectorPassesLinter(t *testing.T) {
	q := pgqueries.ByID("pg_functions_definitions_v1")
	if q == nil {
		t.Fatal("pg_functions_definitions_v1 not registered")
	}
	if err := pgqueries.LintQuery(q.SQL); err != nil {
		t.Errorf("pg_functions_definitions_v1 failed linter: %v", err)
	}
}

func TestFunctionsDefinitionsCollectorIncludesBody(t *testing.T) {
	q := pgqueries.ByID("pg_functions_definitions_v1")
	if q == nil {
		t.Fatal("pg_functions_definitions_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)

	for _, col := range []string{"schemaname", "proname", "prokind", "body"} {
		if !strings.Contains(sql, col) {
			t.Errorf("pg_functions_definitions_v1 must include column %q", col)
		}
	}
	if !strings.Contains(sql, "prosrc") {
		t.Error("pg_functions_definitions_v1 must select prosrc as body")
	}
}

func TestFunctionsDefinitionsCollectorMinPGVersion(t *testing.T) {
	q := pgqueries.ByID("pg_functions_definitions_v1")
	if q == nil {
		t.Fatal("pg_functions_definitions_v1 not registered")
	}
	if q.MinPGVersion != 11 {
		t.Errorf("MinPGVersion: got %d, want 11", q.MinPGVersion)
	}
}

// ---------------------------------------------------------------------------
// Schema Metadata Collectors — Phase 2 Step 11: pg_sequences_v1
// ---------------------------------------------------------------------------

func TestSequencesCollectorRegistered(t *testing.T) {
	q := pgqueries.ByID("pg_sequences_v1")
	if q == nil {
		t.Fatal("pg_sequences_v1 is not registered")
	}
	if q.Category != "schema" {
		t.Errorf("category: got %q, want %q", q.Category, "schema")
	}
}

func TestSequencesCollectorPassesLinter(t *testing.T) {
	q := pgqueries.ByID("pg_sequences_v1")
	if q == nil {
		t.Fatal("pg_sequences_v1 not registered")
	}
	if err := pgqueries.LintQuery(q.SQL); err != nil {
		t.Errorf("pg_sequences_v1 failed linter: %v", err)
	}
}

func TestSequencesCollectorCadence(t *testing.T) {
	q := pgqueries.ByID("pg_sequences_v1")
	if q == nil {
		t.Fatal("pg_sequences_v1 not registered")
	}
	if q.Cadence != pgqueries.CadenceDaily {
		t.Errorf("cadence: got %v, want CadenceDaily (24h)", q.Cadence)
	}
}

func TestSequencesCollectorExcludesSystemSchemas(t *testing.T) {
	q := pgqueries.ByID("pg_sequences_v1")
	if q == nil {
		t.Fatal("pg_sequences_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	for _, schema := range []string{"pg_catalog", "information_schema", "pg_toast", "pg_temp_"} {
		if !strings.Contains(sql, schema) {
			t.Errorf("pg_sequences_v1 must filter out %q", schema)
		}
	}
}

func TestSequencesCollectorHasOrderBy(t *testing.T) {
	q := pgqueries.ByID("pg_sequences_v1")
	if q == nil {
		t.Fatal("pg_sequences_v1 not registered")
	}
	if !containsCI(q.SQL, "ORDER BY") {
		t.Error("pg_sequences_v1 must have ORDER BY for deterministic output")
	}
}

func TestSequencesCollectorNoSelectStar(t *testing.T) {
	q := pgqueries.ByID("pg_sequences_v1")
	if q == nil {
		t.Fatal("pg_sequences_v1 not registered")
	}
	if strings.Contains(q.SQL, "SELECT *") || strings.Contains(q.SQL, "select *") {
		t.Error("pg_sequences_v1 must not use SELECT *")
	}
}

func TestSequencesCollectorOutputColumns(t *testing.T) {
	q := pgqueries.ByID("pg_sequences_v1")
	if q == nil {
		t.Fatal("pg_sequences_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)

	for _, col := range []string{
		"schemaname", "sequencename", "data_type", "start_value",
		"min_value", "max_value", "increment_by", "cycle", "last_value",
	} {
		if !strings.Contains(sql, col) {
			t.Errorf("pg_sequences_v1 must include column %q", col)
		}
	}
}

func TestSequencesCollectorUsesPgSequences(t *testing.T) {
	q := pgqueries.ByID("pg_sequences_v1")
	if q == nil {
		t.Fatal("pg_sequences_v1 not registered")
	}
	if !containsCI(q.SQL, "pg_sequences") {
		t.Error("pg_sequences_v1 must use pg_sequences view")
	}
}

func TestSequencesCollectorResultKind(t *testing.T) {
	q := pgqueries.ByID("pg_sequences_v1")
	if q == nil {
		t.Fatal("pg_sequences_v1 not registered")
	}
	if q.ResultKind != pgqueries.ResultRowset {
		t.Errorf("ResultKind: got %q, want rowset", q.ResultKind)
	}
}

// ---------------------------------------------------------------------------
// Schema Metadata Collectors — pg_statistic_ext_v1 (Arq#393 / Signals#130)
// ---------------------------------------------------------------------------

func TestStatisticExtCollectorRegistered(t *testing.T) {
	q := pgqueries.ByID("pg_statistic_ext_v1")
	if q == nil {
		t.Fatal("pg_statistic_ext_v1 is not registered")
	}
	if q.Category != "schema" {
		t.Errorf("category: got %q, want %q", q.Category, "schema")
	}
}

func TestStatisticExtCollectorPassesLinter(t *testing.T) {
	q := pgqueries.ByID("pg_statistic_ext_v1")
	if q == nil {
		t.Fatal("pg_statistic_ext_v1 not registered")
	}
	if err := pgqueries.LintQuery(q.SQL); err != nil {
		t.Errorf("pg_statistic_ext_v1 failed linter: %v", err)
	}
}

func TestStatisticExtCollectorExcludesSystemSchemas(t *testing.T) {
	q := pgqueries.ByID("pg_statistic_ext_v1")
	if q == nil {
		t.Fatal("pg_statistic_ext_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	for _, schema := range []string{"pg_catalog", "information_schema", "pg_toast", "pg_temp_"} {
		if !strings.Contains(sql, schema) {
			t.Errorf("pg_statistic_ext_v1 must filter out %q", schema)
		}
	}
}

func TestStatisticExtCollectorHasOrderBy(t *testing.T) {
	q := pgqueries.ByID("pg_statistic_ext_v1")
	if q == nil {
		t.Fatal("pg_statistic_ext_v1 not registered")
	}
	if !containsCI(q.SQL, "ORDER BY") {
		t.Error("pg_statistic_ext_v1 must have ORDER BY for deterministic output")
	}
}

func TestStatisticExtCollectorNoSelectStar(t *testing.T) {
	q := pgqueries.ByID("pg_statistic_ext_v1")
	if q == nil {
		t.Fatal("pg_statistic_ext_v1 not registered")
	}
	if strings.Contains(q.SQL, "SELECT *") || strings.Contains(q.SQL, "select *") {
		t.Error("pg_statistic_ext_v1 must not use SELECT *")
	}
}

func TestStatisticExtCollectorOutputColumns(t *testing.T) {
	q := pgqueries.ByID("pg_statistic_ext_v1")
	if q == nil {
		t.Fatal("pg_statistic_ext_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	for _, col := range []string{
		"stat_schema", "stat_name",
		"table_schema", "table_name",
		"attnums", "kinds",
	} {
		if !strings.Contains(sql, col) {
			t.Errorf("pg_statistic_ext_v1 must include column %q", col)
		}
	}
}

// pg_statistic_ext_data is NOT read — owner-only visibility post-PG12
// requires permissions beyond pg_monitor and would defeat the
// no-superuser safety promise. The catalog-metadata-only query stays
// within pg_monitor's reach.
func TestStatisticExtCollectorDoesNotReadStatsExtData(t *testing.T) {
	q := pgqueries.ByID("pg_statistic_ext_v1")
	if q == nil {
		t.Fatal("pg_statistic_ext_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	if strings.Contains(sql, "pg_statistic_ext_data") {
		t.Error("pg_statistic_ext_v1 must NOT read pg_statistic_ext_data (requires owner / superuser)")
	}
}

func TestSequencesCollectorIncludedOnPG14(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 14,
		Extensions:     []string{},
	})
	found := false
	for _, q := range filtered {
		if q.ID == "pg_sequences_v1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("pg_sequences_v1 must be included on PG 14")
	}
}

// --- Final catalog count ---

func TestSchemaPhase2CompleteCatalogCount(t *testing.T) {
	all := pgqueries.All()
	// 29 baseline + 15 schema collectors = 44 minimum
	// (+ pg_statistic_ext_v1 from Signals#130 keeps us above
	// the floor; the floor is intentionally a >= check so adding
	// collectors doesn't churn the test.)
	if len(all) < 44 {
		t.Errorf("catalog has %d collectors, want at least 44", len(all))
	}
}

// --- Version filtering ---

func TestSchemaPhase1IncludedOnPG14(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 14,
		Extensions:     []string{},
	})
	idSet := make(map[string]bool)
	for _, q := range filtered {
		idSet[q.ID] = true
	}
	for _, tc := range schemaPhase1 {
		if !idSet[tc.id] {
			t.Errorf("collector %q must be included on PG 14", tc.id)
		}
	}
}

// ---------------------------------------------------------------------------
// Schema Metadata Collectors — pg_statistic_ext_data_v1 / _mcv_v1 (#171)
// ---------------------------------------------------------------------------
//
// AT-01 through AT-05 derive from
// specifications/collectors/pg_statistic_ext_data_v1.md. The
// privilege-degraded path (AT-02) and HS-gated MCV opt-in
// (AT-03 / AT-04) are integration concerns that need a real PG
// fixture; tests here pin the structural invariants (collector
// registered, SQL shape, redaction-by-default for kind=m via
// the two-collector split, HS posture on the MCV sibling).

func TestStatisticExtDataCollectorRegistered(t *testing.T) {
	q := pgqueries.ByID("pg_statistic_ext_data_v1")
	if q == nil {
		t.Fatal("pg_statistic_ext_data_v1 is not registered")
	}
	if q.Category != "schema" {
		t.Errorf("category: got %q, want %q", q.Category, "schema")
	}
	if q.MinPGVersion != 14 {
		t.Errorf("MinPGVersion: got %d, want 14", q.MinPGVersion)
	}
	// AT-04 / INV-04: the base collector MUST stay non-HS so it
	// emits on every snapshot for the {d, f, e} kinds (no PII).
	if q.HighSensitivity {
		t.Error("pg_statistic_ext_data_v1 must NOT be HighSensitivity-gated; the {d,f,e} kinds carry no sampled values")
	}
}

func TestStatisticExtDataMCVCollectorRegistered(t *testing.T) {
	q := pgqueries.ByID("pg_statistic_ext_data_mcv_v1")
	if q == nil {
		t.Fatal("pg_statistic_ext_data_mcv_v1 is not registered")
	}
	if q.Category != "schema" {
		t.Errorf("category: got %q, want %q", q.Category, "schema")
	}
	if q.MinPGVersion != 14 {
		t.Errorf("MinPGVersion: got %d, want 14", q.MinPGVersion)
	}
	// AT-03 / INV-04: MCV blob is the only kind that may contain
	// PII; collector MUST be HS-gated so the daemon-wide
	// HighSensitivityEnabled flag must be on for emission.
	if !q.HighSensitivity {
		t.Error("pg_statistic_ext_data_mcv_v1 MUST be HighSensitivity-gated (carries sampled column values)")
	}
}

func TestStatisticExtDataCollectorPassesLinter(t *testing.T) {
	for _, id := range []string{
		"pg_statistic_ext_data_v1",
		"pg_statistic_ext_data_mcv_v1",
	} {
		q := pgqueries.ByID(id)
		if q == nil {
			t.Fatalf("%s not registered", id)
			continue
		}
		if err := pgqueries.LintQuery(q.SQL); err != nil {
			t.Errorf("%s failed linter: %v", id, err)
		}
	}
}

func TestStatisticExtDataCollectorExcludesSystemSchemas(t *testing.T) {
	for _, id := range []string{
		"pg_statistic_ext_data_v1",
		"pg_statistic_ext_data_mcv_v1",
	} {
		q := pgqueries.ByID(id)
		if q == nil {
			t.Fatalf("%s not registered", id)
			continue
		}
		sql := strings.ToLower(q.SQL)
		for _, schema := range []string{"pg_catalog", "information_schema", "pg_toast", "pg_temp_"} {
			if !strings.Contains(sql, schema) {
				t.Errorf("%s must filter out %q", id, schema)
			}
		}
	}
}

func TestStatisticExtDataCollectorHasOrderBy(t *testing.T) {
	for _, id := range []string{
		"pg_statistic_ext_data_v1",
		"pg_statistic_ext_data_mcv_v1",
	} {
		q := pgqueries.ByID(id)
		if q == nil {
			t.Fatalf("%s not registered", id)
			continue
		}
		if !containsCI(q.SQL, "ORDER BY") {
			t.Errorf("%s must have ORDER BY for deterministic output", id)
		}
	}
}

func TestStatisticExtDataCollectorNoSelectStar(t *testing.T) {
	for _, id := range []string{
		"pg_statistic_ext_data_v1",
		"pg_statistic_ext_data_mcv_v1",
	} {
		q := pgqueries.ByID(id)
		if q == nil {
			t.Fatalf("%s not registered", id)
			continue
		}
		if strings.Contains(q.SQL, "SELECT *") || strings.Contains(q.SQL, "select *") {
			t.Errorf("%s must not use SELECT *", id)
		}
	}
}

func TestStatisticExtDataCollectorOutputColumns(t *testing.T) {
	q := pgqueries.ByID("pg_statistic_ext_data_v1")
	if q == nil {
		t.Fatal("pg_statistic_ext_data_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	for _, col := range []string{
		"stat_schema", "stat_name",
		"table_schema", "table_name",
		"kind", "kind_data", "available",
	} {
		if !strings.Contains(sql, col) {
			t.Errorf("pg_statistic_ext_data_v1 SQL must project %q", col)
		}
	}
}

func TestStatisticExtDataMCVCollectorOutputColumns(t *testing.T) {
	q := pgqueries.ByID("pg_statistic_ext_data_mcv_v1")
	if q == nil {
		t.Fatal("pg_statistic_ext_data_mcv_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	for _, col := range []string{
		"stat_schema", "stat_name",
		"table_schema", "table_name",
		"kind", "kind_data", "available",
	} {
		if !strings.Contains(sql, col) {
			t.Errorf("pg_statistic_ext_data_mcv_v1 SQL must project %q", col)
		}
	}
}

// AT-02 (privilege-degraded path): the SQL MUST use a LEFT JOIN
// to pg_statistic_ext_data so owner-only refusals surface as
// NULL data columns rather than dropped rows.
func TestStatisticExtDataCollectorUsesLeftJoinForDegradedPath(t *testing.T) {
	for _, id := range []string{
		"pg_statistic_ext_data_v1",
		"pg_statistic_ext_data_mcv_v1",
	} {
		q := pgqueries.ByID(id)
		if q == nil {
			t.Fatalf("%s not registered", id)
			continue
		}
		if !containsCI(q.SQL, "LEFT JOIN pg_statistic_ext_data") {
			t.Errorf("%s must use LEFT JOIN against pg_statistic_ext_data so owner-only refusals surface as availability rows, not dropped rows",
				id)
		}
	}
}

// AT-03 / INV-04: the base collector MUST NOT emit the 'm' MCV
// kind. The MCV blob is owned exclusively by the HS-gated
// _mcv_v1 sibling.
func TestStatisticExtDataCollectorExcludesMCVKind(t *testing.T) {
	q := pgqueries.ByID("pg_statistic_ext_data_v1")
	if q == nil {
		t.Fatal("pg_statistic_ext_data_v1 not registered")
	}
	sql := q.SQL
	// The base collector's kind filter must list d/f/e and
	// exclude m. We check structurally — the filter line uses
	// an IN-list. A future refactor that broadens the filter
	// must also remove this assertion.
	if !containsCI(sql, "k.kind IN ('d', 'f', 'e')") {
		t.Error("pg_statistic_ext_data_v1 kind filter MUST be `k.kind IN ('d', 'f', 'e')` — the 'm' (MCV) kind belongs to the HS-gated _mcv_v1 sibling (INV-04)")
	}
}

// AT-03 / INV-04 (companion): the MCV sibling MUST ONLY emit
// kind='m'.
func TestStatisticExtDataMCVCollectorEmitsOnlyMCV(t *testing.T) {
	q := pgqueries.ByID("pg_statistic_ext_data_mcv_v1")
	if q == nil {
		t.Fatal("pg_statistic_ext_data_mcv_v1 not registered")
	}
	if !containsCI(q.SQL, "'m' = ANY(es.stxkind)") {
		t.Error("pg_statistic_ext_data_mcv_v1 must filter `'m' = ANY(es.stxkind)` so only objects declaring the MCV kind contribute rows")
	}
	if !containsCI(q.SQL, "stxdmcv") {
		t.Error("pg_statistic_ext_data_mcv_v1 must project the stxdmcv blob")
	}
}

// ---------------------------------------------------------------------------
// pg_identity_columns_v1 — Signals#202 / Arq#652 dependency
// ---------------------------------------------------------------------------

func TestIdentityColumnsCollectorRegistered(t *testing.T) {
	q := pgqueries.ByID("pg_identity_columns_v1")
	if q == nil {
		t.Fatal("pg_identity_columns_v1 is not registered")
	}
	if q.Category != "schema" {
		t.Errorf("category: got %q, want %q", q.Category, "schema")
	}
}

func TestIdentityColumnsCollectorPassesLinter(t *testing.T) {
	q := pgqueries.ByID("pg_identity_columns_v1")
	if q == nil {
		t.Fatal("pg_identity_columns_v1 not registered")
	}
	if err := pgqueries.LintQuery(q.SQL); err != nil {
		t.Errorf("pg_identity_columns_v1 failed linter: %v", err)
	}
}

func TestIdentityColumnsCollectorCadence(t *testing.T) {
	q := pgqueries.ByID("pg_identity_columns_v1")
	if q == nil {
		t.Fatal("pg_identity_columns_v1 not registered")
	}
	if q.Cadence != pgqueries.CadenceDaily {
		t.Errorf("cadence: got %v, want CadenceDaily (24h)", q.Cadence)
	}
}

func TestIdentityColumnsCollectorMinPGVersion(t *testing.T) {
	q := pgqueries.ByID("pg_identity_columns_v1")
	if q == nil {
		t.Fatal("pg_identity_columns_v1 not registered")
	}
	if q.MinPGVersion != 10 {
		t.Errorf("MinPGVersion: got %d, want 10 (IDENTITY columns landed in PG10)", q.MinPGVersion)
	}
}

func TestIdentityColumnsCollectorExcludesSystemSchemas(t *testing.T) {
	q := pgqueries.ByID("pg_identity_columns_v1")
	if q == nil {
		t.Fatal("pg_identity_columns_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	for _, schema := range []string{"pg_catalog", "information_schema", "pg_toast", "pg_temp_"} {
		if !strings.Contains(sql, schema) {
			t.Errorf("pg_identity_columns_v1 must filter out %q", schema)
		}
	}
}

func TestIdentityColumnsCollectorHasOrderBy(t *testing.T) {
	q := pgqueries.ByID("pg_identity_columns_v1")
	if q == nil {
		t.Fatal("pg_identity_columns_v1 not registered")
	}
	if !containsCI(q.SQL, "ORDER BY") {
		t.Error("pg_identity_columns_v1 must have ORDER BY for deterministic output")
	}
}

func TestIdentityColumnsCollectorNoSelectStar(t *testing.T) {
	q := pgqueries.ByID("pg_identity_columns_v1")
	if q == nil {
		t.Fatal("pg_identity_columns_v1 not registered")
	}
	if strings.Contains(q.SQL, "SELECT *") || strings.Contains(q.SQL, "select *") {
		t.Error("pg_identity_columns_v1 must not use SELECT *")
	}
}

func TestIdentityColumnsCollectorOutputColumns(t *testing.T) {
	q := pgqueries.ByID("pg_identity_columns_v1")
	if q == nil {
		t.Fatal("pg_identity_columns_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	required := []string{
		"schemaname", "relname", "attname", "atttypname",
		"attidentity", "atthasdef", "default_is_nextval",
		"sequence_schema", "sequence_name", "auto_owned_sequence",
		"is_primary_key", "is_unique",
	}
	for _, col := range required {
		if !strings.Contains(sql, col) {
			t.Errorf("pg_identity_columns_v1 must include column %q", col)
		}
	}
}

func TestIdentityColumnsCollectorExcludesDefaultText(t *testing.T) {
	q := pgqueries.ByID("pg_identity_columns_v1")
	if q == nil {
		t.Fatal("pg_identity_columns_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	// default_is_nextval must be derived from the pg_depend
	// auto-ownership link, not from parsing the default expression.
	// pg_get_expr risks exposing literal default values.
	if strings.Contains(sql, "pg_get_expr") {
		t.Error("pg_identity_columns_v1 must NOT use pg_get_expr (default expression may contain sensitive values; derive default_is_nextval from pg_depend instead)")
	}
	if strings.Contains(sql, "column_default") {
		t.Error("pg_identity_columns_v1 must NOT use information_schema.column_default (same exposure concern)")
	}
}

func TestIdentityColumnsCollectorUsesAutoOwnershipDepend(t *testing.T) {
	q := pgqueries.ByID("pg_identity_columns_v1")
	if q == nil {
		t.Fatal("pg_identity_columns_v1 not registered")
	}
	if !containsCI(q.SQL, "pg_depend") {
		t.Error("pg_identity_columns_v1 must join pg_depend to detect the SERIAL auto-ownership link")
	}
	if !containsCI(q.SQL, "deptype = 'a'") {
		t.Error("pg_identity_columns_v1 must filter pg_depend on deptype='a' (auto-ownership) — other deptypes would generate false positives")
	}
}

func TestIdentityColumnsCollectorUsesAttidentity(t *testing.T) {
	q := pgqueries.ByID("pg_identity_columns_v1")
	if q == nil {
		t.Fatal("pg_identity_columns_v1 not registered")
	}
	if !containsCI(q.SQL, "a.attidentity") {
		t.Error("pg_identity_columns_v1 must project pg_attribute.attidentity to distinguish IDENTITY from SERIAL")
	}
}

func TestIdentityColumnsCollectorRelkindFilter(t *testing.T) {
	q := pgqueries.ByID("pg_identity_columns_v1")
	if q == nil {
		t.Fatal("pg_identity_columns_v1 not registered")
	}
	// Restrict to ordinary tables ('r') and partitioned tables ('p').
	// Views, materialized views, and foreign tables cannot carry
	// IDENTITY columns.
	if !containsCI(q.SQL, "relkind IN ('r', 'p')") {
		t.Error("pg_identity_columns_v1 must restrict relkind to ('r', 'p') — IDENTITY is meaningless on views/matviews/foreign tables")
	}
}

func TestIdentityColumnsCollectorPKUniqueViaConkey(t *testing.T) {
	q := pgqueries.ByID("pg_identity_columns_v1")
	if q == nil {
		t.Fatal("pg_identity_columns_v1 not registered")
	}
	if !containsCI(q.SQL, "pg_constraint") {
		t.Error("pg_identity_columns_v1 must join pg_constraint for primary-key and unique-constraint membership")
	}
	if !containsCI(q.SQL, "unnest(conkey)") {
		t.Error("pg_identity_columns_v1 must use unnest(conkey) to map composite-constraint columns to per-column rows")
	}
	if !containsCI(q.SQL, "contype = 'p'") {
		t.Error("pg_identity_columns_v1 must look for contype='p' (primary key)")
	}
	if !containsCI(q.SQL, "contype = 'u'") {
		t.Error("pg_identity_columns_v1 must look for contype='u' (unique)")
	}
}

func TestIdentityColumnsCollectorRowEmissionFilter(t *testing.T) {
	q := pgqueries.ByID("pg_identity_columns_v1")
	if q == nil {
		t.Fatal("pg_identity_columns_v1 not registered")
	}
	// Only emit rows for columns that are candidates for IDENTITY/SERIAL
	// reasoning: identity columns, columns with an auto-owned sequence,
	// or numeric/uuid columns that could be surrogate PKs. Wide text/jsonb
	// columns would inflate the result set without analyzer value.
	sql := strings.ToLower(q.SQL)
	if !strings.Contains(sql, "attidentity <> ''") {
		t.Error("pg_identity_columns_v1 row-emission filter must include `attidentity <> ''`")
	}
	for _, typ := range []string{"'int2'", "'int4'", "'int8'", "'uuid'"} {
		if !strings.Contains(sql, typ) {
			t.Errorf("pg_identity_columns_v1 row-emission filter must include type %s", typ)
		}
	}
}
