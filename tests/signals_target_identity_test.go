// Tests for the target-identity / orphan-target_id fix (R089, R090).
//
// Spec: features/arq-signals/specification.md — R089, R090, INV-SIGNALS-14.
// Spec: features/arq-signals/acceptance-tests.md — TC-SIG-100..107.
//
// Background: a v0.3.x daemon running for ~17 hours produced 1,337
// distinct values in `snapshots.target_id` against 1 actual `targets`
// row. Mechanism: SQLite's INSERT ... ON CONFLICT ... DO UPDATE
// reserves an AUTOINCREMENT id from sqlite_sequence before evaluating
// the conflict; the wasted reserved id is then returned by
// last_insert_rowid() (Go's `Result.LastInsertId()`). The collector's
// UpsertTarget code trusted that return value and used it as the
// snapshot's target_id, which drifted upward every cycle.
//
// These tests pin the producer-side fix (R089) and the consumer-side
// defense-in-depth filter (R090). They run against the in-process
// SQLite store; no daemon process or HTTP layer is involved.

package tests

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/elevarq/arq-signals/internal/db"
	"github.com/elevarq/arq-signals/internal/export"
)

// ---------------------------------------------------------------
// R089 — UpsertTarget idempotency
// ---------------------------------------------------------------

const (
	tName  = "test-target"
	tHost  = "localhost"
	tPort  = 5432
	tDB    = "postgres"
	tUser  = "arq_monitor"
	tSSL   = "disable"
	tSType = "NONE"
	tSRef  = ""
)

func upsertSameTargetN(t *testing.T, store *db.DB, n int) []int64 {
	t.Helper()
	ids := make([]int64, 0, n)
	for i := 0; i < n; i++ {
		id, err := store.UpsertTarget(tName, tHost, tPort, tDB, tUser, tSSL, tSType, tSRef, true)
		if err != nil {
			t.Fatalf("UpsertTarget call %d: %v", i, err)
		}
		ids = append(ids, id)
	}
	return ids
}

// TestUpsertTargetIsIdempotent (TC-SIG-100, R089) — repeated upserts
// with the same name return the same id and leave the table with one
// row.
func TestUpsertTargetIsIdempotent(t *testing.T) {
	store := openTestDB(t)
	ids := upsertSameTargetN(t, store, 10)

	// All returned ids equal.
	for i, id := range ids {
		if id != ids[0] {
			t.Errorf("call %d returned id=%d, want %d (drift)", i, id, ids[0])
		}
	}

	// targets table has exactly 1 row.
	var n int
	if err := store.SQL().QueryRow("SELECT COUNT(*) FROM targets WHERE name = ?", tName).Scan(&n); err != nil {
		t.Fatalf("count targets: %v", err)
	}
	if n != 1 {
		t.Errorf("targets row count = %d, want 1", n)
	}

	// Returned id matches the table row.
	var fromTable int64
	if err := store.SQL().QueryRow("SELECT id FROM targets WHERE name = ?", tName).Scan(&fromTable); err != nil {
		t.Fatalf("select id: %v", err)
	}
	if ids[0] != fromTable {
		t.Errorf("UpsertTarget returned id=%d but table has id=%d", ids[0], fromTable)
	}
}

// TestUpsertTargetReturnsRealTableID (TC-SIG-101, R089) — every
// returned id must reference a real row in the targets table.
func TestUpsertTargetReturnsRealTableID(t *testing.T) {
	store := openTestDB(t)
	ids := upsertSameTargetN(t, store, 6)

	for i, id := range ids {
		var name string
		err := store.SQL().QueryRow("SELECT name FROM targets WHERE id = ?", id).Scan(&name)
		if err == sql.ErrNoRows {
			t.Errorf("call %d returned id=%d which is NOT in the targets table — orphan return value", i, id)
			continue
		}
		if err != nil {
			t.Fatalf("select name: %v", err)
		}
		if name != tName {
			t.Errorf("call %d: id=%d → name=%q, want %q", i, id, name, tName)
		}
	}
}

// TestUpsertTargetSurvivesIntermediateInserts (TC-SIG-102, R089) —
// the realistic collector flow alternates UpsertTarget and
// InsertCollectionAtomic. Intermediate INSERTs on other tables must
// not poison the next UpsertTarget's return value.
func TestUpsertTargetSurvivesIntermediateInserts(t *testing.T) {
	store := openTestDB(t)
	id1, err := store.UpsertTarget(tName, tHost, tPort, tDB, tUser, tSSL, tSType, tSRef, true)
	if err != nil {
		t.Fatalf("first UpsertTarget: %v", err)
	}

	// Insert 3 collection cycles using id1 (the canonical id).
	for i := 0; i < 3; i++ {
		snap := db.Snapshot{
			ID:          ulid(t, i+1),
			TargetID:    id1,
			CollectedAt: rfcAt(2026, 5, 7, 12, i, 0),
			PGVersion:   "PostgreSQL 18.0",
			Payload:     json.RawMessage(`{}`),
		}
		runs := []db.QueryRun{{
			ID: snap.ID + "-r", TargetID: id1, SnapshotID: snap.ID,
			QueryID: "pg_settings_v1", CollectedAt: snap.CollectedAt,
			PGVersion: snap.PGVersion, CreatedAt: snap.CollectedAt, Status: "success",
		}}
		results := []db.QueryResult{{
			RunID: runs[0].ID, Payload: []byte("{\"k\":\"v\"}\n"), SizeBytes: 8,
		}}
		if err := store.InsertCollectionAtomic(snap, runs, results); err != nil {
			t.Fatalf("cycle %d: %v", i, err)
		}
	}

	// Second UpsertTarget — must return the same id.
	id2, err := store.UpsertTarget(tName, tHost, tPort, tDB, tUser, tSSL, tSType, tSRef, true)
	if err != nil {
		t.Fatalf("second UpsertTarget: %v", err)
	}
	if id2 != id1 {
		t.Errorf("second UpsertTarget returned id=%d, want %d (drift across intermediate inserts)", id2, id1)
	}

	// Every snapshot must reference id1.
	rows, err := store.SQL().Query("SELECT target_id FROM snapshots ORDER BY collected_at")
	if err != nil {
		t.Fatalf("select snapshots: %v", err)
	}
	defer rows.Close()
	var seen int
	for rows.Next() {
		var tid int64
		if err := rows.Scan(&tid); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if tid != id1 {
			t.Errorf("snapshot row has target_id=%d, want %d", tid, id1)
		}
		seen++
	}
	if seen != 3 {
		t.Errorf("scanned %d snapshot rows, want 3", seen)
	}
}

// ---------------------------------------------------------------
// R090 — default-scope orphan filter
// ---------------------------------------------------------------

// seedWithOrphans inserts a fixture that exercises R090: 1 canonical
// target with one snapshot, plus 4 orphan snapshots whose target_id
// values do not appear in the targets table. The orphan rows
// represent the v0.3.x drift bug's leftover state on existing
// daemon stores.
func seedWithOrphans(t *testing.T, store *db.DB) (canonical int64, orphans []int64) {
	t.Helper()
	// Canonical target.
	canonical, err := store.UpsertTarget("X", tHost, tPort, tDB, tUser, tSSL, tSType, tSRef, true)
	if err != nil {
		t.Fatalf("UpsertTarget canonical: %v", err)
	}

	// One canonical snapshot.
	for i, tid := range []int64{canonical} {
		insertOneCycle(t, store, tid, ulid(t, 100+i), rfcAt(2026, 5, 7, 12, 0, 0))
	}

	// Four orphan snapshots whose target_ids do NOT match any row
	// in targets. We pick high integers to make the orphan-ness
	// obvious.
	orphans = []int64{99, 100, 101, 102}
	for i, tid := range orphans {
		insertOneCycle(t, store, tid, ulid(t, 200+i), rfcAt(2026, 5, 7, 12, i+1, 0))
	}
	return canonical, orphans
}

// insertOneCycle bypasses UpsertTarget so it can write rows with any
// target_id — including orphan ids that violate the foreign-key
// intent. Used by seedWithOrphans to simulate the v0.3.x drift bug
// in stored data without depending on the buggy code.
func insertOneCycle(t *testing.T, store *db.DB, targetID int64, snapID, collectedAt string) {
	t.Helper()
	snap := db.Snapshot{
		ID:          snapID,
		TargetID:    targetID,
		CollectedAt: collectedAt,
		PGVersion:   "PostgreSQL 18.0",
		Payload:     json.RawMessage(`{}`),
	}
	runs := []db.QueryRun{{
		ID: snapID + "-r", TargetID: targetID, SnapshotID: snapID,
		QueryID: "pg_settings_v1", CollectedAt: collectedAt,
		PGVersion: snap.PGVersion, CreatedAt: collectedAt, Status: "success",
	}}
	results := []db.QueryResult{{
		RunID: runs[0].ID, Payload: []byte("{\"k\":\"v\"}\n"), SizeBytes: 8,
	}}
	if err := store.InsertCollectionAtomic(snap, runs, results); err != nil {
		t.Fatalf("InsertCollectionAtomic: %v", err)
	}
}

// readZipNDJSONIDsFromBuf is a helper specific to this file (cannot
// import the helper from signals_export_default_scope_test.go because
// its signature is build-bound to that file's seed fixtures). Reads
// every "id" field from the named NDJSON file in the ZIP.
func readZipSnapshotIDs(t *testing.T, zr *zip.Reader) []string {
	t.Helper()
	for _, f := range zr.File {
		if f.Name != "snapshots.ndjson" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open snapshots.ndjson: %v", err)
		}
		defer rc.Close()
		var ids []string
		dec := json.NewDecoder(rc)
		for dec.More() {
			var row map[string]any
			if err := dec.Decode(&row); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if v, ok := row["id"].(string); ok {
				ids = append(ids, v)
			}
		}
		return ids
	}
	t.Fatalf("snapshots.ndjson missing")
	return nil
}

// runExport is a convenience wrapper.
func runExport(t *testing.T, store *db.DB, opts export.Options) *zip.Reader {
	t.Helper()
	builder := export.NewBuilder(store, "test-instance")
	var buf bytes.Buffer
	if err := builder.WriteTo(&buf, opts); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	return zr
}

func readMeta(t *testing.T, zr *zip.Reader) map[string]any {
	t.Helper()
	for _, f := range zr.File {
		if f.Name != "metadata.json" {
			continue
		}
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
	t.Fatalf("metadata.json missing")
	return nil
}

// TestExportDefaultScopeIgnoresOrphanedTargetIDs (TC-SIG-103, R090).
func TestExportDefaultScopeIgnoresOrphanedTargetIDs(t *testing.T) {
	store := openTestDB(t)
	_, _ = seedWithOrphans(t, store)

	zr := runExport(t, store, export.Options{})

	ids := readZipSnapshotIDs(t, zr)
	if len(ids) != 1 {
		t.Errorf("default scope returned %d snapshots, want 1 (orphans should be filtered)", len(ids))
	}
	meta := readMeta(t, zr)
	if got := int(meta["snapshot_count"].(float64)); got != 1 {
		t.Errorf("metadata.snapshot_count = %d, want 1", got)
	}
	if got := meta["ingest_mode"]; got != "analyze" {
		t.Errorf("metadata.ingest_mode = %v, want %q", got, "analyze")
	}
}

// TestExportAllStillShowsOrphans (TC-SIG-104, R085 + R090).
func TestExportAllStillShowsOrphans(t *testing.T) {
	store := openTestDB(t)
	_, _ = seedWithOrphans(t, store)

	zr := runExport(t, store, export.Options{All: true})

	ids := readZipSnapshotIDs(t, zr)
	if len(ids) != 5 {
		t.Errorf("--all returned %d snapshots, want 5 (orphans must be visible to forensic exports)", len(ids))
	}
	meta := readMeta(t, zr)
	if got := int(meta["snapshot_count"].(float64)); got != 5 {
		t.Errorf("metadata.snapshot_count = %d, want 5", got)
	}
}

// TestExportTargetIDComposesWithOrphanFilter (TC-SIG-105).
func TestExportTargetIDComposesWithOrphanFilter(t *testing.T) {
	store := openTestDB(t)
	canonical, orphans := seedWithOrphans(t, store)
	orphanID := orphans[0]

	t.Run("default+target_id=canonical → 1 snapshot", func(t *testing.T) {
		zr := runExport(t, store, export.Options{TargetID: canonical})
		if got := len(readZipSnapshotIDs(t, zr)); got != 1 {
			t.Errorf("got %d snapshots, want 1", got)
		}
	})

	t.Run("default+target_id=<orphan> → 0 snapshots", func(t *testing.T) {
		zr := runExport(t, store, export.Options{TargetID: orphanID})
		if got := len(readZipSnapshotIDs(t, zr)); got != 0 {
			t.Errorf("got %d snapshots, want 0 (orphan filter applies)", got)
		}
		meta := readMeta(t, zr)
		if got := int(meta["snapshot_count"].(float64)); got != 0 {
			t.Errorf("metadata.snapshot_count = %d, want 0", got)
		}
	})

	t.Run("--all+target_id=<orphan> → 1 snapshot (forensic)", func(t *testing.T) {
		zr := runExport(t, store, export.Options{All: true, TargetID: orphanID})
		ids := readZipSnapshotIDs(t, zr)
		if len(ids) != 1 {
			t.Errorf("got %d snapshots, want 1 (--all bypasses orphan filter)", len(ids))
		}
	})
}

// TestStatusAndDefaultExportAgreeOnTargetCount (TC-SIG-106, INV-SIGNALS-14).
// We can't easily start a real daemon in this test, so we assert the
// invariant at the data-layer: GetTargets and the default export
// scope MUST report the same count.
func TestStatusAndDefaultExportAgreeOnTargetCount(t *testing.T) {
	store := openTestDB(t)
	canonical, _ := seedWithOrphans(t, store)
	_ = canonical

	// Status side: GetTargets returns the actual targets table.
	gotTargets, err := store.GetTargets()
	if err != nil {
		t.Fatalf("GetTargets: %v", err)
	}
	statusCount := len(gotTargets)

	// Export side: default scope's distinct target_id count.
	zr := runExport(t, store, export.Options{})
	for _, f := range zr.File {
		if f.Name != "snapshots.ndjson" {
			continue
		}
		rc, _ := f.Open()
		defer rc.Close()
		seen := map[int64]bool{}
		dec := json.NewDecoder(rc)
		for dec.More() {
			var row map[string]any
			if err := dec.Decode(&row); err != nil {
				t.Fatalf("decode: %v", err)
			}
			tid, ok := row["target_id"].(float64)
			if !ok {
				t.Errorf("row %v: target_id not a number", row)
				continue
			}
			seen[int64(tid)] = true
		}
		exportCount := len(seen)

		if exportCount != statusCount {
			t.Errorf("INV-SIGNALS-14 violation: signalsctl-status target count = %d, default-export distinct target_ids = %d",
				statusCount, exportCount)
		}
		return
	}
	t.Fatal("snapshots.ndjson missing from export")
}

// TestSqliteSequenceTargetsBoundedByActualRows (TC-SIG-107, R089
// sentinel). Documents that sqlite_sequence.targets may grow past 1
// (SQLite quirk) but the fix in R089 makes that growth harmless —
// targets table stays at 1 row, returned id stays canonical.
func TestSqliteSequenceTargetsBoundedByActualRows(t *testing.T) {
	store := openTestDB(t)
	const n = 50
	ids := upsertSameTargetN(t, store, n)

	// targets table has 1 row.
	var rowCount int
	if err := store.SQL().QueryRow("SELECT COUNT(*) FROM targets WHERE name = ?", tName).Scan(&rowCount); err != nil {
		t.Fatalf("count: %v", err)
	}
	if rowCount != 1 {
		t.Errorf("after %d upserts, targets has %d rows, want 1", n, rowCount)
	}

	// All returned ids equal the canonical row.
	for i, id := range ids {
		if id != ids[0] {
			t.Errorf("upsert %d returned id=%d, want %d (R089 idempotency)", i, id, ids[0])
		}
	}

	// Document the sqlite_sequence quirk — DO NOT assert it stays
	// at 1, because SQLite legitimately bumps it on every UPSERT
	// regardless of branch. The R089 fix is to NOT trust that
	// counter, not to suppress its growth.
	var seq sql.NullInt64
	_ = store.SQL().QueryRow("SELECT seq FROM sqlite_sequence WHERE name = ?", "targets").Scan(&seq)
	t.Logf("sqlite_sequence.targets after %d upserts = %v (informational only — the R089 fix tolerates this)", n, seq)
}

// ---------------------------------------------------------------
// Helpers — tiny ULIDs + RFC3339 timestamps for deterministic IDs.
// ---------------------------------------------------------------

// ulid produces a deterministic-looking 26-char string suitable as a
// snapshot id in tests. Real ULIDs in the daemon are time-sortable;
// here we just need stable distinct ids.
func ulid(t *testing.T, n int) string {
	t.Helper()
	const tmpl = "01TEST00000000000000000000"
	if n < 0 || n > 999 {
		t.Fatalf("ulid n out of range: %d", n)
	}
	// Replace last 3 chars of the template with the integer.
	suffix := []byte{'0', '0', '0'}
	for i := 2; i >= 0 && n > 0; i-- {
		suffix[i] = byte('0' + (n % 10))
		n /= 10
	}
	return tmpl[:len(tmpl)-3] + string(suffix)
}

// rfcAt produces a deterministic RFC3339 timestamp.
func rfcAt(year, month, day, hour, minute, second int) string {
	// Hand-format to avoid pulling time.Time into hot paths.
	return fmtTime(year, month, day, hour, minute, second)
}

func fmtTime(y, m, d, hh, mm, ss int) string {
	pad2 := func(v int) string {
		if v < 10 {
			return "0" + itoa(v)
		}
		return itoa(v)
	}
	pad4 := func(v int) string {
		s := itoa(v)
		for len(s) < 4 {
			s = "0" + s
		}
		return s
	}
	return pad4(y) + "-" + pad2(m) + "-" + pad2(d) + "T" +
		pad2(hh) + ":" + pad2(mm) + ":" + pad2(ss) + "Z"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	if n < 0 {
		return "-" + itoa(-n)
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// silence unused-import linter when filepath isn't used
var _ = filepath.Clean
