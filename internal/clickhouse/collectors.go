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

// Catalog returns the validated ClickHouse collector set. CH-1 is the backend
// foundation: a single identity collector proves the end-to-end
// connect→identify→collect→persist→export path. The full system.* catalog
// (clusters/databases/tables/parts/replicas/merges/mutations/metrics/settings/
// query_log) is CH-2. Every statement is a read-only SELECT (INV-CHB-01),
// confirmed live against ClickHouse 24.8.
func Catalog() []Collector {
	return []Collector{
		{
			ID:       "clickhouse_identity_v1",
			Category: "server",
			SQL:      "SELECT version() AS version, uptime() AS uptime_s",
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
