-- Deterministic seed for the Signals ClickHouse REPLICATED live-qualification
-- cluster (#566). Applied by scripts/clickhouse-cluster-fixture.sh against node
-- ch1 (the Signals collection target) as the default (admin) user. Everything
-- is version-controlled and deterministic.
--
-- Read-only monitoring user — identical policy to the single-node fixture
-- (examples/clickhouse-init.sql, specifications/clickhouse-grants.md #565):
-- SELECT ON system.* (baseline + the optional query-log grant) so the full
-- collector catalog runs. Writes stay ACCESS_DENIED. ch1 is the collection
-- target; system.clusters / system.replicas on ch1 already carry the whole
-- cluster's topology + replication state, so the user only needs to exist here.
CREATE USER IF NOT EXISTS signals_ro IDENTIFIED BY 'signals_ro_pw';
GRANT SELECT ON system.* TO signals_ro;

-- A ReplicatedMergeTree created ON CLUSTER so the cluster-scope collectors see
-- real replication evidence:
--   * system.clusters  -> signals_cluster with 2 replicas (clickhouse_clusters_v1)
--   * system.replicas  -> one row per replicated table with is_leader /
--                         is_readonly / absolute_delay / queue_size
--                         (clickhouse_replicas_v1)
-- PARTITION BY + two INSERTs give a non-trivial parts inventory on a replicated
-- table (clickhouse_parts_v1 under replication), and the data replicates to ch2.
CREATE DATABASE IF NOT EXISTS signals_demo ON CLUSTER signals_cluster;

-- Cluster-scope grant model (#566): system.replicas is access-filtered by
-- table visibility — the baseline `SELECT ON system.*` alone yields ZERO rows
-- for a least-privilege user, even when ReplicatedMergeTree tables exist. To
-- surface per-table replica state (clickhouse_replicas_v1) the monitoring role
-- needs SHOW-level visibility on the monitored databases. `SHOW TABLES` is a
-- METADATA grant (not data read) — the role stays read-only and gets no row
-- access to user data; it only becomes able to see the table's replica row.
GRANT SHOW TABLES ON signals_demo.* TO signals_ro;

CREATE TABLE IF NOT EXISTS signals_demo.repl ON CLUSTER signals_cluster
(
    p   UInt16,
    k   UInt32,
    v   String
)
ENGINE = ReplicatedMergeTree('/clickhouse/tables/{shard}/repl', '{replica}')
PARTITION BY p
ORDER BY k;

INSERT INTO signals_demo.repl VALUES (1, 1, 'a'), (1, 2, 'b');
INSERT INTO signals_demo.repl VALUES (2, 3, 'c'), (2, 4, 'd');

-- Flush the query log so system.query_log exists + carries rows (lazily created
-- on first flush), mirroring the single-node fixture. Exercises the Signals
-- collectors only; not shaped to trigger any Analyzer rule.
SYSTEM FLUSH LOGS;
