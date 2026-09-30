package db

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Elevarq/Signals#473 — a non-writable data directory must yield a clear
// "data directory not writable by uid N" diagnostic at config-validation,
// not the misleading "enable WAL" surfaced deep in the SQLite open path.

func TestPreflightWritable_WritableDir(t *testing.T) {
	dir := t.TempDir()
	if err := PreflightWritable(filepath.Join(dir, "signals.db")); err != nil {
		t.Fatalf("writable dir should pass preflight, got: %v", err)
	}
}

func TestPreflightWritable_MissingDir(t *testing.T) {
	dir := t.TempDir()
	err := PreflightWritable(filepath.Join(dir, "nope", "signals.db"))
	if err == nil {
		t.Fatal("missing data directory should fail preflight")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("error should name the missing directory, got: %v", err)
	}
}

func TestPreflightWritable_NotWritable(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root defeats directory permission checks")
	}
	dir := t.TempDir()
	ro := filepath.Join(dir, "ro")
	if err := os.Mkdir(ro, 0o500); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Restore write bit so t.TempDir cleanup can remove it.
	t.Cleanup(func() { _ = os.Chmod(ro, 0o700) })

	err := PreflightWritable(filepath.Join(ro, "signals.db"))
	if err == nil {
		t.Fatal("non-writable data directory should fail preflight")
	}
	msg := err.Error()
	if !strings.Contains(msg, "not writable") {
		t.Fatalf("error should say the directory is not writable, got: %v", err)
	}
	if !strings.Contains(msg, "uid") {
		t.Fatalf("error should name the uid, got: %v", err)
	}
	// The misleading WAL wording must not appear.
	if strings.Contains(strings.ToLower(msg), "wal") {
		t.Fatalf("preflight error must not blame WAL, got: %v", err)
	}
}
