package tests

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// AWS EKS add-on ingestion renders the chart with Helm's reserved `global`
// value injected. A top-level `additionalProperties: false` in values.schema.json
// makes `helm template` fail ("additional properties 'global' not allowed") ->
// the add-on change-set fails INVALID_HELM_TEMPLATE. The top-level object must
// stay open. Guards Elevarq/Signals#290 (regression from #285).
func TestHelm_ValuesSchemaToleratesInjectedGlobal(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm CLI not on PATH; skipping helm-template assertion")
	}
	cmd := exec.Command("helm", "template", "signals", "../deploy/helm/signals",
		"--set", "global.foo=bar")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helm template with an injected `global` value must render "+
			"(add-on ingestion injects it); got error: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "additional properties") {
		t.Fatalf("schema rejected an injected top-level key:\n%s", out)
	}
}

// The Amazon EKS add-on configuration schema is derived by AWS from the chart's
// values.schema.json (aws eks describe-addon-configuration). If the required
// buyer-configurable keys are absent, the add-on reports "No configuration
// support" and cannot be pointed at a database, so it cannot reach behavioral
// parity with the Helm delivery. Guards R-EAO-07 / FC-EAO-05 / INV-EAO-02 of
// specifications/marketplace-eks-addon-delivery.md (Elevarq/Signals#285).
func TestHelm_ValuesSchemaExposesAddOnConfigContract(t *testing.T) {
	raw, err := os.ReadFile("../deploy/helm/signals/values.schema.json")
	if err != nil {
		t.Fatalf("read values.schema.json: %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("values.schema.json is not valid JSON: %v", err)
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("values.schema.json has no top-level \"properties\" object")
	}

	// Nested buyer-configurable keys the add-on install path requires
	// (R-EAO-07): parent -> child properties that must be declared.
	nested := map[string][]string{
		"target":         {"host", "user", "authMethod", "sslmode", "sslRootCertFile"},
		"persistence":    {"storageClass"},
		"serviceAccount": {"annotations"},
	}
	for parent, children := range nested {
		p, ok := props[parent].(map[string]any)
		if !ok {
			t.Errorf("values.schema.json: missing configurable property %q", parent)
			continue
		}
		cp, ok := p["properties"].(map[string]any)
		if !ok {
			t.Errorf("values.schema.json: %q declares no sub-properties", parent)
			continue
		}
		for _, child := range children {
			if _, ok := cp[child]; !ok {
				t.Errorf("values.schema.json: %q must declare %q for the add-on config schema", parent, child)
			}
		}
	}

	// The chart-managed extraEnv guard must survive the expansion.
	if _, ok := props["extraEnv"]; !ok {
		t.Errorf("values.schema.json: extraEnv property was dropped (the #274 guard must be preserved)")
	}
}
