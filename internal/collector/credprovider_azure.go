package collector

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"

	"github.com/elevarq/signals/internal/config"
)

// entraScope is the fixed OAuth2 scope (audience) for Azure Database for
// PostgreSQL — Flexible Server Entra authentication. It is hard-pinned and
// MUST NOT be widened or made operator-configurable
// (SIGNALS-AUTH-AZURE-INV004).
const entraScope = "https://ossrdbms-aad.database.windows.net/.default"

// azureTokenTTLFallback is used only if the token endpoint returns no
// usable expiry. Entra access tokens are normally valid 60–90 minutes and
// carry their own ExpiresOn, which the cache uses directly.
const azureTokenTTLFallback = 60 * time.Minute

// azureFailureHint is appended to every azure_entra resolution failure. It
// covers both connect-time failure modes (FC-AZURE-001 / FC-AZURE-005)
// without leaking any secret: the identity could not be resolved (or is
// ambiguous), or the DB role is not mapped to an Entra principal.
const azureFailureHint = "ensure the collector has a usable Azure identity " +
	"(set azure_client_id or AZURE_CLIENT_ID to disambiguate a user-assigned " +
	"managed identity) and that the database role is mapped to an Entra " +
	"principal (see pgaadauth_create_principal)"

// entraTokenMinter is the seam between the resolver and Azure. The
// production implementation builds an Azure credential and requests a token
// for entraScope; unit tests inject a fake so no test makes a real Azure
// call (SIGNALS-AUTH-AZURE-NFR003). clientID disambiguates a
// user-assigned managed identity ("" = system-assigned / single identity).
type entraTokenMinter interface {
	Mint(ctx context.Context, scope, clientID string) (token string, expiresAt time.Time, err error)
}

// resolveAzure acquires (or reuses a cached) Entra access token for the
// target and returns it as a password-kind credential. The token is the
// connection password; the target carries no stored secret (INV001).
func (r *credentialResolver) resolveAzure(ctx context.Context, tgt config.TargetConfig) (Credential, error) {
	now := r.now()
	key := azureCacheKey(tgt)
	if cred, ok := r.cache.get(key, now); ok {
		return cred, nil
	}

	clientID := resolveAzureClientID(tgt)

	token, expiresAt, err := r.azureMinter.Mint(ctx, entraScope, clientID)
	if err != nil {
		// FC-AZURE-001 / FC-AZURE-005: redact (the SDK error may embed
		// request detail), attribute the failure to the method, and append
		// an actionable hint covering both identity and principal-mapping
		// causes. The token never exists on this path, so nothing leaks.
		return Credential{}, fmt.Errorf("target %s: %s: acquiring Entra access token failed: %w; %s",
			tgt.Name, config.AuthMethodAzureEntra, redactError(err), azureFailureHint)
	}
	if expiresAt.IsZero() {
		expiresAt = now.Add(azureTokenTTLFallback)
	}

	cred := Credential{Kind: CredKindPassword, Password: token, ExpiresAt: expiresAt}
	r.cache.put(key, cred, expiresAt.Add(-refreshSkew(expiresAt.Sub(now))))

	// INV002/INV007: log metadata only — never the token value. The scope
	// and client_id are non-secret (a client id is a public GUID).
	r.logger.Info("resolved azure_entra credential",
		"auth_method", config.AuthMethodAzureEntra,
		"target", tgt.Name,
		"scope", entraScope,
		"db_user", tgt.User,
		"client_id_set", clientID != "",
		"resolved_at", now,
		"expires_at", expiresAt,
	)

	return cred, nil
}

// azureCacheKey is the per-target cache key: the connection identity
// (host:port/dbname@user) plus the auth method. Distinct targets — and
// distinct hosts/users — produce distinct keys, so a token is never shared
// across targets (NFR001).
func azureCacheKey(tgt config.TargetConfig) string {
	return tgt.ConnIdentity() + "|" + config.AuthMethodAzureEntra
}

// resolveAzureClientID resolves the user-assigned managed identity client
// id in the order fixed by the spec: explicit config, then
// AZURE_CLIENT_ID. An empty result lets the credential chain select the
// system-assigned / single identity.
func resolveAzureClientID(tgt config.TargetConfig) string {
	if tgt.AzureClientID != "" {
		return tgt.AzureClientID
	}
	return os.Getenv("AZURE_CLIENT_ID")
}

// azureEntraTokenMinter is the production entraTokenMinter. With a client
// id it uses a managed-identity credential bound to that identity; without
// one it uses the default Azure credential chain (env → workload identity →
// managed identity → Azure CLI), the confirmed design for #95.
type azureEntraTokenMinter struct{}

func (azureEntraTokenMinter) Mint(ctx context.Context, scope, clientID string) (string, time.Time, error) {
	var cred azcore.TokenCredential
	var err error
	if clientID != "" {
		cred, err = azidentity.NewManagedIdentityCredential(&azidentity.ManagedIdentityCredentialOptions{
			ID: azidentity.ClientID(clientID),
		})
	} else {
		cred, err = azidentity.NewDefaultAzureCredential(nil)
	}
	if err != nil {
		return "", time.Time{}, fmt.Errorf("build Azure credential: %w", err)
	}

	tok, err := cred.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{scope}})
	if err != nil {
		return "", time.Time{}, err
	}
	return tok.Token, tok.ExpiresOn, nil
}

// AzureEntraGuidance returns the operator remediation text for an
// azure_entra target whose DB role is not mapped to an Entra principal
// (AC-AZURE-009). It contains the exact create-principal call and the
// display-name match note — and no secret material.
func AzureEntraGuidance(tgt config.TargetConfig) string {
	return fmt.Sprintf(`azure_entra connection for target %q was rejected.
Verify both halves of Entra authentication:
  1. Map the database role to the Entra principal (run as an Entra admin on the server):
       SELECT * FROM pgaadauth_create_principal('%s', false, false);
     The PostgreSQL role name must exactly match the Entra principal's display name.
  2. Ensure the collector runs under an Azure identity (managed identity, workload
     identity, or az login) that corresponds to that principal; for a host with
     multiple user-assigned identities, set azure_client_id (or AZURE_CLIENT_ID).`,
		tgt.Name, tgt.User)
}
