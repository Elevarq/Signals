package collector

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/elevarq/signals/internal/db"
)

// #726 (F19) — the collector's cleanup pass must prune the `events` audit log by
// its own bound (auditRetentionDays), distinct from diagnostic retention, so a
// long-lived daemon does not grow the store unbounded. 0 = keep forever.
//
// Spec: specifications/audit-event-retention.md (AER-R002, AER-R003).

func eventStore(t *testing.T) *db.DB {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "cleanup.db"), false)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
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

func seedEvent(t *testing.T, store *db.DB, ts, kind string) {
	t.Helper()
	if _, err := store.SQL().Exec(
		"INSERT INTO events (timestamp, event_type, detail) VALUES (?, ?, '')", ts, kind,
	); err != nil {
		t.Fatalf("seed event: %v", err)
	}
}

func eventCount(t *testing.T, store *db.DB) int {
	t.Helper()
	var n int
	if err := store.SQL().QueryRow("SELECT COUNT(*) FROM events").Scan(&n); err != nil {
		t.Fatalf("count events: %v", err)
	}
	return n
}

// AER-R002 — cleanup prunes events older than auditRetentionDays and keeps
// recent ones, even when diagnostic retention is disabled (retentionDays=0).
func TestCleanup_PrunesAuditEventsByOwnBound(t *testing.T) {
	store := eventStore(t)
	old := time.Now().UTC().AddDate(0, 0, -200).Format(time.RFC3339)
	recent := time.Now().UTC().AddDate(0, 0, -1).Format(time.RFC3339)
	seedEvent(t, store, old, "snapshot_collected")
	seedEvent(t, store, recent, "snapshot_collected")

	// retentionDays=0 (diagnostic off) but audit retention = 90 days.
	c := New(store, nil, time.Hour, 0, WithAuditRetentionDays(90))
	c.cleanup()

	if n := eventCount(t, store); n != 1 {
		t.Errorf("cleanup should prune the >90d-old event and keep the recent one; remaining=%d", n)
	}
}

// AER-R003 — auditRetentionDays <= 0 keeps events forever (explicit opt-out).
func TestCleanup_AuditRetentionZeroKeepsEvents(t *testing.T) {
	store := eventStore(t)
	old := time.Now().UTC().AddDate(0, 0, -1000).Format(time.RFC3339)
	seedEvent(t, store, old, "snapshot_collected")

	// Nothing configured (all retention off) → cleanup is a no-op for events.
	c := New(store, nil, time.Hour, 0, WithAuditRetentionDays(0))
	c.cleanup()

	if n := eventCount(t, store); n != 1 {
		t.Errorf("auditRetentionDays=0 must keep events forever; remaining=%d", n)
	}
}
