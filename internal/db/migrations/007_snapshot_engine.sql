-- 007_snapshot_engine.sql — engine identity foundation (engine-identity.md, EID-R003).
--
-- Add the engine identity column to snapshots. Existing (pre-engine) rows are
-- backfilled to 'postgresql' by the DEFAULT, and the NOT NULL + DEFAULT keep
-- the column non-empty going forward, so an empty engine is never persisted
-- (INV-EID-01). This is additive and behaviour-preserving for PostgreSQL.
ALTER TABLE snapshots ADD COLUMN engine TEXT NOT NULL DEFAULT 'postgresql';
