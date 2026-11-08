package tests

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/elevarq/signals/internal/db"
	"github.com/elevarq/signals/internal/export"
)

// buildExportZIPAll writes a forensic --all export (packages every snapshot,
// including differing collection-time policy across cycles) and returns a zip
// reader.
func buildExportZIPAll(t *testing.T, store *db.DB) *zip.Reader {
	t.Helper()
	builder := export.NewBuilder(store, "test-instance-id")
	var buf bytes.Buffer
	if err := builder.WriteTo(&buf, export.Options{All: true}); err != nil {
		t.Fatalf("WriteTo(All): %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	return zr
}

// #729 (audit A3) — export metadata must report collection-time sensitivity /
// unsafe-mode provenance HONESTLY, aggregated per packaged snapshot, instead of
// stamping the current exporter's policy onto historical evidence. A current
// "false" flag must never be advertised over mixed or unknown history.
//
// Spec: specifications/export-collection-provenance.md (CTP-R001..R005).

func ctpBoolPtr(b bool) *bool { return &b }

// seedPolicySnapshot inserts a snapshot carrying a specific collection-time
// policy (nil pointer = unknown / legacy).
func seedPolicySnapshot(t *testing.T, store *db.DB, id string, targetID int64, collectedAt string, unsafe, highSens *bool) {
	t.Helper()
	snap := db.Snapshot{
		ID:                     id,
		TargetID:               targetID,
		CollectedAt:            collectedAt,
		PGVersion:              "PostgreSQL 16.2",
		Payload:                json.RawMessage(`{"version":"PostgreSQL 16.2"}`),
		SizeBytes:              42,
		SourceHost:             "localhost",
		SourcePort:             5432,
		SourceDB:               "testdb",
		SourceUser:             "elevarq",
		CollectUnsafeMode:      unsafe,
		CollectHighSensitivity: highSens,
	}
	if err := store.InsertSnapshot(snap); err != nil {
		t.Fatalf("InsertSnapshot(%s): %v", id, err)
	}
}

func ctpTarget(t *testing.T, store *db.DB) int64 {
	t.Helper()
	id, err := store.UpsertTarget("test-pg", "localhost", 5432, "testdb", "elevarq", "disable", "NONE", "", true)
	if err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}
	return id
}

func ctpExportMetadata(t *testing.T, store *db.DB) map[string]any {
	t.Helper()
	zr := buildExportZIPAll(t, store)
	return readZipFileJSON(t, zr, "metadata.json")
}

func ctpCollectionPolicy(t *testing.T, meta map[string]any) map[string]any {
	t.Helper()
	cp, ok := meta["collection_policy"].(map[string]any)
	if !ok {
		t.Fatalf("metadata.json missing collection_policy object; got %v", meta["collection_policy"])
	}
	return cp
}

// AC-CTP-01 — a single snapshot collected with a known policy reports that
// exact value (uniform).
func TestCTP_UniformPolicyReported(t *testing.T) {
	store := openTestDB(t)
	tid := ctpTarget(t, store)
	seedPolicySnapshot(t, store, "snap-1", tid, time.Now().UTC().Format(time.RFC3339), ctpBoolPtr(false), ctpBoolPtr(true))

	cp := ctpCollectionPolicy(t, ctpExportMetadata(t, store))
	if cp["unsafe_mode"] != "false" {
		t.Errorf("unsafe_mode: want \"false\", got %v", cp["unsafe_mode"])
	}
	if cp["high_sensitivity_collectors_enabled"] != "true" {
		t.Errorf("high_sensitivity: want \"true\", got %v", cp["high_sensitivity_collectors_enabled"])
	}
}

// AC-CTP-02 — snapshots collected under DIFFERENT high-sensitivity policy
// aggregate to "mixed", even when the exporter's own flag is false. This is
// the core defect: a current "false" cannot erase the fact that history
// contains high-sensitivity content.
func TestCTP_MixedHistoryNotFalselyClean(t *testing.T) {
	store := openTestDB(t)
	tid := ctpTarget(t, store)
	base := time.Now().UTC()
	// Older cycle collected WITH high-sensitivity; newer WITHOUT.
	seedPolicySnapshot(t, store, "snap-old", tid, base.Add(-time.Hour).Format(time.RFC3339), ctpBoolPtr(false), ctpBoolPtr(true))
	seedPolicySnapshot(t, store, "snap-new", tid, base.Format(time.RFC3339), ctpBoolPtr(false), ctpBoolPtr(false))

	cp := ctpCollectionPolicy(t, ctpExportMetadata(t, store))
	if cp["high_sensitivity_collectors_enabled"] != "mixed" {
		t.Errorf("mixed high-sensitivity history must report \"mixed\", got %v", cp["high_sensitivity_collectors_enabled"])
	}
}

// AC-CTP-03 — a snapshot with unknown (unrecorded / legacy) collection-time
// policy reports "unknown" and NEVER a fabricated true/false.
func TestCTP_UnknownLegacyNotInvented(t *testing.T) {
	store := openTestDB(t)
	tid := ctpTarget(t, store)
	// nil pointers simulate a pre-#729 snapshot: policy columns are NULL.
	seedPolicySnapshot(t, store, "snap-legacy", tid, time.Now().UTC().Format(time.RFC3339), nil, nil)

	cp := ctpCollectionPolicy(t, ctpExportMetadata(t, store))
	if cp["unsafe_mode"] != "unknown" {
		t.Errorf("legacy unsafe_mode must be \"unknown\", got %v", cp["unsafe_mode"])
	}
	if cp["high_sensitivity_collectors_enabled"] != "unknown" {
		t.Errorf("legacy high_sensitivity must be \"unknown\", got %v", cp["high_sensitivity_collectors_enabled"])
	}
}

// AC-CTP-04 — a recorded value coexisting with an unknown is "mixed", so the
// export never advertises false certainty when part of its content is unknown.
func TestCTP_KnownPlusUnknownIsMixed(t *testing.T) {
	store := openTestDB(t)
	tid := ctpTarget(t, store)
	base := time.Now().UTC()
	seedPolicySnapshot(t, store, "snap-known", tid, base.Format(time.RFC3339), ctpBoolPtr(false), ctpBoolPtr(false))
	seedPolicySnapshot(t, store, "snap-unknown", tid, base.Add(-time.Hour).Format(time.RFC3339), nil, nil)

	cp := ctpCollectionPolicy(t, ctpExportMetadata(t, store))
	if cp["unsafe_mode"] != "mixed" {
		t.Errorf("known+unknown unsafe_mode must be \"mixed\", got %v", cp["unsafe_mode"])
	}
}

// AC-CTP-05 — snapshots.ndjson carries the per-snapshot collection-time policy,
// tri-state (true/false/null), so multi-cycle consumers attribute provenance
// per cycle rather than from the top-level/current flag.
func TestCTP_SnapshotNdjsonCarriesPerCyclePolicy(t *testing.T) {
	store := openTestDB(t)
	tid := ctpTarget(t, store)
	base := time.Now().UTC()
	seedPolicySnapshot(t, store, "snap-hs", tid, base.Format(time.RFC3339), ctpBoolPtr(false), ctpBoolPtr(true))
	seedPolicySnapshot(t, store, "snap-legacy", tid, base.Add(-time.Hour).Format(time.RFC3339), nil, nil)

	// Forensic --all scope packages every snapshot including differing policy.
	zr := buildExportZIPAll(t, store)
	rows := readZipNDJSONRows(t, zr, "snapshots.ndjson")

	byID := map[string]map[string]any{}
	for _, r := range rows {
		byID[r["id"].(string)] = r
	}
	hs := byID["snap-hs"]
	if hs == nil {
		t.Fatal("snap-hs missing from snapshots.ndjson")
	}
	if hs["collect_high_sensitivity"] != true {
		t.Errorf("snap-hs collect_high_sensitivity: want true, got %v", hs["collect_high_sensitivity"])
	}
	if hs["collect_unsafe_mode"] != false {
		t.Errorf("snap-hs collect_unsafe_mode: want false, got %v", hs["collect_unsafe_mode"])
	}
	legacy := byID["snap-legacy"]
	if legacy == nil {
		t.Fatal("snap-legacy missing from snapshots.ndjson")
	}
	// Unknown encodes as JSON null (absent/explicit nil), never a fabricated value.
	if v, present := legacy["collect_high_sensitivity"]; present && v != nil {
		t.Errorf("legacy collect_high_sensitivity must be null, got %v", v)
	}
}
