package collector

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elevarq/arq-signals/internal/config"
)

// --- GCP test doubles -------------------------------------------------

// fakeGCPMinter records call counts and the inputs it was handed, and
// mints deterministic tokens whose expiry is now()+ttl so cache/refresh
// behavior is observable. It makes no real GCP call (NFR003).
type fakeGCPMinter struct {
	mu    sync.Mutex
	calls int
	ttl   time.Duration
	now   func() time.Time
	err   error
	last  struct{ scope, impersonate string }
}

func (m *fakeGCPMinter) Mint(ctx context.Context, scope, impersonate string) (string, time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	m.last.scope, m.last.impersonate = scope, impersonate
	if m.err != nil {
		return "", time.Time{}, m.err
	}
	return gcpTokenAt(m.calls), m.now().Add(m.ttl), nil
}

func (m *fakeGCPMinter) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func (m *fakeGCPMinter) lastImpersonate() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.last.impersonate
}

func gcpTokenAt(n int) string {
	return "gcp-token-" + string(rune('A'+n-1))
}

func gcpTestTarget() config.TargetConfig {
	return config.TargetConfig{
		Name:       "gcp1",
		Host:       "10.10.0.5",
		Port:       5432,
		DBName:     "appdb",
		User:       "monitor@my-proj.iam",
		SSLMode:    "verify-full",
		AuthMethod: config.AuthMethodGCPCloudSQLIAM,
		Enabled:    true,
	}
}

func newGCPTestResolver(clock *fakeClock, m *fakeGCPMinter, logger *slog.Logger) *credentialResolver {
	return &credentialResolver{
		cache:     newTokenCache(),
		gcpMinter: m,
		now:       clock.now,
		logger:    logger,
	}
}

// --- tests ------------------------------------------------------------

// AC-GCP-001 — a verify-full gcp_cloudsql_iam target resolves a
// password-kind credential whose value is the acquired token and whose
// ExpiresAt is the token's expiry; the minter is called for the fixed
// scope.
func TestResolveGCPMintsTokenAsPassword(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0).UTC()}
	m := &fakeGCPMinter{ttl: 60 * time.Minute, now: clock.now}
	r := newGCPTestResolver(clock, m, discardLogger())

	cred, err := r.Resolve(context.Background(), gcpTestTarget())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cred.Kind != CredKindPassword {
		t.Errorf("Kind = %v, want CredKindPassword", cred.Kind)
	}
	if cred.Password != gcpTokenAt(1) {
		t.Errorf("Password = %q, want %q", cred.Password, gcpTokenAt(1))
	}
	if want := clock.now().Add(60 * time.Minute); !cred.ExpiresAt.Equal(want) {
		t.Errorf("ExpiresAt = %v, want %v", cred.ExpiresAt, want)
	}
	if m.last.scope != gcpScope {
		t.Errorf("minter scope = %q, want fixed %q", m.last.scope, gcpScope)
	}
}

// AC-GCP-002 — token cached and reused within the refresh skew; re-minted
// once a cached token crosses the skew (5 min before the 60 min expiry,
// i.e. at 55 min); never shared across targets.
func TestResolveGCPCacheRefreshAndIsolation(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0).UTC()}
	m := &fakeGCPMinter{ttl: 60 * time.Minute, now: clock.now}
	r := newGCPTestResolver(clock, m, discardLogger())
	ctx := context.Background()
	tgt := gcpTestTarget()

	first, _ := r.Resolve(ctx, tgt)
	if m.callCount() != 1 {
		t.Fatalf("first resolve: calls = %d, want 1", m.callCount())
	}

	// Within skew (50 min < 55 min) → reuse, no new mint.
	clock.advance(50 * time.Minute)
	reuse, _ := r.Resolve(ctx, tgt)
	if m.callCount() != 1 {
		t.Errorf("within skew: calls = %d, want 1 (reuse)", m.callCount())
	}
	if reuse.Password != first.Password {
		t.Errorf("within skew: token changed %q -> %q", first.Password, reuse.Password)
	}

	// Cross the skew (now 56 min old > 55 min) → re-mint.
	clock.advance(6 * time.Minute)
	refreshed, _ := r.Resolve(ctx, tgt)
	if m.callCount() != 2 {
		t.Errorf("after skew: calls = %d, want 2 (re-mint)", m.callCount())
	}
	if refreshed.Password == first.Password {
		t.Errorf("after skew: token should change, still %q", refreshed.Password)
	}

	// Different target → distinct cache key → independent mint.
	other := gcpTestTarget()
	other.Name = "gcp2"
	other.Host = "10.10.0.9"
	if _, err := r.Resolve(ctx, other); err != nil {
		t.Fatalf("resolve other: %v", err)
	}
	if m.callCount() != 3 {
		t.Errorf("second target: calls = %d, want 3 (not shared)", m.callCount())
	}
}

// AC-GCP-005 — a mint failure surfaces an actionable error attributable to
// gcp_cloudsql_iam and returns no token.
func TestResolveGCPMintFailure(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0).UTC()}
	m := &fakeGCPMinter{ttl: 60 * time.Minute, now: clock.now, err: errors.New("oauth2: token endpoint returned 403")}
	r := newGCPTestResolver(clock, m, discardLogger())

	cred, err := r.Resolve(context.Background(), gcpTestTarget())
	if err == nil {
		t.Fatalf("expected mint error, got nil")
	}
	if !strings.Contains(err.Error(), "gcp_cloudsql_iam") {
		t.Errorf("error should be attributable to gcp_cloudsql_iam; got: %v", err)
	}
	if cred.Password != "" {
		t.Errorf("no token should be returned on failure, got %q", cred.Password)
	}
}

// AC-GCP-006 — an identity-resolution failure fails the target with an
// actionable error naming the impersonation remediation; it leaves no
// cached entry, and a subsequent healthy resolution still succeeds (other
// targets keep collecting; FC-GCP-005).
func TestResolveGCPIdentityErrorIsActionableAndIsolated(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0).UTC()}
	m := &fakeGCPMinter{ttl: 60 * time.Minute, now: clock.now,
		err: errors.New("google: could not find default credentials")}
	r := newGCPTestResolver(clock, m, discardLogger())
	ctx := context.Background()

	_, err := r.Resolve(ctx, gcpTestTarget())
	if err == nil {
		t.Fatalf("expected identity error, got nil")
	}
	if !strings.Contains(err.Error(), "gcp_impersonate_service_account") {
		t.Errorf("error should name the impersonation remediation; got: %v", err)
	}

	// The transient failure must not block a later healthy attempt.
	m.mu.Lock()
	m.err = nil
	m.mu.Unlock()
	healthy := gcpTestTarget()
	healthy.Name = "gcp2"
	healthy.Host = "10.10.0.9"
	if _, err := r.Resolve(ctx, healthy); err != nil {
		t.Errorf("a healthy target must still resolve after another target failed: %v", err)
	}
}

// AC-GCP-007 / INV002 / INV007 — a successful resolution logs metadata
// (auth_method, scope, db_user) but never the token value.
func TestResolveGCPLogsMetadataNotToken(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0).UTC()}
	m := &fakeGCPMinter{ttl: 60 * time.Minute, now: clock.now}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	r := newGCPTestResolver(clock, m, logger)

	cred, err := r.Resolve(context.Background(), gcpTestTarget())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	out := buf.String()
	if cred.Password == "" {
		t.Fatal("expected a non-empty token")
	}
	if strings.Contains(out, cred.Password) {
		t.Errorf("log leaked the token %q in: %s", cred.Password, out)
	}
	for _, want := range []string{"gcp_cloudsql_iam", gcpScope, "monitor@my-proj.iam"} {
		if !strings.Contains(out, want) {
			t.Errorf("log should contain metadata %q; got: %s", want, out)
		}
	}
}

// Confirmed design — service-account impersonation: an explicit
// gcp_impersonate_service_account is handed to the minter; absent it, the
// minter is called with an empty impersonation target (ambient ADC).
func TestResolveGCPImpersonationResolution(t *testing.T) {
	t.Run("config set", func(t *testing.T) {
		clock := &fakeClock{t: time.Unix(1_700_000_000, 0).UTC()}
		m := &fakeGCPMinter{ttl: 60 * time.Minute, now: clock.now}
		r := newGCPTestResolver(clock, m, discardLogger())
		tgt := gcpTestTarget()
		tgt.GCPImpersonateServiceAccount = "collector@my-proj.iam.gserviceaccount.com"
		if _, err := r.Resolve(context.Background(), tgt); err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if m.lastImpersonate() != "collector@my-proj.iam.gserviceaccount.com" {
			t.Errorf("impersonate = %q, want the configured SA", m.lastImpersonate())
		}
	})

	t.Run("none set", func(t *testing.T) {
		clock := &fakeClock{t: time.Unix(1_700_000_000, 0).UTC()}
		m := &fakeGCPMinter{ttl: 60 * time.Minute, now: clock.now}
		r := newGCPTestResolver(clock, m, discardLogger())
		if _, err := r.Resolve(context.Background(), gcpTestTarget()); err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if m.lastImpersonate() != "" {
			t.Errorf("impersonate = %q, want empty (ambient ADC)", m.lastImpersonate())
		}
	})
}

// AC-GCP-009 — the operator-guidance text names the gcloud IAM-user create
// command and the target role, with no secret material.
func TestGCPCloudSQLGuidance(t *testing.T) {
	g := GCPCloudSQLGuidance(gcpTestTarget())
	if !strings.Contains(g, "gcloud sql users create") {
		t.Errorf("guidance should include the gcloud users-create command; got: %s", g)
	}
	if !strings.Contains(g, "monitor@my-proj.iam") {
		t.Errorf("guidance should name the role; got: %s", g)
	}
}
