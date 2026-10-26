package collector

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/elevarq/signals/internal/pgqueries"
)

// Tests for the #647 structural-query-text capability
// (specifications/structural-query-text.md and its acceptance cases).

func digestQD() pgqueries.QueryDef {
	return pgqueries.QueryDef{
		ID:               "mysql_statement_digests_v1",
		HighSensitivity:  true,
		SensitiveColumns: []string{"DIGEST_TEXT"},
	}
}

// TC-SQT-01 — secure default: DIGEST_TEXT is null when not opted in,
// regardless of the generic high-sensitivity switch (SQT-R001/INV-SQT-02).
func TestSQT01_SecureDefaultRedactsDigestText(t *testing.T) {
	for _, highSens := range []bool{false, true} {
		q := digestQD()
		rows := []map[string]any{{"DIGEST": "abc", "DIGEST_TEXT": "SELECT * FROM orders WHERE id = ?"}}
		redactHighSensitivityColumnsIfNeeded(q, rows, highSens /*structural*/, false)
		if rows[0]["DIGEST_TEXT"] != nil {
			t.Errorf("highSensitivity=%v: DIGEST_TEXT must be nil when structural opt-in is off, got %v", highSens, rows[0]["DIGEST_TEXT"])
		}
		if rows[0]["DIGEST"] != "abc" {
			t.Errorf("non-sensitive DIGEST column was altered: %v", rows[0]["DIGEST"])
		}
	}
}

// TC-SQT-02 — opt-in retains the normalized text verbatim (under cap) with
// digest_text_truncated=false (SQT-R001/R003, INV-SQT-03 producer side).
func TestSQT02_OptInRetainsBoundedText(t *testing.T) {
	q := digestQD()
	src := "SELECT * FROM orders WHERE id = ?"
	rows := []map[string]any{{"DIGEST_TEXT": src}}
	redactHighSensitivityColumnsIfNeeded(q, rows, false, true)
	applyStructuralQueryTextBound(q, rows, true)
	if rows[0]["DIGEST_TEXT"] != src {
		t.Errorf("DIGEST_TEXT not retained verbatim: got %v", rows[0]["DIGEST_TEXT"])
	}
	if rows[0][digestTextTruncatedField] != false {
		t.Errorf("digest_text_truncated must be false for an under-cap value, got %v", rows[0][digestTextTruncatedField])
	}
}

// TC-SQT-03 — the opt-in does NOT un-redact other sensitive columns
// (SQT-R002/INV-SQT-04): trx_query / LOCK_DATA stay null with highSens off.
func TestSQT03_OptInDoesNotWidenOtherColumns(t *testing.T) {
	for _, c := range []struct {
		id, col, val string
	}{
		{"mysql_innodb_transactions_v1", "trx_query", "UPDATE accounts SET balance=9.99"},
		{"mysql_data_locks_v1", "LOCK_DATA", "0x1234"},
	} {
		q := pgqueries.QueryDef{ID: c.id, HighSensitivity: true, SensitiveColumns: []string{c.col}}
		rows := []map[string]any{{c.col: c.val}}
		// structural opt-in ON, generic high-sensitivity OFF.
		redactHighSensitivityColumnsIfNeeded(q, rows, false, true)
		applyStructuralQueryTextBound(q, rows, true)
		if rows[0][c.col] != nil {
			t.Errorf("%s.%s must stay redacted under the structural opt-in, got %v", c.id, c.col, rows[0][c.col])
		}
		if _, stamped := rows[0][digestTextTruncatedField]; stamped {
			t.Errorf("%s: non-digest collector must not get a digest_text_truncated flag", c.id)
		}
	}
}

// TC-SQT-04 — the switches are independent: generic high-sensitivity ON but
// structural OFF still redacts DIGEST_TEXT (SQT-R001/R002 independence).
func TestSQT04_SwitchesAreIndependent(t *testing.T) {
	q := digestQD()
	rows := []map[string]any{{"DIGEST_TEXT": "SELECT 1"}}
	redactHighSensitivityColumnsIfNeeded(q, rows /*highSens*/, true /*structural*/, false)
	if rows[0]["DIGEST_TEXT"] != nil {
		t.Errorf("enabling high-sensitivity must NOT implicitly enable structural query text; DIGEST_TEXT = %v", rows[0]["DIGEST_TEXT"])
	}
}

// TC-SQT-05 — truncation is machine-visible and UTF-8-safe (SQT-R003/R007).
func TestSQT05_TruncationMachineVisibleAndUTF8Safe(t *testing.T) {
	// Build a value longer than the cap whose cap boundary lands inside a
	// multi-byte rune ("é" = 2 bytes).
	long := strings.Repeat("a", structuralQueryTextMaxBytes-1) + "é" + "trailing"
	q := digestQD()
	rows := []map[string]any{{"DIGEST_TEXT": long}}
	applyStructuralQueryTextBound(q, rows, true)

	got, _ := rows[0]["DIGEST_TEXT"].(string)
	if !utf8.ValidString(got) {
		t.Fatalf("truncated DIGEST_TEXT is not valid UTF-8")
	}
	if len(got) > structuralQueryTextMaxBytes {
		t.Errorf("truncated value exceeds cap: %d > %d", len(got), structuralQueryTextMaxBytes)
	}
	if !strings.HasSuffix(got, structuralQueryTextMarker) {
		t.Errorf("truncated value must end with the marker %q", structuralQueryTextMarker)
	}
	if rows[0][digestTextTruncatedField] != true {
		t.Errorf("digest_text_truncated must be true for a truncated value")
	}
}

// TC-SQT-06 — a null source digest stays null, marked not truncated
// (SQT-R001/R003): nothing is fabricated.
func TestSQT06_NullSourceStaysNull(t *testing.T) {
	q := digestQD()
	rows := []map[string]any{{"DIGEST_TEXT": nil}}
	redactHighSensitivityColumnsIfNeeded(q, rows, false, true)
	applyStructuralQueryTextBound(q, rows, true)
	if rows[0]["DIGEST_TEXT"] != nil {
		t.Errorf("null source DIGEST_TEXT must stay null, got %v", rows[0]["DIGEST_TEXT"])
	}
	if rows[0][digestTextTruncatedField] != false {
		t.Errorf("null source must be marked not truncated, got %v", rows[0][digestTextTruncatedField])
	}
}

// TC-SQT-09 (unit portion) — MariaDB reuses the MySQL digest path: the
// structural column is resolved by collector ID, which is shared across the
// MySQL-wire family, so no divergent path exists (SQT-R008).
func TestSQT09_SharedDigestCollectorHasStructuralColumn(t *testing.T) {
	col, ok := structuralQueryTextColumnFor(digestQD())
	if !ok || col != "DIGEST_TEXT" {
		t.Fatalf("shared digest collector must resolve structural column DIGEST_TEXT, got (%q,%v)", col, ok)
	}
	// A non-digest collector resolves no structural column.
	if _, ok := structuralQueryTextColumnFor(pgqueries.QueryDef{ID: "mysql_innodb_transactions_v1"}); ok {
		t.Errorf("non-digest collector must not resolve a structural column")
	}
}

// Disabled capability must be a strict no-op for the bound helper
// (INV-SQT-02): no flag stamped, value untouched.
func TestSQT_BoundDisabledIsNoOp(t *testing.T) {
	q := digestQD()
	rows := []map[string]any{{"DIGEST_TEXT": "SELECT 1"}}
	applyStructuralQueryTextBound(q, rows, false)
	if rows[0]["DIGEST_TEXT"] != "SELECT 1" {
		t.Errorf("disabled bound helper must not touch the value, got %v", rows[0]["DIGEST_TEXT"])
	}
	if _, stamped := rows[0][digestTextTruncatedField]; stamped {
		t.Errorf("disabled bound helper must not stamp the truncation flag")
	}
}
