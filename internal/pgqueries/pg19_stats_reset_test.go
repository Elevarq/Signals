// Copyright (c) 2026 Scantr LLC. All rights reserved.
// Elevarq is a trade name of Scantr LLC.
// This file is part of Elevarq Signals. Use is governed by the
// commercial license at LICENSE in the repository root.

package pgqueries

import (
	"strings"
	"testing"
)

// resolvedSQL returns the effective SQL that Filter resolves for the
// given logical id under p. It fails the test if the id is not eligible.
func resolvedSQL(t *testing.T, p FilterParams, id string) string {
	t.Helper()
	for _, q := range Filter(p) {
		if q.ID == id {
			return q.SQL
		}
	}
	t.Fatalf("collector %q not eligible under %+v", id, p)
	return ""
}

var statsResetCollectors = []string{
	"pg_stat_user_tables_v1",
	"pg_stat_user_indexes_v1",
}

// TC-SR-01 (CPJ-R001, SR-R001) — on an actual PG19 target the capability
// projection resolves the REAL stats_reset column, even though the
// catalog baseline is clamped to 18 (#816).
func TestStatsReset_PG19CapabilityProjectionResolves(t *testing.T) {
	p := FilterParams{PGMajorVersion: 19, CatalogBaselineMajor: 18}
	for _, id := range statsResetCollectors {
		sql := resolvedSQL(t, p, id)
		if !strings.Contains(sql, "stats_reset") {
			t.Errorf("%s @ actual PG19: resolved SQL does not reference stats_reset:\n%s", id, sql)
		}
		if strings.Contains(sql, "NULL::timestamptz AS stats_reset") {
			t.Errorf("%s @ actual PG19: resolved SQL still emits the typed-NULL stub (capability projection did not win):\n%s", id, sql)
		}
	}
}

// TC-SR-02 (SR-R002, FC-01) — every supported-window major (14-18)
// resolves to the typed-NULL stub and never names the absent catalog
// column.
func TestStatsReset_SupportedWindowEmitsTypedNull(t *testing.T) {
	for _, major := range []int{14, 15, 16, 17, 18} {
		p := FilterParams{PGMajorVersion: major}
		for _, id := range statsResetCollectors {
			sql := resolvedSQL(t, p, id)
			if !strings.Contains(sql, "NULL::timestamptz AS stats_reset") {
				t.Errorf("%s @ PG%d: expected typed-NULL stub, got:\n%s", id, major, sql)
			}
		}
	}
}

// TC-SR-03 (INV-SR-01, SR-R003) — stats_reset is the terminal projected
// column on both PG18 (stub) and PG19 (real), and the pre-existing
// column prefix is preserved.
func TestStatsReset_IsTerminalColumn(t *testing.T) {
	// Pre-existing first column of each collector's projection — a cheap
	// prefix-stability check that the identity columns are untouched.
	firstCol := map[string]string{
		"pg_stat_user_tables_v1":  "schemaname",
		"pg_stat_user_indexes_v1": "schemaname",
	}
	check := func(p FilterParams, label string) {
		for _, id := range statsResetCollectors {
			sql := resolvedSQL(t, p, id)
			selectIdx := strings.Index(sql, "SELECT")
			fromIdx := strings.Index(sql, "FROM")
			if selectIdx < 0 || fromIdx < 0 || fromIdx <= selectIdx {
				t.Fatalf("%s @ %s: cannot locate SELECT ... FROM in:\n%s", id, label, sql)
			}
			projection := sql[selectIdx+len("SELECT") : fromIdx]
			if !strings.Contains(projection, firstCol[id]) {
				t.Errorf("%s @ %s: identity column %q missing from projection:\n%s", id, label, firstCol[id], projection)
			}
			// stats_reset must be the last selected column: nothing but
			// whitespace/newline between it and FROM.
			lastResetIdx := strings.LastIndex(projection, "stats_reset")
			if lastResetIdx < 0 {
				t.Errorf("%s @ %s: stats_reset not in projection:\n%s", id, label, projection)
				continue
			}
			tail := strings.TrimSpace(projection[lastResetIdx+len("stats_reset"):])
			if tail != "" {
				t.Errorf("%s @ %s: stats_reset is not the terminal column (trailing %q):\n%s", id, label, tail, projection)
			}
		}
	}
	check(FilterParams{PGMajorVersion: 18}, "PG18-stub")
	check(FilterParams{PGMajorVersion: 19, CatalogBaselineMajor: 18}, "PG19-real")
}

// TC-SR-04 (CPJ-R002, CPJ-R003) — capability projection keys on the
// ACTUAL major; precedence and registration guards hold.
func TestStatsReset_CapabilityOverrideRegistryContract(t *testing.T) {
	for _, id := range statsResetCollectors {
		if !HasCapabilityOverride(19, id) {
			t.Errorf("expected a PG19 capability override for %q", id)
		}
		if HasCapabilityOverride(18, id) {
			t.Errorf("did not expect a PG18 capability override for %q", id)
		}
	}

	// CPJ-R003: duplicate (major, id) panics.
	assertPanics(t, "duplicate capability override", func() {
		RegisterCapabilityOverride(19, "pg_stat_user_tables_v1",
			`SELECT 1 AS stats_reset`)
	})
	// CPJ-R003: unknown logical id panics.
	assertPanics(t, "unknown logical id", func() {
		RegisterCapabilityOverride(19, "not_a_registered_collector_v1",
			`SELECT 1`)
	})
}

// TC-SR-04 precedence: a capability projection for the actual major wins
// over a baseline override for the baseline major on the same ID. Uses a
// pure in-memory resolver so it does not touch the global registry.
func TestStatsReset_CapabilityBeatsBaseline(t *testing.T) {
	const id = "pg_stat_user_tables_v1"
	cap := map[int]map[string]string{19: {id: "CAP"}}
	base := map[int]map[string]string{18: {id: "BASE"}}
	got := resolveEffectiveSQL(cap, base, id, 19, 18, "DEFAULT")
	if got != "CAP" {
		t.Errorf("capability projection must win: got %q, want CAP", got)
	}
	// With no capability projection, baseline (for the baseline major) wins.
	got = resolveEffectiveSQL(nil, base, id, 19, 18, "DEFAULT")
	if got != "BASE" {
		t.Errorf("baseline override must apply when no capability projection: got %q, want BASE", got)
	}
	// With neither, default.
	got = resolveEffectiveSQL(nil, nil, id, 19, 18, "DEFAULT")
	if got != "DEFAULT" {
		t.Errorf("default must apply when neither override present: got %q, want DEFAULT", got)
	}
}

func assertPanics(t *testing.T, label string, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("%s: expected panic, got none", label)
		}
	}()
	fn()
}
