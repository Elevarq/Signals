package pgqueries

import "time"

// TimescaleDB / Tiger Data collector family (R114) — gated on the
// 'timescaledb' extension via RequiresExtension (plain PostgreSQL
// targets skip every member with reason=extension_missing, EA-R001 /
// INV-SIGNALS-24) and on extension version ≥ 2.14 via
// RequiresExtensionMinVersion (R115; below that the multi-node-era
// catalog predates the views/functions used here — only the detection
// collector remains eligible).
//
// Sources are restricted to the documented, PUBLIC-readable surface:
// timescaledb_information views plus the catalog-priced functions
// hypertable_approximate_detailed_size() and
// hypertable_compression_stats(). Internal _timescaledb_* catalogs,
// timescaledb_experimental.policies (deprecated upstream), and the
// exact size functions (O(chunks) AccessShareLocks per call) are
// deliberately not used — rationale in
// docs/timescaledb-collectors-design.md § 2-3.
//
// Version-variant views are captured with dynamic columns (SELECT *,
// R037 precedent) so each target's rows carry exactly what its
// TimescaleDB version exposes (e.g. hypertables.primary_dimension
// ≥ 2.20, continuous_aggregates.finalized ≤ 2.24). The two LATERAL
// function calls are unqualified: the extension's API schema (default
// `public`) must be on the collector role's search_path; when it is
// not, the failure is isolated to the collector's savepoint and
// classified `object_missing` (FC-TSDB-03).
//
// Specification: specifications/collectors/timescaledb_family_v1.md
// Acceptance:    specifications/collectors/timescaledb_family_v1.acceptance.md

func init() {
	// timescaledb_extension_v1: detection + capability flags. Exactly
	// one row when the extension is installed. Capabilities are
	// feature-detected via to_regclass/to_regnamespace existence
	// probes — never inferred from version tables; extversion is
	// provenance for the family's snapshot. No
	// RequiresExtensionMinVersion: detection must work on any
	// TimescaleDB version (FC-TSDB-02).
	Register(QueryDef{
		ID:                "timescaledb_extension_v1",
		Category:          "timescaledb",
		RequiresExtension: "timescaledb",
		MinPGVersion:      14,
		SQL: `SELECT
			e.extversion,
			e.extnamespace::regnamespace::text AS extension_schema,
			current_setting('timescaledb.license', true) AS license,
			current_setting('timescaledb.telemetry_level', true) AS telemetry_level,
			(to_regclass('timescaledb_information.hypertables') IS NOT NULL) AS has_information_views,
			(to_regclass('timescaledb_information.job_history') IS NOT NULL) AS has_job_history,
			(to_regclass('timescaledb_information.hypertable_columnstore_settings') IS NOT NULL) AS has_columnstore_aliases,
			(to_regclass('timescaledb_experimental.policies') IS NOT NULL) AS has_experimental_policies,
			(to_regnamespace('_timescaledb_functions') IS NOT NULL) AS has_functions_schema,
			(to_regclass('_timescaledb_catalog.bgw_job') IS NOT NULL) AS bgw_job_in_catalog
		FROM pg_catalog.pg_extension e
		WHERE e.extname = 'timescaledb'`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionLong,
		Timeout:        5 * time.Second,
		Cadence:        Cadence6h,
	})

	// timescaledb_hypertables_v1: hypertable inventory. Dynamic
	// columns: primary_dimension/primary_dimension_type appear ≥ 2.20.
	Register(QueryDef{
		ID:                          "timescaledb_hypertables_v1",
		Category:                    "timescaledb",
		RequiresExtension:           "timescaledb",
		RequiresExtensionMinVersion: "2.14",
		MinPGVersion:                14,
		SQL: `SELECT *
		FROM timescaledb_information.hypertables
		ORDER BY hypertable_schema, hypertable_name`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        15 * time.Second,
		Cadence:        Cadence6h,
	})

	// timescaledb_dimensions_v1: partitioning dimensions (time +
	// space) per hypertable.
	Register(QueryDef{
		ID:                          "timescaledb_dimensions_v1",
		Category:                    "timescaledb",
		RequiresExtension:           "timescaledb",
		RequiresExtensionMinVersion: "2.14",
		MinPGVersion:                14,
		SQL: `SELECT *
		FROM timescaledb_information.dimensions
		ORDER BY hypertable_schema, hypertable_name, dimension_number`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        15 * time.Second,
		Cadence:        CadenceDaily,
	})

	// timescaledb_chunks_v1: per-chunk rows, newest CREATED first,
	// bounded at 5000 (TC-TSDB-15). Truncation is always detectable
	// against timescaledb_chunk_summary_v1.chunk_count. Ordering by
	// chunk_creation_time (not range_end) keeps the cap uniformly
	// newest-first across time- AND integer-dimension hypertables —
	// integer-dimension chunks have NULL range_end, and range-based
	// NULLS LAST ordering would push every one of them past the cap
	// whenever 5000+ time-dimension chunks exist.
	Register(QueryDef{
		ID:                          "timescaledb_chunks_v1",
		Category:                    "timescaledb",
		RequiresExtension:           "timescaledb",
		RequiresExtensionMinVersion: "2.14",
		MinPGVersion:                14,
		SQL: `SELECT *
		FROM timescaledb_information.chunks
		ORDER BY chunk_creation_time DESC,
			hypertable_schema, hypertable_name, chunk_name
		LIMIT 5000`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        30 * time.Second,
		Cadence:        Cadence6h,
	})

	// timescaledb_chunk_summary_v1: complete per-hypertable rollup —
	// one row per hypertable regardless of chunk count, so the
	// chunks_v1 cap never hides topology (TC-TSDB-05/15). Cadence
	// matches chunks_v1 (6h): both fully evaluate the chunks view —
	// a multi-way catalog join that is seconds-scale at 1e5 chunks —
	// and chunk counts move at chunk-creation speed (hours).
	Register(QueryDef{
		ID:                          "timescaledb_chunk_summary_v1",
		Category:                    "timescaledb",
		RequiresExtension:           "timescaledb",
		RequiresExtensionMinVersion: "2.14",
		MinPGVersion:                14,
		SQL: `SELECT
			hypertable_schema,
			hypertable_name,
			count(*)                              AS chunk_count,
			count(*) FILTER (WHERE is_compressed) AS compressed_chunk_count,
			min(range_start)                      AS oldest_range_start,
			max(range_end)                        AS newest_range_end,
			min(range_start_integer)              AS oldest_range_start_integer,
			max(range_end_integer)                AS newest_range_end_integer,
			min(chunk_creation_time)              AS oldest_chunk_created_at,
			max(chunk_creation_time)              AS newest_chunk_created_at
		FROM timescaledb_information.chunks
		GROUP BY hypertable_schema, hypertable_name
		ORDER BY hypertable_schema, hypertable_name`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        30 * time.Second,
		Cadence:        Cadence6h,
	})

	// timescaledb_hypertable_sizes_v1: approximate sizes via the
	// monitoring-priced hypertable_approximate_detailed_size()
	// (smgr-cache backed, introduced 2.14.0 for exactly this use).
	// to_regclass() (not a hard ::regclass cast, which would raise
	// 42P01 and fail the whole collector) degrades a hypertable
	// dropped between the view read and the resolution to a NULL
	// argument. Verified against 2.27.2: this helper is NOT strict
	// and returns one all-NULL row for a NULL argument, while
	// hypertable_compression_stats below IS strict and returns no
	// row — either way the LEFT JOIN preserves the hypertable row
	// with NULL metrics.
	Register(QueryDef{
		ID:                          "timescaledb_hypertable_sizes_v1",
		Category:                    "timescaledb",
		RequiresExtension:           "timescaledb",
		RequiresExtensionMinVersion: "2.14",
		MinPGVersion:                14,
		SQL: `SELECT
			h.hypertable_schema,
			h.hypertable_name,
			s.table_bytes,
			s.index_bytes,
			s.toast_bytes,
			s.total_bytes
		FROM timescaledb_information.hypertables h
		LEFT JOIN LATERAL hypertable_approximate_detailed_size(
			to_regclass(format('%I.%I', h.hypertable_schema, h.hypertable_name))
		) s ON true
		ORDER BY h.hypertable_schema, h.hypertable_name`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        30 * time.Second,
		Cadence:        Cadence1h,
	})

	// timescaledb_compression_settings_v1: per-hypertable rolled-up
	// segmentby/orderby settings. Queries the pre-rename view name —
	// valid across the whole 2.14 → 2.27 window; the 2.18+
	// columnstore alias is recorded as a capability flag by
	// timescaledb_extension_v1 (has_columnstore_aliases). Dynamic
	// columns: `index` appears ≥ 2.22. The upstream view exposes
	// hypertable identity only as a regclass, whose text rendering
	// (schema-qualified or not) follows the session search_path; the
	// collector session uses the server-default search_path, so both
	// the emitted value and this ORDER BY are stable per target.
	Register(QueryDef{
		ID:                          "timescaledb_compression_settings_v1",
		Category:                    "timescaledb",
		RequiresExtension:           "timescaledb",
		RequiresExtensionMinVersion: "2.14",
		MinPGVersion:                14,
		SQL: `SELECT *
		FROM timescaledb_information.hypertable_compression_settings
		ORDER BY hypertable::text`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        15 * time.Second,
		Cadence:        CadenceDaily,
	})

	// timescaledb_compression_stats_v1: per-hypertable before/after
	// compression statistics — pure catalog reads of sizes recorded
	// at compression time (upstream-documented accuracy caveat: later
	// inserts into a compressed chunk do not update them). Byte
	// columns are NULL when nothing is compressed; ratio derivation
	// is Analyzer-side (INV-SIGNALS-01). node_name is omitted — a
	// permanent NULL since multi-node was removed in 2.14.
	Register(QueryDef{
		ID:                          "timescaledb_compression_stats_v1",
		Category:                    "timescaledb",
		RequiresExtension:           "timescaledb",
		RequiresExtensionMinVersion: "2.14",
		MinPGVersion:                14,
		SQL: `SELECT
			h.hypertable_schema,
			h.hypertable_name,
			s.total_chunks,
			s.number_compressed_chunks,
			s.before_compression_table_bytes,
			s.before_compression_index_bytes,
			s.before_compression_toast_bytes,
			s.before_compression_total_bytes,
			s.after_compression_table_bytes,
			s.after_compression_index_bytes,
			s.after_compression_toast_bytes,
			s.after_compression_total_bytes
		FROM timescaledb_information.hypertables h
		LEFT JOIN LATERAL hypertable_compression_stats(
			to_regclass(format('%I.%I', h.hypertable_schema, h.hypertable_name))
		) s ON true
		ORDER BY h.hypertable_schema, h.hypertable_name`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        30 * time.Second,
		Cadence:        Cadence1h,
	})

	// timescaledb_continuous_aggregates_v1: cagg inventory. The
	// defining SELECT (view_definition) is application-authored SQL —
	// R075 redact path: with the high-sensitivity opt-out active the
	// collector keeps running and view_definition is NULL-ed per row.
	// Dynamic columns: `finalized` exists ≤ 2.24 only.
	Register(QueryDef{
		ID:                          "timescaledb_continuous_aggregates_v1",
		Category:                    "timescaledb",
		RequiresExtension:           "timescaledb",
		RequiresExtensionMinVersion: "2.14",
		MinPGVersion:                14,
		SQL: `SELECT *
		FROM timescaledb_information.continuous_aggregates
		ORDER BY view_schema, view_name`,
		ResultKind:       ResultRowset,
		RetentionClass:   RetentionMedium,
		Timeout:          15 * time.Second,
		Cadence:          Cadence6h,
		HighSensitivity:  true,
		SensitiveColumns: []string{"view_definition"},
	})

	// timescaledb_jobs_v1: every automation-framework job — built-in
	// (job_id < 1000: telemetry, log retention), policy jobs
	// (retention / compression / cagg refresh, identified by
	// proc_name with settings in the config JSONB), and user-defined
	// actions. This is the canonical policy surface; the deprecated
	// timescaledb_experimental.policies view is intentionally unused.
	Register(QueryDef{
		ID:                          "timescaledb_jobs_v1",
		Category:                    "timescaledb",
		RequiresExtension:           "timescaledb",
		RequiresExtensionMinVersion: "2.14",
		MinPGVersion:                14,
		SQL: `SELECT *
		FROM timescaledb_information.jobs
		ORDER BY job_id`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        15 * time.Second,
		Cadence:        Cadence1h,
	})

	// timescaledb_job_stats_v1: per-job run statistics (success /
	// failure counters, last run status, next start). Visible for ALL
	// jobs regardless of ownership — the always-available signal for
	// job-failure analysis when job_errors rows are owner-filtered.
	// On < 2.23 jobs that never ran are absent (upstream INNER JOIN);
	// ≥ 2.23 they appear with NULL stats.
	Register(QueryDef{
		ID:                          "timescaledb_job_stats_v1",
		Category:                    "timescaledb",
		RequiresExtension:           "timescaledb",
		RequiresExtensionMinVersion: "2.14",
		MinPGVersion:                14,
		SQL: `SELECT *
		FROM timescaledb_information.job_stats
		ORDER BY job_id`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionShort,
		Timeout:        15 * time.Second,
		Cadence:        Cadence15m,
	})

	// timescaledb_job_errors_v1: failed/crashed job executions.
	// Partial by design for least-privilege roles: the upstream view
	// is security_barrier-filtered to job-owner / database-owner role
	// membership, so zero rows with status=success is the expected
	// state for the standard arq_signals role (FC-TSDB-05) — never an
	// error. err_message can embed data values → R075 redact path.
	// Bounded newest-first: the backing table is per-execution (a
	// crash-looping job can accumulate far more rows than the monthly
	// retention job prunes).
	Register(QueryDef{
		ID:                          "timescaledb_job_errors_v1",
		Category:                    "timescaledb",
		RequiresExtension:           "timescaledb",
		RequiresExtensionMinVersion: "2.14",
		MinPGVersion:                14,
		SQL: `SELECT *
		FROM timescaledb_information.job_errors
		ORDER BY start_time DESC NULLS LAST, job_id
		LIMIT 1000`,
		ResultKind:       ResultRowset,
		RetentionClass:   RetentionMedium,
		Timeout:          15 * time.Second,
		Cadence:          Cadence1h,
		HighSensitivity:  true,
		SensitiveColumns: []string{"err_message"},
	})
}
