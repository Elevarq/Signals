package pgqueries

import "time"

// Schema Metadata Collectors
//
// These collectors extract structural schema metadata on a slow
// cadence (default 24h). They focus on non-system objects only.
//
// Phase 1: pg_constraints_v1, pg_indexes_v1, pg_stats_v1, pg_columns_v1
// Phase 2: pg_schemas_v1, ...
//
// Specifications: specifications/collectors/*.md

// SchemaFilter is the standard WHERE clause that excludes PostgreSQL
// internal schemas from all schema metadata collectors.
const SchemaFilter = `
  AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
  AND n.nspname NOT LIKE 'pg_temp_%'
  AND n.nspname NOT LIKE 'pg_toast_temp_%'`

// SchemaFilterDirect is the standard filter for views that expose
// schemaname directly (e.g., pg_indexes) without a pg_namespace join.
const SchemaFilterDirect = `
WHERE schemaname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
  AND schemaname NOT LIKE 'pg_temp_%'
  AND schemaname NOT LIKE 'pg_toast_temp_%'`

func init() {
	// pg_constraints_v1: constraint inventory, one row per constrained
	// column. Multi-column constraints emit multiple rows with the
	// same conname and sequential column_position values.
	//
	// Specification: specifications/collectors/pg_constraints_v1.md
	// Unblocks: FI-R010 through FI-R016 (Category 1 missing-FK-index detector)
	Register(QueryDef{
		ID:       "pg_constraints_v1",
		Category: "schema",
		SQL: `SELECT
			n.nspname AS schemaname,
			c.relname,
			con.conname,
			con.contype,
			pg_get_constraintdef(con.oid, true) AS condef,
			a.attname AS column_name,
			ord.ordinality::int AS column_position,
			c.relkind,
			COALESCE(s.n_live_tup, 0)::bigint AS n_live_tup,
			COALESCE(rc.relname, '') AS confrelname,
			COALESCE(rn.nspname, '') AS confschemaname
		FROM pg_constraint con
		JOIN pg_class c ON c.oid = con.conrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		CROSS JOIN LATERAL unnest(con.conkey) WITH ORDINALITY AS ord(attnum, ordinality)
		JOIN pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = ord.attnum
		LEFT JOIN pg_class rc ON rc.oid = con.confrelid
		LEFT JOIN pg_namespace rn ON rn.oid = rc.relnamespace
		LEFT JOIN pg_stat_user_tables s ON s.relid = c.oid
		WHERE n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND n.nspname NOT LIKE 'pg_temp_%'
		  AND n.nspname NOT LIKE 'pg_toast_temp_%'
		ORDER BY n.nspname, c.relname, con.conname, ord.ordinality`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        30 * time.Second,
		Cadence:        CadenceDaily,
	})

	// pg_indexes_v1: index definitions for all user-schema indexes.
	// The indexdef column contains the full CREATE INDEX statement,
	// needed to identify leading columns for composite indexes.
	//
	// Specification: specifications/collectors/pg_indexes_v1.md
	// Unblocks: FI-R014 (FK index suppression with leading column parsing)
	Register(QueryDef{
		ID:       "pg_indexes_v1",
		Category: "schema",
		SQL: `SELECT
			schemaname,
			tablename,
			indexname,
			indexdef,
			COALESCE(tablespace, '') AS tablespace
		FROM pg_indexes
		WHERE schemaname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND schemaname NOT LIKE 'pg_temp_%'
		  AND schemaname NOT LIKE 'pg_toast_temp_%'
		ORDER BY schemaname, tablename, indexname`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        30 * time.Second,
		Cadence:        CadenceDaily,
	})

	// pg_stats_v1: column-level planner statistics for cardinality
	// and correlation analysis. Deliberately excludes most_common_vals,
	// histogram_bounds, and other columns that contain data samples.
	//
	// Specification: specifications/collectors/pg_stats_v1.md
	// Unblocks: FI-R012 (n_distinct cardinality), FI-R052 (correlation)
	Register(QueryDef{
		ID:       "pg_stats_v1",
		Category: "schema",
		SQL: `SELECT
			schemaname,
			tablename,
			attname,
			n_distinct,
			correlation,
			null_frac,
			avg_width
		FROM pg_stats
		WHERE schemaname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND schemaname NOT LIKE 'pg_temp_%'
		  AND schemaname NOT LIKE 'pg_toast_temp_%'
		ORDER BY schemaname, tablename, attname`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        30 * time.Second,
		Cadence:        CadenceDaily,
	})

	// pg_stats_extended_v1: extended planner statistics — the
	// sampled-value columns pg_stats_v1 deliberately excludes
	// (most_common_vals, most_common_freqs, histogram_bounds).
	//
	// HighSensitivity: emits actual data values from pg_statistic
	// arrays cast to text. Disabled by default; runs only when
	// the operator explicitly enables it via signals config.
	// HighSensitivity is the existing gating mechanism used by
	// the application-authored-SQL collectors (R075); applying
	// it here matches the same opt-in posture.
	//
	// RetentionShort because sampled values should not persist.
	//
	// Specification: specifications/collectors/pg_stats_extended_v1.md
	Register(QueryDef{
		ID:       "pg_stats_extended_v1",
		Category: "schema",
		SQL: `SELECT
			schemaname,
			tablename,
			attname,
			most_common_vals::text  AS most_common_vals,
			most_common_freqs::text AS most_common_freqs,
			histogram_bounds::text  AS histogram_bounds
		FROM pg_stats
		WHERE schemaname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND schemaname NOT LIKE 'pg_temp_%'
		  AND schemaname NOT LIKE 'pg_toast_temp_%'
		ORDER BY schemaname, tablename, attname`,
		ResultKind:      ResultRowset,
		RetentionClass:  RetentionShort,
		Timeout:         30 * time.Second,
		Cadence:         CadenceDaily,
		HighSensitivity: true,
	})

	// pg_stats_array_range_v1 (#128): per-element MCV + range
	// histograms — the pg_stats slot kinds that pg_stats_extended_v1
	// excludes. Records the array/range element statistics
	// (stakind=4 MCELEM, stakind=6 RANGE_LENGTH_HISTOGRAM,
	// stakind=7 RANGE_BOUNDS_HISTOGRAM) for downstream analysis.
	//
	// Two-gate opt-in:
	//   1. HighSensitivityEnabled (daemon-wide safety floor)
	//   2. CollectArrayRangeHistograms (per-collector flag — see
	//      RequiresArrayRangeOptIn in types.go)
	// Both must be true. The per-collector flag is layered on top
	// of the safety floor specifically because per-element MCV
	// data (tsvector tokens, array values) is materially MORE
	// sensitive than the regular MCV columns covered by
	// pg_stats_extended_v1.
	//
	// MinPGVersion=14: range_length_histogram + range_bounds_histogram
	// columns require PG 14+ on pg_stats.
	//
	// Specification: specifications/collectors/pg_stats_array_range_v1.md
	Register(QueryDef{
		ID:       "pg_stats_array_range_v1",
		Category: "schema",
		SQL: `SELECT
			schemaname,
			tablename,
			attname,
			most_common_elems::text       AS most_common_elems,
			most_common_elem_freqs::text  AS most_common_elem_freqs,
			range_length_histogram::text  AS range_length_histogram,
			range_bounds_histogram::text  AS range_bounds_histogram
		FROM pg_stats
		WHERE schemaname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND schemaname NOT LIKE 'pg_temp_%'
		  AND schemaname NOT LIKE 'pg_toast_temp_%'
		ORDER BY schemaname, tablename, attname`,
		ResultKind:      ResultRowset,
		RetentionClass:  RetentionShort,
		Timeout:         30 * time.Second,
		Cadence:         CadenceDaily,
		MinPGVersion:    14,
		HighSensitivity: true,
	})

	// pg_columns_v1: column inventory with data types for user-schema
	// relations. Uses pg_attribute + pg_class + pg_namespace + pg_attrdef
	// with format_type() for human-readable type names. Excludes system
	// columns (attnum <= 0) and dropped columns. Default expression
	// text is NOT emitted — only the boolean has_default.
	//
	// Specification: specifications/collectors/pg_columns_v1.md
	Register(QueryDef{
		ID:       "pg_columns_v1",
		Category: "schema",
		SQL: `SELECT
			n.nspname AS schemaname,
			c.relname,
			a.attname,
			a.attnum,
			format_type(a.atttypid, a.atttypmod) AS typname,
			NOT a.attnotnull AS is_nullable,
			d.adrelid IS NOT NULL AS has_default,
			a.attlen
		FROM pg_attribute a
		JOIN pg_class c ON c.oid = a.attrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
		WHERE a.attnum > 0
		  AND NOT a.attisdropped
		  AND c.relkind IN ('r', 'p', 'v', 'm', 'f')
		  AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND n.nspname NOT LIKE 'pg_temp_%'
		  AND n.nspname NOT LIKE 'pg_toast_temp_%'
		ORDER BY n.nspname, c.relname, a.attnum`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        30 * time.Second,
		Cadence:        CadenceDaily,
	})

	// --- Phase 2: Schema snapshot foundation ---

	// pg_schemas_v1: schema (namespace) inventory with ownership.
	// Provides namespace context for all other schema collectors.
	//
	// Specification: specifications/collectors/pg_schemas_v1.md
	Register(QueryDef{
		ID:       "pg_schemas_v1",
		Category: "schema",
		SQL: `SELECT
			n.nspname,
			r.rolname AS nspowner,
			n.nspname = 'public' AS is_default
		FROM pg_namespace n
		JOIN pg_roles r ON r.oid = n.nspowner
		WHERE n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND n.nspname NOT LIKE 'pg_temp_%'
		  AND n.nspname NOT LIKE 'pg_toast_temp_%'
		ORDER BY n.nspname`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionLong,
		Timeout:        10 * time.Second,
		Cadence:        CadenceDaily,
	})

	// pg_views_v1: view inventory (inventory mode — no definition text).
	// Lists all user-schema views with owner. Definition text is
	// excluded by default for safety; use pg_views_definitions_v1
	// when definition/hash_only mode is needed.
	//
	// Specification: specifications/collectors/pg_views_v1.md
	Register(QueryDef{
		ID:       "pg_views_v1",
		Category: "schema",
		SQL: `SELECT
			schemaname,
			viewname,
			viewowner
		FROM pg_views
		WHERE schemaname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND schemaname NOT LIKE 'pg_temp_%'
		  AND schemaname NOT LIKE 'pg_toast_temp_%'
		ORDER BY schemaname, viewname`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        10 * time.Second,
		Cadence:        CadenceDaily,
	})

	// pg_views_definitions_v1: view inventory with definition text
	// (definition mode). Includes all inventory columns plus the
	// full view SQL from pg_get_viewdef(). Disabled by default in
	// typical configurations; enabled when schema drift detection
	// or documentation is needed.
	//
	// For hash_only mode, the Elevarq Signals runtime computes SHA-256
	// of the definition column application-side before persistence,
	// then strips the raw text. No pgcrypto dependency.
	//
	// Specification: specifications/collectors/pg_views_v1.md
	Register(QueryDef{
		ID:              "pg_views_definitions_v1",
		Category:        "schema",
		HighSensitivity: true,
		SQL: `SELECT
			v.schemaname,
			v.viewname,
			v.viewowner,
			pg_get_viewdef(c.oid, true) AS definition
		FROM pg_views v
		JOIN pg_class c ON c.relname = v.viewname
		JOIN pg_namespace n ON n.oid = c.relnamespace AND n.nspname = v.schemaname
		WHERE v.schemaname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND v.schemaname NOT LIKE 'pg_temp_%'
		  AND v.schemaname NOT LIKE 'pg_toast_temp_%'
		ORDER BY v.schemaname, v.viewname`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        30 * time.Second,
		Cadence:        CadenceDaily,
	})

	// pg_matviews_v1: materialized view inventory (inventory mode).
	// Lists all user-schema matviews with owner, populated status,
	// and index presence. Definition text excluded by default.
	//
	// Specification: specifications/collectors/pg_matviews_v1.md
	Register(QueryDef{
		ID:       "pg_matviews_v1",
		Category: "schema",
		SQL: `SELECT
			schemaname,
			matviewname,
			matviewowner,
			ispopulated,
			hasindexes
		FROM pg_matviews
		WHERE schemaname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND schemaname NOT LIKE 'pg_temp_%'
		  AND schemaname NOT LIKE 'pg_toast_temp_%'
		ORDER BY schemaname, matviewname`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        10 * time.Second,
		Cadence:        CadenceDaily,
	})

	// pg_matviews_definitions_v1: materialized view inventory with
	// definition text (definition mode). Includes inventory columns
	// plus full matview SQL from pg_get_viewdef().
	//
	// Specification: specifications/collectors/pg_matviews_v1.md
	Register(QueryDef{
		ID:              "pg_matviews_definitions_v1",
		Category:        "schema",
		HighSensitivity: true,
		SQL: `SELECT
			m.schemaname,
			m.matviewname,
			m.matviewowner,
			m.ispopulated,
			m.hasindexes,
			pg_get_viewdef(c.oid, true) AS definition
		FROM pg_matviews m
		JOIN pg_class c ON c.relname = m.matviewname
		JOIN pg_namespace n ON n.oid = c.relnamespace AND n.nspname = m.schemaname
		WHERE m.schemaname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND m.schemaname NOT LIKE 'pg_temp_%'
		  AND m.schemaname NOT LIKE 'pg_toast_temp_%'
		ORDER BY m.schemaname, m.matviewname`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        30 * time.Second,
		Cadence:        CadenceDaily,
	})

	// pg_statistic_ext_v1: inventory of multi-column extended
	// statistics objects (CREATE STATISTICS) declared on
	// user-schema relations. Catalog-metadata only — does NOT
	// read pg_statistic_ext_data (the actual sampled stats data
	// has owner-only visibility post-PG12 and requires
	// permissions beyond pg_monitor).
	//
	// Consumer: Elevarq Analyzer's CREATE STATISTICS advisor
	// which uses this as the "what already exists"
	// guard so it never recommends a duplicate (table, attnums)
	// statistics object.
	//
	// kinds is the per-object array of statistic-kind char codes
	// (see PG `include/statistics/statistics.h` /
	// https://www.postgresql.org/docs/current/catalog-pg-statistic-ext.html):
	//   d - ndistinct (PG 10+)
	//   f - functional dependencies (PG 10+)
	//   m - MCV list (PG 12+)
	//   e - expression (PG 14+)
	//
	// Specification: specifications/collectors/pg_statistic_ext_v1.md
	Register(QueryDef{
		ID:       "pg_statistic_ext_v1",
		Category: "schema",
		SQL: `SELECT
			n.nspname           AS stat_schema,
			es.stxname          AS stat_name,
			cn.nspname          AS table_schema,
			c.relname           AS table_name,
			es.stxkeys::int[]   AS attnums,
			es.stxkind::text[]  AS kinds
		FROM pg_statistic_ext es
		JOIN pg_class c      ON c.oid = es.stxrelid
		JOIN pg_namespace cn ON cn.oid = c.relnamespace
		JOIN pg_namespace n  ON n.oid = es.stxnamespace
		WHERE cn.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND cn.nspname NOT LIKE 'pg_temp_%'
		  AND cn.nspname NOT LIKE 'pg_toast_temp_%'
		ORDER BY cn.nspname, c.relname, es.stxname`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        15 * time.Second,
		Cadence:        CadenceDaily,
	})

	// pg_statistic_ext_data_v1 (#171): per-(object, kind) sampled
	// statistics blobs for the multivariate-stats kinds that do
	// NOT carry sampled column values (d = ndistinct, f =
	// functional dependencies, e = expression). The 'm' MCV blob
	// — the only kind that may contain PII — is split out into
	// the HighSensitivity-gated sibling
	// pg_statistic_ext_data_mcv_v1 below.
	//
	// pg_statistic_ext_data is owner-only post-PG12: non-owners
	// see no row in the view even when the parent
	// pg_statistic_ext row is visible. The LEFT JOIN preserves
	// the catalog row with NULL data columns for objects the
	// role can't read, so the snapshot emits per-object
	// availability rows (kind_data=NULL, available=false) rather
	// than failing.
	//
	// Specification: specifications/collectors/pg_statistic_ext_data_v1.md
	Register(QueryDef{
		ID:       "pg_statistic_ext_data_v1",
		Category: "schema",
		SQL: `SELECT
			n.nspname            AS stat_schema,
			es.stxname           AS stat_name,
			cn.nspname           AS table_schema,
			c.relname            AS table_name,
			k.kind               AS kind,
			CASE k.kind
				WHEN 'd' THEN esd.stxdndistinct::text
				WHEN 'f' THEN esd.stxddependencies::text
				WHEN 'e' THEN esd.stxdexpr::text
			END                  AS kind_data,
			(CASE k.kind
				WHEN 'd' THEN esd.stxdndistinct IS NOT NULL
				WHEN 'f' THEN esd.stxddependencies IS NOT NULL
				WHEN 'e' THEN esd.stxdexpr IS NOT NULL
			END)                 AS available
		FROM pg_statistic_ext es
		LEFT JOIN pg_statistic_ext_data esd ON esd.stxoid = es.oid
		JOIN pg_class c      ON c.oid = es.stxrelid
		JOIN pg_namespace cn ON cn.oid = c.relnamespace
		JOIN pg_namespace n  ON n.oid = es.stxnamespace
		CROSS JOIN LATERAL unnest(es.stxkind) AS k(kind)
		WHERE cn.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND cn.nspname NOT LIKE 'pg_temp_%'
		  AND cn.nspname NOT LIKE 'pg_toast_temp_%'
		  AND k.kind IN ('d', 'f', 'e')
		ORDER BY cn.nspname, c.relname, es.stxname, k.kind`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        15 * time.Second,
		Cadence:        CadenceDaily,
		MinPGVersion:   14,
	})

	// pg_statistic_ext_data_mcv_v1 (#171): the 'm' (multivariate
	// MCV) blob from pg_statistic_ext_data. Split into its own
	// collector because the MCV blob is the only multivariate-
	// stats kind that carries ACTUAL SAMPLED COLUMN VALUES and
	// may therefore contain PII.
	//
	// HighSensitivity-gated: only runs when the operator has
	// explicitly enabled the daemon-wide HS floor. Same posture
	// as pg_stats_extended_v1's per-column MCV / histogram blob.
	//
	// Specification: specifications/collectors/pg_statistic_ext_data_v1.md
	Register(QueryDef{
		ID:       "pg_statistic_ext_data_mcv_v1",
		Category: "schema",
		SQL: `SELECT
			n.nspname            AS stat_schema,
			es.stxname           AS stat_name,
			cn.nspname           AS table_schema,
			c.relname            AS table_name,
			'm'                  AS kind,
			esd.stxdmcv::text    AS kind_data,
			(esd.stxdmcv IS NOT NULL) AS available
		FROM pg_statistic_ext es
		LEFT JOIN pg_statistic_ext_data esd ON esd.stxoid = es.oid
		JOIN pg_class c      ON c.oid = es.stxrelid
		JOIN pg_namespace cn ON cn.oid = c.relnamespace
		JOIN pg_namespace n  ON n.oid = es.stxnamespace
		WHERE cn.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND cn.nspname NOT LIKE 'pg_temp_%'
		  AND cn.nspname NOT LIKE 'pg_toast_temp_%'
		  AND 'm' = ANY(es.stxkind)
		ORDER BY cn.nspname, c.relname, es.stxname`,
		ResultKind:      ResultRowset,
		RetentionClass:  RetentionShort,
		Timeout:         15 * time.Second,
		Cadence:         CadenceDaily,
		MinPGVersion:    14,
		HighSensitivity: true,
	})

	// pg_partitions_v1: partition topology — strategy, key, and
	// parent/child relationships. Parents with no children produce
	// one row with empty child columns.
	//
	// Specification: specifications/collectors/pg_partitions_v1.md
	Register(QueryDef{
		ID:       "pg_partitions_v1",
		Category: "schema",
		SQL: `SELECT
			pn.nspname AS parent_schema,
			pc.relname AS parent_name,
			pt.partstrat AS partition_strategy,
			pg_get_partkeydef(pc.oid) AS partition_key,
			COALESCE(cn.nspname, '') AS child_schema,
			COALESCE(cc.relname, '') AS child_name,
			COALESCE(pg_get_expr(cc.relpartbound, cc.oid), '') AS child_bounds
		FROM pg_partitioned_table pt
		JOIN pg_class pc ON pc.oid = pt.partrelid
		JOIN pg_namespace pn ON pn.oid = pc.relnamespace
		LEFT JOIN pg_inherits i ON i.inhparent = pc.oid
		LEFT JOIN pg_class cc ON cc.oid = i.inhrelid
		LEFT JOIN pg_namespace cn ON cn.oid = cc.relnamespace
		WHERE pn.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND pn.nspname NOT LIKE 'pg_temp_%'
		  AND pn.nspname NOT LIKE 'pg_toast_temp_%'
		ORDER BY pn.nspname, pc.relname, cn.nspname, cc.relname`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        30 * time.Second,
		Cadence:        CadenceDaily,
	})

	// pg_triggers_v1: trigger inventory (inventory mode). Outputs
	// the tgtype bitmask as an integer — timing and events are
	// decoded by the analyzer. Excludes internal triggers.
	//
	// tgtype bitmask: bit 0=ROW, bit 1=BEFORE, bit 2=INSERT,
	// bit 3=DELETE, bit 4=UPDATE, bit 5=TRUNCATE, bit 6=INSTEAD OF
	//
	// Specification: specifications/collectors/pg_triggers_v1.md
	Register(QueryDef{
		ID:       "pg_triggers_v1",
		Category: "schema",
		SQL: `SELECT
			n.nspname AS schemaname,
			c.relname,
			t.tgname,
			t.tgtype::int AS tgtype,
			fn.nspname AS tg_funcschema,
			p.proname AS tg_funcname,
			t.tgenabled AS tg_enabled
		FROM pg_trigger t
		JOIN pg_class c ON c.oid = t.tgrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_proc p ON p.oid = t.tgfoid
		JOIN pg_namespace fn ON fn.oid = p.pronamespace
		WHERE NOT t.tgisinternal
		  AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND n.nspname NOT LIKE 'pg_temp_%'
		  AND n.nspname NOT LIKE 'pg_toast_temp_%'
		ORDER BY n.nspname, c.relname, t.tgname`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        15 * time.Second,
		Cadence:        CadenceDaily,
	})

	// pg_triggers_definitions_v1: trigger inventory with definition
	// text from pg_get_triggerdef(). Includes all inventory columns
	// plus the full trigger definition.
	//
	// Specification: specifications/collectors/pg_triggers_v1.md
	Register(QueryDef{
		ID:              "pg_triggers_definitions_v1",
		Category:        "schema",
		HighSensitivity: true,
		SQL: `SELECT
			n.nspname AS schemaname,
			c.relname,
			t.tgname,
			t.tgtype::int AS tgtype,
			fn.nspname AS tg_funcschema,
			p.proname AS tg_funcname,
			t.tgenabled AS tg_enabled,
			pg_get_triggerdef(t.oid, true) AS triggerdef
		FROM pg_trigger t
		JOIN pg_class c ON c.oid = t.tgrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_proc p ON p.oid = t.tgfoid
		JOIN pg_namespace fn ON fn.oid = p.pronamespace
		WHERE NOT t.tgisinternal
		  AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND n.nspname NOT LIKE 'pg_temp_%'
		  AND n.nspname NOT LIKE 'pg_toast_temp_%'
		ORDER BY n.nspname, c.relname, t.tgname`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        30 * time.Second,
		Cadence:        CadenceDaily,
	})

	// pg_functions_v1: function/procedure inventory (inventory mode).
	// Signature, return type, language, volatility, security properties.
	// Function bodies excluded by default (high sensitivity).
	// Requires PG 11+ for prokind column.
	//
	// Specification: specifications/collectors/pg_functions_v1.md
	Register(QueryDef{
		ID:           "pg_functions_v1",
		Category:     "schema",
		MinPGVersion: 11,
		SQL: `SELECT
			n.nspname AS schemaname,
			p.proname,
			pg_get_function_identity_arguments(p.oid) AS identity_args,
			pg_get_function_result(p.oid) AS return_type,
			l.lanname AS language,
			p.provolatile AS volatility,
			p.prosecdef AS security_definer,
			p.proisstrict AS is_strict,
			p.prokind
		FROM pg_proc p
		JOIN pg_namespace n ON n.oid = p.pronamespace
		JOIN pg_language l ON l.oid = p.prolang
		WHERE n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND n.nspname NOT LIKE 'pg_temp_%'
		  AND n.nspname NOT LIKE 'pg_toast_temp_%'
		ORDER BY n.nspname, p.proname, pg_get_function_identity_arguments(p.oid)`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        30 * time.Second,
		Cadence:        CadenceDaily,
	})

	// pg_functions_definitions_v1: function/procedure inventory with
	// body text (definition mode). Includes all inventory columns
	// plus prosrc. High sensitivity — opt-in only.
	//
	// Specification: specifications/collectors/pg_functions_v1.md
	Register(QueryDef{
		ID:              "pg_functions_definitions_v1",
		Category:        "schema",
		MinPGVersion:    11,
		HighSensitivity: true,
		SQL: `SELECT
			n.nspname AS schemaname,
			p.proname,
			pg_get_function_identity_arguments(p.oid) AS identity_args,
			pg_get_function_result(p.oid) AS return_type,
			l.lanname AS language,
			p.provolatile AS volatility,
			p.prosecdef AS security_definer,
			p.proisstrict AS is_strict,
			p.prokind,
			p.prosrc AS body
		FROM pg_proc p
		JOIN pg_namespace n ON n.oid = p.pronamespace
		JOIN pg_language l ON l.oid = p.prolang
		WHERE n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND n.nspname NOT LIKE 'pg_temp_%'
		  AND n.nspname NOT LIKE 'pg_toast_temp_%'
		ORDER BY n.nspname, p.proname, pg_get_function_identity_arguments(p.oid)`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        30 * time.Second,
		Cadence:        CadenceDaily,
	})

	// pg_sequences_v1: sequence inventory and health.
	// Provides identity, configuration, and current value for
	// exhaustion detection and identity column monitoring.
	//
	// Specification: specifications/collectors/pg_sequences_v1.md
	Register(QueryDef{
		ID:       "pg_sequences_v1",
		Category: "schema",
		SQL: `SELECT
			schemaname,
			sequencename,
			data_type,
			start_value,
			min_value,
			max_value,
			increment_by,
			cycle,
			last_value
		FROM pg_sequences
		WHERE schemaname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND schemaname NOT LIKE 'pg_temp_%'
		  AND schemaname NOT LIKE 'pg_toast_temp_%'
		ORDER BY schemaname, sequencename`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        10 * time.Second,
		Cadence:        CadenceDaily,
	})

	// pg_types_v1: user-defined enum / composite / domain inventory
	// with the structural pieces needed to describe them
	// (CREATE TYPE / CREATE DOMAIN), so tables using those types
	// remain analysable instead of being skipped. Emits
	// structured columns, NOT a ready DDL string: the safety linter
	// bans the literal keyword CREATE in collector SQL, and there is no
	// pg_get_typedef() — so the analyzer assembles the DDL from these
	// pieces. Type/label VALUES appear only in result rows, never in
	// the query text.
	//
	// Specification: specifications/collectors/pg_types_v1.md
	Register(QueryDef{
		ID:       "pg_types_v1",
		Category: "schema",
		SQL: `SELECT
			n.nspname AS schemaname,
			t.typname AS typename,
			t.typtype::text AS typtype,
			CASE WHEN t.typtype = 'e' THEN (
				SELECT array_agg(e.enumlabel ORDER BY e.enumsortorder)
				FROM pg_enum e WHERE e.enumtypid = t.oid
			) END AS enum_labels,
			CASE WHEN t.typtype = 'c' THEN (
				SELECT array_agg(quote_ident(a.attname) || ' ' || format_type(a.atttypid, a.atttypmod) ORDER BY a.attnum)
				FROM pg_attribute a
				WHERE a.attrelid = t.typrelid AND a.attnum > 0 AND NOT a.attisdropped
			) END AS composite_columns,
			CASE WHEN t.typtype = 'd' THEN format_type(t.typbasetype, t.typtypmod) END AS domain_basetype,
			CASE WHEN t.typtype = 'd' THEN t.typnotnull END AS domain_notnull,
			CASE WHEN t.typtype = 'd' THEN t.typdefault END AS domain_default,
			CASE WHEN t.typtype = 'd' THEN (
				SELECT array_agg(pg_get_constraintdef(c.oid, true) ORDER BY c.conname)
				FROM pg_constraint c WHERE c.contypid = t.oid
			) END AS domain_constraints
		FROM pg_type t
		JOIN pg_namespace n ON n.oid = t.typnamespace
		WHERE t.typtype IN ('e', 'c', 'd')
		  AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND n.nspname NOT LIKE 'pg_temp_%'
		  AND n.nspname NOT LIKE 'pg_toast_temp_%'
		  AND (t.typtype <> 'c' OR EXISTS (
				SELECT 1 FROM pg_class cl WHERE cl.oid = t.typrelid AND cl.relkind = 'c'))
		  AND NOT EXISTS (
				SELECT 1 FROM pg_depend d
				WHERE d.objid = t.oid AND d.classid = 'pg_type'::regclass AND d.deptype = 'e')
		ORDER BY n.nspname, t.typname`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        10 * time.Second,
		Cadence:        CadenceDaily,
	})

	// pg_policies_v1: row-level security policy inventory with the
	// per-table RLS-enabled flags, so RLS-protected tables can be
	// analysed accurately. HighSensitivity: the
	// qual / with_check columns are arbitrary SQL expressions (same
	// class as view/function/trigger definition text). The expression
	// VALUES come from runtime column references, never query-text
	// literals, so they cannot trip the safety linter.
	//
	// Specification: specifications/collectors/pg_policies_v1.md
	Register(QueryDef{
		ID:              "pg_policies_v1",
		Category:        "schema",
		HighSensitivity: true,
		SQL: `SELECT
			p.schemaname,
			p.tablename,
			p.policyname,
			p.permissive,
			array_to_string(p.roles, ', ') AS roles,
			p.cmd,
			p.qual,
			p.with_check,
			c.relrowsecurity AS rls_enabled,
			c.relforcerowsecurity AS rls_forced
		FROM pg_policies p
		JOIN pg_namespace n ON n.nspname = p.schemaname
		JOIN pg_class c ON c.relname = p.tablename AND c.relnamespace = n.oid
		WHERE p.schemaname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND p.schemaname NOT LIKE 'pg_temp_%'
		  AND p.schemaname NOT LIKE 'pg_toast_temp_%'
		ORDER BY p.schemaname, p.tablename, p.policyname`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        10 * time.Second,
		Cadence:        CadenceDaily,
	})

	// pg_operators_v1: user-defined operators. A query
	// using a user-defined operator in a predicate cannot be analysed
	// accurately unless the operator exists. Extension operators come via
	// CREATE EXTENSION; emit only non-extension-owned ones. Built-ins
	// live in pg_catalog (excluded by the schema filter).
	//
	// Specification: specifications/collectors/pg_operators_v1.md
	Register(QueryDef{
		ID:       "pg_operators_v1",
		Category: "schema",
		SQL: `SELECT
			n.nspname AS schemaname,
			o.oprname,
			CASE WHEN o.oprleft <> 0 THEN format_type(o.oprleft, NULL) END AS left_type,
			CASE WHEN o.oprright <> 0 THEN format_type(o.oprright, NULL) END AS right_type,
			format_type(o.oprresult, NULL) AS result_type,
			fn.nspname || '.' || fp.proname AS function,
			o.oprcanmerge,
			o.oprcanhash
		FROM pg_operator o
		JOIN pg_namespace n ON n.oid = o.oprnamespace
		JOIN pg_proc fp ON fp.oid = o.oprcode
		JOIN pg_namespace fn ON fn.oid = fp.pronamespace
		WHERE n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND n.nspname NOT LIKE 'pg_temp_%'
		  AND n.nspname NOT LIKE 'pg_toast_temp_%'
		  AND NOT EXISTS (
				SELECT 1 FROM pg_depend d
				WHERE d.objid = o.oid AND d.classid = 'pg_operator'::regclass AND d.deptype = 'e')
		ORDER BY n.nspname, o.oprname, left_type, right_type`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        10 * time.Second,
		Cadence:        CadenceDaily,
	})

	// pg_aggregates_v1: user-defined aggregates. A
	// query calling a user-defined aggregate cannot be analysed
	// accurately unless it exists. Emit non-extension-owned ones; built-ins are in
	// pg_catalog (schema filter).
	//
	// Specification: specifications/collectors/pg_aggregates_v1.md
	Register(QueryDef{
		ID:       "pg_aggregates_v1",
		Category: "schema",
		SQL: `SELECT
			n.nspname AS schemaname,
			p.proname AS aggname,
			pg_get_function_identity_arguments(p.oid) AS identity_args,
			format_type(a.aggtranstype, NULL) AS state_type,
			tn.nspname || '.' || tp.proname AS sfunc,
			CASE WHEN a.aggfinalfn <> 0 THEN fn.nspname || '.' || fp.proname END AS finalfunc,
			CASE WHEN a.aggcombinefn <> 0 THEN cn.nspname || '.' || cp.proname END AS combinefunc,
			a.agginitval AS initcond,
			a.aggkind::text AS aggkind
		FROM pg_aggregate a
		JOIN pg_proc p ON p.oid = a.aggfnoid
		JOIN pg_namespace n ON n.oid = p.pronamespace
		JOIN pg_proc tp ON tp.oid = a.aggtransfn
		JOIN pg_namespace tn ON tn.oid = tp.pronamespace
		LEFT JOIN pg_proc fp ON fp.oid = a.aggfinalfn
		LEFT JOIN pg_namespace fn ON fn.oid = fp.pronamespace
		LEFT JOIN pg_proc cp ON cp.oid = a.aggcombinefn
		LEFT JOIN pg_namespace cn ON cn.oid = cp.pronamespace
		WHERE n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND n.nspname NOT LIKE 'pg_temp_%'
		  AND n.nspname NOT LIKE 'pg_toast_temp_%'
		  AND NOT EXISTS (
				SELECT 1 FROM pg_depend d
				WHERE d.objid = p.oid AND d.classid = 'pg_proc'::regclass AND d.deptype = 'e')
		ORDER BY n.nspname, p.proname, identity_args`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        10 * time.Second,
		Cadence:        CadenceDaily,
	})

	// pg_rules_v1: rewrite rules. Rules rewrite
	// SELECT/INSERT/UPDATE/DELETE on a relation, so they change DML
	// planning. The implicit _RETURN view rule is excluded (it comes
	// with the view definition). HighSensitivity: the rule action is arbitrary
	// SQL (the `definition` value is produced at runtime by
	// pg_get_ruledef — it is result data, never query text, so it does
	// not trip the linter).
	//
	// Specification: specifications/collectors/pg_rules_v1.md
	Register(QueryDef{
		ID:              "pg_rules_v1",
		Category:        "schema",
		HighSensitivity: true,
		SQL: `SELECT schemaname, tablename, rulename, definition
		FROM pg_rules
		WHERE schemaname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND schemaname NOT LIKE 'pg_temp_%'
		  AND schemaname NOT LIKE 'pg_toast_temp_%'
		  AND rulename <> '_RETURN'
		ORDER BY schemaname, tablename, rulename`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        10 * time.Second,
		Cadence:        CadenceDaily,
	})

	// pg_casts_v1: user-defined casts. A query using a
	// user-defined cast may be analysed inaccurately if absent. Casts have no
	// schema, so built-ins are excluded by OID (< FirstNormalObjectId =
	// 16384); extension-owned casts are excluded via pg_depend.
	//
	// Specification: specifications/collectors/pg_casts_v1.md
	Register(QueryDef{
		ID:       "pg_casts_v1",
		Category: "schema",
		SQL: `SELECT
			sn.nspname AS source_schema,
			st.typname AS source_type,
			tn.nspname AS target_schema,
			tt.typname AS target_type,
			CASE c.castmethod WHEN 'f' THEN pn.nspname || '.' || p.proname WHEN 'i' THEN 'inout' ELSE 'binary' END AS cast_impl,
			c.castcontext::text AS castcontext
		FROM pg_cast c
		JOIN pg_type st ON st.oid = c.castsource
		JOIN pg_namespace sn ON sn.oid = st.typnamespace
		JOIN pg_type tt ON tt.oid = c.casttarget
		JOIN pg_namespace tn ON tn.oid = tt.typnamespace
		LEFT JOIN pg_proc p ON p.oid = c.castfunc
		LEFT JOIN pg_namespace pn ON pn.oid = p.pronamespace
		WHERE c.oid >= 16384
		  AND NOT EXISTS (
				SELECT 1 FROM pg_depend d
				WHERE d.objid = c.oid AND d.classid = 'pg_cast'::regclass AND d.deptype = 'e')
		ORDER BY source_schema, source_type, target_schema, target_type`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        10 * time.Second,
		Cadence:        CadenceDaily,
	})

	// pg_collations_v1: user-defined collations. A
	// query with COLLATE <user collation>, or a column/index using one,
	// cannot be analysed accurately unless it exists. Built-ins are in
	// pg_catalog (schema filter); extension-owned excluded via
	// pg_depend. ICU-locale columns vary across PG majors and are
	// omitted for version-stability (libc collcollate/collctype emitted;
	// ICU-locale fidelity is a documented follow-up).
	//
	// Specification: specifications/collectors/pg_collations_v1.md
	Register(QueryDef{
		ID:       "pg_collations_v1",
		Category: "schema",
		SQL: `SELECT
			n.nspname AS schemaname,
			c.collname,
			c.collprovider::text AS provider,
			c.collcollate,
			c.collctype,
			c.collisdeterministic
		FROM pg_collation c
		JOIN pg_namespace n ON n.oid = c.collnamespace
		WHERE n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND n.nspname NOT LIKE 'pg_temp_%'
		  AND n.nspname NOT LIKE 'pg_toast_temp_%'
		  AND NOT EXISTS (
				SELECT 1 FROM pg_depend d
				WHERE d.objid = c.oid AND d.classid = 'pg_collation'::regclass AND d.deptype = 'e')
		ORDER BY n.nspname, c.collname`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        10 * time.Second,
		Cadence:        CadenceDaily,
	})

	// pg_text_search_v1: user-defined text-search configurations.
	// A query like to_tsvector('cfg', x) resolves the
	// configuration by name at PLAN time; tokenisation (which needs the
	// dictionary mappings) only happens at EXECUTION, which analysis
	// never does. So emitting the configuration (name + parser) is
	// sufficient for the config to exist and the query to plan;
	// dicts/parsers/templates/mappings are a documented follow-up.
	// Built-ins are in pg_catalog (schema filter); extension-owned
	// excluded via pg_depend.
	//
	// Specification: specifications/collectors/pg_text_search_v1.md
	Register(QueryDef{
		ID:       "pg_text_search_v1",
		Category: "schema",
		SQL: `SELECT
			n.nspname AS schemaname,
			c.cfgname,
			pn.nspname || '.' || p.prsname AS parser
		FROM pg_ts_config c
		JOIN pg_namespace n ON n.oid = c.cfgnamespace
		JOIN pg_ts_parser p ON p.oid = c.cfgparser
		JOIN pg_namespace pn ON pn.oid = p.prsnamespace
		WHERE n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND n.nspname NOT LIKE 'pg_temp_%'
		  AND n.nspname NOT LIKE 'pg_toast_temp_%'
		  AND NOT EXISTS (
				SELECT 1 FROM pg_depend d
				WHERE d.objid = c.oid AND d.classid = 'pg_ts_config'::regclass AND d.deptype = 'e')
		ORDER BY n.nspname, c.cfgname`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        10 * time.Second,
		Cadence:        CadenceDaily,
	})

	// pg_identity_columns_v1: per-column identity / SERIAL metadata
	// for user-schema tables. Distinguishes SERIAL (attidentity=''
	// + auto-owned sequence) from GENERATED ALWAYS (attidentity='a')
	// and GENERATED BY DEFAULT (attidentity='d'). Surfaces the
	// owning-sequence (schema, name) so the analyzer can build the
	// SERIAL → IDENTITY migration DDL.
	//
	// default_is_nextval is derived from the pg_depend auto-ownership
	// link rather than from parsing the default expression — avoids
	// pg_get_expr and the literal-value exposure that comes with it.
	//
	// Specification: specifications/collectors/pg_identity_columns_v1.md
	Register(QueryDef{
		ID:           "pg_identity_columns_v1",
		Category:     "schema",
		MinPGVersion: 10,
		SQL: `SELECT
			n.nspname AS schemaname,
			c.relname,
			a.attname,
			t.typname AS atttypname,
			a.attidentity,
			a.atthasdef,
			(a.atthasdef AND dep.refobjid IS NOT NULL) AS default_is_nextval,
			sn.nspname AS sequence_schema,
			sc.relname AS sequence_name,
			(dep.refobjid IS NOT NULL) AS auto_owned_sequence,
			COALESCE(pk.is_pk, false) AS is_primary_key,
			COALESCE(uq.is_uq, false) AS is_unique
		FROM pg_attribute a
		JOIN pg_class c ON c.oid = a.attrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_type t ON t.oid = a.atttypid
		LEFT JOIN pg_depend dep
		  ON dep.classid = 'pg_class'::regclass
		 AND dep.refclassid = 'pg_class'::regclass
		 AND dep.refobjid = a.attrelid
		 AND dep.refobjsubid = a.attnum
		 AND dep.deptype = 'a'
		LEFT JOIN pg_class sc ON sc.oid = dep.objid AND sc.relkind = 'S'
		LEFT JOIN pg_namespace sn ON sn.oid = sc.relnamespace
		LEFT JOIN (
			SELECT conrelid, unnest(conkey) AS attnum, true AS is_pk
			FROM pg_constraint
			WHERE contype = 'p'
		) pk ON pk.conrelid = a.attrelid AND pk.attnum = a.attnum
		LEFT JOIN (
			SELECT DISTINCT conrelid, unnest(conkey) AS attnum, true AS is_uq
			FROM pg_constraint
			WHERE contype = 'u'
		) uq ON uq.conrelid = a.attrelid AND uq.attnum = a.attnum
		WHERE a.attnum > 0
		  AND NOT a.attisdropped
		  AND c.relkind IN ('r', 'p')
		  AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND n.nspname NOT LIKE 'pg_temp_%'
		  AND n.nspname NOT LIKE 'pg_toast_temp_%'
		  AND (
		    a.attidentity <> ''
		    OR dep.refobjid IS NOT NULL
		    OR t.typname IN ('int2', 'int4', 'int8', 'uuid')
		  )
		ORDER BY n.nspname, c.relname, a.attnum`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        30 * time.Second,
		Cadence:        CadenceDaily,
	})
}
