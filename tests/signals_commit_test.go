package tests

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestCommitErrorIsChecked verifies that the read-tx commit in
// collectTarget captures and checks its error rather than discarding it.
// A bare commit would let downstream persistence proceed after a failed
// PostgreSQL transaction.
//
// R108: the commit uses a dedicated short context (commitCtx), NOT the
// per-cycle budget context, so an over-budget cycle still persists its
// complete status inventory. The error must still be captured + checked.
//
// Traces: ARQ-SIGNALS-R021 / ARQ-SIGNALS-R108 / TC-SIG-036
func TestCommitErrorIsChecked(t *testing.T) {
	root := repoRoot(t)
	src := readFileString(t, filepath.Join(root, "internal", "collector", "collector.go"))

	// A bare, error-discarding commit (under any context var) is forbidden.
	for _, bare := range []string{"\ttx.Commit(ctx)\n", "\ttx.Commit(commitCtx)\n"} {
		if strings.Contains(src, bare) {
			t.Fatalf("commit is called without checking the returned error: %q — "+
				"downstream persistence may proceed after a failed PostgreSQL transaction",
				strings.TrimSpace(bare))
		}
	}

	// The commit result must be captured and checked, and must not be
	// governed by the per-cycle budget context (R108).
	if !strings.Contains(src, "cErr := tx.Commit(commitCtx)") || !strings.Contains(src, "if cErr != nil") {
		t.Fatal("read-tx commit error is not captured and checked — " +
			"expected 'cErr := tx.Commit(commitCtx)' followed by 'if cErr != nil'")
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
