package export

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

// #730 (A6) — expensive exports must honour request-context cancellation so a
// cancelled or timed-out request aborts promptly and frees resources instead of
// materializing the full history into memory.
//
// Spec: specifications/bounded-export.md (BEX-R003).

// BEX-R003 — a WriteToCtx whose context is already cancelled returns the
// context error and writes no archive.
func TestWriteToCtx_CancelledReturnsError(t *testing.T) {
	store := newStore(t)
	b := NewBuilder(store, "inst")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled before the export starts

	var buf bytes.Buffer
	err := b.WriteToCtx(ctx, &buf, Options{All: true})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled export must return context.Canceled, got %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("a cancelled export must not write a partial archive; wrote %d bytes", buf.Len())
	}
}

// BEX-R003 — the plain WriteTo shim runs with a background context (no
// cancellation) so existing non-HTTP callers are unaffected.
func TestWriteTo_BackgroundContextStillWorks(t *testing.T) {
	store := newStore(t)
	b := NewBuilder(store, "inst")

	var buf bytes.Buffer
	// An empty store's forensic --all export is permitted (it carries the
	// collection_status marker); it must succeed, proving the shim delegates
	// to WriteToCtx with a live (non-cancelled) context.
	if err := b.WriteTo(&buf, Options{All: true}); err != nil {
		t.Fatalf("WriteTo with a background context should succeed: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("WriteTo should produce a non-empty archive")
	}
}
