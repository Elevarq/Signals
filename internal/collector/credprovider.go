package collector

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/elevarq/arq-signals/internal/config"
)

// CredKind classifies how a resolved Credential is applied to a
// connection. The credential-providers keystone (#93) currently models
// every provider as producing a password-kind credential (the value is
// placed in ConnConfig.Password); the kind exists so future providers
// (e.g. mTLS) can carry non-password material without changing the
// resolver contract.
type CredKind int

const (
	// CredKindPassword is a credential applied as ConnConfig.Password.
	// Both the default password provider and aws_rds_iam produce this
	// kind — for AWS the password value is the short-lived IAM token.
	CredKindPassword CredKind = iota
)

// Credential is the resolved authentication material for one connection
// attempt. The keystone forbids persisting or logging the secret value
// (INV002/INV007); only the metadata (Kind, ExpiresAt) is loggable.
type Credential struct {
	// Kind selects how Password is applied.
	Kind CredKind
	// Password is the secret value (a stored password, or for
	// aws_rds_iam the minted IAM token). Never logged or persisted.
	Password string
	// ExpiresAt is the credential's expiry. Zero means "no expiry"
	// (a static password); a non-zero value drives cache refresh.
	ExpiresAt time.Time
}

// credentialResolver dispatches a target to its provider based on the
// effective auth_method, returning a Credential for the connection. It
// is the single seam wired into the pgx BeforeConnect hook. The
// password path delegates to ResolvePassword (read fresh every call to
// support rotation); the aws_rds_iam path mints and caches a token.
//
// The clock (now) and AWS dependencies (minter, region) are injected so
// unit tests run deterministically and make no real AWS calls (NFR003).
type credentialResolver struct {
	cache  *tokenCache
	minter rdsTokenMinter
	region func(ctx context.Context, tgt config.TargetConfig) (string, error)
	now    func() time.Time
	logger *slog.Logger
}

// newCredentialResolver builds the production resolver: a real AWS token
// minter, the real region resolver (config → env → IMDS), and the wall
// clock. The AWS SDK is only invoked on the aws_rds_iam path, so targets
// using password auth never require AWS credentials at runtime (NFR001).
func newCredentialResolver(logger *slog.Logger) *credentialResolver {
	if logger == nil {
		logger = slog.Default()
	}
	return &credentialResolver{
		cache:  newTokenCache(),
		minter: awsRDSTokenMinter{},
		region: resolveAWSRegion,
		now:    time.Now,
		logger: logger,
	}
}

// Resolve returns the credential for a single connection attempt,
// dispatching on the target's effective auth_method. An unrecognised
// method cannot reach here — ValidateStrict rejects it at startup
// (keystone FC001) — so the default branch is the password provider.
func (r *credentialResolver) Resolve(ctx context.Context, tgt config.TargetConfig) (Credential, error) {
	switch tgt.EffectiveAuthMethod() {
	case config.AuthMethodAWSRDSIAM:
		return r.resolveAWS(ctx, tgt)
	default:
		password, err := ResolvePassword(tgt)
		if err != nil {
			return Credential{}, redactError(err)
		}
		// Static password: no expiry, never cached (rotation support).
		return Credential{Kind: CredKindPassword, Password: password}, nil
	}
}

// tokenCache holds resolved, expiring credentials keyed per target. The
// keystone (NFR001) requires the cache to be per-target and never shared
// across targets; the key carried by the caller encodes the target's
// connection identity and auth method, so a token minted for one target
// can never be returned for another.
type tokenCache struct {
	mu sync.Mutex
	m  map[string]cacheEntry
}

type cacheEntry struct {
	cred Credential
	// refreshAt is the instant at which the cached credential must be
	// re-minted — the expiry minus the refresh skew. A cached entry is
	// reusable only while now < refreshAt, so a token is never knowingly
	// presented inside its skew window or after expiry (FC-AWS-002).
	refreshAt time.Time
}

func newTokenCache() *tokenCache {
	return &tokenCache{m: make(map[string]cacheEntry)}
}

// get returns the cached credential for key when it is still outside its
// refresh skew at now; otherwise it reports a miss so the caller re-mints.
func (c *tokenCache) get(key string, now time.Time) (Credential, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok {
		return Credential{}, false
	}
	if !now.Before(e.refreshAt) {
		return Credential{}, false
	}
	return e.cred, true
}

// put stores cred under key with the computed refresh instant.
func (c *tokenCache) put(key string, cred Credential, refreshAt time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = cacheEntry{cred: cred, refreshAt: refreshAt}
}

// refreshSkew is the lead time before expiry at which a cached token is
// re-minted: max(60s, min(5m, ttl*0.20)). For the 15-minute RDS IAM
// token this is 3 minutes, so a cached token is reused for ~12 minutes
// then re-minted (keystone NFR001 / ARQ-SIGNALS-AUTH-AWS-INV003).
func refreshSkew(ttl time.Duration) time.Duration {
	skew := ttl / 5
	if skew > 5*time.Minute {
		skew = 5 * time.Minute
	}
	if skew < 60*time.Second {
		skew = 60 * time.Second
	}
	return skew
}
