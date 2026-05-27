package tests

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/elevarq/arq-signals/internal/collector"
	"github.com/elevarq/arq-signals/internal/config"
	"github.com/elevarq/arq-signals/internal/db"
	"github.com/elevarq/arq-signals/internal/export"
)

// ---------------------------------------------------------------------------
// TC-SIG-025 / R017 / R021: SafetyResult types and session guard
// ---------------------------------------------------------------------------

// TestSafetyResultIsSafe_NoFailures verifies that a SafetyResult with no hard
// failures reports IsSafe()=true.
// Traces: ARQ-SIGNALS-R017 / TC-SIG-025
func TestSafetyResultIsSafe_NoFailures(t *testing.T) {
	r := collector.SafetyResult{}
	if !r.IsSafe() {
		t.Error("expected IsSafe()=true for empty SafetyResult, got false")
	}
}

// TestSafetyResultIsSafe_WithFailures verifies that a SafetyResult with hard
// failures reports IsSafe()=false.
// Traces: ARQ-SIGNALS-R017 / TC-SIG-025
func TestSafetyResultIsSafe_WithFailures(t *testing.T) {
	r := collector.SafetyResult{
		HardFailures: []string{"role has superuser attribute"},
	}
	if r.IsSafe() {
		t.Error("expected IsSafe()=false when HardFailures is non-empty, got true")
	}
}

// TestSafetyResultError_Empty verifies that Error() returns an empty string
// when there are no hard failures.
// Traces: ARQ-SIGNALS-R017 / TC-SIG-025
func TestSafetyResultError_Empty(t *testing.T) {
	r := collector.SafetyResult{}
	if got := r.Error(); got != "" {
		t.Errorf("expected empty Error() for safe result, got %q", got)
	}
}

// TestSessionReadOnlyParamSet verifies that BuildConnConfig sets
// default_transaction_read_only=on in RuntimeParams.
// Traces: ARQ-SIGNALS-R021 / TC-SIG-025
func TestSessionReadOnlyParamSet(t *testing.T) {
	tgt := config.TargetConfig{
		Name:   "ro-check",
		Host:   "localhost",
		Port:   5432,
		DBName: "postgres",
		User:   "arq",
	}

	cfg, err := collector.BuildConnConfig(tgt)
	if err != nil {
		t.Fatalf("BuildConnConfig: %v", err)
	}

	val, ok := cfg.RuntimeParams["default_transaction_read_only"]
	if !ok {
		t.Fatal("default_transaction_read_only not set in RuntimeParams")
	}
	if val != "on" {
		t.Errorf("default_transaction_read_only = %q, want %q", val, "on")
	}
}

// ---------------------------------------------------------------------------
// TC-SIG-026 / R018: Superuser blocked
// ---------------------------------------------------------------------------

// TestSafetyResultSuperuserBlocked verifies that a SafetyResult with a hard
// failure for superuser reports IsSafe()=false, and Error() contains
// "superuser", "BLOCKED", and remediation guidance.
// Traces: ARQ-SIGNALS-R018 / TC-SIG-026
func TestSafetyResultSuperuserBlocked(t *testing.T) {
	r := collector.SafetyResult{
		HardFailures: []string{
			`role "postgres" has superuser attribute (rolsuper=true) — collection requires a non-superuser role`,
		},
	}

	if r.IsSafe() {
		t.Fatal("expected IsSafe()=false for superuser hard failure")
	}

	errMsg := r.Error()
	for _, want := range []string{"superuser", "BLOCKED", "rolsuper=true"} {
		if !strings.Contains(errMsg, want) {
			t.Errorf("Error() missing %q; got:\n%s", want, errMsg)
		}
	}
	// Remediation guidance.
	if !strings.Contains(errMsg, "CREATE ROLE arq_monitor") {
		t.Errorf("Error() missing remediation guidance; got:\n%s", errMsg)
	}
}

// ---------------------------------------------------------------------------
// TC-SIG-027 / R019: Replication blocked
// ---------------------------------------------------------------------------

// TestSafetyResultReplicationBlocked verifies that a SafetyResult with a hard
// failure for replication reports IsSafe()=false, and Error() mentions
// "replication" and "BLOCKED".
// Traces: ARQ-SIGNALS-R019 / TC-SIG-027
func TestSafetyResultReplicationBlocked(t *testing.T) {
	r := collector.SafetyResult{
		HardFailures: []string{
			`role "replicator" has replication attribute (rolreplication=true) — collection requires a role without replication privileges`,
		},
	}

	if r.IsSafe() {
		t.Fatal("expected IsSafe()=false for replication hard failure")
	}

	errMsg := r.Error()
	for _, want := range []string{"replication", "BLOCKED", "rolreplication=true"} {
		if !strings.Contains(errMsg, want) {
			t.Errorf("Error() missing %q; got:\n%s", want, errMsg)
		}
	}
}

// ---------------------------------------------------------------------------
// TC-SIG-028 / R020: BypassRLS blocked
// ---------------------------------------------------------------------------

// TestSafetyResultBypassRLSBlocked verifies that a SafetyResult with a hard
// failure for bypassrls reports IsSafe()=false, and Error() mentions
// "bypassrls" and "BLOCKED".
// Traces: ARQ-SIGNALS-R020 / TC-SIG-028
func TestSafetyResultBypassRLSBlocked(t *testing.T) {
	r := collector.SafetyResult{
		HardFailures: []string{
			`role "admin" has bypassrls attribute (rolbypassrls=true) — collection requires a role without BYPASSRLS`,
		},
	}

	if r.IsSafe() {
		t.Fatal("expected IsSafe()=false for bypassrls hard failure")
	}

	errMsg := r.Error()
	for _, want := range []string{"bypassrls", "BLOCKED", "rolbypassrls=true"} {
		if !strings.Contains(errMsg, want) {
			t.Errorf("Error() missing %q; got:\n%s", want, errMsg)
		}
	}
}

// ---------------------------------------------------------------------------
// TC-SIG-029 / R022: Timeout values
// ---------------------------------------------------------------------------

// TestCollectorTimeoutDefaults verifies that a new Collector has
// queryTimeout=10s and targetTimeout=60s by inspecting the accessor and
// the source code constants.
// Traces: ARQ-SIGNALS-R022 / TC-SIG-029
func TestCollectorTimeoutDefaults(t *testing.T) {
	// We cannot instantiate a Collector without a db.DB, but we can verify
	// the source constants via AST scanning.
	root := repoRoot(t)
	src := readFileString(t, filepath.Join(root, "internal", "collector", "collector.go"))

	// queryTimeout default is 10*time.Second
	if !strings.Contains(src, `queryTimeout:         10 * time.Second`) {
		t.Error("collector.go does not set default queryTimeout to 10 * time.Second")
	}
	// targetTimeout default is 60*time.Second
	if !strings.Contains(src, `targetTimeout:        60 * time.Second`) {
		t.Error("collector.go does not set default targetTimeout to 60 * time.Second")
	}
}

// TestTimeoutValuesPassedCorrectly verifies that the collector code computes
// stmtTimeoutMs from queryTimeout.Milliseconds() and uses the conservative
// lockTimeoutMs constant of 5000.
// Traces: ARQ-SIGNALS-R022 / TC-SIG-029
func TestTimeoutValuesPassedCorrectly(t *testing.T) {
	root := repoRoot(t)
	src := readFileString(t, filepath.Join(root, "internal", "collector", "collector.go"))

	if !strings.Contains(src, "c.queryTimeout.Milliseconds()") {
		t.Error("collector.go does not compute stmtTimeoutMs from queryTimeout.Milliseconds()")
	}
	if !strings.Contains(src, "lockTimeoutMs := 5000") {
		t.Error("collector.go does not define lockTimeoutMs := 5000")
	}
}

// ---------------------------------------------------------------------------
// TC-SIG-030 / R023: Hard vs soft distinction
// ---------------------------------------------------------------------------

// TestSafetyResultWarningsDoNotBlock verifies that a SafetyResult with only
// Warnings (no HardFailures) returns IsSafe()=true.
// Traces: ARQ-SIGNALS-R023 / TC-SIG-030
func TestSafetyResultWarningsDoNotBlock(t *testing.T) {
	r := collector.SafetyResult{
		Warnings: []string{"role is member of pg_write_all_data"},
	}
	if !r.IsSafe() {
		t.Error("expected IsSafe()=true when only Warnings are present")
	}
	if r.Error() != "" {
		t.Errorf("expected empty Error() when only Warnings present, got %q", r.Error())
	}
}

// TestSafetyResultMixedFailuresAndWarnings verifies that IsSafe()=false when
// both HardFailures and Warnings are present, and Error() only mentions
// the hard failures (not warnings).
// Traces: ARQ-SIGNALS-R023 / TC-SIG-030
func TestSafetyResultMixedFailuresAndWarnings(t *testing.T) {
	r := collector.SafetyResult{
		HardFailures: []string{`role "admin" has superuser attribute (rolsuper=true)`},
		Warnings:     []string{"role is member of pg_write_all_data"},
	}

	if r.IsSafe() {
		t.Fatal("expected IsSafe()=false with hard failures present")
	}

	errMsg := r.Error()
	if !strings.Contains(errMsg, "superuser") {
		t.Errorf("Error() should mention hard failure; got:\n%s", errMsg)
	}
	if strings.Contains(errMsg, "pg_write_all_data") {
		t.Errorf("Error() should not mention warnings; got:\n%s", errMsg)
	}
}

// ---------------------------------------------------------------------------
// TC-SIG-031 / R024: Credential redaction
// ---------------------------------------------------------------------------

// TestRedactDSNPassword verifies that RedactDSN replaces the password in a
// key=value connection string.
// Traces: ARQ-SIGNALS-R024 / TC-SIG-031
func TestRedactDSNPassword(t *testing.T) {
	got := collector.RedactDSN("host=x password=secret user=y")
	want := "host=x password=**** user=y"
	if got != want {
		t.Errorf("RedactDSN key=value: got %q, want %q", got, want)
	}
}

// TestRedactDSNURL verifies that RedactDSN replaces the password in a
// postgres:// URL connection string.
// Traces: ARQ-SIGNALS-R024 / TC-SIG-031
func TestRedactDSNURL(t *testing.T) {
	got := collector.RedactDSN("postgres://user:secret@host/db")
	want := "postgres://user:****@host/db"
	if got != want {
		t.Errorf("RedactDSN URL: got %q, want %q", got, want)
	}
}

// TestRedactErrorContainingPassword verifies that redactError (unexported)
// sanitizes errors containing "password". We verify this through the exported
// BuildConnConfig error path when a password_file does not exist: the error
// should NOT contain the raw path when the error message matches the
// redaction pattern. Instead, we verify via source scanning that redactError
// exists and checks for "password".
// Traces: ARQ-SIGNALS-R024 / TC-SIG-031
func TestRedactErrorContainingPassword(t *testing.T) {
	root := repoRoot(t)
	src := readFileString(t, filepath.Join(root, "internal", "collector", "secrets.go"))

	// Verify redactError function exists and handles "password" keyword.
	if !strings.Contains(src, "func redactError(err error) error") {
		t.Fatal("secrets.go does not define redactError function")
	}
	if !strings.Contains(src, `strings.Contains(msg, "password")`) {
		t.Error("redactError does not check for 'password' in error message")
	}
	if !strings.Contains(src, "credential resolution failed") {
		t.Error("redactError does not return generic redacted message")
	}
}

// TestRedactErrorSafe verifies that redactError returns the original error
// for non-password errors, by scanning the source for the fallback path.
// Traces: ARQ-SIGNALS-R024 / TC-SIG-031
func TestRedactErrorSafe(t *testing.T) {
	root := repoRoot(t)
	src := readFileString(t, filepath.Join(root, "internal", "collector", "secrets.go"))

	// Verify the function returns the original error when no secret keywords found.
	// The function ends with "return err" as the fallback.
	lines := strings.Split(src, "\n")
	foundReturnErr := false
	inRedactError := false
	braceDepth := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "func redactError") {
			inRedactError = true
			braceDepth = 0
		}
		if inRedactError {
			braceDepth += strings.Count(trimmed, "{") - strings.Count(trimmed, "}")
			if trimmed == "return err" {
				foundReturnErr = true
			}
			if braceDepth == 0 && inRedactError && strings.Contains(trimmed, "}") {
				break
			}
		}
	}
	if !foundReturnErr {
		t.Error("redactError does not have a 'return err' fallback for safe errors")
	}
}

// ---------------------------------------------------------------------------
// TC-SIG-032 / R025: Actionable error messages
// ---------------------------------------------------------------------------

// TestSafetyResultErrorContainsRemediation verifies that Error() includes
// remediation guidance with CREATE ROLE and GRANT pg_monitor.
// Traces: ARQ-SIGNALS-R025 / TC-SIG-032
func TestSafetyResultErrorContainsRemediation(t *testing.T) {
	r := collector.SafetyResult{
		HardFailures: []string{"role has superuser attribute"},
	}

	errMsg := r.Error()
	if !strings.Contains(errMsg, "CREATE ROLE arq_monitor") {
		t.Errorf("Error() missing 'CREATE ROLE arq_monitor'; got:\n%s", errMsg)
	}
	if !strings.Contains(errMsg, "GRANT pg_monitor") {
		t.Errorf("Error() missing 'GRANT pg_monitor'; got:\n%s", errMsg)
	}
}

// TestSafetyResultErrorContainsAttributeInfo verifies that Error() includes
// the specific attribute name and value from the hard failure.
// Traces: ARQ-SIGNALS-R025 / TC-SIG-032
func TestSafetyResultErrorContainsAttributeInfo(t *testing.T) {
	r := collector.SafetyResult{
		HardFailures: []string{
			`role "pg" has superuser attribute (rolsuper=true)`,
		},
	}

	errMsg := r.Error()
	if !strings.Contains(errMsg, "rolsuper=true") {
		t.Errorf("Error() should contain attribute info 'rolsuper=true'; got:\n%s", errMsg)
	}
	if !strings.Contains(errMsg, "superuser") {
		t.Errorf("Error() should contain 'superuser'; got:\n%s", errMsg)
	}
}

// ---------------------------------------------------------------------------
// TC-SIG-033 / R026: Unsafe override
// ---------------------------------------------------------------------------

// TestAllowUnsafeRoleOption verifies that WithAllowUnsafeRole(true) sets the
// internal flag, accessible via GetAllowUnsafeRole().
// Traces: ARQ-SIGNALS-R026 / TC-SIG-033
func TestAllowUnsafeRoleOption(t *testing.T) {
	store := openTestDB(t)
	c := collector.New(store, nil, 0, 0, collector.WithAllowUnsafeRole(true))
	if !c.GetAllowUnsafeRole() {
		t.Error("expected GetAllowUnsafeRole()=true after WithAllowUnsafeRole(true)")
	}
}

// TestAllowUnsafeRoleDefaultFalse verifies that the default collector has
// GetAllowUnsafeRole()=false.
// Traces: ARQ-SIGNALS-R026 / TC-SIG-033
func TestAllowUnsafeRoleDefaultFalse(t *testing.T) {
	store := openTestDB(t)
	c := collector.New(store, nil, 0, 0)
	if c.GetAllowUnsafeRole() {
		t.Error("expected GetAllowUnsafeRole()=false by default")
	}
}

// TestConfigAllowUnsafeRoleField verifies that config.Config has the
// AllowUnsafeRole field.
// Traces: ARQ-SIGNALS-R026 / TC-SIG-033
func TestConfigAllowUnsafeRoleField(t *testing.T) {
	cfgType := reflect.TypeOf(config.Config{})
	field, found := cfgType.FieldByName("AllowUnsafeRole")
	if !found {
		t.Fatal("config.Config is missing AllowUnsafeRole field")
	}
	if field.Type.Kind() != reflect.Bool {
		t.Errorf("AllowUnsafeRole should be bool, got %s", field.Type.Kind())
	}
}

// ---------------------------------------------------------------------------
// TC-SIG-034 / R026: Default blocks
// ---------------------------------------------------------------------------

// TestDefaultBehaviorIsBlocking verifies that the default collector blocks
// unsafe roles (GetAllowUnsafeRole()=false).
// Traces: ARQ-SIGNALS-R026 / TC-SIG-034
func TestDefaultBehaviorIsBlocking(t *testing.T) {
	store := openTestDB(t)
	c := collector.New(store, nil, 0, 0)
	if c.GetAllowUnsafeRole() {
		t.Error("default collector should block unsafe roles (GetAllowUnsafeRole()=false)")
	}
}

// ---------------------------------------------------------------------------
// TC-SIG-035: Multiple attributes
// ---------------------------------------------------------------------------

// TestSafetyResultMultipleFailures verifies that a SafetyResult with multiple
// hard failures lists all of them in Error().
// Traces: TC-SIG-035
func TestSafetyResultMultipleFailures(t *testing.T) {
	r := collector.SafetyResult{
		HardFailures: []string{
			`role "admin" has superuser attribute (rolsuper=true)`,
			`role "admin" has replication attribute (rolreplication=true)`,
			`role "admin" has bypassrls attribute (rolbypassrls=true)`,
		},
	}

	if r.IsSafe() {
		t.Fatal("expected IsSafe()=false with multiple hard failures")
	}

	errMsg := r.Error()
	for _, want := range []string{"rolsuper=true", "rolreplication=true", "rolbypassrls=true"} {
		if !strings.Contains(errMsg, want) {
			t.Errorf("Error() missing %q; got:\n%s", want, errMsg)
		}
	}

	// Verify each failure is prefixed with BLOCKED.
	blockedCount := strings.Count(errMsg, "BLOCKED")
	if blockedCount != 3 {
		t.Errorf("expected 3 BLOCKED markers, got %d; output:\n%s", blockedCount, errMsg)
	}
}

// ---------------------------------------------------------------------------
// Unsafe mode in export metadata
// ---------------------------------------------------------------------------

// TestExportUnsafeModeFalseByDefault verifies that a default export builder
// writes unsafe_mode=false in metadata.json.
// Traces: ARQ-SIGNALS-R026 / TC-SIG-033
func TestExportUnsafeModeFalseByDefault(t *testing.T) {
	store := openTestDB(t)
	builder := export.NewBuilder(store, "test-instance")
	var buf bytes.Buffer
	if err := builder.WriteTo(&buf, export.Options{}); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	meta := readZipMetadata(t, buf.Bytes())
	unsafeMode, ok := meta["unsafe_mode"].(bool)
	if !ok {
		t.Fatal("metadata.json missing unsafe_mode field")
	}
	if unsafeMode {
		t.Error("expected unsafe_mode=false by default")
	}
}

// TestExportUnsafeModeTrue verifies that SetUnsafeMode writes
// unsafe_mode=true and unsafe_reasons in metadata.json.
// Traces: ARQ-SIGNALS-R026 / TC-SIG-033
func TestExportUnsafeModeTrue(t *testing.T) {
	store := openTestDB(t)
	builder := export.NewBuilder(store, "test-instance")
	reasons := []string{"rolsuper=true bypassed", "rolreplication=true bypassed"}
	builder.SetUnsafeMode(func() []string { return reasons })

	var buf bytes.Buffer
	if err := builder.WriteTo(&buf, export.Options{}); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	meta := readZipMetadata(t, buf.Bytes())

	unsafeMode, ok := meta["unsafe_mode"].(bool)
	if !ok {
		t.Fatal("metadata.json missing unsafe_mode field")
	}
	if !unsafeMode {
		t.Error("expected unsafe_mode=true after SetUnsafeMode")
	}

	rawReasons, ok := meta["unsafe_reasons"].([]any)
	if !ok {
		t.Fatal("metadata.json missing unsafe_reasons field")
	}
	if len(rawReasons) != 2 {
		t.Errorf("expected 2 unsafe_reasons, got %d", len(rawReasons))
	}
	for i, want := range reasons {
		if i < len(rawReasons) {
			if got, ok := rawReasons[i].(string); !ok || got != want {
				t.Errorf("unsafe_reasons[%d] = %q, want %q", i, got, want)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Config env var parsing
// ---------------------------------------------------------------------------

// TestConfigAllowUnsafeRoleEnvVar verifies that setting
// ARQ_SIGNALS_ALLOW_UNSAFE_ROLE=true causes config.Load to set AllowUnsafeRole=true.
// Traces: ARQ-SIGNALS-R026 / TC-SIG-033
func TestConfigAllowUnsafeRoleEnvVar(t *testing.T) {
	// Create a minimal config file so Load does not try to open /etc/arq/signals.yaml.
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "signals.yaml")
	if err := os.WriteFile(cfgPath, []byte("env: dev\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("ARQ_SIGNALS_ALLOW_UNSAFE_ROLE", "true")

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if !cfg.AllowUnsafeRole {
		t.Error("expected AllowUnsafeRole=true after setting ARQ_SIGNALS_ALLOW_UNSAFE_ROLE=true")
	}
}

// ---------------------------------------------------------------------------
// Collector code verification (AST / source scan)
// ---------------------------------------------------------------------------

// TestCollectorCallsValidateRoleSafety scans collector.go for a call to
// ValidateRoleSafety.
// Traces: ARQ-SIGNALS-R017 / TC-SIG-025
func TestCollectorCallsValidateRoleSafety(t *testing.T) {
	root := repoRoot(t)
	assertFuncCalledInFile(t,
		filepath.Join(root, "internal", "collector", "collector.go"),
		"ValidateRoleSafety",
	)
}

// TestCollectorVerifiesReadOnlyInline verifies that collector.go checks
// default_transaction_read_only on the acquired connection inline.
// Traces: ARQ-SIGNALS-R021 / TC-SIG-025
func TestCollectorVerifiesReadOnlyInline(t *testing.T) {
	root := repoRoot(t)
	src := readFileString(t, filepath.Join(root, "internal", "collector", "collector.go"))
	if !strings.Contains(src, `SHOW default_transaction_read_only`) {
		t.Error("collector.go does not verify default_transaction_read_only on the acquired connection")
	}
}

// TestCollectorSetsTimeoutsViaSetLocal verifies that collector.go uses
// SET LOCAL to apply timeouts inside the collection transaction,
// guaranteeing they apply to the same connection.
// Traces: ARQ-SIGNALS-R022 / TC-SIG-029
func TestCollectorSetsTimeoutsViaSetLocal(t *testing.T) {
	root := repoRoot(t)
	src := readFileString(t, filepath.Join(root, "internal", "collector", "collector.go"))
	if !strings.Contains(src, "SET LOCAL") {
		t.Error("collector.go does not use SET LOCAL for transaction-scoped timeouts")
	}
	// Verify all three timeout params are referenced
	for _, param := range []string{"statement_timeout", "lock_timeout", "idle_in_transaction_session_timeout"} {
		if !strings.Contains(src, param) {
			t.Errorf("collector.go does not reference timeout param %q", param)
		}
	}
}

// TestCollectorLockTimeout5000 scans collector.go for the conservative lock
// timeout constant of 5000 ms.
// Traces: ARQ-SIGNALS-R022 / TC-SIG-029
func TestCollectorLockTimeout5000(t *testing.T) {
	root := repoRoot(t)
	src := readFileString(t, filepath.Join(root, "internal", "collector", "collector.go"))

	if !strings.Contains(src, "lockTimeoutMs := 5000") {
		t.Error("collector.go does not contain 'lockTimeoutMs := 5000'")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// readFileString reads a file and returns its contents as a string.
func readFileString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	return string(data)
}

// readZipMetadata opens a ZIP from raw bytes and decodes metadata.json.
func readZipMetadata(t *testing.T, zipData []byte) map[string]any {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	for _, f := range zr.File {
		if f.Name == "metadata.json" {
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("open metadata.json: %v", err)
			}
			defer rc.Close()
			var m map[string]any
			if err := json.NewDecoder(rc).Decode(&m); err != nil {
				t.Fatalf("decode metadata.json: %v", err)
			}
			return m
		}
	}
	t.Fatal("metadata.json not found in ZIP")
	return nil
}

// assertFuncCalledInFile parses a Go source file's AST and asserts that a
// function with the given name is called at least once.
func assertFuncCalledInFile(t *testing.T, filePath, funcName string) {
	t.Helper()

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filePath, nil, parser.AllErrors)
	if err != nil {
		t.Fatalf("parse %s: %v", filePath, err)
	}

	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			if fn.Name == funcName {
				found = true
			}
		case *ast.SelectorExpr:
			if fn.Sel.Name == funcName {
				found = true
			}
		}
		return !found
	})

	if !found {
		t.Errorf("expected call to %s in %s, but none found", funcName, filepath.Base(filePath))
	}
}

// Ensure imports are used. These variables are only here to satisfy the
// compiler for packages referenced in the test logic above.
var (
	_ = fmt.Sprintf
	_ db.DB
	_ = reflect.TypeOf
)
