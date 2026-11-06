// Copyright (c) 2026 Scantr LLC. All rights reserved.
// Elevarq is a trade name of Scantr LLC.
// This file is part of Elevarq Signals. Use is governed by the
// commercial license at LICENSE in the repository root.

package singlestore

import (
	"strings"
	"testing"
)

// TC-MYC-22 (#686): the MySQL global_variables collector carries sql_mode —
// the comma-separated server SQL-mode flag string for the Analyzer sql_mode
// advisory rule (Analyzer #4010). The IN-list stays a curated, version-tolerant
// allowlist: sql_mode absent on a given build simply returns no row, never an
// error. MYC-R018.
func TestMySQLGlobalVariablesSQLMode(t *testing.T) {
	sql := globalVariablesSQL(t, mysqlCatalog(), "mysql_global_variables_v1")
	if !strings.Contains(sql, "'sql_mode'") {
		t.Errorf("mysql global_variables IN-list is missing %q", "sql_mode")
	}
}

// TC-MDC-27 (#686): the MariaDB global_variables collector carries sql_mode too
// — sql_mode is a shared MySQL/MariaDB server variable, so the same name is
// asserted on both engines. MDC-R020.
func TestMariaDBGlobalVariablesSQLMode(t *testing.T) {
	sql := globalVariablesSQL(t, mariadbCatalog(), "mariadb_global_variables_v1")
	if !strings.Contains(sql, "'sql_mode'") {
		t.Errorf("mariadb global_variables IN-list is missing %q", "sql_mode")
	}
}
