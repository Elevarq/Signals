package pgqueries

import "time"

// Vector column inventory — pgvector-gated. Extension-absent is handled
// by the standard RequiresExtension filter (collector is simply not
// eligible). Extension-present-but-no-vector-columns is a normal state
// (pgvector installed without being used) and yields an empty rowset —
// not a failure.

func init() {
	// pg_vector_columns_v1: enumerate pgvector columns with dimension,
	// average stored width, storage mode, compression, index coverage,
	// and a derived likely_toasted hint.
	//
	// pgvector encodes vector dimension directly in atttypmod for the
	// 'vector' type family, so dimension is read from atttypmod.
	//
	// Specification: specifications/collectors/pg_vector_columns_v1.md
	Register(QueryDef{
		ID:                "pg_vector_columns_v1",
		Category:          "schema",
		RequiresExtension: "vector",
		MinPGVersion:      14,
		SQL: `SELECT
			c.oid                                                 AS relid,
			n.nspname                                             AS schemaname,
			c.relname,
			a.attname,
			t.typname                                             AS atttypname,
			CASE WHEN a.atttypmod > 0
			     THEN a.atttypmod
			     ELSE NULL
			END                                                   AS dimension,
			s.avg_width,
			a.attstorage::text                                    AS attstorage,
			a.attcompression::text                                AS attcompression,
			(a.attstorage IN ('e', 'x')
			   AND COALESCE(s.avg_width, 0) > 2000)               AS likely_toasted,
			EXISTS (
			    SELECT 1 FROM pg_index i
			    WHERE i.indrelid = c.oid
			      AND a.attnum = ANY(i.indkey::int[])
			)                                                     AS has_index,
			(SELECT array_agg(DISTINCT am.amname ORDER BY am.amname)
			 FROM pg_index i
			 JOIN pg_class ic ON ic.oid = i.indexrelid
			 JOIN pg_am am ON am.oid = ic.relam
			 WHERE i.indrelid = c.oid
			   AND a.attnum = ANY(i.indkey::int[]))               AS index_types
		FROM pg_attribute a
		JOIN pg_class c ON c.oid = a.attrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_type t ON t.oid = a.atttypid
		LEFT JOIN pg_stats s
		       ON s.schemaname = n.nspname
		      AND s.tablename = c.relname
		      AND s.attname = a.attname
		WHERE t.typname IN ('vector', 'halfvec', 'sparsevec')
		  AND a.attnum > 0
		  AND NOT a.attisdropped
		  AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND n.nspname NOT LIKE 'pg_temp_%'
		  AND n.nspname NOT LIKE 'pg_toast_temp_%'
		ORDER BY n.nspname, c.relname, a.attnum`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        15 * time.Second,
		Cadence:        CadenceDaily,
	})
}
