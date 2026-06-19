package tests

import (
	"strings"
	"testing"
	"time"

	"github.com/elevarq/signals/internal/config"
)

// ---------------------------------------------------------------------------
// R097 / signals.circuit ValidateStrict gates (issue #90).
//
// Spec: features/signals/specification.md § Circuit breaker
// ---------------------------------------------------------------------------

func TestCircuitConfig_RejectsNegativeFailThreshold(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Signals.Circuit.FailThreshold = -1
	_, err := config.ValidateStrict(cfg)
	if err == nil {
		t.Fatal("ValidateStrict must reject negative fail_threshold")
	}
	if !strings.Contains(err.Error(), "fail_threshold") {
		t.Errorf("error must name the offending field; got %v", err)
	}
}

func TestCircuitConfig_RejectsNegativeOpenCooldown(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Signals.Circuit.OpenCooldown = -1 * time.Second
	_, err := config.ValidateStrict(cfg)
	if err == nil {
		t.Fatal("ValidateStrict must reject negative open_cooldown")
	}
	if !strings.Contains(err.Error(), "open_cooldown") {
		t.Errorf("error must name the offending field; got %v", err)
	}
}

func TestCircuitConfig_ZeroIsValid(t *testing.T) {
	// Zero means "use package default" — must not be rejected.
	cfg := config.DefaultConfig()
	cfg.Signals.Circuit.FailThreshold = 0
	cfg.Signals.Circuit.OpenCooldown = 0
	if _, err := config.ValidateStrict(cfg); err != nil {
		t.Errorf("zero (= use default) must validate; got %v", err)
	}
}

func TestCircuitConfig_PositiveIsValid(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Signals.Circuit.FailThreshold = 5
	cfg.Signals.Circuit.OpenCooldown = 10 * time.Minute
	if _, err := config.ValidateStrict(cfg); err != nil {
		t.Errorf("positive values must validate; got %v", err)
	}
}
