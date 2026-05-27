package tests

import (
	"strings"
	"testing"

	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// ---------------------------------------------------------------------------
// pg_role_capabilities_v1 — single-row capability matrix for the monitoring
// role. Tells the analyzer what the connected user can actually see so
// coverage notes distinguish "insufficient_privilege" from
// "extension_not_installed" from "collector_empty".
//
// Specification: specifications/collectors/pg_role_capabilities_v1.md
// ---------------------------------------------------------------------------

func TestRoleCapabilitiesCollectorRegistered(t *testing.T) {
	q := pgqueries.ByID("pg_role_capabilities_v1")
	if q == nil {
		t.Fatal("pg_role_capabilities_v1 is not registered")
	}
	if q.Category != "security" {
		t.Errorf("category: got %q, want %q", q.Category, "security")
	}
}

func TestRoleCapabilitiesCollectorPassesLinter(t *testing.T) {
	q := pgqueries.ByID("pg_role_capabilities_v1")
	if q == nil {
		t.Fatal("pg_role_capabilities_v1 not registered")
	}
	if err := pgqueries.LintQuery(q.SQL); err != nil {
		t.Errorf("pg_role_capabilities_v1 failed linter: %v", err)
	}
}

func TestRoleCapabilitiesCollectorCadence(t *testing.T) {
	q := pgqueries.ByID("pg_role_capabilities_v1")
	if q == nil {
		t.Fatal("pg_role_capabilities_v1 not registered")
	}
	if q.Cadence != pgqueries.CadenceDaily {
		t.Errorf("cadence: got %v, want CadenceDaily (24h)", q.Cadence)
	}
}

func TestRoleCapabilitiesCollectorRetention(t *testing.T) {
	q := pgqueries.ByID("pg_role_capabilities_v1")
	if q == nil {
		t.Fatal("pg_role_capabilities_v1 not registered")
	}
	if q.RetentionClass != pgqueries.RetentionMedium {
		t.Errorf("retention: got %q, want RetentionMedium", q.RetentionClass)
	}
}

func TestRoleCapabilitiesCollectorResultKind(t *testing.T) {
	q := pgqueries.ByID("pg_role_capabilities_v1")
	if q == nil {
		t.Fatal("pg_role_capabilities_v1 not registered")
	}
	if q.ResultKind != pgqueries.ResultScalar {
		t.Errorf("ResultKind: got %q, want scalar (single row)", q.ResultKind)
	}
}

func TestRoleCapabilitiesCollectorIncludedOnPG14(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 14,
		Extensions:     []string{},
	})
	found := false
	for _, q := range filtered {
		if q.ID == "pg_role_capabilities_v1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("pg_role_capabilities_v1 must be included on PG 14")
	}
}

func TestRoleCapabilitiesCollectorOutputColumns(t *testing.T) {
	q := pgqueries.ByID("pg_role_capabilities_v1")
	if q == nil {
		t.Fatal("pg_role_capabilities_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	required := []string{
		"session_user", "current_user",
		"is_superuser", "is_pg_monitor",
		"is_pg_read_all_stats", "is_pg_read_all_settings",
		"is_pg_read_server_files", "is_pg_signal_backend",
		"can_read_all_stats", "can_read_all_settings",
		"default_transaction_read_only", "statement_timeout",
		"role_attrs",
	}
	for _, col := range required {
		if !strings.Contains(sql, col) {
			t.Errorf("pg_role_capabilities_v1 must include column %q", col)
		}
	}
}

// Role-existence check must wrap pg_has_role so missing built-in roles on
// older PG point releases (e.g. pg_read_all_settings on PG 10.4) do not
// raise.
func TestRoleCapabilitiesCollectorGuardsAgainstMissingRoles(t *testing.T) {
	q := pgqueries.ByID("pg_role_capabilities_v1")
	if q == nil {
		t.Fatal("pg_role_capabilities_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	if !strings.Contains(sql, "pg_has_role") {
		t.Error("pg_role_capabilities_v1 must use pg_has_role")
	}
	// Presence of pg_roles in the query body (other than as the FROM target
	// row) signals the existence-guard pattern.
	if strings.Count(sql, "pg_roles") < 2 {
		t.Error("pg_role_capabilities_v1 must guard pg_has_role with pg_roles existence checks for older PG point releases")
	}
}

func TestRoleCapabilitiesCollectorEmitsRoleAttrs(t *testing.T) {
	q := pgqueries.ByID("pg_role_capabilities_v1")
	if q == nil {
		t.Fatal("pg_role_capabilities_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	if !strings.Contains(sql, "jsonb_build_object") {
		t.Error("pg_role_capabilities_v1 must emit role_attrs as a JSON object via jsonb_build_object")
	}
	// Every role attribute listed in the spec must appear.
	for _, attr := range []string{
		"rolcreaterole", "rolcreatedb", "rolcanlogin",
		"rolreplication", "rolbypassrls", "rolconnlimit",
	} {
		if !strings.Contains(sql, attr) {
			t.Errorf("pg_role_capabilities_v1 role_attrs must include %q", attr)
		}
	}
}
