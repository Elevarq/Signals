package tests

import (
	"strings"
	"testing"

	"github.com/elevarq/signals/internal/pgqueries"
)

// Elevarq/Signals#214: pg_policies_v1 emits RLS policies + the
// per-table RLS flags so RLS-protected tables can be analysed
// accurately. Audit: #212.

func TestPgPoliciesV1Registered(t *testing.T) {
	q := pgqueries.ByID("pg_policies_v1")
	if q == nil {
		t.Fatal("pg_policies_v1 not registered")
	}
	if q.Category != "schema" {
		t.Errorf("category = %q, want schema", q.Category)
	}
	if err := pgqueries.LintQuery(q.SQL); err != nil {
		t.Errorf("pg_policies_v1 failed lint: %v", err)
	}
}

// Policy qual/with_check are arbitrary SQL expressions → HighSensitivity
// (like view/function/trigger definition text), gated off by default.
func TestPgPoliciesV1HighSensitivity(t *testing.T) {
	q := pgqueries.ByID("pg_policies_v1")
	if q == nil {
		t.Fatal("pg_policies_v1 not registered")
	}
	if !q.HighSensitivity {
		t.Errorf("pg_policies_v1 must be HighSensitivity (qual/with_check are expression text)")
	}
}

// Pins the output columns downstream analysis needs.
func TestPgPoliciesV1Columns(t *testing.T) {
	q := pgqueries.ByID("pg_policies_v1")
	if q == nil {
		t.Fatal("pg_policies_v1 not registered")
	}
	for _, col := range []string{
		"schemaname", "tablename", "policyname", "permissive",
		"roles", "cmd", "qual", "with_check",
		"rls_enabled", "rls_forced",
	} {
		if !strings.Contains(q.SQL, col) {
			t.Errorf("pg_policies_v1 SQL missing output column %q", col)
		}
	}
}

// HighSensitivity collectors are gated off by default — confirm the
// daemon-wide gate (R075) hides it unless enabled.
func TestPgPoliciesV1GatedByDefault(t *testing.T) {
	out := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion:         16,
		HighSensitivityEnabled: false,
	})
	for _, q := range out {
		if q.ID == "pg_policies_v1" {
			t.Errorf("pg_policies_v1 should be gated off when HighSensitivityEnabled=false")
		}
	}
	on := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion:         16,
		HighSensitivityEnabled: true,
	})
	found := false
	for _, q := range on {
		if q.ID == "pg_policies_v1" {
			found = true
		}
	}
	if !found {
		t.Errorf("pg_policies_v1 should run when HighSensitivityEnabled=true")
	}
}
