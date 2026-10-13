package schedule

import (
	"testing"
	"time"
)

func mustNew(t *testing.T, o Options) *Schedule {
	t.Helper()
	s, err := New(o)
	if err != nil {
		t.Fatalf("New(%+v): %v", o, err)
	}
	return s
}

// AC-SCHED-01 — daily cron, UTC default.
func TestSchedule_NextDailyUTC(t *testing.T) {
	s := mustNew(t, Options{Expr: "0 2 * * *"})
	now := time.Date(2026, 10, 13, 8, 0, 0, 0, time.UTC)
	got := s.Next(now)
	want := time.Date(2026, 10, 14, 2, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("Next = %s, want %s", got, want)
	}
}

// AC-SCHED-02 — explicit timezone, DST-aware. In Amsterdam summer (CEST,
// UTC+2), 02:00 local is 00:00 UTC.
func TestSchedule_NextWithTimezone(t *testing.T) {
	s := mustNew(t, Options{Expr: "0 2 * * *", Timezone: "Europe/Amsterdam"})
	// A summer afternoon, UTC.
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	got := s.Next(now).UTC()
	want := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC) // 02:00 CEST == 00:00Z
	if !got.Equal(want) {
		t.Errorf("Next = %s, want %s (02:00 Amsterdam)", got, want)
	}
}

// AC-SCHED-03 — next is STRICTLY after now; the current match is not re-run.
func TestSchedule_NextIsStrictlyAfterNow(t *testing.T) {
	s := mustNew(t, Options{Expr: "0 2 * * *"})
	now := time.Date(2026, 10, 13, 2, 0, 0, 0, time.UTC) // exactly on a match
	got := s.Next(now)
	if !got.After(now) {
		t.Errorf("Next = %s, must be strictly after %s", got, now)
	}
	want := time.Date(2026, 10, 14, 2, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("Next = %s, want %s (no catch-up of the current slot)", got, want)
	}
}

// AC-SCHED-04 — jitter stays within [0, J).
func TestSchedule_JitterWithinBound(t *testing.T) {
	jitter := 10 * time.Minute
	// Deterministic rand: always return n-1 (the max), then a mid value.
	for _, r := range []int64{0, int64(jitter) - 1, int64(jitter) / 2} {
		s := mustNew(t, Options{
			Expr:   "0 2 * * *",
			Jitter: jitter,
			RandN:  func(int64) int64 { return r },
		})
		now := time.Date(2026, 10, 13, 8, 0, 0, 0, time.UTC)
		base := time.Date(2026, 10, 14, 2, 0, 0, 0, time.UTC)
		got := s.Next(now)
		delta := got.Sub(base)
		if delta < 0 || delta >= jitter {
			t.Errorf("jitter delta %s out of [0, %s)", delta, jitter)
		}
		if delta != time.Duration(r) {
			t.Errorf("delta = %s, want %s", delta, time.Duration(r))
		}
	}
}

// AC-SCHED-04 — zero jitter fires exactly at the computed time.
func TestSchedule_ZeroJitterExact(t *testing.T) {
	calls := 0
	s := mustNew(t, Options{
		Expr:   "0 2 * * *",
		Jitter: 0,
		RandN:  func(int64) int64 { calls++; return 5 },
	})
	now := time.Date(2026, 10, 13, 8, 0, 0, 0, time.UTC)
	got := s.Next(now)
	want := time.Date(2026, 10, 14, 2, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("Next = %s, want exactly %s", got, want)
	}
	if calls != 0 {
		t.Errorf("rand must not be consulted when jitter is 0 (called %d times)", calls)
	}
}

// Weekly and monthly expressions resolve to the right next slot.
func TestSchedule_WeeklyAndMonthly(t *testing.T) {
	now := time.Date(2026, 10, 13, 8, 0, 0, 0, time.UTC) // a Tuesday
	weekly := mustNew(t, Options{Expr: "0 2 * * 1"})     // Mondays 02:00
	if got := weekly.Next(now).Weekday(); got != time.Monday {
		t.Errorf("weekly next weekday = %s, want Monday", got)
	}
	monthly := mustNew(t, Options{Expr: "0 2 1 * *"}) // 1st of month 02:00
	if got := monthly.Next(now); got.Day() != 1 {
		t.Errorf("monthly next day = %d, want 1", got.Day())
	}
}

// AC-SCHED-05/06/07 at the package layer — New rejects bad inputs.
func TestSchedule_NewRejectsInvalid(t *testing.T) {
	cases := []Options{
		{Expr: ""},           // empty
		{Expr: "not a cron"}, // unparseable
		{Expr: "0 2 * * *", Timezone: "Mars/Olympus"}, // unknown tz
		{Expr: "0 2 * * *", Jitter: -5 * time.Minute}, // negative jitter
		{Expr: "0 2 * * * *"},                         // 6 fields (non-standard)
	}
	for _, o := range cases {
		if _, err := New(o); err == nil {
			t.Errorf("New(%+v) = nil error, want error", o)
		}
	}
}
