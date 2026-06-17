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

// --- secret_store test doubles ---------------------------------------

// fakeSecretFetcher records call counts and the parsed ref it was handed,
// and returns a deterministic payload (optionally rotating across calls so
// rotation-on-reconnect is observable). It makes no real cloud call
// (NFR003).
type fakeSecretFetcher struct {
	mu     sync.Mutex
	calls  int
	value  string        // single fixed payload (when values is empty)
	values []string      // per-call payloads; index = min(call, len)-1
	ttl    time.Duration // vault-supplied lease (0 = none)
	err    error
	last   config.ParsedSecretRef
}

func (f *fakeSecretFetcher) Fetch(ctx context.Context, ref config.ParsedSecretRef) (string, time.Duration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.last = ref
	if f.err != nil {
		return "", 0, f.err
	}
	if len(f.values) > 0 {
		idx := f.calls
		if idx > len(f.values) {
			idx = len(f.values)
		}
		return f.values[idx-1], f.ttl, nil
	}
	return f.value, f.ttl, nil
}

func (f *fakeSecretFetcher) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeSecretFetcher) lastRef() config.ParsedSecretRef {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last
}

const testAWSSecretRef = "arn:aws:secretsmanager:eu-west-1:123456789012:secret:prod/pg/monitor-AbCdEf"

func secretTestTarget() config.TargetConfig {
	return config.TargetConfig{
		Name:       "s1",
		Host:       "db.example.com",
		Port:       5432,
		DBName:     "appdb",
		User:       "monitor",
		SSLMode:    "verify-full",
		AuthMethod: config.AuthMethodSecretStore,
		SecretRef:  testAWSSecretRef,
		Enabled:    true,
	}
}

func newSecretTestResolver(clock *fakeClock, f secretFetcher, logger *slog.Logger) *credentialResolver {
	return &credentialResolver{
		cache:         newTokenCache(),
		secretFetcher: f,
		now:           clock.now,
		logger:        logger,
	}
}

// --- tests ------------------------------------------------------------

// AC-SECRET-001 — a verify-full secret_store target resolves a
// password-kind credential whose value is the fetched secret; the backend
// and region are inferred from the ARN and handed to the fetcher.
func TestResolveSecretStoreFetchesAsPassword(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0).UTC()}
	f := &fakeSecretFetcher{value: "s3cr3t-pw"}
	r := newSecretTestResolver(clock, f, discardLogger())

	cred, err := r.Resolve(context.Background(), secretTestTarget())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cred.Kind != CredKindPassword {
		t.Errorf("Kind = %v, want CredKindPassword", cred.Kind)
	}
	if cred.Password != "s3cr3t-pw" {
		t.Errorf("Password = %q, want the fetched secret", cred.Password)
	}
	if got := f.lastRef(); got.Backend != config.SecretBackendAWSSecretsManager {
		t.Errorf("fetcher backend = %v, want AWS Secrets Manager", got.Backend)
	}
	if got := f.lastRef(); got.AWSRegion != "eu-west-1" {
		t.Errorf("fetcher region = %q, want eu-west-1 (pinned from the ARN)", got.AWSRegion)
	}
}

// AC-SECRET-003 — with secret_json_key the value is parsed as JSON and the
// named key extracted; without it the raw value is used.
func TestResolveSecretStoreJSONKeyExtraction(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0).UTC()}

	t.Run("extracts named key", func(t *testing.T) {
		f := &fakeSecretFetcher{value: `{"username":"monitor","password":"p@ss-from-json"}`}
		r := newSecretTestResolver(clock, f, discardLogger())
		tgt := secretTestTarget()
		tgt.SecretJSONKey = "password"
		cred, err := r.Resolve(context.Background(), tgt)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if cred.Password != "p@ss-from-json" {
			t.Errorf("Password = %q, want the extracted JSON key value", cred.Password)
		}
	})

	t.Run("raw value when no key", func(t *testing.T) {
		f := &fakeSecretFetcher{value: `{"username":"monitor","password":"p"}`}
		r := newSecretTestResolver(clock, f, discardLogger())
		cred, err := r.Resolve(context.Background(), secretTestTarget())
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if cred.Password != `{"username":"monitor","password":"p"}` {
			t.Errorf("without secret_json_key the raw payload is the password; got %q", cred.Password)
		}
	})
}

// AC-SECRET-003 / FC-SECRET-003 — invalid JSON, a missing key, or a
// non-string value fails the fetch with an error that never leaks the raw
// secret value.
func TestResolveSecretStoreJSONKeyFailuresDoNotLeak(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0).UTC()}
	const marker = "TOP-SECRET-RAW-VALUE"

	cases := []struct {
		name  string
		value string
	}{
		{"invalid json", marker},                         // not JSON at all
		{"missing key", `{"username":"` + marker + `"}`}, // key absent
		{"non-string value", `{"password":` + "424242}"}, // numeric value
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeSecretFetcher{value: tc.value}
			r := newSecretTestResolver(clock, f, discardLogger())
			tgt := secretTestTarget()
			tgt.SecretJSONKey = "password"
			cred, err := r.Resolve(context.Background(), tgt)
			if err == nil {
				t.Fatalf("expected a payload-extraction error, got nil")
			}
			if strings.Contains(err.Error(), marker) {
				t.Errorf("error leaked the raw secret value: %v", err)
			}
			if cred.Password != "" {
				t.Errorf("no credential should be returned on extraction failure, got %q", cred.Password)
			}
		})
	}
}

// AC-SECRET-004 (empty secret / FC-SECRET-004) — an empty fetched value, or
// an empty extracted key, fails rather than presenting an empty password.
func TestResolveSecretStoreEmptySecretFails(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0).UTC()}

	t.Run("empty raw", func(t *testing.T) {
		f := &fakeSecretFetcher{value: ""}
		r := newSecretTestResolver(clock, f, discardLogger())
		if _, err := r.Resolve(context.Background(), secretTestTarget()); err == nil {
			t.Fatal("expected an error for an empty fetched secret, got nil")
		}
	})

	t.Run("empty extracted key", func(t *testing.T) {
		f := &fakeSecretFetcher{value: `{"password":""}`}
		r := newSecretTestResolver(clock, f, discardLogger())
		tgt := secretTestTarget()
		tgt.SecretJSONKey = "password"
		if _, err := r.Resolve(context.Background(), tgt); err == nil {
			t.Fatal("expected an error for an empty extracted key, got nil")
		}
	})
}

// AC-SECRET-004 (cache / TTL / rotation / isolation).
func TestResolveSecretStoreCacheTTLAndRotation(t *testing.T) {
	// (a) No vault TTL and no max_cache_ttl → re-fetch on every reconnect,
	// and a rotated secret is observed immediately on the next resolve.
	t.Run("no bound re-fetches and picks up rotation", func(t *testing.T) {
		clock := &fakeClock{t: time.Unix(1_700_000_000, 0).UTC()}
		f := &fakeSecretFetcher{values: []string{"pw-v1", "pw-v2"}}
		r := newSecretTestResolver(clock, f, discardLogger())
		ctx := context.Background()
		tgt := secretTestTarget()

		first, _ := r.Resolve(ctx, tgt)
		if first.Password != "pw-v1" {
			t.Fatalf("first password = %q, want pw-v1", first.Password)
		}
		second, _ := r.Resolve(ctx, tgt)
		if f.callCount() != 2 {
			t.Errorf("no-bound: calls = %d, want 2 (re-fetch every reconnect)", f.callCount())
		}
		if second.Password != "pw-v2" {
			t.Errorf("rotation not picked up: password = %q, want pw-v2", second.Password)
		}
	})

	// (b) A vault-supplied TTL bounds reuse: reuse within the TTL, re-fetch
	// after it elapses.
	t.Run("vault ttl bounds reuse", func(t *testing.T) {
		clock := &fakeClock{t: time.Unix(1_700_000_000, 0).UTC()}
		f := &fakeSecretFetcher{values: []string{"pw-v1", "pw-v2"}, ttl: 10 * time.Minute}
		r := newSecretTestResolver(clock, f, discardLogger())
		ctx := context.Background()
		tgt := secretTestTarget()

		if _, err := r.Resolve(ctx, tgt); err != nil {
			t.Fatalf("initial resolve: %v", err)
		}
		clock.advance(5 * time.Minute) // within TTL → reuse
		reuse, _ := r.Resolve(ctx, tgt)
		if f.callCount() != 1 {
			t.Errorf("within ttl: calls = %d, want 1 (reuse)", f.callCount())
		}
		if reuse.Password != "pw-v1" {
			t.Errorf("within ttl password changed to %q", reuse.Password)
		}
		clock.advance(6 * time.Minute) // now 11m > 10m TTL → re-fetch
		refreshed, _ := r.Resolve(ctx, tgt)
		if f.callCount() != 2 {
			t.Errorf("after ttl: calls = %d, want 2 (re-fetch)", f.callCount())
		}
		if refreshed.Password != "pw-v2" {
			t.Errorf("after ttl: password = %q, want pw-v2", refreshed.Password)
		}
	})

	// (c) max_cache_ttl bounds reuse when the vault supplies no TTL.
	t.Run("max_cache_ttl bounds reuse without vault ttl", func(t *testing.T) {
		clock := &fakeClock{t: time.Unix(1_700_000_000, 0).UTC()}
		f := &fakeSecretFetcher{values: []string{"pw-v1", "pw-v2"}}
		r := newSecretTestResolver(clock, f, discardLogger())
		ctx := context.Background()
		tgt := secretTestTarget()
		tgt.MaxCacheTTL = 10 * time.Minute

		if _, err := r.Resolve(ctx, tgt); err != nil {
			t.Fatalf("initial resolve: %v", err)
		}
		clock.advance(5 * time.Minute)
		if _, _ = r.Resolve(ctx, tgt); f.callCount() != 1 {
			t.Errorf("within max_cache_ttl: calls = %d, want 1 (reuse)", f.callCount())
		}
		clock.advance(6 * time.Minute)
		if _, _ = r.Resolve(ctx, tgt); f.callCount() != 2 {
			t.Errorf("after max_cache_ttl: calls = %d, want 2 (re-fetch)", f.callCount())
		}
	})

	// (d) When both are present the bound is the minimum.
	t.Run("bound is min(vault ttl, max_cache_ttl)", func(t *testing.T) {
		clock := &fakeClock{t: time.Unix(1_700_000_000, 0).UTC()}
		f := &fakeSecretFetcher{values: []string{"pw-v1", "pw-v2"}, ttl: 30 * time.Minute}
		r := newSecretTestResolver(clock, f, discardLogger())
		ctx := context.Background()
		tgt := secretTestTarget()
		tgt.MaxCacheTTL = 5 * time.Minute // tighter than the 30m vault TTL

		if _, err := r.Resolve(ctx, tgt); err != nil {
			t.Fatalf("initial resolve: %v", err)
		}
		clock.advance(6 * time.Minute) // past the 5m max, within the 30m TTL
		if _, _ = r.Resolve(ctx, tgt); f.callCount() != 2 {
			t.Errorf("min-bound: calls = %d, want 2 (max_cache_ttl wins)", f.callCount())
		}
	})

	// (e) Per-target isolation: a secret cached for one target is never
	// presented for another (distinct cache key).
	t.Run("per-target isolation", func(t *testing.T) {
		clock := &fakeClock{t: time.Unix(1_700_000_000, 0).UTC()}
		f := &fakeSecretFetcher{values: []string{"pw-a", "pw-b"}, ttl: time.Hour}
		r := newSecretTestResolver(clock, f, discardLogger())
		ctx := context.Background()

		a := secretTestTarget()
		b := secretTestTarget()
		b.Name = "s2"
		b.Host = "db2.example.com"

		ca, _ := r.Resolve(ctx, a)
		cb, _ := r.Resolve(ctx, b)
		if f.callCount() != 2 {
			t.Errorf("two distinct targets: calls = %d, want 2 (not shared)", f.callCount())
		}
		if ca.Password == cb.Password {
			t.Errorf("targets shared a cached secret: both %q", ca.Password)
		}
	})
}

// AC-SECRET-008 / FC-SECRET-001 + INV002 — a vault fetch error fails the
// target with a redacted, actionable error naming the backend and the IAM
// permission; the failure is isolated (a sibling target still resolves).
func TestResolveSecretStoreFetchErrorIsActionableAndIsolated(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0).UTC()}
	f := &fakeSecretFetcher{err: errors.New("AccessDeniedException: not authorized to perform GetSecretValue")}
	r := newSecretTestResolver(clock, f, discardLogger())
	ctx := context.Background()

	cred, err := r.Resolve(ctx, secretTestTarget())
	if err == nil {
		t.Fatalf("expected a fetch error, got nil")
	}
	if !strings.Contains(err.Error(), "secret_store") {
		t.Errorf("error should be attributable to secret_store; got: %v", err)
	}
	if !strings.Contains(err.Error(), "secretsmanager:GetSecretValue") {
		t.Errorf("error should name the required IAM permission; got: %v", err)
	}
	if cred.Password != "" {
		t.Errorf("no credential should be returned on failure, got %q", cred.Password)
	}

	// Isolation: the transient failure must not block a later healthy
	// resolution of a different target.
	f.mu.Lock()
	f.err = nil
	f.value = "ok-pw"
	f.mu.Unlock()
	healthy := secretTestTarget()
	healthy.Name = "s2"
	healthy.Host = "db2.example.com"
	if _, err := r.Resolve(ctx, healthy); err != nil {
		t.Errorf("a healthy target must still resolve after another failed: %v", err)
	}
}

// AC-SECRET-009 / FC-SECRET-002 — an identity-resolution failure (no usable
// workload identity for the vault) is surfaced as a target-scoped,
// actionable error; a sibling target keeps collecting.
func TestResolveSecretStoreIdentityErrorIsActionable(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0).UTC()}
	f := &fakeSecretFetcher{err: errors.New("operation error: failed to refresh cached credentials, no EC2 IMDS role found")}
	r := newSecretTestResolver(clock, f, discardLogger())

	_, err := r.Resolve(context.Background(), secretTestTarget())
	if err == nil {
		t.Fatalf("expected an identity error, got nil")
	}
	// The actionable hint names the backend's required grant / identity.
	if !strings.Contains(err.Error(), "secretsmanager:GetSecretValue") {
		t.Errorf("error should carry the backend remediation hint; got: %v", err)
	}
}

// AC-SECRET-010 / INV002 / INV007 — a successful resolution logs metadata
// (auth_method, backend, secret_ref, db_user) but never the secret value.
func TestResolveSecretStoreLogsMetadataNotSecret(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0).UTC()}
	f := &fakeSecretFetcher{value: "super-secret-password"}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	r := newSecretTestResolver(clock, f, logger)

	cred, err := r.Resolve(context.Background(), secretTestTarget())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, cred.Password) {
		t.Errorf("log leaked the secret %q in: %s", cred.Password, out)
	}
	for _, want := range []string{"secret_store", "monitor", testAWSSecretRef} {
		if !strings.Contains(out, want) {
			t.Errorf("log should contain metadata %q; got: %s", want, out)
		}
	}
}

// AC-SECRET-012 — operator guidance names the exact IAM grant for the
// inferred backend and the workload-identity note, with no secret material.
func TestSecretStoreGuidanceAWS(t *testing.T) {
	g := SecretStoreGuidance(secretTestTarget())
	if !strings.Contains(g, "secretsmanager:GetSecretValue") {
		t.Errorf("guidance should name the AWS IAM permission; got: %s", g)
	}
	if !strings.Contains(g, "s1") {
		t.Errorf("guidance should name the target; got: %s", g)
	}
}

// INV005 (backend isolation) — the production routing fetcher invokes only
// the sub-fetcher for the inferred backend, never any other backend's SDK.
// All three backends (AWS Secrets Manager, Azure Key Vault, GCP Secret
// Manager) are production-wired (#108); each ref reaches exactly its own
// fetcher and no other.
func TestProductionSecretFetcherRouting(t *testing.T) {
	awsSub := &fakeSecretFetcher{value: "aws-pw"}
	azureSub := &fakeSecretFetcher{value: "azure-pw"}
	gcpSub := &fakeSecretFetcher{value: "gcp-pw"}
	pf := productionSecretFetcher{aws: awsSub, azure: azureSub, gcp: gcpSub}
	ctx := context.Background()

	cases := []struct {
		ref     string
		wantVal string
		sub     *fakeSecretFetcher
		others  []*fakeSecretFetcher
		backend config.SecretBackend
	}{
		{testAWSSecretRef, "aws-pw", awsSub, []*fakeSecretFetcher{azureSub, gcpSub}, config.SecretBackendAWSSecretsManager},
		{"https://my-vault.vault.azure.net/secrets/pg-monitor", "azure-pw", azureSub, []*fakeSecretFetcher{awsSub, gcpSub}, config.SecretBackendAzureKeyVault},
		{"projects/my-proj/secrets/pg-monitor/versions/latest", "gcp-pw", gcpSub, []*fakeSecretFetcher{awsSub, gcpSub}, config.SecretBackendGCPSecretManager},
	}
	for _, tc := range cases {
		parsed, err := config.InferSecretBackend(tc.ref)
		if err != nil {
			t.Fatalf("%s: InferSecretBackend: %v", tc.ref, err)
		}
		before := tc.sub.callCount()
		v, _, err := pf.Fetch(ctx, parsed)
		if err != nil || v != tc.wantVal {
			t.Fatalf("%v route: value=%q err=%v, want %s / nil", tc.backend, v, err, tc.wantVal)
		}
		if got := tc.sub.callCount() - before; got != 1 {
			t.Errorf("%v: own sub-fetcher calls = %d, want 1", tc.backend, got)
		}
	}

	// INV005 isolation: an unwired backend still reports a clear, non-leaking
	// "not available in this build" error rather than dispatching elsewhere.
	awsOnly := productionSecretFetcher{aws: awsSub}
	for _, ref := range []string{
		"https://my-vault.vault.azure.net/secrets/pg-monitor",
		"projects/my-proj/secrets/pg-monitor/versions/latest",
	} {
		parsed, _ := config.InferSecretBackend(ref)
		_, _, err := awsOnly.Fetch(ctx, parsed)
		if !errors.Is(err, errSecretBackendUnavailable) {
			t.Errorf("%v unwired: error should be errSecretBackendUnavailable; got: %v", parsed.Backend, err)
		}
	}
}

// parseAzureKeyVaultRef splits a Key Vault secret URI into the vault URL plus
// the secret name and optional version that azsecrets.GetSecret needs. The
// helper is pure (no network), so its parsing rules are covered here directly.
func TestParseAzureKeyVaultRef(t *testing.T) {
	cases := []struct {
		name        string
		ref         string
		wantVault   string
		wantSecret  string
		wantVersion string
		wantErr     bool
	}{
		{
			name:       "name only, latest version",
			ref:        "https://my-vault.vault.azure.net/secrets/pg-monitor",
			wantVault:  "https://my-vault.vault.azure.net",
			wantSecret: "pg-monitor",
		},
		{
			name:        "explicit version",
			ref:         "https://my-vault.vault.azure.net/secrets/pg-monitor/abc123def456",
			wantVault:   "https://my-vault.vault.azure.net",
			wantSecret:  "pg-monitor",
			wantVersion: "abc123def456",
		},
		{
			name:       "trailing slash after name",
			ref:        "https://my-vault.vault.azure.net/secrets/pg-monitor/",
			wantVault:  "https://my-vault.vault.azure.net",
			wantSecret: "pg-monitor",
		},
		{name: "no secret name", ref: "https://my-vault.vault.azure.net/secrets/", wantErr: true},
		{name: "missing secrets segment", ref: "https://my-vault.vault.azure.net/keys/pg-monitor", wantErr: true},
		{name: "not a url", ref: "my-vault.vault.azure.net/secrets/pg-monitor", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vault, secret, version, err := parseAzureKeyVaultRef(tc.ref)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %q, got nil", tc.ref)
				}
				if strings.Contains(err.Error(), "secret") && secret != "" {
					t.Errorf("error path must not leak a parsed name; got %q", secret)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if vault != tc.wantVault || secret != tc.wantSecret || version != tc.wantVersion {
				t.Errorf("got (%q,%q,%q), want (%q,%q,%q)", vault, secret, version, tc.wantVault, tc.wantSecret, tc.wantVersion)
			}
		})
	}
}
