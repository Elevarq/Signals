package pgqueries

import "time"

func init() {
	// Database-level wraparound signals.
	// age(datfrozenxid) works on all PG versions.
	// mxid_age(datminmxid) exists since PG 9.3 but we target PG 10+.
	// current_setting() is safe even in restricted roles — it reads
	// server config without requiring superuser.
	Register(QueryDef{
		ID:           "wraparound_db_level_v1",
		Category:     "wraparound",
		MinPGVersion: 10,
		SQL: `SELECT
			datname,
			age(datfrozenxid)                                              AS db_xid_age,
			current_setting('autovacuum_freeze_max_age')::bigint           AS freeze_max_age,
			mxid_age(datminmxid)                                           AS db_mxid_age,
			current_setting('autovacuum_multixact_freeze_max_age')::bigint AS multixact_freeze_max_age
		FROM pg_database
		WHERE datallowconn
		ORDER BY age(datfrozenxid) DESC`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        10 * time.Second,
		Cadence:        CadenceDaily,
	})

	// Relation-level wraparound signals for user tables + TOAST.
	// Fetches top 200 relations by XID age as a prefilter; final
	// ranking is done in Go. pg_relation_size / pg_total_relation_size
	// are read-only and safe.
	//
	// relminmxid / mxid_age() exist since PG 9.3; we target PG 10+.
	// last_vacuum / last_autovacuum come from pg_stat_all_tables.
	//
	// Permission note: pg_relation_size() requires SELECT privilege on
	// the relation or membership in pg_read_all_stats (PG 10+). If the
	// monitoring role lacks access, the function returns NULL and the
	// COALESCE ensures the row is still emitted with size = 0 so the
	// ranking falls back to urgency-only.
	Register(QueryDef{
		ID:           "wraparound_rel_level_v1",
		Category:     "wraparound",
		MinPGVersion: 10,
		SQL: `SELECT
			n.nspname                                      AS schema,
			c.relname,
			c.relkind,
			c.oid,
			COALESCE(pg_relation_size(c.oid), 0)           AS table_bytes,
			COALESCE(pg_total_relation_size(c.oid), 0)     AS total_bytes,
			age(c.relfrozenxid)                            AS rel_xid_age,
			mxid_age(c.relminmxid)                         AS rel_mxid_age,
			s.last_vacuum,
			s.last_autovacuum,
			array_to_string(c.reloptions, ', ')            AS reloptions
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN pg_stat_all_tables s ON s.relid = c.oid
		WHERE c.relkind IN ('r', 't')
		  AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND COALESCE(pg_relation_size(c.oid), 0) > 0
		ORDER BY age(c.relfrozenxid) DESC
		LIMIT 200`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        15 * time.Second,
		Cadence:        CadenceDaily,
	})

	// Potential freeze blockers: long-running transactions that hold back
	// the XID freeze horizon.
	//
	// pg_stat_activity is readable by pg_monitor / pg_read_all_stats (PG 10+).
	// If the monitoring role cannot see other backends, the query returns only
	// its own row, which the ranker filters out as uninteresting.
	Register(QueryDef{
		ID:           "wraparound_blockers_v1",
		Category:     "wraparound",
		MinPGVersion: 10,
		SQL: `SELECT
			'long_tx'                              AS blocker_type,
			pid::text                              AS identifier,
			usename,
			application_name,
			age(backend_xmin)                      AS xmin_age,
			state,
			left(query, 200)                       AS query_snippet,
			extract(epoch FROM now() - xact_start) AS xact_age_seconds
		FROM pg_stat_activity
		WHERE xact_start IS NOT NULL
		  AND pid != pg_backend_pid()
		  AND state != 'idle'
		ORDER BY xact_start ASC
		LIMIT 20`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionShort,
		Timeout:        10 * time.Second,
		Cadence:        Cadence5m,
	})
}
