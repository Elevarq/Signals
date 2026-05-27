package pgqueries

import "time"

// pg_stat_progress_* family — in-flight operation visibility.
//
// One collector per upstream view. All gated to PG 14+ and share
// the same cadence (5m), retention class (short), and result kind
// (rowset). Empty rowsets are the success state — most cycles
// against a quiet cluster will emit zero rows.
//
// Specification: specifications/collectors/pg_stat_progress_family_v1.md

func init() {
	// pg_stat_progress_vacuum: per-(auto)vacuum operation. Surfaces
	// phase + heap scan progress. Column shape drifts twice across
	// the supported window:
	//   - PG 14, 15, 16: max_dead_tuples, num_dead_tuples.
	//   - PG 17:         renamed to max_dead_tuple_bytes /
	//                    dead_tuple_bytes, added num_dead_item_ids,
	//                    indexes_total, indexes_processed.
	//   - PG 18:         PG 17 columns + delay_time.
	// The canonical SQL emits the union of every shape with NULL
	// stubs; per-major overrides populate the real columns.
	Register(QueryDef{
		ID:           "pg_stat_progress_vacuum_v1",
		Category:     "progress",
		MinPGVersion: 14,
		SQL: `SELECT
			pid,
			datid,
			datname,
			relid,
			phase,
			heap_blks_total,
			heap_blks_scanned,
			heap_blks_vacuumed,
			index_vacuum_count,
			max_dead_tuples,
			num_dead_tuples,
			NULL::bigint           AS max_dead_tuple_bytes,
			NULL::bigint           AS dead_tuple_bytes,
			NULL::bigint           AS num_dead_item_ids,
			NULL::bigint           AS indexes_total,
			NULL::bigint           AS indexes_processed,
			NULL::double precision AS delay_time
		FROM pg_stat_progress_vacuum
		ORDER BY pid`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionShort,
		Timeout:        5 * time.Second,
		Cadence:        Cadence5m,
	})

	RegisterOverride(17, "pg_stat_progress_vacuum_v1", `SELECT
		pid,
		datid,
		datname,
		relid,
		phase,
		heap_blks_total,
		heap_blks_scanned,
		heap_blks_vacuumed,
		index_vacuum_count,
		NULL::bigint           AS max_dead_tuples,
		NULL::bigint           AS num_dead_tuples,
		max_dead_tuple_bytes,
		dead_tuple_bytes,
		num_dead_item_ids,
		indexes_total,
		indexes_processed,
		NULL::double precision AS delay_time
	FROM pg_stat_progress_vacuum
	ORDER BY pid`)

	RegisterOverride(18, "pg_stat_progress_vacuum_v1", `SELECT
		pid,
		datid,
		datname,
		relid,
		phase,
		heap_blks_total,
		heap_blks_scanned,
		heap_blks_vacuumed,
		index_vacuum_count,
		NULL::bigint AS max_dead_tuples,
		NULL::bigint AS num_dead_tuples,
		max_dead_tuple_bytes,
		dead_tuple_bytes,
		num_dead_item_ids,
		indexes_total,
		indexes_processed,
		delay_time
	FROM pg_stat_progress_vacuum
	ORDER BY pid`)

	// pg_stat_progress_analyze: per-ANALYZE operation. Sample-block
	// progress and extended-statistics counters. Columns stable
	// across PG 13..18.
	Register(QueryDef{
		ID:           "pg_stat_progress_analyze_v1",
		Category:     "progress",
		MinPGVersion: 14,
		SQL: `SELECT
			pid,
			datid,
			datname,
			relid,
			phase,
			sample_blks_scanned,
			sample_blks_total,
			ext_stats_total,
			ext_stats_computed,
			child_tables_total,
			child_tables_done,
			current_child_table_relid
		FROM pg_stat_progress_analyze
		ORDER BY pid`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionShort,
		Timeout:        5 * time.Second,
		Cadence:        Cadence5m,
	})

	// pg_stat_progress_create_index: per-CREATE INDEX (incl. CONCURRENTLY)
	// and REINDEX operation. Surfaces lockers + per-phase progress
	// (relation scan, sort, build, validate). Columns stable across
	// PG 12..18.
	Register(QueryDef{
		ID:           "pg_stat_progress_create_index_v1",
		Category:     "progress",
		MinPGVersion: 14,
		SQL: `SELECT
			pid,
			datid,
			datname,
			relid,
			index_relid,
			command,
			phase,
			lockers_total,
			lockers_done,
			current_locker_pid,
			blocks_total,
			blocks_done,
			tuples_total,
			tuples_done,
			partitions_total,
			partitions_done
		FROM pg_stat_progress_create_index
		ORDER BY pid`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionShort,
		Timeout:        5 * time.Second,
		Cadence:        Cadence5m,
	})

	// pg_stat_progress_cluster: per-CLUSTER and per-VACUUM FULL
	// operation. Columns stable across PG 12..18.
	Register(QueryDef{
		ID:           "pg_stat_progress_cluster_v1",
		Category:     "progress",
		MinPGVersion: 14,
		SQL: `SELECT
			pid,
			datid,
			datname,
			relid,
			command,
			phase,
			cluster_index_relid,
			heap_tuples_scanned,
			heap_tuples_written,
			heap_blks_total,
			heap_blks_scanned,
			index_rebuild_count
		FROM pg_stat_progress_cluster
		ORDER BY pid`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionShort,
		Timeout:        5 * time.Second,
		Cadence:        Cadence5m,
	})

	// pg_stat_progress_basebackup: per-active basebackup operation.
	// Surfaces backup_total / backup_streamed bytes + tablespace
	// progress. Columns stable across PG 13..18.
	Register(QueryDef{
		ID:           "pg_stat_progress_basebackup_v1",
		Category:     "progress",
		MinPGVersion: 14,
		SQL: `SELECT
			pid,
			phase,
			backup_total,
			backup_streamed,
			tablespaces_total,
			tablespaces_streamed
		FROM pg_stat_progress_basebackup
		ORDER BY pid`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionShort,
		Timeout:        5 * time.Second,
		Cadence:        Cadence5m,
	})

	// pg_stat_progress_copy: per-COPY operation. Bytes + tuples
	// progress for bulk imports/exports. PG 17 added tuples_skipped;
	// the canonical SQL emits a NULL stub and the PG-17+ overrides
	// populate it.
	Register(QueryDef{
		ID:           "pg_stat_progress_copy_v1",
		Category:     "progress",
		MinPGVersion: 14,
		SQL: `SELECT
			pid,
			datid,
			datname,
			relid,
			command,
			type,
			bytes_processed,
			bytes_total,
			tuples_processed,
			tuples_excluded,
			NULL::bigint AS tuples_skipped
		FROM pg_stat_progress_copy
		ORDER BY pid`,
		ResultKind:     ResultRowset,
		RetentionClass: RetentionShort,
		Timeout:        5 * time.Second,
		Cadence:        Cadence5m,
	})

	RegisterOverride(17, "pg_stat_progress_copy_v1", `SELECT
		pid,
		datid,
		datname,
		relid,
		command,
		type,
		bytes_processed,
		bytes_total,
		tuples_processed,
		tuples_excluded,
		tuples_skipped
	FROM pg_stat_progress_copy
	ORDER BY pid`)

	RegisterOverride(18, "pg_stat_progress_copy_v1", `SELECT
		pid,
		datid,
		datname,
		relid,
		command,
		type,
		bytes_processed,
		bytes_total,
		tuples_processed,
		tuples_excluded,
		tuples_skipped
	FROM pg_stat_progress_copy
	ORDER BY pid`)
}
