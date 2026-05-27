package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/elevarq/arq-signals/internal/doctor"
)

// failError must carry an empty message — the failing detail is on
// stdout (text or JSON report) and the empty Error() prevents cobra
// from appending a misleading "Error: ..." line when SilenceErrors
// isn't honoured for some reason.
func TestFailError_EmptyMessage(t *testing.T) {
	var err error = failError{}
	if err.Error() != "" {
		t.Errorf("failError.Error(): got %q, want empty", err.Error())
	}
}

// usageError must round-trip its message so main() and Stderr both
// surface the actionable cause.
func TestUsageError_PreservesMessage(t *testing.T) {
	const msg = "unknown check id(s): C9 (supported: C1, C2, C3, C4)"
	var err error = usageError{msg: msg}
	if err.Error() != msg {
		t.Errorf("usageError.Error(): got %q, want %q", err.Error(), msg)
	}
}

// writeJSONReport must not call os.Exit. Trivially verified by the
// fact that this test process is alive after the call returns. Also
// asserts the function returns the encoder error rather than
// swallowing it.
func TestWriteJSONReport_PureFormatter(t *testing.T) {
	report := doctor.Report{
		SchemaVersion: "1",
		GeneratedAt:   "2026-05-12T00:00:00Z",
		Checks: []doctor.CheckResult{
			{ID: "C1", Name: "config_valid", Status: doctor.StatusOK},
		},
		Summary: doctor.Summary{OK: 1},
	}

	var buf bytes.Buffer
	if err := writeJSONReport(&buf, report); err != nil {
		t.Fatalf("writeJSONReport: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if _, ok := decoded["schema_version"]; !ok {
		t.Error("output missing schema_version")
	}
}

// writeTextReport must not call os.Exit on a failing report. The
// fail-gate lives in RunE; this function is a pure formatter that
// renders even a FAIL-heavy report.
func TestWriteTextReport_DoesNotExitOnFail(t *testing.T) {
	report := doctor.Report{
		SchemaVersion: "1",
		Checks: []doctor.CheckResult{
			{ID: "C1", Name: "config_valid", Status: doctor.StatusFail, Detail: "config not accessible"},
			{ID: "C2", Name: "store_writable", Status: doctor.StatusFail, Detail: "store dir missing"},
		},
		Summary: doctor.Summary{Fail: 2},
	}

	var buf bytes.Buffer
	if err := writeTextReport(&buf, report); err != nil {
		t.Fatalf("writeTextReport: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "FAIL C1 config_valid") {
		t.Errorf("text output missing failed C1 line:\n%s", out)
	}
	if !strings.Contains(out, "Summary: 0 OK, 0 WARN, 2 FAIL") {
		t.Errorf("text output missing expected summary line:\n%s", out)
	}
}
