package conntest

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/elevarq/arq-signals/internal/collector"
	"github.com/elevarq/arq-signals/internal/config"
)

// ---------------------------------------------------------------------------
// R096 / signalsctl connect test — classification + report-shape unit tests.
//
// Spec:        specifications/connect-test.md
// Acceptance:  specifications/connect-test.acceptance.md (TC-CONN-01..10)
//
// Tests against synthetic errors so the classification logic is
// exercised in isolation; end-to-end connection paths run under
// //go:build integration.
// ---------------------------------------------------------------------------

// --- Classify enum coverage -------------------------------------------------

func TestClassify_NilReturnsOK(t *testing.T) {
	cat, detail := Classify(nil)
	if cat != CategoryOK {
		t.Errorf("nil error: got %q, want %q", cat, CategoryOK)
	}
	if detail != "" {
		t.Errorf("nil error detail: got %q, want empty", detail)
	}
}

func TestClassify_DNSError(t *testing.T) {
	err := &net.DNSError{Name: "nosuchhost.example", Err: "no such host"}
	cat, detail := Classify(err)
	if cat != CategoryDNS {
		t.Errorf("DNSError: got %q, want %q (detail=%q)", cat, CategoryDNS, detail)
	}
	if !strings.Contains(detail, "nosuchhost.example") {
		t.Errorf("detail should name the host: got %q", detail)
	}
}

func TestClassify_TCPRefused(t *testing.T) {
	// pgx wraps refused connects as a *net.OpError sometimes, or as
	// a plain "connection refused" string. The classifier must catch
	// both shapes.
	for _, err := range []error{
		&net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connect: connection refused")},
		errors.New("failed to connect to host=127.0.0.1: dial tcp 127.0.0.1:5432: connect: connection refused"),
	} {
		cat, detail := Classify(err)
		if cat != CategoryTCP {
			t.Errorf("TCP refused (%T): got %q, want %q (detail=%q)", err, cat, CategoryTCP, detail)
		}
	}
}

func TestClassify_TLSHandshake(t *testing.T) {
	err := errors.New("tls: handshake failure")
	cat, _ := Classify(err)
	if cat != CategoryTLS {
		t.Errorf("TLS error: got %q, want %q", cat, CategoryTLS)
	}
}

func TestClassify_AuthSQLSTATE28P01(t *testing.T) {
	err := &pgconn.PgError{Code: "28P01", Message: `password authentication failed for user "arq"`}
	cat, detail := Classify(err)
	if cat != CategoryAuth {
		t.Errorf("28P01: got %q, want %q (detail=%q)", cat, CategoryAuth, detail)
	}
	if !strings.Contains(detail, "28P01") {
		t.Errorf("detail must name the SQLSTATE; got %q", detail)
	}
}

func TestClassify_AuthSQLSTATE28000(t *testing.T) {
	err := &pgconn.PgError{Code: "28000", Message: "invalid authorization specification"}
	cat, _ := Classify(err)
	if cat != CategoryAuth {
		t.Errorf("28000: got %q, want %q", cat, CategoryAuth)
	}
}

func TestClassify_StartupDatabaseDoesNotExist(t *testing.T) {
	err := &pgconn.PgError{Code: "3D000", Message: `database "nope" does not exist`}
	cat, detail := Classify(err)
	if cat != CategoryStartup {
		t.Errorf("3D000: got %q, want %q (detail=%q)", cat, CategoryStartup, detail)
	}
}

func TestClassify_Deterministic(t *testing.T) {
	// INV-CONN-03: the same input must produce the same category +
	// the same Detail string across runs.
	err := &pgconn.PgError{Code: "28P01", Message: "password authentication failed"}
	cat1, detail1 := Classify(err)
	cat2, detail2 := Classify(err)
	if cat1 != cat2 {
		t.Errorf("non-deterministic category: %q vs %q", cat1, cat2)
	}
	if detail1 != detail2 {
		t.Errorf("non-deterministic detail: %q vs %q", detail1, detail2)
	}
}

func TestSupportedCategories_Exhaustive(t *testing.T) {
	// Belt-and-braces: any new category must be added to the
	// enumeration too (so the CLI shell renders it correctly and
	// downstream consumers can plan for it).
	want := map[Category]bool{
		CategoryOK: true, CategoryDNS: true, CategoryTCP: true, CategoryTLS: true,
		CategoryAuth: true, CategoryStartup: true, CategoryRole: true,
		CategoryPasswordResolve: true, CategoryConfig: true,
	}
	if len(SupportedCategories) != len(want) {
		t.Fatalf("SupportedCategories length: got %d, want %d", len(SupportedCategories), len(want))
	}
	for _, c := range SupportedCategories {
		if !want[c] {
			t.Errorf("unexpected category in SupportedCategories: %q", c)
		}
	}
}

// --- Wire-shape contract (TC-CONN-07) ---------------------------------------

func TestResult_JSONShape(t *testing.T) {
	r := Result{
		Target: "prod-db", Category: CategoryOK, Detail: "connected",
		Host: "prod.example.com", Port: 5432, DBName: "app",
		Username: "signals_ro", PGVersion: "16.2",
		Duration: 47 * time.Millisecond,
	}
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"target", "category", "detail", "host", "port", "dbname", "username", "duration_ms"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("Result JSON missing %q: %s", key, raw)
		}
	}
	if decoded["category"] != "ok" {
		t.Errorf("category must be the lowercase enum; got %v", decoded["category"])
	}
	if d, ok := decoded["duration_ms"].(float64); !ok || d != 47 {
		t.Errorf("duration_ms: got %v (%T), want 47 (47ms)", decoded["duration_ms"], decoded["duration_ms"])
	}
}

func TestReport_JSONShape(t *testing.T) {
	report := Report{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   "2026-05-12T12:34:56Z",
		Attempts: []Result{
			{Target: "prod-db", Category: CategoryOK, Host: "x", Port: 5432, DBName: "d", Username: "u"},
			{Target: "staging-db", Category: CategoryTCP, Detail: "dial: refused", Host: "x", Port: 5432, DBName: "d", Username: "u"},
		},
		Summary: Summary{OK: 1, Fail: 1},
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"schema_version", "generated_at", "attempts", "summary"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("Report JSON missing %q", key)
		}
	}
	attempts, ok := decoded["attempts"].([]any)
	if !ok || len(attempts) != 2 {
		t.Fatalf("attempts must be a 2-element array; got %T length %d", decoded["attempts"], len(attempts))
	}
	summary, ok := decoded["summary"].(map[string]any)
	if !ok {
		t.Fatalf("summary must be an object; got %T", decoded["summary"])
	}
	for _, key := range []string{"ok", "fail"} {
		if _, ok := summary[key]; !ok {
			t.Errorf("summary missing %q", key)
		}
	}
}

// --- TestConnection: password resolution failure (TC-CONN-04) ---------------

// Implementation will use collector.ResolvePassword; this test pins
// the contract that an unresolvable password yields category
// `password_resolve` without attempting a TCP dial.
//
// The test does NOT need an integration build tag — TestConnection
// must return before opening any socket when ResolvePassword fails.
// (Run with -race to catch any goroutine leakage in the early-return
// path.)
func TestTestConnection_PasswordResolveCategory(t *testing.T) {
	// Stub: implementation pending.
	// This test will only pass after step 3/3 of #68.
	t.Skip("pending GREEN implementation in step 3/3 of #68")
}

// --- TestConnectionWithResolver — resolver-path classification -------------

// errResolver is a CredentialResolver that always fails to resolve.
type errResolver struct{ err error }

func (e errResolver) Resolve(context.Context, config.TargetConfig) (collector.Credential, error) {
	return collector.Credential{}, e.err
}

// TestTestConnectionWithResolver_ResolveFailure verifies a resolver error
// short-circuits to CategoryPasswordResolve with a redacted detail and no
// connection attempt (ARQ-SIGNALS-CONNECT-FC002 / INV001). No DB required.
func TestTestConnectionWithResolver_ResolveFailure(t *testing.T) {
	tgt := config.TargetConfig{
		Name: "t", Host: "db.example.com", Port: 5432, DBName: "app",
		User: "signals", SSLMode: "verify-full",
		AuthMethod: config.AuthMethodAWSRDSIAM,
	}
	res := errResolver{err: errors.New("minting RDS IAM auth token failed")}
	got := TestConnectionWithResolver(context.Background(), tgt, res, Options{})
	if got.Category != CategoryPasswordResolve {
		t.Fatalf("category = %q, want password_resolve", got.Category)
	}
	if got.Detail == "" {
		t.Error("expected a redacted detail")
	}
}

// TestTestConnectionWithResolver_BuildFailure verifies a ConnConfig build
// failure (missing host) is classified as CategoryConfig, not a panic.
func TestTestConnectionWithResolver_BuildFailure(t *testing.T) {
	tgt := config.TargetConfig{Name: "t", Port: 5432, User: "u", SSLMode: "verify-full"}
	res := okPasswordResolver{}
	got := TestConnectionWithResolver(context.Background(), tgt, res, Options{})
	if got.Category != CategoryConfig {
		t.Fatalf("category = %q, want config", got.Category)
	}
}

// okPasswordResolver returns a password credential.
type okPasswordResolver struct{}

func (okPasswordResolver) Resolve(context.Context, config.TargetConfig) (collector.Credential, error) {
	return collector.Credential{Kind: collector.CredKindPassword, Password: "pw"}, nil
}
