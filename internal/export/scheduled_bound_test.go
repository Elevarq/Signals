package export

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// #721 / F15: scheduled-export delivery must be isolated per target, bounded by
// a finite deadline, coalesced (single-flight), and non-blocking — so a hung or
// slow sink cannot abort unrelated targets, hang forever, build an unbounded
// backlog, or stall startup / the collection loop.

// --- SE-R015: per-target delivery isolation -------------------------------

func TestScheduledExporter_ExportLatest_PerTargetIsolation(t *testing.T) {
	dest := t.TempDir()
	fw := &fakeWriter{
		targetIDs:  []int64{1, 2, 3},
		payloadFor: func(id int64) []byte { return []byte{'t', byte('0' + id)} },
		failFor:    map[int64]bool{2: true}, // only target 2 fails
	}
	se := NewScheduledExporter(fw, dest, "inst", fixedClock(time.Now().UTC()), nil)

	written, err := se.ExportLatest(context.Background())
	if err == nil {
		t.Fatal("want an aggregate error naming the failed target")
	}
	if !strings.Contains(err.Error(), "target 2") {
		t.Errorf("aggregate error does not name the failed target: %v", err)
	}
	// Targets 1 and 3 were delivered despite 2 failing (not aborted).
	if len(written) != 2 {
		t.Fatalf("wrote %d files, want 2 (targets 1 and 3 delivered despite target 2 failing)", len(written))
	}
	entries, _ := os.ReadDir(dest)
	if len(entries) != 2 {
		t.Errorf("dest has %d entries, want exactly 2 (no file for the failed target, no leftover temp)", len(entries))
	}
}

// --- SE-R016: ExportLatest stops promptly when its bounded context is done --

func TestScheduledExporter_ExportLatest_CancelledContextWritesNothing(t *testing.T) {
	dest := t.TempDir()
	fw := &fakeWriter{targetIDs: []int64{1, 2}, payload: []byte("PK\x03\x04")}
	se := NewScheduledExporter(fw, dest, "inst", fixedClock(time.Now().UTC()), nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled (as a hung-sink deadline / shutdown would be)

	written, err := se.ExportLatest(ctx)
	if err == nil {
		t.Fatal("want a cancellation error")
	}
	if len(written) != 0 {
		t.Errorf("wrote %d files on a cancelled context, want 0", len(written))
	}
	if entries, _ := os.ReadDir(dest); len(entries) != 0 {
		t.Errorf("dest has %d entries on a cancelled context, want 0", len(entries))
	}
}

// --- SE-R016: Deliver is non-blocking + single-flight (coalescing) --------

// blockingWriter blocks inside WriteTo until release is closed, so a test can
// hold a delivery "in flight" deterministically.
type blockingWriter struct {
	ids     []int64
	entered chan struct{}
	release chan struct{}
}

func (b *blockingWriter) LatestTargetIDs() ([]int64, error) { return b.ids, nil }
func (b *blockingWriter) WriteTo(w io.Writer, _ Options) error {
	b.entered <- struct{}{}
	<-b.release
	_, _ = w.Write([]byte("PK\x03\x04"))
	return nil
}

func TestScheduledExporter_Deliver_NonBlockingAndCoalesces(t *testing.T) {
	dest := t.TempDir()
	bw := &blockingWriter{ids: []int64{1}, entered: make(chan struct{}, 1), release: make(chan struct{})}
	se := NewScheduledExporter(bw, dest, "inst", fixedClock(time.Now().UTC()), nil)
	done := make(chan error, 2)
	se.SetAfterDelivery(func(_ []string, err error) { done <- err })

	// Trigger 1: returns immediately (non-blocking) and starts a delivery.
	if !se.Deliver(context.Background()) {
		t.Fatal("first Deliver returned false, want true (should start a delivery)")
	}
	select {
	case <-bw.entered: // the background delivery is now blocked inside WriteTo
	case <-time.After(2 * time.Second):
		t.Fatal("background delivery did not start")
	}

	// Trigger 2 while the first is in flight: coalesced (dropped), still immediate.
	if se.Deliver(context.Background()) {
		t.Fatal("second Deliver returned true while one was in flight, want false (coalesced)")
	}

	// Let the first finish; it must deliver successfully.
	close(bw.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("in-flight delivery failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("delivery did not complete after release")
	}
}

// --- SE-R016: a hung sink delivery is bounded by the finite deadline -------

// blockingPutAPI blocks PutObject until the context is cancelled, modelling a
// hung S3 sink; the bounded delivery deadline must cancel it.
type blockingPutAPI struct{ entered chan struct{} }

func (b *blockingPutAPI) PutObject(ctx context.Context, _ *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	select {
	case b.entered <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestScheduledExporter_Deliver_BoundedDeadlineOnHungSink(t *testing.T) {
	put := &blockingPutAPI{entered: make(chan struct{}, 1)}
	se := NewScheduledExporterS3(fakeS3Builder{ids: []int64{1}}, put, "b", "p", "", "inst", fixedClock(time.Now().UTC()), nil)
	se.SetDeliveryTimeout(80 * time.Millisecond) // finite bound
	done := make(chan error, 1)
	se.SetAfterDelivery(func(_ []string, err error) { done <- err })

	start := time.Now()
	if !se.Deliver(context.Background()) {
		t.Fatal("Deliver returned false, want true")
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("hung sink delivery returned nil error, want a deadline/cancel error")
		}
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Errorf("delivery took %v — the finite deadline did not bound the hung sink", elapsed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("delivery did not return — the finite deadline did not cancel the hung sink (SE-R016)")
	}
}

// --- SE-R016: delivery recovers once the sink is healthy again ------------

func TestScheduledExporter_Deliver_RecoversAfterCompletion(t *testing.T) {
	dest := t.TempDir()
	fw := &fakeWriter{targetIDs: []int64{1}, payload: []byte("PK\x03\x04")}
	se := NewScheduledExporter(fw, dest, "inst", fixedClock(time.Now().UTC()), nil)
	done := make(chan error, 4)
	se.SetAfterDelivery(func(_ []string, err error) { done <- err })

	if !se.Deliver(context.Background()) {
		t.Fatal("first Deliver returned false, want true")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("first delivery did not complete")
	}

	// After the first completes, a new trigger must be able to start again
	// (single-flight released). Retry briefly to tolerate the unlock happening
	// just after afterDelivery fired.
	started := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if se.Deliver(context.Background()) {
			started = true
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if !started {
		t.Fatal("Deliver never started a second delivery after the first completed (no recovery)")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("second delivery did not complete")
	}
}

// Guard: the aggregate error from a fully-failed ExportLatest is a joined error
// (errors.Join), so callers can still inspect individual target failures.
func TestScheduledExporter_ExportLatest_AggregatesAllTargetErrors(t *testing.T) {
	dest := t.TempDir()
	fw := &fakeWriter{
		targetIDs: []int64{1, 2},
		payload:   []byte("x"),
		failFor:   map[int64]bool{1: true, 2: true},
	}
	se := NewScheduledExporter(fw, dest, "inst", fixedClock(time.Now().UTC()), nil)
	written, err := se.ExportLatest(context.Background())
	if len(written) != 0 {
		t.Errorf("wrote %d files, want 0 (both targets failed)", len(written))
	}
	if err == nil || !strings.Contains(err.Error(), "target 1") || !strings.Contains(err.Error(), "target 2") {
		t.Errorf("aggregate error must name both failed targets (errors.Join), got: %v", err)
	}
}
