package pgqueries

// catalog_pg19.go — PostgreSQL 19 capability projections.
//
// PG 19 is above the supported catalog window (MaxSupportedMajor = 18),
// so default/baseline SQL still resolves against the PG18 catalog (#816
// clamps CatalogBaselineMajor to 18 on a real PG19 target). That is a
// routing decision, NOT a full-support claim — MaxSupportedMajor is
// unchanged and stays gated on live PG19 CI.
//
// A handful of PG19 additions are single, additive, capability-gated
// columns that are safe to project on a real PG19 target without
// claiming full support. Those register here as CAPABILITY projections
// (RegisterCapabilityOverride) — keyed on the ACTUAL server major, so
// they are selected on a real PG19 even while the catalog baseline is
// clamped to 18 (#817 / CPJ-R001).
//
// This file deliberately does NOT use RegisterOverride (baseline-keyed):
// a baseline override for major 19 would never resolve on a real PG19
// target, because resolveEffectiveSQL resolves the baseline leg against
// the clamped baseline (18), not the actual major.

func init() {
	// #817 / SR-R001 — PG19 added a per-object stats_reset column to
	// pg_stat_all_tables / pg_stat_user_tables and
	// pg_stat_all_indexes / pg_stat_user_indexes (PG19 release notes,
	// commit a5b543258: "Add stats_reset column to system views
	// pg_stat_all_tables, pg_stat_all_indexes, and
	// pg_statio_all_sequences"). stats_reset is appended LAST so the
	// pre-existing identity and column order are preserved (INV-SR-01);
	// the only delta from the default SQL is the real column in place of
	// the NULL::timestamptz stub.
	RegisterCapabilityOverride(19, "pg_stat_user_tables_v1", `SELECT schemaname, relname, seq_scan, seq_tup_read,
		idx_scan, idx_tup_fetch, n_tup_ins, n_tup_upd, n_tup_del, n_tup_hot_upd,
		n_live_tup, n_dead_tup, last_vacuum, last_autovacuum, last_analyze,
		last_autoanalyze, vacuum_count, autovacuum_count, analyze_count,
		autoanalyze_count,
		stats_reset
		FROM pg_stat_user_tables
		ORDER BY schemaname, relname`)

	RegisterCapabilityOverride(19, "pg_stat_user_indexes_v1", `SELECT schemaname, relname, indexrelname, idx_scan,
		idx_tup_read, idx_tup_fetch,
		stats_reset
		FROM pg_stat_user_indexes
		ORDER BY schemaname, relname, indexrelname`)
}
