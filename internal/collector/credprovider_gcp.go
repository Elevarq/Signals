package collector

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/oauth2/google"
	"google.golang.org/api/impersonate"

	"github.com/elevarq/arq-signals/internal/config"
)

// gcpScope is the fixed OAuth2 scope for Cloud SQL IAM database
// authentication. It is hard-pinned and MUST NOT be widened or made
// operator-configurable (ARQ-SIGNALS-AUTH-GCP-INV004).
const gcpScope = "https://www.googleapis.com/auth/sqlservice.login"

// gcpTokenTTLFallback is used only if the token source returns no usable
// expiry. Google OAuth2 access tokens are normally valid ~60 minutes and
// carry their own Expiry, which the cache uses directly.
const gcpTokenTTLFallback = 60 * time.Minute

// gcpFailureHint is appended to every gcp_cloudsql_iam resolution failure.
// It covers both connect-time failure modes (FC-GCP-001 / FC-GCP-005)
// without leaking any secret: the identity could not be resolved (or the
// impersonation was denied), or the DB role is not an IAM database user.
const gcpFailureHint = "ensure the collector has a usable Google identity " +
	"(Application Default Credentials, or set gcp_impersonate_service_account " +
	"to a service account it may impersonate) and that the database role is a " +
	"Cloud SQL IAM database user (see gcloud sql users create)"

// gcpTokenMinter is the seam between the resolver and Google. The
// production implementation builds a token source from Application Default
// Credentials (optionally impersonating a service account) and requests a
// token for gcpScope; unit tests inject a fake so no test makes a real GCP
// call (ARQ-SIGNALS-AUTH-GCP-NFR003). impersonate names the service account
// to impersonate ("" = use the ambient ADC identity directly).
type gcpTokenMinter interface {
	Mint(ctx context.Context, scope, impersonate string) (token string, expiresAt time.Time, err error)
}

// resolveGCP acquires (or reuses a cached) Cloud SQL IAM access token for
// the target and returns it as a password-kind credential. The token is the
// connection password; the target carries no stored secret (INV001).
func (r *credentialResolver) resolveGCP(ctx context.Context, tgt config.TargetConfig) (Credential, error) {
	now := r.now()
	key := gcpCacheKey(tgt)
	if cred, ok := r.cache.get(key, now); ok {
		return cred, nil
	}

	impersonate := resolveGCPImpersonation(tgt)

	token, expiresAt, err := r.gcpMinter.Mint(ctx, gcpScope, impersonate)
	if err != nil {
		// FC-GCP-001 / FC-GCP-005: redact (the SDK error may embed request
		// detail), attribute the failure to the method, and append an
		// actionable hint covering both identity/impersonation and IAM-user
		// causes. The token never exists on this path, so nothing leaks.
		return Credential{}, fmt.Errorf("target %s: %s: acquiring Cloud SQL IAM access token failed: %w; %s",
			tgt.Name, config.AuthMethodGCPCloudSQLIAM, redactError(err), gcpFailureHint)
	}
	if expiresAt.IsZero() {
		expiresAt = now.Add(gcpTokenTTLFallback)
	}

	cred := Credential{Kind: CredKindPassword, Password: token, ExpiresAt: expiresAt}
	r.cache.put(key, cred, expiresAt.Add(-refreshSkew(expiresAt.Sub(now))))

	// INV002/INV007: log metadata only — never the token value. The scope
	// and service-account email are non-secret public identifiers.
	r.logger.Info("resolved gcp_cloudsql_iam credential",
		"auth_method", config.AuthMethodGCPCloudSQLIAM,
		"target", tgt.Name,
		"scope", gcpScope,
		"db_user", tgt.User,
		"impersonate_set", impersonate != "",
		"resolved_at", now,
		"expires_at", expiresAt,
	)

	return cred, nil
}

// gcpCacheKey is the per-target cache key: the connection identity
// (host:port/dbname@user) plus the auth method. Distinct targets — and
// distinct hosts/users — produce distinct keys, so a token is never shared
// across targets (NFR001).
func gcpCacheKey(tgt config.TargetConfig) string {
	return tgt.ConnIdentity() + "|" + config.AuthMethodGCPCloudSQLIAM
}

// resolveGCPImpersonation returns the service account the collector should
// impersonate, or "" to use the ambient ADC identity directly.
func resolveGCPImpersonation(tgt config.TargetConfig) string {
	return tgt.GCPImpersonateServiceAccount
}

// gcpADCTokenMinter is the production gcpTokenMinter. Without an
// impersonation target it mints tokens from Application Default Credentials
// directly; with one it builds an impersonated token source for that
// service account — the confirmed design for #96.
type gcpADCTokenMinter struct{}

func (gcpADCTokenMinter) Mint(ctx context.Context, scope, impersonateSA string) (string, time.Time, error) {
	if impersonateSA != "" {
		// Build an impersonated token source: the ambient ADC identity calls
		// the IAM Credentials API to mint a scoped token for the target
		// service account (which must grant it the Token Creator role).
		ts, err := impersonate.CredentialsTokenSource(ctx, impersonate.CredentialsConfig{
			TargetPrincipal: impersonateSA,
			Scopes:          []string{scope},
		})
		if err != nil {
			return "", time.Time{}, fmt.Errorf("build impersonated token source: %w", err)
		}
		tok, err := ts.Token()
		if err != nil {
			return "", time.Time{}, err
		}
		return tok.AccessToken, tok.Expiry, nil
	}

	creds, err := google.FindDefaultCredentials(ctx, scope)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("find Application Default Credentials: %w", err)
	}
	tok, err := creds.TokenSource.Token()
	if err != nil {
		return "", time.Time{}, err
	}
	return tok.AccessToken, tok.Expiry, nil
}

// GCPCloudSQLGuidance returns the operator remediation text for a
// gcp_cloudsql_iam target whose DB role is not a Cloud SQL IAM database user
// (AC-GCP-009). It contains the exact gcloud create call and no secret
// material.
func GCPCloudSQLGuidance(tgt config.TargetConfig) string {
	return fmt.Sprintf(`gcp_cloudsql_iam connection for target %q was rejected.
Verify both halves of Cloud SQL IAM authentication:
  1. Register the database role as a Cloud SQL IAM database user:
       gcloud sql users create %s --instance=<INSTANCE> --type=cloud_iam_service_account
     (use --type=cloud_iam_user for a human IAM user). The PostgreSQL role
     name must match the IAM principal with the trailing domain stripped.
  2. Ensure the collector runs under a Google identity (Application Default
     Credentials, workload identity, or an impersonated service account via
     gcp_impersonate_service_account) that has the cloudsql.instances.connect
     permission and the Cloud SQL Instance User role.`,
		tgt.Name, tgt.User)
}
