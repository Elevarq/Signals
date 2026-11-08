package collector

import (
	"context"
	"testing"
	"time"
)

// #714 / F08: the credential cache key is the target's full
// CredentialCacheIdentity (connection tuple + name + source selectors + cache
// policy), not just ConnIdentity+auth_method. Two targets that share a
// connection tuple but resolve from a DIFFERENT source must not share a cache
// entry, and a changed cache policy must not reuse a stale entry.

const testAWSSecretRefOther = "arn:aws:secretsmanager:eu-west-1:123456789012:secret:prod/pg/other-XyZ123"

// Control: an identical target resolved twice hits the cache exactly once —
// so the callCount==2 assertions below are meaningful (the cache really caches).
func TestSecretCache_SameSourceReusesEntry(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0).UTC()}
	f := &fakeSecretFetcher{value: "cred-A", ttl: time.Hour}
	r := newSecretTestResolver(clock, f, discardLogger())

	tgt := secretTestTarget()
	if _, err := r.Resolve(context.Background(), tgt); err != nil {
		t.Fatalf("Resolve #1: %v", err)
	}
	if _, err := r.Resolve(context.Background(), tgt); err != nil {
		t.Fatalf("Resolve #2: %v", err)
	}
	if got := f.callCount(); got != 1 {
		t.Fatalf("identical target resolved twice fetched %d times, want 1 (cache must reuse)", got)
	}
}

// The reported repro: same connection tuple + same name, but a DIFFERENT
// secret_ref. Before #714 the key omitted the source, so target B's resolution
// returned target A's credential and the vault was called once.
func TestSecretCache_DifferentSecretRefNotShared(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0).UTC()}
	f := &fakeSecretFetcher{values: []string{"cred-A", "cred-B"}, ttl: time.Hour}
	r := newSecretTestResolver(clock, f, discardLogger())

	tgtA := secretTestTarget()
	tgtA.SecretRef = testAWSSecretRef
	tgtB := secretTestTarget()             // same host/port/db/user/name as A...
	tgtB.SecretRef = testAWSSecretRefOther // ...but a different vault reference

	credA, err := r.Resolve(context.Background(), tgtA)
	if err != nil {
		t.Fatalf("Resolve A: %v", err)
	}
	credB, err := r.Resolve(context.Background(), tgtB)
	if err != nil {
		t.Fatalf("Resolve B: %v", err)
	}

	if credA.Password != "cred-A" {
		t.Errorf("target A password = %q, want cred-A", credA.Password)
	}
	if credB.Password == credA.Password {
		t.Errorf("target B (different secret_ref) got target A's credential %q — cache key must include the source (#714/F08)", credB.Password)
	}
	if credB.Password != "cred-B" {
		t.Errorf("target B password = %q, want cred-B (its own freshly-fetched secret)", credB.Password)
	}
	if got := f.callCount(); got != 2 {
		t.Errorf("fetcher called %d times, want 2 (a different secret_ref must not reuse the cached entry)", got)
	}
}

// A changed cache policy (max_cache_ttl) must also yield a different key so a
// stale entry (fetched under the old policy) is not reused.
func TestSecretCache_ChangedCacheTTLNotShared(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0).UTC()}
	f := &fakeSecretFetcher{values: []string{"cred-A", "cred-B"}} // no vault TTL; policy governs
	r := newSecretTestResolver(clock, f, discardLogger())

	tgt1 := secretTestTarget()
	tgt1.MaxCacheTTL = time.Hour
	tgt2 := secretTestTarget()
	tgt2.MaxCacheTTL = 30 * time.Minute // same source, different cache policy

	if _, err := r.Resolve(context.Background(), tgt1); err != nil {
		t.Fatalf("Resolve ttl=1h: %v", err)
	}
	if _, err := r.Resolve(context.Background(), tgt2); err != nil {
		t.Fatalf("Resolve ttl=30m: %v", err)
	}
	if got := f.callCount(); got != 2 {
		t.Errorf("fetcher called %d times, want 2 (a changed max_cache_ttl must not reuse the prior entry)", got)
	}
}
