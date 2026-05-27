package pgqueries

// catalog_pg14.go — version-specific overrides for PostgreSQL 14.
//
// PG 14 is the oldest supported major (R081). No collector currently
// requires a PG 14-specific SQL variant: every collector that's eligible
// for PG 14 uses the version-agnostic SQL from the default catalog. If
// a future PG 14 schema-deprecation forces a fork, register it here via
// RegisterOverride(14, "<query_id>", "<sql>").
//
// This file exists to preserve the per-major file layout described in
// the spec and to give the per-version catalog list a consistent
// shape: catalog_pg14.go … catalog_pg18.go each define the exception
// set for their major.

// pgStatDatabaseV14SQL is the PG 14+ variant of pg_stat_database_v1:
// identical to the default except the seven session/timing fields carry
// the real columns from pg_stat_database instead of the NULL stubs the
// default emits for PG 10–13 (#210). The view shape is unchanged across
// PG 14–18, so all five majors register this same override.
const pgStatDatabaseV14SQL = `SELECT datid, datname, numbackends, xact_commit, xact_rollback,
		blks_read, blks_hit, tup_returned, tup_fetched, tup_inserted, tup_updated,
		tup_deleted, conflicts, temp_files, temp_bytes, deadlocks, blk_read_time,
		blk_write_time,
		session_time,
		active_time,
		idle_in_transaction_time,
		sessions,
		sessions_abandoned,
		sessions_fatal,
		sessions_killed,
		stats_reset
		FROM pg_stat_database
		WHERE datname IS NOT NULL
		ORDER BY datname`

func init() {
	// #210 — real PG 14+ session/timing columns (NULL-stubbed in the
	// default SQL for PG 10–13).
	RegisterOverride(14, "pg_stat_database_v1", pgStatDatabaseV14SQL)
}
