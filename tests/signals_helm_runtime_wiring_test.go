package tests

import (
	"os/exec"
	"strings"
	"testing"
)

func TestHelm_DefaultPersistentVolumeOwnership(t *testing.T) {
	out := renderHelm(t)
	for _, want := range []string{
		"runAsUser: 10001",
		"runAsGroup: 10001",
		"fsGroup: 10001",
		"fsGroupChangePolicy: OnRootMismatch",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("default pod security context missing %q:\n%s", want, out)
		}
	}
}

func TestHelm_PersistenceStorageClassRenders(t *testing.T) {
	out := renderHelm(t, "persistence.storageClass=signals-gp3")
	if !strings.Contains(out, "storageClassName: \"signals-gp3\"") {
		t.Errorf("selected storage class not rendered:\n%s", out)
	}
}

func TestHelm_ExtraEnvRendersTLSFilePaths(t *testing.T) {
	out := renderHelm(t,
		"extraEnv[0].name=SIGNALS_API_TLS_CERT_FILE",
		"extraEnv[0].value=/etc/ssl/signals/tls.crt",
		"extraEnv[1].name=SIGNALS_API_TLS_KEY_FILE",
		"extraEnv[1].value=/etc/ssl/signals/tls.key",
	)
	for _, want := range []string{
		"name: SIGNALS_API_TLS_CERT_FILE",
		"value: /etc/ssl/signals/tls.crt",
		"name: SIGNALS_API_TLS_KEY_FILE",
		"value: /etc/ssl/signals/tls.key",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("extraEnv TLS wiring missing %q:\n%s", want, out)
		}
	}
}

func TestHelm_ExtraEnvSecretReferenceDoesNotRenderValue(t *testing.T) {
	out := renderHelm(t,
		"extraEnv[0].name=EXTERNAL_SERVICE_TOKEN",
		"extraEnv[0].valueFrom.secretKeyRef.name=external-service",
		"extraEnv[0].valueFrom.secretKeyRef.key=token",
	)
	for _, want := range []string{
		"name: EXTERNAL_SERVICE_TOKEN",
		"valueFrom:",
		"secretKeyRef:",
		"name: external-service",
		"key: token",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("extraEnv secret reference missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "value: external-service") {
		t.Errorf("secret reference was rendered as a literal value:\n%s", out)
	}
}

func TestHelm_ExtraEnvRejectsManagedName(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm CLI not on PATH; skipping helm-template assertions")
	}
	cmd := exec.Command("helm", "template", "signals", prodChartPath,
		"--set", "extraEnv[0].name=SIGNALS_API_TOKEN",
		"--set", "extraEnv[0].value=not-allowed",
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("managed environment name unexpectedly accepted:\n%s", out)
	}
	if !strings.Contains(string(out), "managed by the chart") {
		t.Fatalf("unexpected managed-name validation error:\n%s", out)
	}
}

func TestHelm_ExtraEnvRejectsMalformedEntry(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm CLI not on PATH; skipping helm-template assertions")
	}
	cmd := exec.Command("helm", "template", "signals", prodChartPath,
		"--set", "extraEnv[0].value=missing-name",
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("malformed extraEnv entry unexpectedly accepted:\n%s", out)
	}
	if !strings.Contains(string(out), "missing property 'name'") {
		t.Fatalf("unexpected schema validation error:\n%s", out)
	}
}
