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
)

// Collector is one read-only SingleStore query and its handling metadata.
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
			ID:       "singlestore_cluster_status_v1",
			Category: "topology",
			SQL:      "SELECT NODE_ID, HOST, PORT, DATABASE_NAME, ROLE, STATE, POSITION FROM information_schema.MV_CLUSTER_STATUS",
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
	}
}

// Collect runs every collector in cat via run, capturing rows or a non-fatal
// error per collector (SSC-R002). When redactSensitive is true, each
// SensitiveColumns value is nilled in every row (SSC-R003).
func Collect(ctx context.Context, run QueryRunner, cat []Collector, redactSensitive bool) []Result {
	results := make([]Result, 0, len(cat))
	for _, c := range cat {
		res := Result{CollectorID: c.ID, Category: c.Category}
		rows, err := run(ctx, c.SQL)
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
