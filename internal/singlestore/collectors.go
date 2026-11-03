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
	// StructuralColumn (#647) names the single server-normalized column this
	// collector may surface under the dedicated structural-query opt-in,
	// independent of the generic high-sensitivity switch. Empty = the collector
	// has no structural column and this capability never surfaces any of its
	// SensitiveColumns (SQT-R010). Set only on the MySQL/MariaDB digest
	// collectors (= "DIGEST_TEXT").
	StructuralColumn string
	// ElevatedPrivilege, when non-empty, names a SingleStore privilege beyond
	// the baseline monitoring grant (SELECT + PROCESS) that this collector
	// requires — e.g. "CLUSTER" for MV_CLUSTER_STATUS. A permission-denied
	// error from an elevated collector run under the baseline role is an
	// EXPECTED degrade (skip), not a fault (#554, least-privilege). Empirically
	// determined against a live workspace.
	ElevatedPrivilege string
	// PerfSchemaGated (#665) marks a collector whose source table is populated
	// only when the server's performance_schema is ON — the MySQL/MariaDB
	// statement-digest collectors read
	// performance_schema.events_statements_summary_by_digest, which is EMPTY
	// when performance_schema is OFF (the MariaDB default). A bare 0-row result
	// is ambiguous ("perf_schema ON, no statements" vs "perf_schema OFF,
	// unavailable"), so on a 0-row no-error result Collect fires the read-only
	// probe perfSchemaProbeSQL; if it reports OFF, the run is recorded as a
	// DEGRADED / NOT_ASSESSED skip (ReasonPerfSchemaDisabled), not a silent
	// success (MDC-R010). A collector that returned rows is never probed.
	PerfSchemaGated bool
}

// ReasonPerfSchemaDisabled is the canonical skip reason recorded when a
// PerfSchemaGated collector returns 0 rows and the server's performance_schema
// is OFF — a DEGRADED / NOT_ASSESSED outcome, distinct from an empty success or
// a collector error (#665, MDC-R011). Pinned to metrics.CollectorSkippedReasons
// by a drift test in the collector package.
const ReasonPerfSchemaDisabled = "perf_schema_disabled"

// perfSchemaProbeSQL is the cheap read-only probe fired at most once, only on a
// PerfSchemaGated collector's 0-row no-error result, to tell perf_schema OFF
// from "enabled but empty". SELECT-only, so INV-SSC-01 / INV-MDC-01 hold.
const perfSchemaProbeSQL = "SELECT @@performance_schema AS performance_schema"

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
	// Status / Reason carry a runtime-determined outcome that the collector
	// could not know from its SQL alone (#665). When Collect detects a
	// PerfSchemaGated collector returned 0 rows with perf_schema OFF, it sets
	// Status="skipped" and Reason=ReasonPerfSchemaDisabled — a DEGRADED /
	// NOT_ASSESSED signal the persistence layer records verbatim, distinct from
	// a plain empty success (both empty) or an Err failure. Empty for every
	// ordinary collector outcome; the persistence layer treats empty Status as
	// "classify from Err/rows as before".
	Status string
	Reason string
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
		{
			// #618: native streaming-ingest pipelines. CONFIG_JSON embeds the
			// source connection (Kafka/S3) config, which can carry credentials ->
			// HighSensitivity + redacted; operational fields (name/state/user/time)
			// are preserved. Empty when no pipelines exist is a legitimate success.
			// Baseline SELECT, PROCESS grant.
			ID:               "singlestore_pipelines_v1",
			Category:         "workload",
			SQL:              "SELECT DATABASE_NAME, PIPELINE_ID, PIPELINE_NAME, CONFIG_JSON, STATE, SKIPPED_BATCH_PARTITIONS, CREATE_TIME, ALTER_TIME, CREATE_USER, ALTER_USER FROM information_schema.PIPELINES",
			HighSensitivity:  true,
			SensitiveColumns: []string{"CONFIG_JSON"},
		},
		{
			// #618: per-node memory from the distributed sysinfo view (host /
			// cgroup / memsql bytes). Baseline SELECT, PROCESS grant.
			ID:       "singlestore_sysinfo_memory_v1",
			Category: "resources",
			SQL:      "SELECT NODE_ID, IP_ADDR, PORT, TYPE, HOST_TOTAL_B, HOST_USED_B, CGROUP_TOTAL_B, CGROUP_USED_B, MEMSQL_B, TIMESTAMP_NS FROM information_schema.MV_SYSINFO_MEM",
		},
		{
			// #618: per-node disk usage + cumulative IO from the distributed
			// sysinfo view. Baseline SELECT, PROCESS grant.
			ID:       "singlestore_sysinfo_disk_v1",
			Category: "resources",
			SQL:      "SELECT NODE_ID, IP_ADDR, PORT, TYPE, MOUNT_POINT, MOUNT_TOTAL_B, MOUNT_USED_B, READ_CUMULATIVE_B, WRITE_CUMULATIVE_B, TIMESTAMP_NS FROM information_schema.MV_SYSINFO_DISK",
		},
		{
			// #618: cluster lifecycle events (severity/type/details). DETAILS is
			// event metadata (db name/status), not SQL or credentials, so it is kept
			// as native evidence. Requires CLUSTER (confirmed live: baseline
			// SELECT+PROCESS returns ERROR 1227) -> ElevatedPrivilege, degrades to a
			// skip without it. Bounded, most-recent first.
			ID:                "singlestore_mv_events_v1",
			Category:          "activity",
			SQL:               "SELECT ORIGIN_NODE_ID, EVENT_TIME, SEVERITY, EVENT_TYPE, DETAILS FROM information_schema.MV_EVENTS ORDER BY EVENT_TIME DESC LIMIT 200",
			ElevatedPrivilege: "CLUSTER",
		},
		{
			// #618: per-node capacity inventory (cpu/memory/disk/uptime/version) —
			// richer than aggregators/leaves. Baseline SELECT, PROCESS grant.
			ID:       "singlestore_nodes_v1",
			Category: "topology",
			SQL:      "SELECT ID, IP_ADDR, PORT, TYPE, STATE, AVAILABILITY_GROUP, NUM_CPUS, MAX_MEMORY_MB, MAX_TABLE_MEMORY_MB, MEMORY_USED_MB, TABLE_MEMORY_USED_MB, TOTAL_DATA_DISK_MB, AVAILABLE_DATA_DISK_MB, UPTIME, VERSION FROM information_schema.MV_NODES",
		},
	}
}

// Collect runs every collector in cat via run, capturing rows or a non-fatal
// error per collector (SSC-R002). When redactSensitive is true, each
// SensitiveColumns value is nilled in every row (SSC-R003). Each collector's
// wall-clock duration is measured (MKR-R006).
func Collect(ctx context.Context, run QueryRunner, cat []Collector, redactSensitive, structuralQueryText bool) []Result {
	return CollectWithClock(ctx, run, cat, redactSensitive, structuralQueryText, time.Now)
}

// CollectWithClock is Collect with an injectable clock so the per-collector
// duration measurement (MKR-R006) is deterministically testable. Production
// passes time.Now; a test passes a step clock and asserts the measured delta.
func CollectWithClock(ctx context.Context, run QueryRunner, cat []Collector, redactSensitive, structuralQueryText bool, now func() time.Time) []Result {
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
		applyWireRedaction(c, rows, redactSensitive, structuralQueryText)
		res.Rows = rows
		// #665 (MDC-R010): a PerfSchemaGated collector that returned 0 rows with
		// no error is ambiguous. Probe @@performance_schema (read-only, at most
		// once, only on this 0-row path) to tell "perf_schema OFF -> unavailable"
		// from "perf_schema ON -> no statements". OFF -> DEGRADED / NOT_ASSESSED
		// skip; ON (or a probe that errors, MDC-R011) -> leave the empty success
		// intact. A collector that returned rows is never probed.
		if c.PerfSchemaGated && len(rows) == 0 {
			if off, ok := perfSchemaIsOff(ctx, run); ok && off {
				res.Status = "skipped"
				res.Reason = ReasonPerfSchemaDisabled
			}
		}
		results = append(results, res)
	}
	return results
}

// perfSchemaIsOff runs the read-only @@performance_schema probe and reports
// (off, ok): ok is false when the probe errored or returned no usable value (in
// which case the caller leaves the pre-probe outcome untouched, MDC-R011); when
// ok is true, off reports whether performance_schema is OFF. Accepts the several
// shapes MySQL-wire drivers return the boolean system variable as: "0"/"1",
// "OFF"/"ON", []byte of those, or a numeric/bool.
func perfSchemaIsOff(ctx context.Context, run QueryRunner) (off, ok bool) {
	rows, err := run(ctx, perfSchemaProbeSQL)
	if err != nil || len(rows) == 0 {
		return false, false
	}
	v, present := rows[0]["performance_schema"]
	if !present {
		return false, false
	}
	switch t := v.(type) {
	case string:
		return perfSchemaStringIsOff(t), true
	case []byte:
		return perfSchemaStringIsOff(string(t)), true
	case bool:
		return !t, true
	case int:
		return t == 0, true
	case int64:
		return t == 0, true
	case float64:
		return t == 0, true
	default:
		return false, false
	}
}

// perfSchemaStringIsOff reports whether a textual @@performance_schema value
// denotes OFF. MySQL/MariaDB report the variable as "0"/"1" or "OFF"/"ON".
func perfSchemaStringIsOff(s string) bool {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "0", "OFF", "FALSE", "NO":
		return true
	default:
		return false
	}
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
