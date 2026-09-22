package collector

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	// math/rand seeds the ULID entropy source (see lockedRandReader
	// below). ULIDs are sortable+unique IDs for traceability, not
	// security tokens — crypto/rand would be slower for zero
	// security benefit.
	"math/rand" // nosemgrep: go.lang.security.audit.crypto.math_random.math-random-used
	"strings"
	"sync"
	"time"

	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"

	"github.com/elevarq/signals/internal/circuit"
	"github.com/elevarq/signals/internal/config"
	"github.com/elevarq/signals/internal/db"
	"github.com/elevarq/signals/internal/metrics"
	"github.com/elevarq/signals/internal/pgqueries"
	"github.com/elevarq/signals/internal/safety"
)

// connConfigFunc is the function used to build pgx configs. Overridable for testing.
var connConfigFunc = BuildConnConfig

// Collector handles scheduled PostgreSQL telemetry collection.
//
// Field-level reload semantics (R100):
//
//   - `targets` is reload-mutable: SIGHUP / POST /reload swap it
//     under `runtimeMu`. Read it through `currentTargets()` only.
//   - Every OTHER field below is set ONCE at construction and never
//     written again. Reload v1 deliberately leaves them in place.
//     A future widening of reload scope to include these fields
//     MUST add `runtimeMu`-protected access here AND in every
//     cycle-goroutine reader, or it introduces a data race
//     (issue #89). Race-detector tests for the v1 reload scope
//     live in `tests/signals_config_reload_test.go`.
type Collector struct {
	db            *db.DB
	targets       []config.TargetConfig // reload-mutable; access via currentTargets() / Targets().
	interval      time.Duration         // set-at-construction; reload-v1 leaves the ticker armed at boot value.
	retentionDays int                   // set-at-construction.
	// afterCycle, when non-nil, is invoked after every completed collection
	// cycle (#350 scheduled export). Set once via SetAfterCycle before
	// Run starts, so it is read-only for Run's lifetime (no lock needed).
	afterCycle func(context.Context)
	// retention (R099) carries per-class retention thresholds.
	// Zero-value (Retention.IsSet() == false) means the daemon
	// uses the flat retentionDays for every class.
	// set-at-construction; reload-v1 leaves cleanup using boot values.
	retention            config.RetentionConfig
	maxConcurrentTargets int           // set-at-construction.
	targetTimeout        time.Duration // set-at-construction.
	queryTimeout         time.Duration // set-at-construction.
	// minSnapshotInterval is the per-target floor between
	// completed snapshots (R091). Zero means "rule not configured"
	// — should never reach the collector at runtime; the config
	// validator (FC-10) rejects non-positive values at startup.
	// set-at-construction; reload-v1 leaves it in place.
	minSnapshotInterval    time.Duration
	allowUnsafeRole        bool // set-at-construction.
	highSensitivityEnabled bool // set-at-construction.
	// #128: per-collector opt-in for pg_stats_array_range_v1.
	// Layered on top of highSensitivityEnabled.
	collectArrayRangeHistograms bool // set-at-construction.
	metrics                     *metrics.Registry
	bypassedChecks              []string
	bypassedChecksMu            sync.Mutex
	pools                       map[string]*pgxpool.Pool
	poolsMu                     sync.Mutex
	collectNowCh                chan CollectRequest
	entropy                     io.Reader
	running                     sync.Mutex

	// credResolver dispatches each connection's credential by
	// auth_method (credential-providers.md #93): the default password
	// provider, or the aws_rds_iam token provider (#94). Set once at
	// construction; invoked from getPool's BeforeConnect hook.
	//
	// Typed as the CredentialResolver interface (not the concrete
	// *credentialResolver) so tests can inject a recording fake via
	// WithCredentialResolverForTest to verify the R100.1 guarantee that
	// a reload-created pool re-resolves with the NEW target config. The
	// production path assigns the concrete resolver, which satisfies the
	// interface.
	credResolver CredentialResolver

	// circuit is the per-target state machine (R097). Always
	// non-nil after New — defaults to a manager with the
	// documented thresholds when no override is supplied.
	circuit *circuit.Manager

	// runtimeMu guards the runtime-mutable subset of the
	// configuration: the targets list (R100). Other fields stay
	// set-at-construction for v1 — see Reload's documented scope.
	runtimeMu sync.RWMutex

	// warnedOnce deduplicates per-cycle operator advisories so a
	// persistent condition (e.g. an owner-only or permission-denied
	// collector) is logged once per (target, collector, kind) for the
	// daemon's lifetime instead of every poll (R117, #200). Keyed via
	// warnKey; guarded by warnedOnceMu.
	warnedOnceMu sync.Mutex
	warnedOnce   map[string]struct{}

	// beforeQueryForTest, when non-nil, is invoked immediately before
	// each collector query runs, with the per-query context, the live
	// transaction, the collector id, and its real SQL; it returns the
	// SQL actually executed. It is a test-only seam (wired via
	// WithBeforeQueryForTest) that lets an execution-faithful
	// integration test drive one collector into a mid-query
	// budget-exhaustion state — blocking it until the per-query context
	// (bounded by the remaining per-cycle budget) is done so the real
	// query observes an already-expired context and returns a deadline
	// error with the connection preserved, exercising the savepoint-
	// recovery path (R108 / #329) against the real transaction. nil in
	// production; the real SQL runs unchanged.
	beforeQueryForTest func(qCtx context.Context, tx pgx.Tx, queryID, sql string) string
}

// CollectRequest is the on-demand cycle payload carried over
// collectNowCh. RequestID is the R082 Phase 2 correlation identifier
// that propagates through to per-target audit events; an empty value
// means no correlation id was supplied. Actor is the R083 audit
// `actor` value resolved by the auth middleware from the matched
// token; an empty value means "no actor in scope" (e.g.
// interval-driven cycles), in which case audit events emit no
// `actor` attribute at all.
type CollectRequest struct {
	Targets   []string // nil = collect every enabled target
	RequestID string   // empty = no correlation id
	Actor     string   // empty = omit actor attribute
	// Force, when true, bypasses R091's per-target
	// min_snapshot_interval check for this one cycle (R092).
	// Operators set it via `signalsctl collect now --force` or
	// `POST /collect/now?force=true`. The override is
	// per-request only — it does not persist or change
	// configuration.
	Force bool
}

// lockedRandReader serializes access to a math/rand.Rand source. The
// standard library's *rand.Rand is not safe for concurrent use, but ULID
// generation runs in parallel per target and per query. Without this wrapper
// concurrent ulid.MustNew calls race on the underlying state, occasionally
// producing duplicate IDs.
type lockedRandReader struct {
	mu sync.Mutex
	r  *rand.Rand
}

func (l *lockedRandReader) Read(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.r.Read(p)
}

// New creates a new Collector.
func New(store *db.DB, targets []config.TargetConfig, interval time.Duration, retentionDays int, opts ...CollectorOption) *Collector {
	c := &Collector{
		db:                   store,
		targets:              targets,
		interval:             interval,
		retentionDays:        retentionDays,
		maxConcurrentTargets: 4,
		targetTimeout:        60 * time.Second,
		queryTimeout:         10 * time.Second,
		minSnapshotInterval:  DefaultMinSnapshotInterval, // R091; overridable via WithMinSnapshotInterval
		pools:                make(map[string]*pgxpool.Pool),
		collectNowCh:         make(chan CollectRequest, 1),
		entropy:              &lockedRandReader{r: rand.New(rand.NewSource(time.Now().UnixNano()))},
		// R097: per-target circuit-breaker. Defaults to documented
		// thresholds (3 consecutive failures, 5min cooldown); the
		// daemon overrides via WithCircuitManager when config
		// supplies different values.
		circuit: circuit.NewManager(0, 0),
		// Credential provider dispatch (#93/#94). Default to the
		// production resolver; tests may override via WithCredentialResolver.
		credResolver: newCredentialResolver(slog.Default()),
		// R117 (#200): once-per-daemon-run advisory dedup.
		warnedOnce: make(map[string]struct{}),
	}
	for _, opt := range opts {
		opt(c)
	}

	// Wire the circuit manager's onChange callback after options
	// run so any custom manager passed via WithCircuitManager
	// inherits the same audit + metrics surface.
	c.circuit.SetOnChange(func(target string, from, to circuit.State, meta circuit.TransitionMeta) {
		c.onCircuitStateChange(target, from, to, meta)
	})

	return c
}

// CollectorOption configures a Collector.
type CollectorOption func(*Collector)

// WithMaxConcurrentTargets sets the max number of targets collected in parallel.
func WithMaxConcurrentTargets(n int) CollectorOption {
	return func(c *Collector) {
		if n > 0 {
			c.maxConcurrentTargets = n
		}
	}
}

// WithTargetTimeout sets the per-target collection timeout.
func WithTargetTimeout(d time.Duration) CollectorOption {
	return func(c *Collector) {
		if d > 0 {
			c.targetTimeout = d
		}
	}
}

// WithMinSnapshotInterval sets the per-target minimum interval
// between completed snapshots (R091). Zero or negative is rejected
// — the config validator (FC-10) does the same at startup.
func WithMinSnapshotInterval(d time.Duration) CollectorOption {
	return func(c *Collector) {
		if d > 0 {
			c.minSnapshotInterval = d
		}
	}
}

// WithCredentialResolver overrides the credential provider dispatch
// (#93/#94). Tests inject a resolver with a fake AWS token minter and a
// deterministic clock so no test makes a real AWS call (NFR003).
func WithCredentialResolver(r *credentialResolver) CollectorOption {
	return func(c *Collector) {
		if r != nil {
			c.credResolver = r
		}
	}
}

// WithCredentialResolverForTest injects an arbitrary CredentialResolver
// implementation (test seam for #328 / R100.1). Unlike
// WithCredentialResolver it accepts the exported interface, letting a
// test supply a recording fake that captures the exact TargetConfig each
// pool's BeforeConnect closure resolves with — proving a reload-created
// pool re-resolves the credential against the NEW config rather than the
// config captured by a stale pool's closure.
func WithCredentialResolverForTest(r CredentialResolver) CollectorOption {
	return func(c *Collector) {
		if r != nil {
			c.credResolver = r
		}
	}
}

// WithQueryTimeout sets the per-query timeout.
func WithQueryTimeout(d time.Duration) CollectorOption {
	return func(c *Collector) {
		if d > 0 {
			c.queryTimeout = d
		}
	}
}

// WithBeforeQueryForTest injects a hook consulted for each collector's
// query immediately before it executes, receiving the per-query
// context, the live transaction, the collector id, and its real SQL,
// and returning the SQL actually run. It is an execution-faithful test
// seam (#329): a test can block one collector until the per-query
// context (bounded by the remaining per-cycle budget) is done, so the
// real query then observes an already-expired context and returns a
// deadline error WITH THE CONNECTION PRESERVED — exercising the real
// savepoint-recovery path (R108) against live PostgreSQL. No production
// caller sets it; production SQL runs unchanged.
func WithBeforeQueryForTest(hook func(qCtx context.Context, tx pgx.Tx, queryID, sql string) string) CollectorOption {
	return func(c *Collector) {
		c.beforeQueryForTest = hook
	}
}

// WithAllowUnsafeRole enables collection with unsafe role attributes (lab/dev only).
func WithAllowUnsafeRole(allow bool) CollectorOption {
	return func(c *Collector) {
		c.allowUnsafeRole = allow
	}
}

// GetAllowUnsafeRole returns whether unsafe role mode is enabled.
func (c *Collector) GetAllowUnsafeRole() bool {
	return c.allowUnsafeRole
}

// WithHighSensitivityCollectors enables the four definition-text collectors
// flagged HighSensitivity in the query catalog (R075). Off by default.
func WithHighSensitivityCollectors(enabled bool) CollectorOption {
	return func(c *Collector) {
		c.highSensitivityEnabled = enabled
	}
}

// WithCollectArrayRangeHistograms enables pg_stats_array_range_v1
// (#128). Layered ON TOP of WithHighSensitivityCollectors — both
// must be true for the collector to run. Off by default.
func WithCollectArrayRangeHistograms(enabled bool) CollectorOption {
	return func(c *Collector) {
		c.collectArrayRangeHistograms = enabled
	}
}

// WithMetrics attaches a Prometheus registry so collection cycle, per-
// collector outcome, and sqlite persistence counters get updated. Pass
// nil (the default) to disable metric recording.
func WithMetrics(m *metrics.Registry) CollectorOption {
	return func(c *Collector) {
		c.metrics = m
	}
}

// WithCircuitManager replaces the default per-target circuit-breaker
// manager (R097). The daemon constructs one from `signals.circuit`
// config at startup and passes it here so the collector and the API
// layer share the same state.
func WithCircuitManager(m *circuit.Manager) CollectorOption {
	return func(c *Collector) {
		if m != nil {
			c.circuit = m
		}
	}
}

// WithRetention threads the per-class retention config (R099) into
// the collector's cleanup logic. Empty value falls back to the flat
// retention_days argument passed to New.
func WithRetention(r config.RetentionConfig) CollectorOption {
	return func(c *Collector) {
		c.retention = r
	}
}

// Circuit returns the per-target circuit manager. Used by the API
// layer to expose pause / resume endpoints and to surface state in
// /status.
func (c *Collector) Circuit() *circuit.Manager {
	return c.circuit
}

// currentTargets returns a snapshot of the active target list under
// RLock. Used by the cycle loop and the cleanup pass — any code
// that reads c.targets directly bypasses the reload guarantee
// (R100) and risks racing with an in-flight Reload.
func (c *Collector) currentTargets() []config.TargetConfig {
	c.runtimeMu.RLock()
	defer c.runtimeMu.RUnlock()
	out := make([]config.TargetConfig, len(c.targets))
	copy(out, c.targets)
	return out
}

// Targets is the exported sibling of currentTargets, available to
// the API layer so /collect/pause and /collect/resume see the
// reload-updated list. Returns a defensive copy — callers can
// mutate the slice without affecting the collector's state.
func (c *Collector) Targets() []config.TargetConfig {
	return c.currentTargets()
}

// Reload atomically swaps the collector's target list with the
// supplied one (R100). Removed targets have their pools closed
// after the swap; added targets pick up on the next cycle.
//
// Validation is the caller's responsibility — Reload only handles
// the runtime swap. The daemon's SIGHUP handler and the
// POST /reload endpoint both run ValidateStrict on the freshly-
// loaded config BEFORE invoking Reload, and on rejection emit a
// `config_reload_rejected` audit event and leave the running
// state untouched.
//
// Scope of v1 reload (R100):
//   - Add target
//   - Remove target (closes its pool)
//   - Modify connection params or collectors profile of an existing
//     target (closes the old pool so the next cycle re-dials with
//     the new params)
//
// Out of scope of v1 reload:
//   - poll_interval re-arm (the Run ticker keeps its boot value;
//     interval changes require a daemon restart for v1).
//   - retention / sensitivity-flag re-read (set-at-construction
//     until a future iteration).
//
// Callers that need broader config swap should follow up via a
// separate issue.
func (c *Collector) Reload(newTargets []config.TargetConfig) error {
	// R109: reconcile the persisted `targets.enabled` column FIRST. If
	// reconcile fails, we return the error WITHOUT mutating any
	// in-memory state, so the caller sees a clean abort and the
	// daemon stays in a consistent (old) state — rather than a split
	// where the in-memory target list says one thing and the DB
	// `enabled` column says another. (#16: previously this error was
	// logged warn-only after the in-memory swap, so /reload + SIGHUP
	// could appear successful while persistence was stale.)
	enabledNames := make([]string, 0, len(newTargets))
	for _, t := range newTargets {
		if t.Enabled {
			enabledNames = append(enabledNames, t.Name)
		}
	}
	if err := c.db.ReconcileEnabledTargets(enabledNames); err != nil {
		return fmt.Errorf("reconcile enabled targets: %w", err)
	}

	c.runtimeMu.Lock()
	prev := c.targets
	c.targets = make([]config.TargetConfig, len(newTargets))
	copy(c.targets, newTargets)
	c.runtimeMu.Unlock()

	prevByName := make(map[string]config.TargetConfig, len(prev))
	for _, t := range prev {
		prevByName[t.Name] = t
	}
	nextByName := make(map[string]config.TargetConfig, len(newTargets))
	for _, t := range newTargets {
		nextByName[t.Name] = t
	}

	// Close pools for removed AND modified targets. A modified
	// target needs a fresh pool because connection params changed.
	c.poolsMu.Lock()
	for name, pool := range c.pools {
		next, stillThere := nextByName[name]
		if !stillThere {
			pool.Close()
			delete(c.pools, name)
			safety.AuditLog("config_reload_target_removed", "target", name)
			continue
		}
		if !sameConnection(prevByName[name], next) {
			pool.Close()
			delete(c.pools, name)
			safety.AuditLog("config_reload_target_modified", "target", name)
		}
	}
	c.poolsMu.Unlock()

	// Audit-add events for genuinely new targets.
	for name := range nextByName {
		if _, existed := prevByName[name]; !existed {
			safety.AuditLog("config_reload_target_added", "target", name)
		}
	}

	return nil
}

// connIdentity is the derived "connection identity" of a target: the
// exhaustive set of TargetConfig fields that affect how the collector
// dials, secures (TLS), selects, resolves, or caches its connection
// credential (SIGNALS-R100.1, #328). Two targets with equal
// connIdentity dial and authenticate identically, so a reload that
// leaves the identity unchanged may keep the existing pool; any
// difference MUST tear the pool down, because the pool's BeforeConnect
// closure captured the OLD target config and would otherwise keep
// resolving the stale auth method / secret / cloud identity / cert / CA
// even on future connections.
//
// It deliberately EXCLUDES the non-connection fields — the `collectors`
// sensitivity profile and the `Enabled` flag — so a profile-only change
// takes effect without an unnecessary pool drop. TestConnIdentity_
// CoversEveryConnectionField reflects over TargetConfig and fails if a
// newly added field is neither mapped here nor listed as a known
// non-connection field, forcing a test-time decision rather than a
// silent omission from the reload comparison.
type connIdentity struct {
	// Dialing
	Host    string
	Port    int
	DBName  string
	User    string
	SSLMode string
	// TLS: server verification (CA) + mTLS client material
	SSLRootCertFile      string
	SSLCert              string
	SSLKey               string
	SSLKeyPassphraseFile string
	// Credential selection
	AuthMethod                   string
	Region                       string
	AzureClientID                string
	GCPImpersonateServiceAccount string
	SecretRef                    string
	SecretJSONKey                string
	// Credential content (password sources)
	PasswordFile string
	PasswordEnv  string
	PgpassFile   string
	// Credential caching
	MaxCacheTTL time.Duration
}

// connectionIdentity derives the connIdentity of a target.
func connectionIdentity(t config.TargetConfig) connIdentity {
	return connIdentity{
		Host:                         t.Host,
		Port:                         t.Port,
		DBName:                       t.DBName,
		User:                         t.User,
		SSLMode:                      t.SSLMode,
		SSLRootCertFile:              t.SSLRootCertFile,
		SSLCert:                      t.SSLCert,
		SSLKey:                       t.SSLKey,
		SSLKeyPassphraseFile:         t.SSLKeyPassphraseFile,
		AuthMethod:                   t.AuthMethod,
		Region:                       t.Region,
		AzureClientID:                t.AzureClientID,
		GCPImpersonateServiceAccount: t.GCPImpersonateServiceAccount,
		SecretRef:                    t.SecretRef,
		SecretJSONKey:                t.SecretJSONKey,
		PasswordFile:                 t.PasswordFile,
		PasswordEnv:                  t.PasswordEnv,
		PgpassFile:                   t.PgpassFile,
		MaxCacheTTL:                  t.MaxCacheTTL,
	}
}

// sameConnection returns true when two TargetConfigs share the same
// connection identity (SIGNALS-R100.1). Used by Reload to decide
// whether a modified target needs its pool torn down. Any change to a
// dialing, TLS, credential-selection, credential-content, or
// credential-caching field flips this to false and forces a re-dial;
// a change to only the sensitivity profile or the enabled flag leaves
// it true so the pool is preserved.
func sameConnection(a, b config.TargetConfig) bool {
	return connectionIdentity(a) == connectionIdentity(b)
}

// circuitStateLabels returns every circuit-state value as a string
// slice, used by SetCircuitState to keep the gauge complete.
var circuitStateLabels = func() []string {
	out := make([]string, 0, len(circuit.AllStates))
	for _, s := range circuit.AllStates {
		out = append(out, string(s))
	}
	return out
}()

// onCircuitStateChange emits an audit event and updates the Prometheus
// gauge after a circuit transition. Called outside the manager's lock
// by circuit.Manager via SetOnChange.
//
// `meta` carries the operator-supplied actor + reason on manual
// pause/resume transitions (empty on auto). Embedding it in the
// canonical transition event resolves the issue #88 dual-event
// audit-correlation hazard — one event per state change with full
// causal context.
func (c *Collector) onCircuitStateChange(target string, from, to circuit.State, meta circuit.TransitionMeta) {
	switch to {
	case circuit.StateOpen:
		safety.AuditLog("circuit_opened", "target", target, "from", string(from))
	case circuit.StatePaused:
		attrs := []any{"target", target, "from", string(from)}
		if meta.Actor != "" {
			attrs = append(attrs, "actor", meta.Actor)
		}
		if meta.Reason != "" {
			attrs = append(attrs, "reason", meta.Reason)
		}
		safety.AuditLog("circuit_paused", attrs...)
	case circuit.StateClosed:
		switch from {
		case circuit.StateOpen:
			safety.AuditLog("circuit_closed", "target", target)
		case circuit.StatePaused:
			attrs := []any{"target", target}
			if meta.Actor != "" {
				attrs = append(attrs, "actor", meta.Actor)
			}
			safety.AuditLog("circuit_resumed", attrs...)
		}
	}
	c.metrics.SetCircuitState(target, string(to), circuitStateLabels)

	// #455: persist non-closed state so it survives a daemon restart.
	// Only fires on an actual transition (onChange is transition-gated),
	// never per cycle. Best-effort: the in-memory circuit is
	// authoritative for the running process, so a store error is logged,
	// not fatal.
	now := time.Now().UTC().Format(time.RFC3339)
	var perr error
	if to == circuit.StateClosed {
		perr = c.db.DeleteCircuitState(target)
	} else {
		perr = c.db.UpsertCircuitState(target, string(to), now, meta.Reason, meta.Actor, now)
	}
	if perr != nil {
		slog.Warn("persist circuit state failed", "target", target, "state", string(to), "err", perr)
	}
}

// RestoreCircuitState rehydrates persisted circuit-breaker state at
// startup (#455) so an open or operator-paused target stays that way
// across a restart instead of silently resuming collection. Call once
// before the collection loop starts.
func (c *Collector) RestoreCircuitState() error {
	states, err := c.db.GetCircuitStates()
	if err != nil {
		return fmt.Errorf("load persisted circuit state: %w", err)
	}
	for _, s := range states {
		since, perr := time.Parse(time.RFC3339, s.Since)
		if perr != nil {
			since = time.Now() // a malformed timestamp must not drop the restore
		}
		c.circuit.Restore(s.TargetName, circuit.State(s.State), since, s.Reason, s.Actor)
		slog.Info("restored circuit state across restart", "target", s.TargetName, "state", s.State)
	}
	return nil
}

// PauseTarget is the collector-side entry point for the operator
// pause (R097). The API and CLI layers call this; it delegates to
// the circuit manager which records the operator-provided actor +
// reason on the canonical circuit_paused audit event.
//
// reason may be empty — the manager substitutes a sensible default.
//
// Issue #88: the legacy circuit_paused_request supplemental event
// is gone. One state change = one audit event with full causal
// context.
func (c *Collector) PauseTarget(target, reason, actor string) error {
	return c.circuit.Pause(target, reason, actor)
}

// ResumeTarget is the collector-side entry point for operator
// resume. The actor flows through to the manager's onChange
// callback so the canonical circuit_resumed event carries the
// operator identity (issue #88).
func (c *Collector) ResumeTarget(target, actor string) {
	c.circuit.Resume(target, actor)
}

// applyTargetProfile projects a TargetConfig.Collectors block into
// the pgqueries.FilterParams per-target override fields (R098). No
// effect when the target's profile is empty / "default".
func applyTargetProfile(p *pgqueries.FilterParams, tgt config.TargetConfig) {
	switch tgt.Collectors.Profile {
	case "", config.ProfileDefault:
		return
	case config.ProfileRestricted:
		p.ProfileRestricted = true
	case config.ProfileCustom:
		if len(tgt.Collectors.Include) > 0 {
			p.IncludeOnly = make(map[string]bool, len(tgt.Collectors.Include))
			for _, id := range tgt.Collectors.Include {
				p.IncludeOnly[id] = true
			}
		}
		if len(tgt.Collectors.Exclude) > 0 {
			p.Exclude = make(map[string]bool, len(tgt.Collectors.Exclude))
			for _, id := range tgt.Collectors.Exclude {
				p.Exclude[id] = true
			}
		}
	}
}

// recordBypassedChecks stores the specific checks that were bypassed in unsafe mode.
func (c *Collector) recordBypassedChecks(checks []string) {
	c.bypassedChecksMu.Lock()
	defer c.bypassedChecksMu.Unlock()
	c.bypassedChecks = append(c.bypassedChecks, checks...)
}

// GetBypassedChecks returns the checks bypassed in unsafe mode.
func (c *Collector) GetBypassedChecks() []string {
	c.bypassedChecksMu.Lock()
	defer c.bypassedChecksMu.Unlock()
	out := make([]string, len(c.bypassedChecks))
	copy(out, c.bypassedChecks)
	return out
}

// Run starts the collection loop, blocking until ctx is cancelled.
func (c *Collector) Run(ctx context.Context) {
	slog.Info("collector starting", "interval", c.interval, "targets", len(c.currentTargets()))

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	// Initial collection — force all queries as a baseline. nil filter
	// means "collect every enabled target".
	c.cycle(ctx, true, CollectRequest{})

	for {
		select {
		case <-ctx.Done():
			slog.Info("collector stopping")
			c.closePools()
			return
		case <-ticker.C:
			c.cycle(ctx, false, CollectRequest{})
		case req := <-c.collectNowCh:
			slog.Info("on-demand collection triggered", "targets", len(req.Targets), "request_id", req.RequestID)
			c.cycle(ctx, true, req)
		}
	}
}

// cycle runs one collection cycle and then invokes the post-cycle hook
// (#350 scheduled export). The hook runs after every cycle — the initial
// baseline, each scheduled tick, and each on-demand collection — so a fresh
// export follows every collection. A nil hook (the default) is a no-op.
func (c *Collector) cycle(ctx context.Context, forceAll bool, req CollectRequest) {
	c.runCycle(ctx, forceAll, req)
	if c.afterCycle != nil {
		c.afterCycle(ctx)
	}
}

// SetAfterCycle registers the post-cycle hook (#350). It MUST be called
// before Run starts; Run is then the only reader, so no synchronisation is
// needed.
func (c *Collector) SetAfterCycle(fn func(context.Context)) {
	c.afterCycle = fn
}

// CollectNow triggers an immediate collection cycle (non-blocking).
// Returns true when the request was queued, false when the buffer was
// already full and the new request was dropped — R082 Phase 2 lets
// the caller emit a `collect_now_dropped` audit event so the
// correlation id stays in the audit trail even when the cycle never
// fires.
//
// req.Targets nil means "collect every enabled target", preserving
// Mode A semantics. req.RequestID propagates to per-target
// `collection_started` / `collection_completed` audit records.
func (c *Collector) CollectNow(req CollectRequest) bool {
	select {
	case c.collectNowCh <- req:
		return true
	default:
		// Already pending — caller decides how to log the drop.
		return false
	}
}

// LastCollected returns the most recent snapshot time, or empty string.
func (c *Collector) LastCollected() string {
	var ts string
	row := c.db.SQL().QueryRow("SELECT collected_at FROM snapshots ORDER BY collected_at DESC LIMIT 1")
	_ = row.Scan(&ts) // ignore error, empty is fine
	return ts
}

// runCycle runs a collection cycle with overlap protection.
//
// If forceAll is true, all eligible queries are executed regardless
// of cadence. If req.Targets is non-nil, only configured targets
// whose names appear in the filter are collected — R082 Phase 1
// narrowing. nil filter means "collect every enabled target"
// (interval-driven cycles always pass a zero CollectRequest here).
//
// req.RequestID, when non-empty, is propagated to each per-target
// audit event (R082 Phase 2 correlation).
func (c *Collector) runCycle(ctx context.Context, forceAll bool, req CollectRequest) {
	if !c.running.TryLock() {
		slog.Warn("collection cycle skipped — previous cycle still running")
		// R082 audit-consistency: an on-demand request whose cycle
		// never runs because of overlap must produce a terminal
		// dropped event so the request_id has exactly one outcome
		// in the audit trail. Interval-driven cycles (req.RequestID
		// empty) skip silently — the previous cycle's events are
		// already in the audit log.
		if req.RequestID != "" {
			droppedAttrs := []any{
				"request_id", req.RequestID,
				"reason_category", "cycle_overlap_skipped",
			}
			if req.Actor != "" {
				droppedAttrs = append(droppedAttrs, "actor", req.Actor)
			}
			safety.AuditLog("collect_now_dropped", droppedAttrs...)
		}
		return
	}
	defer c.running.Unlock()

	start := time.Now()

	// Build list of enabled targets, narrowed by the optional filter.
	var filterSet map[string]struct{}
	if req.Targets != nil {
		filterSet = make(map[string]struct{}, len(req.Targets))
		for _, name := range req.Targets {
			filterSet[name] = struct{}{}
		}
	}
	var enabled []config.TargetConfig
	// R100: read targets through the accessor so SIGHUP / POST /reload
	// can swap the list between cycles without racing.
	for _, tgt := range c.currentTargets() {
		if !tgt.Enabled {
			continue
		}
		if filterSet != nil {
			if _, ok := filterSet[tgt.Name]; !ok {
				continue
			}
		}
		enabled = append(enabled, tgt)
	}

	// Worker pool: bounded channel semaphore + WaitGroup.
	sem := make(chan struct{}, c.maxConcurrentTargets)
	var wg sync.WaitGroup
	for _, tgt := range enabled {
		tgt := tgt // capture
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}        // acquire
			defer func() { <-sem }() // release

			tgtCtx := ctx
			if c.targetTimeout > 0 {
				var cancel context.CancelFunc
				tgtCtx, cancel = context.WithTimeout(ctx, c.targetTimeout)
				defer cancel()
			}

			if err := c.collectTarget(tgtCtx, tgt, forceAll, req.Force, req.RequestID, req.Actor); err != nil {
				slog.Error("collection failed", "target", tgt.Name, "err", err)
				_ = c.db.InsertEvent("collect_error", fmt.Sprintf("target=%s err=%v", tgt.Name, err))
			}
		}()
	}
	wg.Wait()

	c.cleanup()

	slog.Info("collection cycle completed", "duration_ms", time.Since(start).Milliseconds(), "targets", len(enabled))
}

// reasonBudgetExhausted marks collectors that were due and eligible but
// did not get a turn before the target's per-cycle time budget elapsed
// (R108). Recorded as a skipped run so the status inventory is complete.
const reasonBudgetExhausted = "budget_exhausted"

// budgetSkippedRuns builds one skipped/budget_exhausted run per
// remaining due collector when a cycle stops early on budget
// (R108 / INV-SIGNALS-19). newID supplies run IDs (ULIDs in production;
// injected so the construction is unit-testable without an entropy
// source).
func budgetSkippedRuns(remaining []pgqueries.QueryDef, targetID int64, snapID, collectedAt, versionStr string, newID func() string) []db.QueryRun {
	if len(remaining) == 0 {
		return nil
	}
	runs := make([]db.QueryRun, 0, len(remaining))
	for _, q := range remaining {
		runs = append(runs, db.QueryRun{
			ID:          newID(),
			TargetID:    targetID,
			SnapshotID:  snapID,
			QueryID:     q.ID,
			CollectedAt: collectedAt,
			PGVersion:   versionStr,
			CreatedAt:   collectedAt,
			Status:      "skipped",
			Reason:      reasonBudgetExhausted,
		})
	}
	return runs
}

// warnOnce reports whether an advisory for (target, query, kind) has not
// yet been logged this daemon run, recording it as logged when it returns
// true. It deduplicates per-cycle warnings so a persistent condition is
// surfaced once, not on every poll (R117, #200).
func (c *Collector) warnOnce(target, query, kind string) bool {
	key := target + "\x00" + query + "\x00" + kind
	c.warnedOnceMu.Lock()
	defer c.warnedOnceMu.Unlock()
	if _, seen := c.warnedOnce[key]; seen {
		return false
	}
	c.warnedOnce[key] = struct{}{}
	return true
}

// cycleStatus classifies a completed collection cycle (R108). A non-nil
// err is "failed"; otherwise any failed collector or any
// budget-exhausted skip makes the cycle "partial"; else "success".
func cycleStatus(err error, failed, budgetExhausted int) string {
	switch {
	case err != nil:
		return "failed"
	case failed > 0 || budgetExhausted > 0:
		return "partial"
	default:
		return "success"
	}
}

func (c *Collector) collectTarget(ctx context.Context, tgt config.TargetConfig, forceAll, force bool, requestID, actor string) (err error) {
	cycleStart := time.Now()

	// R097: circuit-breaker gate. Runs BEFORE the R091 floor and
	// BEFORE any PG connection — the cheapest path for a target
	// that's auto-disabled or operator-paused. --force (R092)
	// bypasses R091's min-interval ONLY, not the circuit: an
	// operator who wants to override paused must explicitly
	// `signalsctl collect resume` first (spec § R092 interaction).
	if dec := c.circuit.ShouldCollect(tgt.Name); dec.Skip {
		skippedAttrs := []any{
			"target", tgt.Name,
			"reason_category", dec.Reason,
		}
		if requestID != "" {
			skippedAttrs = append(skippedAttrs, "request_id", requestID)
		}
		if actor != "" {
			skippedAttrs = append(skippedAttrs, "actor", actor)
		}
		slog.Info("collection skipped — circuit gate",
			"target", tgt.Name, "state", dec.State, "reason", dec.Reason)
		safety.AuditLog("collection_skipped", skippedAttrs...)
		return nil
	}

	// R091: per-target min-snapshot-interval check. Runs BEFORE
	// any PG connection so a skipped target costs only the SQLite
	// lookup. The check is bypassed when `force` is set (R092).
	// First-cycle (no prior snapshot) and unknown-target (lookup
	// returns found=false) both fall through to normal collection.
	if !force && c.minSnapshotInterval > 0 {
		last, found, lookupErr := c.db.GetLatestSnapshotTimeByTargetName(tgt.Name)
		if lookupErr != nil {
			// A read failure on the lookup is informational, not
			// blocking. Log and continue — the cycle still runs;
			// the worst case is one extra snapshot during the
			// window, never an unexpected skip.
			slog.Warn("min_interval check: lookup failed; running cycle",
				"target", tgt.Name, "err", lookupErr)
		} else if found {
			if skip, elapsed := ShouldSkipForMinInterval(last, c.minSnapshotInterval, cycleStart, force); skip {
				skippedAttrs := []any{
					"target", tgt.Name,
					"reason_category", "min_interval_not_elapsed",
					"last_collected_at", last.Format(time.RFC3339),
					"elapsed_ms", elapsed.Milliseconds(),
					"min_interval_ms", c.minSnapshotInterval.Milliseconds(),
				}
				if requestID != "" {
					skippedAttrs = append(skippedAttrs, "request_id", requestID)
				}
				if actor != "" {
					skippedAttrs = append(skippedAttrs, "actor", actor)
				}
				slog.Info("collection skipped — min_snapshot_interval not elapsed",
					"target", tgt.Name,
					"last_collected_at", last.Format(time.RFC3339),
					"elapsed_ms", elapsed.Milliseconds(),
					"min_interval_ms", c.minSnapshotInterval.Milliseconds())
				safety.AuditLog("collection_skipped", skippedAttrs...)
				return nil
			}
		}
	}

	var (
		snapID string
		runs   []db.QueryRun
	)
	startedAttrs := []any{"target", tgt.Name}
	if requestID != "" {
		startedAttrs = append(startedAttrs, "request_id", requestID)
	}
	if actor != "" {
		startedAttrs = append(startedAttrs, "actor", actor)
	}
	if force {
		// R092 — every audit event for a forced cycle records the
		// override so an auditor can see exactly which cycles
		// bypassed R091.
		startedAttrs = append(startedAttrs, "forced", true)
	}
	safety.AuditLog("collection_started", startedAttrs...)
	defer func() {
		success, failed, skipped := 0, 0, 0
		failedByReason := map[string]int{}
		skippedByReason := map[string]int{}
		for _, r := range runs {
			switch r.Status {
			case "skipped":
				skipped++
				reason := r.Reason
				if reason == "" {
					reason = "unknown"
				}
				skippedByReason[reason]++
			case "failed":
				failed++
				reason := r.Reason
				if reason == "" {
					reason = "execution_error"
				}
				failedByReason[reason]++
			default:
				success++
			}
		}
		// R108: a budget-exhausted skip makes the cycle partial, same as
		// a failed collector — the cycle did not complete its full due set.
		status := cycleStatus(err, failed, skippedByReason[reasonBudgetExhausted])
		duration := time.Since(cycleStart)
		completedAttrs := []any{
			"target", tgt.Name,
			"snapshot_id", snapID,
			"status", status,
			"duration_ms", duration.Milliseconds(),
			"collectors_total", len(runs),
			"collectors_success", success,
			"collectors_failed", failed,
			"collectors_skipped", skipped,
		}
		if requestID != "" {
			completedAttrs = append(completedAttrs, "request_id", requestID)
		}
		if actor != "" {
			completedAttrs = append(completedAttrs, "actor", actor)
		}
		if force {
			completedAttrs = append(completedAttrs, "forced", true)
		}
		safety.AuditLog("collection_completed", completedAttrs...)

		// R126: persist the last-cycle outcome durably so a later
		// export can distinguish "last collection failed: <category>"
		// from "no collection has run yet" (R125 / FC-05). A failed
		// cycle records the bounded failure category; success/partial
		// clears any prior failure marker. Metadata only — no
		// credential or secret is written (INV-SIGNALS-07). A write
		// failure here is logged, not fatal: the cycle's real outcome
		// already stands, and the worst case is a stale marker that a
		// later cycle overwrites.
		outcomeCategory := ""
		if status == "failed" {
			outcomeCategory = classifyCollectionFailure(err)
		}
		if recErr := c.db.RecordCycleOutcome(tgt.Name, status, outcomeCategory); recErr != nil {
			slog.Warn("failed to persist cycle outcome (R126)",
				"target", tgt.Name, "status", status, "err", recErr)
		}

		c.metrics.ObserveCollection(tgt.Name, status, duration.Seconds())
		c.metrics.AddCollectorOutcomes(tgt.Name, success, failedByReason, skippedByReason)
		if status == "failed" {
			c.metrics.ObserveCollectionFailure(tgt.Name, classifyCollectionFailure(err))
		} else {
			c.metrics.SetLastSuccessfulCollection(tgt.Name, float64(time.Now().Unix()))
		}

		// R097: feed the circuit-breaker state machine. A cycle
		// that returned a non-nil err counts as a failure; everything
		// else (success or partial) resets the consecutive-fail
		// counter. The Manager handles the closed → open
		// transition + audit event via its onChange callback.
		if err != nil {
			c.circuit.RecordFailure(tgt.Name)
		} else {
			c.circuit.RecordSuccess(tgt.Name)
		}
	}()

	pool, err := c.getPool(ctx, tgt)
	if err != nil {
		return fmt.Errorf("connect %s: %w", tgt.Name, err)
	}

	// Register/update target in DB — store only non-secret metadata.
	targetID, err := c.db.UpsertTarget(
		tgt.Name, tgt.Host, tgt.Port, tgt.DBName, tgt.User,
		tgt.SSLMode, tgt.SecretType(), tgt.CredentialSourceRef(), tgt.Enabled,
	)
	if err != nil {
		return fmt.Errorf("upsert target %s: %w", tgt.Name, err)
	}

	// --- Runtime safety validation (fail-closed) ---
	safetyResult, safetyErr := ValidateRoleSafety(ctx, pool)
	if safetyErr != nil {
		return fmt.Errorf("safety validation failed for %s: %w", tgt.Name, safetyErr)
	}

	// Log warnings (non-blocking).
	for _, w := range safetyResult.Warnings {
		slog.Warn("safety hygiene warning", "target", tgt.Name, "warning", w)
	}

	// Check hard failures.
	if !safetyResult.IsSafe() {
		if c.allowUnsafeRole {
			slog.Warn("UNSAFE MODE: bypassing safety checks — not recommended for production",
				"target", tgt.Name, "bypassed_checks", safetyResult.HardFailures)
			// Record bypassed checks for export metadata.
			c.recordBypassedChecks(safetyResult.HardFailures)
		} else {
			return fmt.Errorf("collection blocked for target %s: %s", tgt.Name, safetyResult.Error())
		}
	}

	// Acquire a dedicated connection from the pool. This ensures all
	// safety checks, timeouts, and queries execute on the SAME connection.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection for %s: %w", tgt.Name, err)
	}
	defer conn.Release()

	// Verify session read-only posture on this specific connection.
	var readOnly string
	if err := conn.QueryRow(ctx, "SHOW default_transaction_read_only").Scan(&readOnly); err != nil {
		return fmt.Errorf("session safety check failed for %s: cannot verify read-only posture: %w", tgt.Name, err)
	}
	if readOnly != "on" {
		return fmt.Errorf("session safety check failed for %s: session is not read-only (default_transaction_read_only=%s)", tgt.Name, readOnly)
	}

	// Begin a READ ONLY transaction on this dedicated connection.
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Apply timeouts via SET LOCAL inside this transaction. SET LOCAL
	// ensures timeouts apply to exactly this transaction on this connection
	// and are automatically reset when the transaction ends.
	//
	// These three timeouts are mandatory safety guards: without them a
	// runaway query, lock conflict, or idle-in-transaction wedge can
	// hold a backend indefinitely. If SET LOCAL itself fails (the only
	// realistic causes are permission revocation on the GUC, an aborted
	// transaction state, or a dropped connection mid-statement) the
	// safety contract is broken and we must not run diagnostic queries.
	// post-0.3.1 H-004.
	stmtTimeoutMs := int(c.queryTimeout.Milliseconds())
	lockTimeoutMs := 5000 // 5 seconds — conservative default
	idleTimeoutMs := int(c.targetTimeout.Milliseconds())
	for _, t := range []struct {
		param string
		value int
	}{
		{"statement_timeout", stmtTimeoutMs},
		{"lock_timeout", lockTimeoutMs},
		{"idle_in_transaction_session_timeout", idleTimeoutMs},
	} {
		if _, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL %s = %d", t.param, t.value)); err != nil {
			return fmt.Errorf("set %s for %s: %w (timeout safety cannot be enforced; aborting cycle)", t.param, tgt.Name, err)
		}
	}

	// Step 1: Discovery (R081). One probe yields the version, major,
	// installed extensions, current_database, current_user.
	disc, err := pgqueries.Discover(ctx, tx)
	if err != nil {
		return fmt.Errorf("discovery for %s: %w", tgt.Name, err)
	}
	versionStr := disc.ServerVersion

	// Fail closed when the server is older than the supported window.
	// We have no per-major catalog files for PG < MinSupportedMajor and
	// no realistic test surface, so running against such targets risks
	// silent miscollection. Returning a bounded `version_unsupported`
	// error makes the failure auditable and surfaces in metrics.
	if disc.MajorVersion < pgqueries.MinSupportedMajor {
		return fmt.Errorf("collection blocked for target %s: PostgreSQL major %d is below the supported minimum %d (reason=version_unsupported)",
			tgt.Name, disc.MajorVersion, pgqueries.MinSupportedMajor)
	}

	// PG 19+: experimental — no first-class catalog support yet. Fall
	// back to the highest supported major (PG 18) catalog so collection
	// still works against pre-release servers, but log it loudly so the
	// operator notices.
	effectiveMajor := disc.MajorVersion
	if pgqueries.IsExperimentalMajor(effectiveMajor) {
		slog.Warn("PG major above supported window — falling back to highest supported catalog",
			"target", tgt.Name,
			"server_major", disc.MajorVersion,
			"falling_back_to", pgqueries.MaxSupportedMajor,
		)
		effectiveMajor = pgqueries.MaxSupportedMajor
	}

	// Step 2: Filter eligible queries with version-aware SQL resolution.
	// R098: per-target sensitivity profile, layered on top of the
	// daemon-wide gates.
	filterParams := pgqueries.FilterParams{
		PGMajorVersion:              effectiveMajor,
		Extensions:                  disc.Extensions,
		ExtensionVersions:           disc.ExtensionVersions, // R115
		HighSensitivityEnabled:      c.highSensitivityEnabled,
		CollectArrayRangeHistograms: c.collectArrayRangeHistograms,
	}
	applyTargetProfile(&filterParams, tgt)
	eligible := pgqueries.Filter(filterParams)
	gatedByReason := pgqueries.GatedIDsByReason(filterParams)
	// R079 / #79: surface eligible collector count as a gauge so
	// operators can alert on sudden coverage drops (extension
	// uninstall, version downgrade, profile change).
	c.metrics.SetEligibleCollectors(tgt.Name, len(eligible))

	// Step 3b: Apply cadence planner unless forceAll.
	queries := eligible
	if !forceAll {
		lastRuns, lrErr := c.db.GetLastRunTimes(targetID)
		if lrErr != nil {
			slog.Warn("cadence planner: GetLastRunTimes failed, running all eligible", "target", tgt.Name, "err", lrErr)
		} else {
			queries = pgqueries.SelectDue(time.Now().UTC(), eligible, lastRuns)
			if len(queries) == 0 {
				slog.Debug("no queries due this cycle", "target", tgt.Name, "eligible", len(eligible))
				_ = tx.Rollback(ctx)
				return nil
			}
		}
	}

	now := time.Now().UTC()
	snapID = ulid.MustNew(ulid.Timestamp(now), c.entropy).String()
	collectedAt := now.Format(time.RFC3339)

	data := &SnapshotData{Version: versionStr}
	var results []db.QueryResult

	newRunID := func() string {
		return ulid.MustNew(ulid.Timestamp(now), c.entropy).String()
	}

	// Step 4: Execute each query with budget-aware timeout.
	for i, q := range queries {
		// Check if target context is already expired.
		if ctx.Err() != nil {
			// R108: the budget elapsed before this collector (and the
			// rest) was attempted — record every remaining due collector
			// as skipped/budget_exhausted so the inventory is complete.
			runs = append(runs, budgetSkippedRuns(queries[i:], targetID, snapID, collectedAt, versionStr, newRunID)...)
			slog.Warn("target budget exhausted, remaining due collectors recorded as skipped",
				"target", tgt.Name, "remaining", len(queries[i:]))
			break
		}

		// Per-query timeout: min(queryTimeout, q.Timeout, remaining target budget).
		qTimeout := c.queryTimeout
		if q.Timeout > 0 && q.Timeout < qTimeout {
			qTimeout = q.Timeout
		}
		if deadline, ok := ctx.Deadline(); ok {
			remaining := time.Until(deadline)
			if remaining < qTimeout {
				qTimeout = remaining
			}
		}

		qCtx, qCancel := context.WithTimeout(ctx, qTimeout)
		start := time.Now()

		// Use a savepoint so a single query failure does not abort
		// the entire READ ONLY transaction (PostgreSQL marks the
		// transaction as aborted after any error). post-0.3.1
		// M-005: every savepoint operation's error is now checked.
		// SAVEPOINT failure is fatal — without it the per-query
		// recovery contract is broken and a downstream failure would
		// poison the whole cycle. ROLLBACK TO and RELEASE failures
		// are also fatal because the transaction state is now
		// inconsistent and continuing risks stale or partial data
		// reaching the snapshot.
		savepointName := fmt.Sprintf("signals_q_%d", len(runs))
		if _, spErr := tx.Exec(ctx, "SAVEPOINT "+savepointName); spErr != nil {
			qCancel()
			return fmt.Errorf("savepoint %s for %s: %w", savepointName, tgt.Name, spErr)
		}

		sql := q.SQL
		if c.beforeQueryForTest != nil {
			sql = c.beforeQueryForTest(qCtx, tx, q.ID, sql)
		}
		rows, qErr := queryToMaps(qCtx, tx, sql)
		elapsed := time.Since(start)
		qTimedOut := qCtx.Err() == context.DeadlineExceeded
		qCancel()

		// R108 (#329): when the query drained the per-cycle budget, the
		// parent ctx is now expired too — so the savepoint bookkeeping
		// below MUST run under a bounded fresh context, not ctx. With ctx
		// pgx rejects ROLLBACK TO SAVEPOINT / RELEASE ("context deadline
		// exceeded"), collectTarget returns a hard error here, and
		// execution never reaches the block that appends the remaining
		// budget_exhausted skipped runs or persists the partial cycle —
		// the exact completeness gap #8 / SIGNALS-R108 was closed to fix.
		// This mirrors the fresh-context commit at the end of the cycle:
		// transaction bookkeeping is not governed by the elapsed budget.
		spCtx, spCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if qErr != nil {
			// Roll back to the savepoint to recover the transaction.
			if _, rbErr := tx.Exec(spCtx, "ROLLBACK TO SAVEPOINT "+savepointName); rbErr != nil {
				spCancel()
				return fmt.Errorf("rollback to savepoint %s for %s: %w (transaction state inconsistent after query error: %v)", savepointName, tgt.Name, rbErr, qErr)
			}
		}
		if _, relErr := tx.Exec(spCtx, "RELEASE SAVEPOINT "+savepointName); relErr != nil {
			spCancel()
			return fmt.Errorf("release savepoint %s for %s: %w", savepointName, tgt.Name, relErr)
		}
		spCancel()

		runID := ulid.MustNew(ulid.Timestamp(now), c.entropy).String()

		run := db.QueryRun{
			ID:          runID,
			TargetID:    targetID,
			SnapshotID:  snapID,
			QueryID:     q.ID,
			CollectedAt: collectedAt,
			PGVersion:   versionStr,
			DurationMS:  int(elapsed.Milliseconds()),
			CreatedAt:   collectedAt,
		}

		if qErr != nil {
			run.Error = qErr.Error()
			// R116 (#200) / #305: a collector flagged for a privilege
			// degrade (OwnerOnlyDegrade or PrivilegedViewDegrade) that hits
			// a permission-denied error is recorded skipped, not failed —
			// an expected privilege boundary, not a fault. Every other
			// failure keeps status=failed.
			run.Status, run.Reason = classifyQueryFailure(q.OwnerOnlyDegrade, q.PrivilegedViewDegrade, qErr)
			switch {
			case run.Status == "skipped" && run.Reason == reasonPrivilegeOwnerOnly:
				// R117 (#200): advise once per (target, collector), not
				// every poll — and with correct wording (ownership, not
				// pg_monitor, grants the owner-only catalog).
				if c.warnOnce(tgt.Name, q.ID, "owner_only") {
					slog.Warn("collector skipped: pg_statistic_ext_data has PUBLIC SELECT revoked and is not readable by a least-privilege monitoring role (pg_monitor does not confer it). To collect it, GRANT SELECT ON pg_catalog.pg_statistic_ext_data to the monitoring role — no superuser required — otherwise this is an expected privilege boundary, recorded skipped, not failed",
						"query", q.ID, "target", tgt.Name)
				}
			case run.Status == "skipped" && run.Reason == reasonPrivilegeRestricted:
				// #305: pg_hba_file_rules needs SELECT on the view plus
				// EXECUTE on pg_hba_file_rules() (pg_monitor /
				// pg_read_all_settings do not grant them). Advise once per
				// (target, collector) how to grant read access.
				if c.warnOnce(tgt.Name, q.ID, "privilege_restricted") {
					slog.Warn("collector skipped: pg_hba_file_rules needs GRANT SELECT ON pg_catalog.pg_hba_file_rules and GRANT EXECUTE ON FUNCTION pg_catalog.pg_hba_file_rules() to the monitoring role (pg_monitor/pg_read_all_settings do not grant them) — recorded skipped, not failed",
						"query", q.ID, "target", tgt.Name)
				}
			case isPermissionDenied(qErr):
				if c.warnOnce(tgt.Name, q.ID, "permission_denied") {
					slog.Warn("query permission denied — grant pg_monitor to the monitoring role",
						"query", q.ID, "target", tgt.Name)
				}
			case ctx.Err() != nil:
				slog.Warn("query timed out (target budget exhausted)",
					"query", q.ID, "target", tgt.Name, "duration_ms", elapsed.Milliseconds())
			case qTimedOut:
				slog.Warn("query timed out",
					"query", q.ID, "target", tgt.Name, "timeout", qTimeout, "duration_ms", elapsed.Milliseconds())
			default:
				slog.Warn("query failed", "query", q.ID, "target", tgt.Name, "err", qErr)
			}
			runs = append(runs, run)

			// If target context expired, stop processing more queries —
			// but first record the remaining due collectors as skipped so
			// the status inventory stays complete (R108).
			if ctx.Err() != nil {
				runs = append(runs, budgetSkippedRuns(queries[i+1:], targetID, snapID, collectedAt, versionStr, newRunID)...)
				break
			}
			continue
		}

		// Do NOT record this run as success yet. A run is success ⟺ its
		// payload is persisted (#312): the status is set only AFTER a
		// successful encode + result append below. An encode failure
		// records a FAILED run — never an orphaned success that would
		// claim row_count rows while the export carries no payload (a
		// consumer joining runs→results would then silently see no
		// evidence — the missing-FK-index miss in Elevarq/Analyzer#1871).

		// FDW post-processing: redact credential-shaped option values
		// in fdw_*_v1 collector rows BEFORE NDJSON encoding so the
		// cleartext never reaches disk. No-op for non-FDW collectors.
		// Spec: specifications/collectors/fdw_*_v1.md REDACT-R001..R004.
		redactFDWRowsIfNeeded(q.ID, rows)

		// Statement-text post-processing: redact structured credential
		// literals (PASSWORD '…', conninfo password=) in collectors that
		// carry raw SQL text (pg_stat_statements_v1.query) BEFORE encoding,
		// so cleartext never reaches disk or the export. No-op otherwise.
		// Spec: specifications/collectors/pg_stat_statements_v1.md
		// REDACT-R001..R003 (#188 / INV-SIGNALS-07).
		redactStatementSecretsIfNeeded(q.ID, rows)

		// R075 (revised, issue #6): when the operator has opted out of
		// high-sensitivity collection, NULL the collector's declared
		// SensitiveColumns in each row so the sensitive payload never
		// reaches disk while the row's non-sensitive diagnostic columns
		// survive. No-op when high-sensitivity is enabled (the default)
		// or when the collector has no SensitiveColumns (skip-path
		// collectors are already excluded by Filter).
		redactHighSensitivityColumnsIfNeeded(q, rows, c.highSensitivityEnabled)

		// Encode result as NDJSON.
		payload, compressed, sizeBytes, encErr := db.EncodeNDJSON(rows)
		if encErr != nil {
			// INVARIANT (#312): status="success" ⟺ a joinable QueryResult
			// payload exists with row_count rows. An encode failure must
			// record a FAILED run, never an orphaned success — otherwise
			// collector_status/query_runs claim success while
			// query_results carries no payload, and a consumer silently
			// sees no evidence.
			slog.Warn("encode failed", "query", q.ID, "err", encErr)
			run.Error = encErr.Error()
			run.Status = "failed"
			run.Reason = "encode_failed"
			run.RowCount = len(rows)
			runs = append(runs, run)
			continue
		}

		// Payload encoded — only now is it safe to record the run as
		// success AND append its result. Kept adjacent so the status
		// manifest and the data payload can never disagree (#312).
		run.RowCount = len(rows)
		run.Status = "success"
		runs = append(runs, run)

		results = append(results, db.QueryResult{
			RunID:      runID,
			Payload:    payload,
			Compressed: compressed,
			SizeBytes:  sizeBytes,
		})

		// Populate legacy SnapshotData for backward compatibility.
		populateSnapshotField(data, q.ID, rows)
	}

	// Commit the read-only transaction with a fresh, short context.
	// The per-cycle budget (ctx) governs query execution, not the
	// bookkeeping commit — committing under the (possibly elapsed)
	// budget would fail an over-budget cycle and discard the complete
	// status inventory R108 just recorded. R108: persistence must
	// survive the exhausted budget.
	commitCtx, commitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	cErr := tx.Commit(commitCtx)
	commitCancel()
	if cErr != nil {
		return fmt.Errorf("commit tx for %s: %w", tgt.Name, cErr)
	}

	// Step 4b: Record gated collectors as skipped runs so
	// collector_status.json contains exactly one entry per registered
	// collector that was relevant to this target. Without this the
	// status file is silent about why a collector did not run — the
	// operator has to compare against the registry to notice. Reasons:
	// version_unsupported (MinPGVersion gate), extension_missing
	// (required extension not present), config_disabled (R075
	// high-sensitivity gate). Each collector appears under exactly one
	// reason; precedence is enforced by GatedIDsByReason.
	for _, reason := range []string{
		pgqueries.GateReasonVersionUnsupported,
		pgqueries.GateReasonExtensionMissing,
		pgqueries.GateReasonConfigDisabled,
	} {
		for _, id := range gatedByReason[reason] {
			runID := ulid.MustNew(ulid.Timestamp(now), c.entropy).String()
			runs = append(runs, db.QueryRun{
				ID:          runID,
				TargetID:    targetID,
				SnapshotID:  snapID,
				QueryID:     id,
				CollectedAt: collectedAt,
				PGVersion:   versionStr,
				CreatedAt:   collectedAt,
				Status:      "skipped",
				Reason:      reason,
			})
		}
	}

	// Build the legacy monolithic snapshot.
	payload, err := MarshalPayload(data)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	snap := db.Snapshot{
		ID:          snapID,
		TargetID:    targetID,
		CollectedAt: collectedAt,
		PGVersion:   data.Version,
		Payload:     json.RawMessage(payload),
		SizeBytes:   len(payload),
	}

	// Step 5: Persist snapshot + runs + results atomically (R077). A
	// failure here rolls everything back so an export never sees a
	// snapshot whose query runs are missing or vice versa.
	if dbErr := c.db.InsertCollectionAtomic(snap, runs, results); dbErr != nil {
		c.metrics.IncSQLitePersistenceFailure()
		return fmt.Errorf("persist collection cycle for %s: %w", tgt.Name, dbErr)
	}

	slog.Info("snapshot collected", "target", tgt.Name, "id", snap.ID, "size", snap.SizeBytes,
		"pg_version", data.Version, "queries_due", len(runs), "queries_eligible", len(eligible))
	_ = c.db.InsertTargetEvent(targetID, "snapshot_collected", fmt.Sprintf("target=%s id=%s queries=%d", tgt.Name, snap.ID, len(runs)))

	return nil
}

func (c *Collector) getPool(ctx context.Context, tgt config.TargetConfig) (*pgxpool.Pool, error) {
	c.poolsMu.Lock()
	defer c.poolsMu.Unlock()

	if pool, ok := c.pools[tgt.Name]; ok {
		return pool, nil
	}

	// Build connection config from structured fields, resolving secrets at runtime.
	connCfg, err := connConfigFunc(tgt)
	if err != nil {
		return nil, err
	}

	poolCfg, err := pgxpool.ParseConfig("")
	if err != nil {
		return nil, err
	}
	poolCfg.ConnConfig = connCfg
	poolCfg.MaxConns = 2

	// Re-resolve the credential on each new connection: password targets
	// re-read their secret to support rotation; aws_rds_iam targets mint
	// or reuse a cached, short-lived IAM token (#93/#94); mtls targets
	// re-read the client cert/key so a rotated cert is picked up without a
	// restart (#98). The resolver dispatches on auth_method; the resolved
	// value is applied as a password or a TLS client certificate per Kind.
	//
	// The closure captures `tgt` at pool-creation time, so a stale pool
	// keeps resolving the OLD config even on future connections. Reload
	// (R100.1) closes the pool whenever any connection/credential field
	// changes precisely so the NEXT getPool rebuilds this closure with
	// the NEW tgt (regression covered by
	// TestReload_NewConnectionResolvesWithNewConfig).
	poolCfg.BeforeConnect = c.beforeConnectFor(tgt)

	// OC-R005: normalize the PostgreSQL internal "char" type (OID 18) to
	// text on every pooled connection. pgx's default QCharCodec decodes
	// OID 18 as an integer byte value, so any collector column of this
	// type selected without an explicit ::text cast serializes as a JSON
	// number (e.g. relkind → 114 instead of "r"). Registering a TextCodec
	// for OID 18 makes rows.Values() return the char as its text form for
	// ALL collectors and queries, present and future — matching col::text
	// semantics (the text protocol cannot carry an embedded NUL, so a
	// non-set attidentity comes back as "" exactly as the cast would).
	// This fixes the class centrally rather than per column (#319,
	// follow-up to #312/#313's leaky per-column ::text sweep).
	poolCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		conn.TypeMap().RegisterType(&pgtype.Type{
			Name: "char", OID: pgtype.QCharOID, Codec: pgtype.TextCodec{},
		})
		return nil
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, err
	}

	c.pools[tgt.Name] = pool
	return pool, nil
}

// beforeConnectFor builds the pgx BeforeConnect hook for a target,
// capturing `tgt` so the resolved credential reflects exactly the
// config that was in effect when the pool was created (#93/#94/#98).
// Extracted from getPool so the reload regression test
// (TestReload_NewConnectionResolvesWithNewConfig) can drive the exact
// production closure via EnsurePoolBeforeConnectForTest.
func (c *Collector) beforeConnectFor(tgt config.TargetConfig) func(context.Context, *pgx.ConnConfig) error {
	return func(ctx context.Context, cfg *pgx.ConnConfig) error {
		cred, err := c.credResolver.Resolve(ctx, tgt)
		if err != nil {
			slog.Error("failed to resolve credential for target", "target", tgt.Name, "auth_method", tgt.EffectiveAuthMethod(), "err", redactError(err))
			return fmt.Errorf("resolve credential: %w", redactError(err))
		}
		switch cred.Kind {
		case CredKindCertificate:
			// mtls (#98): present the client certificate during the TLS
			// handshake. verify-full is enforced at config validation, so
			// pgx has built a TLSConfig we extend (never replace).
			if cfg.TLSConfig == nil {
				return fmt.Errorf("mtls target %s: connection has no TLS config (sslmode must be verify-full)", tgt.Name)
			}
			if cred.ClientCert != nil {
				cfg.TLSConfig.Certificates = []tls.Certificate{*cred.ClientCert}
			}
		default:
			cfg.Password = cred.Password
		}
		return nil
	}
}

// ---------------------------------------------------------------------------
// Test seams for #328 / R100.1 (connection-identity pool invalidation).
//
// The reload/pool-invalidation behaviour lives entirely behind the
// unexported pools map. These exported helpers let the external
// `tests` package observe pool presence and drive a pool's captured
// BeforeConnect closure WITHOUT a live PostgreSQL — pgxpool.New is
// lazy, so a seeded pool never dials until Acquire, which the tests
// never call. They must never be used outside tests.
// ---------------------------------------------------------------------------

// SeedPoolForTest installs a lazy (non-dialing) pool for the named
// target so a subsequent Reload can be observed closing it. Returns an
// error if the placeholder pool cannot be constructed.
func (c *Collector) SeedPoolForTest(name string) error {
	poolCfg, err := pgxpool.ParseConfig("postgres://127.0.0.1:1/seed")
	if err != nil {
		return err
	}
	// A non-zero MinConns would make pgxpool dial eagerly; keep it 0 so
	// the pool stays purely in-memory until an Acquire the tests avoid.
	poolCfg.MinConns = 0
	pool, err := pgxpool.NewWithConfig(context.Background(), poolCfg)
	if err != nil {
		return err
	}
	c.poolsMu.Lock()
	defer c.poolsMu.Unlock()
	if existing, ok := c.pools[name]; ok {
		existing.Close()
	}
	c.pools[name] = pool
	return nil
}

// HasPoolForTest reports whether a live pool is currently registered
// for the named target.
func (c *Collector) HasPoolForTest(name string) bool {
	c.poolsMu.Lock()
	defer c.poolsMu.Unlock()
	_, ok := c.pools[name]
	return ok
}

// EnsurePoolBeforeConnectForTest builds (via the production getPool
// path) a pool for tgt and returns its BeforeConnect closure so a test
// can invoke it directly — verifying which TargetConfig the pool's
// credential resolution captured. Because getPool is lazy, no dial
// occurs. The returned closure is exactly the one the real pool uses.
func (c *Collector) EnsurePoolBeforeConnectForTest(ctx context.Context, tgt config.TargetConfig) (func(context.Context, *pgx.ConnConfig) error, error) {
	if _, err := c.getPool(ctx, tgt); err != nil {
		return nil, err
	}
	// Rebuild the identical closure the pool holds; both capture the
	// same tgt value passed to getPool, so invoking this observes the
	// same credResolver.Resolve(tgt) the pool would issue.
	return c.beforeConnectFor(tgt), nil
}

func (c *Collector) closePools() {
	c.poolsMu.Lock()
	defer c.poolsMu.Unlock()

	for name, pool := range c.pools {
		pool.Close()
		slog.Info("closed pool", "target", name)
	}
}

func (c *Collector) cleanup() {
	// #443: refresh the store-size gauge every pass — even when
	// retention is disabled — so operators can still alert on growth.
	// Deferred so it reflects any space reclaimed by the VACUUM below.
	defer c.updateStoreSizeMetric()

	if c.retentionDays <= 0 && !c.retention.IsSet() {
		return
	}

	// R110: hold the exclusive retention lock for the duration of the
	// destructive DELETEs so a concurrent export sees either the
	// pre-cleanup state or the post-cleanup state, never a mix that
	// would let it reference rows that have just been removed (the
	// "missing result payload" tear). Cleanup is short-lived; an
	// export overlapping it briefly waits.
	release := c.db.LockRetention()
	defer release()

	// R099: per-class retention. Each retention class has its own
	// cutoff; query_runs are pruned per class so high-cadence data
	// (RetentionShort) doesn't linger in storage just because slow-
	// changing data (RetentionLong) wants its rows kept.
	//
	// Snapshot rows are pruned by the LARGEST class cutoff so a
	// snapshot stays alive as long as ANY class still has rows
	// inside its window. Backward-compat: when only retentionDays
	// is set (flat config), every class collapses to that value
	// and the behaviour matches today's single-cutoff pruning.
	now := time.Now().UTC()
	totalRunsDeleted := int64(0)
	for _, class := range []string{"short", "medium", "long"} {
		days := c.retention.DaysFor(class, c.retentionDays)
		if days <= 0 {
			continue
		}
		cutoff := now.AddDate(0, 0, -days).Format(time.RFC3339)
		deletedRuns, err := c.db.DeleteQueryRunsOlderThanByClass(class, cutoff)
		if err != nil {
			slog.Error("query runs cleanup failed", "class", class, "err", err)
			continue
		}
		if deletedRuns > 0 {
			slog.Info("query runs cleanup complete", "class", class, "deleted", deletedRuns, "cutoff", cutoff)
			totalRunsDeleted += deletedRuns
		}
	}

	// Snapshot rows: prune those older than the LARGEST cutoff so
	// long-class data still has its snapshot row alive.
	snapshotsDeleted := int64(0)
	maxDays := c.retention.MaxDays(c.retentionDays)
	if maxDays > 0 {
		snapCutoff := now.AddDate(0, 0, -maxDays).Format(time.RFC3339)
		deleted, err := c.db.DeleteSnapshotsOlderThan(snapCutoff)
		if err != nil {
			slog.Error("snapshot cleanup failed", "err", err)
		} else if deleted > 0 {
			slog.Info("snapshot cleanup complete", "deleted", deleted, "cutoff", snapCutoff)
			snapshotsDeleted = deleted
		}
	}

	// #443: VACUUM to reclaim the freed space. SQLite never shrinks the
	// file on its own, so without this the store grows monotonically
	// even under active retention. Only VACUUM when rows were actually
	// deleted (VACUUM rewrites the whole file — pointless when nothing
	// was freed). A SQLITE_BUSY is logged, not fatal: the next
	// retention pass retries; the collection cycle is unaffected.
	if totalRunsDeleted > 0 || snapshotsDeleted > 0 {
		before, _ := c.db.SizeBytes()
		if err := c.db.Vacuum(); err != nil {
			slog.Warn("store vacuum failed; will retry next retention pass", "err", err)
		} else if after, err := c.db.SizeBytes(); err == nil {
			slog.Info("store vacuum complete",
				"runs_deleted", totalRunsDeleted, "snapshots_deleted", snapshotsDeleted,
				"bytes_before", before, "bytes_after", after, "bytes_reclaimed", before-after)
		}
	}
}

// updateStoreSizeMetric refreshes the signals_store_size_bytes gauge
// from the current on-disk size (#443).
func (c *Collector) updateStoreSizeMetric() {
	if sz, err := c.db.SizeBytes(); err == nil {
		c.metrics.SetStoreSizeBytes(sz)
	}
}

// classifyCollectionFailure maps a collectTarget hard-error into the
// bounded reason enum used by the metrics labels. Keeps cardinality
// fixed (R079) and avoids leaking raw error text into metric output.
func classifyCollectionFailure(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.HasPrefix(msg, "connect "):
		return "connect_error"
	case strings.Contains(msg, "version_unsupported"):
		return "version_unsupported"
	case strings.Contains(msg, "timeout safety cannot be enforced"):
		return "timeout_setup"
	case strings.Contains(msg, "safety validation failed") ||
		strings.Contains(msg, "collection blocked"):
		return "safety_check"
	case strings.Contains(msg, "persist collection cycle"):
		return "persistence"
	default:
		return "internal"
	}
}

// isPermissionDenied returns true if the error is a PostgreSQL
// "insufficient_privilege" error (SQLSTATE 42501).
func isPermissionDenied(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "42501"
	}
	return false
}

// queryToMaps executes a query and returns each row as a map[string]any.
func queryToMaps(ctx context.Context, tx pgx.Tx, query string) ([]map[string]any, error) {
	rows, err := tx.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	descs := rows.FieldDescriptions()
	var result []map[string]any

	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, err
		}
		m := make(map[string]any, len(descs))
		for i, desc := range descs {
			m[desc.Name] = values[i]
		}
		result = append(result, m)
	}

	return result, rows.Err()
}
