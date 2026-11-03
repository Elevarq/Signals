package tests

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/elevarq/signals/internal/collector"
	"github.com/elevarq/signals/internal/config"
	"github.com/elevarq/signals/internal/db"
)

// ---------------------------------------------------------------------------
// R100 / config reload — unit-level tests for Collector.Reload.
//
// Spec: features/signals/specification.md § Configuration reload
// ---------------------------------------------------------------------------

func newReloadTestCollector(t *testing.T, initial []config.TargetConfig) (*collector.Collector, *db.DB) {
	t.Helper()
	dir := t.TempDir()
	store, err := db.Open(filepath.Join(dir, "test.db"), false)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	c := collector.New(store, initial, time.Hour, 30,
		collector.WithMinSnapshotInterval(60*time.Second))
	return c, store
}

// Reload with the same target list is a no-op for callers.
func TestReload_NoOpWhenTargetsUnchanged(t *testing.T) {
	tgts := []config.TargetConfig{
		{Name: "a", Host: "h", Port: 5432, DBName: "d", User: "u", SSLMode: "disable", Enabled: true},
	}
	c, _ := newReloadTestCollector(t, tgts)
	if err := c.Reload(tgts); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	got := c.Targets()
	if len(got) != 1 || got[0].Name != "a" {
		t.Errorf("Reload no-op: got %+v, want one target named a", got)
	}
}

// Reload adding a target makes it visible immediately.
func TestReload_AddsNewTarget(t *testing.T) {
	c, _ := newReloadTestCollector(t, []config.TargetConfig{
		{Name: "a", Host: "h", Port: 5432, DBName: "d", User: "u", SSLMode: "disable", Enabled: true},
	})
	if err := c.Reload([]config.TargetConfig{
		{Name: "a", Host: "h", Port: 5432, DBName: "d", User: "u", SSLMode: "disable", Enabled: true},
		{Name: "b", Host: "h2", Port: 5432, DBName: "d", User: "u", SSLMode: "disable", Enabled: true},
	}); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	got := c.Targets()
	if len(got) != 2 {
		t.Fatalf("expected 2 targets after Reload-add, got %d", len(got))
	}
	names := map[string]bool{}
	for _, t := range got {
		names[t.Name] = true
	}
	if !names["a"] || !names["b"] {
		t.Errorf("expected {a, b}, got %v", names)
	}
}

// Reload removing a target removes it from the active list.
func TestReload_RemovesTarget(t *testing.T) {
	c, _ := newReloadTestCollector(t, []config.TargetConfig{
		{Name: "a", Host: "h", Port: 5432, DBName: "d", User: "u", SSLMode: "disable", Enabled: true},
		{Name: "b", Host: "h", Port: 5432, DBName: "d", User: "u", SSLMode: "disable", Enabled: true},
	})
	if err := c.Reload([]config.TargetConfig{
		{Name: "a", Host: "h", Port: 5432, DBName: "d", User: "u", SSLMode: "disable", Enabled: true},
	}); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	got := c.Targets()
	if len(got) != 1 || got[0].Name != "a" {
		t.Errorf("expected only 'a' after Reload-remove; got %+v", got)
	}
}

// Reload returning a deep copy — the caller mutating the returned slice
// must NOT affect the collector's internal state.
func TestReload_TargetsReturnsDefensiveCopy(t *testing.T) {
	c, _ := newReloadTestCollector(t, []config.TargetConfig{
		{Name: "a", Host: "h", Port: 5432, DBName: "d", User: "u", SSLMode: "disable", Enabled: true},
	})
	got := c.Targets()
	got[0].Name = "mutated"

	again := c.Targets()
	if again[0].Name != "a" {
		t.Errorf("Targets() returned a shared reference; got %q after caller mutation", again[0].Name)
	}
}

// The reload swap is safe from concurrent Targets() reads — race
// detector verifies absence of data races.
func TestReload_ConcurrentReadsAreSafe(t *testing.T) {
	c, _ := newReloadTestCollector(t, []config.TargetConfig{
		{Name: "a", Host: "h", Port: 5432, DBName: "d", User: "u", SSLMode: "disable", Enabled: true},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for ctx.Err() == nil {
			_ = c.Targets()
		}
	}()

	for ctx.Err() == nil {
		// Discard the error in this race-stress loop — the test asserts
		// concurrency safety, not reconcile success. (#16 returns error
		// from Reload; a stray reconcile failure here would still be
		// surfaced by the dedicated #16 propagation test.)
		_ = c.Reload([]config.TargetConfig{
			{Name: "a", Host: "h", Port: 5432, DBName: "d", User: "u", SSLMode: "disable", Enabled: true},
			{Name: "b", Host: "h", Port: 5432, DBName: "d", User: "u", SSLMode: "disable", Enabled: true},
		})
		_ = c.Reload([]config.TargetConfig{
			{Name: "a", Host: "h", Port: 5432, DBName: "d", User: "u", SSLMode: "disable", Enabled: true},
		})
	}
	<-done
}

// ---------------------------------------------------------------------------
// R100.1 / #328 — connection-identity pool invalidation on reload.
//
// A reload that changes ANY field affecting dialing, TLS, credential
// selection, credential content, or credential caching MUST close the
// modified target's pool so the next cycle re-dials and re-resolves the
// credential with the new config. Before the fix, sameConnection
// compared only host/port/db/user/sslmode + the three password-source
// fields, so a change to any of the TLS / cloud-identity / secret /
// cache fields silently kept the stale pool.
// ---------------------------------------------------------------------------

// baseTarget is a minimal, valid connection-identity baseline the
// per-field table mutates one field at a time.
func baseTarget() config.TargetConfig {
	return config.TargetConfig{
		Name: "t", Host: "h", Port: 5432, DBName: "d", User: "u",
		SSLMode: "disable", Enabled: true,
	}
}

// TestReload_PoolInvalidatedPerConnectionField asserts that changing
// EACH pool-affecting TargetConfig field on reload closes the target's
// pool. The pool is seeded lazily (never dials), so the assertion is
// purely on pool presence in the collector's map.
func TestReload_PoolInvalidatedPerConnectionField(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*config.TargetConfig)
	}{
		// Dialing
		{"Host", func(tc *config.TargetConfig) { tc.Host = "h2" }},
		{"Port", func(tc *config.TargetConfig) { tc.Port = 6543 }},
		{"DBName", func(tc *config.TargetConfig) { tc.DBName = "d2" }},
		{"User", func(tc *config.TargetConfig) { tc.User = "u2" }},
		{"SSLMode", func(tc *config.TargetConfig) { tc.SSLMode = "require" }},
		// TLS: server verification + mTLS client material
		{"SSLRootCertFile", func(tc *config.TargetConfig) { tc.SSLRootCertFile = "/etc/ca.pem" }},
		{"SSLCert", func(tc *config.TargetConfig) { tc.SSLCert = "/etc/client.pem" }},
		{"SSLKey", func(tc *config.TargetConfig) { tc.SSLKey = "/etc/client.key" }},
		// Credential selection
		{"AuthMethod", func(tc *config.TargetConfig) { tc.AuthMethod = config.AuthMethodAWSRDSIAM }},
		{"Region", func(tc *config.TargetConfig) { tc.Region = "eu-west-1" }},
		{"AzureClientID", func(tc *config.TargetConfig) { tc.AzureClientID = "11111111-1111-1111-1111-111111111111" }},
		{"GCPImpersonateServiceAccount", func(tc *config.TargetConfig) {
			tc.GCPImpersonateServiceAccount = "svc@proj.iam.gserviceaccount.com"
		}},
		{"SecretRef", func(tc *config.TargetConfig) { tc.SecretRef = "arn:aws:secretsmanager:eu-west-1:1:secret:db" }},
		{"SecretJSONKey", func(tc *config.TargetConfig) { tc.SecretJSONKey = "password" }},
		// Credential content (password sources)
		{"PasswordFile", func(tc *config.TargetConfig) { tc.PasswordFile = "/etc/pw" }},
		{"PasswordEnv", func(tc *config.TargetConfig) { tc.PasswordEnv = "PGPASSWORD" }},
		{"PgpassFile", func(tc *config.TargetConfig) { tc.PgpassFile = "/etc/pgpass" }},
		// Credential caching
		{"MaxCacheTTL", func(tc *config.TargetConfig) { tc.MaxCacheTTL = 5 * time.Minute }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newReloadTestCollector(t, []config.TargetConfig{baseTarget()})
			if err := c.SeedPoolForTest("t"); err != nil {
				t.Fatalf("SeedPoolForTest: %v", err)
			}
			if !c.HasPoolForTest("t") {
				t.Fatal("precondition: expected a seeded pool for target t")
			}

			changed := baseTarget()
			tc.mutate(&changed)
			if err := c.Reload([]config.TargetConfig{changed}); err != nil {
				t.Fatalf("Reload: %v", err)
			}

			if c.HasPoolForTest("t") {
				t.Errorf("field %s changed on reload but pool was NOT closed — stale pool keeps the old connection identity", tc.name)
			}
		})
	}
}

// TestReload_ProfileOnlyChangeKeepsPool asserts that changing only a
// non-connection field (the per-target sensitivity profile, or the
// enabled flag) does NOT drop the pool — an unnecessary re-dial is a
// regression the connection-identity comparison must avoid.
func TestReload_ProfileOnlyChangeKeepsPool(t *testing.T) {
	c, _ := newReloadTestCollector(t, []config.TargetConfig{baseTarget()})
	if err := c.SeedPoolForTest("t"); err != nil {
		t.Fatalf("SeedPoolForTest: %v", err)
	}

	changed := baseTarget()
	changed.Collectors = config.TargetCollectorConfig{Profile: "minimal"}
	if err := c.Reload([]config.TargetConfig{changed}); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	if !c.HasPoolForTest("t") {
		t.Error("profile-only change dropped the pool; the connection identity should be unchanged so the pool is preserved")
	}
}

// recordingResolver captures every TargetConfig a pool's BeforeConnect
// closure resolves with, so the staleness regression can assert the
// NEW config is used after reload.
type recordingResolver struct {
	mu   sync.Mutex
	seen []config.TargetConfig
}

func (r *recordingResolver) Resolve(_ context.Context, tgt config.TargetConfig) (collector.Credential, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, tgt)
	return collector.Credential{Kind: collector.CredKindPassword, Password: "x"}, nil
}

func (r *recordingResolver) last() (config.TargetConfig, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.seen) == 0 {
		return config.TargetConfig{}, false
	}
	return r.seen[len(r.seen)-1], true
}

// TestReload_NewConnectionResolvesWithNewConfig is the direct
// regression for the BeforeConnect-closure staleness: after a reload
// changes a credential field, the pool rebuilt on the next connection
// must resolve the credential against the NEW TargetConfig, never the
// one captured by the pre-reload pool's closure.
func TestReload_NewConnectionResolvesWithNewConfig(t *testing.T) {
	rec := &recordingResolver{}
	// Build the collector with the recording resolver injected via the
	// #328 seam so we can observe the exact TargetConfig each pool's
	// BeforeConnect closure resolves with.
	dir := t.TempDir()
	store, err := db.Open(filepath.Join(dir, "test.db"), false)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	c := collector.New(store, []config.TargetConfig{baseTarget()}, time.Hour, 30,
		collector.WithMinSnapshotInterval(60*time.Second),
		collector.WithCredentialResolverForTest(rec))

	ctx := context.Background()

	// First connection: resolves against the ORIGINAL config.
	before, err := c.EnsurePoolBeforeConnectForTest(ctx, baseTarget())
	if err != nil {
		t.Fatalf("EnsurePoolBeforeConnectForTest (before): %v", err)
	}
	if err := before(ctx, &pgx.ConnConfig{}); err != nil {
		t.Fatalf("before BeforeConnect: %v", err)
	}
	if got, ok := rec.last(); !ok || got.SecretRef != "" {
		t.Fatalf("precondition: first resolve should see the original config (empty SecretRef), got %+v ok=%v", got, ok)
	}

	// Change a credential-selection field and reload.
	changed := baseTarget()
	changed.AuthMethod = config.AuthMethodSecretStore
	changed.SecretRef = "arn:aws:secretsmanager:eu-west-1:1:secret:new"
	if err := c.Reload([]config.TargetConfig{changed}); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	// The pool must have been closed by reload, so a new connection
	// rebuilds the closure with the NEW config.
	if c.HasPoolForTest("t") {
		t.Fatal("reload did not close the pool for the changed credential field")
	}

	after, err := c.EnsurePoolBeforeConnectForTest(ctx, changed)
	if err != nil {
		t.Fatalf("EnsurePoolBeforeConnectForTest (after): %v", err)
	}
	if err := after(ctx, &pgx.ConnConfig{}); err != nil {
		t.Fatalf("after BeforeConnect: %v", err)
	}

	got, ok := rec.last()
	if !ok {
		t.Fatal("resolver was never called after reload")
	}
	if got.SecretRef != changed.SecretRef || got.AuthMethod != config.AuthMethodSecretStore {
		t.Errorf("post-reload connection resolved with STALE config: got AuthMethod=%q SecretRef=%q, want AuthMethod=%q SecretRef=%q",
			got.AuthMethod, got.SecretRef, config.AuthMethodSecretStore, changed.SecretRef)
	}
}
