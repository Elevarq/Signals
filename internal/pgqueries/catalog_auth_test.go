package pgqueries

import (
	"sort"
	"strings"
	"testing"
)

// Authentication / transport-security collectors (#305).
//
// Specification: specifications/collectors/pg_hba_file_rules_v1.md

// #305: pg_hba_file_rules_v1 is registered with the expected shape.
func TestPgHbaFileRulesRegistered(t *testing.T) {
	q := ByID("pg_hba_file_rules_v1")
	if q == nil {
		t.Fatal("collector pg_hba_file_rules_v1 not registered")
	}
	if q.Category != "server" {
		t.Errorf("Category = %q, want server", q.Category)
	}
	if q.ResultKind != ResultRowset {
		t.Errorf("ResultKind = %q, want rowset", q.ResultKind)
	}
	// The view exists from PG10; rule_number/file_name come in on PG15+.
	if q.MinPGVersion != 10 {
		t.Errorf("MinPGVersion = %d, want 10", q.MinPGVersion)
	}
	if !q.PrivilegedViewDegrade {
		t.Error("pg_hba_file_rules_v1 must set PrivilegedViewDegrade (view SELECT + function EXECUTE gate)")
	}
	if q.OwnerOnlyDegrade {
		t.Error("pg_hba_file_rules_v1 is not an owner-only (PUBLIC-revoked) catalog; use PrivilegedViewDegrade")
	}
	if q.HighSensitivity {
		t.Error("pg_hba_file_rules_v1 emits config metadata, not sensitive payload — must not be HighSensitivity")
	}
	// Config posture, changes rarely.
	if q.RetentionClass != RetentionLong {
		t.Errorf("RetentionClass = %q, want long", q.RetentionClass)
	}
	if q.Cadence != CadenceDaily {
		t.Errorf("Cadence = %v, want daily", q.Cadence)
	}
}

// #305 / #210: rule_number and file_name are PG15+; the base SQL stubs
// them as NULL for column stability, and an override supplies the real
// columns on PG15..18 (the integration matrix's upper majors).
func TestPgHbaFileRulesVersionColumns(t *testing.T) {
	q := ByID("pg_hba_file_rules_v1")
	if q == nil {
		t.Fatal("collector pg_hba_file_rules_v1 not registered")
	}
	// Base (PG14 and below): NULL stubs, no real rule_number/file_name.
	if !strings.Contains(q.SQL, "NULL::integer AS rule_number") ||
		!strings.Contains(q.SQL, "NULL::text    AS file_name") {
		t.Error("base SQL must emit NULL stubs for rule_number and file_name")
	}
	for _, major := range []int{15, 16, 17, 18} {
		if !HasOverride(major, "pg_hba_file_rules_v1") {
			t.Errorf("expected a PG%d override supplying real rule_number/file_name", major)
		}
	}
	if HasOverride(14, "pg_hba_file_rules_v1") {
		t.Error("PG14 must use the base (stubbed) SQL, not an override")
	}
}

// #305: PrivilegedViewDegrade is confined to exactly pg_hba_file_rules_v1
// across the whole registry — a guard against accidental spread, since a
// wrongly-flagged collector would silently turn a real permission failure
// into an ignored skip.
func TestOnlyPgHbaFileRulesIsPrivilegedViewDegrade(t *testing.T) {
	var flagged []string
	for _, q := range All() {
		if q.PrivilegedViewDegrade {
			flagged = append(flagged, q.ID)
		}
	}
	sort.Strings(flagged)
	want := []string{"pg_hba_file_rules_v1"}
	if len(flagged) != len(want) || flagged[0] != want[0] {
		t.Fatalf("PrivilegedViewDegrade collectors = %v, want %v", flagged, want)
	}
}
