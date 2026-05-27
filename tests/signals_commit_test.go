package tests

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestCommitErrorIsChecked verifies that tx.Commit(ctx) in collectTarget
// has its error return value captured and checked. A bare tx.Commit(ctx)
// without error handling would allow downstream persistence to proceed
// after a failed PostgreSQL transaction.
//
// Traces: ARQ-SIGNALS-R021 / TC-SIG-036
func TestCommitErrorIsChecked(t *testing.T) {
	root := repoRoot(t)
	src := readFileString(t, filepath.Join(root, "internal", "collector", "collector.go"))

	// The commit call must be in an error-checking pattern.
	// Correct:   if err := tx.Commit(ctx); err != nil {
	// Incorrect: tx.Commit(ctx)   (bare call, error discarded)
	if strings.Contains(src, "\ttx.Commit(ctx)\n") {
		t.Fatal("tx.Commit(ctx) is called without checking the returned error — " +
			"downstream persistence may proceed after a failed PostgreSQL transaction")
	}

	if !strings.Contains(src, "tx.Commit(ctx); err != nil") {
		t.Fatal("tx.Commit(ctx) error is not checked with 'if err := tx.Commit(ctx); err != nil'")
	}
}

// TestCommitFailureBlocksDownstreamPersistence verifies that a commit
// failure causes the function to return before reaching the downstream
// SQLite write (InsertCollectionAtomic, which atomically persists the
// snapshot, query runs and query results — see R077). This ensures no
// contradictory state is created in SQLite when the PostgreSQL
// transaction fails.
//
// Traces: ARQ-SIGNALS-R021 / TC-SIG-036
func TestCommitFailureBlocksDownstreamPersistence(t *testing.T) {
	root := repoRoot(t)
	src := readFileString(t, filepath.Join(root, "internal", "collector", "collector.go"))

	// Find the positions of the commit error handling and downstream persistence.
	commitReturn := strings.Index(src, `return fmt.Errorf("commit tx for`)
	atomicInsert := strings.Index(src, "InsertCollectionAtomic")

	if commitReturn < 0 {
		t.Fatal("collector.go does not return an error on commit failure — " +
			"missing 'return fmt.Errorf(\"commit tx for...'")
	}
	if atomicInsert < 0 {
		t.Fatal("collector.go does not call InsertCollectionAtomic")
	}

	// The commit-error return must appear BEFORE the downstream persistence call.
	// This guarantees that if commit fails, the function exits before writing to SQLite.
	if commitReturn > atomicInsert {
		t.Error("commit error return appears AFTER InsertCollectionAtomic — " +
			"SQLite writes may proceed after failed PostgreSQL commit")
	}
}
