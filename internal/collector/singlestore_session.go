package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/elevarq/signals/internal/config"
	"github.com/elevarq/signals/internal/db"
	"github.com/elevarq/signals/internal/mysqlwire"
	"github.com/elevarq/signals/internal/singlestore"
)

// SingleStore engine session (#532, engine-collection-dispatch.md ECD-R003..R005).
//
// A SingleStore target is collected through this session behind the SAME common
// collection lifecycle as PostgreSQL (circuit breaker, min-interval, started/
// completed audit, R126 outcome, metrics, circuit feedback). It opens a
// MySQL-wire connection — never pgx (INV-ECD-01) — validates the server is
// positively SingleStore (ECD-R004; never a MySQL fallback, ECD-R006), runs the
// existing SingleStore collectors, maps the results onto the engine-neutral
// persistence records, and persists through the common InsertCollectionAtomic.

// ssConn is the SingleStore engine's DB/network surface: a scalar probe (engine
// identity) and a row runner (collectors). It is the ONLY boundary a
// production-boundary test replaces — dispatch, session selection, identity
// validation, collector orchestration, result mapping, persistence, and export
// all run for real above it (ECD-R003).
type ssConn struct {
	scalar mysqlwire.ScalarQuery
	runner singlestore.QueryRunner
	close  func() error
}

// ssConnector opens the SingleStore connection for a target. Production builds a
// MySQL-wire connection from the normal TargetConfig + resolved credentials
// (openSingleStore); a test injects a fake at this boundary via
// WithSingleStoreConnectorForTest without replacing any product behavior above.
type ssConnector func(ctx context.Context, tgt config.TargetConfig) (ssConn, error)

// singleStoreTLS maps a PostgreSQL-style sslmode onto a go-sql-driver TLS key.
// Opportunistic by default; disable/require are honored. (verify-ca/verify-full
// map to verified TLS; a full custom-CA path is a later refinement.)
func singleStoreTLS(sslmode string) string {
	switch strings.ToLower(strings.TrimSpace(sslmode)) {
	case "disable", "disabled":
		return "false"
	case "require", "required", "verify-ca", "verify-full":
		return "true"
	default: // "", prefer, preferred, allow
		return "preferred"
	}
}

// singleStoreConnConfig builds the MySQL-wire connection config from the normal
// target config + a resolved password + the configured per-query timeout. Pure;
// no I/O (ECD-R003). For verify-ca/verify-full it carries the configured CA path
// + verification mode so the connection honors the private CA (#585, SCH-R001/
// R002); the CA file is loaded (I/O) later, inside mysqlwire.Open.
func singleStoreConnConfig(tgt config.TargetConfig, password string, queryTimeout time.Duration) mysqlwire.ConnConfig {
	cc := mysqlwire.ConnConfig{
		Host:         tgt.Host,
		Port:         tgt.Port,
		User:         tgt.User,
		Password:     password,
		DBName:       tgt.DBName,
		TLS:          singleStoreTLS(tgt.SSLMode),
		QueryTimeout: queryTimeout,
	}
	switch strings.ToLower(strings.TrimSpace(tgt.SSLMode)) {
	case "verify-ca":
		cc.TLSRootCAFile = tgt.SSLRootCertFile
		cc.TLSVerifyHostname = false
	case "verify-full":
		cc.TLSRootCAFile = tgt.SSLRootCertFile
		cc.TLSVerifyHostname = true
	}
	return cc
}

// boundedSingleStoreRunner wraps a QueryRunner so each collector operation is
// bounded by min(queryTimeout, remaining target budget) (#586, SCH-R004/R005).
// A per-collector deadline means one slow collector is canceled at the query
// timeout without aborting the cycle; the target context remains the outer
// bound. A non-positive queryTimeout with no target deadline means no inner
// bound (the target deadline, if any, still applies).
func boundedSingleStoreRunner(base singlestore.QueryRunner, queryTimeout time.Duration) singlestore.QueryRunner {
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

// openSingleStore is the production ssConnector: resolve the credential via the
// normal resolver (no SingleStore-specific secret mechanism) and open a
// MySQL-wire connection (never pgx — INV-ECD-01). The password is used only to
// dial and is never persisted or logged.
func (c *Collector) openSingleStore(ctx context.Context, tgt config.TargetConfig) (ssConn, error) {
	cred, err := c.credResolver.Resolve(ctx, tgt)
	if err != nil {
		return ssConn{}, fmt.Errorf("resolve credential: %w", err)
	}
	database, err := mysqlwire.Open(ctx, singleStoreConnConfig(tgt, cred.Password, c.queryTimeout))
	if err != nil {
		return ssConn{}, err
	}
	return ssConn{
		scalar: mysqlwire.NewScalar(database),
		runner: singlestore.NewRunner(database),
		close:  database.Close,
	}, nil
}

// mapWireCollection maps MySQL-wire collector results onto the engine-neutral
// persistence records (ECD-R005): one snapshot (engine = the target's engine),
// one query_run per collector (query_id = collector id, failed runs preserved as
// a partial outcome), one query_result per run (NDJSON of rows via the common
// encoder). Engine-generic (SingleStore, MySQL). Pure; unit-testable without a
// DB or a server.
func mapWireCollection(engine, snapID string, targetID int64, version, collectedAt string, results []singlestore.Result) (db.Snapshot, []db.QueryRun, []db.QueryResult, error) {
	runs := make([]db.QueryRun, 0, len(results))
	payloads := make([]db.QueryResult, 0, len(results))
	for i, r := range results {
		runID := fmt.Sprintf("%s-%03d-%s", snapID, i, r.CollectorID)
		status := "success"
		reason := ""
		if r.Err != "" {
			// MKR-R007: a permission-denied error on a collector that declares
			// an elevated privilege (e.g. MV_CLUSTER_STATUS needs CLUSTER) is an
			// EXPECTED least-privilege degrade — recorded skipped, not failed.
			// A non-elevated collector's permission error stays a genuine fault.
			if r.ElevatedPrivilege != "" && singlestore.IsPermissionDenied(r.Err) {
				status = "skipped"
				reason = "permission_denied"
			} else {
				status = "failed"
				// SCH-R007: canonical reason (timeout / permission_denied /
				// object_missing / execution_error), never empty.
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
			// MKR-R006: carry the measured per-collector duration, not a fake 0.
			DurationMS: r.DurationMS,
			RowCount:   len(r.Rows), Error: r.Err, CreatedAt: collectedAt,
			Status: status, Reason: reason,
		})
		payloads = append(payloads, db.QueryResult{RunID: runID, Payload: payload, Compressed: compressed, SizeBytes: size})
	}
	// Minimal engine-neutral monolithic payload for the legacy snapshots.payload
	// column; the exporter rebuilds query_results from the per-run NDJSON above.
	mono, err := json.Marshal(map[string]any{
		"engine": engine, "engine_version": version, "collectors": len(results),
	})
	if err != nil {
		return db.Snapshot{}, nil, nil, fmt.Errorf("marshal snapshot payload: %w", err)
	}
	snap := db.Snapshot{
		ID: snapID, TargetID: targetID, CollectedAt: collectedAt,
		PGVersion: version, Engine: engine,
		Payload: mono, SizeBytes: len(mono),
	}
	return snap, runs, payloads, nil
}

// ssOutcomeCounts tallies persisted runs by outcome (MKR-R004) so the
// SingleStore "snapshot collected" log reports per-outcome counts instead of a
// single conflated total.
func ssOutcomeCounts(runs []db.QueryRun) (success, failed, skipped int) {
	for _, r := range runs {
		switch r.Status {
		case "failed":
			failed++
		case "skipped":
			skipped++
		default:
			success++
		}
	}
	return success, failed, skipped
}

// ssFailure identifies one failed SingleStore collector for the per-collector
// failure log (MKR-R003).
type ssFailure struct {
	ID     string
	Reason string
	Error  string
}

// ssFailedCollectors returns the failed runs by collector identity (MKR-R003).
// Skipped (e.g. permission-denied elevated) collectors are not failures.
func ssFailedCollectors(runs []db.QueryRun) []ssFailure {
	var out []ssFailure
	for _, r := range runs {
		if r.Status == "failed" {
			out = append(out, ssFailure{ID: r.QueryID, Reason: r.Reason, Error: r.Error})
		}
	}
	return out
}

// collectMySQLWire is the shared MySQL-wire engine session, serving every wire
// engine (SingleStore, MySQL) — selected by backend, never per engine
// (ECD-R002). It returns the snapshot id and the per-collector runs so the
// caller's common deferred lifecycle (outcome/metrics/circuit) reports the wire
// engine exactly as PostgreSQL. A connection/identity failure is a target-scoped
// collection failure (non-nil err), never a silent skip (INV-ECD-03). The
// collector catalog is chosen by the target's engine via CatalogFor (ECD-R008);
// no engine-specific branch lives here.
func (c *Collector) collectMySQLWire(ctx context.Context, tgt config.TargetConfig) (snapID string, runs []db.QueryRun, err error) {
	engine := tgt.EffectiveEngine()
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

	conn, err := c.ssConnect(ctx, tgt)
	if err != nil {
		return snapID, nil, fmt.Errorf("%s connect %s: %w", engine, tgt.Name, err)
	}
	defer func() { _ = conn.close() }()

	// ECD-R004: the connection must positively identify as the target's
	// CONFIGURED engine before we collect; a mismatched or unidentifiable server
	// is a failure, not a cross-engine fallback.
	gotEngine, version, idErr := mysqlwire.Identify(ctx, conn.scalar)
	if idErr != nil {
		return snapID, nil, fmt.Errorf("%s identify %s: %w", engine, tgt.Name, idErr)
	}
	if gotEngine != engine {
		return snapID, nil, fmt.Errorf("target %s configured engine=%s but server identified as %q — refusing to collect (ECD-R004)", tgt.Name, engine, gotEngine)
	}

	// Collector orchestration (real). The catalog is selected by engine via the
	// single CatalogFor registry (ECD-R008). Redaction mirrors the PostgreSQL
	// high-sensitivity posture: redact sensitive query text unless opted in.
	// SCH-R004: bound each collector by the configured per-query timeout while
	// the target context remains the outer bound of the cycle.
	runner := boundedSingleStoreRunner(conn.runner, c.queryTimeout)
	results := singlestore.Collect(ctx, runner, singlestore.CatalogFor(engine), !c.highSensitivityEnabled, c.collectNormalizedQueryText)

	for _, r := range results {
		if cerr := c.db.UpsertQueryCatalog(db.QueryCatalogRow{
			QueryID: r.CollectorID, Category: r.Category, ResultKind: "rowset",
			RetentionClass: "medium", RegisteredAt: collectedAt,
		}); cerr != nil {
			return snapID, runs, fmt.Errorf("register catalog %s: %w", r.CollectorID, cerr)
		}
	}

	snap, mappedRuns, payloads, mapErr := mapWireCollection(engine, snapID, targetID, version, collectedAt, results)
	if mapErr != nil {
		return snapID, nil, mapErr
	}
	runs = mappedRuns

	if insErr := c.db.InsertCollectionAtomic(snap, mappedRuns, payloads); insErr != nil {
		c.metrics.IncSQLitePersistenceFailure()
		return snapID, runs, fmt.Errorf("persist collection cycle for %s: %w", tgt.Name, insErr)
	}
	// MKR-R003: log each failed collector by identity, not just an aggregate.
	for _, f := range ssFailedCollectors(runs) {
		slog.Warn("wire collector failed", "target", tgt.Name, "engine", engine,
			"collector", f.ID, "reason", f.Reason, "error", f.Error)
	}
	// MKR-R004: report per-outcome counts, not a single len(results) total.
	success, failed, skipped := ssOutcomeCounts(runs)
	slog.Info("snapshot collected", "target", tgt.Name, "id", snapID,
		"engine", engine, "engine_version", version,
		"collectors_total", len(runs), "collectors_success", success,
		"collectors_failed", failed, "collectors_skipped", skipped)
	_ = c.db.InsertTargetEvent(targetID, "snapshot_collected",
		fmt.Sprintf("target=%s id=%s engine=%s collectors=%d success=%d failed=%d skipped=%d",
			tgt.Name, snapID, engine, len(runs), success, failed, skipped))
	return snapID, runs, nil
}
