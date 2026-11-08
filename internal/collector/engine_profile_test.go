package collector

import (
	"context"
	"testing"

	"github.com/elevarq/signals/internal/clickhouse"
	"github.com/elevarq/signals/internal/config"
	"github.com/elevarq/signals/internal/db"
	"github.com/elevarq/signals/internal/singlestore"
)

// F02 / #709 / SP-R010: the per-target collection profile must constrain EVERY
// engine, not only PostgreSQL. Before the fix the MySQL-wire (MySQL/MariaDB/
// SingleStore) and ClickHouse sessions ran the whole catalog and consulted only
// the daemon-wide sensitivity flag, so a restricted/custom target still ran and
// persisted high-sensitivity / excluded collectors (INV-SENS-01 breach).
//
// These are production-boundary tests: a fake replaces ONLY the DB/network
// boundary; dispatch, identity, catalog selection, profile filtering, mapping,
// and SQLite persistence all run for real. High sensitivity is enabled, so the
// sensitive collectors WOULD run as success without the profile gate.

// fakeMariaDBConn is defined in mariadb_session_test.go (identifies as MariaDB).

// latestRunsByID indexes persisted query runs by collector ID for the
// single snapshot the test produced.
func latestRunsByID(t *testing.T, store *db.DB) map[string]db.QueryRun {
	t.Helper()
	snaps, err := store.GetAllSnapshots("", "")
	if err != nil {
		t.Fatalf("GetAllSnapshots: %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("want exactly one snapshot, got %d", len(snaps))
	}
	runs, err := store.GetQueryRunsBySnapshotIDs([]string{snaps[0].ID})
	if err != nil {
		t.Fatalf("GetQueryRunsBySnapshotIDs: %v", err)
	}
	byID := make(map[string]db.QueryRun, len(runs))
	for _, r := range runs {
		byID[r.QueryID] = r
	}
	return byID
}

// --- pure unit: the engine-neutral gate ------------------------------------

func TestProfileFilterCatalog(t *testing.T) {
	type c struct {
		id string
		hs bool
	}
	cat := []c{{"a_v1", false}, {"b_hs_v1", true}, {"c_v1", false}, {"d_hs_v1", true}}
	idOf := func(x c) string { return x.id }
	hsOf := func(x c) bool { return x.hs }

	t.Run("default admits all", func(t *testing.T) {
		el, gated := profileFilterCatalog(cat, config.TargetConfig{}, idOf, hsOf)
		if len(el) != 4 || len(gated) != 0 {
			t.Fatalf("default: eligible=%d gated=%d, want 4/0", len(el), len(gated))
		}
	})
	t.Run("restricted drops high-sensitivity", func(t *testing.T) {
		tgt := config.TargetConfig{Collectors: config.TargetCollectorConfig{Profile: config.ProfileRestricted}}
		el, gated := profileFilterCatalog(cat, tgt, idOf, hsOf)
		if len(el) != 2 || len(gated) != 2 {
			t.Fatalf("restricted: eligible=%d gated=%d, want 2/2", len(el), len(gated))
		}
		for _, g := range gated {
			if !g.hs {
				t.Errorf("restricted gated a non-sensitive collector %q", g.id)
			}
		}
	})
	t.Run("custom include is an allowlist", func(t *testing.T) {
		tgt := config.TargetConfig{Collectors: config.TargetCollectorConfig{Profile: config.ProfileCustom, Include: []string{"a_v1"}}}
		el, gated := profileFilterCatalog(cat, tgt, idOf, hsOf)
		if len(el) != 1 || el[0].id != "a_v1" {
			t.Fatalf("include: eligible=%v, want only a_v1", el)
		}
		if len(gated) != 3 {
			t.Fatalf("include: gated=%d, want 3", len(gated))
		}
	})
	t.Run("custom exclude is a denylist", func(t *testing.T) {
		tgt := config.TargetConfig{Collectors: config.TargetCollectorConfig{Profile: config.ProfileCustom, Exclude: []string{"b_hs_v1"}}}
		el, gated := profileFilterCatalog(cat, tgt, idOf, hsOf)
		if len(el) != 3 || len(gated) != 1 || gated[0].id != "b_hs_v1" {
			t.Fatalf("exclude: eligible=%d gated=%v, want 3 and only b_hs_v1 gated", len(el), gated)
		}
	})
	t.Run("invalid include admits none", func(t *testing.T) {
		tgt := config.TargetConfig{Collectors: config.TargetCollectorConfig{Profile: config.ProfileCustom, Include: []string{"nope_v1"}}}
		el, gated := profileFilterCatalog(cat, tgt, idOf, hsOf)
		if len(el) != 0 || len(gated) != 4 {
			t.Fatalf("invalid include: eligible=%d gated=%d, want 0/4 (never full-catalog fallback)", len(el), len(gated))
		}
	})
}

// --- MySQL-wire engines: restricted drops every HighSensitivity collector ---

func TestCollectTarget_WireRestrictedDropsHighSensitivity(t *testing.T) {
	cases := []struct {
		name   string
		engine string
		conn   ssConnector
	}{
		{"mysql", config.EngineMySQL, fakeMySQLConn()},
		{"mariadb", config.EngineMariaDB, fakeMariaDBConn()},
		{"singlestore", config.EngineSingleStore, fakeSSConn("8.5.22")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, store := newTestCollector(t,
				WithSingleStoreConnectorForTest(tc.conn),
				WithHighSensitivityCollectors(true)) // global ON — restricted must STILL drop
			tgt := config.TargetConfig{
				Name: tc.name + "-t", Host: "h", Port: 3306, User: "u", DBName: "d",
				SSLMode: "disable", Engine: tc.engine, Enabled: true,
				Collectors: config.TargetCollectorConfig{Profile: config.ProfileRestricted},
			}
			if err := c.collectTarget(context.Background(), tgt, true, true, "", ""); err != nil {
				t.Fatalf("collectTarget: %v", err)
			}
			byID := latestRunsByID(t, store)
			var sawSuccess bool
			for _, col := range singlestore.CatalogFor(tc.engine) {
				r, ok := byID[col.ID]
				if !ok {
					t.Errorf("collector %q absent from the outcome inventory (SP-R010 truthful-inventory)", col.ID)
					continue
				}
				if col.HighSensitivity {
					if r.Status == "success" {
						t.Errorf("restricted %s still RAN high-sensitivity collector %q (status=success) — profile not applied (F02/#709)", tc.engine, col.ID)
					}
					if r.Status != "skipped" || r.Reason != "config_disabled" {
						t.Errorf("restricted %s: HS collector %q status=%q reason=%q, want skipped/config_disabled", tc.engine, col.ID, r.Status, r.Reason)
					}
				} else if r.Status == "success" {
					sawSuccess = true
				}
			}
			if !sawSuccess {
				t.Errorf("restricted %s dropped EVERYTHING — expected non-sensitive collectors to still run", tc.engine)
			}
		})
	}
}

// --- MySQL custom include is an allowlist; invalid include runs none --------

func TestCollectTarget_MySQLCustomIncludeOnlyRunsListed(t *testing.T) {
	c, store := newTestCollector(t,
		WithSingleStoreConnectorForTest(fakeMySQLConn()),
		WithHighSensitivityCollectors(true))
	tgt := config.TargetConfig{
		Name: "mysql-inc", Host: "h", Port: 3306, User: "u", DBName: "d",
		SSLMode: "disable", Engine: config.EngineMySQL, Enabled: true,
		Collectors: config.TargetCollectorConfig{Profile: config.ProfileCustom, Include: []string{"server_identity_v1"}},
	}
	if err := c.collectTarget(context.Background(), tgt, true, true, "", ""); err != nil {
		t.Fatalf("collectTarget: %v", err)
	}
	byID := latestRunsByID(t, store)
	for _, col := range singlestore.CatalogFor(config.EngineMySQL) {
		r := byID[col.ID]
		if col.ID == "server_identity_v1" {
			if r.Status != "success" {
				t.Errorf("included collector %q status=%q, want success", col.ID, r.Status)
			}
		} else if r.Status == "success" {
			t.Errorf("custom include ran non-listed collector %q (allowlist breached, F02/#709)", col.ID)
		}
	}
}

func TestCollectTarget_MySQLInvalidIncludeRunsNone(t *testing.T) {
	c, store := newTestCollector(t,
		WithSingleStoreConnectorForTest(fakeMySQLConn()),
		WithHighSensitivityCollectors(true))
	tgt := config.TargetConfig{
		Name: "mysql-bad-inc", Host: "h", Port: 3306, User: "u", DBName: "d",
		SSLMode: "disable", Engine: config.EngineMySQL, Enabled: true,
		Collectors: config.TargetCollectorConfig{Profile: config.ProfileCustom, Include: []string{"does_not_exist_v1"}},
	}
	if err := c.collectTarget(context.Background(), tgt, true, true, "", ""); err != nil {
		t.Fatalf("collectTarget: %v", err)
	}
	byID := latestRunsByID(t, store)
	for id, r := range byID {
		if r.Status == "success" {
			t.Errorf("invalid include still ran collector %q — must never fall back to the full catalog (F02/#709)", id)
		}
	}
}

func TestCollectTarget_MySQLCustomExcludeDropsListed(t *testing.T) {
	c, store := newTestCollector(t,
		WithSingleStoreConnectorForTest(fakeMySQLConn()),
		WithHighSensitivityCollectors(true))
	tgt := config.TargetConfig{
		Name: "mysql-exc", Host: "h", Port: 3306, User: "u", DBName: "d",
		SSLMode: "disable", Engine: config.EngineMySQL, Enabled: true,
		Collectors: config.TargetCollectorConfig{Profile: config.ProfileCustom, Exclude: []string{"mysql_processlist_v1"}},
	}
	if err := c.collectTarget(context.Background(), tgt, true, true, "", ""); err != nil {
		t.Fatalf("collectTarget: %v", err)
	}
	byID := latestRunsByID(t, store)
	if r := byID["mysql_processlist_v1"]; r.Status == "success" {
		t.Errorf("excluded collector mysql_processlist_v1 still ran (status=success)")
	} else if r.Status != "skipped" || r.Reason != "config_disabled" {
		t.Errorf("excluded collector status=%q reason=%q, want skipped/config_disabled", r.Status, r.Reason)
	}
	if r := byID["server_identity_v1"]; r.Status != "success" {
		t.Errorf("non-excluded collector server_identity_v1 status=%q, want success", r.Status)
	}
}

// --- ClickHouse: restricted drops the query_log high-sensitivity collectors -

func TestCollectTarget_ClickHouseRestrictedDropsQueryLog(t *testing.T) {
	c, store := newTestCollector(t,
		WithClickHouseConnectorForTest(fakeCHConn()),
		WithHighSensitivityCollectors(true))
	tgt := config.TargetConfig{
		Name: "ch-restricted", Host: "h", Port: 9000, User: "u", DBName: "default",
		SSLMode: "disable", Engine: config.EngineClickHouse, Enabled: true,
		Collectors: config.TargetCollectorConfig{Profile: config.ProfileRestricted},
	}
	if err := c.collectTarget(context.Background(), tgt, true, true, "", ""); err != nil {
		t.Fatalf("collectTarget: %v", err)
	}
	byID := latestRunsByID(t, store)
	var sawSuccess bool
	for _, col := range clickhouse.Catalog() {
		r, ok := byID[col.ID]
		if !ok {
			t.Errorf("clickhouse collector %q absent from the outcome inventory (SP-R010)", col.ID)
			continue
		}
		if col.HighSensitivity {
			if r.Status == "success" {
				t.Errorf("restricted clickhouse still RAN high-sensitivity collector %q — profile not applied (F02/#709)", col.ID)
			}
			if r.Status != "skipped" || r.Reason != "config_disabled" {
				t.Errorf("restricted clickhouse HS collector %q status=%q reason=%q, want skipped/config_disabled", col.ID, r.Status, r.Reason)
			}
		} else if r.Status == "success" {
			sawSuccess = true
		}
	}
	if !sawSuccess {
		t.Error("restricted clickhouse dropped everything — expected non-sensitive collectors to still run")
	}
}
