package singlestore

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// Tests derive from specifications/singlestore-collectors.acceptance.md.

// stubRunner returns canned rows per collector ID-matched SQL substring, or an
// error when the SQL contains a configured failing marker.
func stubRunner(failSubstr string) QueryRunner {
	return func(_ context.Context, sql string) ([]map[string]any, error) {
		if failSubstr != "" && strings.Contains(sql, failSubstr) {
			return nil, errors.New("boom")
		}
		switch {
		case strings.Contains(sql, "@@memsql_version"):
			return []map[string]any{{"memsql_version": "8.5.22", "version_comment": "SingleStore"}}, nil
		case strings.Contains(sql, "AGGREGATORS"):
			return []map[string]any{{"HOST": "10.0.0.1", "ROLE": "Leader"}}, nil
		case strings.Contains(sql, "MV_PLANCACHE"):
			return []map[string]any{{"PLAN_ID": "p1", "QUERY_TEXT": "SELECT secret FROM t"}}, nil
		default:
			return []map[string]any{}, nil
		}
	}
}

func resultByID(rs []Result, id string) *Result {
	for i := range rs {
		if rs[i].CollectorID == id {
			return &rs[i]
		}
	}
	return nil
}

func TestCollectRunsCatalog(t *testing.T) {
	// TC-SSC-01
	rs := Collect(context.Background(), stubRunner(""), Catalog(), false, false)
	if len(rs) != len(Catalog()) {
		t.Fatalf("want a result per collector (%d), got %d", len(Catalog()), len(rs))
	}
	id := resultByID(rs, "singlestore_identity_v1")
	if id == nil || id.Err != "" || len(id.Rows) != 1 || id.Rows[0]["memsql_version"] != "8.5.22" {
		t.Fatalf("identity result wrong: %+v", id)
	}
	agg := resultByID(rs, "singlestore_aggregators_v1")
	if agg == nil || agg.Err != "" || len(agg.Rows) != 1 {
		t.Fatalf("aggregators result wrong: %+v", agg)
	}
}

func TestCollectPartialFailureDoesNotAbort(t *testing.T) {
	// TC-SSC-02: fail the aggregators collector; others must still run.
	rs := Collect(context.Background(), stubRunner("AGGREGATORS"), Catalog(), false, false)
	agg := resultByID(rs, "singlestore_aggregators_v1")
	if agg == nil || agg.Err == "" {
		t.Fatalf("aggregators should have captured an error: %+v", agg)
	}
	if id := resultByID(rs, "singlestore_identity_v1"); id == nil || id.Err != "" {
		t.Fatalf("identity must still succeed despite aggregators failing: %+v", id)
	}
}

func TestCollectRedactsSensitive(t *testing.T) {
	// TC-SSC-03
	rs := Collect(context.Background(), stubRunner(""), Catalog(), true, false)
	pc := resultByID(rs, "singlestore_plancache_v1")
	if pc == nil || len(pc.Rows) != 1 {
		t.Fatalf("plancache result missing: %+v", pc)
	}
	if pc.Rows[0]["QUERY_TEXT"] != nil {
		t.Fatalf("SSC-R003: QUERY_TEXT must be redacted, got %v", pc.Rows[0]["QUERY_TEXT"])
	}
	if pc.Rows[0]["PLAN_ID"] != "p1" {
		t.Fatalf("non-sensitive column must survive, got %v", pc.Rows[0]["PLAN_ID"])
	}
}

func TestCatalogShape(t *testing.T) {
	// TC-SSC-04
	for _, c := range Catalog() {
		if HasWrite(c.SQL) {
			t.Fatalf("SSC-R001: collector %s is not a SELECT: %q", c.ID, c.SQL)
		}
	}
	pc := func() *Collector {
		for i, c := range Catalog() {
			if c.ID == "singlestore_plancache_v1" {
				return &Catalog()[i]
			}
		}
		return nil
	}()
	if pc == nil || !pc.HighSensitivity {
		t.Fatal("INV-SSC-02: plancache must be HighSensitivity")
	}
	found := false
	for _, col := range pc.SensitiveColumns {
		if col == "QUERY_TEXT" {
			found = true
		}
	}
	if !found {
		t.Fatal("INV-SSC-02: plancache SensitiveColumns must include QUERY_TEXT")
	}
}

func TestSensitiveColumnsAreHighSensitivity(t *testing.T) {
	// TC-SSC-06 (#554 query-text + #618 secret-config handling): any collector
	// that selects a query-text column (QUERY_TEXT in MV_PLANCACHE, INFO in
	// MV_PROCESSLIST) or a secret-config column (CONFIG_JSON in PIPELINES, which
	// can embed connection credentials) MUST be flagged HighSensitivity with that
	// column in SensitiveColumns, so it is redacted on opt-out. This guards
	// against a new collector leaking SQL text or credentials.
	sensitiveCols := []string{"QUERY_TEXT", "INFO", "CONFIG_JSON"}
	for _, c := range Catalog() {
		up := strings.ToUpper(c.SQL)
		for _, qt := range sensitiveCols {
			// Match as a select-list token (preceded by ", " or followed by " FROM"
			// / ","), not a substring of another identifier — e.g. "INFO" must not
			// match inside "MV_SYSINFO_MEM".
			if !strings.Contains(up, ", "+qt) && !strings.Contains(up, " "+qt+" FROM") && !strings.Contains(up, " "+qt+",") {
				continue
			}
			if !c.HighSensitivity {
				t.Fatalf("collector %s selects sensitive column %s but is not HighSensitivity", c.ID, qt)
			}
			listed := false
			for _, sc := range c.SensitiveColumns {
				if sc == qt {
					listed = true
				}
			}
			if !listed {
				t.Fatalf("collector %s selects %s but it is not in SensitiveColumns %v", c.ID, qt, c.SensitiveColumns)
			}
		}
	}
}

// TC-SSC-05 (#618): the pipelines collector carries CONFIG_JSON (connection
// strings / credentials) and must be HighSensitivity with it redacted on opt-out,
// while operational fields survive.
func TestPipelinesConfigRedacted(t *testing.T) {
	var p *Collector
	for i := range Catalog() {
		if Catalog()[i].ID == "singlestore_pipelines_v1" {
			c := Catalog()[i]
			p = &c
		}
	}
	if p == nil {
		t.Fatal("singlestore_pipelines_v1 missing")
	}
	if !p.HighSensitivity {
		t.Error("singlestore_pipelines_v1 must be HighSensitivity (carries CONFIG_JSON)")
	}
	found := false
	for _, col := range p.SensitiveColumns {
		if col == "CONFIG_JSON" {
			found = true
		}
	}
	if !found {
		t.Error("singlestore_pipelines_v1 must list CONFIG_JSON in SensitiveColumns")
	}
	runner := func(_ context.Context, sql string) ([]map[string]any, error) {
		if strings.Contains(sql, "PIPELINES") {
			return []map[string]any{{"PIPELINE_NAME": "ingest_kafka", "STATE": "Running", "CONFIG_JSON": `{"connection_string":"kafka://user:secret@host"}`}}, nil
		}
		return []map[string]any{}, nil
	}
	rs := Collect(context.Background(), runner, []Collector{*p}, true, false)
	if len(rs) != 1 || len(rs[0].Rows) != 1 {
		t.Fatalf("unexpected result: %+v", rs)
	}
	row := rs[0].Rows[0]
	if row["CONFIG_JSON"] != nil {
		t.Errorf("CONFIG_JSON must be redacted to nil, got %v", row["CONFIG_JSON"])
	}
	if row["PIPELINE_NAME"] != "ingest_kafka" || row["STATE"] != "Running" {
		t.Errorf("operational pipeline fields must survive, got %+v", row)
	}
}
