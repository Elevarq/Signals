package collector

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/elevarq/signals/internal/config"
	"github.com/elevarq/signals/internal/singlestore"
)

// Tests derive from specifications/singlestore-connection-hardening.acceptance.md.

// TC-SCH-06 (SCH-R004, SCH-R007): the bounded runner cancels a blocking query at
// the configured query timeout, and the error classifies as "timeout".
func TestBoundedSingleStoreRunner_CancelsAtQueryTimeout(t *testing.T) {
	blocking := func(ctx context.Context, _ string) ([]map[string]any, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	runner := boundedSingleStoreRunner(blocking, 10*time.Millisecond)

	start := time.Now()
	_, err := runner(context.Background(), "SELECT 1")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected a timeout error, got nil")
	}
	if elapsed > time.Second {
		t.Errorf("bounded runner took %v, want ~10ms", elapsed)
	}
	if got := classifyRunError(err.Error()); got != "timeout" {
		t.Errorf("classifyRunError(%q) = %q, want timeout", err.Error(), got)
	}
}

// TC-SCH-07 (SCH-R004): a timed-out collector does not abort the cycle — later
// collectors still run.
func TestBoundedSingleStoreRunner_LaterCollectorsRun(t *testing.T) {
	base := func(ctx context.Context, sql string) ([]map[string]any, error) {
		if sql == "SLOW" {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return []map[string]any{{"ok": 1}}, nil
	}
	runner := boundedSingleStoreRunner(base, 10*time.Millisecond)
	cat := []singlestore.Collector{
		{ID: "slow_v1", Category: "x", SQL: "SLOW"},
		{ID: "fast_v1", Category: "x", SQL: "FAST"},
	}
	results := singlestore.Collect(context.Background(), runner, cat, false, false)
	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}
	if results[0].Err == "" {
		t.Error("the slow collector must capture a timeout error")
	}
	if results[1].Err != "" || len(results[1].Rows) != 1 {
		t.Errorf("the fast collector must still run: %+v", results[1])
	}
}

// TC-SCH-05 (SCH-R001, SCH-R002): singleStoreConnConfig maps sslmode +
// sslrootcert_file onto the wire TLS fields and carries the query timeout.
func TestSingleStoreConnConfig_TLSAndTimeoutMapping(t *testing.T) {
	ca := "/etc/signals/ca.pem"
	cases := []struct {
		sslmode        string
		wantTLS        string
		wantCAFile     string
		wantVerifyHost bool
	}{
		{"disable", "false", "", false},
		{"require", "true", "", false},
		{"verify-ca", "true", ca, false},
		{"verify-full", "true", ca, true},
		{"", "preferred", "", false},
	}
	for _, c := range cases {
		tgt := config.TargetConfig{
			Host: "db.example", Port: 3306, User: "u", DBName: "d",
			SSLMode: c.sslmode, SSLRootCertFile: ca,
		}
		cc := singleStoreConnConfig(tgt, "pw", 7*time.Second)
		if cc.TLS != c.wantTLS {
			t.Errorf("sslmode=%q TLS=%q, want %q", c.sslmode, cc.TLS, c.wantTLS)
		}
		if cc.TLSRootCAFile != c.wantCAFile {
			t.Errorf("sslmode=%q TLSRootCAFile=%q, want %q", c.sslmode, cc.TLSRootCAFile, c.wantCAFile)
		}
		if cc.TLSVerifyHostname != c.wantVerifyHost {
			t.Errorf("sslmode=%q TLSVerifyHostname=%v, want %v", c.sslmode, cc.TLSVerifyHostname, c.wantVerifyHost)
		}
		if cc.QueryTimeout != 7*time.Second {
			t.Errorf("sslmode=%q QueryTimeout=%v, want 7s", c.sslmode, cc.QueryTimeout)
		}
	}
}

// TC-SCH-08 (SCH-R007): a failed SingleStore run carries a canonical reason; the
// elevated permission-denied degrade stays skipped/permission_denied.
func TestMapSingleStoreCollection_CanonicalReasons(t *testing.T) {
	results := []singlestore.Result{
		{CollectorID: "slow_v1", Category: "x", Err: "context deadline exceeded"},
		{CollectorID: "generic_v1", Category: "x", Err: "some other failure"},
		{CollectorID: "cluster_v1", Category: "topology", Err: "Error 1142 (42000): command denied", ElevatedPrivilege: "CLUSTER"},
	}
	_, runs, _, err := mapWireCollection("singlestore", "snap", 1, "9.1.1", "2026-10-24T00:00:00Z", results)
	if err != nil {
		t.Fatalf("mapSingleStoreCollection: %v", err)
	}
	byID := map[string]struct{ status, reason string }{}
	for _, r := range runs {
		byID[r.QueryID] = struct{ status, reason string }{r.Status, r.Reason}
	}
	if got := byID["slow_v1"]; got.status != "failed" || got.reason != "timeout" {
		t.Errorf("slow_v1 = %+v, want failed/timeout", got)
	}
	if got := byID["generic_v1"]; got.status != "failed" || got.reason != "execution_error" {
		t.Errorf("generic_v1 = %+v, want failed/execution_error", got)
	}
	if got := byID["cluster_v1"]; got.status != "skipped" || got.reason != "permission_denied" {
		t.Errorf("cluster_v1 = %+v, want skipped/permission_denied", got)
	}
}

// Guard that the bounded runner returns a context error type on timeout.
func TestBoundedSingleStoreRunner_DeadlineErrorType(t *testing.T) {
	runner := boundedSingleStoreRunner(func(ctx context.Context, _ string) ([]map[string]any, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}, 5*time.Millisecond)
	_, err := runner(context.Background(), "x")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("want context.DeadlineExceeded, got %v", err)
	}
}
