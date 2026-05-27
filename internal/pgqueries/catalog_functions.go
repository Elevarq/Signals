package pgqueries

import "time"

// Function-level collectors — execution counters for user-schema functions
// and per-function GUC overrides. Feed the function-hint-candidate detector.

func init() {
	// pg_stat_user_functions_v1: per-function execution counters.
	// Populated only when track_functions is 'pl' or 'all'; the view
	// returns an empty result under the default 'none'.
	//
	// Specification: specifications/collectors/pg_stat_user_functions_v1.md
	Register(QueryDef{
		ID:       "pg_stat_user_functions_v1",
		Category: "functions",
		SQL: `SELECT
			funcid,
			schemaname,
			funcname,
			calls,
			total_time,
			self_time
		FROM pg_stat_user_functions
		ORDER BY total_time DESC NULLS LAST, funcid ASC`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        10 * time.Second,
		Cadence:        Cadence1h,
	})

	// pg_proc_config_v1: functions with per-function SET overrides
	// (pg_proc.proconfig IS NOT NULL). Prevents the function-hint advisor
	// from recommending SETs that are already in place, and enables drift
	// detection when an advised SET has been removed.
	//
	// Specification: specifications/collectors/pg_proc_config_v1.md
	Register(QueryDef{
		ID:       "pg_proc_config_v1",
		Category: "schema",
		SQL: `SELECT
			p.oid                                        AS funcid,
			n.nspname                                    AS schemaname,
			p.proname                                    AS funcname,
			p.proargtypes::text                          AS proargtypes_oids,
			l.lanname                                    AS prolang_name,
			p.provolatile,
			p.proisstrict,
			p.prosecdef,
			p.proconfig
		FROM pg_proc p
		JOIN pg_namespace n ON n.oid = p.pronamespace
		JOIN pg_language l ON l.oid = p.prolang
		WHERE p.proconfig IS NOT NULL
		  AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND n.nspname NOT LIKE 'pg_temp_%'
		  AND n.nspname NOT LIKE 'pg_toast_temp_%'
		ORDER BY n.nspname, p.proname, p.oid`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        15 * time.Second,
		Cadence:        CadenceDaily,
	})
}
