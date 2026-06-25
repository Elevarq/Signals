package tests

import (
	"sort"
	"testing"

	"github.com/elevarq/signals/internal/pgqueries"
)

// Owner-only privilege degradation — registry surface (#200).
//
// Specification: specifications/owner_only_privilege_degradation.md
// Acceptance:    specifications/owner_only_privilege_degradation.acceptance.md
//
// Exactly the two pg_statistic_ext_data collectors read an owner-only
// catalog (PUBLIC SELECT revoked), so exactly those two carry
// OwnerOnlyDegrade. Any other collector flagged OwnerOnlyDegrade would
// silently turn a real permission failure into an ignored skip.

// R116 / TC-OOPD-06: the extended-statistics-data collectors are flagged.
func TestExtStatDataCollectorsAreOwnerOnlyDegrade(t *testing.T) {
	for _, id := range []string{"pg_statistic_ext_data_v1", "pg_statistic_ext_data_mcv_v1"} {
		q := pgqueries.ByID(id)
		if q == nil {
			t.Fatalf("collector %q not registered", id)
		}
		if !q.OwnerOnlyDegrade {
			t.Errorf("collector %q must set OwnerOnlyDegrade", id)
		}
	}
}

// R116 / TC-OOPD-07: a representative ordinary collector is NOT flagged.
func TestOrdinaryCollectorIsNotOwnerOnlyDegrade(t *testing.T) {
	q := pgqueries.ByID("pg_stat_user_tables_v1")
	if q == nil {
		t.Fatal("control collector pg_stat_user_tables_v1 not registered")
	}
	if q.OwnerOnlyDegrade {
		t.Error("an ordinary collector must not set OwnerOnlyDegrade")
	}
}

// R116 / TC-OOPD-08: the flag is confined to exactly the two owner-only
// collectors across the whole registry — a guard against accidental spread.
func TestOnlyExtStatDataCollectorsAreOwnerOnlyDegrade(t *testing.T) {
	var flagged []string
	for _, q := range pgqueries.All() {
		if q.OwnerOnlyDegrade {
			flagged = append(flagged, q.ID)
		}
	}
	sort.Strings(flagged)
	want := []string{"pg_statistic_ext_data_mcv_v1", "pg_statistic_ext_data_v1"}
	if len(flagged) != len(want) {
		t.Fatalf("OwnerOnlyDegrade collectors = %v, want %v", flagged, want)
	}
	for i := range want {
		if flagged[i] != want[i] {
			t.Errorf("OwnerOnlyDegrade collectors = %v, want %v", flagged, want)
			break
		}
	}
}
