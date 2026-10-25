package tests

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TC-VSP-04 (version-support-policy.md VSP-R006; mariadb-live-qualification.md
// MDLQ-R006): the integration-mariadb CI job must qualify the four supported
// MariaDB LTS lines — 10.11, 11.4, 11.8, 12.3 — as separate pinned live legs.
// This parses the real workflow, so it cannot drift from what CI runs: drop any
// of these legs (leaving a support claim with no evidence) and this goes red.
// MariaDB qualification is independent of MySQL's (VSP-R006).
func TestMariaDBVersionMatrixQualifiesLTS(t *testing.T) {
	b, err := os.ReadFile("../.github/workflows/ci.yml")
	if err != nil {
		t.Fatalf("read ci.yml: %v", err)
	}
	var wf struct {
		Jobs struct {
			IntegrationMariaDB struct {
				Strategy struct {
					Matrix struct {
						Include []struct {
							MariaDBVersion string `yaml:"mariadb_version"`
							Image          string `yaml:"image"`
							VersionPrefix  string `yaml:"version_prefix"`
						} `yaml:"include"`
					} `yaml:"matrix"`
				} `yaml:"strategy"`
			} `yaml:"integration-mariadb"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(b, &wf); err != nil {
		t.Fatalf("parse ci.yml: %v", err)
	}

	legs := wf.Jobs.IntegrationMariaDB.Strategy.Matrix.Include
	if len(legs) < 4 {
		t.Fatalf("integration-mariadb must qualify the four MariaDB LTS lines (10.11 + 11.4 + 11.8 + 12.3); got %d legs (VSP-R006)", len(legs))
	}
	// Each qualified LTS line must be present, pinned by digest, and carry a
	// version_prefix matching its LTS line (so the live verifier asserts the exact
	// generation, not just a shared major).
	want := map[string]string{
		"10.11": "10.11.",
		"11.4":  "11.4.",
		"11.8":  "11.8.",
		"12.3":  "12.3.",
	}
	seen := map[string]bool{}
	for _, leg := range legs {
		prefix, ok := want[leg.MariaDBVersion]
		if ok {
			seen[leg.MariaDBVersion] = true
			if leg.VersionPrefix != prefix {
				t.Errorf("integration-mariadb leg %q version_prefix=%q, want %q (VSP-R006)", leg.MariaDBVersion, leg.VersionPrefix, prefix)
			}
		}
		// Every leg must be a pinned digest, never a float tag (VSP-R001).
		if !strings.Contains(leg.Image, "@sha256:") {
			t.Errorf("integration-mariadb leg %q image %q must be pinned by digest", leg.MariaDBVersion, leg.Image)
		}
	}
	for v := range want {
		if !seen[v] {
			t.Errorf("integration-mariadb must include a live-qualified MariaDB %s LTS leg (VSP-R006)", v)
		}
	}
}
