// Package singlestore collects read-only diagnostic evidence from a SingleStore
// instance over the MySQL wire (via internal/mysqlwire) and returns per-collector
// results for a snapshot that declares engine=singlestore.
//
// The catalog SQL and result columns are validated against a live SingleStore
// workspace (see specifications/singlestore-collectors.md) — not authored
// blind. Collection is read-only (SELECT only, INV-SSC-01) and never touches
// the PostgreSQL path.
package singlestore

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// Collector is one read-only SingleStore query and its handling metadata.
type Collector struct {
	ID               string
	Category         string
	SQL              string
	HighSensitivity  bool
	SensitiveColumns []string
	// ElevatedPrivilege, when non-empty, names a SingleStore privilege beyond
	// the baseline monitoring grant (SELECT + PROCESS) that this collector
	// requires — e.g. "CLUSTER" for MV_CLUSTER_STATUS. A permission-denied
	// error from an elevated collector run under the baseline role is an
	// EXPECTED degrade (skip), not a fault (#554, least-privilege). Empirically
	// determined against a live workspace.
	ElevatedPrivilege string
}

// Result is the outcome of running one Collector.
type Result struct {
	CollectorID string
	Category    string
	Rows        []map[string]any
	Err         string
	// DurationMS is the measured wall-clock duration of the collector's
	// statement, in milliseconds (MKR-R006). It is a real measurement, never
	// a hardcoded 0, so a downstream consumer can tell "ran in 0ms" from
	// "not measured".
	DurationMS int
	// ElevatedPrivilege mirrors the Collector field: when non-empty, a
	// permission-denied error from this collector is an EXPECTED degrade
	// (skipped), not a fault (MKR-R007). Carried onto the Result so the
	// persistence layer can classify without re-consulting the catalog.
	ElevatedPrivilege string
}

// QueryRunner runs a single read-only statement and returns its rows as maps.
// A seam so Collect is unit-testable without a live server.
type QueryRunner func(ctx context.Context, query string) ([]map[string]any, error)

// Catalog returns the validated SingleStore collector set. Every statement is a
// read-only SELECT over information_schema MV_*/cluster views; columns were
// confirmed live against a SingleStore S-00 workspace.
func Catalog() []Collector {
	return []Collector{
		{
			ID:       "singlestore_identity_v1",
			Category: "server",
			SQL:      "SELECT @@memsql_version AS memsql_version, @@version_comment AS version_comment, VERSION() AS version",
		},
		{
			ID:       "singlestore_aggregators_v1",
			Category: "topology",
			SQL:      "SELECT HOST, PORT, ROLE, STATE, NODE_ID, AVAILABILITY_GROUP, OPENED_CONNECTIONS, AVERAGE_ROUNDTRIP_LATENCY FROM information_schema.AGGREGATORS",
		},
		{
			ID:       "singlestore_leaves_v1",
			Category: "topology",
			SQL:      "SELECT HOST, PORT, AVAILABILITY_GROUP, PAIR_HOST, PAIR_PORT, STATE, NODE_ID, OPENED_CONNECTIONS FROM information_schema.LEAVES",
		},
		{
			ID:                "singlestore_cluster_status_v1",
			Category:          "topology",
			SQL:               "SELECT NODE_ID, HOST, PORT, DATABASE_NAME, ROLE, STATE, POSITION FROM information_schema.MV_CLUSTER_STATUS",
			ElevatedPrivilege: "CLUSTER", // needs CLUSTER beyond baseline SELECT+PROCESS (#554); degrades to skip without it
		},
		{
			ID:       "singlestore_table_statistics_v1",
			Category: "schema",
			SQL:      "SELECT DATABASE_NAME, TABLE_NAME, PARTITION_TYPE, STORAGE_TYPE, ROWS, MEMORY_USE FROM information_schema.TABLE_STATISTICS",
		},
		{
			ID:               "singlestore_plancache_v1",
			Category:         "workload",
			SQL:              "SELECT DATABASE_NAME, PLAN_ID, QUERY_TEXT, COMMITS, EXECUTION_TIME, AVERAGE_EXEC_TIME, ROWCOUNT, CPU_TIME, LAST_EXECUTED FROM information_schema.MV_PLANCACHE",
			HighSensitivity:  true,
			SensitiveColumns: []string{"QUERY_TEXT"},
		},
		{
			ID:       "singlestore_global_variables_v1",
			Category: "config",
			SQL:      "SELECT VARIABLE_NAME, VARIABLE_VALUE FROM information_schema.MV_GLOBAL_VARIABLES WHERE VARIABLE_NAME IN ('maximum_memory', 'maximum_table_memory', 'max_connections', 'workload_management_max_queue_depth')",
		},
		{
			ID:       "singlestore_activities_v1",
			Category: "resources",
			SQL:      "SELECT ACTIVITY_NAME, ACTIVITY_TYPE, DATABASE_NAME, CPU_TIME_MS, ELAPSED_TIME_MS, MEMORY_BS, RUN_COUNT FROM information_schema.MV_ACTIVITIES",
		},
		{
			ID:               "singlestore_processlist_v1",
			Category:         "activity",
			SQL:              "SELECT NODE_ID, ID, USER, HOST, DB, COMMAND, TIME, STATE, INFO, PLAN_ID, TRANSACTION_STATE FROM information_schema.MV_PROCESSLIST",
			HighSensitivity:  true,
			SensitiveColumns: []string{"INFO"},
		},
		{
			ID:       "singlestore_distributed_partitions_v1",
			Category: "resources",
			SQL:      "SELECT DATABASE_NAME, ORDINAL, HOST, PORT, ROLE, STATE, NODE_ID, IS_OFFLINE FROM information_schema.DISTRIBUTED_PARTITIONS",
		},
	}
}

// Collect runs every collector in cat via run, capturing rows or a non-fatal
// error per collector (SSC-R002). When redactSensitive is true, each
// SensitiveColumns value is nilled in every row (SSC-R003). Each collector's
// wall-clock duration is measured (MKR-R006).
func Collect(ctx context.Context, run QueryRunner, cat []Collector, redactSensitive bool) []Result {
	return CollectWithClock(ctx, run, cat, redactSensitive, time.Now)
}

// CollectWithClock is Collect with an injectable clock so the per-collector
// duration measurement (MKR-R006) is deterministically testable. Production
// passes time.Now; a test passes a step clock and asserts the measured delta.
func CollectWithClock(ctx context.Context, run QueryRunner, cat []Collector, redactSensitive bool, now func() time.Time) []Result {
	results := make([]Result, 0, len(cat))
	for _, c := range cat {
		res := Result{CollectorID: c.ID, Category: c.Category, ElevatedPrivilege: c.ElevatedPrivilege}
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

// IsPermissionDenied reports whether a MySQL-wire error string denotes a
// privilege error (MKR-R007). SingleStore surfaces these as MySQL error codes
// 1142 (command denied), 1227 (access denied; need privilege), and 1045
// (access denied for user). Matching is on the textual error the driver
// returns (the collector layer only has the string form).
func IsPermissionDenied(msg string) bool {
	if msg == "" {
		return false
	}
	m := strings.ToLower(msg)
	if strings.Contains(m, "command denied") || strings.Contains(m, "access denied") {
		return true
	}
	for _, code := range []string{"error 1142", "error 1227", "error 1045"} {
		if strings.Contains(m, code) {
			return true
		}
	}
	return false
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

// normalizeCell converts driver []byte values (MySQL returns most text/numeric
// columns as bytes) into strings so results marshal cleanly.
func normalizeCell(v any) any {
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return v
}

// HasWrite is a defensive guard used in tests to assert catalog statements are
// read-only (SSC-R001): it reports whether sql begins with a non-SELECT verb.
func HasWrite(sqlText string) bool {
	s := strings.ToUpper(strings.TrimSpace(sqlText))
	return !strings.HasPrefix(s, "SELECT")
}
