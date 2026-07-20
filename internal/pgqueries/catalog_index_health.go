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

// index_health_summary_v2 — decision-grade index-health contract. One row per
// non-system index with explicit safety/constraint/build state, access method,
// key/INCLUDE counts, and a versioned semantic structure fingerprint so exact
// duplicates are proven by structural equality (not a key-column prefix). Usage
// counters and state booleans are emitted raw — NULL is never coerced to 0/false.
// v1 stays registered and unchanged; v2 is additive.
//
// Specification: specifications/collectors/index_health_summary_v2.md
func init() {
	Register(QueryDef{
		ID:       "index_health_summary_v2",
		Category: "indexes",
		SQL: `WITH idx_keycols AS (
    SELECT
        i.indexrelid,
        array_agg(a.attname ORDER BY pos.ord) FILTER (WHERE pos.attnum > 0) AS key_cols,
        bool_and(pos.attnum > 0)                                            AS all_key_resolved
    FROM pg_index i
    CROSS JOIN LATERAL unnest(i.indkey) WITH ORDINALITY AS pos(attnum, ord)
    LEFT JOIN pg_attribute a
        ON a.attrelid = i.indrelid AND a.attnum = pos.attnum
    WHERE pos.ord <= i.indnkeyatts
    GROUP BY i.indexrelid
),
idx AS (
    SELECT
        n.nspname                                            AS schemaname,
        t.relname                                            AS tablename,
        ic.relname                                           AS indexname,
        i.indexrelid                                         AS index_oid,
        i.indrelid                                           AS table_oid,
        pg_relation_size(i.indexrelid)                       AS size_bytes,
        s.idx_scan                                           AS idx_scan,
        s.idx_tup_read                                       AS idx_tup_read,
        s.idx_tup_fetch                                      AS idx_tup_fetch,
        i.indisvalid                                         AS is_valid,
        i.indisready                                         AS is_ready,
        i.indislive                                          AS is_live,
        i.indisprimary                                       AS is_primary,
        i.indisunique                                        AS is_unique,
        i.indisexclusion                                     AS is_exclusion,
        i.indimmediate                                       AS is_immediate,
        i.indisreplident                                     AS is_replica_identity,
        (con.contype IS NOT NULL)                            AS is_constraint_backed,
        CASE
            WHEN con.contype IS NULL  THEN NULL
            WHEN con.contype = 'p'    THEN 'primary'
            WHEN con.contype = 'u'    THEN 'unique'
            WHEN con.contype = 'x'    THEN 'exclusion'
            ELSE 'other'
        END                                                  AS constraint_type,
        CASE
            WHEN p.command IS NOT NULL
                THEN CASE WHEN left(p.command, 2) = 'RE' THEN 'active_reindex' ELSE 'active_build' END
            WHEN NOT i.indisvalid THEN 'invalid_residue'
            WHEN NOT i.indisready THEN 'not_ready_residue'
            ELSE 'ready'
        END                                                  AS build_state,
        am.amname                                            AS access_method,
        i.indnkeyatts                                        AS key_column_count,
        (i.indnatts - i.indnkeyatts)                         AS include_column_count,
        1                                                    AS structure_version,
        md5(
            (CASE WHEN i.indisunique THEN 'U:' ELSE 'N:' END)
            || (CASE WHEN i.indisexclusion THEN 'X:' || COALESCE(con.conexclop::text, '') ELSE '' END)
            || regexp_replace(pg_get_indexdef(i.indexrelid), '^.*? USING ', '')
        )                                                    AS structure_fingerprint,
        CASE WHEN kc.all_key_resolved THEN kc.key_cols ELSE NULL::text[] END AS key_cols
    FROM pg_index i
    JOIN pg_class ic          ON ic.oid = i.indexrelid
    JOIN pg_class t           ON t.oid  = i.indrelid
    JOIN pg_namespace n       ON n.oid  = ic.relnamespace
    JOIN pg_am am             ON am.oid = ic.relam
    LEFT JOIN pg_stat_user_indexes s        ON s.indexrelid = i.indexrelid
    -- conindid is also populated on foreign-key rows (pointing at the
    -- referenced index), so match only constraints this index OWNS
    -- (primary/unique/exclusion) and collapse to one deterministic row.
    LEFT JOIN LATERAL (
        SELECT c.contype, c.conexclop
        FROM pg_constraint c
        WHERE c.conindid = i.indexrelid
          AND c.contype IN ('p', 'u', 'x')
        ORDER BY CASE c.contype WHEN 'p' THEN 0 WHEN 'u' THEN 1 ELSE 2 END
        LIMIT 1
    ) con ON TRUE
    -- one progress row per backend, so collapse to a single row per index.
    LEFT JOIN LATERAL (
        SELECT pr.command
        FROM pg_stat_progress_create_index pr
        WHERE pr.index_relid = i.indexrelid
        LIMIT 1
    ) p ON TRUE
    LEFT JOIN idx_keycols kc                ON kc.indexrelid = i.indexrelid
    WHERE n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
      AND n.nspname NOT LIKE 'pg\_temp\_%' ESCAPE '\'
      AND n.nspname NOT LIKE 'pg\_toast\_temp\_%' ESCAPE '\'
)
SELECT
    m.schemaname,
    m.tablename,
    m.indexname,
    m.index_oid,
    m.table_oid,
    m.size_bytes,
    m.idx_scan,
    m.idx_tup_read,
    m.idx_tup_fetch,
    m.is_valid,
    m.is_ready,
    m.is_live,
    m.is_primary,
    m.is_unique,
    m.is_exclusion,
    m.is_immediate,
    m.is_replica_identity,
    m.is_constraint_backed,
    m.constraint_type,
    m.build_state,
    m.access_method,
    m.key_column_count,
    m.include_column_count,
    m.structure_version,
    m.structure_fingerprint,
    dup.indexname  AS exact_duplicate_of,
    pref.indexname AS prefix_candidate_of,
    CASE WHEN pref.indexname IS NOT NULL THEN 'key_column_left_prefix' END AS prefix_candidate_basis
FROM idx m
LEFT JOIN LATERAL (
    SELECT m2.indexname
    FROM idx m2
    WHERE m2.schemaname            = m.schemaname
      AND m2.tablename             = m.tablename
      AND m2.structure_fingerprint IS NOT NULL
      AND m.structure_fingerprint  IS NOT NULL
      AND m2.structure_version     = m.structure_version
      AND m2.structure_fingerprint = m.structure_fingerprint
      AND m2.index_oid             < m.index_oid
    ORDER BY m2.index_oid ASC
    LIMIT 1
) dup ON TRUE
LEFT JOIN LATERAL (
    SELECT m2.indexname
    FROM idx m2
    WHERE m2.schemaname = m.schemaname
      AND m2.tablename  = m.tablename
      AND m2.key_cols   IS NOT NULL
      AND m.key_cols    IS NOT NULL
      AND array_length(m2.key_cols, 1) > array_length(m.key_cols, 1)
      AND m2.key_cols[1:array_length(m.key_cols, 1)] = m.key_cols
    ORDER BY array_length(m2.key_cols, 1) ASC, m2.indexname ASC
    LIMIT 1
) pref ON TRUE
ORDER BY m.schemaname, m.tablename, m.indexname`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        30 * time.Second,
		Cadence:        Cadence6h,
	})
}
