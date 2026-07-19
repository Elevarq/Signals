package tests

import (
	"encoding/json"
	"os"
	"testing"
)

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
