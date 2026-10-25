package export

import (
	"strings"
	"testing"

	"github.com/elevarq/signals/internal/clickhouse"
	"github.com/elevarq/signals/internal/pgqueries"
	"github.com/elevarq/signals/internal/singlestore"
)

// Tests derive from specifications/workload-measurement-model.acceptance.md.
//
// Producer-side guard for the #533 workload model: every engine's workload
// collector carries its native typed identity (WMM-R001) and redacts its sample
// SQL while retaining the non-sensitive identity (WMM-R003 / INV-WMM-02). The
// normalized envelope (WMM-R004) and the consumer never-equate obligation
// (WMM-R002) are model/consumer items tracked with the #530 report contract.

// selectsColumn reports whether sql selects col as a distinct select-list token
// (so "DIGEST" does not match inside "DIGEST_TEXT").
func selectsColumn(sql, col string) bool {
	up := strings.ToUpper(sql)
	c := strings.ToUpper(col)
	return strings.Contains(up, ", "+c+",") ||
		strings.Contains(up, ", "+c+" ") ||
		strings.Contains(up, " "+c+" FROM") ||
		strings.Contains(up, ", "+c+"\n")
}

// TC-WMM-01 / TC-WMM-02: the wire + ClickHouse workload collectors carry a typed
// identity and redact their sample SQL.
func TestWorkloadIdentityAndRedaction(t *testing.T) {
	type tc struct {
		engine      string
		collID      string
		identityCol string
		sampleCol   string
	}
	// Resolve each engine's workload collector (ID, SQL, HighSensitivity,
	// SensitiveColumns) via a uniform lookup over the real catalogs.
	type coll struct {
		id        string
		sql       string
		highSens  bool
		sensitive []string
	}
	lookup := func(engine, id string) *coll {
		if engine == "clickhouse" {
			for _, c := range clickhouse.Catalog() {
				if c.ID == id {
					return &coll{c.ID, c.SQL, c.HighSensitivity, c.SensitiveColumns}
				}
			}
			return nil
		}
		for _, c := range singlestore.CatalogFor(engine) {
			if c.ID == id {
				return &coll{c.ID, c.SQL, c.HighSensitivity, c.SensitiveColumns}
			}
		}
		return nil
	}

	cases := []tc{
		{"singlestore", "singlestore_plancache_v1", "PLAN_ID", "QUERY_TEXT"},
		{"mysql", "mysql_statement_digests_v1", "DIGEST", "DIGEST_TEXT"},
		{"mariadb", "mariadb_statement_digests_v1", "DIGEST", "DIGEST_TEXT"},
		{"clickhouse", "clickhouse_query_log_v1", "normalized_query_hash", "query"},
	}
	for _, c := range cases {
		t.Run(c.engine, func(t *testing.T) {
			wc := lookup(c.engine, c.collID)
			if wc == nil {
				t.Fatalf("%s workload collector %s not found", c.engine, c.collID)
			}
			// WMM-R001: the typed identity column is selected.
			if !selectsColumn(wc.sql, c.identityCol) {
				t.Errorf("WMM-R001: %s must select identity column %q; SQL=%q", c.collID, c.identityCol, wc.sql)
			}
			// WMM-R003: sample SQL is HighSensitivity + redacted.
			if !wc.highSens {
				t.Errorf("WMM-R003: %s carries sample SQL %q and must be HighSensitivity", c.collID, c.sampleCol)
			}
			sampleRedacted, idRedacted := false, false
			for _, s := range wc.sensitive {
				if s == c.sampleCol {
					sampleRedacted = true
				}
				if s == c.identityCol {
					idRedacted = true
				}
			}
			if !sampleRedacted {
				t.Errorf("WMM-R003: %s must list sample SQL %q in SensitiveColumns %v", c.collID, c.sampleCol, wc.sensitive)
			}
			// The non-sensitive typed identity must survive (not be redacted).
			if idRedacted {
				t.Errorf("WMM-R003: identity column %q must NOT be redacted (it is non-sensitive)", c.identityCol)
			}
		})
	}
}

// TC-WMM-01 (PostgreSQL leg): pg_stat_statements carries the queryid identity.
// PostgreSQL sample-SQL redaction is RedactStatementSecrets (REDACT-R002/R003,
// a separate mechanism), not SensitiveColumns, so it is referenced, not re-tested
// here.
func TestWorkloadIdentityPostgreSQL(t *testing.T) {
	q := pgqueries.ByID("pg_stat_statements_v1")
	if q == nil {
		t.Fatal("WMM-R001: pg_stat_statements_v1 not found")
	}
	// Identity is queryid (selected via s.* and used in the stable ORDER BY).
	if !strings.Contains(strings.ToLower(q.SQL), "queryid") {
		t.Errorf("WMM-R001: pg_stat_statements_v1 must carry the queryid identity; SQL=%q", q.SQL)
	}
}

// TC-WMM-03: MySQL and MariaDB share the `digest` identity KIND but are distinct
// collectors scoped to their own engine catalogs — the producer never emits a
// single cross-engine-equated identity (the never-equate guarantee is scoped by
// engine; the consumer obligation is Analyzer's, #530).
func TestWorkloadIdentityScopedPerEngine(t *testing.T) {
	mysqlDigest, mariaDigest := false, false
	for _, c := range singlestore.CatalogFor("mysql") {
		if c.ID == "mysql_statement_digests_v1" {
			mysqlDigest = true
		}
		if strings.HasPrefix(c.ID, "mariadb_") {
			t.Errorf("WMM-R002: mysql catalog leaked a mariadb collector %q", c.ID)
		}
	}
	for _, c := range singlestore.CatalogFor("mariadb") {
		if c.ID == "mariadb_statement_digests_v1" {
			mariaDigest = true
		}
		if strings.HasPrefix(c.ID, "mysql_") {
			t.Errorf("WMM-R002: mariadb catalog leaked a mysql collector %q", c.ID)
		}
	}
	if !mysqlDigest || !mariaDigest {
		t.Fatalf("WMM-R002: expected a digest collector in each of mysql(%v)/mariadb(%v)", mysqlDigest, mariaDigest)
	}
}
