package tests

import (
	"strings"
	"testing"

	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// Elevarq/Arq-Signals#213: pg_types_v1 emits user-defined enum /
// composite / domain structure (CREATE TYPE / CREATE DOMAIN) so tables
// that use them remain analysable instead of being skipped. Audit: #212.

func TestPgTypesV1Registered(t *testing.T) {
	q := pgqueries.ByID("pg_types_v1")
	if q == nil {
		t.Fatal("pg_types_v1 not registered")
	}
	if q.Category != "schema" {
		t.Errorf("category = %q, want schema", q.Category)
	}
	if err := pgqueries.LintQuery(q.SQL); err != nil {
		t.Errorf("pg_types_v1 failed lint: %v", err)
	}
}

// The collector MUST emit structured columns, NOT a ready DDL string:
// the safety linter bans the literal keyword CREATE in collector SQL,
// so a server-side `CREATE TYPE …` string would be rejected. This test
// pins that the query text carries none of the banned DDL keywords and
// exposes the structured columns the analyzer assembles the DDL from.
func TestPgTypesV1StructuredColumns(t *testing.T) {
	q := pgqueries.ByID("pg_types_v1")
	if q == nil {
		t.Fatal("pg_types_v1 not registered")
	}
	for _, col := range []string{
		"schemaname", "typename", "typtype",
		"enum_labels", "composite_columns",
		"domain_basetype", "domain_notnull", "domain_default", "domain_constraints",
	} {
		if !strings.Contains(q.SQL, col) {
			t.Errorf("pg_types_v1 SQL missing output column %q", col)
		}
	}
	// No DDL keyword literals in the query text (the structured-columns
	// requirement / linter constraint).
	upper := strings.ToUpper(q.SQL)
	for _, banned := range []string{"CREATE ", "ALTER ", "DROP "} {
		if strings.Contains(upper, banned) {
			t.Errorf("pg_types_v1 SQL must not contain the DDL keyword %q (linter constraint)", strings.TrimSpace(banned))
		}
	}
}

// pg_types_v1 must be sorted into the catalog deterministically and
// included in the default (non-high-sensitivity) collection — type
// structure is normal sensitivity, like pg_constraints_v1.
func TestPgTypesV1NotHighSensitivity(t *testing.T) {
	q := pgqueries.ByID("pg_types_v1")
	if q == nil {
		t.Fatal("pg_types_v1 not registered")
	}
	if q.HighSensitivity {
		t.Errorf("pg_types_v1 should be normal sensitivity (structure metadata, like pg_constraints_v1)")
	}
}
