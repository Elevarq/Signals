package tests

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/elevarq/arq-signals/internal/collector"
	"github.com/elevarq/arq-signals/internal/db"
)

// TestDefaultTimeouts verifies that a Collector created with no timeout options
// has the expected default query timeout (10s) and target timeout (60s).
// Traces: ARQ-SIGNALS-R012 / TC-SIG-024
func TestDefaultTimeouts(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "timeout-test.db")
	store, err := db.Open(dbPath, false)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	coll := collector.New(store, nil, 5*time.Minute, 30)

	qt := collector.GetQueryTimeout(coll)
	tt := collector.GetTargetTimeout(coll)

	if qt != 10*time.Second {
		t.Errorf("default queryTimeout = %v, want %v", qt, 10*time.Second)
	}
	if tt != 60*time.Second {
		t.Errorf("default targetTimeout = %v, want %v", tt, 60*time.Second)
	}
}

// TestWithQueryTimeoutSetsField verifies WithQueryTimeout overrides the default.
// Traces: ARQ-SIGNALS-R012 / TC-SIG-024
func TestWithQueryTimeoutSetsField(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "timeout-test.db")
	store, err := db.Open(dbPath, false)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	coll := collector.New(store, nil, 5*time.Minute, 30,
		collector.WithQueryTimeout(30*time.Second),
	)

	qt := collector.GetQueryTimeout(coll)
	if qt != 30*time.Second {
		t.Errorf("queryTimeout = %v, want %v", qt, 30*time.Second)
	}
}

// TestWithTargetTimeoutSetsField verifies WithTargetTimeout overrides the default.
// Traces: ARQ-SIGNALS-R012 / TC-SIG-024
func TestWithTargetTimeoutSetsField(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "timeout-test.db")
	store, err := db.Open(dbPath, false)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	coll := collector.New(store, nil, 5*time.Minute, 30,
		collector.WithTargetTimeout(120*time.Second),
	)

	tt := collector.GetTargetTimeout(coll)
	if tt != 120*time.Second {
		t.Errorf("targetTimeout = %v, want %v", tt, 120*time.Second)
	}
}
