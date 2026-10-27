-- Deterministic ClickHouse Signals test-environment seed (ClickHouse CH-3).
--
-- Applied by scripts/clickhouse-dev-fixture.sh against the pinned official
-- ClickHouse image, as the default (admin) user. Everything is
-- version-controlled and deterministic.
--
-- Read-only monitoring user. Per specifications/clickhouse-grants.md (#565) the
-- least-privilege BASELINE is SELECT on the 11 metadata system.* tables (no
-- cross-user query visibility); the query-workload collectors
-- (query_log/query_thread_log) need an OPTIONAL, separately-approved grant and
-- otherwise degrade to skipped. This fixture grants SELECT ON system.* — the
-- baseline PLUS the optional grant — so the live qualification exercises the full
-- 14-collector catalog (the "optional approved" scenario). Writes are
-- ACCESS_DENIED (SELECT-only). No write/admin/SYSTEM grant is ever required.
--
-- A tiny workload + SYSTEM FLUSH LOGS so system.query_log exists and carries
-- rows for clickhouse_query_log_v1 (the log table is lazily created on first
-- flush). The workload exercises the Signals collectors only; it is not shaped
-- to trigger any Analyzer rule.

CREATE USER IF NOT EXISTS signals_ro IDENTIFIED BY 'signals_ro_pw';
GRANT SELECT ON system.* TO signals_ro;

-- #562 (CHC-R006): seed a deliberately multi-partition MergeTree so the live
-- qualification exercises clickhouse_parts_v1 against a non-trivial parts
-- inventory (many partitions, multiple active parts per partition) and proves
-- the aggregated/bounded shape reconciles. PARTITION BY the key + two separate
-- INSERTs force distinct partitions and 2 active parts per partition (no merge
-- in between).
CREATE DATABASE IF NOT EXISTS signals_demo;
CREATE TABLE IF NOT EXISTS signals_demo.parts_fixture
(
    p   UInt16,
    k   UInt32,
    v   String
)
ENGINE = MergeTree
PARTITION BY p
ORDER BY k;

INSERT INTO signals_demo.parts_fixture SELECT number % 24 AS p, number AS k, 'a' FROM numbers(240);
INSERT INTO signals_demo.parts_fixture SELECT number % 24 AS p, number + 1000 AS k, 'b' FROM numbers(240);

-- Deterministic workload (read-only) so the query log has rows after a flush.
SELECT count() FROM system.tables;
SELECT name FROM system.databases ORDER BY name;
SELECT 1 + 1;

SYSTEM FLUSH LOGS;
