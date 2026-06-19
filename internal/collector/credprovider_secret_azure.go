package collector

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets"

	"github.com/elevarq/signals/internal/config"
)

// azureKeyVaultFetcher is the production secretFetcher for Azure Key Vault.
// It authenticates with the collector's ambient Azure workload identity via
// the default credential chain (managed identity / workload identity), which
// matches the spec's "Managed Identity / default credential" integration
// mapping. No client secret or connection string is ever read from config
// (INV001).
type azureKeyVaultFetcher struct{}

// Fetch retrieves the secret payload for ref from Azure Key Vault. Key Vault
// supplies no lease/TTL for a stored secret, so the returned ttl is always
// zero — reuse between reconnects is governed entirely by the operator's
// max_cache_ttl (INV003), exactly as for AWS Secrets Manager.
func (azureKeyVaultFetcher) Fetch(ctx context.Context, ref config.ParsedSecretRef) (string, time.Duration, error) {
	vaultURL, name, version, err := parseAzureKeyVaultRef(ref.Ref)
	if err != nil {
		return "", 0, err
	}

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return "", 0, fmt.Errorf("acquire Azure credential: %w", err)
	}

	client, err := azsecrets.NewClient(vaultURL, cred, nil)
	if err != nil {
		return "", 0, fmt.Errorf("create Key Vault client: %w", err)
	}

	// version "" requests the current (latest) version.
	resp, err := client.GetSecret(ctx, name, version, nil)
	if err != nil {
		return "", 0, err
	}
	if resp.Value == nil {
		// A secret with no string value is not a usable DB password; do not
		// echo any payload.
		return "", 0, errors.New("key vault secret has no value")
	}
	return *resp.Value, 0, nil
}

// parseAzureKeyVaultRef splits a Key Vault secret URI into the vault URL plus
// the secret name and optional version that azsecrets.GetSecret needs.
//
// URI layout: https://<vault>.vault.azure.net/secrets/<name>[/<version>]
//
// Errors never include the parsed secret name — the name of a secret is
// low-sensitivity, but the convention across this package is that error paths
// carry no fetched material at all (INV002).
func parseAzureKeyVaultRef(ref string) (vaultURL, name, version string, err error) {
	u, err := url.Parse(ref)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", "", "", errors.New("secret_ref is not a valid Azure Key Vault secret URI")
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	// Expect: ["secrets", "<name>"] or ["secrets", "<name>", "<version>"].
	if len(segs) < 2 || segs[0] != "secrets" || segs[1] == "" {
		return "", "", "", errors.New("secret_ref is not a valid Azure Key Vault secret URI (expected /secrets/<name>[/<version>])")
	}
	vaultURL = u.Scheme + "://" + u.Host
	name = segs[1]
	if len(segs) >= 3 {
		version = segs[2]
	}
	return vaultURL, name, version, nil
}
