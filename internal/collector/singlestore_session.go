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
	"github.com/elevarq/signals/internal/engineid"
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
// target config + a resolved password. Pure; no I/O (ECD-R003).
func singleStoreConnConfig(tgt config.TargetConfig, password string) mysqlwire.ConnConfig {
	return mysqlwire.ConnConfig{
		Host:     tgt.Host,
		Port:     tgt.Port,
		User:     tgt.User,
		Password: password,
		DBName:   tgt.DBName,
		TLS:      singleStoreTLS(tgt.SSLMode),
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
	database, err := mysqlwire.Open(ctx, singleStoreConnConfig(tgt, cred.Password))
	if err != nil {
		return ssConn{}, err
	}
	return ssConn{
		scalar: mysqlwire.NewScalar(database),
		runner: singlestore.NewRunner(database),
		close:  database.Close,
	}, nil
}

// mapSingleStoreCollection maps engine-native collector results onto the
// engine-neutral persistence records (ECD-R005): one snapshot (engine=
// singlestore), one query_run per collector (query_id = collector id, failed
// runs preserved as a partial outcome), one query_result per run (NDJSON of
// rows via the common encoder). Pure; unit-testable without a DB or a server.
func mapSingleStoreCollection(snapID string, targetID int64, version, collectedAt string, results []singlestore.Result) (db.Snapshot, []db.QueryRun, []db.QueryResult, error) {
	runs := make([]db.QueryRun, 0, len(results))
	payloads := make([]db.QueryResult, 0, len(results))
	for i, r := range results {
		runID := fmt.Sprintf("%s-%03d-%s", snapID, i, r.CollectorID)
		status := "success"
		if r.Err != "" {
			status = "failed"
		}
		payload, compressed, size, err := db.EncodeNDJSON(r.Rows)
		if err != nil {
			return db.Snapshot{}, nil, nil, fmt.Errorf("encode %s: %w", r.CollectorID, err)
		}
		runs = append(runs, db.QueryRun{
			ID: runID, TargetID: targetID, SnapshotID: snapID,
			QueryID: r.CollectorID, CollectedAt: collectedAt, PGVersion: version,
			RowCount: len(r.Rows), Error: r.Err, CreatedAt: collectedAt, Status: status,
		})
		payloads = append(payloads, db.QueryResult{RunID: runID, Payload: payload, Compressed: compressed, SizeBytes: size})
	}
	// Minimal engine-neutral monolithic payload for the legacy snapshots.payload
	// column; the exporter rebuilds query_results from the per-run NDJSON above.
	mono, err := json.Marshal(map[string]any{
		"engine": engineid.EngineSingleStore, "engine_version": version, "collectors": len(results),
	})
	if err != nil {
		return db.Snapshot{}, nil, nil, fmt.Errorf("marshal snapshot payload: %w", err)
	}
	snap := db.Snapshot{
		ID: snapID, TargetID: targetID, CollectedAt: collectedAt,
		PGVersion: version, Engine: engineid.EngineSingleStore,
		Payload: mono, SizeBytes: len(mono),
	}
	return snap, runs, payloads, nil
}

// collectSingleStore is the SingleStore engine session. It returns the snapshot
// id and the per-collector runs so the caller's common deferred lifecycle
// (outcome/metrics/circuit) reports SingleStore exactly as PostgreSQL. A
// connection/identity failure is a target-scoped collection failure (non-nil
// err), never a silent skip (INV-ECD-03).
func (c *Collector) collectSingleStore(ctx context.Context, tgt config.TargetConfig) (snapID string, runs []db.QueryRun, err error) {
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
		return snapID, nil, fmt.Errorf("singlestore connect %s: %w", tgt.Name, err)
	}
	defer func() { _ = conn.close() }()

	// ECD-R004: the connection must be positively SingleStore before we collect;
	// a non-SingleStore or unidentifiable server is a failure, not a fallback.
	gotEngine, version, idErr := mysqlwire.Identify(ctx, conn.scalar)
	if idErr != nil {
		return snapID, nil, fmt.Errorf("singlestore identify %s: %w", tgt.Name, idErr)
	}
	if gotEngine != engineid.EngineSingleStore {
		return snapID, nil, fmt.Errorf("target %s configured engine=singlestore but server identified as %q — refusing to collect (ECD-R004)", tgt.Name, gotEngine)
	}

	// Collector orchestration (real). Redaction mirrors the PostgreSQL
	// high-sensitivity posture: redact sensitive query text unless opted in.
	results := singlestore.Collect(ctx, conn.runner, singlestore.Catalog(), !c.highSensitivityEnabled)

	for _, r := range results {
		if cerr := c.db.UpsertQueryCatalog(db.QueryCatalogRow{
			QueryID: r.CollectorID, Category: r.Category, ResultKind: "rowset",
			RetentionClass: "medium", RegisteredAt: collectedAt,
		}); cerr != nil {
			return snapID, runs, fmt.Errorf("register catalog %s: %w", r.CollectorID, cerr)
		}
	}

	snap, mappedRuns, payloads, mapErr := mapSingleStoreCollection(snapID, targetID, version, collectedAt, results)
	if mapErr != nil {
		return snapID, nil, mapErr
	}
	runs = mappedRuns

	if insErr := c.db.InsertCollection(snap, mappedRuns, payloads); insErr != nil {
		c.metrics.IncSQLitePersistenceFailure()
		return snapID, runs, fmt.Errorf("persist collection cycle for %s: %w", tgt.Name, insErr)
	}
	slog.Info("snapshot collected", "target", tgt.Name, "id", snapID,
		"engine", engineid.EngineSingleStore, "engine_version", version, "collectors", len(results))
	_ = c.db.InsertTargetEvent(targetID, "snapshot_collected",
		fmt.Sprintf("target=%s id=%s engine=singlestore collectors=%d", tgt.Name, snapID, len(results)))
	return snapID, runs, nil
}
