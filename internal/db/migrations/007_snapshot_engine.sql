-- Migration: engine identity on snapshots (Elevarq/Signals#532, ECD-R005/R007).
--
-- The storage model (snapshots -> query_runs -> query_results -> query_catalog)
-- is engine-neutral; only the `pg_version` column name was PostgreSQL-shaped,
-- and it is carried forward as the engine-neutral version string. This adds the
-- one discriminator the model lacked: which engine produced the collection, so
-- the common exporter can declare `engine`/`engine_version` in metadata.json and
-- a SingleStore collection is persisted + exported through the same pipeline as
-- PostgreSQL.
--
-- Default 'postgresql' so every pre-existing snapshot (all PostgreSQL) keeps its
-- exact meaning with no backfill — additive and v1-compatible (INV-ECD-02).

ALTER TABLE snapshots ADD COLUMN engine TEXT NOT NULL DEFAULT 'postgresql';

CREATE INDEX IF NOT EXISTS idx_snapshots_engine ON snapshots(engine);
