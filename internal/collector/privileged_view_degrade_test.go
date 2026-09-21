package collector

import (
	"errors"
	"testing"

	"github.com/elevarq/signals/internal/db"
	"github.com/jackc/pgx/v5/pgconn"
)

// Privileged-view privilege degradation (#305).
//
// Specification: specifications/collectors/pg_hba_file_rules_v1.md
//
// pg_hba_file_rules needs SELECT on the view plus EXECUTE on
// pg_hba_file_rules() (neither pg_monitor nor pg_read_all_settings grants
// them). A role lacking those grants gets a hard 42501. For
// PrivilegedViewDegrade collectors that is an expected privilege boundary,
// not a fault: the run is recorded skipped/privilege_restricted so the
// cycle is not reported partial.

func hbaPermDeniedErr() error {
	return &pgconn.PgError{Code: "42501", Message: "permission denied for view pg_hba_file_rules"}
}

// #305 (normal): a PrivilegedViewDegrade collector that hits a
// permission-denied error degrades to skipped/privilege_restricted.
func TestClassifyQueryFailurePrivilegedViewPermissionDeniedDegradesToSkipped(t *testing.T) {
	status, reason := classifyQueryFailure(false, true, hbaPermDeniedErr())
	if status != "skipped" {
		t.Errorf("status = %q, want skipped", status)
	}
	if reason != reasonPrivilegeRestricted {
		t.Errorf("reason = %q, want %q", reason, reasonPrivilegeRestricted)
	}
}

// #305 (boundary): only a permission-denied error degrades — any other
// failure on a PrivilegedViewDegrade collector stays failed.
func TestClassifyQueryFailurePrivilegedViewNonPermissionStaysFailed(t *testing.T) {
	status, reason := classifyQueryFailure(false, true, errors.New("connection reset by peer"))
	if status != "failed" {
		t.Errorf("status = %q, want failed", status)
	}
	if reason == reasonPrivilegeRestricted {
		t.Errorf("reason = %q, want a real failure reason (not privilege_restricted)", reason)
	}
}

// #305 (invalid): the degrade is collector-scoped. A permission-denied
// error on a collector with neither degrade flag is a genuine failure.
func TestClassifyQueryFailureNonPrivilegedViewPermissionDeniedStaysFailed(t *testing.T) {
	status, reason := classifyQueryFailure(false, false, hbaPermDeniedErr())
	if status != "failed" {
		t.Errorf("status = %q, want failed", status)
	}
	if reason != "permission_denied" {
		t.Errorf("reason = %q, want permission_denied", reason)
	}
}

// #305 (invariant): a privilege_restricted skip must NOT be a
// budget-exhausted skip, so cycleStatus never sees it as a partial
// trigger; and it must reconstruct as a skip through the persisted-run
// -> status path.
func TestPrivilegeRestrictedIsNotBudgetExhausted(t *testing.T) {
	if reasonPrivilegeRestricted == reasonBudgetExhausted {
		t.Fatal("privilege_restricted must differ from budget_exhausted, else it would mark the cycle partial")
	}
	statuses := BuildStatusFromRuns([]db.QueryRun{
		{QueryID: "pg_hba_file_rules_v1", Status: "skipped", Reason: reasonPrivilegeRestricted, Error: "permission denied"},
	})
	if len(statuses) != 1 {
		t.Fatalf("got %d statuses, want 1", len(statuses))
	}
	if statuses[0].Status != "skipped" || statuses[0].Reason != reasonPrivilegeRestricted {
		t.Errorf("status=%q reason=%q, want skipped/%s", statuses[0].Status, statuses[0].Reason, reasonPrivilegeRestricted)
	}
	if statuses[0].Attempted {
		t.Error("a skip must report Attempted=false")
	}
}
