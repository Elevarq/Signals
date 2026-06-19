package collector

import (
	"context"
	"errors"
	"fmt"
	"time"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"

	"github.com/elevarq/signals/internal/config"
)

// gcpSecretManagerFetcher is the production secretFetcher for GCP Secret
// Manager. It authenticates with the collector's ambient Google workload
// identity via Application Default Credentials (ADC) — Workload Identity on
// GKE, the attached service account on GCE — matching the spec's integration
// mapping. No service-account key is ever read from config (INV001).
type gcpSecretManagerFetcher struct{}

// Fetch retrieves the secret payload for ref from GCP Secret Manager. Secret
// Manager supplies no lease/TTL for a stored secret version, so the returned
// ttl is always zero — reuse between reconnects is governed entirely by the
// operator's max_cache_ttl (INV003), exactly as for AWS Secrets Manager.
func (gcpSecretManagerFetcher) Fetch(ctx context.Context, ref config.ParsedSecretRef) (string, time.Duration, error) {
	client, err := secretmanager.NewClient(ctx)
	if err != nil {
		return "", 0, fmt.Errorf("create Secret Manager client: %w", err)
	}
	defer func() { _ = client.Close() }()

	// ref.Ref is the full resource name, including the version segment
	// (projects/<proj>/secrets/<name>/versions/<version|latest>), so it is
	// passed straight through (FC-SECRET-007 guarantees the shape at startup).
	resp, err := client.AccessSecretVersion(ctx, &secretmanagerpb.AccessSecretVersionRequest{
		Name: ref.Ref,
	})
	if err != nil {
		return "", 0, err
	}
	if resp.GetPayload() == nil || resp.GetPayload().GetData() == nil {
		// No payload is not a usable DB password; do not echo any material.
		return "", 0, errors.New("secret manager version has no payload")
	}
	return string(resp.GetPayload().GetData()), 0, nil
}
