package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/elevarq/arq-signals/internal/config"
)

// secretFetcher is the seam between the resolver and a cloud secret store.
// The production implementation (productionSecretFetcher) routes to the SDK
// for the inferred backend; unit tests inject a fake so no test makes a real
// cloud call (ARQ-SIGNALS-AUTH-SECRET-NFR003). ttl is the vault-supplied
// lease duration, or 0 when the backend supplies none (e.g. AWS Secrets
// Manager). The returned value is the raw payload — JSON-key extraction
// happens in the resolver, not the fetcher.
type secretFetcher interface {
	Fetch(ctx context.Context, ref config.ParsedSecretRef) (value string, ttl time.Duration, err error)
}

// errSecretBackendUnavailable marks a backend that is recognised by the
// reference shape (so it passes startup validation, per the spec) but whose
// production SDK wiring is absent from the fetcher it was handed. All three
// backends are wired in the production construction (#97 AWS, #108 Azure Key
// Vault + GCP Secret Manager); this is a defensive guard for a
// partially-constructed fetcher. The error is surfaced verbatim (not
// redacted) because it carries no secret material.
var errSecretBackendUnavailable = errors.New("secret_store backend is not available in this build")

// resolveSecretStore fetches the target's database password from the cloud
// secret store inferred from secret_ref and returns it as a password-kind
// credential. The target carries no inline password (INV001). A rotated
// secret is picked up on the next reconnect (INV003).
func (r *credentialResolver) resolveSecretStore(ctx context.Context, tgt config.TargetConfig) (Credential, error) {
	parsed, err := config.InferSecretBackend(tgt.SecretRef)
	if err != nil {
		// ValidateStrict rejects bad references at startup (FC-SECRET-007),
		// so this is a defensive guard rather than the normal path.
		return Credential{}, fmt.Errorf("target %s: %s: %w", tgt.Name, config.AuthMethodSecretStore, err)
	}

	now := r.now()
	key := secretCacheKey(tgt)
	if cred, ok := r.cache.get(key, now); ok {
		return cred, nil
	}

	value, ttl, err := r.secretFetcher.Fetch(ctx, parsed)
	if err != nil {
		if errors.Is(err, errSecretBackendUnavailable) {
			// Capability error, not a fetch failure — no secret to redact,
			// surface it directly so the operator sees which backend is
			// unavailable.
			return Credential{}, fmt.Errorf("target %s: %s: %w", tgt.Name, config.AuthMethodSecretStore, err)
		}
		// FC-SECRET-001 / FC-SECRET-002: redact (the SDK error may embed
		// request detail), attribute the failure to the method and backend,
		// and append an actionable hint naming the required IAM permission.
		// No secret exists on this path, so nothing leaks.
		return Credential{}, fmt.Errorf("target %s: %s: fetching secret from %s failed: %w; %s",
			tgt.Name, config.AuthMethodSecretStore, parsed.Backend, redactError(err), secretFailureHint(parsed.Backend))
	}

	password, err := extractSecretPayload(value, tgt.SecretJSONKey)
	if err != nil {
		// FC-SECRET-003 / FC-SECRET-004: the error never includes the raw
		// fetched value (extractSecretPayload guarantees this).
		return Credential{}, fmt.Errorf("target %s: %s: %w", tgt.Name, config.AuthMethodSecretStore, err)
	}

	// INV003: cache bound = min(vault TTL if present, max_cache_ttl if set).
	// With neither set the bound is zero — re-fetch on every reconnect so a
	// rotated secret is always picked up.
	bound := secretCacheBound(ttl, tgt.MaxCacheTTL)
	var expiresAt time.Time
	if bound > 0 {
		expiresAt = now.Add(bound)
	}

	cred := Credential{Kind: CredKindPassword, Password: password, ExpiresAt: expiresAt}
	if bound > 0 {
		// A static secret does not expire mid-connection like a token, so the
		// bound is a freshness cap, not a hard expiry — store it directly with
		// no refresh skew (the skew exists to re-mint tokens before they
		// expire; a secret_store secret has no such failure mode).
		r.cache.put(key, cred, expiresAt)
	}

	// INV002 / INV007: log metadata only — never the secret value. The
	// secret_ref is a non-secret reference and is safe to log.
	r.logger.Info("resolved secret_store credential",
		"auth_method", config.AuthMethodSecretStore,
		"target", tgt.Name,
		"backend", parsed.Backend.String(),
		"secret_ref", parsed.Ref,
		"db_user", tgt.User,
		"resolved_at", now,
		"ttl_present", bound > 0,
		"expires_at", expiresAt,
		"json_key_extracted", tgt.SecretJSONKey != "",
	)

	return cred, nil
}

// secretCacheKey is the per-target cache key: the connection identity
// (host:port/dbname@user) plus the auth method. Distinct targets — and
// distinct hosts/users — produce distinct keys, so a secret is never shared
// across targets (NFR001 / INV003).
func secretCacheKey(tgt config.TargetConfig) string {
	return tgt.ConnIdentity() + "|" + config.AuthMethodSecretStore
}

// secretCacheBound computes the cache reuse bound from the vault-supplied
// TTL and the operator's max_cache_ttl: the minimum of whichever are set, or
// zero when neither is (re-fetch every reconnect). (INV003.)
func secretCacheBound(vaultTTL, maxCacheTTL time.Duration) time.Duration {
	switch {
	case vaultTTL > 0 && maxCacheTTL > 0:
		return min(vaultTTL, maxCacheTTL)
	case vaultTTL > 0:
		return vaultTTL
	case maxCacheTTL > 0:
		return maxCacheTTL
	default:
		return 0
	}
}

// extractSecretPayload turns a fetched secret value into the password. When
// jsonKey is empty the raw value is the password; otherwise the value is
// parsed as a JSON object and the named key's string value is used. Every
// error path is written to NEVER include the raw secret value (FC-SECRET-003
// / FC-SECRET-004 / INV002).
func extractSecretPayload(value, jsonKey string) (string, error) {
	if jsonKey == "" {
		if value == "" {
			return "", errors.New("fetched secret is empty (FC-SECRET-004)")
		}
		return value, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(value), &obj); err != nil {
		return "", fmt.Errorf("secret_json_key %q is set but the fetched secret is not a JSON object (FC-SECRET-003)", jsonKey)
	}
	raw, ok := obj[jsonKey]
	if !ok {
		return "", fmt.Errorf("secret_json_key %q is not present in the fetched JSON secret (FC-SECRET-003)", jsonKey)
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", fmt.Errorf("secret_json_key %q value is not a JSON string (FC-SECRET-003)", jsonKey)
	}
	if s == "" {
		return "", fmt.Errorf("secret_json_key %q value is empty (FC-SECRET-004)", jsonKey)
	}
	return s, nil
}

// secretFailureHint is appended to every secret_store fetch failure. It
// names the IAM permission the inferred backend requires, with no secret
// material (FC-SECRET-001 / AC-SECRET-012).
func secretFailureHint(b config.SecretBackend) string {
	switch b {
	case config.SecretBackendAWSSecretsManager:
		return "ensure the collector's AWS workload identity is allowed secretsmanager:GetSecretValue for this secret and that the ARN region is correct"
	case config.SecretBackendAWSParameterStore:
		return "ensure the collector's AWS workload identity is allowed ssm:GetParameter (and kms:Decrypt on the CMK for a SecureString) for this parameter and that the ARN region is correct"
	case config.SecretBackendAzureKeyVault:
		return "ensure the collector's Azure managed identity has the Key Vault Secrets User role (get) on this vault"
	case config.SecretBackendGCPSecretManager:
		return "ensure the collector's Google workload identity has secretmanager.versions.access on this secret"
	default:
		return "verify the secret reference and the collector's workload identity"
	}
}

// SecretStoreGuidance returns the operator remediation text for a
// secret_store target whose vault fetch failed for permission/identity
// reasons (AC-SECRET-012). It names the exact IAM grant for the inferred
// backend and the workload-identity note, and contains no secret material.
// An unrecognised reference falls back to listing all three grants.
func SecretStoreGuidance(tgt config.TargetConfig) string {
	parsed, err := config.InferSecretBackend(tgt.SecretRef)
	backend := config.SecretBackendUnknown
	if err == nil {
		backend = parsed.Backend
	}
	switch backend {
	case config.SecretBackendAWSSecretsManager:
		return fmt.Sprintf(`secret_store fetch for target %q (AWS Secrets Manager) was rejected.
  1. Grant the collector's AWS workload identity (instance profile / IRSA /
     Pod Identity) the secretsmanager:GetSecretValue action for this secret:
       {"Effect":"Allow","Action":"secretsmanager:GetSecretValue","Resource":%q}
  2. Confirm the ARN region (%s) matches the secret's region — the region is
     taken from the ARN, never from AWS_REGION or instance metadata.`,
			tgt.Name, parsed.Ref, parsed.AWSRegion)
	case config.SecretBackendAWSParameterStore:
		return fmt.Sprintf(`secret_store fetch for target %q (AWS Systems Manager Parameter Store) was rejected.
  1. Grant the collector's AWS workload identity (instance profile / IRSA /
     Pod Identity) the ssm:GetParameter action for this parameter:
       {"Effect":"Allow","Action":"ssm:GetParameter","Resource":%q}
     For a SecureString parameter, also grant kms:Decrypt on the CMK that
     encrypts it.
  2. Confirm the ARN region (%s) matches the parameter's region — the region
     is taken from the ARN, never from AWS_REGION or instance metadata.`,
			tgt.Name, parsed.Ref, parsed.AWSRegion)
	case config.SecretBackendAzureKeyVault:
		return fmt.Sprintf(`secret_store fetch for target %q (Azure Key Vault) was rejected.
  1. Assign the collector's managed identity the "Key Vault Secrets User"
     role (get) on the vault.
  2. Confirm the secret URI is correct: %s`, tgt.Name, parsed.Ref)
	case config.SecretBackendGCPSecretManager:
		return fmt.Sprintf(`secret_store fetch for target %q (GCP Secret Manager) was rejected.
  1. Grant the collector's Google workload identity the
     secretmanager.versions.access permission on this secret.
  2. Confirm the resource name is correct: %s`, tgt.Name, parsed.Ref)
	default:
		return fmt.Sprintf(`secret_store fetch for target %q was rejected, or the secret_ref is unrecognised.
Grant the collector's workload identity the read permission for the backend:
  - AWS Secrets Manager: secretsmanager:GetSecretValue
  - Azure Key Vault: Key Vault Secrets User (get)
  - GCP Secret Manager: secretmanager.versions.access`, tgt.Name)
	}
}

// productionSecretFetcher routes a fetch to the SDK for the inferred
// backend, guaranteeing only that backend's SDK is invoked for a given
// target (INV005 backend isolation). All three backends — AWS Secrets
// Manager (#97), Azure Key Vault and GCP Secret Manager (#108) — are
// production-wired. The nil guards remain as a defensive seam so a
// partially-constructed fetcher reports errSecretBackendUnavailable rather
// than panicking; they are not reached with the production construction.
type productionSecretFetcher struct {
	aws               secretFetcher
	awsParameterStore secretFetcher
	azure             secretFetcher
	gcp               secretFetcher
}

func (f productionSecretFetcher) Fetch(ctx context.Context, ref config.ParsedSecretRef) (string, time.Duration, error) {
	switch ref.Backend {
	case config.SecretBackendAWSSecretsManager:
		if f.aws == nil {
			return "", 0, fmt.Errorf("%w: %s", errSecretBackendUnavailable, ref.Backend)
		}
		return f.aws.Fetch(ctx, ref)
	case config.SecretBackendAWSParameterStore:
		if f.awsParameterStore == nil {
			return "", 0, fmt.Errorf("%w: %s", errSecretBackendUnavailable, ref.Backend)
		}
		return f.awsParameterStore.Fetch(ctx, ref)
	case config.SecretBackendAzureKeyVault:
		if f.azure == nil {
			return "", 0, fmt.Errorf("%w: %s", errSecretBackendUnavailable, ref.Backend)
		}
		return f.azure.Fetch(ctx, ref)
	case config.SecretBackendGCPSecretManager:
		if f.gcp == nil {
			return "", 0, fmt.Errorf("%w: %s", errSecretBackendUnavailable, ref.Backend)
		}
		return f.gcp.Fetch(ctx, ref)
	default:
		return "", 0, fmt.Errorf("%w: unrecognised backend", errSecretBackendUnavailable)
	}
}
