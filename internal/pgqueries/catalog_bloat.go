package pgqueries

import "time"

// bloat_estimate_v1 — statistical table-bloat estimate, no
// pgstattuple required. One row per non-system, table-shaped
// relation. Operators on managed PG (RDS / Aurora / Cloud SQL /
// AlloyDB / Azure Flex) can't install pgstattuple; this collector
// gives them the operator-facing answer anyway. The formula is
// the canonical PG-wiki "Show Database Bloat" derivation
// simplified to the parts that matter on modern PG and pinned to
// current_setting('block_size') so non-default page sizes work.
//
// Specification: specifications/collectors/bloat_estimate_v1.md

func init() {
	Register(QueryDef{
		ID:       "bloat_estimate_v1",
		Category: "tables",
		SQL: `WITH page AS (
    SELECT current_setting('block_size')::numeric AS bs
),
widths AS (
    SELECT
        s.schemaname,
        s.tablename,
        SUM(s.avg_width)::numeric AS sum_avg_width
    FROM pg_stats s
    WHERE s.schemaname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
    GROUP BY s.schemaname, s.tablename
),
base AS (
    SELECT
        n.nspname                                       AS schemaname,
        c.relname                                       AS tablename,
        c.oid                                           AS table_oid,
        c.relkind                                       AS relkind,
        c.reltuples::bigint                             AS reltuples,
        pg_relation_size(c.oid)                         AS actual_size_bytes,
        w.sum_avg_width
    FROM pg_class c
    JOIN pg_namespace n ON n.oid = c.relnamespace
    LEFT JOIN widths w
        ON w.schemaname = n.nspname AND w.tablename = c.relname
    WHERE c.relkind IN ('r', 'm', 'p')
      AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
      AND n.nspname NOT LIKE 'pg\_temp\_%' ESCAPE '\'
      AND n.nspname NOT LIKE 'pg\_toast\_temp\_%' ESCAPE '\'
),
estimated AS (
    SELECT
        b.*,
        CASE
            WHEN b.sum_avg_width IS NULL OR b.reltuples <= 0 THEN NULL
            ELSE (
                CEIL(
                    (b.reltuples::numeric * (23 + 4 + b.sum_avg_width + 8))
                    / GREATEST(p.bs - 24, 1::numeric)
                ) * p.bs
            )::bigint
        END AS expected_size_bytes,
        (b.sum_avg_width IS NULL) AS stats_missing
    FROM base b
    CROSS JOIN page p
)
SELECT
    e.schemaname,
    e.tablename,
    e.table_oid,
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
    s.n_live_tup,
    s.n_dead_tup,
    s.last_autovacuum,
    e.stats_missing
FROM estimated e
LEFT JOIN pg_stat_user_tables s
    ON s.schemaname = e.schemaname AND s.relname = e.tablename
ORDER BY e.schemaname, e.tablename`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        30 * time.Second,
		Cadence:        Cadence6h,
	})
}
