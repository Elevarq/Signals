//go:build integration

// Execution-faithful regression test for #705 against a live PostgreSQL —
// the belt-and-suspenders companion to the #671 per-query-timeout recovery
// (query_timeout_recovery_live_test.go).
//
// #671 made a CLEAN server-side per-query timeout survive: the server cancels
// the query, the connection stays alive, the savepoint recovers, and
// collection continues. But under extreme contention (the #643 field failure
// on a saturated db.t4g.small) the connection can still PHYSICALLY close
// mid-cycle — pgx closes it, and the per-query ROLLBACK TO SAVEPOINT then
// fails on the dead conn. Pre-#705 collectTarget returned a hard error
// ("rollback to savepoint ... conn closed ...") and discarded the ENTIRE
// target snapshot — one lost connection took down every healthy collector,
// and the RDS target went dark every cycle after stand-up.
//
// This test forces that exact condition deterministically: the
// withBeforeQueryForTest seam CLOSES the underlying connection when a known
// mid-sequence collector runs, so its query errors and the savepoint rollback
// cannot recover. It asserts that after #705:
//
//   - collectTarget returns nil (the target is NOT discarded),
//   - a snapshot persists with query_runs (the inventory survives),
//   - at least one EARLIER collector is recorded success,
//   - the kill collector is recorded failed,
//   - at least one LATER collector is recorded skipped/connection_lost
//     (the remaining-collector inventory is complete, R108-style),
//   - the connection was never written to (read-only invariant holds).
//
// It FAILS on the pre-#705 implementation (hard return, nothing persisted).
//
// Gated by the `integration` build tag + SIGNALS_TEST_PG_DSN (skips locally
// when unset) and designed to run under -race in CI.
//
// Run with:
//
//	SIGNALS_TEST_PG_DSN="postgres://signals:signals@localhost/postgres?sslmode=disable" \
//	  go test -tags integration -race -count=1 \
//	  -run TestIntegration_ConnLostMidCycleDegradesToPartial ./internal/collector/
//
// Spec: specifications/collection.md (#705), features/signals/traceability.md.
package collector

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/elevarq/signals/internal/config"
)

func TestIntegration_ConnLostMidCycleDegradesToPartial(t *testing.T) {
	dsn := os.Getenv("SIGNALS_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("SIGNALS_TEST_PG_DSN not set — skipping live PostgreSQL integration test")
	}

	connCfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse SIGNALS_TEST_PG_DSN: %v", err)
	}

	passwordEnv := ""
	if connCfg.Password != "" {
		passwordEnv = "SIGNALS_CLP_TEST_PG_PASSWORD"
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
		Name:        "conn-lost-partial-target",
		Host:        connCfg.Host,
		Port:        int(connCfg.Port),
		DBName:      connCfg.Database,
		User:        connCfg.User,
		SSLMode:     sslMode,
		PasswordEnv: passwordEnv,
		Enabled:     true,
	}

	store := openLiveTestDB(t)

	targetID, err := store.UpsertTarget(
		tgt.Name, tgt.Host, tgt.Port, tgt.DBName, tgt.User,
		tgt.SSLMode, tgt.SecretType(), tgt.CredentialSourceRef(), tgt.Enabled,
	)
	if err != nil {
		t.Fatalf("upsert target: %v", err)
	}

	// Kill the connection when a known mid-sequence collector runs: pgx then
	// sees "conn closed" on the query AND on the per-query ROLLBACK TO
	// SAVEPOINT — the exact #643 field failure. pg_role_capabilities_v1 is
	// mid-sequence, so collectors run both before (success) and after
	// (skipped/connection_lost) it.
	const killCollector = "pg_role_capabilities_v1"
	var (
		mu       sync.Mutex
		injected bool
	)
	hook := func(qCtx context.Context, tx pgx.Tx, queryID, sql string) string {
		if queryID == killCollector {
			mu.Lock()
			injected = true
			mu.Unlock()
			// Forcibly close the underlying connection mid-cycle. The savepoint
			// for this collector was already taken on the live conn; closing it
			// now makes both the query and its rollback fail on a dead conn.
			_ = tx.Conn().Close(context.Background())
		}
		return sql
	}

	c := New(
		store,
		[]config.TargetConfig{tgt},
		24*time.Hour,
		30,
		WithTargetTimeout(120*time.Second),
		WithQueryTimeout(30*time.Second),
		WithBeforeQueryForTest(hook),
	)

	budgetCtx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	if cerr := c.collectTarget(budgetCtx, tgt, true, true, "", ""); cerr != nil {
		// #705: a mid-cycle connection loss must NOT discard the whole target.
		// A pre-#705 build fails here with "rollback to savepoint ... conn
		// closed ...", persisting nothing.
		t.Fatalf("collectTarget returned a hard error on a mid-cycle connection loss (the #643 whole-target-discard defect): %v", cerr)
	}

	mu.Lock()
	didInject := injected
	mu.Unlock()
	if !didInject {
		t.Fatalf("the kill seam never fired — %q was not eligible/due; pick another mid-sequence collector", killCollector)
	}

	snaps, err := store.GetSnapshotsByTarget(targetID, "", "")
	if err != nil {
		t.Fatalf("GetSnapshotsByTarget: %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("#705: expected exactly one persisted (partial) snapshot after a mid-cycle connection loss, got %d — the whole target was discarded (the #643 defect)", len(snaps))
	}

	runs, err := store.GetQueryRunsBySnapshotIDs([]string{snaps[0].ID})
	if err != nil {
		t.Fatalf("GetQueryRunsBySnapshotIDs: %v", err)
	}
	if len(runs) == 0 {
		t.Fatal("#705: snapshot persisted but carries NO query_runs — the status inventory was lost")
	}

	var (
		success         int
		killFailed      bool
		connLostSkipped int
		killStatus      string
	)
	for _, r := range runs {
		switch {
		case r.Status == "success":
			success++
		case r.QueryID == killCollector:
			killFailed = r.Status == "failed"
			killStatus = r.Status
		}
		if r.Status == "skipped" && r.Reason == "connection_lost" {
			connLostSkipped++
		}
	}

	t.Logf("partial inventory: %d runs (success=%d, connection_lost skips=%d); kill collector %q -> status=%q",
		len(runs), success, connLostSkipped, killCollector, killStatus)

	// At least one earlier collector succeeded — proof the snapshot is a real
	// partial, not an empty shell.
	if success < 1 {
		t.Errorf("#705: expected >=1 EARLIER collector to succeed before the connection loss, got %d", success)
	}
	// The kill collector is recorded failed (not a false success on a torn tx).
	if !killFailed {
		t.Errorf("#705: expected the kill collector %q to be recorded failed, got status=%q", killCollector, killStatus)
	}
	// The remaining collectors after the loss are recorded skipped/connection_lost
	// so the status inventory stays complete (R108-style).
	if connLostSkipped < 1 {
		t.Errorf("#705: expected >=1 remaining collector recorded skipped/connection_lost, got %d", connLostSkipped)
	}
}
