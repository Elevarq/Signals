-- 009_snapshot_collection_policy.sql — immutable per-snapshot collection-time
-- sensitivity/unsafe policy provenance (SIGNALS-CTP, #729 / A3).
--
-- metadata.json stamped the CURRENT exporter's unsafe_mode and
-- high_sensitivity_collectors_enabled onto exports that can contain historical
-- snapshots collected under a DIFFERENT policy. A later "false" flag could then
-- accompany retained sensitive text, falsely implying old content was collected
-- without high-sensitivity / was scrubbed. Capture the collection-time policy on
-- EACH snapshot at insert so an export can describe its included evidence
-- honestly (uniform / mixed / unknown) instead of claiming the current process
-- policy applies to all of history.
--
-- Tri-state by design: these columns are NULLABLE with NO default and NO
-- backfill. A pre-#729 snapshot has an UNKNOWN collection-time policy, and the
-- honest representation of that is NULL — never an invented true/false. New
-- snapshots set 0/1 at insert; the value is immutable thereafter.
ALTER TABLE snapshots ADD COLUMN collect_unsafe_mode    INTEGER;
ALTER TABLE snapshots ADD COLUMN collect_high_sensitivity INTEGER;
