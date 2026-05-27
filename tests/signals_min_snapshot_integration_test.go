//go:build integration

// Integration smoke test for R091 / R092 — promotes the manually-verified
// min-snapshot-interval check (issue #44) into automated coverage against
// a real PostgreSQL instance.
//
// Companion to the comprehensive unit-level coverage in
// signals_min_snapshot_interval_test.go (TC-SIG-110..117). The unit tests
// exercise the decision function and SQLite skip-leaves-no-rows invariant
// against a mocked store. This test exercises the full Run() orchestration
// against a real PG so that orchestration-layer regressions surface in CI
// (or local runs) before they reach production.
//
// Gated by the `integration` build tag and the ARQ_TEST_PG_DSN env var,
// matching the pattern in signals_integration_test.go.
//
// Run with:
//
//	ARQ_TEST_PG_DSN="postgres://arq_monitor@localhost/postgres" \
//	  go test -tags integration ./tests/ -run TestIntegration_MinSnapshotInterval
//
// Optional: ARQ_TEST_PG_PASSWORD_ENV=<env-var-name> if the test role
// requires password auth (peer/trust auth otherwise).

package tests

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/elevarq/arq-signals/internal/collector"
	"github.com/elevarq/arq-signals/internal/config"
	"github.com/elevarq/arq-signals/internal/db"
)

func TestIntegration_MinSnapshotIntervalAgainstRealPG(t *testing.T) {
	dsn := os.Getenv("ARQ_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("ARQ_TEST_PG_DSN not set — skipping live PostgreSQL integration test")
	}

	connCfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse ARQ_TEST_PG_DSN: %v", err)
	}

	tgt := config.TargetConfig{
		Name:        "integration-target",
		Host:        connCfg.Host,
		Port:        int(connCfg.Port),
		DBName:      connCfg.Database,
		User:        connCfg.User,
		SSLMode:     "prefer",
		PasswordEnv: os.Getenv("ARQ_TEST_PG_PASSWORD_ENV"),
		Enabled:     true,
	}

	store := openTestDB(t)

	// Long interval so the ticker never fires during the test; cycles
	// only happen via the initial baseline and via CollectNow().
	// min_snapshot_interval=1h is long enough that two consecutive
	// CollectNow calls are unambiguously inside the window.
	c := collector.New(
		store,
		[]config.TargetConfig{tgt},
		24*time.Hour,
		30,
		collector.WithMinSnapshotInterval(1*time.Hour),
		collector.WithTargetTimeout(45*time.Second),
		collector.WithQueryTimeout(10*time.Second),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		c.Run(ctx)
	}()

	// Wait for the initial baseline cycle (Run kicks one off
	// immediately) to land a snapshot row. Poll the store rather
	// than sleeping for a fixed duration — collection time varies
	// by PG load and the catalog size.
	if !waitForSnapshotCount(t, store, 1, 60*time.Second) {
		cancel()
		<-done
		t.Fatalf("initial cycle did not produce a snapshot within 60s; got count=%d — connectivity / role-safety failure?", countRows(t, store, "snapshots"))
	}

	// Second CollectNow within the R091 window (force=false). Should
	// be skipped — no new snapshot row.
	if !c.CollectNow(collector.CollectRequest{
		Targets:   []string{tgt.Name},
		RequestID: "integration-skip-test",
		Actor:     "integration_test",
		Force:     false,
	}) {
		t.Fatal("CollectNow returned false — buffer full unexpectedly")
	}
	// Give the cycle a moment to settle. If R091 were broken the
	// skip path would still write a snapshot; if it's working there
	// will be no row change.
	time.Sleep(3 * time.Second)
	if got := countRows(t, store, "snapshots"); got != 1 {
		cancel()
		<-done
		t.Fatalf("within-window cycle should have been skipped (R091); snapshots count = %d, want 1", got)
	}

	// Third CollectNow with force=true (R092). Should bypass R091
	// and produce a second snapshot.
	if !c.CollectNow(collector.CollectRequest{
		Targets:   []string{tgt.Name},
		RequestID: "integration-force-test",
		Actor:     "integration_test",
		Force:     true,
	}) {
		t.Fatal("forced CollectNow returned false — buffer full unexpectedly")
	}
	if !waitForSnapshotCount(t, store, 2, 60*time.Second) {
		cancel()
		<-done
		t.Fatalf("forced cycle (R092) did not produce a second snapshot within 60s; snapshots count = %d", countRows(t, store, "snapshots"))
	}

	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("collector goroutine did not stop within 10s of context cancel")
	}
}

// waitForSnapshotCount polls the snapshots table until row count >= want
// or the deadline elapses. Returns true on hit, false on timeout.
func waitForSnapshotCount(t *testing.T, store *db.DB, want int, deadline time.Duration) bool {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if countRows(t, store, "snapshots") >= want {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}
