package collector

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ssm"

	"github.com/elevarq/signals/internal/config"
)

// ssmGetParameterAPI is the minimal AWS Systems Manager surface the fetcher
// uses. Narrowing it to one method lets a unit test inject a fake and assert
// the call contract (notably WithDecryption=true) with no real AWS call
// (SIGNALS-AUTH-SECRET-NFR003).
type ssmGetParameterAPI interface {
	GetParameter(ctx context.Context, in *ssm.GetParameterInput, optFns ...func(*ssm.Options)) (*ssm.GetParameterOutput, error)
}

// awsParameterStoreFetcher is the production secretFetcher for AWS Systems
// Manager Parameter Store. Like the Secrets Manager fetcher it authenticates
// with the collector's ambient AWS workload identity but pins the endpoint
// region to the value parsed from the ARN — never AWS_REGION, the SDK default
// region chain, or IMDS (SIGNALS-AUTH-SECRET, region-from-ARN decision).
//
// newClient is the SSM client constructor; it is overridable so tests inject
// a fake. When nil, the production constructor (loadSSMClient) is used.
type awsParameterStoreFetcher struct {
	newClient func(ctx context.Context, region string) (ssmGetParameterAPI, error)
}

// Fetch retrieves the parameter value for ref. GetParameter is always called
// with WithDecryption=true, so a SecureString is returned decrypted and a
// plain String passes through unchanged; both yield a string value. Parameter
// Store supplies no lease/TTL, so the returned ttl is always zero — reuse
// between reconnects is therefore governed entirely by the operator's
// max_cache_ttl (SIGNALS-AUTH-SECRET-INV003).
func (f awsParameterStoreFetcher) Fetch(ctx context.Context, ref config.ParsedSecretRef) (string, time.Duration, error) {
	if ref.AWSRegion == "" {
		// Defensive: InferSecretBackend guarantees a non-empty region for an
		// AWS ref, and startup validation rejects empty-region ARNs.
		return "", 0, errors.New("AWS Parameter Store reference has no region")
	}

	newClient := f.newClient
	if newClient == nil {
		newClient = loadSSMClient
	}
	client, err := newClient(ctx, ref.AWSRegion)
	if err != nil {
		return "", 0, fmt.Errorf("load AWS config: %w", err)
	}

	out, err := client.GetParameter(ctx, &ssm.GetParameterInput{
		Name:           &ref.Ref,
		WithDecryption: aws.Bool(true),
	})
	if err != nil {
		return "", 0, err
	}
	if out.Parameter == nil || out.Parameter.Value == nil {
		// Do not echo any payload; the parameter simply yielded no usable value.
		return "", 0, errors.New("parameter has no value")
	}
	return *out.Parameter.Value, 0, nil
}

// loadSSMClient builds a region-pinned SSM client using the ambient AWS
// workload identity. The region comes from the ARN (ref.AWSRegion), never the
// environment.
func loadSSMClient(ctx context.Context, region string) (ssmGetParameterAPI, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, err
	}
	return ssm.NewFromConfig(cfg), nil
}
