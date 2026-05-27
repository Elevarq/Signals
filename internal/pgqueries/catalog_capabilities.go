package pgqueries

import "time"

// Capability fingerprinting — documents what the monitoring role can actually
// see. Feeds the analyzer's EvidenceCompleteness model so coverage notes
// distinguish "insufficient_privilege" from "extension_not_installed" from
// "collector_empty".

func init() {
	// pg_role_capabilities_v1: single-row capability matrix for the
	// connected role. Each is_pg_* flag is guarded with a pg_roles
	// existence check so missing built-in roles on older PG point
	// releases (e.g. pg_read_all_settings prior to PG 10.5) return
	// false instead of raising.
	//
	// Specification: specifications/collectors/pg_role_capabilities_v1.md
	Register(QueryDef{
		ID:       "pg_role_capabilities_v1",
		Category: "security",
		SQL: `SELECT
			session_user::text                                   AS "session_user",
			current_user::text                                   AS "current_user",
			r.rolsuper                                           AS is_superuser,
			COALESCE((SELECT pg_has_role(current_user, 'pg_monitor', 'MEMBER')
			          FROM pg_roles WHERE rolname = 'pg_monitor'), false)
			                                                     AS is_pg_monitor,
			COALESCE((SELECT pg_has_role(current_user, 'pg_read_all_stats', 'MEMBER')
			          FROM pg_roles WHERE rolname = 'pg_read_all_stats'), false)
			                                                     AS is_pg_read_all_stats,
			COALESCE((SELECT pg_has_role(current_user, 'pg_read_all_settings', 'MEMBER')
			          FROM pg_roles WHERE rolname = 'pg_read_all_settings'), false)
			                                                     AS is_pg_read_all_settings,
			COALESCE((SELECT pg_has_role(current_user, 'pg_read_server_files', 'MEMBER')
			          FROM pg_roles WHERE rolname = 'pg_read_server_files'), false)
			                                                     AS is_pg_read_server_files,
			COALESCE((SELECT pg_has_role(current_user, 'pg_signal_backend', 'MEMBER')
			          FROM pg_roles WHERE rolname = 'pg_signal_backend'), false)
			                                                     AS is_pg_signal_backend,
			(r.rolsuper
			    OR COALESCE((SELECT pg_has_role(current_user, 'pg_monitor', 'MEMBER')
			                 FROM pg_roles WHERE rolname = 'pg_monitor'), false)
			    OR COALESCE((SELECT pg_has_role(current_user, 'pg_read_all_stats', 'MEMBER')
			                 FROM pg_roles WHERE rolname = 'pg_read_all_stats'), false))
			                                                     AS can_read_all_stats,
			(r.rolsuper
			    OR COALESCE((SELECT pg_has_role(current_user, 'pg_read_all_settings', 'MEMBER')
			                 FROM pg_roles WHERE rolname = 'pg_read_all_settings'), false))
			                                                     AS can_read_all_settings,
			current_setting('default_transaction_read_only')     AS default_transaction_read_only,
			current_setting('statement_timeout')                 AS statement_timeout,
			jsonb_build_object(
			    'rolcreaterole', r.rolcreaterole,
			    'rolcreatedb', r.rolcreatedb,
			    'rolcanlogin', r.rolcanlogin,
			    'rolreplication', r.rolreplication,
			    'rolbypassrls', r.rolbypassrls,
			    'rolconnlimit', r.rolconnlimit
			)                                                    AS role_attrs
		FROM pg_roles r
		WHERE r.rolname = current_user`,
		ResultKind:     ResultScalar,
		RetentionClass: RetentionMedium,
		Timeout:        5 * time.Second,
		Cadence:        CadenceDaily,
	})
}
