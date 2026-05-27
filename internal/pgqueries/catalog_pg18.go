package pgqueries

// catalog_pg18.go — version-specific overrides for PostgreSQL 18.
//
// PG 18 changed the column shape of two pg_stat_* views in
// backwards-incompatible ways. Each override below emits the same
// canonical column set the consumer expects (R081: stable logical IDs,
// normalized output across versions); only the SQL underneath differs.

func init() {
	// #210 — real PG 14+ session/timing columns for pg_stat_database_v1
	// (the session view shape did not change in PG 18).
	RegisterOverride(18, "pg_stat_database_v1", pgStatDatabaseV14SQL)

	// pg_stat_io: PG 18 split `op_bytes` (a single per-row size) into
	// per-direction byte counters: `read_bytes`, `write_bytes`,
	// `extend_bytes`. The op_bytes column was removed.
	//
	// Canonical schema (see catalog_io.go default SQL): the union of
	// both shapes — emit op_bytes (NULL on PG 18) plus the three new
	// byte counters (NULL on PG 16/17). Same column order, same names,
	// only the populated subset differs by major.
	RegisterOverride(18, "pg_stat_io_v1", `SELECT
		backend_type,
		object,
		context,
		reads,
		read_time,
		writes,
		write_time,
		writebacks,
		writeback_time,
		extends,
		extend_time,
		NULL::bigint AS op_bytes,
		read_bytes,
		write_bytes,
		extend_bytes,
		hits,
		evictions,
		reuses,
		fsyncs,
		fsync_time,
		stats_reset
	FROM pg_stat_io
	ORDER BY backend_type, object, context`)

	// pg_stat_wal: PG 18 trimmed the view aggressively. The write and
	// sync counters (wal_write, wal_sync, wal_write_time, wal_sync_time)
	// were removed entirely — that data is no longer available from
	// pg_stat_wal. PG 18's pg_stat_wal exposes only:
	//   wal_records, wal_fpi, wal_bytes, wal_buffers_full, stats_reset
	//
	// The equivalent write/sync visibility is now in pg_stat_io
	// per-backend-type rows. Consumers that need it should read
	// pg_stat_io_v1's `writes` / `write_time` / `fsyncs` /
	// `fsync_time` columns and aggregate.
	//
	// Canonical schema is preserved by emitting NULL stubs for the
	// removed columns — same approach as op_bytes on pg_stat_io_v1.
	// Downstream consumers see a stable column list across majors;
	// only the populated subset varies.
	RegisterOverride(18, "pg_stat_wal_v1", `SELECT
		wal_records,
		wal_fpi,
		wal_bytes,
		wal_buffers_full,
		NULL::bigint           AS wal_write,
		NULL::bigint           AS wal_sync,
		NULL::double precision AS wal_write_time,
		NULL::double precision AS wal_sync_time,
		stats_reset
	FROM pg_stat_wal`)
}
