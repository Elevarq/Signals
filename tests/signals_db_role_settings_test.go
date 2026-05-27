package tests

import (
	"strings"
	"testing"

	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// ---------------------------------------------------------------------------
// pg_db_role_settings_v1 — database, role, and role-in-database GUC defaults
// from pg_db_role_setting.
//
// Specification: specifications/collectors/pg_db_role_settings_v1.md
// Acceptance:    specifications/collectors/pg_db_role_settings_v1.acceptance.md
// ---------------------------------------------------------------------------

func TestDBRoleSettingsCollectorRegistered(t *testing.T) {
	q := pgqueries.ByID("pg_db_role_settings_v1")
	if q == nil {
		t.Fatal("pg_db_role_settings_v1 is not registered")
	}
	if q.Category != "server" {
		t.Errorf("category: got %q, want %q", q.Category, "server")
	}
}

func TestDBRoleSettingsCollectorPassesLinter(t *testing.T) {
	q := pgqueries.ByID("pg_db_role_settings_v1")
	if q == nil {
		t.Fatal("pg_db_role_settings_v1 not registered")
	}
	if err := pgqueries.LintQuery(q.SQL); err != nil {
		t.Errorf("pg_db_role_settings_v1 failed linter: %v", err)
	}
}

func TestDBRoleSettingsCollectorCadence(t *testing.T) {
	q := pgqueries.ByID("pg_db_role_settings_v1")
	if q == nil {
		t.Fatal("pg_db_role_settings_v1 not registered")
	}
	if q.Cadence != pgqueries.CadenceDaily {
		t.Errorf("cadence: got %v, want CadenceDaily (24h)", q.Cadence)
	}
}

func TestDBRoleSettingsCollectorRetention(t *testing.T) {
	q := pgqueries.ByID("pg_db_role_settings_v1")
	if q == nil {
		t.Fatal("pg_db_role_settings_v1 not registered")
	}
	if q.RetentionClass != pgqueries.RetentionMedium {
		t.Errorf("retention: got %q, want RetentionMedium", q.RetentionClass)
	}
}

func TestDBRoleSettingsCollectorResultKind(t *testing.T) {
	q := pgqueries.ByID("pg_db_role_settings_v1")
	if q == nil {
		t.Fatal("pg_db_role_settings_v1 not registered")
	}
	if q.ResultKind != pgqueries.ResultRowset {
		t.Errorf("ResultKind: got %q, want rowset", q.ResultKind)
	}
}

func TestDBRoleSettingsCollectorIncludedOnPG14(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 14,
		Extensions:     []string{},
	})
	found := false
	for _, q := range filtered {
		if q.ID == "pg_db_role_settings_v1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("pg_db_role_settings_v1 must be included on PG 14")
	}
}

func TestDBRoleSettingsCollectorHasOrderBy(t *testing.T) {
	q := pgqueries.ByID("pg_db_role_settings_v1")
	if q == nil {
		t.Fatal("pg_db_role_settings_v1 not registered")
	}
	if !containsCI(q.SQL, "ORDER BY") {
		t.Error("pg_db_role_settings_v1 must have ORDER BY for deterministic output")
	}
}

func TestDBRoleSettingsCollectorNoSelectStar(t *testing.T) {
	q := pgqueries.ByID("pg_db_role_settings_v1")
	if q == nil {
		t.Fatal("pg_db_role_settings_v1 not registered")
	}
	if strings.Contains(q.SQL, "SELECT *") || strings.Contains(q.SQL, "select *") {
		t.Error("pg_db_role_settings_v1 must not use SELECT *")
	}
}

func TestDBRoleSettingsCollectorOutputColumns(t *testing.T) {
	q := pgqueries.ByID("pg_db_role_settings_v1")
	if q == nil {
		t.Fatal("pg_db_role_settings_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	required := []string{
		"database_oid", "database_name", "role_oid", "role_name",
		"setting_scope", "setconfig",
	}
	for _, col := range required {
		if !strings.Contains(sql, col) {
			t.Errorf("pg_db_role_settings_v1 must include column %q", col)
		}
	}
}

func TestDBRoleSettingsCollectorUsesRequiredCatalogs(t *testing.T) {
	q := pgqueries.ByID("pg_db_role_settings_v1")
	if q == nil {
		t.Fatal("pg_db_role_settings_v1 not registered")
	}
	for _, catalog := range []string{"pg_db_role_setting", "pg_database", "pg_roles"} {
		if !containsCI(q.SQL, catalog) {
			t.Errorf("pg_db_role_settings_v1 must query %s", catalog)
		}
	}
}

func TestDBRoleSettingsCollectorPreservesZeroOidScopes(t *testing.T) {
	q := pgqueries.ByID("pg_db_role_settings_v1")
	if q == nil {
		t.Fatal("pg_db_role_settings_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	for _, marker := range []string{"setdatabase = 0", "setrole = 0"} {
		if !strings.Contains(sql, marker) {
			t.Errorf("pg_db_role_settings_v1 must explicitly classify %q scope markers", marker)
		}
	}
}

func TestDBRoleSettingsCollectorClassifiesScopes(t *testing.T) {
	q := pgqueries.ByID("pg_db_role_settings_v1")
	if q == nil {
		t.Fatal("pg_db_role_settings_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	for _, scope := range []string{"database", "role", "role_in_database", "global"} {
		if !strings.Contains(sql, "'"+scope+"'") {
			t.Errorf("pg_db_role_settings_v1 must classify scope %q", scope)
		}
	}
}

func TestDBRoleSettingsCollectorDoesNotUsePgSettingsAsSource(t *testing.T) {
	q := pgqueries.ByID("pg_db_role_settings_v1")
	if q == nil {
		t.Fatal("pg_db_role_settings_v1 not registered")
	}
	if containsCI(q.SQL, "pg_settings") {
		t.Error("pg_db_role_settings_v1 must read pg_db_role_setting, not pg_settings")
	}
}
