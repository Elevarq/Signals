//go:build integration

package tests

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/elevarq/arq-signals/internal/collector"
)

// TestIntegration_RoleSafetyAgainstRealPG connects to a real PostgreSQL
// and validates role safety checks. Run with:
//
//	ARQ_TEST_PG_DSN="postgres://arq_monitor@localhost/postgres" go test -tags integration ./tests/ -run Integration
func TestIntegration_RoleSafetyAgainstRealPG(t *testing.T) {
	dsn := os.Getenv("ARQ_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("ARQ_TEST_PG_DSN not set — skipping live PostgreSQL integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to PG: %v", err)
	}
	defer pool.Close()

	result, err := collector.ValidateRoleSafety(ctx, pool)
	if err != nil {
		t.Fatalf("ValidateRoleSafety: %v", err)
	}

	// If connected with a proper monitoring role, expect safe
	if !result.IsSafe() {
		t.Logf("Role is unsafe: %s", result.Error())
		t.Log("This is expected if connected as superuser — use a pg_monitor role for a passing test")
	} else {
		t.Log("Role safety check passed — connected with a safe monitoring role")
	}
}
