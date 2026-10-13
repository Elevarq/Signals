package collector

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/elevarq/signals/internal/db"
	"github.com/elevarq/signals/internal/schedule"
)

func tempStore(t *testing.T) *db.DB {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "sched.db"), false)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	if err := store.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// #491 B2 + B8 — in scheduled mode the collector performs NO start-time
// baseline (it waits for the next cron match, hours away here), but an
// on-demand collection still runs immediately.
func TestRunScheduled_NoBaselineButOnDemandRuns(t *testing.T) {
	store := tempStore(t)
	sched, err := schedule.New(schedule.Options{Expr: "0 2 * * *"}) // next fire hours away
	if err != nil {
		t.Fatalf("schedule.New: %v", err)
	}
	c := New(store, nil, time.Minute, 0, WithSchedule(sched))

	cycles := make(chan struct{}, 4)
	c.SetAfterCycle(func(context.Context) { cycles <- struct{}{} })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)

	// B2: no baseline — nothing fires in a short window (the next cron match
	// is hours away, so this is deterministic, not merely slow).
	select {
	case <-cycles:
		t.Fatal("scheduled mode must not run a start-time baseline")
	case <-time.After(150 * time.Millisecond):
	}

	// B8: on-demand collection runs immediately even in scheduled mode.
	if !c.CollectNow(CollectRequest{RequestID: "r1"}) {
		t.Fatal("CollectNow should queue")
	}
	select {
	case <-cycles:
	case <-time.After(3 * time.Second):
		t.Fatal("on-demand collection did not run in scheduled mode")
	}
}

// Control: with no schedule, interval mode DOES run the start-time baseline.
func TestRunInterval_RunsBaseline(t *testing.T) {
	store := tempStore(t)
	c := New(store, nil, time.Hour, 0) // long interval; baseline is immediate

	cycles := make(chan struct{}, 4)
	c.SetAfterCycle(func(context.Context) { cycles <- struct{}{} })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)

	select {
	case <-cycles:
	case <-time.After(3 * time.Second):
		t.Fatal("interval mode must run a baseline at start")
	}
}
