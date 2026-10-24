package collector

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/elevarq/signals/internal/config"
	"github.com/elevarq/signals/internal/db"
	"github.com/elevarq/signals/internal/singlestore"
)

// ---- pure mapping/config units (no DB, no server) ----

func TestSingleStoreConnConfigFromTarget(t *testing.T) {
	tgt := config.TargetConfig{
		Name: "ss1", Host: "ss.example", Port: 3306, User: "admin",
		DBName: "memsql", SSLMode: "require",
	}
	cc := singleStoreConnConfig(tgt, "secret", 10*time.Second)
	if cc.Host != "ss.example" || cc.Port != 3306 || cc.User != "admin" || cc.DBName != "memsql" {
		t.Errorf("conn config fields not mapped from target: %+v", cc)
	}
	if cc.Password != "secret" {
		t.Errorf("password not carried from resolved credential")
	}
	if cc.TLS != "true" {
		t.Errorf("sslmode=require should map to TLS=true, got %q", cc.TLS)
	}
}

func TestSingleStoreTLSMapping(t *testing.T) {
	cases := map[string]string{
		"": "preferred", "prefer": "preferred", "preferred": "preferred",
		"disable": "false", "disabled": "false",
		"require": "true", "verify-ca": "true", "verify-full": "true",
	}
	for in, want := range cases {
		if got := singleStoreTLS(in); got != want {
			t.Errorf("singleStoreTLS(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMapSingleStoreCollection(t *testing.T) {
	results := []singlestore.Result{
		{CollectorID: "singlestore_identity_v1", Category: "server", Rows: []map[string]any{{"memsql_version": "8.5.22"}}},
		{CollectorID: "singlestore_aggregators_v1", Category: "topology", Rows: []map[string]any{{"state": "online"}, {"state": "online"}}},
		{CollectorID: "singlestore_cluster_status_v1", Category: "topology", Err: "permission denied"},
	}
	snap, runs, payloads, err := mapSingleStoreCollection("snap-1", 7, "8.5.22", "2026-10-21T00:00:00Z", results)
	if err != nil {
		t.Fatalf("mapSingleStoreCollection: %v", err)
	}
	if snap.Engine != "singlestore" || snap.PGVersion != "8.5.22" || snap.TargetID != 7 {
		t.Errorf("snapshot engine identity wrong: %+v", snap)
	}
	if len(runs) != 3 || len(payloads) != 3 {
		t.Fatalf("want 3 runs + 3 payloads, got %d/%d", len(runs), len(payloads))
	}
	if runs[0].QueryID != "singlestore_identity_v1" || runs[0].Status != "success" || runs[0].RowCount != 1 {
		t.Errorf("identity run mapped wrong: %+v", runs[0])
	}
	// A collector error is preserved as a failed run (partial outcome), not dropped.
	if runs[2].Status != "failed" || runs[2].Error == "" {
		t.Errorf("errored collector should map to a failed run: %+v", runs[2])
	}
}

// ---- production-boundary test (ECD-R003): fake ONLY the DB/network boundary ----

// fakeSSConn returns a connector that answers the SingleStore identity probe and
// every collector query with canned rows. It replaces the actual MySQL-wire
// query execution and NOTHING above it — dispatch, session selection, identity
// validation, collector orchestration, result mapping, persistence, and export
// all run for real.
func fakeSSConn(memsqlVersion string) ssConnector {
	return func(ctx context.Context, tgt config.TargetConfig) (ssConn, error) {
		return ssConn{
			scalar: func(ctx context.Context, q string) (string, error) {
				if strings.Contains(q, "memsql_version") {
					return memsqlVersion, nil // only SingleStore exposes this
				}
				return "", context.Canceled // other probes "unsupported"
			},
			runner: func(ctx context.Context, q string) ([]map[string]any, error) {
				return []map[string]any{{"probe": "ok", "memsql_version": memsqlVersion}}, nil
			},
			close: func() error { return nil },
		}, nil
	}
}

func newTestCollector(t *testing.T, opts ...CollectorOption) (*Collector, *db.DB) {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "c.db"), false)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	c := New(store, nil, time.Minute, 7, opts...)
	return c, store
}

// TestCollectTarget_SingleStoreProductionBoundary is the #532 production-path
// acceptance test. It enters the real production boundary (collectTarget) with a
// configured engine=singlestore target and a fake injected ONLY at the DB
// boundary, and proves a complete engine=singlestore snapshot is persisted and
// exported through the normal pipeline.
//
// RED: against the pre-fix behavior (non-PG target -> collection_skipped_engine
// -> return nil) NO snapshot is persisted, so the CountSnapshots and engine
// assertions below fail. GREEN: the dispatch routes to the SingleStore session.
func TestCollectTarget_SingleStoreProductionBoundary(t *testing.T) {
	c, store := newTestCollector(t, WithSingleStoreConnectorForTest(fakeSSConn("8.5.22")))
	tgt := config.TargetConfig{
		Name: "ss-demo", Host: "ss.example", Port: 3306, User: "admin",
		DBName: "memsql", SSLMode: "require", Engine: config.EngineSingleStore, Enabled: true,
	}

	if err := c.collectTarget(context.Background(), tgt, true, true, "", ""); err != nil {
		t.Fatalf("collectTarget(singlestore) returned error: %v", err)
	}

	// Persistence: exactly one snapshot, engine=singlestore (not the PG default).
	n, err := store.CountSnapshots()
	if err != nil {
		t.Fatalf("CountSnapshots: %v", err)
	}
	if n != 1 {
		t.Fatalf("want 1 persisted snapshot (pre-fix skip would give 0), got %d", n)
	}
	snaps, err := store.GetLatestSnapshotsPerTarget()
	if err != nil || len(snaps) != 1 {
		t.Fatalf("GetLatestSnapshotsPerTarget: %v (n=%d)", err, len(snaps))
	}
	snap := snaps[0]
	if snap.Engine != "singlestore" {
		t.Fatalf("persisted snapshot engine = %q, want singlestore", snap.Engine)
	}
	if snap.PGVersion != "8.5.22" {
		t.Errorf("persisted engine version = %q, want 8.5.22", snap.PGVersion)
	}

	// Orchestration: one run per collector in the catalog, all persisted.
	runs, err := store.GetQueryRunsBySnapshotIDs([]string{snap.ID})
	if err != nil {
		t.Fatalf("GetQueryRunsBySnapshotIDs: %v", err)
	}
	if len(runs) != len(singlestore.Catalog()) {
		t.Errorf("want %d collector runs (full catalog orchestrated), got %d", len(singlestore.Catalog()), len(runs))
	}

	// The exact persisted artifact exports with metadata.engine == singlestore
	// through the NORMAL exporter — asserted in internal/export
	// (TestExportMetadataDeclaresEngine) to avoid the export->collector import
	// cycle here. This test owns the dispatch/session/persistence half.
}

// ECD-R004 / ECD-R006: a server that does not positively identify as SingleStore
// is a target-scoped failure — never collected, never a MySQL fallback.
func TestCollectTarget_SingleStoreIdentityMismatch(t *testing.T) {
	notSingleStore := func(ctx context.Context, tgt config.TargetConfig) (ssConn, error) {
		return ssConn{
			scalar: func(ctx context.Context, q string) (string, error) {
				// Plain MySQL: no @@memsql_version; version_comment brands it
				// MySQL so engineid resolves a DEFINITE non-SingleStore engine
				// (the engine-mismatch branch of ECD-R004).
				switch {
				case strings.Contains(q, "version_comment"):
					return "MySQL Community Server - GPL", nil
				case strings.Contains(q, "VERSION()"):
					return "8.0.33", nil
				default:
					return "", context.Canceled
				}
			},
			runner: func(ctx context.Context, q string) ([]map[string]any, error) {
				return nil, nil
			},
			close: func() error { return nil },
		}, nil
	}
	c, store := newTestCollector(t, WithSingleStoreConnectorForTest(notSingleStore))
	tgt := config.TargetConfig{
		Name: "fake-ss", Host: "mysql.example", Port: 3306, User: "root",
		Engine: config.EngineSingleStore, Enabled: true,
	}
	err := c.collectTarget(context.Background(), tgt, true, true, "", "")
	if err == nil {
		t.Fatal("expected a target-scoped failure when the server is not SingleStore; got nil")
	}
	if !strings.Contains(err.Error(), "refusing to collect") {
		t.Errorf("error should cite the identity gate (ECD-R004), got: %v", err)
	}
	if n, _ := store.CountSnapshots(); n != 0 {
		t.Errorf("no snapshot must be persisted on identity mismatch, got %d", n)
	}
}

// ECD-R006: an unsupported engine (mysql) is skipped explicitly — not routed
// through the SingleStore session, and no snapshot produced.
func TestCollectTarget_UnsupportedEngineSkipped(t *testing.T) {
	// A connector that would panic if ever called — proving mysql never enters
	// the SingleStore session.
	trap := func(ctx context.Context, tgt config.TargetConfig) (ssConn, error) {
		t.Fatalf("SingleStore connector must NOT be invoked for a mysql target (ECD-R006)")
		return ssConn{}, nil
	}
	c, store := newTestCollector(t, WithSingleStoreConnectorForTest(trap))
	tgt := config.TargetConfig{Name: "my1", Host: "h", Port: 3306, Engine: config.EngineMySQL, Enabled: true}
	if err := c.collectTarget(context.Background(), tgt, true, true, "", ""); err != nil {
		t.Fatalf("unsupported-engine skip should return nil, got %v", err)
	}
	if n, _ := store.CountSnapshots(); n != 0 {
		t.Errorf("unsupported engine must not persist a snapshot, got %d", n)
	}
}
