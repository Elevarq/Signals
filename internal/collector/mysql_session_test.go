package collector

import (
	"context"
	"strings"
	"testing"

	"github.com/elevarq/signals/internal/config"
)

// Tests derive from specifications/engine-collection-dispatch.md (ECD-R002/R004/
// R008) and specifications/mysql-collectors.md.

// fakeMySQLConn answers the identity probe as a MySQL server (no @@memsql_version;
// version_comment names MySQL) and every collector query with a canned row. It
// replaces ONLY the DB/network boundary; dispatch, backend selection, identity
// validation, catalog selection (CatalogFor), mapping, persistence, and export
// all run for real.
func fakeMySQLConn() ssConnector {
	return func(ctx context.Context, tgt config.TargetConfig) (ssConn, error) {
		return ssConn{
			scalar: func(ctx context.Context, q string) (string, error) {
				switch {
				case strings.Contains(q, "memsql_version"):
					return "", context.Canceled // MySQL rejects this unknown variable
				case strings.Contains(q, "version_comment"):
					return "MySQL Community Server - GPL", nil
				default: // VERSION()
					return "8.4.13", nil
				}
			},
			runner: func(ctx context.Context, q string) ([]map[string]any, error) {
				return []map[string]any{{"probe": "ok"}}, nil
			},
			close: func() error { return nil },
		}, nil
	}
}

// TestCollectTarget_MySQLProductionBoundary proves the backend-driven dispatch
// routes an engine=mysql target through the shared MySQL-wire session, validates
// identity against the configured engine (mysql), selects the MySQL catalog via
// CatalogFor, and persists one engine=mysql snapshot through the normal pipeline.
func TestCollectTarget_MySQLProductionBoundary(t *testing.T) {
	c, store := newTestCollector(t, WithSingleStoreConnectorForTest(fakeMySQLConn()))
	tgt := config.TargetConfig{
		Name: "mysql-demo", Host: "mysql.example", Port: 3306, User: "signals_ro",
		DBName: "app", SSLMode: "disable", Engine: config.EngineMySQL, Enabled: true,
	}

	if err := c.collectTarget(context.Background(), tgt, true, true, "", ""); err != nil {
		t.Fatalf("collectTarget(mysql) returned error: %v", err)
	}

	n, err := store.CountSnapshots()
	if err != nil {
		t.Fatalf("CountSnapshots: %v", err)
	}
	if n != 1 {
		t.Fatalf("want 1 persisted snapshot for the mysql target, got %d", n)
	}

	snaps, err := store.GetAllSnapshots("", "")
	if err != nil {
		t.Fatalf("GetAllSnapshots: %v", err)
	}
	if len(snaps) != 1 || snaps[0].Engine != "mysql" {
		t.Fatalf("want one engine=mysql snapshot, got %+v", snaps)
	}

	runs, err := store.GetQueryRunsBySnapshotIDs([]string{snaps[0].ID})
	if err != nil {
		t.Fatalf("GetQueryRunsBySnapshotIDs: %v", err)
	}
	// One run per MySQL catalog collector, each a mysql_* id, all successful
	// (the fake runner returns a row for every query).
	if len(runs) == 0 {
		t.Fatal("no runs persisted for the mysql snapshot")
	}
	for _, r := range runs {
		if !strings.HasPrefix(r.QueryID, "mysql_") {
			t.Errorf("mysql snapshot carried a non-mysql collector %q (foreign catalog leaked)", r.QueryID)
		}
		if r.Status != "success" {
			t.Errorf("collector %q status=%q, want success", r.QueryID, r.Status)
		}
	}
}

// TestCollectTarget_MySQLIdentityMismatchFails proves the identity guard: a
// target configured engine=mysql whose server identifies as SingleStore is a
// target-scoped failure (ECD-R004), never a cross-engine fallback.
func TestCollectTarget_MySQLIdentityMismatchFails(t *testing.T) {
	// Connector that identifies as SingleStore though the target says mysql.
	c, _ := newTestCollector(t, WithSingleStoreConnectorForTest(fakeSSConn("8.5.22")))
	tgt := config.TargetConfig{
		Name: "mismatch", Host: "h", Port: 3306, User: "u",
		DBName: "d", SSLMode: "disable", Engine: config.EngineMySQL, Enabled: true,
	}
	err := c.collectTarget(context.Background(), tgt, true, true, "", "")
	if err == nil {
		t.Fatal("expected a target-scoped failure when the server engine != configured engine")
	}
}
