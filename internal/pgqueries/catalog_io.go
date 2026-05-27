package pgqueries

import "time"

// I/O Calibration Pack — runtime I/O counters that feed the analyzer's
// io-cost-calibration detector. Collectors in this file emit raw cumulative
// counters; deltas are computed analyzer-side per delta-semantics.md.

func init() {
	// pg_stat_io_v1: per (backend_type, object, context) physical I/O
	// counters from pg_stat_io. Introduced in PG 16.
	//
	// Output schema (normalized across PG majors):
	//   - reads / writes / extends                    (op counts; all majors)
	//   - read_time / write_time / extend_time        (timings; all majors)
	//   - writebacks / writeback_time / hits          (all majors)
	//   - evictions / reuses / fsyncs / fsync_time    (all majors)
	//   - op_bytes      (PG 16/17 native; NULL on PG 18+)
	//   - read_bytes    (NULL on PG 16/17; PG 18+ native)
	//   - write_bytes   (NULL on PG 16/17; PG 18+ native)
	//   - extend_bytes  (NULL on PG 16/17; PG 18+ native)
	//   - stats_reset
	//
	// PG 18 split op_bytes (single per-row size) into separate
	// read_bytes / write_bytes / extend_bytes columns. The catalog
	// emits the union of both schemas so consumers see a stable column
	// set; only the populated subset varies. The PG 18 SQL lives in
	// catalog_pg18.go.
	//
	// Specification: specifications/collectors/pg_stat_io_v1.md
	Register(QueryDef{
		ID:           "pg_stat_io_v1",
		Category:     "io",
		MinPGVersion: 16,
		SQL: `SELECT
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
			op_bytes,
			NULL::bigint AS read_bytes,
			NULL::bigint AS write_bytes,
			NULL::bigint AS extend_bytes,
			hits,
			evictions,
			reuses,
			fsyncs,
			fsync_time,
			stats_reset
		FROM pg_stat_io
		ORDER BY backend_type, object, context`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionMedium,
		Timeout:        10 * time.Second,
		Cadence:        Cadence15m,
	})

	// pg_stat_wal_v1: cluster-wide WAL generation, write, and sync counters.
	// Introduced in PG 14. Feeds wal-retention-risk, checkpoint-pressure,
	// and the io-cost-calibration detector's WAL pressure dimension.
	//
	// PG 18 renamed wal_write -> wal_writes and wal_sync -> wal_syncs.
	// The catalog normalizes to the original column names so consumers
	// see a stable schema across majors. The PG 18 SQL lives in
	// catalog_pg18.go.
	//
	// Specification: specifications/collectors/pg_stat_wal_v1.md
	Register(QueryDef{
		ID:           "pg_stat_wal_v1",
		Category:     "server",
		MinPGVersion: 14,
		SQL: `SELECT
			wal_records,
			wal_fpi,
			wal_bytes,
			wal_buffers_full,
			wal_write,
			wal_sync,
			wal_write_time,
			wal_sync_time,
			stats_reset
		FROM pg_stat_wal`,
		ResultKind:     ResultScalar,
		RetentionClass: RetentionMedium,
		Timeout:        5 * time.Second,
		Cadence:        Cadence15m,
	})
}
