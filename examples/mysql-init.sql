-- Deterministic MySQL Signals test-environment seed (MySQL collection PR2).
--
-- Applied by scripts/mysql-dev-fixture.sh against the pinned official MySQL
-- Dev Image. Everything here is version-controlled and deterministic: no
-- interactive /tmp SQL, no hand-trimming, no developer-workspace dependence.
--
-- Read-only monitoring user + the EXACT minimum grants per
-- specifications/mysql-grants.md: SELECT (read every information_schema /
-- performance_schema view the catalog uses) + PROCESS (so
-- mysql_processlist_v1 surfaces all sessions, not just the monitor's own).
-- No elevated/optional grant is needed for the MySQL catalog.
--
-- The schema/seed/workload exist ONLY to exercise the Signals collectors
-- (table_statistics, processlist, innodb_metrics, …). It is deliberately NOT
-- shaped to trigger any Analyzer rule/finding — Analyzer is downstream.

CREATE USER IF NOT EXISTS 'signals_ro'@'%' IDENTIFIED BY 'signals_ro_pw';
GRANT SELECT, PROCESS ON *.* TO 'signals_ro'@'%';
FLUSH PRIVILEGES;

CREATE DATABASE IF NOT EXISTS signals_fixture;
USE signals_fixture;

-- InnoDB tables so TABLE_STATISTICS + INNODB_METRICS have real rows.
CREATE TABLE IF NOT EXISTS accounts (
  id BIGINT PRIMARY KEY,
  region VARCHAR(16),
  balance DECIMAL(12,2),
  updated_at DATETIME,
  KEY region_idx (region)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS ledger (
  id BIGINT PRIMARY KEY,
  account_id BIGINT,
  amount DECIMAL(12,2),
  entry_at DATETIME,
  KEY account_idx (account_id)
) ENGINE=InnoDB;

-- #538 Replay Tier-1 evidence fixture: a representative table that exercises the
-- full columns + index-inventory contract so the semantic qualification can
-- assert the evidence survives collection -> persistence -> export:
--   * a nullable column (note) and NOT NULL columns (id, tenant_id, created_at,
--     email, status);
--   * a primary key (id);
--   * an ordinary single-column index (idx_status);
--   * a UNIQUE index (uq_email);
--   * a composite index with DELIBERATELY observable column ordering
--     (idx_tenant_created = tenant_id, created_at) — the exported evidence MUST
--     preserve that exact (tenant_id, created_at) order.
CREATE TABLE IF NOT EXISTS replay_fixture (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  created_at DATETIME NOT NULL,
  email VARCHAR(255) NOT NULL,
  note VARCHAR(255) NULL,
  status INT NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_email (email),
  KEY idx_status (status),
  KEY idx_tenant_created (tenant_id, created_at)
) ENGINE=InnoDB;

INSERT IGNORE INTO replay_fixture (id, tenant_id, created_at, email, note, status) VALUES
 (1,100,NOW(),'a@example.com','n1',1),(2,100,NOW(),'b@example.com',NULL,0),
 (3,200,NOW(),'c@example.com','n3',1),(4,200,NOW(),'d@example.com',NULL,0),
 (5,300,NOW(),'e@example.com','n5',1);

INSERT IGNORE INTO accounts (id, region, balance, updated_at) VALUES
 (1,'eu',100.00,NOW()),(2,'us',200.00,NOW()),(3,'eu',300.00,NOW()),(4,'ap',400.00,NOW()),
 (5,'us',500.00,NOW()),(6,'eu',600.00,NOW()),(7,'ap',700.00,NOW()),(8,'us',800.00,NOW()),
 (9,'eu',900.00,NOW()),(10,'ap',1000.00,NOW()),(11,'us',110.00,NOW()),(12,'eu',120.00,NOW()),
 (13,'ap',130.00,NOW()),(14,'us',140.00,NOW()),(15,'eu',150.00,NOW()),(16,'ap',160.00,NOW()),
 (17,'us',170.00,NOW()),(18,'eu',180.00,NOW()),(19,'ap',190.00,NOW()),(20,'us',200.00,NOW());

INSERT IGNORE INTO ledger (id, account_id, amount, entry_at) VALUES
 (1,1,10.00,NOW()),(2,2,20.00,NOW()),(3,3,30.00,NOW()),(4,4,40.00,NOW()),(5,5,50.00,NOW()),
 (6,6,60.00,NOW()),(7,7,70.00,NOW()),(8,8,80.00,NOW()),(9,9,90.00,NOW()),(10,10,100.00,NOW()),
 (11,11,110.00,NOW()),(12,12,120.00,NOW()),(13,13,130.00,NOW()),(14,14,140.00,NOW()),(15,15,150.00,NOW());

-- Deterministic workload so the collectors read a warmed server.
SELECT region, COUNT(*) AS n, SUM(balance) AS total FROM accounts GROUP BY region ORDER BY total DESC;
SELECT a.region, COUNT(l.id) AS entries FROM accounts a JOIN ledger l ON l.account_id = a.id GROUP BY a.region;
SELECT COUNT(*) AS big FROM ledger WHERE amount > 50;
