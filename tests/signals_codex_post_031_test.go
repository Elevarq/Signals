package tests

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/elevarq/arq-signals/internal/api"
	"github.com/elevarq/arq-signals/internal/collector"
	"github.com/elevarq/arq-signals/internal/config"
	"github.com/elevarq/arq-signals/internal/db"
	"github.com/elevarq/arq-signals/internal/export"
	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// TestCollectTargetRejectsPostgresBelowMinSupportedMajor verifies that
// collectTarget refuses to run against PostgreSQL majors older than
// pgqueries.MinSupportedMajor (currently 14). The collector has no
// per-major catalog files for PG < 14 and no realistic test surface;
// running anyway risks silent miscollection.
//
// The test asserts the source structure (the post-discovery branch
// returns a bounded "version_unsupported" error before any catalog
// filtering or query execution), avoiding the need for a live PG 13
// database in CI.
//
// Codex post-0.3.1 review: H-001
func TestCollectTargetRejectsPostgresBelowMinSupportedMajor(t *testing.T) {
	root := repoRoot(t)
	src := readFileString(t, filepath.Join(root, "internal", "collector", "collector.go"))

	if !strings.Contains(src, "disc.MajorVersion < pgqueries.MinSupportedMajor") {
		t.Fatal("collectTarget does not check disc.MajorVersion < pgqueries.MinSupportedMajor — " +
			"PG <14 targets may be collected against the highest supported catalog and silently miscollect")
	}
	if !strings.Contains(src, "reason=version_unsupported") {
		t.Fatal("the PG <14 rejection error does not carry reason=version_unsupported — " +
			"audit/metric classification cannot distinguish version-floor failures")
	}

	// The check must appear after Discover and before catalog filtering
	// so the failure is fail-closed rather than fail-open.
	idxDiscover := strings.Index(src, "pgqueries.Discover(ctx, tx)")
	idxFloor := strings.Index(src, "disc.MajorVersion < pgqueries.MinSupportedMajor")
	idxFilter := strings.Index(src, "pgqueries.Filter(filterParams)")
	if idxDiscover < 0 || idxFloor < 0 || idxFilter < 0 {
		t.Fatal("required collectTarget anchors not found")
	}
	if idxDiscover >= idxFloor || idxFloor >= idxFilter {
		t.Fatal("PG version-floor check is not between Discover and Filter — " +
			"unsupported majors may reach catalog resolution before being rejected")
	}
}

// TestVersionUnsupportedClassifiedSeparately ensures the metrics
// failure classifier returns a dedicated bucket for the version-floor
// rejection so operators can grep for it without being conflated
// with safety_check or generic internal errors.
//
// Codex post-0.3.1 review: H-001
func TestVersionUnsupportedClassifiedSeparately(t *testing.T) {
	root := repoRoot(t)
	src := readFileString(t, filepath.Join(root, "internal", "collector", "collector.go"))

	if !strings.Contains(src, `return "version_unsupported"`) {
		t.Fatal("classifyCollectionFailure does not return a dedicated 'version_unsupported' label")
	}
}

// TestSetLocalTimeoutFailureAbortsCollection verifies that the
// SET LOCAL loop in collectTarget treats failures as hard collection
// errors instead of warnings. The previous behaviour logged at WARN
// and continued; if SET LOCAL was rejected (e.g. permission revoked
// on the GUC) the cycle would run with no statement_timeout,
// lock_timeout, or idle_in_transaction_session_timeout — exactly the
// safety guards the spec requires.
//
// Asserted via source structure because reproducing a SET LOCAL
// failure against a live PostgreSQL needs an attacker-shaped test
// fixture that adds no value over the source check.
//
// Codex post-0.3.1 review: H-004
func TestSetLocalTimeoutFailureAbortsCollection(t *testing.T) {
	root := repoRoot(t)
	src := readFileString(t, filepath.Join(root, "internal", "collector", "collector.go"))

	// The old "warn and continue" pattern must be gone.
	if strings.Contains(src, `slog.Warn("failed to SET LOCAL timeout"`) {
		t.Fatal("collector still warns and continues on SET LOCAL failure — " +
			"the safety contract requires aborting the cycle (H-004)")
	}

	// The fixed pattern must return an error inside the SET LOCAL loop.
	idxLoop := strings.Index(src, `"SET LOCAL %s = %d"`)
	idxAbort := strings.Index(src, "timeout safety cannot be enforced")
	if idxLoop < 0 || idxAbort < 0 {
		t.Fatal("SET LOCAL loop or abort marker missing from collector.go")
	}
	if idxAbort < idxLoop {
		t.Fatal("abort marker appears before the SET LOCAL loop — fix is misplaced")
	}

	// Failure classifier must label this distinctly so the metrics
	// surface explains operator pages without source diving.
	if !strings.Contains(src, `return "timeout_setup"`) {
		t.Fatal("classifyCollectionFailure does not classify SET LOCAL failures as timeout_setup")
	}
}

// TestSavepointErrorsHandled verifies the per-query SAVEPOINT,
// ROLLBACK TO SAVEPOINT, and RELEASE SAVEPOINT calls all check the
// returned error. The previous code discarded all three; a SAVEPOINT
// failure would silently break the per-query recovery contract and a
// downstream failure could poison the whole cycle.
//
// Codex post-0.3.1 review: M-005
func TestSavepointErrorsHandled(t *testing.T) {
	root := repoRoot(t)
	src := readFileString(t, filepath.Join(root, "internal", "collector", "collector.go"))

	// The bare tx.Exec(ctx, "SAVEPOINT/ROLLBACK/RELEASE …") forms
	// must be gone — error returns must be captured.
	bad := []string{
		`tx.Exec(ctx, "SAVEPOINT "+savepointName)`,
		`tx.Exec(ctx, "ROLLBACK TO SAVEPOINT "+savepointName)`,
		`tx.Exec(ctx, "RELEASE SAVEPOINT "+savepointName)`,
	}
	for _, b := range bad {
		if strings.Contains(src, "\t"+b+"\n") || strings.Contains(src, "\t\t"+b+"\n") {
			t.Errorf("collector.go still contains a bare savepoint exec without error check: %q", b)
		}
	}

	// And the fixed form must be present for each operation.
	good := []string{
		`spErr := tx.Exec(ctx, "SAVEPOINT "+savepointName)`,
		`rbErr := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT "+savepointName)`,
		`relErr := tx.Exec(ctx, "RELEASE SAVEPOINT "+savepointName)`,
	}
	for _, g := range good {
		if !strings.Contains(src, g) {
			t.Errorf("collector.go missing checked-error form for savepoint op: %q", g)
		}
	}
}

// TestSkippedRunsPersistedForVersionUnsupportedCollectors verifies
// that GatedIDsByReason buckets registry collectors whose MinPGVersion
// is above the connected target's major under "version_unsupported".
// The collector loop persists the returned IDs as skipped query_runs
// so collector_status.json never silently drops these collectors.
//
// Codex post-0.3.1 review: H-002
func TestSkippedRunsPersistedForVersionUnsupportedCollectors(t *testing.T) {
	// Pick a major that sits well below the registered MinPGVersion
	// values used by version-gated collectors (e.g. PG 17 stat
	// collectors). PG 14 is at the support floor; any registered
	// MinPGVersion >= 15 will be flagged version_unsupported here.
	gated := pgqueries.GatedIDsByReason(pgqueries.FilterParams{
		PGMajorVersion:         pgqueries.MinSupportedMajor,
		HighSensitivityEnabled: true,
	})
	ids := gated[pgqueries.GateReasonVersionUnsupported]
	if len(ids) == 0 {
		t.Fatal("expected at least one collector with MinPGVersion above " +
			"MinSupportedMajor to land in the version_unsupported bucket; " +
			"either no such collector is registered or GatedIDsByReason is mis-bucketing")
	}

	// And the source loop must persist by reason — the call site is
	// the contract that turns the registry signal into stored runs.
	root := repoRoot(t)
	src := readFileString(t, filepath.Join(root, "internal", "collector", "collector.go"))
	for _, expect := range []string{
		"pgqueries.GatedIDsByReason",
		"pgqueries.GateReasonVersionUnsupported",
		"pgqueries.GateReasonExtensionMissing",
		"pgqueries.GateReasonConfigDisabled",
	} {
		if !strings.Contains(src, expect) {
			t.Errorf("collector.go does not reference %s — gated collectors will not be persisted as skipped runs", expect)
		}
	}
}

// TestSkippedRunsPersistedForMissingExtensionCollectors verifies the
// extension-missing bucket is exposed by GatedIDsByReason for at least
// one registered collector so the persistence loop has work to do.
//
// Codex post-0.3.1 review: H-002
func TestSkippedRunsPersistedForMissingExtensionCollectors(t *testing.T) {
	// No extensions present. Any collector that requires an extension
	// must surface here.
	gated := pgqueries.GatedIDsByReason(pgqueries.FilterParams{
		PGMajorVersion:         pgqueries.MaxSupportedMajor,
		Extensions:             nil,
		HighSensitivityEnabled: true,
	})
	if len(gated[pgqueries.GateReasonExtensionMissing]) == 0 {
		t.Fatal("expected at least one collector requiring an extension to be " +
			"bucketed as extension_missing when no extensions are installed")
	}
}

// TestGetLastRunTimesIgnoresSkippedAndFailedRuns verifies that
// cadence is only advanced by successful runs. Skipped and failed
// runs must not be returned by GetLastRunTimes — otherwise a
// once-skipped collector would not be re-attempted for a full cadence
// window, hiding transient failures and configuration gates behind
// invisible delays.
//
// Codex post-0.3.1 review: H-003
func TestGetLastRunTimesIgnoresSkippedAndFailedRuns(t *testing.T) {
	store := openTestDB(t)
	tid, err := store.UpsertTarget("t1", "h", 5432, "db", "u", "disable", "NONE", "", true)
	if err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}

	now := time.Now().UTC()
	older := now.Add(-2 * time.Hour).Format(time.RFC3339)
	newer := now.Add(-1 * time.Hour).Format(time.RFC3339)

	snap := db.Snapshot{
		ID: "snap-h003", TargetID: tid, CollectedAt: older,
		PGVersion: "PostgreSQL 17.0",
		Payload:   json.RawMessage(`{}`),
	}
	runs := []db.QueryRun{
		// Older successful run — this should be the value returned.
		{ID: "ok", TargetID: tid, SnapshotID: snap.ID, QueryID: "pg_stat_database_v1",
			CollectedAt: older, PGVersion: "PostgreSQL 17.0", CreatedAt: older, Status: "success"},
		// Newer skipped run — must NOT advance cadence.
		{ID: "skip", TargetID: tid, SnapshotID: snap.ID, QueryID: "pg_stat_database_v1",
			CollectedAt: newer, PGVersion: "PostgreSQL 17.0", CreatedAt: newer,
			Status: "skipped", Reason: "config_disabled"},
		// Newer failed run on a different collector — must also NOT
		// appear in the map.
		{ID: "fail", TargetID: tid, SnapshotID: snap.ID, QueryID: "pg_stat_io_v1",
			CollectedAt: newer, PGVersion: "PostgreSQL 17.0", CreatedAt: newer,
			Error: "permission denied", Status: "failed", Reason: "permission_denied"},
	}
	if err := store.InsertCollectionAtomic(snap, runs, nil); err != nil {
		t.Fatalf("InsertCollectionAtomic: %v", err)
	}

	got, err := store.GetLastRunTimes(tid)
	if err != nil {
		t.Fatalf("GetLastRunTimes: %v", err)
	}

	// pg_stat_database_v1 must reflect the OLDER successful timestamp,
	// not the newer skipped one.
	expectedOlder, _ := time.Parse(time.RFC3339, older)
	if ts, ok := got["pg_stat_database_v1"]; !ok {
		t.Fatal("pg_stat_database_v1 missing from GetLastRunTimes; successful run was lost")
	} else if !ts.Equal(expectedOlder) {
		t.Errorf("pg_stat_database_v1 last-run = %v, want %v (skipped runs must not advance cadence)", ts, expectedOlder)
	}

	// pg_stat_io_v1 (only failed run) must NOT appear at all.
	if ts, ok := got["pg_stat_io_v1"]; ok {
		t.Errorf("pg_stat_io_v1 unexpectedly present in GetLastRunTimes (%v); failed runs must not advance cadence", ts)
	}
}

// TestValidateStrictRejectsInvalidSSLMode verifies that ValidateStrict
// rejects sslmode values outside the libpq enum. Without this the
// invalid string passes straight through to libpq and surfaces as an
// opaque connect-time error per target rather than at startup.
//
// Codex post-0.3.1 review: M-006
func TestValidateStrictRejectsInvalidSSLMode(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Targets = []config.TargetConfig{{
		Name: "primary", Host: "db", Port: 5432, DBName: "app", User: "u",
		SSLMode: "secure", // typo: not a valid libpq value
		Enabled: true,
	}}
	_, err := config.ValidateStrict(cfg)
	if err == nil {
		t.Fatal("ValidateStrict accepted sslmode=\"secure\" — should be rejected as invalid libpq enum")
	}
	if !strings.Contains(err.Error(), `sslmode "secure"`) {
		t.Errorf("error does not name the offending value: %v", err)
	}
}

// TestValidateProdTLSRejectsWeakSSLMode verifies prod TLS validation
// still rejects everything weaker than verify-ca / verify-full and
// treats `require` as weak (no server-identity verification).
//
// Codex post-0.3.1 review: M-006
func TestValidateProdTLSRejectsWeakSSLMode(t *testing.T) {
	for _, mode := range []string{"disable", "allow", "prefer", "require"} {
		t.Run(mode, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Env = "prod"
			cfg.Targets = []config.TargetConfig{{
				Name: "primary", Host: "db", Port: 5432, DBName: "app", User: "u",
				SSLMode: mode, Enabled: true,
			}}
			if err := config.ValidateProdTLS(cfg); err == nil {
				t.Errorf("prod TLS accepted sslmode=%q — only verify-ca/verify-full count as strong", mode)
			}
		})
	}
}

// TestRetentionDaysZeroMatchesDocumentedBehavior verifies the
// retention_days <= 0 warning text matches the actual cleanup
// behaviour: cleanup() returns immediately, snapshots are kept
// indefinitely. Codex post-0.3.1 review found the previous warning
// said the next cycle would delete them — the opposite of what
// happens — which would mislead operators into thinking they had
// configured an aggressive purge when they had in fact disabled
// cleanup entirely.
//
// Codex post-0.3.1 review: M-004
func TestRetentionDaysZeroMatchesDocumentedBehavior(t *testing.T) {
	cases := []int{0, -1}
	for _, days := range cases {
		cfg := config.DefaultConfig()
		cfg.Targets = []config.TargetConfig{{
			Name: "primary", Host: "db", Port: 5432, DBName: "app", User: "u",
			SSLMode: "verify-full", Enabled: true,
		}}
		cfg.Signals.RetentionDays = days

		warnings, err := config.ValidateStrict(cfg)
		if err != nil {
			t.Fatalf("ValidateStrict(retention_days=%d) returned hard error: %v", days, err)
		}
		var found bool
		for _, w := range warnings {
			if strings.Contains(w, "cleanup is disabled") &&
				strings.Contains(w, "retained") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("retention_days=%d did not produce 'cleanup is disabled / retained' warning; got %v", days, warnings)
		}
		// Negative warning text from before — must NOT appear.
		for _, w := range warnings {
			if strings.Contains(w, "deleted on the next cleanup") ||
				strings.Contains(w, "deleted immediately") {
				t.Errorf("retention_days=%d emits the legacy misleading warning: %q", days, w)
			}
		}
	}
}

// TestGitleaksInstallVerifiesChecksum verifies CI and release
// workflows download gitleaks with checksum verification rather than
// piping curl into tar. Codex post-0.3.1 review: L-003.
func TestGitleaksInstallVerifiesChecksum(t *testing.T) {
	root := repoRoot(t)
	for _, rel := range []string{".github/workflows/ci.yml", ".github/workflows/release.yml"} {
		src := readFileString(t, filepath.Join(root, rel))
		if strings.Contains(src, "curl -sSfL https://github.com/gitleaks/gitleaks/releases/download") &&
			strings.Contains(src, "| tar -xz -C /usr/local/bin gitleaks") {
			t.Errorf("%s still pipes curl directly into tar — gitleaks tarball is unverified", rel)
		}
		if !strings.Contains(src, "sha256sum -c -") {
			t.Errorf("%s does not verify the gitleaks tarball checksum (sha256sum -c -)", rel)
		}
		if !strings.Contains(src, "gitleaks_") || !strings.Contains(src, "checksums.txt") {
			t.Errorf("%s does not download the upstream gitleaks checksums file", rel)
		}
	}
}

// TestPanicRecoveryReturnsJSON verifies that the recovery middleware
// returns a JSON-encoded error with Content-Type: application/json
// when a downstream handler panics. The previous implementation used
// http.Error which sets text/plain — clients that switch on the
// response Content-Type would treat the panic body as a non-JSON
// error and break their parser. Codex post-0.3.1 review: L-002.
//
// Asserted via source structure plus a behaviour check by source
// inspection — recoveryMiddleware is unexported.
func TestPanicRecoveryReturnsJSON(t *testing.T) {
	root := repoRoot(t)
	src := readFileString(t, filepath.Join(root, "internal", "api", "server.go"))

	// The legacy http.Error call site for panics must be gone.
	if strings.Contains(src, `http.Error(w, `+"`"+`{"error":"internal server error"}`+"`") {
		t.Fatal("recoveryMiddleware still uses http.Error — Content-Type will be text/plain (L-002)")
	}

	// The fixed form must use writeJSON inside the recover block.
	idxRecover := strings.Index(src, "if rec := recover(); rec != nil {")
	if idxRecover < 0 {
		t.Fatal("recoveryMiddleware recover() block not found")
	}
	tail := src[idxRecover:]
	tailEnd := 600
	if len(tail) < tailEnd {
		tailEnd = len(tail)
	}
	if !strings.Contains(tail[:tailEnd], "writeJSON(w, http.StatusInternalServerError") {
		t.Error("recoveryMiddleware does not call writeJSON on panic — JSON Content-Type not enforced")
	}
}

// TestCollectNowRejectsOversizedBody verifies the /collect/now
// handler refuses request bodies larger than the configured cap and
// returns HTTP 413 instead of buffering the entire payload before
// rejecting it. Codex post-0.3.1 review: L-001.
func TestCollectNowRejectsOversizedBody(t *testing.T) {
	handler, cleanup := makeTestHandler(t)
	defer cleanup()

	// 128 KiB — well past the 64 KiB cap and many orders of magnitude
	// past the legal payload (three short fields).
	big := bytes.Repeat([]byte("x"), 128*1024)
	req := httptest.NewRequest("POST", "/collect/now", bytes.NewReader(big))
	req.Header.Set("Authorization", "Bearer "+testAPIToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d (oversized body must be rejected with 413); body=%s",
			w.Code, http.StatusRequestEntityTooLarge, w.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["error"] == nil {
		t.Errorf("expected JSON error field on 413, got: %v", body)
	}
}

// TestStatusReturns500OnDatabaseError verifies that /status surfaces
// database read failures as HTTP 500 instead of silently returning a
// partial-zero response that masks the underlying SQLite problem.
//
// We exercise the failure by closing the DB before issuing the
// request — every subsequent SQL query returns "database is closed".
//
// Codex post-0.3.1 review: M-002
func TestStatusReturns500OnDatabaseError(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "status-error.db")
	store, err := db.Open(dbPath, false)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	if err := store.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := store.EnsureInstanceID(); err != nil {
		t.Fatalf("EnsureInstanceID: %v", err)
	}

	exporter := export.NewBuilder(store, "test-id")
	coll := collector.New(store, nil, time.Hour, 30)
	deps := &api.Deps{DB: store, Collector: coll, Exporter: exporter}
	srv := api.NewServer("127.0.0.1:0", 10*time.Second, 10*time.Second, testAPIToken, deps)
	handler := srv.Handler()

	// Simulate a DB-layer failure by closing the underlying SQLite.
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	req := httptest.NewRequest("GET", "/status", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d (DB read failures must surface as 500)", w.Code, http.StatusInternalServerError)
	}
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["error"] == "" {
		t.Errorf("expected non-empty error field on 500, got: %v", body)
	}
}

// TestExportRejectsInvalidSinceUntil verifies /export rejects
// non-RFC3339 since/until values with HTTP 400 instead of passing
// the strings through to SQLite where they silently match nothing.
//
// Codex post-0.3.1 review: M-003
func TestExportRejectsInvalidSinceUntil(t *testing.T) {
	handler, cleanup := makeTestHandler(t)
	defer cleanup()

	cases := []struct {
		name string
		url  string
	}{
		{"invalid_since", "/export?since=not-a-date"},
		{"invalid_until", "/export?until=2026-99-99T00:00:00Z"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tc.url, nil)
			req.Header.Set("Authorization", "Bearer "+testAPIToken)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
			}
			var body map[string]string
			if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if !strings.Contains(body["error"], "RFC3339") {
				t.Errorf("expected RFC3339 in error message, got: %v", body)
			}
		})
	}
}

// TestExportRejectsSinceAfterUntil verifies inverted ranges return 400.
//
// Codex post-0.3.1 review: M-003
func TestExportRejectsSinceAfterUntil(t *testing.T) {
	handler, cleanup := makeTestHandler(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/export?since=2026-04-25T00:00:00Z&until=2026-04-24T00:00:00Z", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(body["error"], "since must be") {
		t.Errorf("expected 'since must be' in error, got: %v", body)
	}
}

// TestExportFailsWhenSuccessfulRunHasMissingResult verifies that
// the export builder treats a successful run with no row in
// query_results as a hard error rather than silently skipping.
// InsertCollectionAtomic guarantees the run+result pair is committed
// together, so a missing partner indicates out-of-band deletion or
// corruption — the export must surface that.
//
// Codex post-0.3.1 review: M-001
func TestExportFailsWhenSuccessfulRunHasMissingResult(t *testing.T) {
	store := openTestDB(t)
	tid, err := store.UpsertTarget("t1", "h", 5432, "db", "u", "disable", "NONE", "", true)
	if err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	snap := db.Snapshot{
		ID: "snap-m001a", TargetID: tid, CollectedAt: now,
		PGVersion: "PostgreSQL 17.0",
		Payload:   json.RawMessage(`{}`),
	}
	// Successful run, but we deliberately do not pass a matching
	// QueryResult — the runs table records success while the results
	// table is empty.
	runs := []db.QueryRun{{
		ID: "run-orphan", TargetID: tid, SnapshotID: snap.ID,
		QueryID: "pg_stat_database_v1", CollectedAt: now,
		PGVersion: "PostgreSQL 17.0", RowCount: 1,
		CreatedAt: now, Status: "success",
	}}
	if err := store.InsertCollectionAtomic(snap, runs, nil); err != nil {
		t.Fatalf("InsertCollectionAtomic: %v", err)
	}

	builder := export.NewBuilder(store, "test-instance-id")
	var buf bytes.Buffer
	err = builder.WriteTo(&buf, export.Options{})
	if err == nil {
		t.Fatal("export succeeded despite missing result payload — should be a data-integrity error (M-001)")
	}
	if !strings.Contains(err.Error(), "missing result payload") {
		t.Errorf("expected 'missing result payload' in error, got: %v", err)
	}
}

// TestExportFailsWhenSuccessfulRunResultCannotDecode verifies the
// builder propagates DecodeNDJSON failures instead of silently
// dropping the row. A corrupt payload is not a "skip and move on"
// condition; it means the stored bytes do not match the schema and
// downstream consumers must be alerted.
//
// Codex post-0.3.1 review: M-001
func TestExportFailsWhenSuccessfulRunResultCannotDecode(t *testing.T) {
	store := openTestDB(t)
	tid, err := store.UpsertTarget("t1", "h", 5432, "db", "u", "disable", "NONE", "", true)
	if err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	snap := db.Snapshot{
		ID: "snap-m001b", TargetID: tid, CollectedAt: now,
		PGVersion: "PostgreSQL 17.0",
		Payload:   json.RawMessage(`{}`),
	}
	runs := []db.QueryRun{{
		ID: "run-corrupt", TargetID: tid, SnapshotID: snap.ID,
		QueryID: "pg_stat_database_v1", CollectedAt: now,
		PGVersion: "PostgreSQL 17.0", RowCount: 1,
		CreatedAt: now, Status: "success",
	}}
	// Payload is gzip-flagged but is not a valid gzip stream. The
	// decoder will fail and the export must propagate.
	results := []db.QueryResult{{
		RunID:      "run-corrupt",
		Payload:    []byte("not-gzip-not-ndjson"),
		Compressed: true,
		SizeBytes:  20,
	}}
	if err := store.InsertCollectionAtomic(snap, runs, results); err != nil {
		t.Fatalf("InsertCollectionAtomic: %v", err)
	}

	builder := export.NewBuilder(store, "test-instance-id")
	var buf bytes.Buffer
	err = builder.WriteTo(&buf, export.Options{})
	if err == nil {
		t.Fatal("export succeeded despite corrupt payload — should fail decode (M-001)")
	}
	if !strings.Contains(err.Error(), "decode result for run") {
		t.Errorf("expected 'decode result for run' in error, got: %v", err)
	}
}

// TestUnscopedExportCollectorStatusSynthesizedFromRuns verifies the
// instance-level (no target_id) export path no longer emits an empty
// collectors[] array when query_runs exist. Synthesising from runs
// keeps the file faithful to what was collected.
//
// Uses a skipped run as fixture so the test does not require a
// matching result payload (M-001 makes that mandatory for success
// runs); the synthesis behaviour we're proving is identical for
// skipped vs. successful inputs.
//
// Codex post-0.3.1 review: H-002
func TestUnscopedExportCollectorStatusSynthesizedFromRuns(t *testing.T) {
	store := openTestDB(t)

	tid, err := store.UpsertTarget("t1", "h", 5432, "db", "u", "disable", "NONE", "", true)
	if err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	snap := db.Snapshot{
		ID: "snap-1", TargetID: tid, CollectedAt: now,
		PGVersion: "PostgreSQL 17.0",
		Payload:   json.RawMessage(`{}`),
	}
	runs := []db.QueryRun{{
		ID: "run-1", TargetID: tid, SnapshotID: snap.ID,
		QueryID: "pg_stat_database_v1", CollectedAt: now,
		PGVersion: "PostgreSQL 17.0", CreatedAt: now,
		Status: "skipped", Reason: "config_disabled",
	}}
	if err := store.InsertCollectionAtomic(snap, runs, nil); err != nil {
		t.Fatalf("InsertCollectionAtomic: %v", err)
	}

	builder := export.NewBuilder(store, "test-instance-id")
	var buf bytes.Buffer
	if err := builder.WriteTo(&buf, export.Options{}); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}

	status := readZipFileJSON(t, zr, "collector_status.json")
	cols, ok := status["collectors"].([]any)
	if !ok {
		t.Fatalf("collector_status.json missing collectors[] field, got: %v", status)
	}
	if len(cols) == 0 {
		t.Fatal("collector_status.json reports zero collectors despite a query_run existing — " +
			"unscoped export should synthesise the status from runs (H-002)")
	}
	first, _ := cols[0].(map[string]any)
	if id, _ := first["id"].(string); id != "pg_stat_database_v1" {
		t.Errorf("expected first collector id 'pg_stat_database_v1', got %v", first)
	}
}
