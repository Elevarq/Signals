package tests

import (
	"strings"
	"testing"

	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// ---------------------------------------------------------------------------
// Server Survival Pack — registration, linting, and safety tests
// ---------------------------------------------------------------------------

var survivalPack = []struct {
	id       string
	category string
}{
	{"replication_slots_risk_v1", "replication"},
	{"replication_status_v1", "replication"},
	{"checkpointer_stats_v1", "server"},
	{"vacuum_health_v1", "tables"},
	{"idle_in_txn_offenders_v1", "activity"},
	{"database_sizes_v1", "server"},
	{"largest_relations_v1", "tables"},
	{"temp_io_pressure_v1", "server"},
}

// TestSurvivalPackAllRegistered verifies all survival pack collectors
// are registered in the catalog.
func TestSurvivalPackAllRegistered(t *testing.T) {
	for _, tc := range survivalPack {
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

// TestSurvivalPackAllPassLinter verifies all survival pack queries
// pass the static SQL linter.
func TestSurvivalPackAllPassLinter(t *testing.T) {
	for _, tc := range survivalPack {
		q := pgqueries.ByID(tc.id)
		if q == nil {
			continue
		}
		if err := pgqueries.LintQuery(q.SQL); err != nil {
			t.Errorf("collector %q failed linter: %v", tc.id, err)
		}
	}
}

// TestSurvivalPackTotalCatalogCount verifies the catalog now contains
// at least 29 collectors (21 baseline + 8 survival).
func TestSurvivalPackTotalCatalogCount(t *testing.T) {
	all := pgqueries.All()
	if len(all) < 29 {
		t.Errorf("catalog has %d collectors, want at least 29", len(all))
	}
}

// TestCheckpointerStatsVersionGated verifies that checkpointer_stats_v1
// is excluded on PG < 17 and included on PG >= 17.
func TestCheckpointerStatsVersionGated(t *testing.T) {
	q := pgqueries.ByID("checkpointer_stats_v1")
	if q == nil {
		t.Fatal("checkpointer_stats_v1 not registered")
	}
	if q.MinPGVersion != 17 {
		t.Errorf("MinPGVersion: got %d, want 17", q.MinPGVersion)
	}

	// PG 16: should be excluded
	filtered16 := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 16,
		Extensions:     []string{},
	})
	for _, fq := range filtered16 {
		if fq.ID == "checkpointer_stats_v1" {
			t.Error("checkpointer_stats_v1 should be excluded on PG 16")
		}
	}

	// PG 17: should be included
	filtered17 := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 17,
		Extensions:     []string{},
	})
	found := false
	for _, fq := range filtered17 {
		if fq.ID == "checkpointer_stats_v1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("checkpointer_stats_v1 should be included on PG 17")
	}
}

// TestReplicationCollectorsGracefulWhenAbsent verifies that replication
// collectors do not require any extension or special gating — they
// return empty rowsets naturally when no replication is configured.
func TestReplicationCollectorsGracefulWhenAbsent(t *testing.T) {
	for _, id := range []string{"replication_slots_risk_v1", "replication_status_v1"} {
		q := pgqueries.ByID(id)
		if q == nil {
			t.Errorf("%s not registered", id)
			continue
		}
		if q.RequiresExtension != "" {
			t.Errorf("%s should not require an extension (graceful-skip via empty result)", id)
		}
		if q.MinPGVersion > 14 {
			t.Errorf("%s has MinPGVersion=%d, should work on PG 14+", id, q.MinPGVersion)
		}
	}
}

// TestVacuumHealthDoesNotDuplicateRawStats verifies that vacuum_health_v1
// adds value beyond pg_stat_user_tables_v1 by including XID age,
// reloptions, and dead tuple percentage.
func TestVacuumHealthDoesNotDuplicateRawStats(t *testing.T) {
	q := pgqueries.ByID("vacuum_health_v1")
	if q == nil {
		t.Fatal("vacuum_health_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	for _, signal := range []string{"dead_pct", "xid_age", "reloptions"} {
		if !strings.Contains(sql, signal) {
			t.Errorf("vacuum_health_v1 should include %q for operator value", signal)
		}
	}
}

// TestSurvivalPackNoDuplicateIDs verifies no ID conflicts.
func TestSurvivalPackNoDuplicateIDs(t *testing.T) {
	seen := make(map[string]bool)
	for _, q := range pgqueries.All() {
		if seen[q.ID] {
			t.Errorf("duplicate collector ID: %q", q.ID)
		}
		seen[q.ID] = true
	}
}

// TestSurvivalPackNoSecretLeakage verifies that no survival collector
// SQL accesses pg_authid or password-related columns.
func TestSurvivalPackNoSecretLeakage(t *testing.T) {
	for _, tc := range survivalPack {
		q := pgqueries.ByID(tc.id)
		if q == nil {
			continue
		}
		lower := strings.ToLower(q.SQL)
		for _, forbidden := range []string{"pg_authid", "rolpassword", "passwd"} {
			if strings.Contains(lower, forbidden) {
				t.Errorf("collector %q SQL contains %q — potential secret leakage", tc.id, forbidden)
			}
		}
	}
}

// TestIdleInTxnOffendersFocused verifies the idle-in-transaction
// collector filters correctly.
func TestIdleInTxnOffendersFocused(t *testing.T) {
	q := pgqueries.ByID("idle_in_txn_offenders_v1")
	if q == nil {
		t.Fatal("idle_in_txn_offenders_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	if !strings.Contains(sql, "idle in transaction") {
		t.Error("idle_in_txn_offenders_v1 should filter for idle-in-transaction state")
	}
	if !strings.Contains(sql, "pid != pg_backend_pid()") {
		t.Error("idle_in_txn_offenders_v1 should exclude own PID")
	}
}
