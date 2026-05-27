package tests

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/elevarq/arq-signals/internal/db"
	"github.com/elevarq/arq-signals/internal/export"
)

// ---------------------------------------------------------------------------
// R094 — export metadata.json carries a target_identity block sourced from
// the daemon's targets row. Connection identity only (host/port/dbname/
// username); never password, secret reference, or sslmode.
//
// Specification:  features/arq-signals/specification.md § R094
// Appendix:       features/arq-signals/appendix-a-api-contract.md
// Tests covered:  TC-SIG-118, TC-SIG-119, TC-SIG-120, TC-SIG-121
// ---------------------------------------------------------------------------

// seedSingleTargetSnapshot inserts one target + one snapshot owned by it.
// Returns the target ID.
func seedSingleTargetSnapshot(t *testing.T, store *db.DB) int64 {
	t.Helper()
	targetID, err := store.UpsertTarget(
		"prod-db", "prod.example.com", 5432, "app", "arq_signals_ro",
		"require", "FILE", "/etc/arq/prod.pw", true,
	)
	if err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	snap := db.Snapshot{
		ID:          "snap-target-identity-001",
		TargetID:    targetID,
		CollectedAt: now,
		PGVersion:   "PostgreSQL 16.2",
		Payload:     json.RawMessage(`{"version":"PostgreSQL 16.2"}`),
		SizeBytes:   42,
	}
	if err := store.InsertSnapshot(snap); err != nil {
		t.Fatalf("InsertSnapshot: %v", err)
	}
	return targetID
}

func buildScopedExportZIP(t *testing.T, store *db.DB, targetID int64) map[string]any {
	t.Helper()
	builder := export.NewBuilder(store, "test-instance-id")
	var buf bytes.Buffer
	if err := builder.WriteTo(&buf, export.Options{TargetID: targetID}); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	return readZipFileJSON(t, zr, "metadata.json")
}

// TC-SIG-118: single-target export embeds target_identity in metadata.json
// with host, port, dbname, username sourced from the daemon's targets row.
func TestExportMetadataCarriesTargetIdentityForSingleTarget(t *testing.T) {
	store := openTestDB(t)
	targetID := seedSingleTargetSnapshot(t, store)

	meta := buildScopedExportZIP(t, store, targetID)

	raw, ok := meta["target_identity"]
	if !ok {
		t.Fatal("metadata.json missing target_identity for single-target export")
	}
	ident, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("target_identity must be an object, got %T", raw)
	}

	if got := ident["host"]; got != "prod.example.com" {
		t.Errorf("target_identity.host: got %v, want %q", got, "prod.example.com")
	}
	// JSON numbers decode to float64.
	if got, ok := ident["port"].(float64); !ok || int(got) != 5432 {
		t.Errorf("target_identity.port: got %v, want 5432", ident["port"])
	}
	if got := ident["dbname"]; got != "app" {
		t.Errorf("target_identity.dbname: got %v, want %q", got, "app")
	}
	if got := ident["username"]; got != "arq_signals_ro" {
		t.Errorf("target_identity.username: got %v, want %q", got, "arq_signals_ro")
	}
}

// TC-SIG-119: unscoped (multi-target / instance-level) export omits the
// top-level target_identity block. Per-snapshot identity lives in
// snapshots.ndjson when present, but metadata.json must not advertise a
// single target.
func TestExportMetadataOmitsTargetIdentityForUnscopedExport(t *testing.T) {
	store := openTestDB(t)
	// Seed two distinct targets so the export is genuinely multi-target.
	if _, err := store.UpsertTarget(
		"prod-db", "prod.example.com", 5432, "app", "arq_signals_ro",
		"require", "FILE", "/etc/arq/prod.pw", true,
	); err != nil {
		t.Fatalf("UpsertTarget prod: %v", err)
	}
	if _, err := store.UpsertTarget(
		"staging-db", "staging.example.com", 5432, "app", "arq_signals_ro",
		"require", "FILE", "/etc/arq/staging.pw", true,
	); err != nil {
		t.Fatalf("UpsertTarget staging: %v", err)
	}

	_, zr := buildExportZIP(t, store)
	meta := readZipFileJSON(t, zr, "metadata.json")

	if _, present := meta["target_identity"]; present {
		t.Error("metadata.json must NOT carry target_identity at the top level for an unscoped export")
	}
}

// TC-SIG-120: target_identity carries connection identity only — no
// password, secret reference, or sslmode (INV-SIGNALS-07).
func TestExportMetadataTargetIdentityCarriesNoAuthMaterial(t *testing.T) {
	store := openTestDB(t)
	targetID := seedSingleTargetSnapshot(t, store)

	meta := buildScopedExportZIP(t, store, targetID)

	raw, ok := meta["target_identity"].(map[string]any)
	if !ok {
		t.Fatal("metadata.json missing target_identity")
	}
	forbidden := []string{"password", "secret", "secret_ref", "secret_type", "sslmode"}
	for _, key := range forbidden {
		if _, present := raw[key]; present {
			t.Errorf("target_identity must NOT carry %q (auth/secret material)", key)
		}
	}

	// Also: the serialised metadata.json bytes must not mention the
	// secret reference even via a stray field.
	asBytes, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("re-marshal metadata: %v", err)
	}
	if containsCI(string(asBytes), "/etc/arq/prod.pw") {
		t.Error("metadata.json must not leak the secret reference path")
	}
}

// TC-SIG-121: orphan snapshot (target_id does not resolve in targets)
// omits target_identity for that snapshot.
func TestExportMetadataOmitsTargetIdentityForOrphanSnapshot(t *testing.T) {
	store := openTestDB(t)

	// Insert a snapshot pointing at a target_id that does NOT exist in
	// the targets table — the R090 orphan case.
	now := time.Now().UTC().Format(time.RFC3339)
	orphan := db.Snapshot{
		ID:          "snap-orphan-001",
		TargetID:    999999, // no such target row
		CollectedAt: now,
		PGVersion:   "PostgreSQL 16.2",
		Payload:     json.RawMessage(`{"version":"PostgreSQL 16.2"}`),
		SizeBytes:   42,
	}
	if err := store.InsertSnapshot(orphan); err != nil {
		t.Fatalf("InsertSnapshot: %v", err)
	}

	builder := export.NewBuilder(store, "test-instance-id")
	var buf bytes.Buffer
	if err := builder.WriteTo(&buf, export.Options{TargetID: 999999}); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	meta := readZipFileJSON(t, zr, "metadata.json")

	if _, present := meta["target_identity"]; present {
		t.Error("metadata.json must NOT carry target_identity for an orphan snapshot (target_id does not resolve)")
	}
}

// readZipNDJSONRows decodes every line of an ndjson member in a ZIP
// into map[string]any. Used by the multi-target tests below which
// need to inspect nested objects (target_identity) per row, not just
// scalar fields.
func readZipNDJSONRows(t *testing.T, zr *zip.Reader, name string) []map[string]any {
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
		data, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		var rows []map[string]any
		for _, line := range bytes.Split(data, []byte("\n")) {
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			var row map[string]any
			if err := json.Unmarshal(line, &row); err != nil {
				t.Fatalf("decode line in %s: %v", name, err)
			}
			rows = append(rows, row)
		}
		return rows
	}
	t.Fatalf("file %s not found in ZIP", name)
	return nil
}

// TC-SIG-122: multi-target export carries per-snapshot target_identity
// in snapshots.ndjson (R094 spec line 1365). Closes the gap that PR
// #45 left when it implemented only the top-level metadata.json block.
func TestExportSnapshotsCarryPerSnapshotTargetIdentityForMultiTarget(t *testing.T) {
	store := openTestDB(t)

	// Seed two distinct targets, each with one snapshot.
	prodID, err := store.UpsertTarget(
		"prod-db", "prod.example.com", 5432, "app", "arq_signals_ro",
		"require", "FILE", "/etc/arq/prod.pw", true,
	)
	if err != nil {
		t.Fatalf("UpsertTarget prod: %v", err)
	}
	stagingID, err := store.UpsertTarget(
		"staging-db", "staging.example.com", 5433, "app_stg", "arq_signals_ro",
		"require", "FILE", "/etc/arq/staging.pw", true,
	)
	if err != nil {
		t.Fatalf("UpsertTarget staging: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	for _, snap := range []db.Snapshot{
		{ID: "snap-prod-001", TargetID: prodID, CollectedAt: now, PGVersion: "PostgreSQL 16.2", Payload: json.RawMessage(`{}`), SizeBytes: 42},
		{ID: "snap-staging-001", TargetID: stagingID, CollectedAt: now, PGVersion: "PostgreSQL 16.2", Payload: json.RawMessage(`{}`), SizeBytes: 42},
	} {
		if err := store.InsertSnapshot(snap); err != nil {
			t.Fatalf("InsertSnapshot %s: %v", snap.ID, err)
		}
	}

	_, zr := buildExportZIP(t, store)
	rows := readZipNDJSONRows(t, zr, "snapshots.ndjson")
	if len(rows) < 2 {
		t.Fatalf("expected at least 2 snapshot rows, got %d", len(rows))
	}

	// Map snapshot id -> expected identity to verify each row carries
	// the right identity, not just any identity.
	want := map[string]map[string]any{
		"snap-prod-001":    {"host": "prod.example.com", "port": float64(5432), "dbname": "app", "username": "arq_signals_ro"},
		"snap-staging-001": {"host": "staging.example.com", "port": float64(5433), "dbname": "app_stg", "username": "arq_signals_ro"},
	}

	for _, row := range rows {
		id, _ := row["id"].(string)
		expected, tracked := want[id]
		if !tracked {
			continue // some other snapshot the test doesn't care about
		}
		raw, ok := row["target_identity"]
		if !ok {
			t.Errorf("snapshot %q missing target_identity in snapshots.ndjson", id)
			continue
		}
		got, ok := raw.(map[string]any)
		if !ok {
			t.Errorf("snapshot %q target_identity must be an object, got %T", id, raw)
			continue
		}
		for k, want := range expected {
			if got[k] != want {
				t.Errorf("snapshot %q target_identity.%s: got %v, want %v", id, k, got[k], want)
			}
		}
		// Auth material must not leak (INV-SIGNALS-07).
		for _, forbidden := range []string{"password", "secret", "secret_ref", "secret_type", "sslmode"} {
			if _, present := got[forbidden]; present {
				t.Errorf("snapshot %q target_identity must NOT carry %q (auth/secret material)", id, forbidden)
			}
		}
	}
}

// Issue #52: a transient DB error during the target_identity lookup
// must fail the export, not silently produce rows with absent
// identity (which is indistinguishable from genuine orphans). The
// previous implementation cached `nil` for any error, poisoning the
// per-target cache and producing a misleadingly-clean ZIP.
//
// We trigger a non-ErrNoRows error by DROPping the `targets` table
// after the snapshot is inserted but before WriteTo runs. The
// subsequent SELECT against `targets` then returns "no such table"
// rather than `sql.ErrNoRows` -- a transient-style error that the
// fix must surface, not swallow.
func TestExportSnapshotsFailsOnNonOrphanTargetIdentityError(t *testing.T) {
	store := openTestDB(t)

	prodID, err := store.UpsertTarget(
		"prod-db", "prod.example.com", 5432, "app", "arq_signals_ro",
		"require", "FILE", "/etc/arq/prod.pw", true,
	)
	if err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if err := store.InsertSnapshot(db.Snapshot{
		ID: "snap-prod-001", TargetID: prodID, CollectedAt: now,
		PGVersion: "PostgreSQL 16.2", Payload: json.RawMessage(`{}`), SizeBytes: 42,
	}); err != nil {
		t.Fatalf("InsertSnapshot: %v", err)
	}

	// Nuke the targets table so GetTargetIdentity returns a
	// non-ErrNoRows error ("no such table").
	if _, err := store.SQL().Exec("DROP TABLE targets"); err != nil {
		t.Fatalf("DROP TABLE targets: %v", err)
	}

	builder := export.NewBuilder(store, "test-instance-id")
	var buf bytes.Buffer
	err = builder.WriteTo(&buf, export.Options{All: true})
	if err == nil {
		t.Fatalf("WriteTo must fail when target_identity lookup hits a non-orphan error; got nil")
	}
	if !strings.Contains(err.Error(), "target_identity") {
		t.Errorf("error must reference target_identity to point operator at the cause; got %q", err.Error())
	}
}

// TC-SIG-123: orphan snapshot in a multi-target export omits the
// target_identity field (target_id does not resolve to a row in
// targets — the R090 case). Non-orphan rows still carry their
// identity.
func TestExportSnapshotsOmitsTargetIdentityForOrphanRowInMultiTarget(t *testing.T) {
	store := openTestDB(t)

	// One real target, one orphan snapshot pointing at a target_id
	// that does not exist.
	prodID, err := store.UpsertTarget(
		"prod-db", "prod.example.com", 5432, "app", "arq_signals_ro",
		"require", "FILE", "/etc/arq/prod.pw", true,
	)
	if err != nil {
		t.Fatalf("UpsertTarget prod: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	for _, snap := range []db.Snapshot{
		{ID: "snap-prod-001", TargetID: prodID, CollectedAt: now, PGVersion: "PostgreSQL 16.2", Payload: json.RawMessage(`{}`), SizeBytes: 42},
		{ID: "snap-orphan-multi", TargetID: 999999, CollectedAt: now, PGVersion: "PostgreSQL 16.2", Payload: json.RawMessage(`{}`), SizeBytes: 42},
	} {
		if err := store.InsertSnapshot(snap); err != nil {
			t.Fatalf("InsertSnapshot %s: %v", snap.ID, err)
		}
	}

	// Use the --all selector so orphans surface (default scope filters
	// them out per R090).
	builder := export.NewBuilder(store, "test-instance-id")
	var buf bytes.Buffer
	if err := builder.WriteTo(&buf, export.Options{All: true}); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	rows := readZipNDJSONRows(t, zr, "snapshots.ndjson")

	var sawOrphan, sawProd bool
	for _, row := range rows {
		id, _ := row["id"].(string)
		switch id {
		case "snap-orphan-multi":
			sawOrphan = true
			if _, present := row["target_identity"]; present {
				t.Error("orphan snapshot row must NOT carry target_identity (target_id does not resolve)")
			}
		case "snap-prod-001":
			sawProd = true
			if _, present := row["target_identity"]; !present {
				t.Error("non-orphan snapshot row must carry target_identity in a multi-target export")
			}
		}
	}
	if !sawOrphan {
		t.Error("orphan snapshot row missing from --all export")
	}
	if !sawProd {
		t.Error("non-orphan snapshot row missing from --all export")
	}
}
