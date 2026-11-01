// Copyright (c) 2026 Scantr LLC. All rights reserved.
// Elevarq is a trade name of Scantr LLC.
// This file is part of Elevarq Signals. Use is governed by the
// commercial license at LICENSE in the repository root.

package singlestore

import (
	"strings"
	"testing"
)

// sizingVars are the config-sizing variables the Analyzer config-advisor family
// (Elevarq/Analyzer#4004) reads; the global_variables collectors MUST keep them
// in their IN-list so a future edit cannot silently drop them (#660). Absent
// variables on a given engine/version simply return no row (the IN-list is
// version-tolerant), so the same set is asserted for both engines.
var sizingVars = []string{
	"tmp_table_size",
	"max_heap_table_size",
	"innodb_log_file_size",
	"sort_buffer_size",
	"join_buffer_size",
	"table_open_cache",
	"thread_cache_size",
	"innodb_io_capacity",
	"binlog_format",
	"sync_binlog",
}

func globalVariablesSQL(t *testing.T, catalog []Collector, id string) string {
	t.Helper()
	for _, c := range catalog {
		if c.ID == id {
			return c.SQL
		}
	}
	t.Fatalf("collector %q not found", id)
	return ""
}

// TC-SV-01 (#660): the MySQL/MariaDB global_variables collectors carry the
// config-sizing variables the Analyzer advisor family depends on.
func TestGlobalVariablesIncludeSizingVars(t *testing.T) {
	cases := []struct {
		name string
		sql  string
	}{
		{"mysql", globalVariablesSQL(t, mysqlCatalog(), "mysql_global_variables_v1")},
		{"mariadb", globalVariablesSQL(t, mariadbCatalog(), "mariadb_global_variables_v1")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, v := range sizingVars {
				if !strings.Contains(c.sql, "'"+v+"'") {
					t.Errorf("%s global_variables IN-list is missing %q", c.name, v)
				}
			}
		})
	}
}
