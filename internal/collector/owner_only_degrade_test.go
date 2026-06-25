package collector

import (
	"errors"
	"testing"

	"github.com/elevarq/signals/internal/db"
	"github.com/jackc/pgx/v5/pgconn"
)

// Owner-only privilege degradation (#200).
//
// Specification: specifications/owner_only_privilege_degradation.md
// Acceptance:    specifications/owner_only_privilege_degradation.acceptance.md
//
// pg_statistic_ext_data has PUBLIC SELECT revoked, so a least-privilege
// monitoring role (pg_monitor, no table ownership) gets a hard 42501. For
// OwnerOnlyDegrade collectors that is an expected privilege boundary, not a
// fault: the run is recorded skipped/privilege_owner_only (R116) and the
// per-cycle advisory is logged once per (target, collector) (R117).

func permDeniedErr() error {
	return &pgconn.PgError{Code: "42501", Message: "permission denied for table pg_statistic_ext_data"}
}

// R116 / TC-OOPD-01 (normal): an OwnerOnlyDegrade collector that hits a
// permission-denied error degrades to skipped/privilege_owner_only.
func TestClassifyQueryFailureOwnerOnlyPermissionDeniedDegradesToSkipped(t *testing.T) {
	status, reason := classifyQueryFailure(true, permDeniedErr())
	if status != "skipped" {
		t.Errorf("status = %q, want skipped", status)
	}
	if reason != reasonPrivilegeOwnerOnly {
		t.Errorf("reason = %q, want %q", reason, reasonPrivilegeOwnerOnly)
	}
}

// R116 / TC-OOPD-02 (boundary): only a permission-denied error degrades —
// any other failure on an OwnerOnlyDegrade collector stays failed.
func TestClassifyQueryFailureOwnerOnlyNonPermissionStaysFailed(t *testing.T) {
	status, reason := classifyQueryFailure(true, errors.New("connection reset by peer"))
	if status != "failed" {
		t.Errorf("status = %q, want failed", status)
	}
	if reason == reasonPrivilegeOwnerOnly {
		t.Errorf("reason = %q, want a real failure reason (not owner-only)", reason)
	}
}

// R116 / TC-OOPD-03 (invalid): the degrade is collector-scoped. A
// permission-denied error on a non-OwnerOnlyDegrade collector is a genuine
// failure (the role is missing pg_monitor it actually needs).
func TestClassifyQueryFailureNonOwnerPermissionDeniedStaysFailed(t *testing.T) {
	status, reason := classifyQueryFailure(false, permDeniedErr())
	if status != "failed" {
		t.Errorf("status = %q, want failed", status)
	}
	if reason != "permission_denied" {
		t.Errorf("reason = %q, want permission_denied", reason)
	}
}

// R116 / TC-OOPD-04 (invariant): a privilege_owner_only skip must NOT be a
// budget-exhausted skip, so cycleStatus never sees it as a partial trigger.
func TestPrivilegeOwnerOnlyIsNotBudgetExhausted(t *testing.T) {
	if reasonPrivilegeOwnerOnly == reasonBudgetExhausted {
		t.Fatal("privilege_owner_only must differ from budget_exhausted, else it would mark the cycle partial")
	}
	// A skipped/privilege_owner_only run reconstructs as a skip (not a
	// failure) through the persisted-run -> status path.
	statuses := BuildStatusFromRuns([]db.QueryRun{
		{QueryID: "pg_statistic_ext_data_v1", Status: "skipped", Reason: reasonPrivilegeOwnerOnly, Error: "permission denied"},
	})
	if len(statuses) != 1 {
		t.Fatalf("got %d statuses, want 1", len(statuses))
	}
	if statuses[0].Status != "skipped" || statuses[0].Reason != reasonPrivilegeOwnerOnly {
		t.Errorf("status=%q reason=%q, want skipped/%s", statuses[0].Status, statuses[0].Reason, reasonPrivilegeOwnerOnly)
	}
	if statuses[0].Attempted {
		t.Error("a skip must report Attempted=false")
	}
}

// R117 / TC-OOPD-05 (dedup): warnOnce returns true exactly once per
// (target, collector, kind) for the daemon's lifetime.
func TestWarnOnceDeduplicatesPerTargetQueryKind(t *testing.T) {
	c := &Collector{warnedOnce: make(map[string]struct{})}

	if !c.warnOnce("prod", "pg_statistic_ext_data_v1", "owner_only") {
		t.Fatal("first call must return true (advisory not yet logged)")
	}
	if c.warnOnce("prod", "pg_statistic_ext_data_v1", "owner_only") {
		t.Error("second identical call must return false (already logged)")
	}
	// A different target, collector, or kind is a distinct advisory.
	if !c.warnOnce("staging", "pg_statistic_ext_data_v1", "owner_only") {
		t.Error("a different target must warn independently")
	}
	if !c.warnOnce("prod", "pg_statistic_ext_data_mcv_v1", "owner_only") {
		t.Error("a different collector must warn independently")
	}
	if !c.warnOnce("prod", "pg_statistic_ext_data_v1", "permission_denied") {
		t.Error("a different advisory kind must warn independently")
	}
}
