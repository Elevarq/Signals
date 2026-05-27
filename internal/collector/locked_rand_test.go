package collector

import (
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
)

// TestLockedRandReaderConcurrentRead verifies that lockedRandReader is safe
// for concurrent use. Run with `go test -race`. Without the mutex, the
// underlying *rand.Rand panics or returns garbage when called from multiple
// goroutines. ulid.MustNew calls Read on the entropy source so ULID
// generation across parallel target collections relies on this guarantee.
func TestLockedRandReaderConcurrentRead(t *testing.T) {
	src := &lockedRandReader{r: rand.New(rand.NewSource(time.Now().UnixNano()))}

	const goroutines = 32
	const idsPerGoroutine = 200

	var wg sync.WaitGroup
	results := make(chan string, goroutines*idsPerGoroutine)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < idsPerGoroutine; j++ {
				id := ulid.MustNew(ulid.Timestamp(time.Now()), src).String()
				results <- id
			}
		}()
	}
	wg.Wait()
	close(results)

	seen := make(map[string]struct{}, goroutines*idsPerGoroutine)
	for id := range results {
		seen[id] = struct{}{}
	}
	// We can't strictly require uniqueness across all calls because two
	// goroutines may genuinely generate IDs in the same millisecond from
	// non-monotonic random bytes that happen to collide — but even at 6400
	// IDs the collision probability is astronomically low. Real wins from
	// this test come from -race detecting concurrent access to the rand
	// state.
	if len(seen) < goroutines*idsPerGoroutine-2 {
		t.Errorf("unexpectedly many duplicate ULIDs: %d unique out of %d",
			len(seen), goroutines*idsPerGoroutine)
	}
}
