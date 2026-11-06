// Tests for the operator-facing collection-failure classifier and the
// redacted failure message (SIGNALS-R128, #681).
//
// Spec:        features/signals/specification.md — SIGNALS-R128
// Acceptance:  features/signals/acceptance-tests.md — TC-SIG-140, TC-SIG-141
//
// Before #681, classifyCollectionFailure flattened every unrecognised
// error to the bare metric reason "internal", and the real error text
// was logged but never surfaced over the API. These tests pin two new
// behaviours:
//
//   - classifyFailureReason maps recognizable PostgreSQL / connection
//     errors to a SPECIFIC metrics.FailureReasonCode (TC-SIG-140), and
//   - even when it cannot (ReasonUnknown), the redacted message still
//     carries the real error text with any DSN/credential scrubbed
//     (TC-SIG-141 / INV-SIGNALS-07).
package collector

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/elevarq/signals/internal/metrics"
)

// TestClassifyFailureReason_MapsRecognizableErrors — TC-SIG-140.
// Recognizable errors resolve to a specific FailureReasonCode, never
// the catch-all. RED until classifyFailureReason exists.
func TestClassifyFailureReason_MapsRecognizableErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want metrics.FailureReasonCode
	}{
		{"auth 28P01", &pgconn.PgError{Code: "28P01", Message: `password authentication failed for user "elevarq"`}, metrics.ReasonAuthFailed},
		{"auth 28000", &pgconn.PgError{Code: "28000", Message: "invalid authorization specification"}, metrics.ReasonAuthFailed},
		{"role 42501", &pgconn.PgError{Code: "42501", Message: "permission denied for view pg_stat_activity"}, metrics.ReasonRoleInsufficient},
		{"query cancel 57014", &pgconn.PgError{Code: "57014", Message: "canceling statement due to statement timeout"}, metrics.ReasonCollectorQueryTimeout},
		{"wrapped OpError refused", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connect: connection refused")}, metrics.ReasonTargetUnreachable},
		{"pgx wrapped refused", errors.New("failed to connect to host=127.0.0.1: dial tcp 127.0.0.1:5432: connect: connection refused"), metrics.ReasonTargetUnreachable},
		{"i/o timeout", errors.New("dial tcp 10.0.0.5:5432: i/o timeout"), metrics.ReasonTargetUnreachable},
		{"tls handshake", errors.New("tls: handshake failure"), metrics.ReasonTargetTLSInvalid},
		{"x509 verify", errors.New("x509: certificate signed by unknown authority"), metrics.ReasonTargetTLSInvalid},
		{"statement timeout string", errors.New("ERROR: canceling statement due to statement timeout"), metrics.ReasonCollectorQueryTimeout},
		{"context deadline", errors.New("context deadline exceeded"), metrics.ReasonCollectorQueryTimeout},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := classifyFailureReason(c.err)
			if got != c.want {
				t.Errorf("classifyFailureReason(%v) = %q, want %q", c.err, got, c.want)
			}
			if !got.IsValid() {
				t.Errorf("classifyFailureReason produced %q which is not a valid FailureReasonCode", got)
			}
		})
	}
}

// TestClassifyFailureReason_UnknownIsLastResort — TC-SIG-140 boundary.
// A genuinely unrecognised error maps to ReasonUnknown, but ONLY then.
func TestClassifyFailureReason_UnknownIsLastResort(t *testing.T) {
	got := classifyFailureReason(errors.New("some entirely novel internal failure"))
	if got != metrics.ReasonUnknown {
		t.Errorf("classifyFailureReason(novel) = %q, want %q", got, metrics.ReasonUnknown)
	}
}

// TestRedactFailureMessage_ScrubsEmbeddedDSNButKeepsDetail —
// INV-SIGNALS-07. The surfaced message must scrub an embedded DSN
// password (the pgx connection-error shape wraps the DSN mid-string,
// which RedactDSN's prefix-only URL branch misses) while keeping the
// diagnosable detail. RED until redactFailureMessage exists.
func TestRedactFailureMessage_ScrubsEmbeddedDSNButKeepsDetail(t *testing.T) {
	raw := fmt.Errorf("connect target-A: failed to connect to `postgres://signals:SuperSecret123@db.internal:5432/app`: dial tcp: i/o timeout")
	msg := redactFailureMessage(raw)

	if msg == "" {
		t.Fatal("redactFailureMessage returned empty — a failure must never be invisible (FC-11)")
	}
	if strings.Contains(msg, "SuperSecret123") {
		t.Errorf("redacted message leaked the embedded DSN password: %q (INV-SIGNALS-07)", msg)
	}
	// The non-secret detail must survive so the failure stays diagnosable.
	if !strings.Contains(msg, "target-A") || !strings.Contains(msg, "db.internal") {
		t.Errorf("redacted message dropped diagnosable detail: %q", msg)
	}
	// The username is preserved (not a secret); only the password is masked.
	if !strings.Contains(msg, "signals:****@") {
		t.Errorf("expected masked userinfo `signals:****@`, got %q", msg)
	}
}

// TestUnknownBucketStillCarriesRedactedMessage — TC-SIG-141 / FC-11.
// A genuinely-unrecognised error maps to ReasonUnknown, yet its redacted
// message is non-empty and still scrubs any embedded credential — the
// failure is never invisible (the exact pre-fix regression).
func TestUnknownBucketStillCarriesRedactedMessage(t *testing.T) {
	raw := fmt.Errorf("novel subsystem fault referencing postgres://admin:topsecret@h/db unexpectedly")
	if got := classifyFailureReason(raw); got != metrics.ReasonUnknown {
		t.Fatalf("classifyFailureReason(novel) = %q, want %q", got, metrics.ReasonUnknown)
	}
	msg := redactFailureMessage(raw)
	if msg == "" {
		t.Fatal("unknown-bucket message is empty — a failure must never be invisible (FC-11)")
	}
	if strings.Contains(msg, "topsecret") {
		t.Errorf("unknown-bucket message leaked a password: %q (INV-SIGNALS-07)", msg)
	}
	if !strings.Contains(msg, "novel subsystem fault") {
		t.Errorf("unknown-bucket message dropped the real detail: %q", msg)
	}
}

// TestRedactFailureMessage_Caps — the message is length-capped so a
// pathological error cannot push an unbounded payload into /status.
func TestRedactFailureMessage_Caps(t *testing.T) {
	huge := errors.New(strings.Repeat("x", failureMessageMaxLen*3))
	msg := redactFailureMessage(huge)
	if len(msg) > failureMessageMaxLen+len("...[truncated]") {
		t.Errorf("redactFailureMessage did not cap length: got %d bytes", len(msg))
	}
	if !strings.HasSuffix(msg, "...[truncated]") {
		t.Errorf("capped message missing truncation marker: %q", msg[len(msg)-20:])
	}
}
