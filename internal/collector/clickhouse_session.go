package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/elevarq/signals/internal/clickhouse"
	"github.com/elevarq/signals/internal/config"
	"github.com/elevarq/signals/internal/db"
)

// ClickHouse engine session (CH-1, specifications/clickhouse-backend.md). A
// ClickHouse target is collected through this session behind the SAME common
// collection lifecycle as PostgreSQL and the MySQL-wire engines (circuit
// breaker, min-interval, started/completed audit, outcome, metrics). It opens a
// ClickHouse native connection — never pgx, never the MySQL wire — validates the
// server is ClickHouse, runs the ClickHouse collectors, maps results onto the
// engine-neutral persistence records, and persists through the common
// InsertCollectionAtomic. ClickHouse is a THIRD backend; this session is
// parallel to collectMySQLWire and the inline pgx path.

// chConn is the ClickHouse engine's DB/network surface: a scalar probe (identity)
// and a row runner (collectors). The ONLY boundary a production-boundary test
// replaces — dispatch, identity validation, collector orchestration, mapping,
// persistence, and export all run for real above it.
type chConn struct {
	scalar clickhouse.ScalarQuery
	runner clickhouse.QueryRunner
	close  func() error
}

// chConnector opens the ClickHouse connection for a target. Production dials the
// native protocol; a test injects a fake at this boundary via
// WithClickHouseConnectorForTest without replacing any product behavior above.
type chConnector func(ctx context.Context, tgt config.TargetConfig) (chConn, error)

// openClickHouse is the production chConnector: resolve the credential via the
// normal resolver and open a ClickHouse native connection (never pgx / never the
// MySQL wire). The password is used only to dial and is never persisted/logged.
func (c *Collector) openClickHouse(ctx context.Context, tgt config.TargetConfig) (chConn, error) {
	cred, err := c.credResolver.Resolve(ctx, tgt)
	if err != nil {
		return chConn{}, fmt.Errorf("resolve credential: %w", err)
	}
	database, err := clickhouse.Open(ctx, clickHouseConnConfig(tgt, cred.Password, c.queryTimeout))
	if err != nil {
		return chConn{}, err
	}
	return chConn{
		scalar: clickhouse.NewScalar(database),
		runner: clickhouse.NewRunner(database),
		close:  database.Close,
	}, nil
}

// clickHouseConnConfig builds the native connector config from a target (#558,
// CCH-R005). The target's sslmode + root CA are carried through to the driver so
// a verify-ca/verify-full ClickHouse target is actually verified — previously
// these were dropped, silently downgrading to plaintext. Pure + no I/O so the
// wiring is directly testable.
func clickHouseConnConfig(tgt config.TargetConfig, password string, queryTimeout time.Duration) clickhouse.ConnConfig {
	return clickhouse.ConnConfig{
		Host:          tgt.Host,
		Port:          tgt.Port,
		User:          tgt.User,
		Password:      password,
		DBName:        tgt.DBName,
		QueryTimeout:  queryTimeout,
		TLSMode:       tgt.SSLMode,
		TLSRootCAFile: tgt.SSLRootCertFile,
	}
}

// boundedClickHouseRunner bounds each collector by min(queryTimeout, remaining
// target budget), so one slow collector is canceled at the query timeout without
// aborting the cycle (mirrors the MySQL-wire bounded runner).
func boundedClickHouseRunner(base clickhouse.QueryRunner, queryTimeout time.Duration) clickhouse.QueryRunner {
	return func(ctx context.Context, query string) ([]map[string]any, error) {
		timeout := queryTimeout
		if deadline, ok := ctx.Deadline(); ok {
			if remaining := time.Until(deadline); timeout <= 0 || remaining < timeout {
				timeout = remaining
			}
		}
		if timeout <= 0 {
			return base(ctx, query)
		}
		qctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return base(qctx, query)
	}
}

// mapClickHouseCollection maps ClickHouse collector results onto the
// engine-neutral persistence records (snapshot engine=clickhouse, one query_run
// per collector, one query_result per run). Pure; unit-testable without a DB or
// a server.
func mapClickHouseCollection(snapID string, targetID int64, version, collectedAt string, results []clickhouse.Result) (db.Snapshot, []db.QueryRun, []db.QueryResult, error) {
	runs := make([]db.QueryRun, 0, len(results))
	payloads := make([]db.QueryResult, 0, len(results))
	for i, r := range results {
		runID := fmt.Sprintf("%s-%03d-%s", snapID, i, r.CollectorID)
		status := "success"
		reason := ""
		if r.Err != "" {
			if clickhouse.IsPermissionDenied(r.Err) {
				status = "skipped"
				reason = "permission_denied"
			} else {
				status = "failed"
				reason = classifyRunError(r.Err)
			}
		}
		payload, compressed, size, err := db.EncodeNDJSON(r.Rows)
		if err != nil {
			return db.Snapshot{}, nil, nil, fmt.Errorf("encode %s: %w", r.CollectorID, err)
		}
		runs = append(runs, db.QueryRun{
			ID: runID, TargetID: targetID, SnapshotID: snapID,
			QueryID: r.CollectorID, CollectedAt: collectedAt, PGVersion: version,
			DurationMS: r.DurationMS,
			RowCount:   len(r.Rows), Error: r.Err, CreatedAt: collectedAt,
			Status: status, Reason: reason,
		})
		payloads = append(payloads, db.QueryResult{RunID: runID, Payload: payload, Compressed: compressed, SizeBytes: size})
	}
	mono, err := json.Marshal(map[string]any{
		"engine": config.EngineClickHouse, "engine_version": version, "collectors": len(results),
	})
	if err != nil {
		return db.Snapshot{}, nil, nil, fmt.Errorf("marshal snapshot payload: %w", err)
	}
	snap := db.Snapshot{
		ID: snapID, TargetID: targetID, CollectedAt: collectedAt,
		PGVersion: version, Engine: config.EngineClickHouse,
		Payload: mono, SizeBytes: len(mono),
	}
	return snap, runs, payloads, nil
}

// collectClickHouse is the ClickHouse engine session. It returns the snapshot id
// and the per-collector runs so the caller's common deferred lifecycle reports
// ClickHouse exactly as the other engines. A connection/identity failure is a
// target-scoped collection failure (non-nil err), never a silent skip.
func (c *Collector) collectClickHouse(ctx context.Context, tgt config.TargetConfig) (snapID string, runs []db.QueryRun, err error) {
	now := time.Now().UTC()
	collectedAt := now.Format(time.RFC3339)
	snapID = ulid.MustNew(ulid.Timestamp(now), c.entropy).String()

	targetID, err := c.db.UpsertTarget(
		tgt.Name, tgt.Host, tgt.Port, tgt.DBName, tgt.User,
		tgt.SSLMode, tgt.SecretType(), tgt.CredentialSourceRef(), tgt.Enabled,
	)
	if err != nil {
		return snapID, nil, fmt.Errorf("upsert target %s: %w", tgt.Name, err)
	}

	conn, err := c.chConnect(ctx, tgt)
	if err != nil {
		return snapID, nil, fmt.Errorf("clickhouse connect %s: %w", tgt.Name, err)
	}
	defer func() { _ = conn.close() }()

	// The server must positively identify as ClickHouse before we collect.
	version, idErr := clickhouse.Identify(ctx, conn.scalar)
	if idErr != nil {
		return snapID, nil, fmt.Errorf("clickhouse identify %s: %w", tgt.Name, idErr)
	}

	runner := boundedClickHouseRunner(conn.runner, c.queryTimeout)
	results := clickhouse.Collect(ctx, runner, clickhouse.Catalog(), !c.highSensitivityEnabled)

	for _, r := range results {
		if cerr := c.db.UpsertQueryCatalog(db.QueryCatalogRow{
			QueryID: r.CollectorID, Category: r.Category, ResultKind: "rowset",
			RetentionClass: "medium", RegisteredAt: collectedAt,
		}); cerr != nil {
			return snapID, runs, fmt.Errorf("register catalog %s: %w", r.CollectorID, cerr)
		}
	}

	snap, mappedRuns, payloads, mapErr := mapClickHouseCollection(snapID, targetID, version, collectedAt, results)
	if mapErr != nil {
		return snapID, nil, mapErr
	}
	runs = mappedRuns

	if insErr := c.db.InsertCollectionAtomic(snap, mappedRuns, payloads); insErr != nil {
		c.metrics.IncSQLitePersistenceFailure()
		return snapID, runs, fmt.Errorf("persist collection cycle for %s: %w", tgt.Name, insErr)
	}
	for _, f := range ssFailedCollectors(runs) {
		slog.Warn("clickhouse collector failed", "target", tgt.Name,
			"collector", f.ID, "reason", f.Reason, "error", f.Error)
	}
	success, failed, skipped := ssOutcomeCounts(runs)
	slog.Info("snapshot collected", "target", tgt.Name, "id", snapID,
		"engine", config.EngineClickHouse, "engine_version", version,
		"collectors_total", len(runs), "collectors_success", success,
		"collectors_failed", failed, "collectors_skipped", skipped)
	_ = c.db.InsertTargetEvent(targetID, "snapshot_collected",
		fmt.Sprintf("target=%s id=%s engine=clickhouse collectors=%d success=%d failed=%d skipped=%d",
			tgt.Name, snapID, len(runs), success, failed, skipped))
	return snapID, runs, nil
}
