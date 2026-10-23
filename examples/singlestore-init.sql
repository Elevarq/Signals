-- #577: deterministic SingleStore Signals test-environment seed.
--
-- Applied by scripts/singlestore-dev-fixture.sh against the pinned official
-- SingleStore Dev Image. Everything here is version-controlled and
-- deterministic: no interactive /tmp SQL, no hand-trimming, no dependence on a
-- developer workspace. Re-running produces an equivalent collector capture.
--
-- Read-only monitoring user + EXACT minimum grants per
-- specifications/singlestore-grants.md: baseline SELECT,PROCESS covers 9/10
-- collectors; the elevated CLUSTER grant enables singlestore_cluster_status_v1
-- (MV_CLUSTER_STATUS) for the full 10/10.
--
-- The schema/seed/workload exist ONLY to exercise the Signals collectors
-- (table_statistics, plancache, activities, processlist, distributed_partitions,
-- …). It is deliberately NOT shaped to trigger any Analyzer rule/finding —
-- Analyzer is downstream and out of scope for #577.

CREATE USER IF NOT EXISTS 'signals_ro'@'%' IDENTIFIED BY 'signals_ro_pw';
GRANT SELECT, PROCESS ON *.* TO 'signals_ro'@'%';
GRANT CLUSTER ON *.* TO 'signals_ro'@'%';

DROP DATABASE IF EXISTS signals_fixture;
CREATE DATABASE signals_fixture;
USE signals_fixture;

-- Rowstore table (in-memory) — exercises STORAGE_TYPE=rowstore in TABLE_STATISTICS.
CREATE ROWSTORE TABLE accounts (
  id BIGINT PRIMARY KEY,
  region VARCHAR(16),
  balance DECIMAL(12,2),
  updated_at DATETIME,
  KEY (region)
);

-- Columnstore table (sharded) — exercises a distributed/columnstore table and
-- DISTRIBUTED_PARTITIONS.
CREATE TABLE ledger (
  id BIGINT,
  account_id BIGINT,
  amount DECIMAL(12,2),
  entry_at DATETIME,
  SHARD KEY (account_id),
  SORT KEY (entry_at)
);

-- Deterministic seed (fixed values; NOW() is only used by the volatile
-- updated_at/entry_at columns, which the collectors do not key on).
INSERT INTO accounts (id, region, balance, updated_at) VALUES
 (1,'eu',100.00,NOW()),(2,'us',200.00,NOW()),(3,'eu',300.00,NOW()),(4,'ap',400.00,NOW()),
 (5,'us',500.00,NOW()),(6,'eu',600.00,NOW()),(7,'ap',700.00,NOW()),(8,'us',800.00,NOW()),
 (9,'eu',900.00,NOW()),(10,'ap',1000.00,NOW()),(11,'us',110.00,NOW()),(12,'eu',120.00,NOW()),
 (13,'ap',130.00,NOW()),(14,'us',140.00,NOW()),(15,'eu',150.00,NOW()),(16,'ap',160.00,NOW()),
 (17,'us',170.00,NOW()),(18,'eu',180.00,NOW()),(19,'ap',190.00,NOW()),(20,'us',200.00,NOW()),
 (21,'eu',210.00,NOW()),(22,'ap',220.00,NOW()),(23,'us',230.00,NOW()),(24,'eu',240.00,NOW()),
 (25,'ap',250.00,NOW()),(26,'us',260.00,NOW()),(27,'eu',270.00,NOW()),(28,'ap',280.00,NOW()),
 (29,'us',290.00,NOW()),(30,'eu',300.00,NOW());

INSERT INTO ledger (id, account_id, amount, entry_at) VALUES
 (1,1,10.00,NOW()),(2,2,20.00,NOW()),(3,3,30.00,NOW()),(4,4,40.00,NOW()),(5,5,50.00,NOW()),
 (6,6,60.00,NOW()),(7,7,70.00,NOW()),(8,8,80.00,NOW()),(9,9,90.00,NOW()),(10,10,100.00,NOW()),
 (11,11,110.00,NOW()),(12,12,120.00,NOW()),(13,13,130.00,NOW()),(14,14,140.00,NOW()),(15,15,150.00,NOW()),
 (16,16,160.00,NOW()),(17,17,170.00,NOW()),(18,18,180.00,NOW()),(19,19,190.00,NOW()),(20,20,200.00,NOW()),
 (21,1,210.00,NOW()),(22,2,220.00,NOW()),(23,3,230.00,NOW()),(24,4,240.00,NOW()),(25,5,250.00,NOW()),
 (26,6,260.00,NOW()),(27,7,270.00,NOW()),(28,8,280.00,NOW()),(29,9,290.00,NOW()),(30,10,300.00,NOW()),
 (31,11,310.00,NOW()),(32,12,320.00,NOW()),(33,13,330.00,NOW()),(34,14,340.00,NOW()),(35,15,350.00,NOW()),
 (36,16,360.00,NOW()),(37,17,370.00,NOW()),(38,18,380.00,NOW()),(39,19,390.00,NOW()),(40,20,400.00,NOW());

-- Deterministic workload (repeated so MV_PLANCACHE registers the plans).
SELECT region, COUNT(*) AS n, SUM(balance) AS total FROM accounts GROUP BY region ORDER BY total DESC;
SELECT region, COUNT(*) AS n, SUM(balance) AS total FROM accounts GROUP BY region ORDER BY total DESC;
SELECT region, COUNT(*) AS n, SUM(balance) AS total FROM accounts GROUP BY region ORDER BY total DESC;
SELECT a.region, COUNT(l.id) AS entries FROM accounts a JOIN ledger l ON l.account_id = a.id GROUP BY a.region;
SELECT a.region, COUNT(l.id) AS entries FROM accounts a JOIN ledger l ON l.account_id = a.id GROUP BY a.region;
SELECT a.region, COUNT(l.id) AS entries FROM accounts a JOIN ledger l ON l.account_id = a.id GROUP BY a.region;
SELECT COUNT(*) AS big FROM ledger WHERE amount > 200;
SELECT COUNT(*) AS big FROM ledger WHERE amount > 200;
SELECT COUNT(*) AS big FROM ledger WHERE amount > 200;
