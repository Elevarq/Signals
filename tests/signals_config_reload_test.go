package tests

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/elevarq/arq-signals/internal/collector"
	"github.com/elevarq/arq-signals/internal/config"
	"github.com/elevarq/arq-signals/internal/db"
)

// ---------------------------------------------------------------------------
// R100 / config reload — unit-level tests for Collector.Reload.
//
// Spec: features/arq-signals/specification.md § Configuration reload
// ---------------------------------------------------------------------------

func newReloadTestCollector(t *testing.T, initial []config.TargetConfig) (*collector.Collector, *db.DB) {
	t.Helper()
	dir := t.TempDir()
	store, err := db.Open(filepath.Join(dir, "test.db"), false)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	c := collector.New(store, initial, time.Hour, 30,
		collector.WithMinSnapshotInterval(60*time.Second))
	return c, store
}

// Reload with the same target list is a no-op for callers.
func TestReload_NoOpWhenTargetsUnchanged(t *testing.T) {
	tgts := []config.TargetConfig{
		{Name: "a", Host: "h", Port: 5432, DBName: "d", User: "u", SSLMode: "disable", Enabled: true},
	}
	c, _ := newReloadTestCollector(t, tgts)
	c.Reload(tgts)
	got := c.Targets()
	if len(got) != 1 || got[0].Name != "a" {
		t.Errorf("Reload no-op: got %+v, want one target named a", got)
	}
}

// Reload adding a target makes it visible immediately.
func TestReload_AddsNewTarget(t *testing.T) {
	c, _ := newReloadTestCollector(t, []config.TargetConfig{
		{Name: "a", Host: "h", Port: 5432, DBName: "d", User: "u", SSLMode: "disable", Enabled: true},
	})
	c.Reload([]config.TargetConfig{
		{Name: "a", Host: "h", Port: 5432, DBName: "d", User: "u", SSLMode: "disable", Enabled: true},
		{Name: "b", Host: "h2", Port: 5432, DBName: "d", User: "u", SSLMode: "disable", Enabled: true},
	})
	got := c.Targets()
	if len(got) != 2 {
		t.Fatalf("expected 2 targets after Reload-add, got %d", len(got))
	}
	names := map[string]bool{}
	for _, t := range got {
		names[t.Name] = true
	}
	if !names["a"] || !names["b"] {
		t.Errorf("expected {a, b}, got %v", names)
	}
}

// Reload removing a target removes it from the active list.
func TestReload_RemovesTarget(t *testing.T) {
	c, _ := newReloadTestCollector(t, []config.TargetConfig{
		{Name: "a", Host: "h", Port: 5432, DBName: "d", User: "u", SSLMode: "disable", Enabled: true},
		{Name: "b", Host: "h", Port: 5432, DBName: "d", User: "u", SSLMode: "disable", Enabled: true},
	})
	c.Reload([]config.TargetConfig{
		{Name: "a", Host: "h", Port: 5432, DBName: "d", User: "u", SSLMode: "disable", Enabled: true},
	})
	got := c.Targets()
	if len(got) != 1 || got[0].Name != "a" {
		t.Errorf("expected only 'a' after Reload-remove; got %+v", got)
	}
}

// Reload returning a deep copy — the caller mutating the returned slice
// must NOT affect the collector's internal state.
func TestReload_TargetsReturnsDefensiveCopy(t *testing.T) {
	c, _ := newReloadTestCollector(t, []config.TargetConfig{
		{Name: "a", Host: "h", Port: 5432, DBName: "d", User: "u", SSLMode: "disable", Enabled: true},
	})
	got := c.Targets()
	got[0].Name = "mutated"

	again := c.Targets()
	if again[0].Name != "a" {
		t.Errorf("Targets() returned a shared reference; got %q after caller mutation", again[0].Name)
	}
}

// The reload swap is safe from concurrent Targets() reads — race
// detector verifies absence of data races.
func TestReload_ConcurrentReadsAreSafe(t *testing.T) {
	c, _ := newReloadTestCollector(t, []config.TargetConfig{
		{Name: "a", Host: "h", Port: 5432, DBName: "d", User: "u", SSLMode: "disable", Enabled: true},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for ctx.Err() == nil {
			_ = c.Targets()
		}
	}()

	for ctx.Err() == nil {
		c.Reload([]config.TargetConfig{
			{Name: "a", Host: "h", Port: 5432, DBName: "d", User: "u", SSLMode: "disable", Enabled: true},
			{Name: "b", Host: "h", Port: 5432, DBName: "d", User: "u", SSLMode: "disable", Enabled: true},
		})
		c.Reload([]config.TargetConfig{
			{Name: "a", Host: "h", Port: 5432, DBName: "d", User: "u", SSLMode: "disable", Enabled: true},
		})
	}
	<-done
}
