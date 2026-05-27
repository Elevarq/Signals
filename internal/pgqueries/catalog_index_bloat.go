package pgqueries

import "time"

// index_bloat_estimate_v1 — statistical index-bloat estimate,
// sibling of bloat_estimate_v1 (R104). Same statistical approach
// applied to indexes — no pgstattuple / pgstatindex required.
// Pairs naturally with index_health_summary_v1 (R103): a bloated
// + unused index is the highest-priority drop candidate.
//
// Specification: specifications/collectors/index_bloat_estimate_v1.md

func init() {
	Register(QueryDef{
		ID:       "index_bloat_estimate_v1",
		Category: "indexes",
		SQL: `WITH page AS (
    SELECT current_setting('block_size')::numeric AS bs
),
idx_widths AS (
    SELECT
        i.indexrelid,
        SUM(COALESCE(s.avg_width, 0))::numeric AS sum_avg_width,
        bool_and(pos.attnum > 0 AND s.avg_width IS NOT NULL)
            AS all_widths_resolved
    FROM pg_index i
    CROSS JOIN LATERAL unnest(i.indkey) WITH ORDINALITY AS pos(attnum, ord)
    JOIN pg_class t ON t.oid = i.indrelid
    JOIN pg_namespace n ON n.oid = t.relnamespace
    LEFT JOIN pg_attribute a
        ON a.attrelid = i.indrelid AND a.attnum = pos.attnum
    LEFT JOIN pg_stats s
        ON s.schemaname = n.nspname
       AND s.tablename  = t.relname
       AND s.attname    = a.attname
    WHERE pos.ord <= i.indnkeyatts
    GROUP BY i.indexrelid
),
base AS (
    SELECT
        n.nspname                                  AS schemaname,
        t.relname                                  AS tablename,
        ic.relname                                 AS indexname,
        i.indexrelid                               AS index_oid,
        ic.relkind                                 AS relkind,
        ic.reltuples::bigint                       AS reltuples,
        pg_relation_size(i.indexrelid)             AS actual_size_bytes,
        i.indisunique                              AS is_unique,
        i.indisprimary                             AS is_primary,
        w.sum_avg_width,
        w.all_widths_resolved
    FROM pg_index i
    JOIN pg_class ic     ON ic.oid = i.indexrelid
    JOIN pg_class t      ON t.oid  = i.indrelid
    JOIN pg_namespace n  ON n.oid  = ic.relnamespace
    LEFT JOIN idx_widths w ON w.indexrelid = i.indexrelid
    WHERE ic.relkind IN ('i', 'I')
      AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
      AND n.nspname NOT LIKE 'pg\_temp\_%' ESCAPE '\'
      AND n.nspname NOT LIKE 'pg\_toast\_temp\_%' ESCAPE '\'
),
estimated AS (
    SELECT
        b.*,
        CASE
            WHEN NOT b.all_widths_resolved
              OR b.sum_avg_width IS NULL
              OR b.reltuples <= 0
            THEN NULL
            ELSE (
                CEIL(
                    (b.reltuples::numeric * (b.sum_avg_width + 8 + 4))
                    / GREATEST(p.bs - 24, 1::numeric)
                ) * p.bs
            )::bigint
        END AS expected_size_bytes,
        (NOT COALESCE(b.all_widths_resolved, FALSE)) AS stats_missing
    FROM base b
    CROSS JOIN page p
)
SELECT
    e.schemaname,
    e.tablename,
    e.indexname,
    e.index_oid,
    e.relkind,
    e.actual_size_bytes,
    e.expected_size_bytes,
    CASE
        WHEN e.expected_size_bytes IS NULL THEN 0::bigint
        WHEN e.actual_size_bytes > e.expected_size_bytes
            THEN e.actual_size_bytes - e.expected_size_bytes
        ELSE 0::bigint
    END AS bloat_bytes,
    CASE
        WHEN e.expected_size_bytes IS NULL OR e.actual_size_bytes <= 0 THEN NULL
        ELSE ROUND(
            GREATEST(e.actual_size_bytes - e.expected_size_bytes, 0)::numeric
                / e.actual_size_bytes::numeric,
            3
        )
    END AS bloat_ratio,
    e.reltuples,
    e.is_unique,
    e.is_primary,
    e.stats_missing
FROM estimated e
ORDER BY e.schemaname, e.tablename, e.indexname`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        30 * time.Second,
		Cadence:        Cadence6h,
	})
}
