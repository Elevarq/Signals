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

// --- test doubles -----------------------------------------------------

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// fakeMinter records call counts and mints deterministic tokens whose
// expiry is now()+ttl, so cache/refresh behavior is observable.
type fakeMinter struct {
	mu    sync.Mutex
	calls int
	ttl   time.Duration
	now   func() time.Time
	err   error
	last  struct{ endpoint, region, dbUser string }
}

func (m *fakeMinter) Mint(ctx context.Context, endpoint, region, dbUser string) (string, time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	m.last.endpoint, m.last.region, m.last.dbUser = endpoint, region, dbUser
	if m.err != nil {
		return "", time.Time{}, m.err
	}
	return tokenAt(m.calls), m.now().Add(m.ttl), nil
}

func (m *fakeMinter) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func tokenAt(n int) string {
	return "rds-token-" + string(rune('A'+n-1))
}

func regionFixed(s string) func(context.Context, config.TargetConfig) (string, error) {
	return func(context.Context, config.TargetConfig) (string, error) { return s, nil }
}

func awsTestTarget() config.TargetConfig {
	return config.TargetConfig{
		Name:       "t1",
		Host:       "db.example.com",
		Port:       5432,
		DBName:     "appdb",
		User:       "monitor",
		SSLMode:    "verify-full",
		AuthMethod: config.AuthMethodAWSRDSIAM,
		Region:     "us-east-1",
		Enabled:    true,
	}
}

func newTestResolver(clock *fakeClock, m *fakeMinter, region func(context.Context, config.TargetConfig) (string, error), logger *slog.Logger) *credentialResolver {
	return &credentialResolver{
		cache:  newTokenCache(),
		minter: m,
		region: region,
		now:    clock.now,
		logger: logger,
	}
}

// --- tests ------------------------------------------------------------

// AC-AWS-001 — a verify-full aws_rds_iam target resolves a password-kind
// credential whose value is the minted token and whose ExpiresAt is the
// token's ~15-minute expiry.
func TestResolveAWSMintsTokenAsPassword(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0).UTC()}
	m := &fakeMinter{ttl: 15 * time.Minute, now: clock.now}
	r := newTestResolver(clock, m, regionFixed("us-east-1"), slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))

	cred, err := r.Resolve(context.Background(), awsTestTarget())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cred.Kind != CredKindPassword {
		t.Errorf("Kind = %v, want CredKindPassword", cred.Kind)
	}
	if cred.Password != tokenAt(1) {
		t.Errorf("Password = %q, want %q", cred.Password, tokenAt(1))
	}
	if want := clock.now().Add(15 * time.Minute); !cred.ExpiresAt.Equal(want) {
		t.Errorf("ExpiresAt = %v, want %v", cred.ExpiresAt, want)
	}
	if m.last.endpoint != "db.example.com:5432" || m.last.region != "us-east-1" || m.last.dbUser != "monitor" {
		t.Errorf("minter inputs = %+v, want endpoint=db.example.com:5432 region=us-east-1 dbUser=monitor", m.last)
	}
}

// AC-AWS-002 — token cached and reused within the refresh skew; re-minted
// once a cached token crosses the skew (3 min before the 15 min expiry,
// i.e. ~12 min old); never shared across targets.
func TestResolveAWSCacheRefreshAndIsolation(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0).UTC()}
	m := &fakeMinter{ttl: 15 * time.Minute, now: clock.now}
	r := newTestResolver(clock, m, regionFixed("us-east-1"), slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	ctx := context.Background()
	tgt := awsTestTarget()

	first, _ := r.Resolve(ctx, tgt)
	if m.callCount() != 1 {
		t.Fatalf("first resolve: calls = %d, want 1", m.callCount())
	}

	// Within skew (5 min < 12 min) → reuse, no new mint.
	clock.advance(5 * time.Minute)
	reuse, _ := r.Resolve(ctx, tgt)
	if m.callCount() != 1 {
		t.Errorf("within skew: calls = %d, want 1 (reuse)", m.callCount())
	}
	if reuse.Password != first.Password {
		t.Errorf("within skew: token changed %q -> %q", first.Password, reuse.Password)
	}

	// Cross the skew (now 13 min old > 12 min) → re-mint.
	clock.advance(8 * time.Minute)
	refreshed, _ := r.Resolve(ctx, tgt)
	if m.callCount() != 2 {
		t.Errorf("after skew: calls = %d, want 2 (re-mint)", m.callCount())
	}
	if refreshed.Password == first.Password {
		t.Errorf("after skew: token should change, still %q", refreshed.Password)
	}

	// Different target → distinct cache key → independent mint.
	other := awsTestTarget()
	other.Name = "t2"
	other.Host = "db2.example.com"
	if _, err := r.Resolve(ctx, other); err != nil {
		t.Fatalf("resolve other: %v", err)
	}
	if m.callCount() != 3 {
		t.Errorf("second target: calls = %d, want 3 (not shared)", m.callCount())
	}
}

// AC-AWS-006 — region unresolved fails the target (FC-AWS-005) and never
// reaches the minter.
func TestResolveAWSRegionUnresolved(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0).UTC()}
	m := &fakeMinter{ttl: 15 * time.Minute, now: clock.now}
	regionErr := func(context.Context, config.TargetConfig) (string, error) {
		return "", errors.New("no region: tried config, AWS_REGION, AWS_DEFAULT_REGION, IMDS")
	}
	r := newTestResolver(clock, m, regionErr, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))

	_, err := r.Resolve(context.Background(), awsTestTarget())
	if err == nil {
		t.Fatalf("expected region error, got nil")
	}
	if !strings.Contains(err.Error(), "region") {
		t.Errorf("error should mention region; got: %v", err)
	}
	if m.callCount() != 0 {
		t.Errorf("minter should not be called when region fails; calls = %d", m.callCount())
	}
}

// AC-AWS-005 — a mint failure surfaces an actionable error and no token.
func TestResolveAWSMintFailure(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0).UTC()}
	m := &fakeMinter{ttl: 15 * time.Minute, now: clock.now, err: errors.New("AccessDenied: not authorized to perform rds-db:connect")}
	r := newTestResolver(clock, m, regionFixed("us-east-1"), slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))

	cred, err := r.Resolve(context.Background(), awsTestTarget())
	if err == nil {
		t.Fatalf("expected mint error, got nil")
	}
	if !strings.Contains(err.Error(), "aws_rds_iam") {
		t.Errorf("error should be attributable to aws_rds_iam; got: %v", err)
	}
	if cred.Password != "" {
		t.Errorf("no token should be returned on failure, got %q", cred.Password)
	}
}

// AC-AWS-007 / INV002 / INV007 — a successful resolution logs metadata
// (auth_method, region, db_user, expires_at) but never the token value.
func TestResolveAWSLogsMetadataNotToken(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0).UTC()}
	m := &fakeMinter{ttl: 15 * time.Minute, now: clock.now}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	r := newTestResolver(clock, m, regionFixed("us-east-1"), logger)

	cred, err := r.Resolve(context.Background(), awsTestTarget())
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
	for _, want := range []string{"aws_rds_iam", "us-east-1", "monitor"} {
		if !strings.Contains(out, want) {
			t.Errorf("log should contain metadata %q; got: %s", want, out)
		}
	}
}

// Backward compatibility — an empty auth_method resolves via the existing
// password source and carries no expiry.
func TestResolvePasswordPathUnchanged(t *testing.T) {
	t.Setenv("SIGNALS_CREDPROV_TEST_PW", "s3cr3t")
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0).UTC()}
	m := &fakeMinter{ttl: 15 * time.Minute, now: clock.now}
	r := newTestResolver(clock, m, regionFixed("us-east-1"), slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))

	tgt := config.TargetConfig{
		Name:        "t1",
		Host:        "db.example.com",
		Port:        5432,
		DBName:      "appdb",
		User:        "monitor",
		PasswordEnv: "SIGNALS_CREDPROV_TEST_PW",
		Enabled:     true,
	}
	cred, err := r.Resolve(context.Background(), tgt)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cred.Kind != CredKindPassword || cred.Password != "s3cr3t" {
		t.Errorf("password path: got kind=%v pw=%q, want password/s3cr3t", cred.Kind, cred.Password)
	}
	if !cred.ExpiresAt.IsZero() {
		t.Errorf("password credential should have no expiry, got %v", cred.ExpiresAt)
	}
	if m.callCount() != 0 {
		t.Errorf("password path must not call the AWS minter; calls = %d", m.callCount())
	}
}

// AC-AWS-009 — the operator-guidance text names the exact grant and the
// IAM action, with no secret material.
func TestAWSGrantGuidance(t *testing.T) {
	g := AWSGrantGuidance(awsTestTarget())
	if !strings.Contains(g, `GRANT rds_iam TO "monitor"`) {
		t.Errorf("guidance should include the grant for the role; got: %s", g)
	}
	if !strings.Contains(g, "rds-db:connect") {
		t.Errorf("guidance should include the IAM action; got: %s", g)
	}
}
