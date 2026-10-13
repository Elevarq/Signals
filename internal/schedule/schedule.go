// Package schedule turns a cron expression into the next collection fire
// time (#491). It is the deterministic, dependency-light core of scheduled
// collection: given a parsed standard 5-field cron expression, an anchor
// timezone (UTC by default), and an optional jitter, it answers one question —
// "when is the next fire strictly after this instant?" — with `now` and the
// jitter randomness injected so the behaviour is fully unit-testable without a
// live clock.
//
// It deliberately does NOT run anything: the collector owns the loop
// (wait → collect → recompute). This package only computes times, so missed
// slots are never back-filled — Next always returns a time strictly in the
// future relative to the instant passed in (R-SCHED-05 / no catch-up).
package schedule

import (
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

// Schedule computes successive collection fire times from a cron expression.
type Schedule struct {
	expr   string
	spec   cron.Schedule
	loc    *time.Location
	jitter time.Duration
	// randN returns a pseudo-random value in [0, n). Injected for
	// deterministic tests; defaults to a crypto-free source seeded by the
	// caller. Only used when jitter > 0.
	randN func(n int64) int64
}

// Options configures a Schedule.
type Options struct {
	// Expr is a standard 5-field cron expression (required, non-empty).
	Expr string
	// Timezone is an IANA location name; empty means UTC.
	Timezone string
	// Jitter, when > 0, delays each fire by a uniform random amount in
	// [0, Jitter). Must be >= 0.
	Jitter time.Duration
	// RandN is the randomness source for jitter; nil installs a default.
	RandN func(n int64) int64
}

// New parses opts into a Schedule, failing on an invalid cron expression,
// unknown timezone, or negative jitter (R-SCHED-01..03 / B7).
func New(opts Options) (*Schedule, error) {
	if opts.Expr == "" {
		return nil, fmt.Errorf("schedule: empty cron expression")
	}
	spec, err := cron.ParseStandard(opts.Expr)
	if err != nil {
		return nil, fmt.Errorf("schedule: invalid cron expression %q: %w", opts.Expr, err)
	}
	loc := time.UTC
	if opts.Timezone != "" {
		l, err := time.LoadLocation(opts.Timezone)
		if err != nil {
			return nil, fmt.Errorf("schedule: unknown timezone %q: %w", opts.Timezone, err)
		}
		loc = l
	}
	if opts.Jitter < 0 {
		return nil, fmt.Errorf("schedule: jitter must be >= 0, got %s", opts.Jitter)
	}
	randN := opts.RandN
	if randN == nil {
		randN = defaultRandN
	}
	return &Schedule{expr: opts.Expr, spec: spec, loc: loc, jitter: opts.Jitter, randN: randN}, nil
}

// Expr reports the cron expression (for logging).
func (s *Schedule) Expr() string { return s.expr }

// Next returns the next fire time strictly after `after`, computed in the
// anchor timezone, plus any jitter in [0, jitter). A slot that elapsed while
// the daemon was down is never returned — the result is always in the future
// relative to `after` (R-SCHED-05/06, INV-SCHED-03).
func (s *Schedule) Next(after time.Time) time.Time {
	base := s.spec.Next(after.In(s.loc))
	if s.jitter > 0 {
		base = base.Add(time.Duration(s.randN(int64(s.jitter))))
	}
	return base
}

// Location reports the anchor timezone (for logging).
func (s *Schedule) Location() *time.Location { return s.loc }

// defaultRandN is a last-resort uniform source. The collector injects its own
// locked source in production; this keeps New usable standalone.
func defaultRandN(n int64) int64 {
	if n <= 0 {
		return 0
	}
	return time.Now().UnixNano() % n
}
