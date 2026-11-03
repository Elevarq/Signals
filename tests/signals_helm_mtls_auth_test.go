package tests

import (
	"os/exec"
	"strings"
	"testing"
)

// #330 — the Helm chart must be able to render the shipped `mtls`
// authentication method. Signals' internal/config/config.go supports
// auth_method: mtls with the file-path fields sslcert / sslkey (both
// required — FC-MTLS-001), yet before #330 the chart's
// values.schema.json rejected target.authMethod=mtls and
// templates/configmap.yaml rendered none of the certificate/key path
// fields — so a supported target could be hand-written but never
// deployed via the chart.
//
// These tests are the chart contract: a VALID mTLS target renders the
// sslcert/sslkey PATHS into signals.yaml, and a mTLS target MISSING the
// required cert/key paths is REJECTED by values.schema.json at `helm
// template` time. Only PATHS ever reach the values/ConfigMap — never
// certificate or key CONTENTS. Encrypted keys are unsupported (#654).

func TestHelm_MTLSRendersCertAndKeyPaths(t *testing.T) {
	out := renderHelm(t,
		targetHost,
		"target.authMethod=mtls",
		"target.sslmode=verify-full",
		"target.sslCertFile=/etc/signals/mtls/client.crt",
		"target.sslKeyFile=/etc/signals/mtls/client.key",
	)
	for _, want := range []string{
		"auth_method: mtls",
		"sslmode: verify-full",
		"sslcert: /etc/signals/mtls/client.crt",
		"sslkey: /etc/signals/mtls/client.key",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("mtls config missing %q:\n%s", want, out)
		}
	}
	// mTLS authenticates by client certificate — passwordless (FC-MTLS-005).
	if strings.Contains(out, "password_env") {
		t.Errorf("mtls is passwordless; password_env must not render (FC-MTLS-005):\n%s", out)
	}
	// Encrypted keys are unsupported (#654) — the chart has no passphrase
	// field, so it must never render one.
	if strings.Contains(out, "sslkey_passphrase_file:") {
		t.Errorf("sslkey_passphrase_file must never render (encrypted keys unsupported, #654):\n%s", out)
	}
}

// SECURITY: only PATHS ever reach the values/ConfigMap. The chart must
// expose no value that carries certificate, key, or passphrase content;
// the files land on disk via a mounted Secret (extraVolumes /
// extraVolumeMounts), and the chart only writes their paths.
func TestHelm_MTLSDoesNotRenderCertificateContents(t *testing.T) {
	out := renderHelm(t,
		targetHost,
		"target.authMethod=mtls",
		"target.sslmode=verify-full",
		"target.sslCertFile=/etc/signals/mtls/client.crt",
		"target.sslKeyFile=/etc/signals/mtls/client.key",
	)
	for _, forbidden := range []string{
		"BEGIN CERTIFICATE",
		"BEGIN PRIVATE KEY",
		"BEGIN RSA PRIVATE KEY",
		"BEGIN ENCRYPTED PRIVATE KEY",
	} {
		if strings.Contains(out, forbidden) {
			t.Errorf("rendered manifest leaked certificate/key material (%q):\n%s", forbidden, out)
		}
	}
}

// A mTLS target that omits the required cert/key paths must be rejected
// by values.schema.json at `helm template` time — mirroring
// config.go's FC-MTLS-001 (both sslcert and sslkey are required). This
// fails the render fast rather than shipping an invalid target.
func TestHelm_MTLSMissingCertKeyRejectedBySchema(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm CLI not on PATH; skipping helm-template assertion")
	}
	cmd := exec.Command("helm", "template", "signals", prodChartPath,
		"--set", targetHost,
		"--set", "target.authMethod=mtls",
		"--set", "target.sslmode=verify-full",
		// sslCertFile / sslKeyFile deliberately omitted.
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("mtls target without cert/key paths was accepted; schema must reject it:\n%s", out)
	}
	if !strings.Contains(string(out), "sslCertFile") && !strings.Contains(string(out), "sslKeyFile") {
		t.Fatalf("schema rejection should name the missing mTLS path field(s):\n%s", out)
	}
}

// The add-on / Helm values schema must ADMIT mtls as an authMethod
// enum value (before #330 it rejected it outright).
func TestHelm_MTLSAuthMethodAcceptedBySchema(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm CLI not on PATH; skipping helm-template assertion")
	}
	cmd := exec.Command("helm", "template", "signals", prodChartPath,
		"--set", targetHost,
		"--set", "target.authMethod=mtls",
		"--set", "target.sslmode=verify-full",
		"--set", "target.sslCertFile=/etc/signals/mtls/client.crt",
		"--set", "target.sslKeyFile=/etc/signals/mtls/client.key",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("valid mtls values must pass schema validation; got:\n%s", out)
	}
}
