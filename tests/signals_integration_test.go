//go:build integration

package tests

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/elevarq/signals/internal/collector"
	"github.com/elevarq/signals/internal/pgqueries"
)

// TestIntegration_RoleSafetyAgainstRealPG connects to a real PostgreSQL
// and validates role safety checks. Run with:
//
//	SIGNALS_TEST_PG_DSN="postgres://signals@localhost/postgres" go test -tags integration ./tests/ -run Integration
func TestIntegration_RoleSafetyAgainstRealPG(t *testing.T) {
	dsn := os.Getenv("SIGNALS_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("SIGNALS_TEST_PG_DSN not set — skipping live PostgreSQL integration test")
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

// TestIntegration_ExtensionInventoryIncludesAvailableNotInstalled runs the
// registered extension_inventory_v1 query against a real PostgreSQL and
// verifies the evidence carries BOTH installed extensions (AC-01) and
// available-but-not-installed ones (AC-02, #415 — rows with a NULL
// installed_version that the old installed-only filter dropped). Run with:
//
//	SIGNALS_TEST_PG_DSN="postgres://signals@localhost/postgres" go test -tags integration ./tests/ -run Integration
func TestIntegration_ExtensionInventoryIncludesAvailableNotInstalled(t *testing.T) {
	dsn := os.Getenv("SIGNALS_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("SIGNALS_TEST_PG_DSN not set — skipping live PostgreSQL integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to PG: %v", err)
	}
	defer pool.Close()

	q := pgqueries.ByID("extension_inventory_v1")
	if q == nil {
		t.Fatal("extension_inventory_v1 not registered")
	}

	rows, err := pool.Query(ctx, q.SQL)
	if err != nil {
		t.Fatalf("run extension_inventory_v1: %v", err)
	}
	defer rows.Close()

	var installed, availableNotInstalled int
	for rows.Next() {
		var name, defaultVersion, installedVersion, comment *string
		if err := rows.Scan(&name, &defaultVersion, &installedVersion, &comment); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if installedVersion != nil {
			installed++
		} else {
			availableNotInstalled++
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate: %v", err)
	}

	// AC-01: at least one installed extension (plpgsql is always created).
	if installed == 0 {
		t.Errorf("AC-01: expected at least one installed extension, got 0")
	}
	// AC-02 (#415): available-but-not-installed extensions must now appear.
	// Any standard PostgreSQL with contrib exposes many; their absence here
	// would mean the pre-#415 installed-only filter is still in effect.
	if availableNotInstalled == 0 {
		t.Errorf("AC-02: expected available-but-not-installed extensions "+
			"(installed_version NULL) in the evidence, got 0 (installed=%d) — "+
			"the installed-only filter appears still in effect", installed)
	}
	t.Logf("extension_inventory_v1: installed=%d available-but-not-installed=%d",
		installed, availableNotInstalled)
}
