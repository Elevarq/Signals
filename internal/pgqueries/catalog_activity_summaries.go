package pgqueries

import "time"

// Activity summary collectors — aggregated views that provide the
// "what is happening right now" overview without per-session detail.
// Companions to the narrower activity collectors
// (long_running_txns_v1, idle_in_txn_offenders_v1, connection_utilization_v1,
// blocking_locks_v1) which emit per-session rows.

func init() {
	// pg_locks_summary_v1: counts of granted and waiting locks aggregated
	// by (locktype, mode, granted), with max wait duration for waiting
	// rows. No per-relation / per-tuple identity — that's the
	// blocking_locks_v1 collector's job.
	//
	// Specification: specifications/collectors/pg_locks_summary_v1.md
	Register(QueryDef{
		ID:       "pg_locks_summary_v1",
		Category: "activity",
		SQL: `SELECT
			l.locktype,
			l.mode,
			l.granted,
			count(*)::int                                                AS count,
			CASE WHEN l.granted
			     THEN NULL
			     ELSE MAX(EXTRACT(EPOCH FROM (now() - a.query_start)))::bigint
			END                                                          AS max_wait_seconds,
			count(DISTINCT l.pid)::int                                   AS distinct_pids
		FROM pg_locks l
		LEFT JOIN pg_stat_activity a ON a.pid = l.pid
		WHERE l.pid IS DISTINCT FROM pg_backend_pid()
		GROUP BY l.locktype, l.mode, l.granted
		ORDER BY l.granted DESC, l.locktype, l.mode`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionShort,
		Timeout:        5 * time.Second,
		Cadence:        Cadence5m,
	})

	// pg_stat_activity_summary_v1: aggregated session-state counts and
	// age distributions. Single row, no per-session identifiers — the
	// reporting-layer "overview glance" before drilling into detail.
	// Uses FILTER aggregates + jsonb_object_agg subqueries for the
	// backend-type / wait-event-type breakdowns.
	//
	// Specification: specifications/collectors/pg_stat_activity_summary_v1.md
	Register(QueryDef{
		ID:       "pg_stat_activity_summary_v1",
		Category: "activity",
		SQL: `WITH activity AS (
			SELECT pid, state, backend_type, xact_start, query_start,
			       state_change, backend_xmin, wait_event, wait_event_type
			FROM pg_stat_activity
			WHERE pid IS DISTINCT FROM pg_backend_pid()
		)
		SELECT
			count(*) FILTER (WHERE backend_type = 'client backend')::int                        AS total_backends,
			count(*) FILTER (WHERE state = 'active')::int                                        AS active_count,
			count(*) FILTER (WHERE state = 'idle')::int                                          AS idle_count,
			count(*) FILTER (WHERE state = 'idle in transaction')::int                           AS idle_in_transaction_count,
			count(*) FILTER (WHERE state = 'idle in transaction (aborted)')::int                 AS idle_in_transaction_aborted_count,
			count(*) FILTER (WHERE state LIKE 'fastpath%')::int                                  AS fastpath_count,
			count(*) FILTER (WHERE state = 'disabled')::int                                      AS disabled_count,
			count(*) FILTER (WHERE wait_event IS NOT NULL AND state = 'active')::int             AS waiting_count,
			EXTRACT(EPOCH FROM (now() - MIN(xact_start) FILTER (WHERE xact_start IS NOT NULL)))::bigint
			                                                                                     AS oldest_xact_age_seconds,
			EXTRACT(EPOCH FROM (now() - MIN(query_start) FILTER (WHERE state = 'active')))::bigint
			                                                                                     AS oldest_query_age_seconds,
			MAX(age(backend_xmin)) FILTER (WHERE backend_xmin IS NOT NULL)::bigint
			                                                                                     AS oldest_backend_xmin_age_xids,
			count(*) FILTER (WHERE state = 'active' AND now() - query_start > interval '1 minute')::int
			                                                                                     AS active_gt_1min,
			count(*) FILTER (WHERE state = 'active' AND now() - query_start > interval '5 minutes')::int
			                                                                                     AS active_gt_5min,
			count(*) FILTER (WHERE state = 'active' AND now() - query_start > interval '1 hour')::int
			                                                                                     AS active_gt_1h,
			count(*) FILTER (WHERE state IN ('idle in transaction', 'idle in transaction (aborted)')
			                    AND now() - state_change > interval '1 minute')::int
			                                                                                     AS long_idle_in_txn_count,
			COALESCE(
			    (SELECT jsonb_object_agg(backend_type, c)
			     FROM (SELECT backend_type, count(*)::int AS c FROM activity
			           WHERE backend_type IS NOT NULL
			           GROUP BY backend_type) t),
			    '{}'::jsonb
			)                                                                                    AS by_backend_type,
			COALESCE(
			    (SELECT jsonb_object_agg(wait_event_type, c)
			     FROM (SELECT wait_event_type, count(*)::int AS c FROM activity
			           WHERE wait_event IS NOT NULL AND state = 'active' AND wait_event_type IS NOT NULL
			           GROUP BY wait_event_type) t),
			    '{}'::jsonb
			)                                                                                    AS by_wait_event_type
		FROM activity`,
		ResultKind:     ResultScalar,
		RetentionClass: RetentionShort,
		Timeout:        5 * time.Second,
		Cadence:        Cadence5m,
	})
}
