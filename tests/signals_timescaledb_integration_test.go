//go:build integration

package tests

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// ---------------------------------------------------------------------------
// timescaledb_family_v1 — live-server integration (R114, issue #73).
//
// Gated on ARQ_TEST_TSDB_DSN pointing at a TimescaleDB-enabled target,
// e.g. timescale/timescaledb:2.27.2-pg17 (Community) or the -oss tag
// (Apache-2 edition). Run with:
//
//	ARQ_TEST_TSDB_DSN="postgres://postgres:secret@localhost:5433/postgres" \
//	  go test -tags integration ./tests/ -run Integration_TimescaleDB
//
// Scenario fixtures (hypertable / chunks / compression / continuous
// aggregate / retention policy — TC-TSDB-03..11) land with the
// implementation slice; this failing-first slice asserts eligibility
// and that every family query executes read-only without error
// (TC-TSDB-02 core).
//
// Specification: specifications/collectors/timescaledb_family_v1.md
// ---------------------------------------------------------------------------

// TestIntegration_TimescaleDBFamilyEligibleAndExecutes connects to a
// TimescaleDB target, asserts the whole R114 family is eligible, and
// executes every member inside a read-only transaction.
func TestIntegration_TimescaleDBFamilyEligibleAndExecutes(t *testing.T) {
	dsn := os.Getenv("ARQ_TEST_TSDB_DSN")
	if dsn == "" {
		t.Skip("ARQ_TEST_TSDB_DSN not set — skipping TimescaleDB integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	var extversion string
	err = pool.QueryRow(ctx,
		`SELECT extversion FROM pg_extension WHERE extname = 'timescaledb'`,
	).Scan(&extversion)
	if err != nil {
		t.Fatalf("ARQ_TEST_TSDB_DSN target has no timescaledb extension (or probe failed): %v", err)
	}
	t.Logf("target TimescaleDB version: %s", extversion)

	var major int
	if err := pool.QueryRow(ctx,
		`SELECT current_setting('server_version_num')::int / 10000`,
	).Scan(&major); err != nil {
		t.Fatalf("server version probe: %v", err)
	}

	eligible := pgqueries.Filter(pgqueries.FilterParams{
		PGMajorVersion:         major,
		Extensions:             []string{"timescaledb"},
		HighSensitivityEnabled: true,
	})
	byID := make(map[string]pgqueries.QueryDef, len(eligible))
	for _, q := range eligible {
		byID[q.ID] = q
	}

	for _, id := range timescaleDBFamilyIDs {
		q, ok := byID[id]
		if !ok {
			t.Errorf("%s not eligible against PG %d + timescaledb %s", id, major, extversion)
			continue
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
// TC-TSDB-01 against a live plain-PostgreSQL target: with the
// extension absent the family is gated out before any SQL runs and is
// accounted for under reason=extension_missing (INV-SIGNALS-24).
func TestIntegration_TimescaleDBFamilyInertOnPlainPostgres(t *testing.T) {
	dsn := os.Getenv("ARQ_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("ARQ_TEST_PG_DSN not set — skipping plain-PostgreSQL integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	var hasTS bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'timescaledb')`,
	).Scan(&hasTS); err != nil {
		t.Fatalf("extension probe: %v", err)
	}
	if hasTS {
		t.Skip("ARQ_TEST_PG_DSN target has timescaledb installed — not a plain-PostgreSQL target")
	}

	var major int
	if err := pool.QueryRow(ctx,
		`SELECT current_setting('server_version_num')::int / 10000`,
	).Scan(&major); err != nil {
		t.Fatalf("server version probe: %v", err)
	}

	params := pgqueries.FilterParams{
		PGMajorVersion:         major,
		Extensions:             nil, // discovery on this target finds no timescaledb
		HighSensitivityEnabled: true,
	}
	for _, q := range pgqueries.Filter(params) {
		for _, id := range timescaleDBFamilyIDs {
			if q.ID == id {
				t.Errorf("%s eligible on a target without timescaledb", id)
			}
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
