package tests

import (
	"testing"

	"github.com/elevarq/arq-signals/internal/collector"
	"github.com/elevarq/arq-signals/internal/db"
	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// ---------------------------------------------------------------------------
// R115 — extension-version gating + object_missing error class.
//
// Discovery captures pg_extension.extversion; QueryDef collectors may
// set RequiresExtensionMinVersion (dotted-numeric floor, evaluated
// only when the required extension is installed). Gate failures are
// reported under the existing version_unsupported reason; absence of
// the extension entirely keeps extension_missing precedence. The gate
// fails OPEN on unknown/unparsable versions — the run-time
// object_missing classification (SQLSTATE 42P01/42883) catches
// genuinely missing objects.
//
// First consumer: the R114 TimescaleDB family (floor "2.14",
// detection collector exempt). These tests exercise the gate through
// that real registry surface.
//
// Specification: features/arq-signals/specification.md R115
// Acceptance:    TC-TSDB-12, TC-TSDB-14
// ---------------------------------------------------------------------------

// timescaleDBVersionGatedIDs is every family member that carries the
// 2.14 floor — all of them except the detection collector, which must
// run on any TimescaleDB version.
func timescaleDBVersionGatedIDs() []string {
	var out []string
	for _, id := range timescaleDBFamilyIDs {
		if id != "timescaledb_extension_v1" {
			out = append(out, id)
		}
	}
	return out
}

// TC-TSDB-12: extension installed but below the family floor — gated
// members drop out of Filter and surface as version_unsupported;
// detection stays eligible.
func TestExtensionVersionGateBlocksBelowFloor(t *testing.T) {
	params := pgqueries.FilterParams{
		PGMajorVersion:         16,
		Extensions:             []string{"timescaledb"},
		ExtensionVersions:      map[string]string{"timescaledb": "2.13.2"},
		HighSensitivityEnabled: true,
	}
	eligible := filteredIDSet(params)
	if !eligible["timescaledb_extension_v1"] {
		t.Error("timescaledb_extension_v1 must stay eligible on TimescaleDB < 2.14 (detection-only tier)")
	}
	for _, id := range timescaleDBVersionGatedIDs() {
		if eligible[id] {
			t.Errorf("%s eligible on TimescaleDB 2.13.2 — floor is 2.14", id)
		}
	}

	gated := pgqueries.GatedIDsByReason(params)
	unsupported := make(map[string]bool)
	for _, id := range gated[pgqueries.GateReasonVersionUnsupported] {
		unsupported[id] = true
	}
	for _, id := range timescaleDBVersionGatedIDs() {
		if !unsupported[id] {
			t.Errorf("%s not reported under reason=%s on TimescaleDB 2.13.2",
				id, pgqueries.GateReasonVersionUnsupported)
		}
	}
	if unsupported["timescaledb_extension_v1"] {
		t.Error("timescaledb_extension_v1 wrongly reported version_unsupported")
	}
}

// Floor and above pass: "2.14" itself, a patch release of the floor,
// and a current version.
func TestExtensionVersionGateAllowsAtAndAboveFloor(t *testing.T) {
	for _, version := range []string{"2.14", "2.14.0", "2.27.2"} {
		eligible := filteredIDSet(pgqueries.FilterParams{
			PGMajorVersion:         17,
			Extensions:             []string{"timescaledb"},
			ExtensionVersions:      map[string]string{"timescaledb": version},
			HighSensitivityEnabled: true,
		})
		for _, id := range timescaleDBFamilyIDs {
			if !eligible[id] {
				t.Errorf("%s not eligible on TimescaleDB %s", id, version)
			}
		}
	}
}

// Fail-open cases: version unknown to discovery (nil map / missing
// entry) or unparsable. The gate must never block on uncertainty —
// the run-time object_missing path is the backstop (R115).
func TestExtensionVersionGateFailsOpen(t *testing.T) {
	cases := []struct {
		name     string
		versions map[string]string
	}{
		{"nil version map", nil},
		{"extension missing from version map", map[string]string{"vector": "0.8.0"}},
		{"unparsable version string", map[string]string{"timescaledb": "garbage"}},
	}
	for _, tc := range cases {
		eligible := filteredIDSet(pgqueries.FilterParams{
			PGMajorVersion:         17,
			Extensions:             []string{"timescaledb"},
			ExtensionVersions:      tc.versions,
			HighSensitivityEnabled: true,
		})
		for _, id := range timescaleDBFamilyIDs {
			if !eligible[id] {
				t.Errorf("%s: %s not eligible — gate must fail open", tc.name, id)
			}
		}
	}
}

// Suffixed versions compare by their numeric prefix: a dev build of a
// too-old line is still blocked; a dev build at the floor passes.
func TestExtensionVersionGateParsesNumericPrefix(t *testing.T) {
	blocked := filteredIDSet(pgqueries.FilterParams{
		PGMajorVersion:         16,
		Extensions:             []string{"timescaledb"},
		ExtensionVersions:      map[string]string{"timescaledb": "2.13.0-dev"},
		HighSensitivityEnabled: true,
	})
	if blocked["timescaledb_hypertables_v1"] {
		t.Error("2.13.0-dev passed a 2.14 floor — numeric prefix must compare as 2.13.0")
	}
	allowed := filteredIDSet(pgqueries.FilterParams{
		PGMajorVersion:         16,
		Extensions:             []string{"timescaledb"},
		ExtensionVersions:      map[string]string{"timescaledb": "2.14.0-dev"},
		HighSensitivityEnabled: true,
	})
	if !allowed["timescaledb_hypertables_v1"] {
		t.Error("2.14.0-dev blocked by a 2.14 floor — numeric prefix must compare as 2.14.0")
	}
}

// Extension absent entirely: extension_missing keeps precedence over
// the version gate, even when a (stale) version entry exists.
func TestExtensionMissingTakesPrecedenceOverVersionGate(t *testing.T) {
	gated := pgqueries.GatedIDsByReason(pgqueries.FilterParams{
		PGMajorVersion:         17,
		Extensions:             nil,
		ExtensionVersions:      map[string]string{"timescaledb": "2.13.0"},
		HighSensitivityEnabled: true,
	})
	missing := make(map[string]bool)
	for _, id := range gated[pgqueries.GateReasonExtensionMissing] {
		missing[id] = true
	}
	unsupported := make(map[string]bool)
	for _, id := range gated[pgqueries.GateReasonVersionUnsupported] {
		unsupported[id] = true
	}
	for _, id := range timescaleDBFamilyIDs {
		if !missing[id] {
			t.Errorf("%s not under extension_missing when the extension is absent", id)
		}
		if unsupported[id] {
			t.Errorf("%s under version_unsupported when extension_missing should take precedence", id)
		}
	}
}

// TC-TSDB-14: SQLSTATE 42P01 / 42883 failures classify as the
// structured object_missing reason; existing classifications are
// untouched.
func TestObjectMissingErrorClassification(t *testing.T) {
	cases := []struct {
		name   string
		errMsg string
		want   string
	}{
		{
			"undefined table",
			`ERROR: relation "timescaledb_information.jobs" does not exist (SQLSTATE 42P01)`,
			"object_missing",
		},
		{
			"undefined function",
			`ERROR: function hypertable_compression_stats(regclass) does not exist (SQLSTATE 42883)`,
			"object_missing",
		},
		{
			"permission denied still classified first",
			`ERROR: permission denied for view job_errors (SQLSTATE 42501)`,
			"permission_denied",
		},
		{
			"generic failure unchanged",
			`ERROR: division by zero (SQLSTATE 22012)`,
			"execution_error",
		},
	}
	for _, tc := range cases {
		statuses := collector.BuildStatusFromRuns([]db.QueryRun{{
			QueryID: "timescaledb_jobs_v1",
			Error:   tc.errMsg,
		}})
		if len(statuses) != 1 {
			t.Fatalf("%s: got %d statuses, want 1", tc.name, len(statuses))
		}
		got := statuses[0]
		if got.Status != "failed" {
			t.Errorf("%s: status = %q, want failed", tc.name, got.Status)
		}
		if got.Reason != tc.want {
			t.Errorf("%s: reason = %q, want %q", tc.name, got.Reason, tc.want)
		}
	}
}
