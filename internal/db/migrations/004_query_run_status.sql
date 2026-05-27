-- Migration: explicit run status (success / failed / skipped).
--
-- Before this migration, collector_status.json could only be reconstructed
-- from query_runs by inspecting the `error` column: empty meant success,
-- non-empty meant failed. There was no way to express "this collector was
-- intentionally skipped" (e.g. a high-sensitivity collector that the
-- operator has not opted into via signals.high_sensitivity_collectors_enabled).
--
-- The new `status` column makes that explicit. `reason` carries a short
-- machine-friendly category that mirrors the values used in
-- collector_status.json (config_disabled, permission_denied, timeout, …).

ALTER TABLE query_runs ADD COLUMN status TEXT NOT NULL DEFAULT 'success';
ALTER TABLE query_runs ADD COLUMN reason TEXT NOT NULL DEFAULT '';

-- Backfill: rows persisted before this migration carried failure info in
-- the `error` column only.
UPDATE query_runs SET status = 'failed', reason = 'execution_error'
 WHERE error != '' AND status = 'success';
