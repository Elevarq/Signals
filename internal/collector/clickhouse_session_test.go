package collector

import (
	"context"
	"strings"
	"testing"

	"github.com/elevarq/signals/internal/clickhouse"
	"github.com/elevarq/signals/internal/config"
	"github.com/elevarq/signals/internal/db"
)

// Tests derive from specifications/clickhouse-backend.acceptance.md.

// fakeCHConn answers the ClickHouse identity probe (SELECT version()) with a
// ClickHouse version and every collector query with a canned row. Replaces ONLY
// the DB/network boundary; dispatch, backend selection, identity validation,
// collector orchestration, mapping, persistence, and export all run for real.
func fakeCHConn() chConnector {
	return func(ctx context.Context, tgt config.TargetConfig) (chConn, error) {
		return chConn{
			scalar: func(ctx context.Context, q string) (string, error) {
				return "24.8.14.39", nil // SELECT version()
			},
			runner: func(ctx context.Context, q string) ([]map[string]any, error) {
				return []map[string]any{{"version": "24.8.14.39"}}, nil
			},
			close: func() error { return nil },
		}, nil
	}
}

// TC-CHB-06: the backend-driven dispatch routes an engine=clickhouse target
// through the ClickHouse session (BackendClickHouse), validates identity,
// selects the ClickHouse catalog, and persists one engine=clickhouse snapshot
// through the normal pipeline.
func TestCollectTarget_ClickHouseProductionBoundary(t *testing.T) {
	c, store := newTestCollector(t, WithClickHouseConnectorForTest(fakeCHConn()))
	tgt := config.TargetConfig{
		Name: "ch-demo", Host: "ch.example", Port: 9000, User: "signals_ro",
		DBName: "default", SSLMode: "disable", Engine: config.EngineClickHouse, Enabled: true,
	}

	if err := c.collectTarget(context.Background(), tgt, true, true, "", ""); err != nil {
		t.Fatalf("collectTarget(clickhouse) returned error: %v", err)
	}

	snaps, err := store.GetAllSnapshots("", "")
	if err != nil {
		t.Fatalf("GetAllSnapshots: %v", err)
	}
	if len(snaps) != 1 || snaps[0].Engine != "clickhouse" {
		t.Fatalf("want one engine=clickhouse snapshot, got %+v", snaps)
	}
	if snaps[0].PGVersion != "24.8.14.39" {
		t.Errorf("snapshot version = %q, want 24.8.14.39", snaps[0].PGVersion)
	}

	runs, err := store.GetQueryRunsBySnapshotIDs([]string{snaps[0].ID})
	if err != nil {
		t.Fatalf("GetQueryRunsBySnapshotIDs: %v", err)
	}
	if len(runs) == 0 {
		t.Fatal("no runs persisted for the clickhouse snapshot")
	}
	for _, r := range runs {
		if !strings.HasPrefix(r.QueryID, "clickhouse_") {
			t.Errorf("clickhouse snapshot carried a non-clickhouse collector %q", r.QueryID)
		}
		if r.Status != "success" {
			t.Errorf("collector %q status=%q, want success", r.QueryID, r.Status)
		}
	}
}

// TC-CHB-07: mapClickHouseCollection maps results onto engine=clickhouse records
// with canonical reasons; a permission-denied error degrades to skipped.
func TestMapClickHouseCollection(t *testing.T) {
	results := []clickhouse.Result{
		{CollectorID: "clickhouse_identity_v1", Category: "server", Rows: []map[string]any{{"v": "24.8"}}, DurationMS: 5},
		{CollectorID: "clickhouse_denied_v1", Category: "x", Err: "Code: 497. ACCESS_DENIED"},
		{CollectorID: "clickhouse_timeout_v1", Category: "x", Err: "context deadline exceeded"},
	}
	snap, runs, _, err := mapClickHouseCollection("snap", 1, "24.8.14.39", "2026-10-24T00:00:00Z", results)
	if err != nil {
		t.Fatalf("mapClickHouseCollection: %v", err)
	}
	if snap.Engine != "clickhouse" {
		t.Errorf("snapshot engine = %q, want clickhouse", snap.Engine)
	}
	byID := map[string]db.QueryRun{}
	for _, r := range runs {
		byID[r.QueryID] = r
	}
	if r := byID["clickhouse_identity_v1"]; r.Status != "success" || r.DurationMS != 5 {
		t.Errorf("identity run = {status=%q dur=%d}, want success/5", r.Status, r.DurationMS)
	}
	if r := byID["clickhouse_denied_v1"]; r.Status != "skipped" || r.Reason != "permission_denied" {
		t.Errorf("denied run = {status=%q reason=%q}, want skipped/permission_denied", r.Status, r.Reason)
	}
	if r := byID["clickhouse_timeout_v1"]; r.Status != "failed" || r.Reason != "timeout" {
		t.Errorf("timeout run = {status=%q reason=%q}, want failed/timeout", r.Status, r.Reason)
	}
}
