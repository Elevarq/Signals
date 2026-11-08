-- 008_snapshot_source_identity.sql — immutable per-snapshot source identity
-- (SIGNALS-R094.1, #716 / F10).
--
-- Target rows are upserted BY NAME and overwrite their connection identity, so
-- a retargeted/reused target name would make historical snapshots identify as
-- the new database when exported (the exporter looked up the CURRENT targets
-- row by target_id). Capture the source identity (host/port/dbname/username) on
-- EACH snapshot at collection time so historical evidence keeps its
-- collection-time identity regardless of later retargeting. Additive and
-- behaviour-preserving: columns default to the empty/zero value.
ALTER TABLE snapshots ADD COLUMN source_host TEXT    NOT NULL DEFAULT '';
ALTER TABLE snapshots ADD COLUMN source_port INTEGER NOT NULL DEFAULT 0;
ALTER TABLE snapshots ADD COLUMN source_db   TEXT    NOT NULL DEFAULT '';
ALTER TABLE snapshots ADD COLUMN source_user TEXT    NOT NULL DEFAULT '';

-- Best-effort one-time backfill of pre-#716 rows from the then-current targets
-- row — the only identity available for historical snapshots. Immutable
-- thereafter (new snapshots set these columns at insert). Rows whose target_id
-- does not resolve (orphans, R090) stay at the empty defaults.
UPDATE snapshots
SET source_host = COALESCE((SELECT host     FROM targets WHERE targets.id = snapshots.target_id), ''),
    source_port = COALESCE((SELECT port     FROM targets WHERE targets.id = snapshots.target_id), 0),
    source_db   = COALESCE((SELECT dbname   FROM targets WHERE targets.id = snapshots.target_id), ''),
    source_user = COALESCE((SELECT username FROM targets WHERE targets.id = snapshots.target_id), '')
WHERE source_host = '' AND source_db = '' AND source_user = '';
