package config

import (
	"fmt"
	"strings"
)

// SecretBackend identifies which vault SDK fetches a secret_store
// secret_ref. The backend is inferred from the reference shape — there is
// no separate provider-selector field (credential-provider-secret-store.md,
// #97, backend-routing decision).
type SecretBackend int

const (
	// SecretBackendUnknown is the zero value; a ref that infers to this is
	// always accompanied by an error.
	SecretBackendUnknown SecretBackend = iota
	SecretBackendAWSSecretsManager
	SecretBackendAzureKeyVault
	SecretBackendGCPSecretManager
)

// String returns the human-readable backend name used in logs, errors, and
// operator guidance.
func (b SecretBackend) String() string {
	switch b {
	case SecretBackendAWSSecretsManager:
		return "AWS Secrets Manager"
	case SecretBackendAzureKeyVault:
		return "Azure Key Vault"
	case SecretBackendGCPSecretManager:
		return "GCP Secret Manager"
	default:
		return "unknown"
	}
}

// ParsedSecretRef is the result of inferring the backend from a secret_ref.
// AWSRegion is populated only for the AWS backend, where the region is
// derived authoritatively from the ARN and never from ambient region
// discovery (ARQ-SIGNALS-AUTH-SECRET, integration-mapping decision).
type ParsedSecretRef struct {
	Backend   SecretBackend
	Ref       string // the original, non-secret reference
	AWSRegion string // AWS only: the region segment of the ARN
}

// secretRefForms is the operator-facing list of accepted reference shapes,
// reused in every FC-SECRET-007 rejection so the error always names the
// three forms.
const secretRefForms = "accepted forms: an AWS Secrets Manager ARN " +
	"(arn:aws:secretsmanager:<region>:<acct>:secret:<name>), " +
	"an Azure Key Vault secret URI " +
	"(https://<vault>.vault.azure.net/secrets/<name>[/<version>]), " +
	"or a GCP Secret Manager resource name " +
	"(projects/<proj>/secrets/<name>/versions/<version|latest>)"

// InferSecretBackend parses a secret_ref and returns the backend plus any
// backend-specific metadata. Inference is total and deterministic: a given
// reference routes to exactly one backend or is rejected with an error
// naming the accepted forms (FC-SECRET-007 / AC-SECRET-002).
func InferSecretBackend(ref string) (ParsedSecretRef, error) {
	switch {
	case strings.HasPrefix(ref, "arn:aws:secretsmanager:"):
		return parseAWSSecretARN(ref)
	case strings.HasPrefix(ref, "https://") && strings.Contains(ref, ".vault.azure.net/secrets/"):
		return ParsedSecretRef{Backend: SecretBackendAzureKeyVault, Ref: ref}, nil
	case strings.HasPrefix(ref, "projects/") &&
		strings.Contains(ref, "/secrets/") &&
		strings.Contains(ref, "/versions/"):
		return ParsedSecretRef{Backend: SecretBackendGCPSecretManager, Ref: ref}, nil
	default:
		return ParsedSecretRef{}, fmt.Errorf("secret_ref %q matches no known vault backend; %s", ref, secretRefForms)
	}
}

// parseAWSSecretARN extracts the region segment from an AWS Secrets Manager
// ARN. The ARN is the single source of truth for the region — it is never
// taken from AWS_REGION, the SDK default chain, or IMDS (integration-mapping
// decision). A missing/empty region segment is FC-SECRET-007.
//
// ARN layout: arn:aws:secretsmanager:<region>:<account>:secret:<name>
func parseAWSSecretARN(ref string) (ParsedSecretRef, error) {
	parts := strings.SplitN(ref, ":", 7)
	// parts: [arn aws secretsmanager region account secret name...]
	if len(parts) < 7 || parts[5] != "secret" {
		return ParsedSecretRef{}, fmt.Errorf("secret_ref %q is not a well-formed AWS Secrets Manager ARN; %s", ref, secretRefForms)
	}
	region := parts[3]
	if region == "" {
		return ParsedSecretRef{}, fmt.Errorf("secret_ref %q has an empty region segment; the region must be present in the ARN (it is never taken from the environment); %s", ref, secretRefForms)
	}
	return ParsedSecretRef{
		Backend:   SecretBackendAWSSecretsManager,
		Ref:       ref,
		AWSRegion: region,
	}, nil
}
