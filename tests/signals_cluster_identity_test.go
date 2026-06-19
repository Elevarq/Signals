package tests

import (
	"strings"
	"testing"
	"time"

	"github.com/elevarq/signals/internal/pgqueries"
)

// ---------------------------------------------------------------------------
// cluster_identity_v1 — network and cluster-level identity for disambiguating
// same-named databases across physical PostgreSQL clusters.
//
// Specification: specifications/collectors/cluster_identity_v1.md
// Acceptance:    specifications/collectors/cluster_identity_v1.acceptance.md
// ---------------------------------------------------------------------------

func TestClusterIdentityCollectorRegistered(t *testing.T) {
	q := pgqueries.ByID("cluster_identity_v1")
	if q == nil {
		t.Fatal("cluster_identity_v1 is not registered")
	}
	if q.Category != "server" {
		t.Errorf("category: got %q, want %q", q.Category, "server")
	}
}

func TestClusterIdentityCollectorPassesLinter(t *testing.T) {
	q := pgqueries.ByID("cluster_identity_v1")
	if q == nil {
		t.Fatal("cluster_identity_v1 not registered")
	}
	if err := pgqueries.LintQuery(q.SQL); err != nil {
		t.Errorf("cluster_identity_v1 failed linter: %v", err)
	}
}

func TestClusterIdentityCollectorCadence(t *testing.T) {
	q := pgqueries.ByID("cluster_identity_v1")
	if q == nil {
		t.Fatal("cluster_identity_v1 not registered")
	}
	if q.Cadence != pgqueries.Cadence6h {
		t.Errorf("cadence: got %v, want Cadence6h", q.Cadence)
	}
}

func TestClusterIdentityCollectorRetention(t *testing.T) {
	q := pgqueries.ByID("cluster_identity_v1")
	if q == nil {
		t.Fatal("cluster_identity_v1 not registered")
	}
	if q.RetentionClass != pgqueries.RetentionLong {
		t.Errorf("retention: got %q, want RetentionLong", q.RetentionClass)
	}
}

func TestClusterIdentityCollectorResultKind(t *testing.T) {
	q := pgqueries.ByID("cluster_identity_v1")
	if q == nil {
		t.Fatal("cluster_identity_v1 not registered")
	}
	if q.ResultKind != pgqueries.ResultScalar {
		t.Errorf("ResultKind: got %q, want scalar (single-row collector)", q.ResultKind)
	}
}

func TestClusterIdentityCollectorTimeout(t *testing.T) {
	q := pgqueries.ByID("cluster_identity_v1")
	if q == nil {
		t.Fatal("cluster_identity_v1 not registered")
	}
	if q.Timeout <= 0 || q.Timeout > 10*time.Second {
		t.Errorf("timeout: got %v, want a bounded value (>0, <=10s)", q.Timeout)
	}
}

func TestClusterIdentityCollectorIncludedOnPG14(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 14,
		Extensions:     []string{},
	})
	for _, q := range filtered {
		if q.ID == "cluster_identity_v1" {
			return
		}
	}
	t.Error("cluster_identity_v1 must be included on PG 14")
}

func TestClusterIdentityCollectorIncludedOnPG18(t *testing.T) {
	filtered := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion: 18,
		Extensions:     []string{},
	})
	for _, q := range filtered {
		if q.ID == "cluster_identity_v1" {
			return
		}
	}
	t.Error("cluster_identity_v1 must be included on PG 18")
}

func TestClusterIdentityCollectorNoSelectStar(t *testing.T) {
	q := pgqueries.ByID("cluster_identity_v1")
	if q == nil {
		t.Fatal("cluster_identity_v1 not registered")
	}
	if strings.Contains(q.SQL, "SELECT *") || strings.Contains(q.SQL, "select *") {
		t.Error("cluster_identity_v1 must not use SELECT *")
	}
}

func TestClusterIdentityCollectorOutputColumns(t *testing.T) {
	q := pgqueries.ByID("cluster_identity_v1")
	if q == nil {
		t.Fatal("cluster_identity_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	required := []string{
		"inet_server_addr",
		"inet_server_port",
		"is_in_recovery",
		"cluster_name",
		"server_timezone",
		"last_wal_receive_lsn",
		"last_wal_replay_lsn",
		"postmaster_start_time",
	}
	for _, col := range required {
		if !strings.Contains(sql, col) {
			t.Errorf("cluster_identity_v1 must emit column %q", col)
		}
	}
}

func TestClusterIdentityCollectorOmitsSystemIdentifier(t *testing.T) {
	// system_identifier is explicitly out of scope (gated by
	// pg_read_all_stats / pg_monitor; linter blocks the natural
	// graceful-fallback pattern). See spec § Out of scope.
	q := pgqueries.ByID("cluster_identity_v1")
	if q == nil {
		t.Fatal("cluster_identity_v1 not registered")
	}
	if containsCI(q.SQL, "pg_control_system") {
		t.Error("cluster_identity_v1 must NOT call pg_control_system() (out of scope per spec)")
	}
	if containsCI(q.SQL, "system_identifier") {
		t.Error("cluster_identity_v1 must NOT emit system_identifier (out of scope per spec)")
	}
}

func TestClusterIdentityCollectorCoalescesEmptyClusterName(t *testing.T) {
	// Empty-string cluster_name must be coalesced to NULL per spec invariant.
	q := pgqueries.ByID("cluster_identity_v1")
	if q == nil {
		t.Fatal("cluster_identity_v1 not registered")
	}
	if !containsCI(q.SQL, "nullif") {
		t.Error("cluster_identity_v1 must use NULLIF (or equivalent) to coalesce empty cluster_name to NULL")
	}
}

func TestClusterIdentityCollectorUsesRequiredFunctions(t *testing.T) {
	q := pgqueries.ByID("cluster_identity_v1")
	if q == nil {
		t.Fatal("cluster_identity_v1 not registered")
	}
	required := []string{
		"inet_server_addr(",
		"inet_server_port(",
		"pg_is_in_recovery(",
		"pg_last_wal_receive_lsn(",
		"pg_last_wal_replay_lsn(",
		"pg_postmaster_start_time(",
	}
	for _, fn := range required {
		if !containsCI(q.SQL, fn) {
			t.Errorf("cluster_identity_v1 must call %s", fn)
		}
	}
}

func TestClusterIdentityCollectorReadsClusterNameAndTimezone(t *testing.T) {
	q := pgqueries.ByID("cluster_identity_v1")
	if q == nil {
		t.Fatal("cluster_identity_v1 not registered")
	}
	sql := strings.ToLower(q.SQL)
	if !strings.Contains(sql, "'cluster_name'") {
		t.Error("cluster_identity_v1 must read current_setting('cluster_name')")
	}
	if !strings.Contains(sql, "'timezone'") {
		t.Error("cluster_identity_v1 must read current_setting('TimeZone')")
	}
}

func TestClusterIdentityCollectorMinPGVersion(t *testing.T) {
	q := pgqueries.ByID("cluster_identity_v1")
	if q == nil {
		t.Fatal("cluster_identity_v1 not registered")
	}
	if q.MinPGVersion > 10 {
		t.Errorf("MinPGVersion: got %d, want <=10 (collector relies only on pg_last_wal_* renamed in PG10)", q.MinPGVersion)
	}
}
