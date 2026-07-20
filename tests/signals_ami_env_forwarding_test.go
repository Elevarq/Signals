package tests

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// AMI / EC2 env-forwarding contract (Elevarq/Signals#292).
//
// Spec: specifications/marketplace-ami-image-builder.md
//   - R-AMI-05 / INV-AMI-03 (TC-AMI-06): the buyer-supplied signals.env is
//     forwarded WHOLE (`--env-file /etc/signals/signals.env`) in BOTH the AMI
//     Image Builder component and the deploy/aws/terraform run path, so any
//     SIGNALS_* var (not just SIGNALS_API_TOKEN) reaches the collector; both
//     paths forward env identically.
//   - R-AMI-06 / INV-AMI-04 (TC-AMI-07): the forwarding mechanism never echoes,
//     cats, tees, or otherwise logs the contents of signals.env or a token
//     value; env crosses the boundary by reference only.
//
// These assert on the committed deployment artifacts as text (the same style
// as tests/signals_helm_production_test.go asserts on the rendered chart) so a
// regression to the pre-#292 "-e SIGNALS_API_TOKEN only" form fails CI.

const (
	amiComponentPath = "../deploy/aws/imagebuilder/signals-collector-component.yaml"
	awsTerraformPath = "../deploy/aws/terraform/main.tf"
	awsCloudFormPath = "../deploy/aws/cloudformation/signals-rds-iam.yaml"
)

// The EC2 run paths that must stay in env-forwarding parity (INV-AMI-03): the
// AMI Image Builder component and the two IaC launch paths. All three docker-run
// the collector; all three must forward the whole signals.env by reference.
var ec2RunPaths = []struct{ name, path string }{
	{"component", amiComponentPath},
	{"terraform", awsTerraformPath},
	{"cloudformation", awsCloudFormPath},
}

func readArtifact(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// TC-AMI-06: both EC2 paths forward the WHOLE signals.env via --env-file, so a
// buyer-supplied SIGNALS_* var reaches the collector (R-AMI-05), and they do it
// the same way (INV-AMI-03 parity).
func TestAMI_EnvFileForwardedInAllPaths(t *testing.T) {
	const envFileFlag = "--env-file /etc/signals/signals.env"

	for _, tc := range ec2RunPaths {
		body := readArtifact(t, tc.path)
		if !strings.Contains(body, envFileFlag) {
			t.Errorf("%s: docker run must forward the whole env with %q so any "+
				"SIGNALS_* var reaches the collector (R-AMI-05, INV-AMI-03 parity); "+
				"not found", tc.name, envFileFlag)
		}
	}

	// The component keeps EnvironmentFile= so systemd fails cleanly if the file
	// is absent (R-AMI-05).
	component := readArtifact(t, amiComponentPath)
	if !strings.Contains(component, "EnvironmentFile=/etc/signals/signals.env") {
		t.Errorf("AMI component: must retain EnvironmentFile=/etc/signals/signals.env")
	}
}

// TC-AMI-06 (regression guard): the pre-#292 form forwarded ONLY the token, so
// other SIGNALS_* vars were silently dropped. Neither path may pass token-only
// (a bare `-e SIGNALS_API_TOKEN` on the docker-run line with no --env-file).
func TestAMI_TokenOnlyForwardingRejected(t *testing.T) {
	for _, tc := range ec2RunPaths {
		body := readArtifact(t, tc.path)
		if strings.Contains(body, "-e SIGNALS_API_TOKEN") &&
			!strings.Contains(body, "--env-file /etc/signals/signals.env") {
			t.Errorf("%s: forwards SIGNALS_API_TOKEN alone without --env-file; "+
				"buyer SIGNALS_* vars would be dropped (FC-AMI-04, #292)", tc.name)
		}
	}
}

// TC-AMI-07: no artifact prints the contents of signals.env or a token value to
// a log stream (R-AMI-06 / INV-AMI-04). Env crosses the boundary by reference.
func TestAMI_EnvSecretsNeverLogged(t *testing.T) {
	// A read of the env file's CONTENTS onto a log stream (stdout/stderr) is the
	// leak (R-AMI-06). Matched forms: a reader command taking signals.env as
	// input (`cat/less/tee/... signals.env`), or a `$(cat ...signals.env)`
	// command substitution feeding an echo/printf. A redirect INTO the file
	// (`... > /etc/signals/signals.env`, the token WRITE) is NOT a leak and is
	// excluded. `EnvironmentFile=` / `--env-file <path>` load without printing
	// and are excluded (they carry no reader command before the path).
	catEnvFile := regexp.MustCompile(`\b(cat|tee|less|more|head|tail|xxd|od|hexdump)\b[^\n>]*signals\.env`)
	substEnvFile := regexp.MustCompile(`\$\([^)]*\bcat\b[^)]*signals\.env`)

	// A token VALUE placed on a docker command line (`-e SIGNALS_API_TOKEN=...`
	// or `--env SIGNALS_API_TOKEN=...`) — the by-reference forms `-e NAME` (no
	// `=`) and `--env-file` are safe. The RHS here is a concrete value/interp.
	tokenValueOnCmdline := regexp.MustCompile(`--?e(nv)?[= ]SIGNALS_API_TOKEN=\S`)

	for _, tc := range ec2RunPaths {
		body := readArtifact(t, tc.path)

		for i, line := range strings.Split(body, "\n") {
			// Comments document behaviour; they carry no runtime secret.
			trimmed := strings.TrimLeft(line, " \t")
			if strings.HasPrefix(trimmed, "#") {
				continue
			}
			if catEnvFile.MatchString(line) || substEnvFile.MatchString(line) {
				t.Errorf("%s:%d prints signals.env to a log stream (R-AMI-06): %q",
					tc.name, i+1, strings.TrimSpace(line))
			}
			if tokenValueOnCmdline.MatchString(line) {
				t.Errorf("%s:%d puts a SIGNALS_API_TOKEN value on the docker command "+
					"line (visible in the journal / ps); pass by reference (R-AMI-06): %q",
					tc.name, i+1, strings.TrimSpace(line))
			}
		}

		// A `set -x` anywhere in the AMI component's shell steps would echo every
		// subsequent command (and its expanded token) to the journal. Guard it in
		// the component (its shell is baked into the unit/build). Terraform's
		// user_data intentionally uses `set -euo pipefail` (no -x); assert no -x.
		if strings.Contains(body, "set -x") ||
			regexp.MustCompile(`set -[a-wyz]*x`).MatchString(body) {
			t.Errorf("%s: enables shell tracing (set -x) — would echo expanded "+
				"token values to the journal (R-AMI-06)", tc.name)
		}
	}
}
