package tests

import (
	"strings"
	"testing"

	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// ---------------------------------------------------------------------------
// Diagnostic Pack 1 — collector registration and linting tests
// ---------------------------------------------------------------------------

var diagnosticPack1 = []struct {
	id       string
	category string
}{
	{"server_identity_v1", "server"},
	{"extension_inventory_v1", "server"},
	{"bgwriter_stats_v1", "server"},
	{"long_running_txns_v1", "activity"},
	{"blocking_locks_v1", "activity"},
	{"login_roles_v1", "security"},
	{"connection_utilization_v1", "activity"},
	{"planner_stats_staleness_v1", "tables"},
	{"pgss_reset_check_v1", "extensions"},
	{"pgss_capacity_v1", "extensions"},
}

// TestDiagnosticPack1AllRegistered verifies that all diagnostic pack 1
// collectors are registered in the catalog.
func TestDiagnosticPack1AllRegistered(t *testing.T) {
	for _, tc := range diagnosticPack1 {
		q := pgqueries.ByID(tc.id)
		if q == nil {
			t.Errorf("collector %q is not registered in catalog", tc.id)
			continue
		}
		if q.Category != tc.category {
			t.Errorf("collector %q: category=%q, want %q", tc.id, q.Category, tc.category)
		}
	}
}

// TestDiagnosticPack1AllPassLinter verifies that all diagnostic pack 1
// queries pass the static SQL linter.
func TestDiagnosticPack1AllPassLinter(t *testing.T) {
	for _, tc := range diagnosticPack1 {
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

// TestDiagnosticPack1CatalogCount verifies that the total catalog now
// contains at least 22 collectors (12 baseline + 9 diagnostic pack 1
// + 1 added by Elevarq/Arq-Signals#132 capacity collector).
func TestDiagnosticPack1CatalogCount(t *testing.T) {
	all := pgqueries.All()
	if len(all) < 22 {
		t.Errorf("catalog has %d collectors, want at least 22 (12 baseline + 9 diagnostic pack 1 + capacity)", len(all))
	}
}

// TestPgssResetCheckRequiresExtension verifies that the
// pg_stat_statements reset check requires the extension.
func TestPgssResetCheckRequiresExtension(t *testing.T) {
	q := pgqueries.ByID("pgss_reset_check_v1")
	if q == nil {
		t.Fatal("pgss_reset_check_v1 not registered")
	}
	if q.RequiresExtension != "pg_stat_statements" {
		t.Errorf("RequiresExtension: got %q, want %q", q.RequiresExtension, "pg_stat_statements")
	}
	if q.MinPGVersion != 14 {
		t.Errorf("MinPGVersion: got %d, want 14", q.MinPGVersion)
	}
}

// TestPgssResetCheckGracefulWhenAbsent verifies that the extension
// gating mechanism correctly excludes pgss_reset_check_v1 when
// pg_stat_statements is not installed.
func TestPgssResetCheckGracefulWhenAbsent(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 16,
		Extensions:     []string{}, // no extensions installed
	})
	for _, q := range filtered {
		if q.ID == "pgss_reset_check_v1" {
			t.Error("pgss_reset_check_v1 should be excluded when pg_stat_statements is not installed")
		}
	}
}

// TestPgssResetCheckIncludedWhenPresent verifies that
// pgss_reset_check_v1 is included when pg_stat_statements is installed
// on PG 14+.
func TestPgssResetCheckIncludedWhenPresent(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 16,
		Extensions:     []string{"pg_stat_statements"},
	})
	found := false
	for _, q := range filtered {
		if q.ID == "pgss_reset_check_v1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("pgss_reset_check_v1 should be included when pg_stat_statements is installed on PG 16")
	}
}

// TestDiagnosticPack1NoDuplicateIDs verifies that no diagnostic pack
// collector has an ID that conflicts with existing collectors.
func TestDiagnosticPack1NoDuplicateIDs(t *testing.T) {
	seen := make(map[string]bool)
	for _, q := range pgqueries.All() {
		if seen[q.ID] {
			t.Errorf("duplicate collector ID: %q", q.ID)
		}
		seen[q.ID] = true
	}
}

// TestLoginRolesSelectsOID verifies login_roles_v1 projects the role
// OID — the join key used to resolve
// pg_stat_statements.userid to a role name.
func TestLoginRolesSelectsOID(t *testing.T) {
	q := pgqueries.ByID("login_roles_v1")
	if q == nil {
		t.Fatal("login_roles_v1 not registered")
	}
	if !containsCI(q.SQL, "oid") {
		t.Errorf("login_roles_v1 SQL must select oid (join key): %s", q.SQL)
	}
}

// TestLoginRolesDoesNotExposePasswords verifies that the login_roles_v1
// query does not select password hashes or secrets.
func TestLoginRolesDoesNotExposePasswords(t *testing.T) {
	q := pgqueries.ByID("login_roles_v1")
	if q == nil {
		t.Fatal("login_roles_v1 not registered")
	}
	sql := q.SQL
	for _, forbidden := range []string{"rolpassword", "passwd", "pg_authid"} {
		if containsCI(sql, forbidden) {
			t.Errorf("login_roles_v1 SQL contains %q — may expose password hashes", forbidden)
		}
	}
}

func containsCI(s, substr string) bool {
	sl := len(substr)
	for i := 0; i <= len(s)-sl; i++ {
		match := true
		for j := 0; j < sl; j++ {
			a, b := s[i+j], substr[j]
			if a >= 'A' && a <= 'Z' {
				a += 32
			}
			if b >= 'A' && b <= 'Z' {
				b += 32
			}
			if a != b {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// TestSnapshotIdentityFieldsPresent verifies that the snapshot
// contract includes the fields required by the Arq Analyzer for
// database key derivation.
// Spec: Arq specifications/arq-analyzer-v0.1/snapshot-identity.md
func TestSnapshotIdentityFieldsPresent(t *testing.T) {
	// metadata.json must include instance_id
	q := pgqueries.ByID("server_identity_v1")
	if q == nil {
		t.Fatal("server_identity_v1 not registered — required for snapshot identity")
	}

	sql := strings.ToLower(q.SQL)
	if !strings.Contains(sql, "database_name") {
		t.Error("server_identity_v1 must include database_name for Arq Analyzer compliance")
	}
}

// ---------------------------------------------------------------------------
// pgss_capacity_v1 — Elevarq/Arq-Signals#132
// ---------------------------------------------------------------------------

// TestPgssCapacityRequiresExtension verifies the capacity collector
// is gated on pg_stat_statements and PG 14+.
func TestPgssCapacityRequiresExtension(t *testing.T) {
	q := pgqueries.ByID("pgss_capacity_v1")
	if q == nil {
		t.Fatal("pgss_capacity_v1 not registered")
	}
	if q.RequiresExtension != "pg_stat_statements" {
		t.Errorf("RequiresExtension: got %q, want %q", q.RequiresExtension, "pg_stat_statements")
	}
	if q.MinPGVersion != 14 {
		t.Errorf("MinPGVersion: got %d, want 14", q.MinPGVersion)
	}
}

// TestPgssCapacityGracefulWhenAbsent verifies the capacity collector
// is excluded when pg_stat_statements is not installed.
func TestPgssCapacityGracefulWhenAbsent(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 16,
		Extensions:     []string{},
	})
	for _, q := range filtered {
		if q.ID == "pgss_capacity_v1" {
			t.Error("pgss_capacity_v1 should be excluded when pg_stat_statements is not installed")
		}
	}
}

// TestPgssCapacityIncludedWhenPresent verifies the capacity collector
// is included when pg_stat_statements is installed on PG 14+.
func TestPgssCapacityIncludedWhenPresent(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 16,
		Extensions:     []string{"pg_stat_statements"},
	})
	found := false
	for _, q := range filtered {
		if q.ID == "pgss_capacity_v1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("pgss_capacity_v1 should be included when pg_stat_statements is installed on PG 16")
	}
}

// TestPgssCapacityExcludedOnPG13 verifies the capacity collector
// is excluded on PG 13 (info view is PG 14+).
func TestPgssCapacityExcludedOnPG13(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 13,
		Extensions:     []string{"pg_stat_statements"},
	})
	for _, q := range filtered {
		if q.ID == "pgss_capacity_v1" {
			t.Error("pgss_capacity_v1 should be excluded on PG 13 (info view is PG 14+)")
		}
	}
}

// TestPgssCapacitySQLEmitsDeallocAndTrackedCount verifies the SQL
// returns both columns the analyzer rules consume.
func TestPgssCapacitySQLEmitsDeallocAndTrackedCount(t *testing.T) {
	q := pgqueries.ByID("pgss_capacity_v1")
	if q == nil {
		t.Fatal("pgss_capacity_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	if !strings.Contains(sql, "dealloc") {
		t.Error("pgss_capacity_v1 SQL must select the dealloc column")
	}
	if !strings.Contains(sql, "tracked_count") {
		t.Error("pgss_capacity_v1 SQL must emit a tracked_count column")
	}
	if !strings.Contains(sql, "pg_stat_statements_info") {
		t.Error("pgss_capacity_v1 SQL must read from pg_stat_statements_info")
	}
}
