package pgqueries

import "time"

func init() {
	Register(QueryDef{
		ID:             "pg_version_v1",
		Category:       "server",
		SQL:            `SELECT version()`,
		ResultKind:     ResultScalar,
		RetentionClass: RetentionLong,
		Timeout:        5 * time.Second,
		Cadence:        Cadence6h,
	})

	Register(QueryDef{
		ID:       "pg_settings_v1",
		Category: "server",
		// context, vartype, boot_val, reset_val let downstream
		// detectors distinguish runtime-settable GUCs (`user` /
		// `superuser` / `superuser-backend` / `backend` / `sighup`)
		// from restart-required ones (`postmaster` / `internal`)
		// and report drift vs. the compile-time default. min_val,
		// max_val, enumvals carry bounds for safe sweep ranges;
		// short_desc is the operator-facing label PostgreSQL ships.
		// sourcefile / sourceline are NULL on managed platforms and
		// excluded for cross-platform safety (FC-01).
		SQL: `SELECT name, setting, unit, category, source, pending_restart,
		context, vartype, boot_val, reset_val,
		min_val, max_val, enumvals, short_desc
		FROM pg_settings ORDER BY name`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionLong,
		Timeout:        10 * time.Second,
		Cadence:        Cadence6h,
	})

	Register(QueryDef{
		ID:       "pg_stat_activity_v1",
		Category: "activity",
		SQL: `SELECT pid, datname, usename, application_name, client_addr,
		backend_start, xact_start, query_start, state_change, wait_event_type,
		wait_event, state, backend_type
		FROM pg_stat_activity
		WHERE pid != pg_backend_pid()
		ORDER BY pid`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionShort,
		Timeout:        10 * time.Second,
		Cadence:        Cadence5m,
	})

	// Default SQL serves PG 10–13: the PG 14+ session/timing fields are
	// emitted as typed NULL stubs so the column set is stable across
	// majors (#210). PG 14–18 get the real columns via RegisterOverride
	// (see catalog_pg14.go … catalog_pg18.go).
	Register(QueryDef{
		ID:       "pg_stat_database_v1",
		Category: "database",
		SQL: `SELECT datid, datname, numbackends, xact_commit, xact_rollback,
		blks_read, blks_hit, tup_returned, tup_fetched, tup_inserted, tup_updated,
		tup_deleted, conflicts, temp_files, temp_bytes, deadlocks, blk_read_time,
		blk_write_time,
		NULL::double precision AS session_time,
		NULL::double precision AS active_time,
		NULL::double precision AS idle_in_transaction_time,
		NULL::bigint AS sessions,
		NULL::bigint AS sessions_abandoned,
		NULL::bigint AS sessions_fatal,
		NULL::bigint AS sessions_killed,
		stats_reset
		FROM pg_stat_database
		WHERE datname IS NOT NULL
		ORDER BY datname`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        10 * time.Second,
		Cadence:        Cadence15m,
	})

	Register(QueryDef{
		ID:       "pg_stat_user_tables_v1",
		Category: "tables",
		SQL: `SELECT schemaname, relname, seq_scan, seq_tup_read,
		idx_scan, idx_tup_fetch, n_tup_ins, n_tup_upd, n_tup_del, n_tup_hot_upd,
		n_live_tup, n_dead_tup, last_vacuum, last_autovacuum, last_analyze,
		last_autoanalyze, vacuum_count, autovacuum_count, analyze_count,
		autoanalyze_count
		FROM pg_stat_user_tables
		ORDER BY schemaname, relname`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        10 * time.Second,
	})

	Register(QueryDef{
		ID:       "pg_stat_user_indexes_v1",
		Category: "indexes",
		SQL: `SELECT schemaname, relname, indexrelname, idx_scan,
		idx_tup_read, idx_tup_fetch
		FROM pg_stat_user_indexes
		ORDER BY schemaname, relname, indexrelname`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        10 * time.Second,
	})

	Register(QueryDef{
		ID:       "pg_statio_user_tables_v1",
		Category: "io",
		SQL: `SELECT schemaname, relname, heap_blks_read,
		heap_blks_hit, idx_blks_read, idx_blks_hit, toast_blks_read,
		toast_blks_hit, tidx_blks_read, tidx_blks_hit
		FROM pg_statio_user_tables
		ORDER BY schemaname, relname`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        10 * time.Second,
	})

	Register(QueryDef{
		ID:       "pg_statio_user_indexes_v1",
		Category: "io",
		SQL: `SELECT schemaname, relname, indexrelname,
		idx_blks_read, idx_blks_hit
		FROM pg_statio_user_indexes
		ORDER BY schemaname, relname, indexrelname`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        10 * time.Second,
	})

	// pg_stat_statements keeps a wildcard projection (`s.*`) for
	// cross-version compatibility. The view schema varies across
	// PostgreSQL and extension versions (e.g. blk_read_time was
	// renamed to shared_blk_read_time in PG 17); the collector
	// captures whatever columns the installed version exposes and
	// serializes them dynamically using actual column names. Signals
	// does not rank or limit statements; Analyzer owns workload
	// selection such as top-N by total execution time.
	//
	// R106 self-filter:
	//   1. Scope to the connected database via a join on pg_database,
	//      equivalent to `s.dbid = (SELECT oid FROM pg_database WHERE
	//      datname = current_database())`. Cross-database rows from
	//      other databases on the same cluster are excluded.
	//   2. Suppress rows attributable to Signals' own sessions via a
	//      NOT EXISTS correlated subquery against pg_stat_activity
	//      where application_name = 'arq-signals', matching on
	//      (userid ↔ usesysid, dbid ↔ datid). pg_stat_statements does
	//      not carry application_name directly so the (user, db)
	//      attribution is the safest available proxy.
	Register(QueryDef{
		ID:                "pg_stat_statements_v1",
		Category:          "extensions",
		RequiresExtension: "pg_stat_statements",
		SQL: `SELECT s.*
		FROM pg_stat_statements s
		JOIN pg_database d ON d.oid = s.dbid
		WHERE d.datname = current_database()
		  AND NOT EXISTS (
		    SELECT 1 FROM pg_stat_activity a
		    WHERE a.application_name = 'arq-signals'
		      AND a.usesysid = s.userid
		      AND a.datid = s.dbid
		  )`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        10 * time.Second,
		Cadence:        Cadence15m,
	})
}
