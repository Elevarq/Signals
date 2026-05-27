package metrics

import "testing"

// TestRegistry_NilReceiverIsSafeOnEveryRecorder pins the invariant
// behind issue #105: every Registry method must handle a nil
// receiver without panicking. The /export and collector code paths
// call these on `deps.Metrics` / `c.metrics`, both of which are
// `nil` when `signals.metrics_enabled` is false (the default).
//
// If a future method skips its `if m == nil { return }` guard, this
// test fails before the daemon ever encounters a runtime panic.
func TestRegistry_NilReceiverIsSafeOnEveryRecorder(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("nil *Registry receiver panicked: %v", r)
		}
	}()

	var m *Registry

	// Every public method that the daemon's hot paths invoke.
	m.ObserveCollection("t", "ok", 0.1)
	m.ObserveCollectionFailure("t", "execution_error")
	m.AddCollectorOutcomes("t", 1, nil, nil)
	m.RecordExport("ok", 0.1)
	m.RecordExportFailure("invalid_time_format")
	m.IncSQLitePersistenceFailure()
	m.SetLastSuccessfulCollection("t", 1.0)
	m.SetHighSensitivityEnabled(true)
	m.SetEligibleCollectors("t", 5)
	m.SetCircuitState("t", "closed", []string{"closed", "open", "paused"})
}
