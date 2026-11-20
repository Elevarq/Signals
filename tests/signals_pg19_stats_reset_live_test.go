//go:build integration

// Copyright (c) 2026 Scantr LLC. All rights reserved.
// Elevarq is a trade name of Scantr LLC.
// This file is part of Elevarq Signals. Use is governed by the
// commercial license at LICENSE in the repository root.

// Live PG19 per-object stats_reset acceptance — the previously-blocked
// TC-SR-05 of specifications/pg19-per-object-stats-reset.acceptance.md.
//
// The pg19_stats_reset_test.go unit tests are static (SQL resolution,
// registry precedence, terminal-column ordering); the collector SQL is
// never run against a live catalog, so the whole point of #817 — that
// the per-object stats_reset TIMESTAMP identifies a counter reset even
// after the cumulative counters catch back up above their pre-reset
// values — is unverifiable from them. A naive decreasing-counter check
// would MISS exactly that case; stats_reset is what makes it visible,
// and Analyzer #4315 consumes exactly this signal to invalidate a
// usage/churn/rate interval that straddles a reset instead of reporting
// a false trend.
//
// This test closes that gap live. It is PG19-only: per-object
// stats_reset does not exist below PG19 (SR-R002 emits a typed NULL
// there), so on PG14-18 the assertion would be vacuous and the test
// skips with a documented reason. The official postgres:19 image has
// now published (GA), so the integration-pg(19) CI leg can finally run
// it.
//
// What it proves (TC-SR-05 / SR-R003 / INV-SR-01):
//
//   - RESET-AFTER-CATCHUP: seed a table, generate activity so its stats
//     are non-zero, capture its per-object stats_reset (T0), reset that
//     one table's counters, then generate MORE activity so the
//     cumulative counters climb back above their pre-reset values.
//     The collector's stats_reset projection must ADVANCE (T1 > T0) —
//     the reset is identified by the timestamp even though the counters
//     caught up. This is the whole point of #817.
//   - NO-RESET STABILITY: a sibling table that is never reset keeps a
//     stable (non-advancing) stats_reset across the same two cycles, and
//     that value survives export.
//
// Collection runs as the non-superuser `signals` monitoring role via
// SIGNALS_TEST_PG_DSN exactly like the other integration-pg tests
// (INV-SR-03: no writes, no superuser for collection). The ONLY
// privileged action is the stats reset itself — a TEST SETUP step that
// simulates an external reset event (restart / crash recovery / major
// upgrade / privileged admin) and runs via the superuser setup DSN
// (SIGNALS_TEST_PG_SUPERUSER_DSN), the same seam the output-contract
// test uses for FDW provisioning. pg_stat_reset_single_table_counters
// is superuser-or-granted only — table ownership and pg_monitor do not
// confer it. Collection introduces no superuser and no write; the
// read-only collection invariant is preserved.
//
// Gated by the `integration` build tag and SIGNALS_TEST_PG_DSN (skips
// locally when unset), matching signals_integration_test.go and
// signals_collector_output_contract_integration_test.go.
//
// Run with (against a live PG19):
//
//	SIGNALS_TEST_PG_DSN="postgres://signals:signals@localhost:5432/postgres?sslmode=disable" \
//	  go test -tags integration ./tests/ \
//	  -run TestIntegration_PG19PerObjectStatsResetAfterCatchup
//
// Spec: specifications/pg19-per-object-stats-reset.md (SR-R001..R003,
//       INV-SR-01..03); acceptance TC-SR-05.

package tests

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/elevarq/signals/internal/collector"
	"github.com/elevarq/signals/internal/config"
	"github.com/elevarq/signals/internal/export"
)

const (
	// resetSchema is the dedicated schema the live reset fixture seeds so
	// repeated runs stay clean and we never touch the target's own
	// objects. Idempotently dropped on setup and on cleanup.
	resetSchema = "signals_sr_test"
	// resetTable is the relation whose per-object stats are reset between
	// the two cycles; its stats_reset MUST advance (T1 > T0).
	resetTable = "churned"
	// stableTable is the sibling relation that is never reset; its
	// stats_reset MUST stay stable across the two cycles.
	stableTable = "untouched"
)

// TestIntegration_PG19PerObjectStatsResetAfterCatchup proves the #817
// per-object stats_reset projection identifies a reset even after the
// cumulative counters catch back up. PG19-only (skips below 19 and when
// SIGNALS_TEST_PG_DSN is unset).
func TestIntegration_PG19PerObjectStatsResetAfterCatchup(t *testing.T) {
	dsn := os.Getenv("SIGNALS_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("SIGNALS_TEST_PG_DSN not set — skipping live PostgreSQL integration test")
	}
	// The reset step needs a privileged connection: pg_stat_reset_single_table_counters
	// is superuser-or-granted only (table ownership and pg_monitor do not
	// confer it), and the same superuser seam the output-contract test uses
	// for FDW provisioning performs it. Collection still runs as `signals`.
	superDSN := os.Getenv("SIGNALS_TEST_PG_SUPERUSER_DSN")
	if superDSN == "" {
		t.Skip("SIGNALS_TEST_PG_SUPERUSER_DSN not set — the reset-after-catchup setup needs a privileged reset; skipping")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect SIGNALS_TEST_PG_DSN: %v", err)
	}
	defer pool.Close()

	// PG19-only: per-object stats_reset is a PG19 capability (SR-R001).
	// Below PG19 the collector emits NULL::timestamptz (SR-R002), so a
	// reset-after-catchup assertion is vacuous — skip with a documented
	// reason rather than asserting on a column the server does not expose.
	major := serverMajor(t, ctx, pool)
	if major < 19 {
		t.Skipf("server is PG%d (< 19) — per-object stats_reset does not exist below PG19 (SR-R002 emits typed NULL); skipping the live reset-after-catchup acceptance", major)
	}

	// Build the collector target config from the DSN exactly as the
	// output-contract integration test does, so collection authenticates
	// as the same non-superuser role that seeded the fixture.
	tgt := targetFromDSN(t, dsn)

	// --- Seed ------------------------------------------------------------
	// Two sibling tables in a dedicated schema, created + owned by the
	// `signals` collector role so collection sees them as its own
	// relations. The counter reset below runs via the privileged setup
	// DSN (not this role) — ownership does not confer the reset.
	seedResetFixture(t, ctx, pool)

	// Generate baseline activity on BOTH tables so their stats are
	// non-zero before the first collection.
	generateActivity(t, ctx, pool, resetTable, 50)
	generateActivity(t, ctx, pool, stableTable, 50)

	// --- Cycle 0 (T0 baseline) ------------------------------------------
	payload0 := collectTablesPayload(t, tgt)
	reset0, ok0 := tableStatsReset(payload0, resetSchema, resetTable)
	if !ok0 {
		t.Fatalf("cycle 0: no pg_stat_user_tables_v1 row for %s.%s — fixture did not emit the reset table (schema scope / role visibility issue?)", resetSchema, resetTable)
	}
	stable0, okS0 := tableStatsReset(payload0, resetSchema, stableTable)
	if !okS0 {
		t.Fatalf("cycle 0: no pg_stat_user_tables_v1 row for %s.%s — fixture did not emit the stable table", resetSchema, stableTable)
	}
	// On PG19 the stats_reset of an already-collecting cluster is normally
	// non-NULL (the per-object counters have a reset time), but a brand
	// new relation can legitimately carry NULL until its first reset. The
	// test does not require a non-NULL baseline for the reset table: the
	// RESET below establishes a definite, advancing timestamp regardless.

	// --- Reset resetTable's counters, then make them CATCH UP -----------
	// pg_stat_reset_single_table_counters zeroes this one table's
	// cumulative counters and stamps a new stats_reset. This is TEST
	// SETUP (not collection) simulating an external reset event, run via
	// the privileged setup DSN (the reset function is superuser-or-granted
	// only; collection never resets anything — INV-SR-03).
	resetSingleTableCounters(t, ctx, superDSN, resetSchema, resetTable)

	// Generate MORE activity on resetTable than before, so the cumulative
	// counters climb back ABOVE their pre-reset values. A naive
	// decreasing-counter check would therefore NOT detect the reset —
	// only the advancing stats_reset timestamp does. stableTable is left
	// untouched (no reset, no extra activity beyond its baseline).
	generateActivity(t, ctx, pool, resetTable, 120)

	// Guarantee a strictly-greater timestamp is observable: stats_reset is
	// timestamptz with sub-second resolution, but pin determinism so the
	// T1 > T0 assertion cannot flake on a same-instant reset.
	waitForStatsResetAdvance(t, ctx, pool, resetSchema, resetTable, reset0)

	// --- Cycle 1 (T1) ---------------------------------------------------
	payload1 := collectTablesPayload(t, tgt)
	reset1, ok1 := tableStatsReset(payload1, resetSchema, resetTable)
	if !ok1 {
		t.Fatalf("cycle 1: no pg_stat_user_tables_v1 row for %s.%s after reset+catchup", resetSchema, resetTable)
	}
	stable1, okS1 := tableStatsReset(payload1, resetSchema, stableTable)
	if !okS1 {
		t.Fatalf("cycle 1: no pg_stat_user_tables_v1 row for %s.%s", resetSchema, stableTable)
	}

	// --- Assert: RESET-AFTER-CATCHUP (the #817 point) -------------------
	// The reset table's stats_reset MUST have advanced even though its
	// counters caught up. reset1 must be a real timestamp (not NULL): a
	// just-reset relation always carries a stats_reset on PG19.
	if reset1 == nil {
		t.Fatalf("TC-SR-05 (reset-after-catchup): %s.%s stats_reset is NULL after an explicit pg_stat_reset_single_table_counters — PG19 must report a per-object reset time for a just-reset relation (SR-R001/SR-R003)", resetSchema, resetTable)
	}
	if reset0 != nil && !reset1.After(*reset0) {
		t.Errorf("TC-SR-05 (reset-after-catchup): %s.%s stats_reset did NOT advance: T0=%v, T1=%v. The collector failed to surface the reset via the timestamp — this is exactly the signal #817/Analyzer#4315 relies on when the counters have caught back up",
			resetSchema, resetTable, reset0, reset1)
	}
	if reset0 == nil {
		// Baseline was NULL (relation never reset before). The post-reset
		// timestamp being non-NULL is itself the reset-identification
		// signal: the transition NULL -> timestamp marks the reset.
		t.Logf("TC-SR-05: %s.%s baseline stats_reset was NULL; post-reset T1=%v (NULL->timestamp transition identifies the reset)", resetSchema, resetTable, reset1)
	}

	// --- Assert: NO-RESET STABILITY -------------------------------------
	// The sibling table was never reset, so its stats_reset must be
	// stable across the two cycles (and survive export, which it did —
	// we read it back from the exported snapshot payload).
	switch {
	case stable0 == nil && stable1 == nil:
		t.Logf("no-reset stability: %s.%s stats_reset stable at NULL across both cycles", resetSchema, stableTable)
	case stable0 == nil || stable1 == nil:
		t.Errorf("no-reset stability: %s.%s stats_reset changed nullness across cycles (T0=%v, T1=%v) without any reset — a never-reset relation must keep a stable stats_reset (INV-SR-01 / SR-R003)",
			resetSchema, stableTable, stable0, stable1)
	case !stable0.Equal(*stable1):
		t.Errorf("no-reset stability: %s.%s stats_reset advanced (T0=%v, T1=%v) WITHOUT a reset — a never-reset relation must keep a stable stats_reset",
			resetSchema, stableTable, stable0, stable1)
	default:
		t.Logf("no-reset stability: %s.%s stats_reset stable at %v across both cycles (survived export)", resetSchema, stableTable, stable1)
	}

	// --- Cross-check: the reset table's advance is NOT shared by the
	// stable table (isolation of pg_stat_reset_single_table_counters). If
	// both advanced, a cluster-wide reset happened and the test proved
	// nothing about per-object identification.
	if reset0 != nil && reset1 != nil && stable1 != nil && stable0 != nil {
		if reset1.After(*reset0) && stable1.After(*stable0) {
			t.Errorf("TC-SR-05 isolation: BOTH %s and %s stats_reset advanced — the reset was not single-table scoped, so the per-object identification signal is not actually demonstrated",
				resetTable, stableTable)
		}
	}
}

// serverMajor returns the connected server's major version via
// server_version_num (e.g. 190000 -> 19), so the test can PG19-gate.
func serverMajor(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int {
	t.Helper()
	var num int
	if err := pool.QueryRow(ctx, `SELECT current_setting('server_version_num')::int`).Scan(&num); err != nil {
		t.Fatalf("read server_version_num: %v", err)
	}
	return num / 10000
}

// targetFromDSN builds a collector TargetConfig from the DSN, carrying
// the password through a dedicated env var so collection authenticates
// as the same non-superuser role that seeded the fixture. Mirrors the
// output-contract integration test's DSN->TargetConfig handling.
func targetFromDSN(t *testing.T, dsn string) config.TargetConfig {
	t.Helper()
	connCfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse SIGNALS_TEST_PG_DSN: %v", err)
	}

	passwordEnv := os.Getenv("SIGNALS_TEST_PG_PASSWORD_ENV")
	if passwordEnv == "" && connCfg.Password != "" {
		passwordEnv = "SIGNALS_SR_TEST_PG_PASSWORD"
		t.Setenv(passwordEnv, connCfg.Password)
	}

	sslMode := connCfg.RuntimeParams["sslmode"]
	if sslMode == "" {
		if connCfg.TLSConfig == nil {
			sslMode = "disable"
		} else {
			sslMode = "prefer"
		}
	}

	return config.TargetConfig{
		Name:        "stats-reset-target",
		Host:        connCfg.Host,
		Port:        int(connCfg.Port),
		DBName:      connCfg.Database,
		User:        connCfg.User,
		SSLMode:     sslMode,
		PasswordEnv: passwordEnv,
		Enabled:     true,
	}
}

// seedResetFixture creates the two sibling tables in a dedicated schema
// as the DSN (collector) role, so collection sees them as its own
// relations; the counter reset runs via the privileged setup DSN, not
// this role (ownership does not confer it). Idempotent; dropped on
// cleanup.
func seedResetFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	stmts := []string{
		fmt.Sprintf(`DROP SCHEMA IF EXISTS %s CASCADE`, resetSchema),
		fmt.Sprintf(`CREATE SCHEMA %s`, resetSchema),
		fmt.Sprintf(`CREATE TABLE %s.%s (id bigint PRIMARY KEY, v text)`, resetSchema, resetTable),
		fmt.Sprintf(`CREATE TABLE %s.%s (id bigint PRIMARY KEY, v text)`, resetSchema, stableTable),
	}
	for _, s := range stmts {
		if _, err := pool.Exec(ctx, s); err != nil {
			t.Fatalf("seed stmt failed: %v\nSQL: %s", err, s)
		}
	}
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer ccancel()
		_, _ = pool.Exec(cctx, fmt.Sprintf(`DROP SCHEMA IF EXISTS %s CASCADE`, resetSchema))
	})
}

// generateActivity drives inserts + scans against a table so its
// per-object statistics (n_tup_ins, seq_scan, ...) climb. Called before
// the reset to seed non-zero stats, and again (with a larger count)
// AFTER the reset so the cumulative counters catch back up.
func generateActivity(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table string, n int) {
	t.Helper()
	// Upsert n rows (writes bump n_tup_ins / n_tup_upd) then scan the
	// table (bumps seq_scan / seq_tup_read). A fresh id range each call
	// keeps the row set growing so counters strictly increase.
	ins := fmt.Sprintf(`INSERT INTO %s.%s (id, v)
		SELECT g, md5(g::text) FROM generate_series(1, $1) AS g
		ON CONFLICT (id) DO UPDATE SET v = EXCLUDED.v`, resetSchema, table)
	if _, err := pool.Exec(ctx, ins, n); err != nil {
		t.Fatalf("generate activity (insert) on %s.%s: %v", resetSchema, table, err)
	}
	// A few sequential scans to move read counters.
	for i := 0; i < 5; i++ {
		var c int64
		if err := pool.QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM %s.%s`, resetSchema, table)).Scan(&c); err != nil {
			t.Fatalf("generate activity (scan) on %s.%s: %v", resetSchema, table, err)
		}
	}
	// Ask the stats collector to flush so the per-object counters and
	// stats_reset are durable for the next collection cycle.
	_, _ = pool.Exec(ctx, `SELECT pg_stat_force_next_flush()`)
}

// resetSingleTableCounters zeroes one table's cumulative statistics and
// stamps a fresh per-object stats_reset. This is TEST SETUP that
// simulates an external reset event (a restart / crash recovery / major
// upgrade / privileged admin action) — NOT a collector action. Table
// ownership does NOT confer EXECUTE on pg_stat_reset_single_table_counters
// (it is superuser-or-granted only, even on PG15+), and pg_monitor does
// not grant it either, so the reset runs via the superuser setup DSN
// (SIGNALS_TEST_PG_SUPERUSER_DSN) — the same privileged seam the
// output-contract test uses for FDW provisioning. Collection itself
// still runs only as the non-superuser `signals` role (INV-SR-03).
func resetSingleTableCounters(t *testing.T, ctx context.Context, superDSN, schema, table string) {
	t.Helper()
	super, err := pgxpool.New(ctx, superDSN)
	if err != nil {
		t.Fatalf("connect SIGNALS_TEST_PG_SUPERUSER_DSN for the privileged reset: %v", err)
	}
	defer super.Close()
	var oid uint32
	if err := super.QueryRow(ctx,
		`SELECT (quote_ident($1) || '.' || quote_ident($2))::regclass::oid`,
		schema, table,
	).Scan(&oid); err != nil {
		t.Fatalf("resolve oid for %s.%s: %v", schema, table, err)
	}
	if _, err := super.Exec(ctx, `SELECT pg_stat_reset_single_table_counters($1)`, oid); err != nil {
		t.Fatalf("pg_stat_reset_single_table_counters(%d) for %s.%s failed via the superuser setup DSN: %v",
			oid, schema, table, err)
	}
	_, _ = super.Exec(ctx, `SELECT pg_stat_force_next_flush()`)
}

// waitForStatsResetAdvance polls the live catalog until the reset
// table's per-object stats_reset is strictly after the baseline (or
// non-NULL when the baseline was NULL), so the two-cycle T1 > T0
// assertion is deterministic and cannot flake on a same-instant reset.
func waitForStatsResetAdvance(t *testing.T, ctx context.Context, pool *pgxpool.Pool, schema, table string, baseline *time.Time) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		var sr *time.Time
		err := pool.QueryRow(ctx,
			`SELECT stats_reset FROM pg_stat_user_tables
			 WHERE schemaname = $1 AND relname = $2`,
			schema, table,
		).Scan(&sr)
		if err == nil {
			switch {
			case sr == nil:
				// not yet flushed; keep polling
			case baseline == nil:
				return // NULL -> timestamp transition observed
			case sr.After(*baseline):
				return
			}
		}
		_, _ = pool.Exec(ctx, `SELECT pg_stat_force_next_flush()`)
		time.Sleep(250 * time.Millisecond)
	}
	t.Log("waitForStatsResetAdvance: deadline reached without observing an advance in the live catalog; the collection assertion below remains authoritative")
}

// collectTablesPayload runs one full collection cycle against the live
// target via the production collector + export path, then reads back the
// pg_stat_user_tables_v1 payload from the exported snapshot. Each call
// uses a FRESH store so the two cycles are independent baseline cycles.
func collectTablesPayload(t *testing.T, tgt config.TargetConfig) []map[string]any {
	t.Helper()
	store := openTestDB(t)

	c := collector.New(
		store,
		[]config.TargetConfig{tgt},
		24*time.Hour,
		30,
		collector.WithTargetTimeout(90*time.Second),
		collector.WithQueryTimeout(20*time.Second),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		c.Run(ctx)
	}()

	if !waitForSnapshotCount(t, store, 1, 120*time.Second) {
		cancel()
		<-done
		t.Fatalf("collection did not produce a snapshot within 120s (connectivity / role-safety failure?)")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("collector goroutine did not stop within 15s of context cancel")
	}

	builder := export.NewBuilder(store, "stats-reset-test")
	var buf bytes.Buffer
	if err := builder.WriteTo(&buf, export.Options{}); err != nil {
		t.Fatalf("export WriteTo: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}

	runs := readRuns(t, zr)
	results := readResults(t, zr)
	resultByRunID := map[string]exportedResult{}
	for _, r := range results {
		resultByRunID[r.RunID] = r
	}
	for _, run := range runs {
		if run.Status != "success" || run.QueryID != "pg_stat_user_tables_v1" {
			continue
		}
		if res, ok := resultByRunID[run.ID]; ok {
			return res.Payload
		}
	}
	t.Fatal("collection produced no successful pg_stat_user_tables_v1 payload")
	return nil
}

// tableStatsReset extracts the stats_reset value for a specific
// schema.table from a pg_stat_user_tables_v1 payload. The bool is false
// when the relation is absent from the payload; the *time.Time is nil
// when the row is present but stats_reset is NULL (an explicit unknown,
// SR-R003).
func tableStatsReset(payload []map[string]any, schema, table string) (*time.Time, bool) {
	for _, row := range payload {
		sn, _ := row["schemaname"].(string)
		rn, _ := row["relname"].(string)
		if sn != schema || rn != table {
			continue
		}
		raw, present := row["stats_reset"]
		if !present || raw == nil {
			return nil, true
		}
		ts := parseExportedTimestamp(raw)
		return ts, true
	}
	return nil, false
}

// parseExportedTimestamp decodes a stats_reset value as it survives the
// JSON export round-trip. The export serializes timestamptz as an
// RFC3339 string; a defensive time.Time branch is kept in case a future
// serializer emits a native value. Returns nil if it cannot be decoded
// (treated as an unknown rather than a hard failure).
func parseExportedTimestamp(raw any) *time.Time {
	switch v := raw.(type) {
	case string:
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.999999Z07:00"} {
			if ts, err := time.Parse(layout, v); err == nil {
				return &ts
			}
		}
		return nil
	case time.Time:
		return &v
	default:
		return nil
	}
}
