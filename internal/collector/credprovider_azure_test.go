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

	"github.com/elevarq/signals/internal/config"
)

// --- Azure test doubles ----------------------------------------------

// fakeAzureMinter records call counts and the inputs it was handed, and
// mints deterministic tokens whose expiry is now()+ttl so cache/refresh
// behavior is observable. It makes no real Azure call (NFR003).
type fakeAzureMinter struct {
	mu    sync.Mutex
	calls int
	ttl   time.Duration
	now   func() time.Time
	err   error
	last  struct{ scope, clientID string }
}

func (m *fakeAzureMinter) Mint(ctx context.Context, scope, clientID string) (string, time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	m.last.scope, m.last.clientID = scope, clientID
	if m.err != nil {
		return "", time.Time{}, m.err
	}
	return entraTokenAt(m.calls), m.now().Add(m.ttl), nil
}

func (m *fakeAzureMinter) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func (m *fakeAzureMinter) lastClientID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.last.clientID
}

func entraTokenAt(n int) string {
	return "entra-token-" + string(rune('A'+n-1))
}

func azureTestTarget() config.TargetConfig {
	return config.TargetConfig{
		Name:       "az1",
		Host:       "mydb.postgres.database.azure.com",
		Port:       5432,
		DBName:     "appdb",
		User:       "monitor",
		SSLMode:    "verify-full",
		AuthMethod: config.AuthMethodAzureEntra,
		Enabled:    true,
	}
}

func newAzureTestResolver(clock *fakeClock, m *fakeAzureMinter, logger *slog.Logger) *credentialResolver {
	return &credentialResolver{
		cache:       newTokenCache(),
		azureMinter: m,
		now:         clock.now,
		logger:      logger,
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
}

// --- tests ------------------------------------------------------------

// AC-AZURE-001 — a verify-full azure_entra target resolves a
// password-kind credential whose value is the acquired token and whose
// ExpiresAt is the token's expiry; the minter is called for the fixed
// scope.
func TestResolveAzureMintsTokenAsPassword(t *testing.T) {
	t.Setenv("AZURE_CLIENT_ID", "")
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0).UTC()}
	m := &fakeAzureMinter{ttl: 75 * time.Minute, now: clock.now}
	r := newAzureTestResolver(clock, m, discardLogger())

	cred, err := r.Resolve(context.Background(), azureTestTarget())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cred.Kind != CredKindPassword {
		t.Errorf("Kind = %v, want CredKindPassword", cred.Kind)
	}
	if cred.Password != entraTokenAt(1) {
		t.Errorf("Password = %q, want %q", cred.Password, entraTokenAt(1))
	}
	if want := clock.now().Add(75 * time.Minute); !cred.ExpiresAt.Equal(want) {
		t.Errorf("ExpiresAt = %v, want %v", cred.ExpiresAt, want)
	}
	if m.last.scope != entraScope {
		t.Errorf("minter scope = %q, want fixed %q", m.last.scope, entraScope)
	}
}

// AC-AZURE-002 — token cached and reused within the refresh skew; re-minted
// once a cached token crosses the skew (5 min before the 75 min expiry,
// i.e. at 70 min); never shared across targets.
func TestResolveAzureCacheRefreshAndIsolation(t *testing.T) {
	t.Setenv("AZURE_CLIENT_ID", "")
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0).UTC()}
	m := &fakeAzureMinter{ttl: 75 * time.Minute, now: clock.now}
	r := newAzureTestResolver(clock, m, discardLogger())
	ctx := context.Background()
	tgt := azureTestTarget()

	first, _ := r.Resolve(ctx, tgt)
	if m.callCount() != 1 {
		t.Fatalf("first resolve: calls = %d, want 1", m.callCount())
	}

	// Within skew (60 min < 70 min) → reuse, no new mint.
	clock.advance(60 * time.Minute)
	reuse, _ := r.Resolve(ctx, tgt)
	if m.callCount() != 1 {
		t.Errorf("within skew: calls = %d, want 1 (reuse)", m.callCount())
	}
	if reuse.Password != first.Password {
		t.Errorf("within skew: token changed %q -> %q", first.Password, reuse.Password)
	}

	// Cross the skew (now 71 min old > 70 min) → re-mint.
	clock.advance(11 * time.Minute)
	refreshed, _ := r.Resolve(ctx, tgt)
	if m.callCount() != 2 {
		t.Errorf("after skew: calls = %d, want 2 (re-mint)", m.callCount())
	}
	if refreshed.Password == first.Password {
		t.Errorf("after skew: token should change, still %q", refreshed.Password)
	}

	// Different target → distinct cache key → independent mint.
	other := azureTestTarget()
	other.Name = "az2"
	other.Host = "other.postgres.database.azure.com"
	if _, err := r.Resolve(ctx, other); err != nil {
		t.Fatalf("resolve other: %v", err)
	}
	if m.callCount() != 3 {
		t.Errorf("second target: calls = %d, want 3 (not shared)", m.callCount())
	}
}

// AC-AZURE-005 — a mint failure surfaces an actionable error attributable
// to azure_entra and returns no token.
func TestResolveAzureMintFailure(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0).UTC()}
	m := &fakeAzureMinter{ttl: 75 * time.Minute, now: clock.now, err: errors.New("AADSTS700016: token endpoint denied")}
	r := newAzureTestResolver(clock, m, discardLogger())

	cred, err := r.Resolve(context.Background(), azureTestTarget())
	if err == nil {
		t.Fatalf("expected mint error, got nil")
	}
	if !strings.Contains(err.Error(), "azure_entra") {
		t.Errorf("error should be attributable to azure_entra; got: %v", err)
	}
	if cred.Password != "" {
		t.Errorf("no token should be returned on failure, got %q", cred.Password)
	}
}

// AC-AZURE-006 — an identity-resolution failure fails the target with an
// actionable error naming the disambiguation step; it leaves no cached
// entry, and a subsequent healthy resolution still succeeds (other
// targets keep collecting; FC-AZURE-005).
func TestResolveAzureIdentityErrorIsActionableAndIsolated(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0).UTC()}
	m := &fakeAzureMinter{ttl: 75 * time.Minute, now: clock.now,
		err: errors.New("ManagedIdentityCredential: multiple user-assigned identities exist, specify which to use")}
	r := newAzureTestResolver(clock, m, discardLogger())
	ctx := context.Background()

	_, err := r.Resolve(ctx, azureTestTarget())
	if err == nil {
		t.Fatalf("expected identity error, got nil")
	}
	if !strings.Contains(err.Error(), "azure_client_id") {
		t.Errorf("error should name the azure_client_id disambiguation; got: %v", err)
	}

	// The transient failure must not block a later healthy attempt.
	m.mu.Lock()
	m.err = nil
	m.mu.Unlock()
	healthy := azureTestTarget()
	healthy.Name = "az2"
	healthy.Host = "other.postgres.database.azure.com"
	if _, err := r.Resolve(ctx, healthy); err != nil {
		t.Errorf("a healthy target must still resolve after another target failed: %v", err)
	}
}

// AC-AZURE-007 / INV002 / INV007 — a successful resolution logs metadata
// (auth_method, scope, db_user) but never the token value.
func TestResolveAzureLogsMetadataNotToken(t *testing.T) {
	t.Setenv("AZURE_CLIENT_ID", "")
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0).UTC()}
	m := &fakeAzureMinter{ttl: 75 * time.Minute, now: clock.now}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	r := newAzureTestResolver(clock, m, logger)

	cred, err := r.Resolve(context.Background(), azureTestTarget())
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
	for _, want := range []string{"azure_entra", entraScope, "monitor"} {
		if !strings.Contains(out, want) {
			t.Errorf("log should contain metadata %q; got: %s", want, out)
		}
	}
}

// Confirmed design — user-assigned MI disambiguation: an explicit
// azure_client_id is handed to the minter; absent that, AZURE_CLIENT_ID is
// used; absent both, the empty client id lets the chain pick the
// single/system-assigned identity.
func TestResolveAzureClientIDResolution(t *testing.T) {
	t.Run("config wins", func(t *testing.T) {
		t.Setenv("AZURE_CLIENT_ID", "env-client-id")
		clock := &fakeClock{t: time.Unix(1_700_000_000, 0).UTC()}
		m := &fakeAzureMinter{ttl: 75 * time.Minute, now: clock.now}
		r := newAzureTestResolver(clock, m, discardLogger())
		tgt := azureTestTarget()
		tgt.AzureClientID = "config-client-id"
		if _, err := r.Resolve(context.Background(), tgt); err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if m.lastClientID() != "config-client-id" {
			t.Errorf("client id = %q, want config-client-id", m.lastClientID())
		}
	})

	t.Run("env fallback", func(t *testing.T) {
		t.Setenv("AZURE_CLIENT_ID", "env-client-id")
		clock := &fakeClock{t: time.Unix(1_700_000_000, 0).UTC()}
		m := &fakeAzureMinter{ttl: 75 * time.Minute, now: clock.now}
		r := newAzureTestResolver(clock, m, discardLogger())
		if _, err := r.Resolve(context.Background(), azureTestTarget()); err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if m.lastClientID() != "env-client-id" {
			t.Errorf("client id = %q, want env-client-id", m.lastClientID())
		}
	})

	t.Run("none set", func(t *testing.T) {
		t.Setenv("AZURE_CLIENT_ID", "")
		clock := &fakeClock{t: time.Unix(1_700_000_000, 0).UTC()}
		m := &fakeAzureMinter{ttl: 75 * time.Minute, now: clock.now}
		r := newAzureTestResolver(clock, m, discardLogger())
		if _, err := r.Resolve(context.Background(), azureTestTarget()); err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if m.lastClientID() != "" {
			t.Errorf("client id = %q, want empty", m.lastClientID())
		}
	})
}

// AC-AZURE-009 — the operator-guidance text names the exact
// pgaadauth_create_principal snippet and the display-name match note, with
// no secret material.
func TestAzureEntraGuidance(t *testing.T) {
	g := AzureEntraGuidance(azureTestTarget())
	if !strings.Contains(g, "pgaadauth_create_principal('monitor'") {
		t.Errorf("guidance should include the create-principal snippet for the role; got: %s", g)
	}
	if !strings.Contains(g, "display name") {
		t.Errorf("guidance should note the role/principal display-name match; got: %s", g)
	}
}
