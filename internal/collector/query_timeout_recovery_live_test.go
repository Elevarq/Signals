//go:build integration

// Execution-faithful regression test for SIGNALS-R128 / Elevarq/Signals#671
// against a live PostgreSQL — the production-path lock for the
// whole-target-destroying per-query-timeout defect.
//
// The defect: the server-side statement_timeout was set once per
// transaction to query_timeout and ignored each collector's own
// (smaller) Timeout, while the CLIENT per-query context honoured it. So
// for every collector with Timeout < query_timeout the client deadline
// was strictly shorter than the server timeout. When such a collector's
// query ran long, the client context fired FIRST — mid-flight — so pgx
// cancelled the in-flight query client-side and CLOSED the connection.
// The per-query ROLLBACK TO SAVEPOINT that recovers the read transaction
// then failed on the dead connection ("conn closed"), collectTarget
// returned a hard error, and the ENTIRE target snapshot was discarded.
// In the field this silently dropped ALL PostgreSQL data on a busy
// database on the first collector to exceed its budget (signals_q_58).
//
// This test drives the real collectTarget against live PG and forces one
// known small-Timeout collector (pg_role_capabilities_v1, Timeout 5s) to
// exceed its budget via the withBeforeQueryForTest seam (the collector's
// SQL is replaced with a pg_sleep longer than its Timeout). It asserts
// that after the fix:
//
//   - collectTarget returns nil (the target is NOT destroyed by one
//     collector's timeout),
//   - a snapshot persists with query_runs,
//   - the over-budget collector is recorded failed/timeout (R012),
//   - at least one OTHER collector ran to success — proof the connection
//     survived the timeout and collection continued.
//
// It FAILS on the pre-fix implementation (the client context cancels the
// in-flight query, the connection closes, and collectTarget returns a
// "rollback to savepoint ... conn closed ... context deadline exceeded"
// hard error — persisting nothing) and passes once the server-side
// statement_timeout is set per-query and the client context carries a
// grace margin above it (SIGNALS-R128).
//
// Gated by the `integration` build tag + SIGNALS_TEST_PG_DSN (skips
// locally when unset) and designed to run under -race in CI.
//
// Run with:
//
//	SIGNALS_TEST_PG_DSN="postgres://signals:signals@localhost/postgres?sslmode=disable" \
//	  go test -tags integration -race -count=1 \
//	  -run TestIntegration_PerQueryTimeoutRecovers ./internal/collector/
//
// Spec: features/signals/specification.md SIGNALS-R128.
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

func TestIntegration_PerQueryTimeoutRecovers(t *testing.T) {
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
		passwordEnv = "SIGNALS_QTR_TEST_PG_PASSWORD"
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
		Name:        "per-query-timeout-target",
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

	// Drive one known small-Timeout collector (pg_role_capabilities_v1,
	// Timeout 5s) past its budget by replacing its SQL with a pg_sleep
	// longer than that Timeout. With query_timeout (30s) > the collector's
	// Timeout (5s), the per-query budget is 5s: pre-fix the server
	// statement_timeout was the tx-wide 30s and the CLIENT context was 5s,
	// so the client cancelled the in-flight sleep at 5s and closed the
	// connection — destroying the whole target. Post-fix the server
	// statement_timeout is set per-query to 5s and the client context is
	// 5s + grace, so the server cancels the sleep cleanly on a live
	// connection and the target survives.
	const slowCollector = "pg_role_capabilities_v1"
	var (
		mu       sync.Mutex
		injected bool
	)
	hook := func(qCtx context.Context, tx pgx.Tx, queryID, sql string) string {
		if queryID == slowCollector {
			mu.Lock()
			injected = true
			mu.Unlock()
			return "SELECT pg_sleep(12) AS slept"
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

	// A generous per-target budget: this exercises the PER-QUERY timeout
	// path, NOT budget exhaustion (R108). The collector times out on its
	// own 5s Timeout long before the 120s budget is anywhere near spent.
	budgetCtx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	if cerr := c.collectTarget(budgetCtx, tgt, true, true, "", ""); cerr != nil {
		// The whole point of #671: one collector's timeout must NOT return a
		// hard error / discard the target. A pre-fix build fails here with
		// "rollback to savepoint ... conn closed ... context deadline exceeded".
		t.Fatalf("collectTarget returned a hard error on a single per-query timeout (the #671 regression — whole target destroyed): %v", cerr)
	}

	mu.Lock()
	didInject := injected
	mu.Unlock()
	if !didInject {
		t.Fatal("the slow-collector seam never fired — pg_role_capabilities_v1 was not eligible/due; pick another small-Timeout collector")
	}

	// --- The target snapshot must have SURVIVED the collector timeout ---
	snaps, err := store.GetSnapshotsByTarget(targetID, "", "")
	if err != nil {
		t.Fatalf("GetSnapshotsByTarget: %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("SIGNALS-R128: expected exactly one persisted snapshot after a single collector timeout, got %d — the whole target was discarded (the #671 defect)", len(snaps))
	}
	snapID := snaps[0].ID

	runs, err := store.GetQueryRunsBySnapshotIDs([]string{snapID})
	if err != nil {
		t.Fatalf("GetQueryRunsBySnapshotIDs: %v", err)
	}
	if len(runs) == 0 {
		t.Fatal("SIGNALS-R128: snapshot persisted but carries NO query_runs — the status inventory was lost")
	}

	var (
		success      int
		slowTimedOut bool
		slowStatus   string
		slowReason   string
	)
	for _, r := range runs {
		if r.Status == "success" {
			success++
		}
		if r.QueryID == slowCollector {
			slowTimedOut = r.Status == "failed" && r.Reason == "timeout"
			slowStatus = r.Status
			slowReason = r.Reason
		}
	}

	t.Logf("persisted inventory: %d runs (success=%d); slow collector %q -> status=%q reason=%q",
		len(runs), success, slowCollector, slowStatus, slowReason)

	// The over-budget collector is recorded failed/timeout (R012 taxonomy).
	if !slowTimedOut {
		t.Errorf("expected the over-budget collector %q to be failed/timeout, got status=%q reason=%q", slowCollector, slowStatus, slowReason)
	}
	// The connection survived and collection continued: other collectors ran.
	if success < 1 {
		t.Errorf("expected >=1 OTHER collector to succeed after the timeout (proof the connection survived), got %d successes", success)
	}
}
