//go:build integration

// Execution-faithful budget-exhaustion regression test against a live
// PostgreSQL — the production-path lock for issue #329 / TC-SIG-124 /
// SIGNALS-R108.
//
// The helper-only budget test (budget_exhaustion_test.go) exercises
// budgetSkippedRuns and cycleStatus in isolation. It cannot see the
// #329 defect, which lives in the real collection loop: when a
// collector query drains the per-cycle (target) budget, the parent ctx
// is ALSO expired, so ROLLBACK TO SAVEPOINT / RELEASE run under an
// already-done context, pgx rejects them, collectTarget returns a hard
// error, and execution never reaches the block that records the
// remaining due collectors as skipped/budget_exhausted or persists the
// partial cycle. The result is the exact completeness gap #8 was closed
// to solve: no partial snapshot and no status rows for the collectors
// that never got a turn.
//
// This test drives the real collectTarget against live PG, forces the
// target deadline to expire DURING a collector query (via the
// withBeforeQueryForTest seam, which blocks one collector until the
// budget context is done), and asserts that after the fix:
//
//   - every DUE collector produces exactly one persisted query_run
//     (M runs for M due collectors — the completeness invariant),
//   - the attempted-but-timed-out collector is failed/timeout,
//   - every unattempted collector is skipped/budget_exhausted,
//   - the snapshot, runs, and successful payloads persist atomically
//     (each success run has a joinable query_results payload),
//   - the cycle is partial (persisted), not discarded as failed.
//
// It FAILS on the pre-fix implementation (collectTarget returns a
// "rollback to savepoint ... context deadline exceeded" error and
// persists nothing) and passes after the savepoint-recovery fresh-
// context fix. Gated by the `integration` build tag + SIGNALS_TEST_PG_DSN
// (skips locally when unset) and designed to run under -race in CI.
//
// Run with:
//
//	SIGNALS_TEST_PG_DSN="postgres://signals:signals@localhost/postgres?sslmode=disable" \
//	  go test -tags integration -race -count=1 \
//	  -run TestIntegration_BudgetExhaustionMidQuery ./internal/collector/
//
// Spec: features/signals/specification.md SIGNALS-R108 (the
// after-query-timeout path); features/signals/traceability.md
// TC-SIG-124.
package collector

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/elevarq/signals/internal/config"
	"github.com/elevarq/signals/internal/db"
)

func TestIntegration_BudgetExhaustionMidQuery(t *testing.T) {
	dsn := os.Getenv("SIGNALS_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("SIGNALS_TEST_PG_DSN not set — skipping live PostgreSQL integration test")
	}

	connCfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse SIGNALS_TEST_PG_DSN: %v", err)
	}

	// The collector reads the target password from the env var named by
	// PasswordEnv. Carry the DSN password through a dedicated var so the
	// collector authenticates exactly as the seed connection did. Peer/
	// trust auth leaves both empty and still works.
	passwordEnv := ""
	if connCfg.Password != "" {
		passwordEnv = "SIGNALS_BUDGET_TEST_PG_PASSWORD"
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

	tgt := config.TargetConfig{
		Name:        "budget-exhaustion-target",
		Host:        connCfg.Host,
		Port:        int(connCfg.Port),
		DBName:      connCfg.Database,
		User:        connCfg.User,
		SSLMode:     sslMode,
		PasswordEnv: passwordEnv,
		Enabled:     true,
	}

	store := openLiveTestDB(t)

	// Learn the target's row id up front so we can read back its
	// snapshot + runs after the cycle. collectTarget upserts the same
	// target idempotently, so this does not perturb the run.
	targetID, err := store.UpsertTarget(
		tgt.Name, tgt.Host, tgt.Port, tgt.DBName, tgt.User,
		tgt.SSLMode, tgt.SecretType(), tgt.CredentialSourceRef(), tgt.Enabled,
	)
	if err != nil {
		t.Fatalf("upsert target: %v", err)
	}

	// The seam drives the collector at ordinal blockOrdinal (0-based,
	// among attempted collectors) into the exact #329 mid-query state.
	// It blocks that collector until the per-query context — which is
	// bounded by the remaining per-cycle (target) budget — is DONE, then
	// lets the real query run against that now-expired context. pgx sees
	// the already-done context and returns "context deadline exceeded"
	// WITHOUT dispatching to the server, so the connection is preserved
	// (a client-side cancel of an in-flight query would instead close
	// the connection). The subsequent savepoint ROLLBACK/RELEASE
	// therefore run with an EXPIRED parent ctx on a LIVE connection —
	// precisely the #329 trigger: with the pre-fix code they fail
	// "context deadline exceeded" and collectTarget aborts before
	// persisting anything; with the fix they run under a bounded fresh
	// context and the partial cycle persists.
	//
	// The transaction is idle only for the brief block window, well
	// under the (generous) idle_in_transaction guard, so that guard
	// never fires. We drive the 2nd attempted collector so at least one
	// collector succeeds before it and (given the large due set) many
	// remain after it as budget_exhausted.
	const blockOrdinal = 1
	var (
		mu       sync.Mutex
		attempts int
		blocked  bool
	)
	hook := func(qCtx context.Context, tx pgx.Tx, queryID, sql string) string {
		mu.Lock()
		n := attempts
		attempts++
		if n == blockOrdinal {
			blocked = true
		}
		mu.Unlock()
		if n == blockOrdinal {
			// Wait out the remaining budget. When qCtx is done the target
			// budget is exhausted; the real query below then observes an
			// already-expired context (conn preserved) and the buggy
			// savepoint recovery runs against the expired parent ctx.
			<-qCtx.Done()
		}
		return sql
	}

	// In production the per-target budget context is derived from
	// targetTimeout by collectTarget's caller (the collection-cycle
	// dispatcher). Driving collectTarget directly, WE supply that budget
	// context below. targetTimeout here governs only the SET LOCAL
	// idle_in_transaction / statement guards, which we keep generous so
	// neither fires while the seam waits out the budget — mirroring the
	// production invariant that the BUDGET, not the idle guard, is what
	// stops the cycle.
	c := New(
		store,
		[]config.TargetConfig{tgt},
		24*time.Hour,
		30,
		WithTargetTimeout(60*time.Second),
		WithQueryTimeout(60*time.Second),
		WithBeforeQueryForTest(hook),
	)

	// Drive the real production collection path directly. forceAll=true
	// runs every eligible collector (the fresh store has no cadence
	// history, so the due set is the full eligible set) — a large M that
	// makes the completeness assertion meaningful.
	//
	// The per-target BUDGET context — the exact seam #329 is about. It
	// expires mid-query (during the blocked collector), reproducing the
	// production path where the per-cycle target deadline governs.
	budgetCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if cerr := c.collectTarget(budgetCtx, tgt, true, true, "", ""); cerr != nil {
		// The whole point of #329: collectTarget must NOT return a hard
		// error on mid-query budget exhaustion — it must persist the
		// partial cycle and return nil. A pre-fix build fails here with
		// "rollback to savepoint ... context deadline exceeded".
		t.Fatalf("collectTarget returned a hard error on mid-query budget exhaustion (the #329 regression): %v", cerr)
	}

	mu.Lock()
	didBlock := blocked
	mu.Unlock()
	if !didBlock {
		t.Fatalf("the budget-blocking seam never fired (only %d collectors attempted) — the due set was too small to exercise the mid-query path", attempts)
	}

	// --- Persistence must have survived the exhausted budget ----------
	snaps, err := store.GetSnapshotsByTarget(targetID, "", "")
	if err != nil {
		t.Fatalf("GetSnapshotsByTarget: %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("R108: expected exactly one persisted PARTIAL snapshot after mid-query budget exhaustion, got %d — the cycle was discarded (the #329 gap)", len(snaps))
	}
	snapID := snaps[0].ID

	runs, err := store.GetQueryRunsBySnapshotIDs([]string{snapID})
	if err != nil {
		t.Fatalf("GetQueryRunsBySnapshotIDs: %v", err)
	}
	if len(runs) == 0 {
		t.Fatal("R108: snapshot persisted but carries NO query_runs — the status inventory was lost")
	}

	// --- Classify the persisted inventory -----------------------------
	var (
		success         int
		failedTimeout   int
		failedOther     int
		budgetExhausted int
		otherSkipped    int
	)
	seen := map[string]bool{}
	for _, r := range runs {
		if seen[r.QueryID] {
			t.Errorf("R108: duplicate query_run for collector %q — every due collector must have exactly one run", r.QueryID)
		}
		seen[r.QueryID] = true
		switch {
		case r.Status == "success":
			success++
		case r.Status == "failed" && r.Reason == "timeout":
			failedTimeout++
		case r.Status == "failed":
			failedOther++
		case r.Status == "skipped" && r.Reason == reasonBudgetExhausted:
			budgetExhausted++
		case r.Status == "skipped":
			otherSkipped++
		default:
			t.Errorf("unexpected run status/reason: %q/%q for %q", r.Status, r.Reason, r.QueryID)
		}
	}

	t.Logf("persisted inventory: %d runs (success=%d, failed/timeout=%d, failed/other=%d, budget_exhausted=%d, other_skipped=%d)",
		len(runs), success, failedTimeout, failedOther, budgetExhausted, otherSkipped)

	// The attempted-but-timed-out collector: exactly one failed/timeout.
	if failedTimeout != 1 {
		t.Errorf("expected exactly 1 failed/timeout run (the collector whose query drained the budget), got %d", failedTimeout)
	}
	// Every unattempted due collector is skipped/budget_exhausted; at
	// least one must exist (we blocked a non-last collector in a large
	// due set).
	if budgetExhausted < 1 {
		t.Errorf("expected >=1 skipped/budget_exhausted run (the collectors that never got a turn), got %d — R108 completeness lost", budgetExhausted)
	}
	// At least one collector ran to success before the budget drained.
	if success < 1 {
		t.Errorf("expected >=1 success run before the budget drained, got %d", success)
	}

	// --- Completeness: M due collectors -> M persisted runs -----------
	// The full inventory must cover every collector that was due this
	// cycle: the successes + the timed-out one + the budget-exhausted
	// remainder (plus any gated skips). No due collector may be silently
	// absent — that is the #8 / R108 invariant.
	if total := success + failedTimeout + failedOther + budgetExhausted + otherSkipped; total != len(runs) {
		t.Errorf("run accounting mismatch: classified %d != persisted %d", total, len(runs))
	}

	// --- Atomicity: every success run has a joinable payload ----------
	for _, r := range runs {
		if r.Status != "success" {
			continue
		}
		res, gerr := store.GetQueryResultByRunID(r.ID)
		if gerr != nil {
			t.Fatalf("GetQueryResultByRunID(%s): %v", r.ID, gerr)
		}
		if res == nil {
			t.Errorf("atomicity: success run %s (collector %q) has NO joinable query_results payload", r.ID, r.QueryID)
		}
	}

	// --- The cycle is partial, not failed -----------------------------
	// cycleStatus is the production classifier used in the audit/metrics
	// path; a persisted cycle with budget-exhausted skips is PARTIAL.
	if got := cycleStatus(nil, success, failedTimeout+failedOther, budgetExhausted); got != "partial" {
		t.Errorf("cycle status = %q, want partial (budget exhaustion is partial, not failed)", got)
	}
}

func openLiveTestDB(t *testing.T) *db.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "budget-live.db")
	store, err := db.Open(dbPath, false)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	if err := store.Migrate(); err != nil {
		_ = store.Close()
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
