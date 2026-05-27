-- Meta: key-value store for instance-level state.
CREATE TABLE IF NOT EXISTS meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- Monitored PostgreSQL targets.
CREATE TABLE IF NOT EXISTS targets (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL UNIQUE,
    dsn_hash   TEXT    NOT NULL DEFAULT '',
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at TEXT    NOT NULL,
    updated_at TEXT    NOT NULL
);

-- Telemetry snapshots.
CREATE TABLE IF NOT EXISTS snapshots (
    id           TEXT PRIMARY KEY,
    target_id    INTEGER NOT NULL REFERENCES targets(id),
    collected_at TEXT    NOT NULL,
    pg_version   TEXT    NOT NULL DEFAULT '',
    payload      TEXT    NOT NULL,
    size_bytes   INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_snapshots_target_time ON snapshots(target_id, collected_at);

-- Event log for audit trail.
CREATE TABLE IF NOT EXISTS events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp  TEXT    NOT NULL,
    event_type TEXT    NOT NULL,
    detail     TEXT    NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_events_time ON events(timestamp);
