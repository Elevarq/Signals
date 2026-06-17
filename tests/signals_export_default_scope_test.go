// Tests for the latest-snapshot export semantics added by R084..R086.
//
// Spec: features/arq-signals/specification.md
//   ARQ-SIGNALS-R084 (default export scope = latest completed
//                     snapshot per active target)
//   ARQ-SIGNALS-R085 (explicit selectors: --all, --snapshot-id,
//                     --since/--until, --target-id)
//   ARQ-SIGNALS-R086 (metadata fields snapshot_count + ingest_mode)
// Spec: features/arq-signals/acceptance-tests.md
//   TC-SIG-093..TC-SIG-099
//
// These tests describe the producer-side wire contract any consumer
// (Elevarq Analyzer, third-party integrations) is entitled to rely on.
// They run against the in-process SQLite store and the export
// Builder; no HTTP layer or arqctl process is involved at this
// level.

package tests

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/elevarq/arq-signals/internal/db"
	"github.com/elevarq/arq-signals/internal/export"
)

// ---------------------------------------------------------------
// Fixtures shared across the new tests
// ---------------------------------------------------------------

// seedSnapshotsTwoTargets inserts a deterministic mix of snapshots
// for two targets so the latest-per-target / --all / --since-until
// branches each have something distinct to assert.
//
// Layout:
//
//	target=1: snap-A1 (older), snap-A2 (newer)
//	target=2: snap-B1 (older), snap-B2 (newer)
//
// The "newer" timestamps are deliberately offset between targets so
// a global-ordered scan would not pick the same row as latest-per-
// target (assert R084 actually groups by target_id).
func seedSnapshotsTwoTargets(t *testing.T, store *db.DB) {
	t.Helper()

	// R090: snapshots whose target_id does not appear in the
	// targets table are filtered from the default scope. Register
	// the two targets up front so the snapshots below land as
	// canonical (non-orphan) rows. UpsertTarget on a fresh DB
	// returns 1, then 2, matching the literal ids used below.
	idA, err := store.UpsertTarget("target-A", "host-a", 5432, "postgres", "arq", "disable", "NONE", "", true)
	if err != nil {
		t.Fatalf("UpsertTarget target-A: %v", err)
	}
	if idA != 1 {
		t.Fatalf("first UpsertTarget returned id=%d, want 1 (test fixture assumes AUTOINCREMENT starts at 1)", idA)
	}
	idB, err := store.UpsertTarget("target-B", "host-b", 5432, "postgres", "arq", "disable", "NONE", "", true)
	if err != nil {
		t.Fatalf("UpsertTarget target-B: %v", err)
	}
	if idB != 2 {
		t.Fatalf("second UpsertTarget returned id=%d, want 2", idB)
	}

	for _, snap := range []db.Snapshot{
		{ID: "snap-A1", TargetID: 1, CollectedAt: "2026-04-25T10:00:00Z", PGVersion: "PostgreSQL 18.0", Payload: json.RawMessage(`{}`)},
		{ID: "snap-A2", TargetID: 1, CollectedAt: "2026-04-25T12:00:00Z", PGVersion: "PostgreSQL 18.0", Payload: json.RawMessage(`{}`)},
		{ID: "snap-B1", TargetID: 2, CollectedAt: "2026-04-25T11:00:00Z", PGVersion: "PostgreSQL 17.5", Payload: json.RawMessage(`{}`)},
		{ID: "snap-B2", TargetID: 2, CollectedAt: "2026-04-25T13:00:00Z", PGVersion: "PostgreSQL 17.5", Payload: json.RawMessage(`{}`)},
	} {
		runs := []db.QueryRun{{
			ID:          "run-" + snap.ID,
			TargetID:    snap.TargetID,
			SnapshotID:  snap.ID,
			QueryID:     "pg_settings_v1",
			CollectedAt: snap.CollectedAt,
			PGVersion:   snap.PGVersion,
			CreatedAt:   snap.CollectedAt,
			Status:      "success",
		}}
		// QueryResult.Payload is NDJSON — one JSON object per line —
		// not a JSON array. The export decodes via db.DecodeNDJSON.
		results := []db.QueryResult{{
			RunID:     runs[0].ID,
			Payload:   []byte("{\"name\":\"shared_buffers\",\"setting\":\"128MB\"}\n"),
			SizeBytes: 50,
		}}
		if err := store.InsertCollectionAtomic(snap, runs, results); err != nil {
			t.Fatalf("InsertCollectionAtomic %s: %v", snap.ID, err)
		}
	}
}

// readZipNDJSONIDs returns the "id" or "snapshot_id" field of every
// row in a JSON-line file inside the ZIP. Used to inspect which
// snapshots ended up in the export.
func readZipNDJSONField(t *testing.T, zr *zip.Reader, name, field string) []string {
	t.Helper()
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", name, err)
		}
		defer rc.Close()
		var out []string
		sc := bufio.NewScanner(rc)
		sc.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
		for sc.Scan() {
			if len(sc.Bytes()) == 0 {
				continue
			}
			var row map[string]any
			if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
				t.Fatalf("decode line in %s: %v", name, err)
			}
			if v, ok := row[field].(string); ok {
				out = append(out, v)
			}
		}
		if err := sc.Err(); err != nil {
			t.Fatalf("scan %s: %v", name, err)
		}
		return out
	}
	t.Fatalf("file %s not found in ZIP", name)
	return nil
}

// buildExportZIPWithOpts is the variant of the existing
// buildExportZIP helper that takes explicit Options. The existing
// helper passes export.Options{} which is the new default-latest
// scope; tests that need --all / --snapshot-id / --since / --until
// use this one.
func buildExportZIPWithOpts(t *testing.T, store *db.DB, opts export.Options) (*bytes.Buffer, *zip.Reader, error) {
	t.Helper()
	builder := export.NewBuilder(store, "test-instance-id")
	var buf bytes.Buffer
	if err := builder.WriteTo(&buf, opts); err != nil {
		return nil, nil, err
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	return &buf, zr, nil
}

// ---------------------------------------------------------------
// TC-SIG-093 — default export scope is latest-per-target (R084)
// ---------------------------------------------------------------

func TestExportDefaultScopeIsLatestPerTarget(t *testing.T) {
	store := openTestDB(t)
	seedSnapshotsTwoTargets(t, store)

	_, zr, err := buildExportZIPWithOpts(t, store, export.Options{})
	if err != nil {
		t.Fatalf("WriteTo (default opts): %v", err)
	}

	got := readZipNDJSONField(t, zr, "snapshots.ndjson", "id")
	want := map[string]bool{"snap-A2": true, "snap-B2": true}
	if len(got) != 2 {
		t.Fatalf("default export snapshot count = %d, want 2; got=%v", len(got), got)
	}
	for _, id := range got {
		if !want[id] {
			t.Errorf("default export contains unexpected snapshot %q (want only the latest per target: snap-A2, snap-B2)", id)
		}
	}

	// query_runs.ndjson and query_results.ndjson must mirror the
	// snapshot set — older cycles' rows must be absent.
	runIDs := readZipNDJSONField(t, zr, "query_runs.ndjson", "snapshot_id")
	for _, sid := range runIDs {
		if !want[sid] {
			t.Errorf("query_runs.ndjson contains snapshot_id %q from a non-latest cycle", sid)
		}
	}

	meta := readZipFileJSON(t, zr, "metadata.json")
	if got := int(meta["snapshot_count"].(float64)); got != 2 {
		t.Errorf("metadata.snapshot_count = %d, want 2 (one per active target)", got)
	}
	if got := meta["ingest_mode"]; got != "analyze" {
		t.Errorf("metadata.ingest_mode = %v, want %q", got, "analyze")
	}
}

// ---------------------------------------------------------------
// TC-SIG-094 — --all restores pre-R084 behavior (R085)
// ---------------------------------------------------------------

func TestExportAllRestoresFullHistory(t *testing.T) {
	store := openTestDB(t)
	seedSnapshotsTwoTargets(t, store)

	_, zr, err := buildExportZIPWithOpts(t, store, export.Options{All: true})
	if err != nil {
		t.Fatalf("WriteTo (--all): %v", err)
	}

	got := readZipNDJSONField(t, zr, "snapshots.ndjson", "id")
	want := []string{"snap-A1", "snap-A2", "snap-B1", "snap-B2"}
	if len(got) != len(want) {
		t.Fatalf("--all snapshot count = %d, want %d; got=%v", len(got), len(want), got)
	}
	have := map[string]bool{}
	for _, id := range got {
		have[id] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("--all export missing %s", w)
		}
	}

	meta := readZipFileJSON(t, zr, "metadata.json")
	if got := int(meta["snapshot_count"].(float64)); got != 4 {
		t.Errorf("metadata.snapshot_count = %d, want 4", got)
	}
	if got := meta["ingest_mode"]; got != "analyze" {
		t.Errorf("metadata.ingest_mode = %v, want %q (operator-driven --all)", got, "analyze")
	}
}

// TestExportAllAndSnapshotIDMutuallyExclusive pins FC-08: both
// selectors set is an input error, surfaced as an export-builder
// error (the HTTP layer maps this to 400 in a separate test).
func TestExportAllAndSnapshotIDMutuallyExclusive(t *testing.T) {
	store := openTestDB(t)
	seedSnapshotsTwoTargets(t, store)

	_, _, err := buildExportZIPWithOpts(t, store, export.Options{All: true, SnapshotID: "snap-A1"})
	if err == nil {
		t.Fatalf("WriteTo accepted both --all and --snapshot-id; want error")
	}
	// The error should name both selectors so an operator reading
	// the message can immediately see which two clashed.
	msg := err.Error()
	if !strings.Contains(msg, "snapshot-id") || !strings.Contains(msg, "all") {
		t.Errorf("error should name both --all and --snapshot-id; got %v", err)
	}
}

// ---------------------------------------------------------------
// TC-SIG-095 — --snapshot-id returns exactly one snapshot (R085, FC-08)
// ---------------------------------------------------------------

func TestExportSnapshotIDReturnsExactlyOne(t *testing.T) {
	store := openTestDB(t)
	seedSnapshotsTwoTargets(t, store)

	_, zr, err := buildExportZIPWithOpts(t, store, export.Options{SnapshotID: "snap-A1"})
	if err != nil {
		t.Fatalf("WriteTo (snapshot-id): %v", err)
	}

	got := readZipNDJSONField(t, zr, "snapshots.ndjson", "id")
	if len(got) != 1 || got[0] != "snap-A1" {
		t.Errorf("--snapshot-id snap-A1: got %v, want exactly [snap-A1]", got)
	}

	meta := readZipFileJSON(t, zr, "metadata.json")
	if got := int(meta["snapshot_count"].(float64)); got != 1 {
		t.Errorf("metadata.snapshot_count = %d, want 1", got)
	}
	if got := meta["ingest_mode"]; got != "analyze" {
		t.Errorf("metadata.ingest_mode = %v, want %q", got, "analyze")
	}
}

// TestExportSnapshotIDUnknownReturnsError pins the FC-08 branch:
// unknown snapshot id surfaces as a typed error so the HTTP layer
// can map it to 404.
func TestExportSnapshotIDUnknownReturnsError(t *testing.T) {
	store := openTestDB(t)
	seedSnapshotsTwoTargets(t, store)

	_, _, err := buildExportZIPWithOpts(t, store, export.Options{SnapshotID: "no-such-snapshot"})
	if err == nil {
		t.Fatalf("WriteTo accepted unknown snapshot-id; want error")
	}
	if !errors.Is(err, export.ErrSnapshotNotFound) {
		t.Errorf("error should wrap export.ErrSnapshotNotFound; got %v", err)
	}
	if !strings.Contains(err.Error(), "no-such-snapshot") {
		t.Errorf("error should echo the requested ID; got %v", err)
	}
}

// ---------------------------------------------------------------
// TC-SIG-096 — --since / --until filter by time range (R085)
// ---------------------------------------------------------------

func TestExportSinceUntilFiltersByTimeRange(t *testing.T) {
	store := openTestDB(t)
	seedSnapshotsTwoTargets(t, store)

	// Window covers snap-B1 (11:00) and snap-A2 (12:00) only.
	_, zr, err := buildExportZIPWithOpts(t, store, export.Options{
		Since: "2026-04-25T10:30:00Z",
		Until: "2026-04-25T12:30:00Z",
	})
	if err != nil {
		t.Fatalf("WriteTo (since/until): %v", err)
	}

	got := readZipNDJSONField(t, zr, "snapshots.ndjson", "id")
	want := map[string]bool{"snap-B1": true, "snap-A2": true}
	if len(got) != 2 {
		t.Fatalf("range scope snapshot count = %d, want 2; got=%v", len(got), got)
	}
	for _, id := range got {
		if !want[id] {
			t.Errorf("range scope contains out-of-range snapshot %q", id)
		}
	}

	meta := readZipFileJSON(t, zr, "metadata.json")
	if got := int(meta["snapshot_count"].(float64)); got != 2 {
		t.Errorf("metadata.snapshot_count = %d, want 2", got)
	}
}

// ---------------------------------------------------------------
// TC-SIG-097 — metadata carries snapshot_count + ingest_mode (R086)
// ---------------------------------------------------------------

func TestExportMetadataCarriesSnapshotCountAndIngestMode(t *testing.T) {
	store := openTestDB(t)
	seedSnapshotsTwoTargets(t, store)

	cases := []struct {
		name    string
		opts    export.Options
		wantCnt int
	}{
		{"default (latest-per-target)", export.Options{}, 2},
		{"--all", export.Options{All: true}, 4},
		{"--snapshot-id", export.Options{SnapshotID: "snap-A1"}, 1},
		{"--since/--until", export.Options{Since: "2026-04-25T10:30:00Z", Until: "2026-04-25T12:30:00Z"}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, zr, err := buildExportZIPWithOpts(t, store, tc.opts)
			if err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			meta := readZipFileJSON(t, zr, "metadata.json")
			if got, ok := meta["snapshot_count"].(float64); !ok || int(got) != tc.wantCnt {
				t.Errorf("snapshot_count = %v (type %T), want %d", meta["snapshot_count"], meta["snapshot_count"], tc.wantCnt)
			}
			if got, ok := meta["ingest_mode"].(string); !ok || got != "analyze" {
				t.Errorf("ingest_mode = %v, want %q", meta["ingest_mode"], "analyze")
			}
		})
	}
}

// ---------------------------------------------------------------
// TC-SIG-098 — empty-store default export is well-formed (FC-09)
// ---------------------------------------------------------------

func TestExportDefaultScopeOnEmptyStore(t *testing.T) {
	store := openTestDB(t)
	// No snapshots seeded.

	_, zr, err := buildExportZIPWithOpts(t, store, export.Options{})
	if err != nil {
		t.Fatalf("WriteTo on empty store: %v", err)
	}

	// Six required files still present.
	want := []string{
		"metadata.json",
		"collector_status.json",
		"snapshots.ndjson",
		"query_catalog.json",
		"query_runs.ndjson",
		"query_results.ndjson",
	}
	have := map[string]int64{}
	for _, f := range zr.File {
		have[f.Name] = int64(f.UncompressedSize64)
	}
	for _, name := range want {
		if _, ok := have[name]; !ok {
			t.Errorf("empty-store export missing %s", name)
		}
	}

	// Data NDJSON files are zero-length on an empty store.
	for _, name := range []string{"snapshots.ndjson", "query_runs.ndjson", "query_results.ndjson"} {
		if have[name] != 0 {
			t.Errorf("%s should be zero-length on empty store; got %d bytes", name, have[name])
		}
	}

	meta := readZipFileJSON(t, zr, "metadata.json")
	if got := int(meta["snapshot_count"].(float64)); got != 0 {
		t.Errorf("metadata.snapshot_count = %d, want 0 on empty store", got)
	}
	if got := meta["ingest_mode"]; got != "analyze" {
		t.Errorf("metadata.ingest_mode = %v, want %q on empty store", got, "analyze")
	}
}

// ---------------------------------------------------------------
// --target-id composes with default-latest (R084 + R073)
// ---------------------------------------------------------------

func TestExportDefaultScopeWithTargetIDReturnsTargetLatest(t *testing.T) {
	store := openTestDB(t)
	seedSnapshotsTwoTargets(t, store)

	// Default scope narrowed to target=1 should return only snap-A2,
	// not snap-A1 and not anything from target=2.
	_, zr, err := buildExportZIPWithOpts(t, store, export.Options{TargetID: 1})
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	got := readZipNDJSONField(t, zr, "snapshots.ndjson", "id")
	if len(got) != 1 || got[0] != "snap-A2" {
		t.Errorf("--target-id=1 default scope: got %v, want exactly [snap-A2]", got)
	}

	meta := readZipFileJSON(t, zr, "metadata.json")
	if got := int(meta["snapshot_count"].(float64)); got != 1 {
		t.Errorf("metadata.snapshot_count = %d, want 1", got)
	}
}

// silence unused-import linter when the file's imports change
// during refactors — keeps the diff stable.
var _ = fmt.Sprintf

// ---------------------------------------------------------------
// HTTP-layer wire contract — same R084/R085/FC-08 rules but
// exercised through GET /export so the API status codes are
// pinned alongside the builder semantics.
// ---------------------------------------------------------------

// TestExportHTTPDefaultScopeAndAllSelector exercises the wire
// contract: bare /export → R084 default; /export?all=true → full
// history; both visible to the operator via metadata.
func TestExportHTTPDefaultScopeAndAllSelector(t *testing.T) {
	handler, cleanup := makeTestHandler(t)
	defer cleanup()

	// Cannot easily seed snapshots through the makeTestHandler path
	// (the store is internal to the helper), so we only exercise the
	// status-code + content-type contract here. The richer scope
	// assertions live in the builder-level tests above.

	r := httptest.NewRequest("GET", "/export", nil)
	r.Header.Set("Authorization", "Bearer "+testAPIToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("GET /export (default) status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/zip" {
		t.Errorf("Content-Type = %q, want application/zip", ct)
	}

	r = httptest.NewRequest("GET", "/export?all=true", nil)
	r.Header.Set("Authorization", "Bearer "+testAPIToken)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("GET /export?all=true status = %d, want 200", w.Code)
	}
}

// TestExportHTTPMutualExclusionReturns400 pins FC-08 part 1.
func TestExportHTTPMutualExclusionReturns400(t *testing.T) {
	handler, cleanup := makeTestHandler(t)
	defer cleanup()

	r := httptest.NewRequest("GET", "/export?all=true&snapshot_id=anything", nil)
	r.Header.Set("Authorization", "Bearer "+testAPIToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("GET /export?all=true&snapshot_id=... status = %d, want 400", w.Code)
	}
}

// TestExportHTTPSnapshotIDNotFoundReturns404 pins FC-08 part 2.
func TestExportHTTPSnapshotIDNotFoundReturns404(t *testing.T) {
	handler, cleanup := makeTestHandler(t)
	defer cleanup()

	r := httptest.NewRequest("GET", "/export?snapshot_id=does-not-exist", nil)
	r.Header.Set("Authorization", "Bearer "+testAPIToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("GET /export?snapshot_id=does-not-exist status = %d, want 404", w.Code)
	}
}

// TestExportHTTPInvalidAllReturns400 pins R085's bool parsing.
func TestExportHTTPInvalidAllReturns400(t *testing.T) {
	handler, cleanup := makeTestHandler(t)
	defer cleanup()

	r := httptest.NewRequest("GET", "/export?all=maybe", nil)
	r.Header.Set("Authorization", "Bearer "+testAPIToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("GET /export?all=maybe status = %d, want 400", w.Code)
	}
}
