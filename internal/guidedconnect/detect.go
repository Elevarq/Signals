// Package guidedconnect implements the orchestration behind
// `signalsctl connect --auto` (#99): it auto-detects the cloud platform and
// ambient identity, proposes an auth_method, resolves the credential,
// runs the existing connection diagnostic, validates the role is
// read-only, and renders either a ready-to-use (secret-free) target
// config block or an actionable, copy-pasteable fix.
//
// The package is an orchestrator only. Credential resolution reuses the
// providers via collector.CredentialResolver, the connection test reuses
// conntest, and the role check reuses collector.ValidateRoleSafety — it
// introduces no new connection, credential, or safety behavior
// (SIGNALS-CONNECT-INV003).
//
// Spec: features/signals/guided-connect.md (ACTIVE).
package guidedconnect

import (
	"strings"

	"github.com/elevarq/signals/internal/config"
)

// Cloud identifies a cloud platform for detection purposes.
type Cloud string

const (
	CloudAWS   Cloud = "aws"
	CloudAzure Cloud = "azure"
	CloudGCP   Cloud = "gcp"
)

// DetectInput carries everything detection needs. Getenv is injected so
// unit tests drive detection from fixtures and the function makes no
// network call (NFR001) and reads only process environment.
type DetectInput struct {
	// Host is the target host (drives host-pattern matching).
	Host string
	// SecretRef is the --secret-ref value, if supplied.
	SecretRef string
	// HasCert is true when --sslcert/--sslkey were supplied.
	HasCert bool
	// Getenv reads an environment variable. nil is treated as os.Getenv
	// by the orchestrator before calling Detect.
	Getenv func(string) string
}

// Detection is the outcome of cloud-identity + host-pattern detection.
type Detection struct {
	// Method is the proposed auth_method (a config.AuthMethod* value).
	Method string
	// Ambiguous is true when more than one cloud identity was found and
	// the host pattern did not disambiguate (CONNECT-FC001); the caller
	// must not guess and must report Notes.
	Ambiguous bool
	// Identities lists the ambient cloud identities found via environment.
	Identities []Cloud
	// HostCloud is the cloud implied by the host pattern ("" if none).
	HostCloud Cloud
	// Notes explains what was and was not detected, for operator output.
	Notes []string
}

// hasIdentity reports whether c is among the detected identities.
func (d Detection) hasIdentity(c Cloud) bool {
	for _, id := range d.Identities {
		if id == c {
			return true
		}
	}
	return false
}

// Detect proposes an auth_method from the ambient environment and the
// host pattern, honouring the resolved design decision that a cloud
// method is proposed only when BOTH an ambient cloud identity AND a
// matching host pattern are present (guided-connect.md, decision 3).
//
// Precedence:
//   - explicit --sslcert/--sslkey   -> mtls
//   - explicit --secret-ref         -> secret_store
//   - cloud identity AND host match -> that cloud's method
//   - >1 identity, host disambiguates none -> Ambiguous (FC001)
//   - otherwise                     -> password (with explanatory Notes)
//
// Detection is advisory; the caller's --auth-method always overrides it.
func Detect(in DetectInput) Detection {
	getenv := in.Getenv
	if getenv == nil {
		getenv = func(string) string { return "" }
	}

	var d Detection

	// Explicit method-bearing flags win — they are unambiguous operator
	// intent (guided-connect.md detection table rows 4 and 5).
	if in.HasCert {
		d.Method = config.AuthMethodMTLS
		d.Notes = append(d.Notes, "client certificate flags supplied -> mtls")
		return d
	}
	if in.SecretRef != "" {
		d.Method = config.AuthMethodSecretStore
		d.Notes = append(d.Notes, "secret reference supplied -> secret_store")
		return d
	}

	// Ambient cloud identities (environment only; no network probe).
	if awsIdentity(getenv) {
		d.Identities = append(d.Identities, CloudAWS)
	}
	if azureIdentity(getenv) {
		d.Identities = append(d.Identities, CloudAzure)
	}
	if gcpIdentity(getenv) {
		d.Identities = append(d.Identities, CloudGCP)
	}

	// Host pattern.
	d.HostCloud = hostCloud(in.Host)

	// A cloud method is proposed only when identity AND host agree.
	if d.HostCloud != "" && d.hasIdentity(d.HostCloud) {
		d.Method = cloudMethod(d.HostCloud)
		d.Notes = append(d.Notes, identityNote(d.HostCloud)+" and a matching "+string(d.HostCloud)+" host -> "+d.Method)
		return d
	}

	// No agreeing cloud pair. If more than one ambient identity is present
	// and the host did not pick one, refuse to guess (FC001).
	if len(d.Identities) > 1 {
		d.Ambiguous = true
		ids := make([]string, len(d.Identities))
		for i, c := range d.Identities {
			ids[i] = string(c)
		}
		d.Notes = append(d.Notes,
			"multiple cloud identities detected ("+strings.Join(ids, ", ")+") and the host pattern matched none; "+
				"pass --auth-method to disambiguate")
		return d
	}

	// Otherwise fall back to password and report what was and was not seen.
	d.Method = config.AuthMethodPassword
	switch {
	case len(d.Identities) == 1 && d.HostCloud == "":
		d.Notes = append(d.Notes, identityNote(d.Identities[0])+" but the host is not a recognised "+
			string(d.Identities[0])+" endpoint; pass --auth-method "+cloudMethod(d.Identities[0])+" if it is one")
	case len(d.Identities) == 1 && d.HostCloud != d.Identities[0]:
		d.Notes = append(d.Notes, identityNote(d.Identities[0])+" but the host looks like a "+
			string(d.HostCloud)+" endpoint; pass --auth-method to override")
	case len(d.Identities) == 0 && d.HostCloud != "":
		d.Notes = append(d.Notes, "the host looks like a "+string(d.HostCloud)+" endpoint but no "+
			string(d.HostCloud)+" identity was detected; configure cloud credentials or pass --auth-method "+cloudMethod(d.HostCloud))
	default:
		d.Notes = append(d.Notes, "no cloud identity or recognised cloud host detected -> password")
	}
	return d
}

// awsIdentity reports an ambient AWS identity from environment signals:
// static keys, a named profile, IRSA (web-identity), or ECS container
// credentials. Pure instance-profile (IMDS-only) environments set none of
// these; there detection proposes password and the operator passes
// --auth-method aws_rds_iam (detection is advisory).
func awsIdentity(getenv func(string) string) bool {
	for _, k := range []string{
		"AWS_ACCESS_KEY_ID",
		"AWS_PROFILE",
		"AWS_ROLE_ARN",
		"AWS_WEB_IDENTITY_TOKEN_FILE",
		"AWS_CONTAINER_CREDENTIALS_RELATIVE_URI",
		"AWS_CONTAINER_CREDENTIALS_FULL_URI",
	} {
		if getenv(k) != "" {
			return true
		}
	}
	return false
}

// azureIdentity reports an ambient Azure identity: an explicit client/tenant,
// workload-identity federation, or an App Service / Functions managed-identity
// endpoint.
func azureIdentity(getenv func(string) string) bool {
	for _, k := range []string{
		"AZURE_CLIENT_ID",
		"AZURE_TENANT_ID",
		"AZURE_FEDERATED_TOKEN_FILE",
		"MSI_ENDPOINT",
		"IDENTITY_ENDPOINT",
	} {
		if getenv(k) != "" {
			return true
		}
	}
	return false
}

// gcpIdentity reports an ambient GCP identity: Application Default
// Credentials, a configured project, a metadata-host override, or a
// Cloud Run service.
func gcpIdentity(getenv func(string) string) bool {
	for _, k := range []string{
		"GOOGLE_APPLICATION_CREDENTIALS",
		"GOOGLE_CLOUD_PROJECT",
		"GCP_PROJECT",
		"CLOUDSDK_CORE_PROJECT",
		"GCE_METADATA_HOST",
		"K_SERVICE",
	} {
		if getenv(k) != "" {
			return true
		}
	}
	return false
}

// hostCloud maps a host to the cloud it belongs to by well-known pattern,
// or "" when none matches.
func hostCloud(host string) Cloud {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "" {
		return ""
	}
	switch {
	case strings.HasSuffix(h, ".rds.amazonaws.com"):
		return CloudAWS
	case strings.HasSuffix(h, ".postgres.database.azure.com"):
		return CloudAzure
	case strings.HasSuffix(h, ".cloudsql.goog"), isCloudSQLConnName(h):
		return CloudGCP
	}
	return ""
}

// isCloudSQLConnName reports whether h is a Cloud SQL instance connection
// name of the form project:region:instance.
func isCloudSQLConnName(h string) bool {
	parts := strings.Split(h, ":")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
	}
	return true
}

func cloudMethod(c Cloud) string {
	switch c {
	case CloudAWS:
		return config.AuthMethodAWSRDSIAM
	case CloudAzure:
		return config.AuthMethodAzureEntra
	case CloudGCP:
		return config.AuthMethodGCPCloudSQLIAM
	}
	return config.AuthMethodPassword
}

func identityNote(c Cloud) string {
	switch c {
	case CloudAWS:
		return "detected an AWS identity"
	case CloudAzure:
		return "detected an Azure identity"
	case CloudGCP:
		return "detected a GCP identity"
	}
	return "detected an identity"
}
