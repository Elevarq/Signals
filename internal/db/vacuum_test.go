package db

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestVacuumReclaimsSpace is the #443 regression: after retention deletes
// rows, SQLite retains the freed pages until VACUUM. SizeBytes must shrink
// after Vacuum, proving the store no longer grows monotonically under
// active retention.
func TestVacuumReclaimsSpace(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "vacuum.db"), false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	tid, err := store.UpsertTarget("t1", "localhost", 5432, "postgres", "signals", "prefer", "", "", true)
	if err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}

	// Insert enough sizable snapshots to grow the file well past its
	// initial page count. Dated in the past so a recent cutoff deletes them.
	old := time.Now().UTC().AddDate(0, 0, -100).Format(time.RFC3339)
	blob := json.RawMessage(`{"pad":"` + strings.Repeat("x", 4096) + `"}`)
	for i := 0; i < 200; i++ {
		if err := store.InsertSnapshot(Snapshot{
			ID: "snap-" + strings.Repeat("0", 3) + itoa(i), TargetID: tid,
			CollectedAt: old, PGVersion: "17", Payload: blob, SizeBytes: len(blob),
		}); err != nil {
			t.Fatalf("InsertSnapshot %d: %v", i, err)
		}
	}

	grown, err := store.SizeBytes()
	if err != nil {
		t.Fatalf("SizeBytes: %v", err)
	}

	cutoff := time.Now().UTC().AddDate(0, 0, -1).Format(time.RFC3339)
	if _, err := store.DeleteSnapshotsOlderThan(cutoff); err != nil {
		t.Fatalf("DeleteSnapshotsOlderThan: %v", err)
	}

	// Without VACUUM the file does not shrink — freed pages are retained.
	afterDelete, err := store.SizeBytes()
	if err != nil {
		t.Fatalf("SizeBytes: %v", err)
	}
	if afterDelete < grown/2 {
		t.Fatalf("file unexpectedly shrank before VACUUM (%d -> %d); test assumption broken", grown, afterDelete)
	}

	if err := store.Vacuum(); err != nil {
		t.Fatalf("Vacuum: %v", err)
	}
	afterVacuum, err := store.SizeBytes()
	if err != nil {
		t.Fatalf("SizeBytes: %v", err)
	}
	if afterVacuum >= afterDelete {
		t.Errorf("VACUUM did not reclaim space: before=%d afterDelete=%d afterVacuum=%d",
			grown, afterDelete, afterVacuum)
	}
}

// itoa is a tiny local int->string to avoid importing strconv just for
// test row ids.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
