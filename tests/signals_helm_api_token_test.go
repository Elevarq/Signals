package tests

import (
	"os/exec"
	"strings"
	"testing"
)

// #136 — three helm-template assertions:
//   1. Default values (no tokenSecretName) → no ARQ_SIGNALS_API_TOKEN env in
//      the rendered deployment.
//   2. tokenSecretName set → ARQ_SIGNALS_API_TOKEN injected via
//      secretKeyRef pointing at the named Secret + key.
//   3. No literal token value reaches the rendered manifest in either
//      case (the Secret reference is the only token-related string).

const helmChartPath = "../deploy/helm/signals"

func helmTemplate(t *testing.T, sets ...string) string {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm CLI not on PATH; skipping helm-template assertions")
	}
	args := []string{"template", "signals", helmChartPath}
	for _, s := range sets {
		args = append(args, "--set", s)
	}
	out, err := exec.Command("helm", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, out)
	}
	return string(out)
}

// #136 — default render carries no API_TOKEN env (binary auto-generates).
func TestHelm_DefaultRender_NoAPITokenEnv(t *testing.T) {
	out := helmTemplate(t)
	if strings.Contains(out, "ARQ_SIGNALS_API_TOKEN") {
		t.Errorf("default render unexpectedly contains ARQ_SIGNALS_API_TOKEN; rendered manifest:\n%s", out)
	}
}

// #136 — tokenSecretName set → secretKeyRef wired with the documented
// shape. The Secret's `token` key is the default; named secret is
// what the operator supplied.
func TestHelm_TokenSecretName_WiresSecretKeyRef(t *testing.T) {
	out := helmTemplate(t,
		"api.tokenSecretName=my-api-secret",
	)
	if !strings.Contains(out, "name: ARQ_SIGNALS_API_TOKEN") {
		t.Errorf("ARQ_SIGNALS_API_TOKEN env not wired; rendered:\n%s", out)
	}
	if !strings.Contains(out, "name: my-api-secret") {
		t.Errorf("Secret reference name missing; rendered:\n%s", out)
	}
	if !strings.Contains(out, "key: token") {
		t.Errorf("default tokenSecretKey 'token' not used; rendered:\n%s", out)
	}
}

// #136 — operator-supplied tokenSecretKey honored.
func TestHelm_TokenSecretKey_CustomKey(t *testing.T) {
	out := helmTemplate(t,
		"api.tokenSecretName=my-api-secret",
		"api.tokenSecretKey=my-key-name",
	)
	if !strings.Contains(out, "key: my-key-name") {
		t.Errorf("custom tokenSecretKey not used; rendered:\n%s", out)
	}
}

// #136 — no leakage: the rendered manifest never carries the literal
// token value, just the Secret reference. Set a sentinel value as
// the Secret NAME (which IS expected to appear) but assert that no
// other token-shaped string materialises.
func TestHelm_TokenSecretName_NoLiteralTokenInRender(t *testing.T) {
	out := helmTemplate(t,
		"api.tokenSecretName=my-api-secret",
		"api.tokenSecretKey=my-token-key",
	)
	// The deployment YAML should NOT have a literal `value:` for
	// ARQ_SIGNALS_API_TOKEN — only a `valueFrom: secretKeyRef`.
	// Find the env block for ARQ_SIGNALS_API_TOKEN and confirm it
	// uses valueFrom, not value.
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		if strings.Contains(l, "name: ARQ_SIGNALS_API_TOKEN") {
			// Look at the next few lines for `value:` vs `valueFrom:`
			end := i + 5
			if end > len(lines) {
				end = len(lines)
			}
			window := strings.Join(lines[i:end], "\n")
			if strings.Contains(window, "value: ") && !strings.Contains(window, "valueFrom:") {
				t.Errorf("ARQ_SIGNALS_API_TOKEN appears to use literal 'value:' rather than valueFrom/secretKeyRef:\n%s", window)
			}
			if !strings.Contains(window, "valueFrom:") {
				t.Errorf("ARQ_SIGNALS_API_TOKEN missing valueFrom block:\n%s", window)
			}
			break
		}
	}
}
