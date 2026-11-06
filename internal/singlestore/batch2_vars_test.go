// Copyright (c) 2026 Scantr LLC. All rights reserved.
// Elevarq is a trade name of Scantr LLC.
// This file is part of Elevarq Signals. Use is governed by the
// commercial license at LICENSE in the repository root.

package singlestore

import (
	"strings"
	"testing"
)

// batch2SharedVars are the "batch 2" global_variables that both the MySQL and
// MariaDB collectors must carry (observability + InnoDB internals + server
// charset/collation). The IN-list is version-tolerant, so a variable absent on a
// given engine/version simply returns no row — the same set is asserted for both
// engines (#679 / Analyzer #4004; MYC-R014 / MDC-R016).
var batch2SharedVars = []string{
	// observability
	"slow_query_log",
	"long_query_time",
	"log_output",
	"performance_schema",
	// InnoDB internals
	"innodb_adaptive_hash_index",
	"innodb_flush_neighbors",
	"innodb_change_buffering",
	"innodb_read_io_threads",
	"innodb_write_io_threads",
	// server charset/collation (global complement of MYC-R013 / MDC-R015)
	"character_set_server",
	"collation_server",
}

// batch2MySQLVars are the MySQL-specific batch-2 replication/HA config variables
// (the MySQL GTID + semi-sync naming, both pre- and post-8.0.26 forms, plus
// super_read_only and binlog retention). MYC-R014.
var batch2MySQLVars = []string{
	"super_read_only",
	"gtid_mode",
	"enforce_gtid_consistency",
	"binlog_expire_logs_seconds",
	"expire_logs_days",
	"rpl_semi_sync_source_enabled",
	"rpl_semi_sync_replica_enabled",
	"rpl_semi_sync_master_enabled",
	"rpl_semi_sync_slave_enabled",
	"log_replica_updates",
	"log_slave_updates",
}

// batch2MariaDBVars are the MariaDB-specific batch-2 replication/HA config
// variables (the MariaDB GTID analogs + MariaDB semi-sync naming + binlog
// retention). MDC-R016.
var batch2MariaDBVars = []string{
	"gtid_strict_mode",
	"gtid_domain_id",
	"expire_logs_days",
	"rpl_semi_sync_master_enabled",
	"rpl_semi_sync_slave_enabled",
	"log_slave_updates",
}

// mysqlOnlyVars must NOT appear on the MariaDB collector (its GTID/semi-sync
// model uses the MariaDB names). MDC-R016.
var mysqlOnlyVars = []string{
	"super_read_only",
	"gtid_mode",
	"enforce_gtid_consistency",
	"binlog_expire_logs_seconds",
	"rpl_semi_sync_source_enabled",
	"rpl_semi_sync_replica_enabled",
	"log_replica_updates",
}

// TC-MYC-15 (#679): the MySQL global_variables collector carries every batch-2
// variable — shared + MySQL-specific.
func TestMySQLGlobalVariablesBatch2(t *testing.T) {
	sql := globalVariablesSQL(t, mysqlCatalog(), "mysql_global_variables_v1")
	for _, v := range append(append([]string{}, batch2SharedVars...), batch2MySQLVars...) {
		if !strings.Contains(sql, "'"+v+"'") {
			t.Errorf("mysql global_variables IN-list is missing %q", v)
		}
	}
}

// TC-MDC-20 (#679): the MariaDB global_variables collector carries every batch-2
// variable — shared + MariaDB-specific — and leaks no MySQL-only name.
func TestMariaDBGlobalVariablesBatch2(t *testing.T) {
	sql := globalVariablesSQL(t, mariadbCatalog(), "mariadb_global_variables_v1")
	for _, v := range append(append([]string{}, batch2SharedVars...), batch2MariaDBVars...) {
		if !strings.Contains(sql, "'"+v+"'") {
			t.Errorf("mariadb global_variables IN-list is missing %q", v)
		}
	}
	for _, v := range mysqlOnlyVars {
		if strings.Contains(sql, "'"+v+"'") {
			t.Errorf("mariadb global_variables IN-list must not carry MySQL-only name %q", v)
		}
	}
}

// TC-MYC-16 / TC-MDC-21 (#679): the global_variables IN-list stays a curated
// allowlist — no credential/secret variable name is ever present on either
// collector. (Version-absent variables are handled by the version-tolerant
// IN-list at runtime: an absent name simply returns no row; the live-qual legs
// exercise that, no error is raised.)
func TestGlobalVariablesNoCredentialNames(t *testing.T) {
	forbidden := []string{"password", "secret", "key", "token", "credential", "auth_"}
	cases := []struct {
		name string
		sql  string
	}{
		{"mysql", globalVariablesSQL(t, mysqlCatalog(), "mysql_global_variables_v1")},
		{"mariadb", globalVariablesSQL(t, mariadbCatalog(), "mariadb_global_variables_v1")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lower := strings.ToLower(c.sql)
			for _, f := range forbidden {
				if strings.Contains(lower, f) {
					t.Errorf("%s global_variables IN-list contains forbidden credential token %q", c.name, f)
				}
			}
		})
	}
}
