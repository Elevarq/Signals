package db

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// #720 / F09 / MTE-R010: since/until export selectors must compare by INSTANT,
// not by RFC3339 text. collected_at is stored canonical UTC second-resolution;
// the store normalizes each bound to that form (since rounds up, until down)
// so equivalent offsets and fractional-second bounds select identical evidence.

func TestCanonicalSinceBound(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"2026-11-07T10:00:00Z", "2026-11-07T10:00:00Z"},
		{"2026-11-07T11:00:00+01:00", "2026-11-07T10:00:00Z"}, // offset collapses to Z
		{"2026-11-07T10:00:00.000Z", "2026-11-07T10:00:00Z"},  // no sub-second -> unchanged second
		{"2026-11-07T10:00:00.500Z", "2026-11-07T10:00:01Z"},  // lower bound rounds UP
		{"2026-11-07T10:00:00.001Z", "2026-11-07T10:00:01Z"},  // any sub-second rounds UP
		{"not-a-time", "not-a-time"},                          // unparseable passthrough
	}
	for _, c := range cases {
		if got := canonicalSinceBound(c.in); got != c.want {
			t.Errorf("canonicalSinceBound(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCanonicalUntilBound(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"2026-11-07T10:00:00Z", "2026-11-07T10:00:00Z"},
		{"2026-11-07T11:00:00+01:00", "2026-11-07T10:00:00Z"}, // offset collapses to Z
		{"2026-11-07T10:00:00.000Z", "2026-11-07T10:00:00Z"},  // fractional truncates DOWN
		{"2026-11-07T10:00:00.999Z", "2026-11-07T10:00:00Z"},  // upper bound truncates DOWN
		{"not-a-time", "not-a-time"},
	}
	for _, c := range cases {
		if got := canonicalUntilBound(c.in); got != c.want {
			t.Errorf("canonicalUntilBound(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// openStoreWithSnapshot returns a migrated store holding exactly one snapshot
// for a single target, collected at the given canonical UTC timestamp.
func openStoreWithSnapshot(t *testing.T, collectedAt string) (*DB, int64) {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "t.db"), false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	tid, err := store.UpsertTarget("t1", "h", 5432, "app", "u", "verify-full", "", "", true)
	if err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}
	if err := store.InsertSnapshot(Snapshot{
		ID: "snap-1", TargetID: tid, CollectedAt: collectedAt,
		PGVersion: "18", Payload: json.RawMessage(`{}`), SizeBytes: 2,
	}); err != nil {
		t.Fatalf("InsertSnapshot: %v", err)
	}
	return store, tid
}

// The reported repro: a stored 10:00:00Z snapshot is selected by an equivalent
// +01:00 lower bound (same instant) — previously excluded by lexical compare.
func TestTimeRangeSelection_EquivalentOffsetIncluded(t *testing.T) {
	store, tid := openStoreWithSnapshot(t, "2026-11-07T10:00:00Z")

	all, err := store.GetAllSnapshots("2026-11-07T11:00:00+01:00", "")
	if err != nil {
		t.Fatalf("GetAllSnapshots: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("GetAllSnapshots(since=+01:00 equivalent): got %d snapshots, want 1 (equivalent instant must select it)", len(all))
	}

	byTarget, err := store.GetSnapshotsByTarget(tid, "2026-11-07T11:00:00+01:00", "")
	if err != nil {
		t.Fatalf("GetSnapshotsByTarget: %v", err)
	}
	if len(byTarget) != 1 {
		t.Errorf("GetSnapshotsByTarget(since=+01:00 equivalent): got %d, want 1", len(byTarget))
	}
}

// Inclusive upper bound with fractional seconds must still include an
// exactly-equal whole-second stored timestamp.
func TestTimeRangeSelection_FractionalUntilInclusive(t *testing.T) {
	store, _ := openStoreWithSnapshot(t, "2026-11-07T10:00:00Z")
	got, err := store.GetAllSnapshots("", "2026-11-07T10:00:00.000Z")
	if err != nil {
		t.Fatalf("GetAllSnapshots: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("until=10:00:00.000Z must include the equal 10:00:00Z snapshot, got %d", len(got))
	}
}

// A sub-second lower bound is strictly after the stored whole second, so it
// must EXCLUDE it (round up — do not wrongly include a prior second).
func TestTimeRangeSelection_SubSecondSinceExcludes(t *testing.T) {
	store, _ := openStoreWithSnapshot(t, "2026-11-07T10:00:00Z")
	got, err := store.GetAllSnapshots("2026-11-07T10:00:00.500Z", "")
	if err != nil {
		t.Fatalf("GetAllSnapshots: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("since=10:00:00.500Z is after the 10:00:00Z instant — must exclude it, got %d", len(got))
	}
}

// A same-instant inclusive window (since==until==the stored instant, expressed
// with an offset) selects the snapshot.
func TestTimeRangeSelection_EqualInstantWindow(t *testing.T) {
	store, _ := openStoreWithSnapshot(t, "2026-11-07T10:00:00Z")
	got, err := store.GetAllSnapshots("2026-11-07T11:00:00+01:00", "2026-11-07T11:00:00+01:00")
	if err != nil {
		t.Fatalf("GetAllSnapshots: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("equal-instant window (offset form) must select the snapshot, got %d", len(got))
	}
}
