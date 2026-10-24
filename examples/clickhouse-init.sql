-- Deterministic ClickHouse Signals test-environment seed (ClickHouse CH-3).
--
-- Applied by scripts/clickhouse-dev-fixture.sh against the pinned official
-- ClickHouse image, as the default (admin) user. Everything is
-- version-controlled and deterministic.
--
-- Read-only monitoring user + the minimum grant per
-- specifications/clickhouse-grants (CH-2 #565): GRANT SELECT ON system.* — reads
-- the whole diagnostic catalog; writes are ACCESS_DENIED. No elevated grant.
--
-- A tiny workload + SYSTEM FLUSH LOGS so system.query_log exists and carries
-- rows for clickhouse_query_log_v1 (the log table is lazily created on first
-- flush). The workload exercises the Signals collectors only; it is not shaped
-- to trigger any Analyzer rule.

CREATE USER IF NOT EXISTS signals_ro IDENTIFIED BY 'signals_ro_pw';
GRANT SELECT ON system.* TO signals_ro;

-- Deterministic workload (read-only) so the query log has rows after a flush.
SELECT count() FROM system.tables;
SELECT name FROM system.databases ORDER BY name;
SELECT 1 + 1;

SYSTEM FLUSH LOGS;
