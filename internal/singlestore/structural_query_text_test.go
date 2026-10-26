package singlestore

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// #647 structural-query-text unit coverage (specifications/structural-query-text.md).

func digestColl() Collector {
	return Collector{
		ID:               "mysql_statement_digests_v1",
		HighSensitivity:  true,
		SensitiveColumns: []string{"DIGEST_TEXT"},
		StructuralColumn: "DIGEST_TEXT",
	}
}

// TC-SQT-01 — secure default: DIGEST_TEXT nil when the structural opt-in is
// off, regardless of the generic high-sensitivity switch (redactSensitive).
func TestSQT01_SecureDefault(t *testing.T) {
	for _, redact := range []bool{true, false} {
		rows := []map[string]any{{"DIGEST": "abc", "DIGEST_TEXT": "SELECT * FROM orders WHERE id = ?"}}
		applyWireRedaction(digestColl(), rows, redact /*structural*/, false)
		if rows[0]["DIGEST_TEXT"] != nil {
			t.Errorf("redactSensitive=%v: DIGEST_TEXT must be nil with structural opt-in off, got %v", redact, rows[0]["DIGEST_TEXT"])
		}
		if rows[0]["DIGEST"] != "abc" {
			t.Errorf("non-sensitive column altered: %v", rows[0]["DIGEST"])
		}
	}
}

// TC-SQT-02 — opt-in retains the value verbatim under the cap, not truncated.
func TestSQT02_OptInRetains(t *testing.T) {
	src := "SELECT * FROM orders WHERE id = ?"
	rows := []map[string]any{{"DIGEST_TEXT": src}}
	applyWireRedaction(digestColl(), rows, true, true)
	if rows[0]["DIGEST_TEXT"] != src {
		t.Errorf("DIGEST_TEXT not retained verbatim: %v", rows[0]["DIGEST_TEXT"])
	}
	if rows[0][DigestTextTruncatedField] != false {
		t.Errorf("under-cap value must be marked not truncated")
	}
}

// TC-SQT-04 — the switches are independent: generic high-sensitivity ON
// (redactSensitive=false) but structural OFF still nils DIGEST_TEXT.
func TestSQT04_Independent(t *testing.T) {
	rows := []map[string]any{{"DIGEST_TEXT": "SELECT 1"}}
	applyWireRedaction(digestColl(), rows /*redactSensitive*/, false /*structural*/, false)
	if rows[0]["DIGEST_TEXT"] != nil {
		t.Errorf("high-sensitivity must not implicitly enable structural text; got %v", rows[0]["DIGEST_TEXT"])
	}
}

// TC-SQT-05 — truncation is machine-visible and UTF-8-safe.
func TestSQT05_Truncation(t *testing.T) {
	long := strings.Repeat("a", StructuralQueryTextMaxBytes-1) + "é" + "trailing"
	rows := []map[string]any{{"DIGEST_TEXT": long}}
	applyWireRedaction(digestColl(), rows, true, true)
	got, _ := rows[0]["DIGEST_TEXT"].(string)
	if !utf8.ValidString(got) {
		t.Fatalf("truncated value is not valid UTF-8")
	}
	if len(got) > StructuralQueryTextMaxBytes {
		t.Errorf("truncated value exceeds cap: %d > %d", len(got), StructuralQueryTextMaxBytes)
	}
	if !strings.HasSuffix(got, StructuralQueryTextMarker) {
		t.Errorf("truncated value must end with the marker")
	}
	if rows[0][DigestTextTruncatedField] != true {
		t.Errorf("digest_text_truncated must be true")
	}
}

// TC-SQT-06 — a null source stays null, marked not truncated.
func TestSQT06_NullSource(t *testing.T) {
	rows := []map[string]any{{"DIGEST_TEXT": nil}}
	applyWireRedaction(digestColl(), rows, true, true)
	if rows[0]["DIGEST_TEXT"] != nil {
		t.Errorf("null source must stay null")
	}
	if rows[0][DigestTextTruncatedField] != false {
		t.Errorf("null source must be marked not truncated")
	}
}

// TC-SQT-03 — other sensitive columns are not surfaced by the structural
// opt-in; they stay governed by redactSensitive.
func TestSQT03_OtherColumnsNotWidened(t *testing.T) {
	c := Collector{ID: "mysql_innodb_transactions_v1", HighSensitivity: true, SensitiveColumns: []string{"trx_query"}}
	rows := []map[string]any{{"trx_query": "UPDATE accounts SET balance = 9.99"}}
	// structural opt-in ON, generic high-sensitivity ON (redactSensitive).
	applyWireRedaction(c, rows, true, true)
	if rows[0]["trx_query"] != nil {
		t.Errorf("trx_query must stay redacted under the structural opt-in, got %v", rows[0]["trx_query"])
	}
	if _, stamped := rows[0][DigestTextTruncatedField]; stamped {
		t.Errorf("non-structural collector must not get a digest_text_truncated flag")
	}
}

// TC-SQT-09 — both digest collectors carry the structural column; qualify
// independently but share the implementation (SQT-R008).
func TestSQT09_DigestCollectorsMarked(t *testing.T) {
	for _, engine := range []string{"mysql", "mariadb"} {
		var found bool
		for _, c := range CatalogFor(engine) {
			if c.ID == engine+"_statement_digests_v1" {
				found = true
				if c.StructuralColumn != "DIGEST_TEXT" {
					t.Errorf("%s digest collector StructuralColumn = %q, want DIGEST_TEXT", engine, c.StructuralColumn)
				}
			}
		}
		if !found {
			t.Errorf("%s has no digest collector", engine)
		}
	}
}

// TC-SQT-10 — SingleStore boundary: its plancache collector (QUERY_TEXT from
// MV_PLANCACHE) carries NO StructuralColumn, so the capability never surfaces
// it. MySQL-wire compatibility does not imply security equivalence (#647).
func TestSQT10_SingleStoreNotAutoEnabled(t *testing.T) {
	for _, c := range CatalogFor("singlestore") {
		if c.StructuralColumn != "" {
			t.Errorf("SingleStore collector %q has StructuralColumn %q — must be empty until independently qualified (#647 boundary)", c.ID, c.StructuralColumn)
		}
	}
	// And the plancache QUERY_TEXT stays redacted under the structural opt-in.
	c := Collector{ID: "singlestore_plancache_v1", HighSensitivity: true, SensitiveColumns: []string{"QUERY_TEXT"}}
	rows := []map[string]any{{"QUERY_TEXT": "SELECT * FROM t WHERE secret = 'abc'"}}
	applyWireRedaction(c, rows /*redactSensitive*/, true /*structural*/, true)
	if rows[0]["QUERY_TEXT"] != nil {
		t.Errorf("SingleStore QUERY_TEXT must stay redacted under the MySQL/MariaDB structural opt-in, got %v", rows[0]["QUERY_TEXT"])
	}
}
