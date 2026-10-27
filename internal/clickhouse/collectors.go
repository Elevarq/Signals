// Package clickhouse collects read-only diagnostic evidence from a ClickHouse
// server over its native protocol (via ClickHouse/clickhouse-go) and returns
// per-collector results for a snapshot that declares engine=clickhouse.
//
// ClickHouse is a THIRD backend, distinct from PostgreSQL (pgx) and the shared
// MySQL-wire backend (SingleStore/MySQL/MariaDB): it speaks neither protocol, so
// it has its own transport + identity + catalog here. Collection is read-only
// (SELECT over system.* only, INV-CHB-01) and never touches the pgx or
// MySQL-wire paths.
//
// CH-1 ships the backend foundation with a minimal catalog (identity); the full
// system.* diagnostic catalog is CH-2 (specifications/clickhouse-backend.md).
package clickhouse

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// Collector is one read-only ClickHouse query and its handling metadata. Shares
// the same shape as the MySQL-wire Collector, but ClickHouse keeps its own type
// so the two backends stay decoupled.
type Collector struct {
	ID               string
	Category         string
	SQL              string
	HighSensitivity  bool
	SensitiveColumns []string
}

// Result is the outcome of running one Collector.
type Result struct {
	CollectorID string
	Category    string
	Rows        []map[string]any
	Err         string
	DurationMS  int
}

// QueryRunner runs a single read-only statement and returns its rows as maps.
// A seam so Collect is unit-testable without a live server.
type QueryRunner func(ctx context.Context, query string) ([]map[string]any, error)

// Catalog returns the validated ClickHouse collector set
// (specifications/clickhouse-collectors.md). Every statement is a read-only
// SELECT over system.* (INV-CHB-01 / CHC-R001), with columns confirmed live
// against ClickHouse 24.8. Collectors that are empty on a single node
// (replicas/merges/mutations/query_thread_log) are legitimate zero-row results,
// not failures (CHC-R003). query_log / query_thread_log carry application SQL
// text in the `query` column and are HighSensitivity with it redacted
// (CHC-R004). The identity collector stays first.
func Catalog() []Collector {
	return []Collector{
		{
			ID:       "clickhouse_identity_v1",
			Category: "server",
			SQL:      "SELECT version() AS version, uptime() AS uptime_s",
		},
		{
			ID:       "clickhouse_clusters_v1",
			Category: "topology",
			SQL:      "SELECT cluster, shard_num, replica_num, host_name, host_address, port, is_local FROM system.clusters",
		},
		{
			ID:       "clickhouse_databases_v1",
			Category: "schema",
			SQL:      "SELECT name, engine FROM system.databases",
		},
		{
			ID:       "clickhouse_tables_v1",
			Category: "schema",
			SQL:      "SELECT database, name, engine, total_rows, total_bytes FROM system.tables WHERE database NOT IN ('system', 'information_schema', 'INFORMATION_SCHEMA')",
		},
		{
			// #562 (CHC-R006): aggregate system.parts to one row per
			// (database, table, partition_id) instead of one row per active part.
			// A busy MergeTree can hold millions of active parts, so the raw
			// per-part SELECT was unbounded in rows/memory/payload. The
			// parts-per-partition count IS the diagnostic signal (merge pressure /
			// too-many-parts), so aggregation preserves — sharpens — the evidence
			// while bounding cardinality to the schema (≈#partitions), independent
			// of part churn. ORDER BY parts DESC + a documented LIMIT keeps the
			// worst-offending partitions if the cap is ever reached.
			ID:       "clickhouse_parts_v1",
			Category: "storage",
			SQL:      "SELECT database, table, partition_id, count() AS parts, sum(rows) AS rows, sum(bytes_on_disk) AS bytes_on_disk FROM system.parts WHERE active AND database NOT IN ('system', 'information_schema', 'INFORMATION_SCHEMA') GROUP BY database, table, partition_id ORDER BY parts DESC LIMIT 10000",
		},
		{
			ID:       "clickhouse_replicas_v1",
			Category: "topology",
			SQL:      "SELECT database, table, is_leader, is_readonly, absolute_delay, queue_size FROM system.replicas",
		},
		{
			ID:       "clickhouse_merges_v1",
			Category: "resources",
			SQL:      "SELECT database, table, elapsed, progress, num_parts, is_mutation FROM system.merges",
		},
		{
			ID:       "clickhouse_mutations_v1",
			Category: "resources",
			SQL:      "SELECT database, table, mutation_id, is_done, parts_to_do FROM system.mutations WHERE NOT is_done",
		},
		{
			ID:       "clickhouse_metrics_v1",
			Category: "resources",
			SQL:      "SELECT metric, value, description FROM system.metrics WHERE value != 0",
		},
		{
			ID:       "clickhouse_events_v1",
			Category: "resources",
			SQL:      "SELECT event, value, description FROM system.events",
		},
		{
			ID:       "clickhouse_asynchronous_metrics_v1",
			Category: "resources",
			SQL:      "SELECT metric, value FROM system.asynchronous_metrics",
		},
		{
			ID:       "clickhouse_settings_v1",
			Category: "config",
			SQL:      "SELECT name, value, changed FROM system.settings WHERE name IN ('max_threads', 'max_memory_usage', 'max_concurrent_queries', 'max_server_memory_usage', 'background_pool_size', 'readonly')",
		},
		{
			// #533: normalized_query_hash is ClickHouse's stable query identity
			// (a UInt64 hash of the query with literals stripped — the ClickHouse
			// analogue of a digest/queryid). It is NOT sensitive (a hash, not SQL
			// text), so it is selected as the typed workload identity while the raw
			// `query` stays HighSensitivity + redacted. Confirmed live against
			// ClickHouse 24.8 under GRANT SELECT ON system.*.
			ID:               "clickhouse_query_log_v1",
			Category:         "workload",
			SQL:              "SELECT type, query_duration_ms, read_rows, result_rows, memory_usage, normalized_query_hash, query FROM system.query_log WHERE type = 'QueryFinish' AND event_time > now() - INTERVAL 1 HOUR ORDER BY event_time DESC LIMIT 200",
			HighSensitivity:  true,
			SensitiveColumns: []string{"query"},
		},
		{
			ID:               "clickhouse_query_thread_log_v1",
			Category:         "workload",
			SQL:              "SELECT event_time, query_duration_ms, memory_usage, read_rows, thread_name, query FROM system.query_thread_log WHERE event_time > now() - INTERVAL 1 HOUR ORDER BY event_time DESC LIMIT 200",
			HighSensitivity:  true,
			SensitiveColumns: []string{"query"},
		},
	}
}

// Collect runs every collector in cat via run, capturing rows or a non-fatal
// error per collector, measuring each collector's wall-clock duration. When
// redactSensitive is true, each SensitiveColumns value is nilled in every row.
func Collect(ctx context.Context, run QueryRunner, cat []Collector, redactSensitive bool) []Result {
	return collectWithClock(ctx, run, cat, redactSensitive, time.Now)
}

func collectWithClock(ctx context.Context, run QueryRunner, cat []Collector, redactSensitive bool, now func() time.Time) []Result {
	results := make([]Result, 0, len(cat))
	for _, c := range cat {
		res := Result{CollectorID: c.ID, Category: c.Category}
		start := now()
		rows, err := run(ctx, c.SQL)
		res.DurationMS = int(now().Sub(start).Milliseconds())
		if err != nil {
			res.Err = err.Error()
			results = append(results, res)
			continue
		}
		if redactSensitive && len(c.SensitiveColumns) > 0 {
			for _, row := range rows {
				for _, col := range c.SensitiveColumns {
					if _, ok := row[col]; ok {
						row[col] = nil
					}
				}
			}
		}
		res.Rows = rows
		results = append(results, res)
	}
	return results
}

// IsPermissionDenied reports whether a ClickHouse error denotes an access/grant
// error (server exception code 497 ACCESS_DENIED / "not enough privileges").
// Used so a collector needing a grant the monitoring role lacks degrades to a
// skip rather than a fault.
func IsPermissionDenied(msg string) bool {
	if msg == "" {
		return false
	}
	m := strings.ToLower(msg)
	return strings.Contains(m, "access_denied") ||
		strings.Contains(m, "not enough privileges") ||
		strings.Contains(m, "code: 497")
}

// NewRunner adapts a *sql.DB into a QueryRunner that scans each row into a
// map[column]value. Read-only: it only executes the provided SELECT.
func NewRunner(db *sql.DB) QueryRunner {
	return func(ctx context.Context, query string) ([]map[string]any, error) {
		rows, err := db.QueryContext(ctx, query)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		cols, err := rows.Columns()
		if err != nil {
			return nil, err
		}
		var out []map[string]any
		for rows.Next() {
			cells := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range cells {
				ptrs[i] = &cells[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				return nil, err
			}
			row := make(map[string]any, len(cols))
			for i, name := range cols {
				row[name] = normalizeCell(cells[i])
			}
			out = append(out, row)
		}
		return out, rows.Err()
	}
}

// normalizeCell converts driver []byte values into strings so results marshal
// cleanly (mirrors the MySQL-wire runner).
func normalizeCell(v any) any {
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return v
}

// HasWrite reports whether sql begins with a non-SELECT verb (test guard for
// the read-only invariant, INV-CHB-01).
func HasWrite(sqlText string) bool {
	s := strings.ToUpper(strings.TrimSpace(sqlText))
	return !strings.HasPrefix(s, "SELECT")
}
