package tests

import (
	"strings"
	"testing"
)

// #727 (audit A1) — the opt-in restrictive NetworkPolicy must permit the egress
// the configured deployment actually needs: every configured target port (not
// just the single primary) and, via an operator-scoped passthrough, the
// cloud-credential / S3-export endpoints — WITHOUT weakening default-deny or
// adding blanket internet egress.
//
// Spec: specifications/networkpolicy-egress-composition.md (NPE-R001..R003).

// npPolicy renders the chart with the NetworkPolicy enabled and a real target
// CIDR (the chart refuses the 0.0.0.0/0 placeholder) plus the given extra sets,
// and returns the NetworkPolicy manifest.
func npPolicy(t *testing.T, sets ...string) string {
	t.Helper()
	base := []string{
		"networkPolicy.enabled=true",
		"networkPolicy.targetCIDRs[0]=10.1.0.0/16",
	}
	out := renderHelm(t, append(base, sets...)...)
	for _, doc := range strings.Split(out, "\n---") {
		if strings.Contains(doc, "kind: NetworkPolicy") {
			return doc
		}
	}
	t.Fatal("no NetworkPolicy manifest in render")
	return ""
}

// NPE-R001 — every configured target port appears in the egress rules.
func TestHelm_EgressCoversAllTargetPorts(t *testing.T) {
	np := npPolicy(t,
		"target.port=5432",
		"targets[0].name=rep", "targets[0].host=r.example.com", "targets[0].port=6432",
	)
	for _, port := range []string{"port: 5432", "port: 6432"} {
		if !strings.Contains(np, port) {
			t.Errorf("egress missing a configured target port (%s); multi-target would be blocked by an enforcing CNI", port)
		}
	}
}

// NPE-R002 — operator-scoped extraEgress is rendered verbatim so cloud / S3
// integration flows can be permitted explicitly.
func TestHelm_EgressExtraEgressPassthrough(t *testing.T) {
	np := npPolicy(t,
		"networkPolicy.extraEgress[0].to[0].ipBlock.cidr=10.0.0.0/8",
		"networkPolicy.extraEgress[0].ports[0].protocol=TCP",
		"networkPolicy.extraEgress[0].ports[0].port=443",
	)
	if !strings.Contains(np, "10.0.0.0/8") || !strings.Contains(np, "port: 443") {
		t.Errorf("extraEgress not rendered into the NetworkPolicy egress; cloud/S3 flows would be denied:\n%s", np)
	}
}

// NPE-R003 — default-deny is preserved: with no extraEgress the policy never
// emits a blanket 0.0.0.0/0 egress destination.
func TestHelm_EgressNoBlanketInternetByDefault(t *testing.T) {
	np := npPolicy(t, "target.port=5432")
	// The only allowed destinations are the operator's target CIDR and kube-DNS.
	if strings.Contains(np, "0.0.0.0/0") {
		t.Errorf("default NetworkPolicy egress must not contain a blanket 0.0.0.0/0 destination:\n%s", np)
	}
}
