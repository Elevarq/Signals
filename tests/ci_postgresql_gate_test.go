package tests

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// flexStrings unmarshals a YAML field that may be a single scalar or a
// sequence of scalars (GitHub Actions `needs:` accepts both forms) into a
// []string so the guard below can treat them uniformly.
type flexStrings []string

func (f *flexStrings) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		*f = flexStrings{value.Value}
		return nil
	}
	var s []string
	if err := value.Decode(&s); err != nil {
		return err
	}
	*f = s
	return nil
}

type ciJob struct {
	Needs    flexStrings `yaml:"needs"`
	Strategy struct {
		Matrix struct {
			PG []int `yaml:"pg"`
		} `yaml:"matrix"`
	} `yaml:"strategy"`
}

type ciWorkflow struct {
	Jobs map[string]ciJob `yaml:"jobs"`
}

// TestCIGateRequiresPostgreSQL is the M0 structural lock (#528).
//
// On a single-maintainer account, GitHub branch protection is not a reliable
// control — required-check settings can be toggled or bypassed. So the
// guarantee that "no Signals test run can be green without PostgreSQL,
// whatever engine it targets" is enforced HERE, in a plain unit test that
// runs in the `test` job on every push and PR. Weaken the CI workflow — drop
// the live-PG matrix, delete the terminal gate, or add an engine integration
// suite that bypasses the gate — and this test goes red.
//
// It reads the real workflow file, so it cannot drift from what CI runs.
func TestCIGateRequiresPostgreSQL(t *testing.T) {
	b, err := os.ReadFile("../.github/workflows/ci.yml")
	if err != nil {
		t.Fatalf("read ci.yml: %v", err)
	}
	var wf ciWorkflow
	if err := yaml.Unmarshal(b, &wf); err != nil {
		t.Fatalf("parse ci.yml: %v", err)
	}

	// 1. The live-PostgreSQL job must exist and cover every supported major.
	pg, ok := wf.Jobs["integration-pg"]
	if !ok {
		t.Fatal("ci.yml: job `integration-pg` is missing — PostgreSQL is no longer exercised in CI (#528)")
	}
	for _, major := range []int{14, 15, 16, 17, 18} {
		found := false
		for _, v := range pg.Strategy.Matrix.PG {
			if v == major {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("ci.yml: integration-pg matrix missing PostgreSQL %d (have %v) (#528)", major, pg.Strategy.Matrix.PG)
		}
	}

	// 2. The built-image smoke (built artifact vs real PostgreSQL) must exist.
	if _, ok := wf.Jobs["built-image-smoke"]; !ok {
		t.Fatal("ci.yml: job `built-image-smoke` is missing — the built artifact is no longer smoked against PostgreSQL (#528)")
	}

	// 3. The terminal gate must depend on the PostgreSQL jobs (and the other
	//    gates) so the run cannot go green — or silently skip PostgreSQL —
	//    without them.
	gate, ok := wf.Jobs["ci-gate"]
	if !ok {
		t.Fatal("ci.yml: terminal job `ci-gate` is missing — nothing forces the run red when PostgreSQL does not pass (#528)")
	}
	gateNeeds := map[string]bool{}
	for _, n := range gate.Needs {
		gateNeeds[n] = true
	}
	for _, required := range []string{"integration-pg", "built-image-smoke", "test", "security-scan"} {
		if !gateNeeds[required] {
			t.Errorf("ci.yml: ci-gate.needs must include %q so the run fails unless it succeeds (#528)", required)
		}
	}

	// 4. Every engine-specific integration suite must route through the SAME
	//    terminal gate as PostgreSQL. This is what stops a future engine from
	//    quietly adding a PostgreSQL-free green path — the exact class of
	//    mistake M0 exists to prevent.
	for name := range wf.Jobs {
		if name == "integration-pg" {
			continue
		}
		if strings.HasPrefix(name, "integration-") && !gateNeeds[name] {
			t.Errorf("ci.yml: engine integration job %q is not in ci-gate.needs — it bypasses the PostgreSQL-mandatory gate (#528)", name)
		}
	}
}
