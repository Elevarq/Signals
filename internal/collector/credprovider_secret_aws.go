package collector

import (
	"context"
	"errors"
	"fmt"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"

	"github.com/elevarq/arq-signals/internal/config"
)

// awsSecretsManagerFetcher is the production secretFetcher for AWS Secrets
// Manager. It authenticates with the collector's ambient AWS workload
// identity (instance profile / IRSA / Pod Identity, via the SDK default
// credential chain) but pins the endpoint region to the value parsed from
// the ARN — never AWS_REGION, the SDK default region chain, or IMDS
// (ARQ-SIGNALS-AUTH-SECRET, region-from-ARN decision).
type awsSecretsManagerFetcher struct{}

// Fetch retrieves the secret payload for ref. AWS Secrets Manager supplies
// no lease/TTL, so the returned ttl is always zero — reuse between
// reconnects is therefore governed entirely by the operator's max_cache_ttl
// (INV003).
func (awsSecretsManagerFetcher) Fetch(ctx context.Context, ref config.ParsedSecretRef) (string, time.Duration, error) {
	if ref.AWSRegion == "" {
		// Defensive: InferSecretBackend guarantees a non-empty region for an
		// AWS ref, and startup validation rejects empty-region ARNs.
		return "", 0, errors.New("AWS Secrets Manager reference has no region")
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(ref.AWSRegion))
	if err != nil {
		return "", 0, fmt.Errorf("load AWS config: %w", err)
	}

	client := secretsmanager.NewFromConfig(cfg)
	out, err := client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: &ref.Ref,
	})
	if err != nil {
		return "", 0, err
	}
	if out.SecretString == nil {
		// Binary secrets are not a supported shape for a DB password; the raw
		// value (or its JSON key) must be a string. Do not echo any payload.
		return "", 0, errors.New("secret has no string value (binary secrets are not supported for database passwords)")
	}
	return *out.SecretString, 0, nil
}
