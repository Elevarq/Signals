// FDW (Foreign Data Wrapper) collectors.
//
// Specs:
//   specifications/collectors/fdw_wrappers_v1.md
//   specifications/collectors/fdw_servers_v1.md
//   specifications/collectors/fdw_user_mappings_v1.md
//   specifications/collectors/fdw_foreign_tables_v1.md
//
// Coverage policy:
//   - Read-only: every query is a SELECT against pg_catalog or
//     information_schema. No remote connections, no superuser, no
//     mutation.
//   - Secret-redacted at the snapshot writer (see fdw_redact.go +
//     the snapshot integration test). The SQL itself emits the raw
//     `text[]` option arrays — Postgres won't parse them on our
//     behalf — and the collector pipeline downstream applies the
//     redactor before the rows are persisted.
//   - PG14–18 compatible: the four catalogs queried below
//     (pg_foreign_data_wrapper, pg_foreign_server, pg_user_mapping,
//     pg_foreign_table) have stable shapes across these majors.
//     PG19 inherits the default; if a future PG release changes a
//     column we add a per-major override (registry pattern, see
//     internal/pgqueries/registry.go::overrideRegistry).
//   - No-FDW database: empty result sets, not errors. (`pg_foreign_*`
//     catalogs always exist; queries return zero rows when no FDW is
//     installed.)

package pgqueries

import "time"

func init() {
	// fdw_wrappers_v1: foreign-data-wrapper inventory.
	//
	// One row per installed FDW (postgres_fdw, file_fdw, oracle_fdw,
	// mysql_fdw, …). The `fdwoptions` array is the system-level FDW
	// option set — it rarely contains secrets but is run through the
	// redactor anyway to be safe.
	//
	// Spec: specifications/collectors/fdw_wrappers_v1.md
	Register(QueryDef{
		ID:       "fdw_wrappers_v1",
		Category: "schema",
		SQL: `SELECT
			fdw.oid::bigint                     AS fdw_oid,
			fdw.fdwname                         AS fdw_name,
			pg_get_userbyid(fdw.fdwowner)       AS fdw_owner,
			COALESCE(h.proname, '')             AS fdw_handler,
			COALESCE(v.proname, '')             AS fdw_validator,
			COALESCE(fdw.fdwoptions, '{}'::text[]) AS fdw_options
		FROM pg_foreign_data_wrapper fdw
		LEFT JOIN pg_proc h ON h.oid = fdw.fdwhandler
		LEFT JOIN pg_proc v ON v.oid = fdw.fdwvalidator
		ORDER BY fdw.fdwname`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionLong,
		Timeout:        15 * time.Second,
		Cadence:        CadenceDaily,
	})

	// fdw_servers_v1: foreign-server inventory with FDW linkage.
	//
	// One row per CREATE SERVER. `srvoptions` carries the connection
	// parameters (host, port, dbname, …); the redactor masks
	// password / token / key-shaped option keys before persistence.
	//
	// Spec: specifications/collectors/fdw_servers_v1.md
	Register(QueryDef{
		ID:       "fdw_servers_v1",
		Category: "schema",
		SQL: `SELECT
			s.oid::bigint                       AS server_oid,
			s.srvname                           AS server_name,
			fdw.fdwname                         AS fdw_name,
			COALESCE(s.srvtype, '')             AS server_type,
			COALESCE(s.srvversion, '')          AS server_version,
			pg_get_userbyid(s.srvowner)         AS server_owner,
			COALESCE(s.srvoptions, '{}'::text[]) AS server_options
		FROM pg_foreign_server s
		JOIN pg_foreign_data_wrapper fdw ON fdw.oid = s.srvfdw
		ORDER BY s.srvname`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionLong,
		Timeout:        15 * time.Second,
		Cadence:        CadenceDaily,
	})

	// fdw_user_mappings_v1: user-mapping inventory via the public
	// `pg_user_mappings` view (NOT the underlying `pg_user_mapping`
	// table — that one is superuser-restricted by default and would
	// fail the collector for `pg_monitor`-level roles).
	//
	// `pg_user_mappings` is the documented public surface:
	//   - All rows visible to the connected user.
	//   - `umoptions` returns NULL for users who lack the privilege
	//     to read other users' option values; the analyzer sees
	//     "mapping exists, options unknown" rather than the
	//     collector failing entirely. This is the spec-mandated
	//     graceful-degradation path (FC-02 in
	//     fdw_user_mappings_v1.md).
	//   - `usename` is the local role; we keep `local_user_name` as
	//     the column name for analyzer-side compatibility and emit
	//     'PUBLIC' when the mapping is unscoped.
	//
	// CRITICAL: when `umoptions` IS visible (e.g. for the user's own
	// mappings, or to a superuser, or — in some platforms — to
	// `pg_monitor`), it typically carries `password=…`. The
	// redactor MUST run before these rows leave the daemon. Tests
	// that exercise this collector verify `<redacted>` in place of
	// any sensitive option value.
	//
	// We never emit raw OIDs of users in case the integer value
	// carries deployment-topology meaning the operator wants kept
	// private.
	//
	// Spec: specifications/collectors/fdw_user_mappings_v1.md
	Register(QueryDef{
		ID:       "fdw_user_mappings_v1",
		Category: "schema",
		SQL: `SELECT
			um.srvname                          AS server_name,
			CASE
				WHEN um.umuser = 0 THEN 'PUBLIC'
				ELSE COALESCE(um.usename, pg_get_userbyid(um.umuser))
			END                                 AS local_user_name,
			COALESCE(um.umoptions, '{}'::text[]) AS mapping_options
		FROM pg_user_mappings um
		ORDER BY um.srvname, local_user_name`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionLong,
		Timeout:        15 * time.Second,
		Cadence:        CadenceDaily,
	})

	// fdw_foreign_tables_v1: foreign-table inventory linked to its
	// server + FDW.
	//
	// We DO NOT collect remote table data — only the local catalog
	// metadata: schema, name, OID, options, server linkage. The
	// `relkind = 'f'` filter is implicit (pg_foreign_table only
	// contains foreign-table relids).
	//
	// Excludes pg_catalog / information_schema / pg_temp / pg_toast
	// to match the policy used by the other schema collectors. If
	// someone defines a foreign table inside one of those schemas
	// we deliberately do not surface it.
	//
	// Spec: specifications/collectors/fdw_foreign_tables_v1.md
	Register(QueryDef{
		ID:       "fdw_foreign_tables_v1",
		Category: "schema",
		SQL: `SELECT
			n.nspname                           AS schemaname,
			c.relname                           AS table_name,
			c.oid::bigint                       AS table_oid,
			c.relkind                           AS relkind,
			s.srvname                           AS server_name,
			fdw.fdwname                         AS fdw_name,
			COALESCE(ft.ftoptions, '{}'::text[]) AS foreign_table_options
		FROM pg_foreign_table ft
		JOIN pg_class c ON c.oid = ft.ftrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_foreign_server s ON s.oid = ft.ftserver
		JOIN pg_foreign_data_wrapper fdw ON fdw.oid = s.srvfdw
		WHERE n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND n.nspname NOT LIKE 'pg_temp_%'
		  AND n.nspname NOT LIKE 'pg_toast_temp_%'
		ORDER BY n.nspname, c.relname`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        30 * time.Second,
		Cadence:        CadenceDaily,
	})
}
