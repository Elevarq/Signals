// Tests for the persisted last-cycle outcome (SIGNALS-R126).
//
// Spec: features/signals/specification.md — SIGNALS-R126
// Acceptance: features/signals/acceptance-tests.md — TC-SIG-126
//
// A collection cycle that fails to produce any successful collector for
// an enabled target (cycleStatus == "failed") must record its failure
// durably, keyed by target, carrying a bounded failure category and a
// timestamp — never any credential or secret. A later successful cycle
// for the same target clears the marker. These records are what let the
// export distinguish "last collection failed" from "no collection yet"
// (SIGNALS-R125) and close the FC-05 false-clean gap.
package tests

import (
	"reflect"
	"testing"

	"github.com/elevarq/signals/internal/db"
)

// TestFailedCyclePersistsOutcome — a failed cycle is recorded durably
// with status=failed and a bounded category (TC-SIG-126). RED until the
// RecordCycleOutcome / GetCycleOutcomes DB surface + migration exist.
func TestFailedCyclePersistsOutcome(t *testing.T) {
	store := openTestDB(t)

	if err := store.RecordCycleOutcome("target-A", "failed", "connect_error", "", ""); err != nil {
		t.Fatalf("RecordCycleOutcome: %v", err)
	}

	outcomes, err := store.GetCycleOutcomes()
	if err != nil {
		t.Fatalf("GetCycleOutcomes: %v", err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("GetCycleOutcomes returned %d rows, want 1", len(outcomes))
	}
	got := outcomes[0]
	if got.TargetName != "target-A" {
		t.Errorf("TargetName = %q, want target-A", got.TargetName)
	}
	if got.Status != "failed" {
		t.Errorf("Status = %q, want failed", got.Status)
	}
	if got.Category != "connect_error" {
		t.Errorf("Category = %q, want connect_error", got.Category)
	}
	if got.UpdatedAt == "" {
		t.Errorf("UpdatedAt is empty, want an RFC3339 timestamp")
	}
}

// TestSuccessfulCycleClearsFailedOutcome — a later success for the same
// target replaces the failed marker with success (TC-SIG-126).
func TestSuccessfulCycleClearsFailedOutcome(t *testing.T) {
	store := openTestDB(t)

	if err := store.RecordCycleOutcome("target-A", "failed", "safety_check", "", ""); err != nil {
		t.Fatalf("RecordCycleOutcome (failed): %v", err)
	}
	if err := store.RecordCycleOutcome("target-A", "success", "", "", ""); err != nil {
		t.Fatalf("RecordCycleOutcome (success): %v", err)
	}

	outcomes, err := store.GetCycleOutcomes()
	if err != nil {
		t.Fatalf("GetCycleOutcomes: %v", err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("GetCycleOutcomes returned %d rows, want 1 (upsert per target)", len(outcomes))
	}
	if outcomes[0].Status != "success" {
		t.Errorf("Status after success = %q, want success (failure marker cleared)", outcomes[0].Status)
	}
	if outcomes[0].Category != "" {
		t.Errorf("Category after success = %q, want empty", outcomes[0].Category)
	}
}

// TestCycleOutcomeCarriesNoSecrets — the record schema stores only the
// target name, status, category, and timestamp; no credential/DSN
// column exists (INV-SIGNALS-07). We assert the row shape is exactly
// the bounded fields.
func TestCycleOutcomeCarriesNoSecrets(t *testing.T) {
	store := openTestDB(t)

	if err := store.RecordCycleOutcome("target-secretful", "failed", "connect_error", "", ""); err != nil {
		t.Fatalf("RecordCycleOutcome: %v", err)
	}
	outcomes, err := store.GetCycleOutcomes()
	if err != nil {
		t.Fatalf("GetCycleOutcomes: %v", err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("want 1 outcome, got %d", len(outcomes))
	}
	// The durable record is metadata only (INV-SIGNALS-07): target
	// name, status, bounded category, operator reason code, a REDACTED
	// message, and a timestamp. The target name is stored verbatim (it
	// is not a secret) and the message is redacted BEFORE it is written
	// (collector.redactFailureMessage → RedactDSN), so no credential /
	// DSN / host value is ever persisted un-redacted. Guard the bounded
	// field set structurally: reflection over the CycleOutcome type must
	// expose exactly these six fields, so a future secret-bearing column
	// cannot slip in unnoticed. SIGNALS-R128 (#681) grew the set from
	// four to six (ReasonCode, Message) — both metadata-only.
	fields := reflect.VisibleFields(reflect.TypeOf(db.CycleOutcome{}))
	got := make(map[string]bool, len(fields))
	for _, f := range fields {
		got[f.Name] = true
	}
	for _, want := range []string{"TargetName", "Status", "Category", "ReasonCode", "Message", "UpdatedAt"} {
		if !got[want] {
			t.Errorf("CycleOutcome missing expected field %q", want)
		}
	}
	if len(got) != 6 {
		t.Errorf("CycleOutcome has %d fields %v, want exactly 6 (metadata only, no secret column)", len(got), got)
	}
}
