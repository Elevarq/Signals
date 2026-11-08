package db

import (
	"path/filepath"
	"testing"
)

// #726 (F19) — the `events` audit log was inserted every cycle but never
// pruned, so a configured diagnostic retention did not bound the store.
// DeleteEventsOlderThan prunes events strictly older than a cutoff, preserving
// recent incident evidence inside the window.
//
// Spec: specifications/audit-event-retention.md (AER-R001..R003).

func openEventStore(t *testing.T) *DB {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "events.db"), false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	// Clear the migration-seeded baseline event so fixtures are the only rows.
	if _, err := store.SQL().Exec("DELETE FROM events"); err != nil {
		t.Fatalf("clear events: %v", err)
	}
	return store
}

func insertEventAt(t *testing.T, store *DB, ts, eventType string) {
	t.Helper()
	if _, err := store.SQL().Exec(
		"INSERT INTO events (timestamp, event_type, detail) VALUES (?, ?, '')", ts, eventType,
	); err != nil {
		t.Fatalf("insert event %s@%s: %v", eventType, ts, err)
	}
}

func countEvents(t *testing.T, store *DB) int {
	t.Helper()
	var n int
	if err := store.SQL().QueryRow("SELECT COUNT(*) FROM events").Scan(&n); err != nil {
		t.Fatalf("count events: %v", err)
	}
	return n
}

// AER-R001 — old events are pruned; recent events inside the window are kept.
func TestDeleteEventsOlderThan_PrunesOldKeepsRecent(t *testing.T) {
	store := openEventStore(t)
	insertEventAt(t, store, "2020-01-01T00:00:00Z", "signals_started")
	insertEventAt(t, store, "2020-06-01T00:00:00Z", "snapshot_collected")
	insertEventAt(t, store, "2099-01-01T00:00:00Z", "snapshot_collected") // recent

	deleted, err := store.DeleteEventsOlderThan("2025-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("DeleteEventsOlderThan: %v", err)
	}
	if deleted != 2 {
		t.Errorf("expected 2 old events pruned, got %d", deleted)
	}
	if n := countEvents(t, store); n != 1 {
		t.Errorf("expected 1 recent event preserved, got %d", n)
	}
}

// AER-R001 (boundary) — the cutoff is strict (`<`): an event exactly at the
// cutoff timestamp is preserved.
func TestDeleteEventsOlderThan_CutoffIsStrict(t *testing.T) {
	store := openEventStore(t)
	insertEventAt(t, store, "2025-01-01T00:00:00Z", "at_cutoff")
	insertEventAt(t, store, "2024-12-31T23:59:59Z", "before_cutoff")

	deleted, err := store.DeleteEventsOlderThan("2025-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("DeleteEventsOlderThan: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected only the strictly-older event pruned, got %d deleted", deleted)
	}
	if n := countEvents(t, store); n != 1 {
		t.Errorf("event exactly at the cutoff must be preserved; remaining=%d", n)
	}
}
