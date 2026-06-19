package tests

import (
	"strings"
	"testing"

	"github.com/elevarq/signals/internal/pgqueries"
)

// Elevarq/Signals#217-#222 (#212 family): the remaining
// user-defined-object collectors — operators, aggregates, rules, casts,
// collations, text-search configurations. Each emits user-defined,
// non-extension-owned objects so queries that reference them remain
// analysable. SQL validated against live PG17.

var userDefinedCollectors = map[string][]string{
	"pg_operators_v1":   {"schemaname", "oprname", "left_type", "right_type", "result_type", "function", "oprcanmerge", "oprcanhash"},
	"pg_aggregates_v1":  {"schemaname", "aggname", "identity_args", "state_type", "sfunc", "finalfunc", "combinefunc", "initcond", "aggkind"},
	"pg_rules_v1":       {"schemaname", "tablename", "rulename", "definition"},
	"pg_casts_v1":       {"source_schema", "source_type", "target_schema", "target_type", "cast_impl", "castcontext"},
	"pg_collations_v1":  {"schemaname", "collname", "provider", "collcollate", "collctype", "collisdeterministic"},
	"pg_text_search_v1": {"schemaname", "cfgname", "parser"},
}

func TestUserDefinedCollectorsRegisteredAndLint(t *testing.T) {
	for id := range userDefinedCollectors {
		q := pgqueries.ByID(id)
		if q == nil {
			t.Errorf("%s not registered", id)
			continue
		}
		if q.Category != "schema" {
			t.Errorf("%s category = %q, want schema", id, q.Category)
		}
		if err := pgqueries.LintQuery(q.SQL); err != nil {
			t.Errorf("%s failed lint: %v", id, err)
		}
	}
}

func TestUserDefinedCollectorsColumns(t *testing.T) {
	for id, cols := range userDefinedCollectors {
		q := pgqueries.ByID(id)
		if q == nil {
			t.Errorf("%s not registered", id)
			continue
		}
		for _, c := range cols {
			if !strings.Contains(q.SQL, c) {
				t.Errorf("%s SQL missing output column %q", id, c)
			}
		}
		// Each catalog-backed collector excludes extension-owned
		// objects (the user-defined-only contract). pg_rules_v1 is the
		// exception: it reads the pg_rules VIEW (no OID for a pg_depend
		// join) and is bounded instead by the user-schema filter plus
		// the _RETURN exclusion — rules are inherently user-table-scoped.
		if id == "pg_rules_v1" {
			continue
		}
		if !strings.Contains(q.SQL, "deptype = 'e'") {
			t.Errorf("%s SQL must exclude extension-owned objects (deptype = 'e')", id)
		}
	}
}

// pg_casts_v1 excludes built-ins by OID floor (casts are schemaless);
// the others rely on the system-schema filter.
func TestUserDefinedCollectorsBuiltinExclusion(t *testing.T) {
	casts := pgqueries.ByID("pg_casts_v1")
	if casts == nil {
		t.Fatal("pg_casts_v1 not registered")
	}
	if !strings.Contains(casts.SQL, "c.oid >= 16384") {
		t.Errorf("pg_casts_v1 must exclude built-in casts by OID floor (16384)")
	}
	for _, id := range []string{"pg_operators_v1", "pg_aggregates_v1", "pg_collations_v1", "pg_text_search_v1"} {
		q := pgqueries.ByID(id)
		if !strings.Contains(q.SQL, "NOT IN ('pg_catalog', 'information_schema', 'pg_toast')") {
			t.Errorf("%s must exclude built-ins via the system-schema filter", id)
		}
	}
}

// Only pg_rules_v1 is HighSensitivity (its definition is arbitrary SQL);
// the rest are Normal (structure/references).
func TestUserDefinedCollectorsSensitivity(t *testing.T) {
	if !pgqueries.ByID("pg_rules_v1").HighSensitivity {
		t.Error("pg_rules_v1 must be HighSensitivity (rule action is arbitrary SQL)")
	}
	for _, id := range []string{"pg_operators_v1", "pg_aggregates_v1", "pg_casts_v1", "pg_collations_v1", "pg_text_search_v1"} {
		if pgqueries.ByID(id).HighSensitivity {
			t.Errorf("%s should be Normal sensitivity (structure, not source text)", id)
		}
	}
}
