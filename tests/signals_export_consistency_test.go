// Tests for R110: exports are serialised against destructive retention
// writes so a concurrent cleanup cannot tear an export's reads
// (the "missing result payload for successful run" hard-error path).
// Added for issue #10.

package tests

import (
	"sync/atomic"
	"testing"
	"time"
)

// Two exports may run concurrently — the lock is shared.
func TestLockExportsAreShared(t *testing.T) {
	store := openTestDB(t)

	r1 := store.LockExports()
	defer r1()

	acquired := make(chan struct{})
	go func() {
		r2 := store.LockExports()
		defer r2()
		close(acquired)
	}()

	select {
	case <-acquired:
		// expected: second export acquired the shared lock immediately
	case <-time.After(200 * time.Millisecond):
		t.Fatal("LockExports must be shared — a second export was blocked while another was holding it")
	}
}

// An export holding the lock blocks a retention writer.
func TestLockRetentionBlockedWhileExportHeld(t *testing.T) {
	store := openTestDB(t)

	releaseExport := store.LockExports()

	acquired := int32(0)
	go func() {
		release := store.LockRetention()
		defer release()
		atomic.StoreInt32(&acquired, 1)
	}()

	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt32(&acquired) == 1 {
		t.Fatal("LockRetention acquired the lock while LockExports was held — R110 serialisation broken")
	}

	releaseExport()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&acquired) == 1 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("LockRetention failed to acquire after LockExports released")
}

// A retention writer holding the lock blocks a new export.
func TestLockExportsBlockedWhileRetentionHeld(t *testing.T) {
	store := openTestDB(t)

	releaseRet := store.LockRetention()

	acquired := int32(0)
	go func() {
		release := store.LockExports()
		defer release()
		atomic.StoreInt32(&acquired, 1)
	}()

	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt32(&acquired) == 1 {
		t.Fatal("LockExports acquired while LockRetention was held — R110 serialisation broken")
	}

	releaseRet()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&acquired) == 1 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("LockExports failed to acquire after LockRetention released")
}
