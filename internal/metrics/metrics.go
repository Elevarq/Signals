// Package metrics owns the Prometheus registry that powers the optional
// /metrics endpoint described in ARQ-SIGNALS-R079. The registry is
// dedicated (not the global default) so test code and embedded use
// don't see metrics they didn't ask for, and so we can guarantee that
// only the metrics defined here are ever exported.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Registry is the dedicated Prometheus registry for Elevarq Signals
// operational metrics. The /metrics endpoint serves from this registry
// only — it never exports the process, Go runtime, or default
// collectors. Operators monitoring the daemon process itself can do
// that via their orchestrator's existing tooling.
type Registry struct {
	reg                              *prometheus.Registry
	collectionCycles                 *prometheus.CounterVec
	collectionFailures               *prometheus.CounterVec
	collectionDuration               *prometheus.HistogramVec
	collectorsSucceeded              *prometheus.CounterVec
	collectorsFailed                 *prometheus.CounterVec
	collectorsSkipped                *prometheus.CounterVec
	exportRequests                   *prometheus.CounterVec
	exportFailures                   *prometheus.CounterVec
	exportDuration                   *prometheus.HistogramVec
	sqlitePersistenceFailures        prometheus.Counter
	lastSuccessfulCollectionTS       *prometheus.GaugeVec
	highSensitivityCollectorsEnabled prometheus.Gauge
	// R097: per-(target, state) gauge for circuit-breaker state.
	// One row per (target, state) pair. The "active" state has
	// value 1; the other two have value 0. Operators alert on
	// `state="open"` or `state="paused"`.
	circuitState *prometheus.GaugeVec
	// R079 #79: per-target eligible-collector count. Captures the
	// number of collectors that would run for a target after the
	// version (R081), extension (EA-R001), daemon-wide
	// sensitivity (R075), and per-target profile (R098) gates have
	// been applied. Operators alert on sudden drops — they signal
	// extension uninstall, version downgrade, or accidental
	// profile change.
	eligibleCollectors *prometheus.GaugeVec
}

// New constructs a Registry with all R079 metrics registered. The
// caller plugs the returned *prometheus.Registry into promhttp.
func New() *Registry {
	r := prometheus.NewRegistry()

	m := &Registry{
		reg: r,
		collectionCycles: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "signals_collection_cycles_total",
				Help: "Per-target collection cycles completed, labelled by outcome.",
			},
			[]string{"target", "status"},
		),
		collectionFailures: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "signals_collection_failures_total",
				Help: "Per-target hard collection failures by reason category.",
			},
			[]string{"target", "reason"},
		),
		collectionDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "signals_collection_duration_seconds",
				Help:    "Wall-clock duration of each per-target collection cycle.",
				Buckets: prometheus.ExponentialBuckets(0.05, 2, 10),
			},
			[]string{"target", "status"},
		),
		collectorsSucceeded: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "signals_collectors_succeeded_total",
				Help: "Sum of per-cycle successful collector counts, by target.",
			},
			[]string{"target"},
		),
		collectorsFailed: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "signals_collectors_failed_total",
				Help: "Sum of per-cycle failed collector counts, by target and reason category.",
			},
			[]string{"target", "reason"},
		),
		collectorsSkipped: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "signals_collectors_skipped_total",
				Help: "Sum of per-cycle skipped collector counts, by target and reason category.",
			},
			[]string{"target", "reason"},
		),
		exportRequests: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "signals_export_requests_total",
				Help: "Export requests received, labelled by outcome.",
			},
			[]string{"status"},
		),
		exportFailures: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "signals_export_failures_total",
				Help: "Export failures, labelled by error category (matches audit log error_category).",
			},
			[]string{"error_category"},
		),
		exportDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "signals_export_duration_seconds",
				Help:    "Wall-clock duration of each export.",
				Buckets: prometheus.ExponentialBuckets(0.01, 3, 9),
			},
			[]string{"status"},
		),
		sqlitePersistenceFailures: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "signals_sqlite_persistence_failures_total",
				Help: "InsertCollectionAtomic transaction rollbacks (R077).",
			},
		),
		lastSuccessfulCollectionTS: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "signals_last_successful_collection_timestamp",
				Help: "Unix seconds of the most recent successful collection per target.",
			},
			[]string{"target"},
		),
		highSensitivityCollectorsEnabled: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "signals_high_sensitivity_collectors_enabled",
				Help: "1 if signals.high_sensitivity_collectors_enabled is true, 0 otherwise (R075).",
			},
		),
		circuitState: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "signals_circuit_state",
				Help: "Per-target circuit state (R097). Each target emits one row per state (closed/open/paused); the active row has value 1, others 0.",
			},
			[]string{"target", "state"},
		),
		eligibleCollectors: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "signals_eligible_collectors",
				Help: "Number of collectors eligible to run for the target after version (R081), extension, daemon-wide sensitivity (R075), and per-target profile (R098) gates. Alert on sudden drops.",
			},
			[]string{"target"},
		),
	}

	r.MustRegister(
		m.collectionCycles,
		m.collectionFailures,
		m.collectionDuration,
		m.collectorsSucceeded,
		m.collectorsFailed,
		m.collectorsSkipped,
		m.exportRequests,
		m.exportFailures,
		m.exportDuration,
		m.sqlitePersistenceFailures,
		m.lastSuccessfulCollectionTS,
		m.highSensitivityCollectorsEnabled,
		m.circuitState,
		m.eligibleCollectors,
	)

	return m
}

// Gatherer returns the underlying prometheus.Gatherer for promhttp.
func (m *Registry) Gatherer() prometheus.Gatherer {
	return m.reg
}

// --- Recorders ---
//
// Recorders accept untyped strings for label values; callers supply
// the bounded enum value defined in R079. A nil receiver is treated
// as a no-op so call sites don't need to nil-check before incrementing
// — the metrics package is opt-in and a daemon running with
// metrics_enabled=false has a nil Registry.

func (m *Registry) ObserveCollection(target, status string, durationSeconds float64) {
	if m == nil {
		return
	}
	m.collectionCycles.WithLabelValues(target, status).Inc()
	m.collectionDuration.WithLabelValues(target, status).Observe(durationSeconds)
}

func (m *Registry) ObserveCollectionFailure(target, reason string) {
	if m == nil {
		return
	}
	m.collectionFailures.WithLabelValues(target, reason).Inc()
}

func (m *Registry) AddCollectorOutcomes(target string, succeeded int, failedByReason, skippedByReason map[string]int) {
	if m == nil {
		return
	}
	if succeeded > 0 {
		m.collectorsSucceeded.WithLabelValues(target).Add(float64(succeeded))
	}
	for reason, n := range failedByReason {
		if n > 0 {
			m.collectorsFailed.WithLabelValues(target, reason).Add(float64(n))
		}
	}
	for reason, n := range skippedByReason {
		if n > 0 {
			m.collectorsSkipped.WithLabelValues(target, reason).Add(float64(n))
		}
	}
}

func (m *Registry) RecordExport(status string, durationSeconds float64) {
	if m == nil {
		return
	}
	m.exportRequests.WithLabelValues(status).Inc()
	m.exportDuration.WithLabelValues(status).Observe(durationSeconds)
}

func (m *Registry) RecordExportFailure(errorCategory string) {
	if m == nil {
		return
	}
	m.exportFailures.WithLabelValues(errorCategory).Inc()
}

func (m *Registry) IncSQLitePersistenceFailure() {
	if m == nil {
		return
	}
	m.sqlitePersistenceFailures.Inc()
}

func (m *Registry) SetLastSuccessfulCollection(target string, unixSeconds float64) {
	if m == nil {
		return
	}
	m.lastSuccessfulCollectionTS.WithLabelValues(target).Set(unixSeconds)
}

func (m *Registry) SetHighSensitivityEnabled(enabled bool) {
	if m == nil {
		return
	}
	v := 0.0
	if enabled {
		v = 1.0
	}
	m.highSensitivityCollectorsEnabled.Set(v)
}

// SetEligibleCollectors sets the per-target eligible-collector
// gauge (R079, see also #79 review). Called at the top of each
// cycle once the per-target FilterParams have been resolved.
func (m *Registry) SetEligibleCollectors(target string, count int) {
	if m == nil {
		return
	}
	m.eligibleCollectors.WithLabelValues(target).Set(float64(count))
}

// SetCircuitState writes the per-target gauge for the active state
// and zeroes the other states for the same target (R097). The
// argument is the lowercase wire form (`closed`, `open`, `paused`).
//
// Caller is responsible for passing every supported state in
// `allStates` so the metric set is complete for that target —
// otherwise a state that's never seen would never appear in the
// gauge.
//
// Issue #97: writes the new active state to 1 BEFORE zeroing the
// others. A scrape that lands mid-transition sees "two states
// active simultaneously" instead of "no state active". Operators
// querying for the active state still get a stable signal; the
// transient overlap is easier for alert rules to reason about
// than a transient "no state" sample.
func (m *Registry) SetCircuitState(target, activeState string, allStates []string) {
	if m == nil {
		return
	}
	// Set the new active state first.
	m.circuitState.WithLabelValues(target, activeState).Set(1)
	// Then zero the others.
	for _, s := range allStates {
		if s == activeState {
			continue
		}
		m.circuitState.WithLabelValues(target, s).Set(0)
	}
}

// CollectionFailureReasons enumerates every value `reason` can take
// on the `signals_collection_failures_total` counter. Mirrors
// the classifier in `internal/collector/collector.go::classifyCollectionFailure`
// — a constant slice here so the metrics consumer guide and the
// code reference one source (issue #93).
var CollectionFailureReasons = []string{
	"connect_error",
	"version_unsupported",
	"timeout_setup",
	"safety_check",
	"persistence",
	"internal",
}
