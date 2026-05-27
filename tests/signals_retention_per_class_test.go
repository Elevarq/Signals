package tests

import (
	"strings"
	"testing"

	"github.com/elevarq/arq-signals/internal/config"
)

// ---------------------------------------------------------------------------
// R099 / per-class retention — config validation and accessor helpers.
//
// Spec: features/arq-signals/specification.md § Per-class retention
// ---------------------------------------------------------------------------

// FC-21: flat `retention_days` and structured `retention:` block
// are mutually exclusive.
func TestRetention_ValidateStrictRejectsBothFlatAndStructured(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Signals.RetentionDays = 30
	cfg.Signals.Retention = config.RetentionConfig{ShortDays: 7, MediumDays: 30, LongDays: 365}
	_, err := config.ValidateStrict(cfg)
	if err == nil {
		t.Fatal("ValidateStrict must reject retention_days + retention.* together")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error must explain the conflict; got %v", err)
	}
}

func TestRetention_ValidateStrictRejectsNegativeClassDays(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Signals.RetentionDays = 0
	cfg.Signals.Retention = config.RetentionConfig{ShortDays: -1, MediumDays: 30}
	_, err := config.ValidateStrict(cfg)
	if err == nil {
		t.Fatal("ValidateStrict must reject negative class day count")
	}
}

// Either form alone is fine.
func TestRetention_FlatAloneIsValid(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Signals.RetentionDays = 30
	cfg.Signals.Retention = config.RetentionConfig{} // empty
	if _, err := config.ValidateStrict(cfg); err != nil {
		t.Errorf("flat retention_days alone must validate; got %v", err)
	}
}

func TestRetention_StructuredAloneIsValid(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Signals.RetentionDays = 0
	cfg.Signals.Retention = config.RetentionConfig{ShortDays: 7, MediumDays: 30, LongDays: 365}
	if _, err := config.ValidateStrict(cfg); err != nil {
		t.Errorf("structured retention alone must validate; got %v", err)
	}
}

// DaysFor returns per-class day count, falling back to default when
// a class isn't explicitly set.
func TestRetention_DaysForReturnsPerClassValue(t *testing.T) {
	r := config.RetentionConfig{ShortDays: 7, MediumDays: 30, LongDays: 365}
	cases := []struct {
		class string
		want  int
	}{
		{"short", 7}, {"medium", 30}, {"long", 365},
	}
	for _, tc := range cases {
		got := r.DaysFor(tc.class, 99)
		if got != tc.want {
			t.Errorf("DaysFor(%q): got %d, want %d", tc.class, got, tc.want)
		}
	}
}

func TestRetention_DaysForFallsBackToDefault(t *testing.T) {
	// Only ShortDays set; medium/long should fall back to default.
	r := config.RetentionConfig{ShortDays: 7}
	if got := r.DaysFor("short", 999); got != 7 {
		t.Errorf("explicit short: got %d, want 7", got)
	}
	if got := r.DaysFor("medium", 999); got != 999 {
		t.Errorf("unset medium must fall back to default: got %d", got)
	}
	if got := r.DaysFor("long", 999); got != 999 {
		t.Errorf("unset long must fall back to default: got %d", got)
	}
}

func TestRetention_DaysForUnknownClassFallsBack(t *testing.T) {
	r := config.RetentionConfig{ShortDays: 7, MediumDays: 30, LongDays: 365}
	if got := r.DaysFor("nonsense", 42); got != 42 {
		t.Errorf("unknown class must fall back; got %d", got)
	}
}

// MaxDays returns the largest configured retention so the snapshot
// pruner doesn't drop snapshots whose long-class data is still in
// retention.
func TestRetention_MaxDaysReturnsLargest(t *testing.T) {
	r := config.RetentionConfig{ShortDays: 7, MediumDays: 30, LongDays: 365}
	if got := r.MaxDays(99); got != 365 {
		t.Errorf("MaxDays: got %d, want 365", got)
	}
}

func TestRetention_MaxDaysFallsBackWhenAllZero(t *testing.T) {
	r := config.RetentionConfig{}
	if got := r.MaxDays(30); got != 30 {
		t.Errorf("empty config: MaxDays must fall back to default; got %d", got)
	}
}

// IsSet distinguishes the empty value from any explicit one. Used
// by the collector to decide whether the structured config is
// authoritative.
func TestRetention_IsSet(t *testing.T) {
	if (config.RetentionConfig{}).IsSet() {
		t.Error("empty RetentionConfig must NOT report IsSet")
	}
	if !(config.RetentionConfig{ShortDays: 1}).IsSet() {
		t.Error("ShortDays set must report IsSet")
	}
	if !(config.RetentionConfig{LongDays: 365}).IsSet() {
		t.Error("LongDays set must report IsSet")
	}
}
