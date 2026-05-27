package doctor

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/elevarq/arq-signals/internal/config"
)

// ---------------------------------------------------------------------------
// R095 / arqctl doctor — per-check unit tests.
//
// Spec:        specifications/doctor.md
// Acceptance:  specifications/doctor.acceptance.md (TC-DOC-01..06)
// ---------------------------------------------------------------------------

// --- C1 config_valid --------------------------------------------------------

func TestCheckConfigValid_OKOnGoodConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	contents := `signals:
  store_path: /tmp/arq-signals
  poll_interval: 5m
  min_snapshot_interval: 60s
targets:
  - name: t1
    host: 127.0.0.1
    port: 5432
    dbname: postgres
    user: arq
    sslmode: disable
`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write test config: %v", err)
	}

	r := CheckConfigValid(path)
	if r.Status != StatusOK {
		t.Errorf("status: got %q, want %q (detail=%q)", r.Status, StatusOK, r.Detail)
	}
	if r.ID != "C1" || r.Name != "config_valid" {
		t.Errorf("unexpected ID/Name: %+v", r)
	}
}

func TestCheckConfigValid_FailOnMissingFile(t *testing.T) {
	r := CheckConfigValid("/definitely/does/not/exist/config.yaml")
	if r.Status != StatusFail {
		t.Errorf("status: got %q, want %q (detail=%q)", r.Status, StatusFail, r.Detail)
	}
	if !strings.Contains(r.Detail, "/definitely/does/not/exist/config.yaml") {
		t.Errorf("detail must mention the offending path; got %q", r.Detail)
	}
}

func TestCheckConfigValid_FailOnUnparseable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.yaml")
	if err := os.WriteFile(path, []byte("not: [valid yaml\n  - missing-bracket"), 0o600); err != nil {
		t.Fatalf("write broken config: %v", err)
	}
	r := CheckConfigValid(path)
	if r.Status != StatusFail {
		t.Errorf("status: got %q, want %q", r.Status, StatusFail)
	}
}

// --- C2 store_writable ------------------------------------------------------

func TestCheckStoreWritable_OKOnWritableDir(t *testing.T) {
	dir := t.TempDir()
	r := CheckStoreWritable(dir)
	if r.Status != StatusOK {
		t.Errorf("status: got %q, want %q (detail=%q)", r.Status, StatusOK, r.Detail)
	}
}

func TestCheckStoreWritable_FailOnNonexistent(t *testing.T) {
	r := CheckStoreWritable("/nonexistent/store/path/9j2k")
	if r.Status != StatusFail {
		t.Errorf("status: got %q, want %q", r.Status, StatusFail)
	}
}

func TestCheckStoreWritable_FailOnFileLikePathReportsParentDir(t *testing.T) {
	// When the configured store path looks file-like (has an
	// extension) but does not exist, the failure detail must point
	// at the *parent* directory — that's where the daemon would
	// have tried to create the file. The previous behaviour reported
	// the full file path, masking the actually-missing directory.
	path := "/nonexistent/parent/path/arq-signals.db"
	r := CheckStoreWritable(path)
	if r.Status != StatusFail {
		t.Errorf("status: got %q, want %q (detail=%q)", r.Status, StatusFail, r.Detail)
	}
	if !strings.Contains(r.Detail, "/nonexistent/parent/path") {
		t.Errorf("detail should reference parent dir, not the file path; got %q", r.Detail)
	}
	if strings.Contains(r.Detail, "arq-signals.db") {
		t.Errorf("detail must not name the missing file itself (operator should look at parent); got %q", r.Detail)
	}
}

func TestCheckStoreWritable_FailOnReadOnly(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root — read-only check is unreliable")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	r := CheckStoreWritable(dir)
	if r.Status != StatusFail {
		t.Errorf("status: got %q, want %q (detail=%q)", r.Status, StatusFail, r.Detail)
	}
}

func TestCheckStoreWritable_LeavesNoProbeFile(t *testing.T) {
	// INV-DOC-02 — the write probe must be removed immediately.
	dir := t.TempDir()
	r := CheckStoreWritable(dir)
	if r.Status != StatusOK {
		t.Fatalf("precondition: status %q (detail=%q)", r.Status, r.Detail)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("store dir should be empty after C2; found: %v", names)
	}
}

// --- C3 target_reachable ----------------------------------------------------

func TestCheckTargetReachable_OKOnListening(t *testing.T) {
	// Stand up a local TCP listener and ask doctor to dial it.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	host, portStr, _ := net.SplitHostPort(l.Addr().String())
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse port %q: %v", portStr, err)
	}

	tgt := config.TargetConfig{Name: "live", Host: host, Port: port, Enabled: true}
	r := CheckTargetReachable(context.Background(), tgt)
	if r.Status != StatusOK {
		t.Errorf("status: got %q, want %q (detail=%q)", r.Status, StatusOK, r.Detail)
	}
	if r.Target != "live" {
		t.Errorf("target: got %q, want %q", r.Target, "live")
	}
}

func TestCheckTargetReachable_FailOnRefused(t *testing.T) {
	// Bind+close to grab a port nothing is listening on. Race window
	// is theoretically open but vanishingly small for a test.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	l.Close()
	host, portStr, _ := net.SplitHostPort(addr)
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse port %q: %v", portStr, err)
	}

	tgt := config.TargetConfig{Name: "dead", Host: host, Port: port, Enabled: true}
	r := CheckTargetReachable(context.Background(), tgt)
	if r.Status != StatusFail {
		t.Errorf("status: got %q, want %q (detail=%q)", r.Status, StatusFail, r.Detail)
	}
}

func TestCheckTargetReachable_WarnOnDisabled(t *testing.T) {
	tgt := config.TargetConfig{Name: "off", Host: "127.0.0.1", Port: 5432, Enabled: false}
	r := CheckTargetReachable(context.Background(), tgt)
	if r.Status != StatusWarn {
		t.Errorf("disabled target should yield WARN, got %q", r.Status)
	}
	if !strings.Contains(strings.ToLower(r.Detail), "disabled") {
		t.Errorf("detail should mention disabled; got %q", r.Detail)
	}
}

// --- C4 role_safe -----------------------------------------------------------

func TestCheckRoleSafe_FailOnPasswordResolutionError(t *testing.T) {
	// Misconfigured password_env (env var not set) must surface as a
	// clear C4 failure with the root cause in Detail, not as a
	// downstream "authentication failed" after the dial succeeds.
	tgt := config.TargetConfig{
		Name:        "auth-broken",
		Host:        "127.0.0.1",
		Port:        5432,
		DBName:      "postgres",
		User:        "arq",
		SSLMode:     "disable",
		PasswordEnv: "ARQ_DOCTOR_TEST_PASSWORD_DOES_NOT_EXIST",
		Enabled:     true,
	}
	r := CheckRoleSafe(context.Background(), tgt, true /* reachable */)
	if r.Status != StatusFail {
		t.Errorf("status: got %q, want %q (detail=%q)", r.Status, StatusFail, r.Detail)
	}
	if !strings.Contains(r.Detail, "resolve password") {
		t.Errorf("detail must name the root cause; got %q", r.Detail)
	}
}

func TestCheckRoleSafe_PasswordResolutionDetailContainsNoSensitiveValue(t *testing.T) {
	// INV-DOC-03 — even on the password-resolution failure path, no
	// sensitive value may appear in Detail. The env-var name itself
	// is acceptable (it's in config), but any echoed password value
	// (which there isn't here because the var isn't set) and any
	// generic "secret"/"password" keyword that might have been pulled
	// from the underlying error message must be sanitised by
	// collector.RedactError.
	const envName = "ARQ_DOCTOR_TEST_REDACT_PROBE"
	t.Setenv(envName, "should-never-appear-in-detail")
	tgt := config.TargetConfig{
		Name:        "redact-probe",
		Host:        "127.0.0.1",
		Port:        5432,
		DBName:      "postgres",
		User:        "arq",
		SSLMode:     "disable",
		PasswordEnv: envName,
		Enabled:     true,
	}
	r := CheckRoleSafe(context.Background(), tgt, true)
	if strings.Contains(r.Detail, "should-never-appear-in-detail") {
		t.Errorf("detail leaked the password VALUE: %q", r.Detail)
	}
}

// TC-DOC-06 (broader): credentials must never appear in Detail under
// any C4 failure mode. PR #63 covered the password-resolution path;
// this test exercises the dial-failure path where the DSN flows
// through pgxpool's error formatter, which historically has been a
// leak risk in this kind of tooling.
func TestCheckRoleSafe_DialFailDetailHasNoCredentialValue(t *testing.T) {
	const sentinel = "SUPER-secret-sentinel-doctor-test"
	const envName = "ARQ_DOCTOR_TEST_DIAL_LEAK_PROBE"
	t.Setenv(envName, sentinel)

	// Point at a port nothing is listening on. With reachable=true
	// the dial path runs and pgxpool returns a connection error.
	// We assert the sentinel password VALUE doesn't appear anywhere
	// in the resulting Detail string.
	tgt := config.TargetConfig{
		Name:        "leak-probe",
		Host:        "127.0.0.1",
		Port:        9, // discard port — refuses or times out
		DBName:      "postgres",
		User:        "arq",
		SSLMode:     "disable",
		PasswordEnv: envName,
		Enabled:     true,
	}
	r := CheckRoleSafe(context.Background(), tgt, true /* reachable */)
	if r.Status != StatusFail {
		t.Fatalf("status: got %q, want %q (detail=%q)", r.Status, StatusFail, r.Detail)
	}
	if strings.Contains(r.Detail, sentinel) {
		t.Errorf("Detail leaked the password VALUE on the dial-failure path: %q", r.Detail)
	}
}

func TestCheckRoleSafe_WarnWhenUpstreamFailed(t *testing.T) {
	// INV-DOC-04 — when target_reachable failed, role_safe MUST emit
	// WARN with a hint rather than attempting a connection.
	tgt := config.TargetConfig{Name: "down", Host: "127.0.0.1", Port: 65530, Enabled: true}
	r := CheckRoleSafe(context.Background(), tgt, false /* reachable */)
	if r.Status != StatusWarn {
		t.Errorf("status: got %q, want %q (detail=%q)", r.Status, StatusWarn, r.Detail)
	}
	if !strings.Contains(strings.ToLower(r.Detail), "skipped") {
		t.Errorf("detail must indicate skip reason; got %q", r.Detail)
	}
}

// --- SupportedCheckIDs ------------------------------------------------------

func TestSupportedCheckIDs_StableSet(t *testing.T) {
	want := map[string]bool{"C1": true, "C2": true, "C3": true, "C4": true, "C5": true, "C6": true}
	if len(SupportedCheckIDs) != len(want) {
		t.Fatalf("SupportedCheckIDs length: got %d, want %d", len(SupportedCheckIDs), len(want))
	}
	for _, id := range SupportedCheckIDs {
		if !want[id] {
			t.Errorf("unexpected check id in SupportedCheckIDs: %q", id)
		}
	}
}

// --- C5 collector_prerequisites --------------------------------------------

func TestCheckCollectorPrerequisites_WarnWhenUpstreamFailed(t *testing.T) {
	// FC-DOC-06 / TC-DOC-09: when reachable=false (C3 or C4 failed),
	// C5 must emit WARN with a dependency reason — not attempt to
	// connect.
	tgt := config.TargetConfig{Name: "down", Host: "127.0.0.1", Port: 65530, Enabled: true}
	r := CheckCollectorPrerequisites(context.Background(), tgt, false /* reachable */, false)
	if r.Status != StatusWarn {
		t.Errorf("status: got %q, want %q (detail=%q)", r.Status, StatusWarn, r.Detail)
	}
	if !strings.Contains(strings.ToLower(r.Detail), "skipped") {
		t.Errorf("detail must mention skip; got %q", r.Detail)
	}
}

// --- C6 snapshot_freshness -------------------------------------------------

func TestCheckSnapshotFreshness_WarnWhenStoreUnreadable(t *testing.T) {
	// FC-DOC-07 / TC-DOC-12: missing store file is WARN, not FAIL.
	// Pre-daemon doctor runs are a valid use case.
	tgt := config.TargetConfig{Name: "any", Enabled: true}
	r := CheckSnapshotFreshness("/nonexistent/store/path.db", tgt, 60*time.Second)
	if r.Status != StatusWarn {
		t.Errorf("status: got %q, want %q (detail=%q)", r.Status, StatusWarn, r.Detail)
	}
	if !strings.Contains(r.Detail, "store unreadable") {
		t.Errorf("detail must begin with the documented prefix; got %q", r.Detail)
	}
}

// --- C5 / C6 dependency wiring in Run --------------------------------------

func TestNormalizeCheckSelection_AutoAddsC3AndC4WhenC5Requested(t *testing.T) {
	out, err := normalizeCheckSelection([]string{"C5"})
	if err != nil {
		t.Fatalf("normalizeCheckSelection: %v", err)
	}
	if !out["C5"] {
		t.Error("explicit C5 must be preserved")
	}
	if !out["C4"] {
		t.Error("C5 must auto-add C4 (role_safe gates the pool open)")
	}
	if !out["C3"] {
		t.Error("C5 must auto-add C3 (target_reachable gates the dial)")
	}
}

func TestNormalizeCheckSelection_C6NoAutoDependencies(t *testing.T) {
	// C6 reads the daemon's SQLite store, not the target. It must
	// NOT pull other checks into the selection.
	out, err := normalizeCheckSelection([]string{"C6"})
	if err != nil {
		t.Fatalf("normalizeCheckSelection: %v", err)
	}
	if !out["C6"] {
		t.Error("explicit C6 must be preserved")
	}
	for _, dep := range []string{"C3", "C4", "C5"} {
		if out[dep] {
			t.Errorf("C6 must NOT auto-add %s", dep)
		}
	}
}

// --- JSON wire shape (TC-DOC-04) -------------------------------------------

// TestReport_JSONShape verifies the documented JSON contract for
// `arqctl doctor --json` output: top-level keys present, per-check
// shape correct, summary triple intact. Pins the wire shape so a
// renamed MarshalJSON field or accidentally exported `Duration`
// instead of `duration_ms` would surface immediately.
//
// Closes the TC-DOC-04 coverage gap from specifications/doctor.acceptance.md.
func TestReport_JSONShape(t *testing.T) {
	report := Report{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   "2026-05-12T12:34:56Z",
		Checks: []CheckResult{
			{ID: "C1", Name: "config_valid", Status: StatusOK, Detail: "ok", Duration: 1500000}, // 1.5ms
			{ID: "C3", Name: "target_reachable", Target: "prod", Status: StatusFail, Detail: "refused", Duration: 3000000000},
		},
		Summary: Summary{OK: 1, Warn: 0, Fail: 1},
	}

	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Top-level required keys.
	for _, key := range []string{"schema_version", "generated_at", "checks", "summary"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("top-level missing %q in JSON: %s", key, raw)
		}
	}
	if decoded["schema_version"] != SchemaVersion {
		t.Errorf("schema_version: got %v, want %q", decoded["schema_version"], SchemaVersion)
	}

	// Per-check shape.
	checks, ok := decoded["checks"].([]any)
	if !ok {
		t.Fatalf("checks must be a JSON array, got %T", decoded["checks"])
	}
	if len(checks) != 2 {
		t.Fatalf("checks length: got %d, want 2", len(checks))
	}
	first, ok := checks[0].(map[string]any)
	if !ok {
		t.Fatalf("check[0] must be a JSON object, got %T", checks[0])
	}
	for _, key := range []string{"id", "name", "target", "status", "detail", "duration_ms"} {
		if _, ok := first[key]; !ok {
			t.Errorf("check[0] missing %q (full row: %+v)", key, first)
		}
	}
	// Status must be the lowercase string enum, not the Go Status type.
	if status, _ := first["status"].(string); status != "ok" {
		t.Errorf("check[0].status: got %v, want %q (lowercase enum)", first["status"], "ok")
	}
	// duration_ms must be a number, not the runtime time.Duration ns value.
	if d, ok := first["duration_ms"].(float64); !ok || d != 1 {
		t.Errorf("check[0].duration_ms: got %v (%T), want 1 (1500000 ns -> 1 ms)", first["duration_ms"], first["duration_ms"])
	}

	// Summary triple.
	summary, ok := decoded["summary"].(map[string]any)
	if !ok {
		t.Fatalf("summary must be a JSON object, got %T", decoded["summary"])
	}
	for _, key := range []string{"ok", "warn", "fail"} {
		v, present := summary[key]
		if !present {
			t.Errorf("summary missing %q", key)
		}
		if _, isNumber := v.(float64); !isNumber {
			t.Errorf("summary.%s: got %v (%T), want number", key, v, v)
		}
	}
}

// --- Run aggregation -------------------------------------------------------

func TestNormalizeCheckSelection_AutoAddsC3WhenC4Requested(t *testing.T) {
	// C4 depends on C3. Selecting C4 alone must auto-promote C3 so
	// the operator doesn't end up with a fleet of WARN rows.
	out, err := normalizeCheckSelection([]string{"C4"})
	if err != nil {
		t.Fatalf("normalizeCheckSelection: %v", err)
	}
	if !out["C3"] {
		t.Error("selecting C4 must auto-add C3 to the selection")
	}
	if !out["C4"] {
		t.Error("explicit C4 selection must be preserved")
	}
	if out["C1"] || out["C2"] {
		t.Errorf("non-dependent checks must NOT be auto-added; got %+v", out)
	}
}

func TestNormalizeCheckSelection_DoesNotAutoAddWhenC4Absent(t *testing.T) {
	out, err := normalizeCheckSelection([]string{"C3"})
	if err != nil {
		t.Fatalf("normalizeCheckSelection: %v", err)
	}
	if out["C4"] {
		t.Error("selecting C3 alone must NOT pull C4 into the selection")
	}
}

func TestRun_UnknownCheckIDReturnsError(t *testing.T) {
	// FC-DOC-05 / FC-13 — unknown --check IDs fail at parse time
	// before any check runs.
	_, err := Run(context.Background(), "/tmp/whatever.yaml", []string{"C9"})
	if err == nil {
		t.Fatal("expected error for unknown check ID, got nil")
	}
	if !strings.Contains(err.Error(), "C9") {
		t.Errorf("error must name the offending ID; got %q", err.Error())
	}
}

func TestRun_AllChecksRunOnEmptySelection(t *testing.T) {
	// Empty selectedIDs runs every supported check.
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	_ = os.WriteFile(cfgPath, []byte("signals:\n  store_path: /tmp/x\ntargets: []\n"), 0o600)

	report, err := Run(context.Background(), cfgPath, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.SchemaVersion != SchemaVersion {
		t.Errorf("schema_version: got %q, want %q", report.SchemaVersion, SchemaVersion)
	}
	// With zero targets, C3/C4 are absent. C1 and C2 always run.
	gotIDs := map[string]bool{}
	for _, c := range report.Checks {
		gotIDs[c.ID] = true
	}
	for _, mustHave := range []string{"C1", "C2"} {
		if !gotIDs[mustHave] {
			t.Errorf("report missing check %s", mustHave)
		}
	}
}

func TestRun_PerTargetResultsAreInConfigOrder(t *testing.T) {
	// Per-target checks now run in parallel (issue #54). Verify the
	// emitted CheckResult order still matches the config-declared
	// target order, regardless of which goroutine finishes first.
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	contents := `signals:
  poll_interval: 5m
  min_snapshot_interval: 60s
targets:
  - name: alpha
    host: 127.0.0.1
    port: 9
    dbname: postgres
    user: arq
    sslmode: disable
  - name: bravo
    host: 127.0.0.1
    port: 9
    dbname: postgres
    user: arq
    sslmode: disable
  - name: charlie
    host: 127.0.0.1
    port: 9
    dbname: postgres
    user: arq
    sslmode: disable
`
	if err := os.WriteFile(cfgPath, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	report, err := Run(context.Background(), cfgPath, []string{"C3"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	wantOrder := []string{"alpha", "bravo", "charlie"}
	var gotOrder []string
	for _, c := range report.Checks {
		if c.ID == "C3" {
			gotOrder = append(gotOrder, c.Target)
		}
	}
	if len(gotOrder) != len(wantOrder) {
		t.Fatalf("got %d C3 results, want %d", len(gotOrder), len(wantOrder))
	}
	for i := range wantOrder {
		if gotOrder[i] != wantOrder[i] {
			t.Errorf("C3 result %d: got target %q, want %q (full order: %v)", i, gotOrder[i], wantOrder[i], gotOrder)
		}
	}
}

func TestRun_SummaryReflectsCheckOutcomes(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	_ = os.WriteFile(cfgPath, []byte("not: [valid yaml\n"), 0o600)

	report, err := Run(context.Background(), cfgPath, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Summary.Fail == 0 {
		t.Errorf("broken config should produce at least one FAIL; summary=%+v", report.Summary)
	}
	if report.Summary.OK+report.Summary.Warn+report.Summary.Fail != len(report.Checks) {
		t.Errorf("summary totals (%d+%d+%d) != check count (%d)",
			report.Summary.OK, report.Summary.Warn, report.Summary.Fail, len(report.Checks))
	}
}
