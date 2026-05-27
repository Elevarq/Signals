package pgqueries

import "time"

// Scoped configuration collectors — role, database, role-in-database, and
// object-level GUC surfaces that pg_settings alone cannot represent.

func init() {
	// pg_db_role_settings_v1: PostgreSQL default GUC overrides stored in
	// pg_db_role_setting. This captures ALTER DATABASE ... SET,
	// ALTER ROLE ... SET, and ALTER ROLE ... IN DATABASE ... SET surfaces.
	//
	// Specification: specifications/collectors/pg_db_role_settings_v1.md
	Register(QueryDef{
		ID:       "pg_db_role_settings_v1",
		Category: "server",
		SQL: `SELECT
			s.setdatabase                                AS database_oid,
			d.datname                                    AS database_name,
			s.setrole                                    AS role_oid,
			r.rolname                                    AS role_name,
			CASE
				WHEN s.setdatabase <> 0 AND s.setrole <> 0 THEN 'role_in_database'
				WHEN s.setdatabase <> 0 AND s.setrole = 0 THEN 'database'
				WHEN s.setdatabase = 0 AND s.setrole <> 0 THEN 'role'
				ELSE 'global'
			END                                          AS setting_scope,
			s.setconfig
		FROM pg_db_role_setting s
		LEFT JOIN pg_database d ON d.oid = s.setdatabase
		LEFT JOIN pg_roles r ON r.oid = s.setrole
		ORDER BY setting_scope,
		         database_name NULLS FIRST,
		         role_name NULLS FIRST,
		         database_oid,
		         role_oid`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        10 * time.Second,
		Cadence:        CadenceDaily,
	})
}
