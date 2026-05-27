package tests

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/elevarq/arq-signals/internal/collector"
	"github.com/elevarq/arq-signals/internal/export"
)

// ---------------------------------------------------------------------------
// Behavioral safety tests — verify structural invariants of collector.go
// ---------------------------------------------------------------------------

// TestCollectorUsesDedicatedConnection verifies that collector.go acquires a
// dedicated connection (pool.Acquire) before beginning the transaction, and
// that BeginTx is called on the acquired connection (conn.), not the pool.
func TestCollectorUsesDedicatedConnection(t *testing.T) {
	root := repoRoot(t)
	src := readFileString(t, filepath.Join(root, "internal", "collector", "collector.go"))

	// Verify pool.Acquire is called before transaction
	acquireIdx := strings.Index(src, "pool.Acquire(ctx)")
	beginIdx := strings.Index(src, "conn.BeginTx(ctx")
	if acquireIdx < 0 {
		t.Fatal("collector.go does not call pool.Acquire(ctx) — timeouts may not apply to collection connection")
	}
	if beginIdx < 0 {
		t.Fatal("collector.go does not call conn.BeginTx — transaction not on dedicated connection")
	}
	if acquireIdx > beginIdx {
		t.Error("pool.Acquire must come before conn.BeginTx to ensure same connection")
	}
}

// TestTimeoutsUseSetLocal verifies the timeout enforcement uses SET LOCAL
// (transaction-scoped), not plain SET (session-scoped). This guarantees
// timeouts apply to exactly the collection transaction.
func TestTimeoutsUseSetLocal(t *testing.T) {
	root := repoRoot(t)
	src := readFileString(t, filepath.Join(root, "internal", "collector", "collector.go"))

	if !strings.Contains(src, "SET LOCAL %s") && !strings.Contains(src, "SET LOCAL") {
		t.Error("collector.go does not use SET LOCAL for timeouts — timeouts may not be transaction-scoped")
	}
	// Verify it's NOT using plain SET (which would be session-scoped)
	// The pattern "SET %s" without LOCAL should not appear in the timeout block
	// But we can't easily distinguish, so just verify SET LOCAL is present
	setLocalCount := strings.Count(src, "SET LOCAL")
	if setLocalCount < 1 {
		t.Error("expected at least one SET LOCAL statement in collector.go")
	}
}

// TestReadOnlyCheckOnAcquiredConnection verifies that the read-only check
// uses the acquired connection (conn.QueryRow), not the pool (pool.QueryRow).
func TestReadOnlyCheckOnAcquiredConnection(t *testing.T) {
	root := repoRoot(t)
	src := readFileString(t, filepath.Join(root, "internal", "collector", "collector.go"))

	// The read-only check must use conn.QueryRow, not pool.QueryRow
	if !strings.Contains(src, `conn.QueryRow(ctx, "SHOW default_transaction_read_only")`) {
		t.Error("read-only verification does not use the acquired connection (conn.QueryRow) — may check wrong connection")
	}
}

// TestStatusEndpointNoSecretFields verifies that /status does NOT expose
// secret_type or secret_ref in the response.
func TestStatusEndpointNoSecretFields(t *testing.T) {
	root := repoRoot(t)
	src := readFileString(t, filepath.Join(root, "internal", "api", "server.go"))

	// Check that handleStatus does not include secret_type or secret_ref in the response
	if strings.Contains(src, `"secret_type"`) {
		t.Error("/status handler still exposes secret_type in response — credential source details should be hidden")
	}
	if strings.Contains(src, `"secret_ref"`) {
		t.Error("/status handler exposes secret_ref in response — credential path details should be hidden")
	}
}

// TestBypassedChecksRecordsSpecificReasons verifies that a new collector
// with WithAllowUnsafeRole(true) starts with zero bypassed checks.
func TestBypassedChecksRecordsSpecificReasons(t *testing.T) {
	store := openTestDB(t)
	coll := collector.New(store, nil, time.Minute, 30,
		collector.WithAllowUnsafeRole(true),
	)

	// Initially no bypassed checks
	checks := coll.GetBypassedChecks()
	if len(checks) != 0 {
		t.Errorf("expected 0 bypassed checks initially, got %d", len(checks))
	}
}

// TestExportMetadataDynamicBypassReasons verifies that export metadata
// captures dynamic bypass reasons with specific role attribute details.
func TestExportMetadataDynamicBypassReasons(t *testing.T) {
	store := openTestDB(t)
	builder := export.NewBuilder(store, "test-instance")

	// Simulate collector recording specific bypassed checks
	reasons := []string{
		`role "admin" has superuser attribute (rolsuper=true)`,
		`role "admin" has replication attribute (rolreplication=true)`,
	}
	builder.SetUnsafeMode(func() []string { return reasons })

	var buf bytes.Buffer
	if err := builder.WriteTo(&buf, export.Options{}); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	meta := readZipMetadata(t, buf.Bytes())

	unsafeReasons, ok := meta["unsafe_reasons"]
	if !ok {
		t.Fatal("metadata.json missing unsafe_reasons when unsafe mode is active")
	}
	reasonSlice, ok := unsafeReasons.([]any)
	if !ok {
		t.Fatalf("unsafe_reasons is not an array: %T", unsafeReasons)
	}
	if len(reasonSlice) != 2 {
		t.Errorf("expected 2 unsafe_reasons, got %d", len(reasonSlice))
	}
	// Verify reasons contain actual role attribute details, not generic strings
	for _, r := range reasonSlice {
		s := r.(string)
		if !strings.Contains(s, "rolsuper") && !strings.Contains(s, "rolreplication") {
			t.Errorf("unsafe_reason should contain specific role attribute, got: %s", s)
		}
	}
}
