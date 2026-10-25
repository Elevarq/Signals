package tests

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TC-VSP-01 (version-support-policy.md VSP-R002): the integration-mysql CI job
// must qualify the MySQL 8.0 legacy line and the 8.4 + 9.7 LTS lines as separate
// pinned live legs. This parses the real workflow, so it cannot drift from what
// CI runs: drop any of these legs (leaving a support claim with no evidence) and
// this goes red.
func TestMySQLVersionMatrixQualifies80And84(t *testing.T) {
	b, err := os.ReadFile("../.github/workflows/ci.yml")
	if err != nil {
		t.Fatalf("read ci.yml: %v", err)
	}
	var wf struct {
		Jobs struct {
			IntegrationMySQL struct {
				Strategy struct {
					Matrix struct {
						Include []struct {
							MySQLVersion string `yaml:"mysql_version"`
							Image        string `yaml:"image"`
						} `yaml:"include"`
					} `yaml:"matrix"`
				} `yaml:"strategy"`
			} `yaml:"integration-mysql"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(b, &wf); err != nil {
		t.Fatalf("parse ci.yml: %v", err)
	}

	legs := wf.Jobs.IntegrationMySQL.Strategy.Matrix.Include
	if len(legs) < 3 {
		t.Fatalf("integration-mysql must qualify >=3 MySQL versions (8.0 + 8.4 LTS + 9.7 LTS); got %d legs (VSP-R002)", len(legs))
	}
	want := map[string]bool{"8.0": false, "8.4": false, "9.7": false}
	for _, leg := range legs {
		if _, ok := want[leg.MySQLVersion]; ok {
			want[leg.MySQLVersion] = true
		}
		// Each qualified leg must be a pinned digest, never a float tag (VSP-R001).
		if !strings.Contains(leg.Image, "@sha256:") {
			t.Errorf("integration-mysql leg %q image %q must be pinned by digest", leg.MySQLVersion, leg.Image)
		}
	}
	for v, found := range want {
		if !found {
			t.Errorf("integration-mysql must include a live-qualified MySQL %s leg (VSP-R002)", v)
		}
	}
}
