package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/elevarq/signals/internal/guidedconnect"
)

// TestWriteAutoOutcome_Success renders the confirmation and the ready
// (secret-free) target block.
func TestWriteAutoOutcome_Success(t *testing.T) {
	var buf bytes.Buffer
	if err := writeAutoOutcome(&buf, guidedconnect.Outcome{
		Success:     true,
		Method:      "aws_rds_iam",
		Message:     "connected and the role passed the read-only safety check.",
		ConfigBlock: "  - name: orders\n    auth_method: aws_rds_iam\n    sslmode: verify-full\n",
	}); err != nil {
		t.Fatalf("writeAutoOutcome: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"OK", "auth_method: aws_rds_iam", "sslmode: verify-full", "Add this target"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// TestWriteAutoOutcome_SuccessWrote notes the file append.
func TestWriteAutoOutcome_SuccessWrote(t *testing.T) {
	var buf bytes.Buffer
	if err := writeAutoOutcome(&buf, guidedconnect.Outcome{Success: true, Wrote: true, ConfigBlock: "  - name: x\n"}); err != nil {
		t.Fatalf("writeAutoOutcome: %v", err)
	}
	if !strings.Contains(buf.String(), "Appended the target block") {
		t.Errorf("expected append note:\n%s", buf.String())
	}
}

// TestWriteAutoOutcome_Failure renders the category and actionable message.
func TestWriteAutoOutcome_Failure(t *testing.T) {
	var buf bytes.Buffer
	if err := writeAutoOutcome(&buf, guidedconnect.Outcome{
		Success:  false,
		Category: "auth",
		Message:  "GRANT rds_iam TO signals;",
	}); err != nil {
		t.Fatalf("writeAutoOutcome: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "FAIL [auth]") || !strings.Contains(out, "GRANT rds_iam") {
		t.Errorf("unexpected failure rendering:\n%s", out)
	}
}

// TestConnectAutoCmd_RequiresUser exercises the CLI wiring + usage-error
// mapping without any network: a missing --user is caught by Run before
// any detection or connection.
func TestConnectAutoCmd_RequiresUser(t *testing.T) {
	cmd := connectAutoCmd()
	cmd.SetArgs([]string{"--host", "db.example.com"})
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected a usage error for missing --user")
	}
	if _, ok := err.(usageError); !ok {
		t.Fatalf("want usageError, got %T: %v", err, err)
	}
}
