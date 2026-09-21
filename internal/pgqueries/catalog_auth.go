package pgqueries

import "time"

// Authentication and transport-security posture collectors (#305).
//
// Transport security (ssl, ssl_min_protocol_version, ssl_ciphers) is
// already carried by pg_settings_v1 — every GUC is emitted there — so it
// is NOT duplicated here (single source of truth). This file adds the
// host-based-authentication surface, which was not collected at all.

func init() {
	// pg_hba_file_rules_v1: host-based authentication rules, one row per
	// pg_hba.conf entry, from the SQL-queryable pg_catalog.pg_hba_file_rules
	// view — no filesystem access. Lets the analyzer flag weak host-auth
	// (e.g. `trust`/`password` on a non-local address) by pointing at the
	// specific rule, per Analyzer #1757 (`weak-host-auth`).
	//
	// Access: the view's SELECT is owner-only and the underlying
	// pg_hba_file_rules() function's EXECUTE is revoked from PUBLIC — and
	// (verified on PG16) neither pg_monitor nor pg_read_all_settings grants
	// either. A non-superuser role needs BOTH
	//   GRANT SELECT ON pg_catalog.pg_hba_file_rules TO <role>;
	//   GRANT EXECUTE ON FUNCTION pg_catalog.pg_hba_file_rules() TO <role>;
	// Without them the view raises a hard permission-denied (42501);
	// PrivilegedViewDegrade records that run skipped/privilege_restricted,
	// not failed, so the default pg_monitor deployment does not turn the
	// cycle partial (the "empty + completeness note" contract).
	//
	// Columns: rule_number and file_name exist only on PG15+. The base SQL
	// emits them as typed NULL stubs so the column set is stable across
	// majors (#210), and RegisterOverride(15..18) supplies the real columns.
	//
	// Sanitization: config metadata only — addresses, role names, and
	// database names in HBA rules are configuration (the same class already
	// emitted by login_roles_v1), not secrets. The `error` column surfaces
	// per-line parse errors and is preserved.
	//
	// Specification: specifications/collectors/pg_hba_file_rules_v1.md
	Register(QueryDef{
		ID:       "pg_hba_file_rules_v1",
		Category: "server",
		SQL: `SELECT
			NULL::integer AS rule_number,
			NULL::text    AS file_name,
			line_number   AS line_number,
			type          AS type,
			database      AS database,
			user_name     AS user_name,
			address       AS address,
			netmask       AS netmask,
			auth_method   AS auth_method,
			options       AS options,
			error         AS error
		FROM pg_catalog.pg_hba_file_rules
		ORDER BY line_number`,
		MinPGVersion:          10,
		ResultKind:            ResultRowset,
		RetentionClass:        RetentionLong,
		Timeout:               10 * time.Second,
		Cadence:               CadenceDaily,
		PrivilegedViewDegrade: true,
	})

	// PG15+ exposes rule_number (stable ordinal) and file_name (the
	// including file for @-referenced HBA fragments).
	RegisterOverride(15, "pg_hba_file_rules_v1", pgHbaFileRulesV15SQL)
	RegisterOverride(16, "pg_hba_file_rules_v1", pgHbaFileRulesV15SQL)
	RegisterOverride(17, "pg_hba_file_rules_v1", pgHbaFileRulesV15SQL)
	RegisterOverride(18, "pg_hba_file_rules_v1", pgHbaFileRulesV15SQL)
}

const pgHbaFileRulesV15SQL = `SELECT
	rule_number  AS rule_number,
	file_name    AS file_name,
	line_number  AS line_number,
	type         AS type,
	database     AS database,
	user_name    AS user_name,
	address      AS address,
	netmask      AS netmask,
	auth_method  AS auth_method,
	options      AS options,
	error        AS error
FROM pg_catalog.pg_hba_file_rules
ORDER BY rule_number`
