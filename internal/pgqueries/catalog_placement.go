package pgqueries

import "time"

// Placement & Storage collectors — tablespaces, per-relation storage
// accounting, and per-column storage configuration. Feed the
// toast-planner-blindspot, object-parameter-drift, and io-cost-calibration
// detectors.

func init() {
	// pg_tablespaces_v1: tablespace inventory with decoded per-tablespace
	// GUC overrides (seq_page_cost, random_page_cost,
	// effective_io_concurrency, maintenance_io_concurrency) and on-disk
	// size.
	//
	// The four cost GUCs are extracted from spcoptions via
	// pg_options_to_table — cluster-level pg_settings values do not reflect
	// per-tablespace overrides.
	//
	// Specification: specifications/collectors/pg_tablespaces_v1.md
	Register(QueryDef{
		ID:       "pg_tablespaces_v1",
		Category: "server",
		SQL: `SELECT
			t.spcname,
			t.spcowner                                   AS spcowner_oid,
			t.spcoptions                                 AS spcoptions_raw,
			(SELECT option_value::real
			   FROM pg_options_to_table(t.spcoptions)
			   WHERE option_name = 'seq_page_cost')      AS seq_page_cost,
			(SELECT option_value::real
			   FROM pg_options_to_table(t.spcoptions)
			   WHERE option_name = 'random_page_cost')   AS random_page_cost,
			(SELECT option_value::int
			   FROM pg_options_to_table(t.spcoptions)
			   WHERE option_name = 'effective_io_concurrency') AS effective_io_concurrency,
			(SELECT option_value::int
			   FROM pg_options_to_table(t.spcoptions)
			   WHERE option_name = 'maintenance_io_concurrency') AS maintenance_io_concurrency,
			pg_tablespace_size(t.oid)                    AS size_bytes
		FROM pg_tablespace t
		ORDER BY t.spcname`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        10 * time.Second,
		Cadence:        CadenceDaily,
	})

	// pg_class_storage_v1: per-relation storage accounting. Emits main,
	// TOAST heap, TOAST index, and total sizes in bytes — the ground
	// truth the planner is blind to for TOAST. reloptions carries
	// per-relation overrides for object-parameter-drift.
	//
	// Timeout raised to 60s because pg_total_relation_size() can be slow
	// on very large schemas (many per-fork lseeks).
	//
	// Specification: specifications/collectors/pg_class_storage_v1.md
	Register(QueryDef{
		ID:       "pg_class_storage_v1",
		Category: "schema",
		SQL: `SELECT
			c.oid                                         AS relid,
			n.nspname                                     AS schemaname,
			c.relname,
			c.relkind,
			c.relpersistence,
			c.relispartition,
			c.relhasindex,
			c.reltuples,
			c.relpages,
			c.relallvisible,
			c.relfrozenxid::text                          AS relfrozenxid,
			c.relminmxid::text                            AS relminmxid,
			c.reltoastrelid,
			(c.reltoastrelid <> 0)                        AS has_toast,
			tc.relpages                                   AS toast_pages,
			tic.relpages                                  AS toast_relpages_index,
			pg_relation_size(c.oid, 'main')               AS main_bytes,
			CASE WHEN c.reltoastrelid <> 0
			     THEN pg_total_relation_size(c.reltoastrelid)
			     ELSE NULL
			END                                           AS toast_bytes,
			pg_indexes_size(c.oid)                        AS indexes_bytes,
			pg_total_relation_size(c.oid)                 AS total_bytes,
			array_to_string(c.reloptions, ', ')           AS reloptions,
			COALESCE(ts.spcname, 'pg_default')            AS tablespace
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN pg_class tc ON tc.oid = c.reltoastrelid
		LEFT JOIN pg_index ti ON ti.indrelid = c.reltoastrelid
		LEFT JOIN pg_class tic ON tic.oid = ti.indexrelid
		LEFT JOIN pg_tablespace ts ON ts.oid = c.reltablespace
		WHERE c.relkind IN ('r', 'm', 'p')
		  AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND n.nspname NOT LIKE 'pg_temp_%'
		  AND n.nspname NOT LIKE 'pg_toast_temp_%'
		ORDER BY n.nspname, c.relname`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        60 * time.Second,
		Cadence:        CadenceDaily,
	})

	// pg_attribute_storage_v1: per-column storage configuration.
	// attstorage (PLAIN/EXTERNAL/EXTENDED/MAIN) and attcompression
	// (none/pglz/lz4, PG14+) drive TOAST amplification and
	// vector-column-storage advice. avg_width comes from pg_stats via
	// LEFT JOIN so columns without stats still emit (avg_width NULL).
	//
	// attstattarget surfaces the per-column statistics-sampling
	// override. Values:
	//   -1 (PG ≤ 17) or NULL (PG ≥ 18) — column uses
	//                                    default_statistics_target.
	//   ≥ 0 — operator-applied override.
	//
	// attcompression was introduced in PG 14 — collector gated accordingly.
	//
	// Specification: specifications/collectors/pg_attribute_storage_v1.md
	Register(QueryDef{
		ID:           "pg_attribute_storage_v1",
		Category:     "schema",
		MinPGVersion: 14,
		SQL: `SELECT
			c.oid                                      AS relid,
			n.nspname                                  AS schemaname,
			c.relname,
			a.attnum,
			a.attname,
			a.atttypid,
			t.typname                                  AS atttypname,
			a.attstorage::text                         AS attstorage,
			a.attcompression::text                     AS attcompression,
			a.atttypmod,
			a.attnotnull,
			a.attstattarget,
			s.avg_width
		FROM pg_attribute a
		JOIN pg_class c ON c.oid = a.attrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_type t ON t.oid = a.atttypid
		LEFT JOIN pg_stats s
		       ON s.schemaname = n.nspname
		      AND s.tablename = c.relname
		      AND s.attname = a.attname
		WHERE a.attnum > 0
		  AND NOT a.attisdropped
		  AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND n.nspname NOT LIKE 'pg_temp_%'
		  AND n.nspname NOT LIKE 'pg_toast_temp_%'
		ORDER BY n.nspname, c.relname, a.attnum`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        30 * time.Second,
		Cadence:        CadenceDaily,
	})
}
