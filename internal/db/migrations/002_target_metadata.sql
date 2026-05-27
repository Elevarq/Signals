-- Migration: secure targets — replace dsn_hash with structured non-secret fields.

-- Add new structured columns to targets.
ALTER TABLE targets ADD COLUMN host TEXT NOT NULL DEFAULT '';
ALTER TABLE targets ADD COLUMN port INTEGER NOT NULL DEFAULT 5432;
ALTER TABLE targets ADD COLUMN dbname TEXT NOT NULL DEFAULT '';
ALTER TABLE targets ADD COLUMN username TEXT NOT NULL DEFAULT '';
ALTER TABLE targets ADD COLUMN sslmode TEXT NOT NULL DEFAULT '';
ALTER TABLE targets ADD COLUMN secret_type TEXT NOT NULL DEFAULT 'NONE';
ALTER TABLE targets ADD COLUMN secret_ref TEXT NOT NULL DEFAULT '';

-- Scrub the dsn_hash column — it could leak information about the connection string.
UPDATE targets SET dsn_hash = 'SCRUBBED';

-- Add target_id to events for scoped events.
ALTER TABLE events ADD COLUMN target_id INTEGER REFERENCES targets(id);

-- Record the migration event.
INSERT INTO events (timestamp, event_type, detail)
VALUES (datetime('now'), 'migration_002', 'added structured target metadata columns and event scoping');
