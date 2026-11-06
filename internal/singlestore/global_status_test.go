// Copyright (c) 2026 Scantr LLC. All rights reserved.
// Elevarq is a trade name of Scantr LLC.
// This file is part of Elevarq Signals. Use is governed by the
// commercial license at LICENSE in the repository root.

package singlestore

import (
	"strings"
	"testing"
)

// globalStatusWorkloadCounters are the workload/query global_status counters
// that both the MySQL and MariaDB collectors must carry (on-disk temp-table
// ratio, server-wide scan/join rate, sort-buffer pressure, scan-heavy workload,
// thread/table-cache miss rate). The IN-list is version-tolerant; the same set
// is asserted for both engines (#678; MYC-R015 / MDC-R017).
var globalStatusWorkloadCounters = []string{
	"Created_tmp_disk_tables",
	"Created_tmp_tables",
	"Select_scan",
	"Select_full_join",
	"Sort_merge_passes",
	"Handler_read_rnd_next",
	"Threads_created",
	"Table_open_cache_hits",
	"Table_open_cache_misses",
}

// globalStatusInnodbCounters are the InnoDB-internals global_status counters
// both engines must carry (dirty/flush pressure, redo/flush stalls, lock
// pressure, and the deadlock counter that enables the deadlock-rate rule). All
// are present on both MySQL and MariaDB; the version-tolerant IN-list carries
// any build-absent counter as simply no row. MYC-R015 / MDC-R017.
var globalStatusInnodbCounters = []string{
	"Innodb_buffer_pool_pages_dirty",
	"Innodb_buffer_pool_pages_total",
	"Innodb_buffer_pool_wait_free",
	"Innodb_log_waits",
	"Innodb_row_lock_waits",
	"Innodb_row_lock_time",
	"Innodb_deadlocks",
}

// globalStatusNewCounters is the full #678 counter set (workload + InnoDB).
func globalStatusNewCounters() []string {
	return append(append([]string{}, globalStatusWorkloadCounters...), globalStatusInnodbCounters...)
}

// TC-MYC-17 (#678): the MySQL global_status collector carries every workload +
// InnoDB counter from #678, alongside the existing #656 buffer-pool reads, and
// stays a read-only, non-HighSensitivity curated allowlist.
func TestMySQLGlobalStatusWorkloadInnodbCounters(t *testing.T) {
	gs := collectorByID(mysqlCatalog(), "mysql_global_status_v1")
	if gs == nil {
		t.Fatal("mysql_global_status_v1 missing")
	}
	for _, c := range globalStatusNewCounters() {
		if !strings.Contains(gs.SQL, "'"+c+"'") {
			t.Errorf("mysql_global_status_v1 IN-list must contain %q; SQL=%q", c, gs.SQL)
		}
	}
	// The #656 buffer-pool reads must remain present.
	for _, c := range []string{"Innodb_buffer_pool_reads", "Innodb_buffer_pool_read_requests"} {
		if !strings.Contains(gs.SQL, "'"+c+"'") {
			t.Errorf("mysql_global_status_v1 IN-list dropped existing counter %q", c)
		}
	}
	if gs.HighSensitivity {
		t.Error("mysql_global_status_v1 must not be HighSensitivity")
	}
	if !strings.HasPrefix(strings.TrimSpace(strings.ToUpper(gs.SQL)), "SELECT") {
		t.Errorf("mysql_global_status_v1 must be a read-only SELECT; SQL=%q", gs.SQL)
	}
}

// TC-MDC-22 (#678): the MariaDB global_status collector carries every workload +
// InnoDB counter from #678, alongside the existing #656 buffer-pool reads.
func TestMariaDBGlobalStatusWorkloadInnodbCounters(t *testing.T) {
	gs := collectorByID(mariadbCatalog(), "mariadb_global_status_v1")
	if gs == nil {
		t.Fatal("mariadb_global_status_v1 missing")
	}
	for _, c := range globalStatusNewCounters() {
		if !strings.Contains(gs.SQL, "'"+c+"'") {
			t.Errorf("mariadb_global_status_v1 IN-list must contain %q; SQL=%q", c, gs.SQL)
		}
	}
	for _, c := range []string{"Innodb_buffer_pool_reads", "Innodb_buffer_pool_read_requests"} {
		if !strings.Contains(gs.SQL, "'"+c+"'") {
			t.Errorf("mariadb_global_status_v1 IN-list dropped existing counter %q", c)
		}
	}
	if gs.HighSensitivity {
		t.Error("mariadb_global_status_v1 must not be HighSensitivity")
	}
	if !strings.HasPrefix(strings.TrimSpace(strings.ToUpper(gs.SQL)), "SELECT") {
		t.Errorf("mariadb_global_status_v1 must be a read-only SELECT; SQL=%q", gs.SQL)
	}
}

// TC-MYC-18 / TC-MDC-23 (#678): the global_status IN-list stays a curated
// allowlist — no credential/secret name is ever present on either collector.
// (Version-absent counters are handled by the version-tolerant IN-list at
// runtime: an absent name simply returns no row, exercised by the live-qual
// legs; no error is raised.)
func TestGlobalStatusNoCredentialNames(t *testing.T) {
	forbidden := []string{"password", "secret", "token", "credential", "auth_"}
	cases := []struct {
		name string
		id   string
		cat  []Collector
	}{
		{"mysql", "mysql_global_status_v1", mysqlCatalog()},
		{"mariadb", "mariadb_global_status_v1", mariadbCatalog()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gs := collectorByID(c.cat, c.id)
			if gs == nil {
				t.Fatalf("%s missing", c.id)
			}
			lower := strings.ToLower(gs.SQL)
			for _, f := range forbidden {
				if strings.Contains(lower, f) {
					t.Errorf("%s global_status IN-list contains forbidden token %q", c.name, f)
				}
			}
		})
	}
}
