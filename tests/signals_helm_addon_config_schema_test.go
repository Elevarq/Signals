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

const addonSchemaPath = "../deploy/helm/signals/aws_mp_configuration_schema.json"

// #297 — the Amazon EKS add-on configuration schema is a DEDICATED
// aws_mp_configuration_schema.json at the chart top level (AWS does NOT derive
// it from values.schema.json — the disproven #285/#290 premise). This guards
// R-EAO-07..09 / FC-EAO-05..07: the file exists, uses a supported JSON Schema
// draft, exposes the buyer-configurable keys the add-on needs to be pointed at
// a database, describes every property (console labels), and declares NO
// secret-value field (AWS INVALID_HELM_SENSITIVE_CONFIG).
func TestHelm_AddOnConfigurationSchemaContract(t *testing.T) {
	raw, err := os.ReadFile(addonSchemaPath)
	if err != nil {
		t.Fatalf("read aws_mp_configuration_schema.json: %v (the add-on config schema must exist at the chart top level, #297)", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("aws_mp_configuration_schema.json is not valid JSON: %v", err)
	}

	// Supported JSON Schema drafts (FC-EAO-07 / INCOMPATIBLE_CONFIGURATION_SCHEMA_VERSION).
	sch, _ := schema["$schema"].(string)
	okDraft := false
	for _, d := range []string{"draft-04", "draft-06", "draft-07", "draft/2019-09"} {
		if strings.Contains(sch, d) {
			okDraft = true
			break
		}
	}
	if !okDraft {
		t.Errorf("$schema %q is not an AWS-supported draft (draft-04/06/07 or draft/2019-09)", sch)
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("aws_mp_configuration_schema.json has no top-level \"properties\"")
	}

	// Required buyer-configurable surface (R-EAO-07): point the add-on at a DB
	// + durable storage.
	nested := map[string][]string{
		"target":      {"host", "user", "authMethod", "sslmode", "sslRootCertFile"},
		"persistence": {"storageClass"},
	}
	for parent, children := range nested {
		p, ok := props[parent].(map[string]any)
		if !ok {
			t.Errorf("add-on schema: missing configurable property %q", parent)
			continue
		}
		cp, _ := p["properties"].(map[string]any)
		for _, child := range children {
			if _, ok := cp[child]; !ok {
				t.Errorf("add-on schema: %q must declare %q", parent, child)
			}
		}
	}

	// Add-on-managed / auto-injected keys MUST be absent (R-EAO-09).
	for _, forbidden := range []string{"image", "serviceAccount"} {
		if _, ok := props[forbidden]; ok {
			t.Errorf("add-on schema must NOT declare %q (add-on-managed / set via the add-on API, #297)", forbidden)
		}
	}

	// Every property (at any depth) must carry a description (AWS renders it as
	// the console label), and no property key may be a raw secret value
	// (R-EAO-08 / FC-EAO-06). Identity is by reference only.
	var missingDesc, secretKeys []string
	walkSchemaProps(props, func(name string, node map[string]any) {
		if _, ok := node["description"]; !ok {
			// Only leaf/declared properties need a description; objects that
			// merely nest `properties` are covered by their children. Require a
			// description on everything we declare for console clarity.
			missingDesc = append(missingDesc, name)
		}
		if isRawSecretKey(name) {
			secretKeys = append(secretKeys, name)
		}
	})
	if len(missingDesc) > 0 {
		t.Errorf("add-on schema: these properties lack a description (console label): %s", strings.Join(missingDesc, ", "))
	}
	if len(secretKeys) > 0 {
		t.Errorf("add-on schema: these keys look like raw secret values (use *SecretName / *File references instead, INVALID_HELM_SENSITIVE_CONFIG): %s", strings.Join(secretKeys, ", "))
	}
}

// walkSchemaProps visits every declared property node (recursing into nested
// object `properties`), calling fn(name, node).
func walkSchemaProps(props map[string]any, fn func(string, map[string]any)) {
	for name, v := range props {
		node, ok := v.(map[string]any)
		if !ok {
			continue
		}
		fn(name, node)
		if child, ok := node["properties"].(map[string]any); ok {
			walkSchemaProps(child, fn)
		}
	}
}

// isRawSecretKey reports whether a property key names a secret VALUE rather
// than a reference to one. A key containing a secret root is allowed only when
// it ends in a reference suffix (…SecretName, …SecretKey, …File, …Ref, …Name,
// …Key, …Env).
func isRawSecretKey(key string) bool {
	k := strings.ToLower(key)
	roots := []string{"password", "passphrase", "privatekey", "apikey", "cert", "token"}
	hasRoot := false
	for _, r := range roots {
		if strings.Contains(k, r) {
			hasRoot = true
			break
		}
	}
	if !hasRoot {
		return false
	}
	for _, suf := range []string{"secretname", "secretkey", "file", "ref", "name", "key", "env"} {
		if strings.HasSuffix(k, suf) {
			return false
		}
	}
	return true
}

// The #274 extraEnv guard on values.schema.json must survive (independent of
// the add-on schema): chart-managed SIGNALS_* env names are rejected at render,
// and the schema declares the extraEnv shape.
func TestHelm_ValuesSchemaKeepsExtraEnvGuard(t *testing.T) {
	raw, err := os.ReadFile("../deploy/helm/signals/values.schema.json")
	if err != nil {
		t.Fatalf("read values.schema.json: %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("values.schema.json invalid JSON: %v", err)
	}
	props, _ := schema["properties"].(map[string]any)
	if _, ok := props["extraEnv"]; !ok {
		t.Errorf("values.schema.json: extraEnv property was dropped (#274 guard must be preserved)")
	}
}
