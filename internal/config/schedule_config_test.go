package config

import (
	"strings"
	"testing"
	"time"
)

// #491 — ValidateStrict fails fast on an invalid cron schedule / timezone /
// jitter so the daemon never starts in a half-configured scheduling state
// (B7, INV-SCHED-02), and a valid schedule relaxes the poll_interval>0
// requirement (B3).

func scheduleConfig(mutate func(*Config)) Config {
	cfg := baseValidConfig(baseValidTarget())
	mutate(&cfg)
	return cfg
}

// AC-SCHED-05 — an unparseable cron expression is a hard error naming the field.
func TestScheduleConfig_RejectsBadCron(t *testing.T) {
	cfg := scheduleConfig(func(c *Config) { c.Signals.Schedule = "not a cron" })
	_, err := ValidateStrict(cfg)
	if err == nil || !strings.Contains(err.Error(), "cron expression") {
		t.Fatalf("want cron-expression error, got %v", err)
	}
}

// AC-SCHED-06 — an unknown timezone is a hard error naming the field.
func TestScheduleConfig_RejectsBadTimezone(t *testing.T) {
	cfg := scheduleConfig(func(c *Config) {
		c.Signals.Schedule = "0 2 * * *"
		c.Signals.ScheduleTimezone = "Mars/Olympus"
	})
	_, err := ValidateStrict(cfg)
	if err == nil || !strings.Contains(err.Error(), "timezone") {
		t.Fatalf("want timezone error, got %v", err)
	}
}

// AC-SCHED-07 — a negative jitter is a hard error.
func TestScheduleConfig_RejectsNegativeJitter(t *testing.T) {
	cfg := scheduleConfig(func(c *Config) {
		c.Signals.Schedule = "0 2 * * *"
		c.Signals.ScheduleJitter = -5 * time.Minute
	})
	_, err := ValidateStrict(cfg)
	if err == nil || !strings.Contains(err.Error(), "jitter") {
		t.Fatalf("want jitter error, got %v", err)
	}
}

// AC-SCHED-01/B3 — a valid schedule passes, and it relaxes the poll_interval>0
// requirement (schedule governs timing; poll_interval is ignored).
func TestScheduleConfig_ValidScheduleAllowsZeroPollInterval(t *testing.T) {
	cfg := scheduleConfig(func(c *Config) {
		c.Signals.Schedule = "0 2 * * *"
		c.Signals.ScheduleTimezone = "Europe/Amsterdam"
		c.Signals.ScheduleJitter = 10 * time.Minute
		c.Signals.PollInterval = 0 // ignored in scheduled mode
	})
	if _, err := ValidateStrict(cfg); err != nil {
		t.Fatalf("valid schedule with zero poll_interval should pass, got %v", err)
	}
}

// AC-SCHED-08 — no schedule keeps interval mode: poll_interval>0 is still
// required, and a valid interval config passes.
func TestScheduleConfig_EmptyIsIntervalMode(t *testing.T) {
	// Valid interval config (no schedule) passes.
	if _, err := ValidateStrict(baseValidConfig(baseValidTarget())); err != nil {
		t.Fatalf("interval-mode config should pass, got %v", err)
	}
	// With no schedule, a non-positive poll_interval is still a hard error.
	cfg := scheduleConfig(func(c *Config) { c.Signals.PollInterval = 0 })
	if _, err := ValidateStrict(cfg); err == nil || !strings.Contains(err.Error(), "poll_interval") {
		t.Fatalf("interval mode must still require poll_interval>0, got %v", err)
	}
}

// The jitter duration string is folded into ScheduleJitter by parseDurations,
// accepting the #490 flexible-duration grammar.
func TestScheduleConfig_JitterStringParsed(t *testing.T) {
	cfg := baseValidConfig(baseValidTarget())
	cfg.Signals.ScheduleJitterS = "90s"
	if err := parseDurations(&cfg); err != nil {
		t.Fatalf("parseDurations: %v", err)
	}
	if cfg.Signals.ScheduleJitter != 90*time.Second {
		t.Errorf("ScheduleJitter = %s, want 90s", cfg.Signals.ScheduleJitter)
	}
}
