package tests

import (
	"strings"
	"testing"
)

// #114 — the Helm chart wires passwordless cloud auth. The collector
// only reads auth_method (and region / azure_client_id /
// gcp_impersonate_service_account / secret_ref) from the YAML
// `targets:` block — the env single-target builder in
// internal/config/config.go does NOT populate those fields. So the
// chart must render the target into the mounted ConfigMap, and the
// auth settings must reach that rendered collector config.
//
// These tests assert the rendered manifest stream carries each
// supported auth_method end-to-end, honours the credential-providers
// invariants (FC005 passwordless methods carry no password source;
// FC006 cloud methods use sslmode=verify-full), and exposes the
// per-platform identity hooks (ServiceAccount annotations for
// IRSA / GKE WI / AKS WI, pod labels for AKS workload identity).

// targetHost is the minimal --set that makes the chart render a
// `targets:` block at all (parity with the env builder, which only
// appended a target when SIGNALS_TARGET_HOST was non-empty).
const targetHost = "target.host=db.internal"

func TestHelm_DefaultRenderHasNoTargetsBlock(t *testing.T) {
	// With no target.host the chart must not emit a targets block,
	// exactly as the env builder added no target for an empty host.
	out := renderHelm(t)
	if strings.Contains(out, "targets:") {
		t.Errorf("default render (empty target.host) should emit no targets block:\n%s", out)
	}
}

func TestHelm_TargetRendersIntoConfigMap(t *testing.T) {
	out := renderHelm(t, targetHost)
	for _, want := range []string{"targets:", "name: default", "host: db.internal"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered config missing %q (target not wired into ConfigMap):\n%s", want, out)
		}
	}
}

func TestHelm_PasswordMethodRendersPasswordEnv(t *testing.T) {
	// Default (empty) auth_method is password: the rendered target
	// references the secret-injected PG_PASSWORD via password_env, and
	// no auth_method key is emitted.
	out := renderHelm(t, targetHost, "target.passwordSecretName=pg-cred")
	if !strings.Contains(out, "password_env: PG_PASSWORD") {
		t.Errorf("password method should wire password_env: PG_PASSWORD:\n%s", out)
	}
	if !strings.Contains(out, "secretKeyRef") || !strings.Contains(out, "name: pg-cred") {
		t.Errorf("password method should inject PG_PASSWORD from the named secret:\n%s", out)
	}
	if strings.Contains(out, "auth_method:") {
		t.Errorf("default password method must not emit an auth_method key:\n%s", out)
	}
}

func TestHelm_AWSRDSIAMReachesConfig(t *testing.T) {
	out := renderHelm(t,
		targetHost,
		"target.authMethod=aws_rds_iam",
		"target.sslmode=verify-full",
		"target.region=us-east-1",
	)
	for _, want := range []string{
		"auth_method: aws_rds_iam",
		"sslmode: verify-full",
		"region: us-east-1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("aws_rds_iam config missing %q:\n%s", want, out)
		}
	}
	// FC005: passwordless — no password source may be rendered.
	if strings.Contains(out, "password_env") {
		t.Errorf("aws_rds_iam is passwordless; password_env must not render (FC005):\n%s", out)
	}
}

func TestHelm_AzureEntraReachesConfig(t *testing.T) {
	out := renderHelm(t,
		targetHost,
		"target.authMethod=azure_entra",
		"target.sslmode=verify-full",
		"target.azureClientId=00000000-0000-0000-0000-000000000000",
	)
	for _, want := range []string{
		"auth_method: azure_entra",
		"azure_client_id: 00000000-0000-0000-0000-000000000000",
		"sslmode: verify-full",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("azure_entra config missing %q:\n%s", want, out)
		}
	}
}

func TestHelm_GCPCloudSQLIAMReachesConfig(t *testing.T) {
	out := renderHelm(t,
		targetHost,
		"target.authMethod=gcp_cloudsql_iam",
		"target.sslmode=verify-full",
		"target.gcpImpersonateServiceAccount=collector@my-proj.iam.gserviceaccount.com",
	)
	for _, want := range []string{
		"auth_method: gcp_cloudsql_iam",
		"gcp_impersonate_service_account: collector@my-proj.iam.gserviceaccount.com",
		"sslmode: verify-full",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("gcp_cloudsql_iam config missing %q:\n%s", want, out)
		}
	}
}

func TestHelm_SecretStoreReachesConfig(t *testing.T) {
	out := renderHelm(t,
		targetHost,
		"target.authMethod=secret_store",
		"target.sslmode=verify-full",
		"target.secretRef=arn:aws:secretsmanager:us-east-1:123456789012:secret:prod/signals-AbCdEf",
	)
	for _, want := range []string{
		"auth_method: secret_store",
		"secret_ref: arn:aws:secretsmanager:us-east-1:123456789012:secret:prod/signals-AbCdEf",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("secret_store config missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "password_env") {
		t.Errorf("secret_store is passwordless; password_env must not render (FC005):\n%s", out)
	}
}

func TestHelm_ServiceAccountAnnotationsRender(t *testing.T) {
	// IRSA / GKE Workload Identity bind the cloud identity through a
	// ServiceAccount annotation. The chart already exposes
	// serviceAccount.annotations; verify it reaches the SA resource.
	out := renderHelm(t,
		`serviceAccount.annotations.eks\.amazonaws\.com/role-arn=arn:aws:iam::111122223333:role/signals-irsa`,
	)
	if !strings.Contains(out, "eks.amazonaws.com/role-arn: arn:aws:iam::111122223333:role/signals-irsa") {
		t.Errorf("IRSA role-arn annotation not rendered on the ServiceAccount:\n%s", out)
	}
}

func TestHelm_PodLabelsRenderForAzureWorkloadIdentity(t *testing.T) {
	// AKS workload identity requires the pod itself to carry
	// azure.workload.identity/use: "true" so the webhook injects the
	// projected token. The chart exposes podLabels for this.
	out := renderHelm(t,
		`podLabels.azure\.workload\.identity/use=true`,
	)
	if !strings.Contains(out, "azure.workload.identity/use:") {
		t.Errorf("podLabels not rendered on the pod template (AKS workload identity needs it):\n%s", out)
	}
}

func TestHelm_ExtraVolumesAndMountsRender(t *testing.T) {
	// verify-full needs a CA bundle; operators mount it via
	// extraVolumes/extraVolumeMounts (e.g. a Secret holding the CA).
	out := renderHelm(t,
		"extraVolumes[0].name=ca",
		"extraVolumes[0].secret.secretName=db-ca",
		"extraVolumeMounts[0].name=ca",
		"extraVolumeMounts[0].mountPath=/etc/ssl/db",
	)
	for _, want := range []string{"name: ca", "secretName: db-ca", "mountPath: /etc/ssl/db"} {
		if !strings.Contains(out, want) {
			t.Errorf("extraVolumes/extraVolumeMounts not wired: missing %q:\n%s", want, out)
		}
	}
}

func TestHelm_TokenMethodShipsNoSecret(t *testing.T) {
	// A token-auth install with no passwordSecretName must not emit a
	// Secret-bound PG_PASSWORD anywhere (no secrets in values, INV001/2).
	out := renderHelm(t,
		targetHost,
		"target.authMethod=aws_rds_iam",
		"target.sslmode=verify-full",
	)
	if strings.Contains(out, "PG_PASSWORD") {
		t.Errorf("token method with no password secret must not reference PG_PASSWORD:\n%s", out)
	}
}
