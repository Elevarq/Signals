package tests

import (
	"os/exec"
	"strings"
	"testing"
)

const prodChartPath = "../deploy/helm/signals"

func renderHelm(t *testing.T, sets ...string) string {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm CLI not on PATH; skipping helm-render assertions")
	}
	args := []string{"template", "signals", prodChartPath}
	for _, s := range sets {
		args = append(args, "--set", s)
	}
	out, err := exec.Command("helm", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, out)
	}
	return string(out)
}

// #140 — default render produces all the templates a single-replica
// production install needs without operator intervention. The PVC
// fix specifically: before #140 the deployment mounted a volume
// named `<release>-signals-data` but no PVC manifest existed.
func TestHelm_DefaultRenderContainsAllTemplates(t *testing.T) {
	out := renderHelm(t)
	for _, kind := range []string{
		"kind: Deployment",
		"kind: Service",
		"kind: ConfigMap",
		"kind: PersistentVolumeClaim", // #140 — was missing before
		"kind: ServiceAccount",        // #140 — new
	} {
		if !strings.Contains(out, kind) {
			t.Errorf("default render missing %q", kind)
		}
	}
}

// #140 — opt-in templates (NetworkPolicy, PDB) are OFF by default.
// Operators MUST consciously enable them; the cluster's CNI may not
// implement NetworkPolicy, making the manifest a no-op rather than
// a security gain, so default-off surfaces the decision.
func TestHelm_DefaultRenderExcludesOptInTemplates(t *testing.T) {
	out := renderHelm(t)
	for _, kind := range []string{
		"kind: NetworkPolicy",
		"kind: PodDisruptionBudget",
	} {
		if strings.Contains(out, kind) {
			t.Errorf("default render unexpectedly contains %q (should be opt-in)", kind)
		}
	}
}

// #140 — networkPolicy.enabled=true wires the manifest with the
// documented deny-by-default posture.
func TestHelm_NetworkPolicyEnabledRenders(t *testing.T) {
	out := renderHelm(t,
		"networkPolicy.enabled=true",
		"networkPolicy.targetCIDRs={10.42.0.0/24}",
	)
	if !strings.Contains(out, "kind: NetworkPolicy") {
		t.Fatalf("NetworkPolicy not in rendered output:\n%s", out)
	}
	if !strings.Contains(out, "10.42.0.0/24") {
		t.Errorf("targetCIDR not wired into NetworkPolicy: %s", out)
	}
	// Both policy types present.
	for _, want := range []string{
		"policyTypes:",
		"- Ingress",
		"- Egress",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in NetworkPolicy: %s", want, out)
		}
	}
}

// #140 — PodDisruptionBudget.enabled=true wires the default
// maxUnavailable: 0 posture (single-replica preservation).
func TestHelm_PDBEnabledDefaultBlocksEviction(t *testing.T) {
	out := renderHelm(t, "podDisruptionBudget.enabled=true")
	if !strings.Contains(out, "kind: PodDisruptionBudget") {
		t.Fatalf("PDB not in rendered output:\n%s", out)
	}
	if !strings.Contains(out, "maxUnavailable: 0") {
		t.Errorf("PDB default should block voluntary eviction; rendered:\n%s", out)
	}
}

// #140 — ServiceAccount is created by default with
// automountServiceAccountToken: false. Signals never talks to the
// apiserver.
func TestHelm_ServiceAccountAutomountDisabled(t *testing.T) {
	out := renderHelm(t)
	if !strings.Contains(out, "kind: ServiceAccount") {
		t.Fatalf("ServiceAccount not in rendered output:\n%s", out)
	}
	if !strings.Contains(out, "automountServiceAccountToken: false") {
		t.Errorf("automountServiceAccountToken should be false; rendered:\n%s", out)
	}
}

// #140 — disabling persistence falls back to an emptyDir, suitable
// for evaluation only. The PVC must NOT render in that case.
func TestHelm_PersistenceDisabledNoPVC(t *testing.T) {
	out := renderHelm(t, "persistence.enabled=false")
	if strings.Contains(out, "kind: PersistentVolumeClaim") {
		t.Errorf("PVC rendered with persistence.enabled=false:\n%s", out)
	}
	if !strings.Contains(out, "emptyDir:") {
		t.Errorf("emptyDir fallback not wired with persistence.enabled=false:\n%s", out)
	}
}
