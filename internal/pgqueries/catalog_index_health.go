package pgqueries

import "time"

// index_health_summary_v1 — one row per non-system index with
// derived hygiene findings (unused / large_unused / invalid /
// not_ready / redundant / duplicate). Centralises the canonical
// index-audit derivation so analyzer consumers ingest already-
// classified rows.
//
// Specification: specifications/collectors/index_health_summary_v1.md

func init() {
	Register(QueryDef{
		ID:       "index_health_summary_v1",
		Category: "indexes",
		SQL: `WITH idx_cols AS (
    SELECT
        i.indexrelid,
        array_agg(a.attname ORDER BY pos.ord)
            FILTER (WHERE pos.attnum > 0) AS column_set,
        bool_and(pos.attnum > 0)          AS all_columns_resolved
    FROM pg_index i
    CROSS JOIN LATERAL unnest(i.indkey) WITH ORDINALITY AS pos(attnum, ord)
    LEFT JOIN pg_attribute a
        ON a.attrelid = i.indrelid AND a.attnum = pos.attnum
    WHERE pos.ord <= i.indnkeyatts
    GROUP BY i.indexrelid
),
idx_meta AS (
    SELECT
        n.nspname                                                                AS schemaname,
        t.relname                                                                AS tablename,
        ic.relname                                                               AS indexname,
        i.indexrelid                                                             AS index_oid,
        pg_relation_size(i.indexrelid)                                           AS size_bytes,
        s.idx_scan                                                               AS idx_scan,
        s.idx_tup_read                                                           AS idx_tup_read,
        i.indisunique                                                            AS is_unique,
        i.indisprimary                                                           AS is_primary,
        i.indisvalid                                                             AS is_valid,
        i.indisready                                                             AS is_ready,
        CASE WHEN c.all_columns_resolved THEN c.column_set ELSE NULL::text[] END AS column_set
    FROM pg_index i
    JOIN pg_class ic     ON ic.oid = i.indexrelid
    JOIN pg_class t      ON t.oid  = i.indrelid
    JOIN pg_namespace n  ON n.oid  = ic.relnamespace
    LEFT JOIN pg_stat_user_indexes s ON s.indexrelid = i.indexrelid
    LEFT JOIN idx_cols c             ON c.indexrelid = i.indexrelid
    WHERE n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
      AND n.nspname NOT LIKE 'pg\_temp\_%' ESCAPE '\'
      AND n.nspname NOT LIKE 'pg\_toast\_temp\_%' ESCAPE '\'
)
SELECT
    m.schemaname,
    m.tablename,
    m.indexname,
    m.index_oid,
    m.size_bytes,
    m.idx_scan,
    m.idx_tup_read,
    m.is_unique,
    m.is_primary,
    m.is_valid,
    m.is_ready,
    m.column_set,
    dup.indexname AS duplicate_of,
    red.indexname AS redundant_with,
    ARRAY_REMOVE(ARRAY[
        CASE WHEN COALESCE(m.idx_scan, 0) = 0
                  AND NOT m.is_unique
                  AND NOT m.is_primary
             THEN 'unused' END,
        CASE WHEN COALESCE(m.idx_scan, 0) = 0
                  AND NOT m.is_unique
                  AND NOT m.is_primary
                  AND m.size_bytes > 104857600
             THEN 'large_unused' END,
        CASE WHEN NOT m.is_valid  THEN 'invalid'   END,
        CASE WHEN NOT m.is_ready  THEN 'not_ready' END,
        CASE WHEN red.indexname IS NOT NULL THEN 'redundant' END,
        CASE WHEN dup.indexname IS NOT NULL THEN 'duplicate' END
    ], NULL) AS health_findings
FROM idx_meta m
LEFT JOIN LATERAL (
    SELECT m2.indexname
    FROM idx_meta m2
    WHERE m2.schemaname = m.schemaname
      AND m2.tablename  = m.tablename
      AND m2.column_set IS NOT NULL
      AND m.column_set  IS NOT NULL
      AND m2.column_set = m.column_set
      AND m2.index_oid  < m.index_oid
    ORDER BY m2.index_oid ASC
    LIMIT 1
) dup ON TRUE
LEFT JOIN LATERAL (
    SELECT m2.indexname
    FROM idx_meta m2
    WHERE m2.schemaname = m.schemaname
      AND m2.tablename  = m.tablename
      AND m2.column_set IS NOT NULL
      AND m.column_set  IS NOT NULL
      AND array_length(m2.column_set, 1) > array_length(m.column_set, 1)
      AND m2.column_set[1:array_length(m.column_set, 1)] = m.column_set
    ORDER BY array_length(m2.column_set, 1) ASC, m2.indexname ASC
    LIMIT 1
) red ON TRUE
ORDER BY m.schemaname, m.tablename, m.indexname`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        30 * time.Second,
		Cadence:        Cadence6h,
	})
}
