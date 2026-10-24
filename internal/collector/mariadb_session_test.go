package collector

import (
	"context"
	"strings"
	"testing"

	"github.com/elevarq/signals/internal/config"
)

// Tests derive from specifications/engine-collection-dispatch.md (backend-driven
// dispatch, CatalogFor seam) and specifications/mariadb-collectors.md.

// fakeMariaDBConn answers the identity probe as a MariaDB server (no
// @@memsql_version; version_comment + VERSION() name MariaDB) and every
// collector query with a canned row. Replaces ONLY the DB/network boundary.
func fakeMariaDBConn() ssConnector {
	return func(ctx context.Context, tgt config.TargetConfig) (ssConn, error) {
		return ssConn{
			scalar: func(ctx context.Context, q string) (string, error) {
				switch {
				case strings.Contains(q, "memsql_version"):
					return "", context.Canceled
				case strings.Contains(q, "version_comment"):
					return "mariadb.org binary distribution", nil
				default: // VERSION()
					return "11.4.13-MariaDB-ubu2404", nil
				}
			},
			runner: func(ctx context.Context, q string) ([]map[string]any, error) {
				return []map[string]any{{"probe": "ok"}}, nil
			},
			close: func() error { return nil },
		}, nil
	}
}

// TestCollectTarget_MariaDBProductionBoundary proves the backend-driven dispatch
// routes an engine=mariadb target through the shared wire session, validates
// identity against the configured engine (mariadb — distinct from mysql, M5),
// selects the MariaDB catalog via CatalogFor, and persists one engine=mariadb
// snapshot through the normal pipeline — with NO per-engine dispatch/session edit.
func TestCollectTarget_MariaDBProductionBoundary(t *testing.T) {
	c, store := newTestCollector(t, WithSingleStoreConnectorForTest(fakeMariaDBConn()))
	tgt := config.TargetConfig{
		Name: "mariadb-demo", Host: "mariadb.example", Port: 3306, User: "signals_ro",
		DBName: "app", SSLMode: "disable", Engine: config.EngineMariaDB, Enabled: true,
	}

	if err := c.collectTarget(context.Background(), tgt, true, true, "", ""); err != nil {
		t.Fatalf("collectTarget(mariadb) returned error: %v", err)
	}

	snaps, err := store.GetAllSnapshots("", "")
	if err != nil {
		t.Fatalf("GetAllSnapshots: %v", err)
	}
	if len(snaps) != 1 || snaps[0].Engine != "mariadb" {
		t.Fatalf("want one engine=mariadb snapshot, got %+v", snaps)
	}

	runs, err := store.GetQueryRunsBySnapshotIDs([]string{snaps[0].ID})
	if err != nil {
		t.Fatalf("GetQueryRunsBySnapshotIDs: %v", err)
	}
	if len(runs) == 0 {
		t.Fatal("no runs persisted for the mariadb snapshot")
	}
	for _, r := range runs {
		if !strings.HasPrefix(r.QueryID, "mariadb_") {
			t.Errorf("mariadb snapshot carried a non-mariadb collector %q (foreign catalog leaked)", r.QueryID)
		}
		if r.Status != "success" {
			t.Errorf("collector %q status=%q, want success", r.QueryID, r.Status)
		}
	}
}

// TestCollectTarget_MariaDBIdentityMismatchFails: a mariadb-configured target
// whose server identifies as mysql is a target-scoped failure (the M5
// distinct-identity split makes mariadb != mysql), never a fallback.
func TestCollectTarget_MariaDBIdentityMismatchFails(t *testing.T) {
	// Connector identifying as plain MySQL (no mariadb branding).
	mysqlish := func(ctx context.Context, tgt config.TargetConfig) (ssConn, error) {
		return ssConn{
			scalar: func(ctx context.Context, q string) (string, error) {
				switch {
				case strings.Contains(q, "memsql_version"):
					return "", context.Canceled
				case strings.Contains(q, "version_comment"):
					return "MySQL Community Server - GPL", nil
				default:
					return "8.4.13", nil
				}
			},
			runner: func(ctx context.Context, q string) ([]map[string]any, error) { return nil, nil },
			close:  func() error { return nil },
		}, nil
	}
	c, _ := newTestCollector(t, WithSingleStoreConnectorForTest(mysqlish))
	tgt := config.TargetConfig{
		Name: "mismatch", Host: "h", Port: 3306, User: "u",
		DBName: "d", SSLMode: "disable", Engine: config.EngineMariaDB, Enabled: true,
	}
	if err := c.collectTarget(context.Background(), tgt, true, true, "", ""); err == nil {
		t.Fatal("expected a target-scoped failure when a mariadb target identifies as mysql")
	}
}
