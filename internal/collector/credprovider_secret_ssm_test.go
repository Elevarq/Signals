package collector

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"

	"github.com/elevarq/signals/internal/config"
)

// fakeSSMClient records the GetParameter input it was handed and returns a
// canned parameter value, so the fetcher's contract (WithDecryption=true,
// value applied verbatim) is asserted with no real AWS call (NFR003).
type fakeSSMClient struct {
	gotInput *ssm.GetParameterInput
	value    string
	noValue  bool // simulate a parameter with no Value
	err      error
}

func (c *fakeSSMClient) GetParameter(ctx context.Context, in *ssm.GetParameterInput, _ ...func(*ssm.Options)) (*ssm.GetParameterOutput, error) {
	c.gotInput = in
	if c.err != nil {
		return nil, c.err
	}
	if c.noValue {
		return &ssm.GetParameterOutput{Parameter: &ssmtypes.Parameter{}}, nil
	}
	return &ssm.GetParameterOutput{Parameter: &ssmtypes.Parameter{Value: aws.String(c.value)}}, nil
}

func parameterStoreParsedRef() config.ParsedSecretRef {
	parsed, err := config.InferSecretBackend(testAWSParameterStoreRef)
	if err != nil {
		panic(err)
	}
	return parsed
}

// AC-SECRET-002a — the Parameter Store fetcher calls GetParameter with
// WithDecryption=true so a SecureString is returned decrypted (and a plain
// String passes through); the decrypted value is applied verbatim and the
// returned ttl is zero (Parameter Store supplies no lease).
func TestAWSParameterStoreFetcherDecryptsAndAppliesValue(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
	}{
		{"securestring decrypted", "decrypted-secure-pw"},
		{"plain string passthrough", "plain-string-pw"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeSSMClient{value: tc.value}
			f := awsParameterStoreFetcher{newClient: func(context.Context, string) (ssmGetParameterAPI, error) {
				return client, nil
			}}

			value, ttl, err := f.Fetch(context.Background(), parameterStoreParsedRef())
			if err != nil {
				t.Fatalf("Fetch: %v", err)
			}
			if value != tc.value {
				t.Errorf("value = %q, want the parameter value %q", value, tc.value)
			}
			if ttl != 0 {
				t.Errorf("ttl = %v, want 0 (Parameter Store supplies no lease)", ttl)
			}
			if client.gotInput == nil {
				t.Fatal("GetParameter was not called")
			}
			if client.gotInput.WithDecryption == nil || !*client.gotInput.WithDecryption {
				t.Errorf("WithDecryption must be true so SecureString parameters are decrypted")
			}
			if client.gotInput.Name == nil || *client.gotInput.Name != testAWSParameterStoreRef {
				t.Errorf("Name = %v, want the parameter ARN %q", client.gotInput.Name, testAWSParameterStoreRef)
			}
		})
	}
}

// AC-SECRET-002a (boundary) — a parameter with no value yields an error
// rather than an empty password, and the error carries no secret material.
func TestAWSParameterStoreFetcherNoValueFails(t *testing.T) {
	client := &fakeSSMClient{noValue: true}
	f := awsParameterStoreFetcher{newClient: func(context.Context, string) (ssmGetParameterAPI, error) {
		return client, nil
	}}
	if _, _, err := f.Fetch(context.Background(), parameterStoreParsedRef()); err == nil {
		t.Fatal("expected an error for a parameter with no value, got nil")
	}
}

// A fetch error propagates so the resolver can wrap it with the redacted,
// actionable hint (FC-SECRET-001).
func TestAWSParameterStoreFetcherPropagatesError(t *testing.T) {
	client := &fakeSSMClient{err: errors.New("AccessDeniedException: ssm:GetParameter denied")}
	f := awsParameterStoreFetcher{newClient: func(context.Context, string) (ssmGetParameterAPI, error) {
		return client, nil
	}}
	if _, _, err := f.Fetch(context.Background(), parameterStoreParsedRef()); err == nil {
		t.Fatal("expected the SDK error to propagate, got nil")
	}
}

// AC-SECRET-012 — operator guidance for a Parameter Store target names the
// exact IAM grant (ssm:GetParameter + kms:Decrypt) and the target.
func TestSecretStoreGuidanceParameterStore(t *testing.T) {
	tgt := secretTestTarget()
	tgt.SecretRef = testAWSParameterStoreRef
	g := SecretStoreGuidance(tgt)
	if !strings.Contains(g, "ssm:GetParameter") {
		t.Errorf("guidance should name ssm:GetParameter; got: %s", g)
	}
	if !strings.Contains(g, "kms:Decrypt") {
		t.Errorf("guidance should name kms:Decrypt for SecureString; got: %s", g)
	}
	if !strings.Contains(g, "s1") {
		t.Errorf("guidance should name the target; got: %s", g)
	}
}
