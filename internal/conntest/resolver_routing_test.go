package conntest

import (
	"context"
	"errors"
	"testing"

	"github.com/elevarq/signals/internal/collector"
	"github.com/elevarq/signals/internal/config"
)

// #722 / F16: config-based connect tests route cloud-identity / mtls targets
// through the SAME credential resolver collection uses (so they are diagnosed
// with their real credential), while password-by-reference stays on the static
// DSN path (actionable, non-secret resolution errors).

// recordingResolver records whether Resolve was invoked and stops the dial.
type recordingResolver struct{ called bool }

func (r *recordingResolver) Resolve(ctx context.Context, tgt config.TargetConfig) (collector.Credential, error) {
	r.called = true
	return collector.Credential{}, errors.New("stop: routing probe")
}

func TestConfigTarget_CloudMethodRoutesThroughResolver(t *testing.T) {
	cloud := config.TargetConfig{
		Name: "c", Host: "h.invalid", Port: 5432, DBName: "db", User: "u",
		SSLMode: "verify-full", AuthMethod: config.AuthMethodAWSRDSIAM, Region: "us-east-1",
	}
	r := &recordingResolver{}
	_ = testConfigTarget(context.Background(), cloud, r, Options{})
	if !r.called {
		t.Error("aws_rds_iam target was NOT routed through the credential resolver — a cloud target would be diagnosed with a password-only DSN (#722)")
	}
}

func TestConfigTarget_PasswordUsesStaticDSNPath(t *testing.T) {
	pw := config.TargetConfig{
		Name: "p", Host: "h.invalid", Port: 5432, DBName: "db", User: "u",
		SSLMode: "disable", AuthMethod: config.AuthMethodPassword,
	}
	r := &recordingResolver{}
	_ = testConfigTarget(context.Background(), pw, r, Options{})
	if r.called {
		t.Error("password target went through the resolver — it should use the static DSN path for actionable resolution errors (#722)")
	}
}
