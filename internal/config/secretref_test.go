package config

import (
	"strings"
	"testing"
)

// AC-SECRET-002 (boundary — backend inference) — each accepted reference
// shape routes to exactly one backend, and the AWS ARN's region segment is
// parsed out for endpoint pinning. Inference is total and deterministic.
func TestInferSecretBackendShapes(t *testing.T) {
	cases := []struct {
		name       string
		ref        string
		wantBe     SecretBackend
		wantRegion string
	}{
		{
			name:       "aws secrets manager arn",
			ref:        "arn:aws:secretsmanager:eu-west-1:123456789012:secret:prod/pg/monitor-AbCdEf",
			wantBe:     SecretBackendAWSSecretsManager,
			wantRegion: "eu-west-1",
		},
		{
			name:       "aws parameter store arn",
			ref:        "arn:aws:ssm:eu-west-1:123456789012:parameter/prod/pg/monitor",
			wantBe:     SecretBackendAWSParameterStore,
			wantRegion: "eu-west-1",
		},
		{
			name:       "aws parameter store arn, single-segment name",
			ref:        "arn:aws:ssm:us-east-2:123456789012:parameter/monitor-pw",
			wantBe:     SecretBackendAWSParameterStore,
			wantRegion: "us-east-2",
		},
		{
			name:   "azure key vault secret uri",
			ref:    "https://my-vault.vault.azure.net/secrets/pg-monitor",
			wantBe: SecretBackendAzureKeyVault,
		},
		{
			name:   "azure key vault secret uri with version",
			ref:    "https://my-vault.vault.azure.net/secrets/pg-monitor/abc123def456",
			wantBe: SecretBackendAzureKeyVault,
		},
		{
			name:   "gcp secret manager resource (versioned)",
			ref:    "projects/my-proj/secrets/pg-monitor/versions/3",
			wantBe: SecretBackendGCPSecretManager,
		},
		{
			name:   "gcp secret manager resource (latest)",
			ref:    "projects/my-proj/secrets/pg-monitor/versions/latest",
			wantBe: SecretBackendGCPSecretManager,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := InferSecretBackend(tc.ref)
			if err != nil {
				t.Fatalf("InferSecretBackend(%q): unexpected error %v", tc.ref, err)
			}
			if parsed.Backend != tc.wantBe {
				t.Errorf("Backend = %v, want %v", parsed.Backend, tc.wantBe)
			}
			if parsed.Ref != tc.ref {
				t.Errorf("Ref = %q, want the original reference %q", parsed.Ref, tc.ref)
			}
			if tc.wantRegion != "" && parsed.AWSRegion != tc.wantRegion {
				t.Errorf("AWSRegion = %q, want %q (pinned from the ARN, never ambient)", parsed.AWSRegion, tc.wantRegion)
			}
			isAWS := tc.wantBe == SecretBackendAWSSecretsManager || tc.wantBe == SecretBackendAWSParameterStore
			if !isAWS && parsed.AWSRegion != "" {
				t.Errorf("non-AWS backend should carry no region, got %q", parsed.AWSRegion)
			}
		})
	}
}

// AC-SECRET-002 / FC-SECRET-007 — an unrecognised shape, or an AWS ARN with
// an empty region segment, is rejected with an error naming the three
// accepted forms (the region rule is part of the AWS shape contract).
func TestInferSecretBackendRejectsUnrecognised(t *testing.T) {
	bad := []string{
		"",
		"monitor-password",
		"arn:aws:s3:::my-bucket/key", // wrong AWS service
		"arn:aws:secretsmanager::123456789012:secret:prod/pg/monitor", // empty region segment
		"arn:aws:ssm::123456789012:parameter/prod/pg/monitor",         // ssm: empty region segment
		"arn:aws:ssm:eu-west-1:123456789012:document/foo",             // ssm: not a parameter resource
		"https://example.com/secrets/pg-monitor",                      // not a Key Vault host
		"projects/my-proj/secrets/pg-monitor",                         // missing /versions/
		"vault://kv/pg-monitor",                                       // unknown scheme
	}
	for _, ref := range bad {
		t.Run(ref, func(t *testing.T) {
			_, err := InferSecretBackend(ref)
			if err == nil {
				t.Fatalf("InferSecretBackend(%q): expected a hard error, got nil", ref)
			}
			// The error must guide the operator to the accepted forms.
			low := strings.ToLower(err.Error())
			for _, want := range []string{"arn", "vault", "projects"} {
				if !strings.Contains(low, want) {
					t.Errorf("error should name the accepted forms (missing %q); got: %v", want, err)
				}
			}
		})
	}
}
