//go:build integration

package tests

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/elevarq/signals/internal/pgqueries"
)

// ---------------------------------------------------------------------------
// timescaledb_family_v1 — live-server integration (R114/R115, issue #73).
//
// Gated on SIGNALS_TEST_TSDB_DSN pointing at a TimescaleDB-enabled target,
// e.g. timescale/timescaledb:2.27.2-pg17 (Community) or the -oss tag
// (Apache-2 edition). Run with:
//
//	SIGNALS_TEST_TSDB_DSN="postgres://postgres:secret@localhost:5433/postgres" \
//	  go test -tags integration ./tests/ -run Integration_TimescaleDB
//
// Eligibility is computed from the REAL discovery probe
// (pgqueries.Discover — version, extensions, extension versions), so
// these tests exercise the same gating path as production, including
// the R115 extension-version floor: against a TimescaleDB < 2.14
// target the floor-gated members are asserted ineligible rather than
// executed.
//
// Scenario fixtures (hypertable / chunks / compression / continuous
// aggregate / retention policy — TC-TSDB-03..11) land with the
// implementation slice; this file asserts eligibility and that every
// eligible member executes read-only without error (TC-TSDB-02 core).
//
// Specification: specifications/collectors/timescaledb_family_v1.md
// ---------------------------------------------------------------------------

// liveDiscover runs the production discovery probe inside a read-only
// transaction, mirroring the collector's R013/R021 posture.
func liveDiscover(ctx context.Context, t *testing.T, pool *pgxpool.Pool) pgqueries.Discovery {
	t.Helper()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatalf("begin discovery tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	disc, err := pgqueries.Discover(ctx, tx)
	if err != nil {
		t.Fatalf("discovery probe: %v", err)
	}
	return disc
}

// TestIntegration_TimescaleDBFamilyEligibleAndExecutes connects to a
// TimescaleDB target, computes eligibility from real discovery
// (including the R115 version floor), and executes every eligible
// member inside a read-only transaction.
func TestIntegration_TimescaleDBFamilyEligibleAndExecutes(t *testing.T) {
	dsn := os.Getenv("SIGNALS_TEST_TSDB_DSN")
	if dsn == "" {
		t.Skip("SIGNALS_TEST_TSDB_DSN not set — skipping TimescaleDB integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	disc := liveDiscover(ctx, t, pool)
	extversion, installed := disc.ExtensionVersions["timescaledb"]
	if !installed {
		t.Fatal("SIGNALS_TEST_TSDB_DSN target has no timescaledb extension")
	}
	t.Logf("target: PG %d, TimescaleDB %s", disc.MajorVersion, extversion)

	params := pgqueries.FilterParams{
		PGMajorVersion:         disc.MajorVersion,
		Extensions:             disc.Extensions,
		ExtensionVersions:      disc.ExtensionVersions, // R115 floor active, as in production
		HighSensitivityEnabled: true,
	}
	eligible := filteredIDSet(params)
	byID := make(map[string]pgqueries.QueryDef)
	for _, q := range pgqueries.Filter(params) {
		byID[q.ID] = q
	}

	// Detection must be eligible on ANY TimescaleDB version; the rest
	// follow the 2.14 floor, which real targets in the test matrix
	// always satisfy — assert eligibility through the same gate
	// production uses rather than assuming it.
	if !eligible["timescaledb_extension_v1"] {
		t.Fatal("timescaledb_extension_v1 not eligible — detection must run on any TimescaleDB version")
	}
	for _, id := range timescaleDBFamilyIDs {
		if !eligible[id] {
			t.Errorf("%s not eligible against PG %d + timescaledb %s (R115 gate?)",
				id, disc.MajorVersion, extversion)
		}
	}

	for _, id := range timescaleDBFamilyIDs {
		q, ok := byID[id]
		if !ok {
			continue // already reported above
		}

		// Each member runs in its own read-only transaction, mirroring
		// the collector's R013/R021 posture.
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
		if err != nil {
			t.Fatalf("%s: begin read-only tx: %v", id, err)
		}
		rows, err := tx.Query(ctx, q.SQL)
		if err != nil {
			t.Errorf("%s: query failed: %v", id, err)
			_ = tx.Rollback(ctx)
			continue
		}
		n := 0
		for rows.Next() {
			n++
		}
		rowErr := rows.Err()
		rows.Close()
		_ = tx.Rollback(ctx)
		if rowErr != nil {
			t.Errorf("%s: row iteration failed: %v", id, rowErr)
			continue
		}
		t.Logf("%s: %d row(s)", id, n)
		if id == "timescaledb_extension_v1" && n != 1 {
			t.Errorf("timescaledb_extension_v1: got %d rows, want exactly 1", n)
		}
	}
}

// TestIntegration_TimescaleDBFamilyInertOnPlainPostgres encodes
// TC-TSDB-01 against a live plain-PostgreSQL target: real discovery
// finds no timescaledb extension, the family is gated out before any
// SQL runs, and every member is accounted for under
// reason=extension_missing (INV-SIGNALS-24).
func TestIntegration_TimescaleDBFamilyInertOnPlainPostgres(t *testing.T) {
	dsn := os.Getenv("SIGNALS_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("SIGNALS_TEST_PG_DSN not set — skipping plain-PostgreSQL integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	disc := liveDiscover(ctx, t, pool)
	if _, hasTS := disc.ExtensionVersions["timescaledb"]; hasTS {
		t.Skip("SIGNALS_TEST_PG_DSN target has timescaledb installed — not a plain-PostgreSQL target")
	}

	params := pgqueries.FilterParams{
		PGMajorVersion:         disc.MajorVersion,
		Extensions:             disc.Extensions,
		ExtensionVersions:      disc.ExtensionVersions,
		HighSensitivityEnabled: true,
	}
	eligible := filteredIDSet(params)
	for _, id := range timescaleDBFamilyIDs {
		if eligible[id] {
			t.Errorf("%s eligible on a target without timescaledb", id)
		}
	}
	gated := pgqueries.GatedIDsByReason(params)
	missing := make(map[string]bool)
	for _, id := range gated[pgqueries.GateReasonExtensionMissing] {
		missing[id] = true
	}
	for _, id := range timescaleDBFamilyIDs {
		if !missing[id] {
			t.Errorf("%s not accounted for under reason=%s on plain PostgreSQL",
				id, pgqueries.GateReasonExtensionMissing)
		}
	}
}
