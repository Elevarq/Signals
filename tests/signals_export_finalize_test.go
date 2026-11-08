package tests

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/elevarq/signals/internal/db"
	"github.com/elevarq/signals/internal/export"
)

// #717 / F11 / SE-R014: WriteTo must return the ZIP finalization (Close) error.
// The zip central directory + final buffered output are written by Close, which
// can fail after every entry writer returned success — for a small/empty export
// the entries stay in the zip.Writer's buffer and only reach the underlying
// writer on Close. Before the fix the deferred Close discarded that error and
// WriteTo returned nil, so a truncated/corrupt archive was reported as a
// successful export (and the scheduled exporter would promote/upload it).

type alwaysFailWriter struct{}

func (alwaysFailWriter) Write(p []byte) (int, error) { return 0, io.ErrClosedPipe }

func TestExport_WriteToPropagatesFinalizationError(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "e.db"), false)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	b := export.NewBuilder(store, "test-instance-id")
	// Empty store + a writer that always errors: the entries buffer inside the
	// zip.Writer and only reach the writer on Close, so finalization fails.
	if err := b.WriteTo(alwaysFailWriter{}, export.Options{All: true}); err == nil {
		t.Fatal("WriteTo returned nil writing to an always-erroring writer — a failed archive finalization must be reported, not swallowed (#717/F11/SE-R014)")
	}
}
