package tests

import (
	"testing"

	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// TestLinterAcceptsValidSelect verifies a simple SELECT passes lint.
// Traces: ARQ-SIGNALS-R002, ARQ-SIGNALS-R013 / TC-SIG-002
func TestLinterAcceptsValidSelect(t *testing.T) {
	if err := pgqueries.LintQuery("SELECT 1"); err != nil {
		t.Fatalf("expected SELECT 1 to pass lint: %v", err)
	}
}

// TestLinterAcceptsCTE verifies a CTE query passes lint.
// Traces: ARQ-SIGNALS-R002, ARQ-SIGNALS-R013 / TC-SIG-002
func TestLinterAcceptsCTE(t *testing.T) {
	sql := "WITH cte AS (SELECT 1) SELECT * FROM cte"
	if err := pgqueries.LintQuery(sql); err != nil {
		t.Fatalf("expected CTE query to pass lint: %v", err)
	}
}

// TestLinterRejectsInsert verifies INSERT is rejected.
// Traces: ARQ-SIGNALS-R013 / TC-SIG-003
func TestLinterRejectsInsert(t *testing.T) {
	if err := pgqueries.LintQuery("INSERT INTO t VALUES (1)"); err == nil {
		t.Fatal("expected INSERT to be rejected")
	}
}

// TestLinterRejectsUpdate verifies UPDATE is rejected.
// Traces: ARQ-SIGNALS-R013 / TC-SIG-003
func TestLinterRejectsUpdate(t *testing.T) {
	if err := pgqueries.LintQuery("UPDATE t SET x = 1"); err == nil {
		t.Fatal("expected UPDATE to be rejected")
	}
}

// TestLinterRejectsDelete verifies DELETE is rejected.
// Traces: ARQ-SIGNALS-R013 / TC-SIG-003
func TestLinterRejectsDelete(t *testing.T) {
	if err := pgqueries.LintQuery("DELETE FROM t"); err == nil {
		t.Fatal("expected DELETE to be rejected")
	}
}

// TestLinterRejectsDrop verifies DROP is rejected.
// Traces: ARQ-SIGNALS-R013 / TC-SIG-003
func TestLinterRejectsDrop(t *testing.T) {
	if err := pgqueries.LintQuery("DROP TABLE t"); err == nil {
		t.Fatal("expected DROP to be rejected")
	}
}

// TestLinterRejectsPgSleep verifies pg_sleep() is rejected.
// Traces: ARQ-SIGNALS-R013 / TC-SIG-003
func TestLinterRejectsPgSleep(t *testing.T) {
	if err := pgqueries.LintQuery("SELECT pg_sleep(10)"); err == nil {
		t.Fatal("expected pg_sleep to be rejected")
	}
}

// TestLinterRejectsPgTerminate verifies pg_terminate_backend() is rejected.
// Traces: ARQ-SIGNALS-R013 / TC-SIG-003
func TestLinterRejectsPgTerminate(t *testing.T) {
	if err := pgqueries.LintQuery("SELECT pg_terminate_backend(123)"); err == nil {
		t.Fatal("expected pg_terminate_backend to be rejected")
	}
}

// TestLinterRejectsEmpty verifies an empty string is rejected.
// Traces: ARQ-SIGNALS-R013 / TC-SIG-003
func TestLinterRejectsEmpty(t *testing.T) {
	if err := pgqueries.LintQuery(""); err == nil {
		t.Fatal("expected empty SQL to be rejected")
	}
}

// TestLinterRejectsMultiStatement verifies multiple statements are rejected.
// Traces: ARQ-SIGNALS-R013 / TC-SIG-003
func TestLinterRejectsMultiStatement(t *testing.T) {
	if err := pgqueries.LintQuery("SELECT 1; DROP TABLE t"); err == nil {
		t.Fatal("expected multi-statement SQL to be rejected")
	}
}
