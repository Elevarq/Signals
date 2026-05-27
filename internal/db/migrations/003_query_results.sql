-- Migration: per-query result storage.

-- Catalog of registered queries.
CREATE TABLE IF NOT EXISTS query_catalog (
    query_id        TEXT PRIMARY KEY,
    category        TEXT NOT NULL DEFAULT '',
    result_kind     TEXT NOT NULL DEFAULT 'rowset',
    retention_class TEXT NOT NULL DEFAULT 'medium',
    registered_at   TEXT NOT NULL
);

-- Individual query execution runs.
CREATE TABLE IF NOT EXISTS query_runs (
    id           TEXT PRIMARY KEY,
    target_id    INTEGER NOT NULL REFERENCES targets(id),
    snapshot_id  TEXT NOT NULL DEFAULT '',
    query_id     TEXT NOT NULL,
    collected_at TEXT NOT NULL,
    pg_version   TEXT NOT NULL DEFAULT '',
    duration_ms  INTEGER NOT NULL DEFAULT 0,
    row_count    INTEGER NOT NULL DEFAULT 0,
    error        TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_query_runs_target_time ON query_runs(target_id, collected_at);
CREATE INDEX IF NOT EXISTS idx_query_runs_query_time ON query_runs(query_id, collected_at);
CREATE INDEX IF NOT EXISTS idx_query_runs_snapshot ON query_runs(snapshot_id);

-- Per-run result payloads (NDJSON, optionally gzip-compressed).
CREATE TABLE IF NOT EXISTS query_results (
    run_id     TEXT PRIMARY KEY REFERENCES query_runs(id),
    payload    BLOB NOT NULL,
    compressed INTEGER NOT NULL DEFAULT 0,
    size_bytes INTEGER NOT NULL DEFAULT 0
);
