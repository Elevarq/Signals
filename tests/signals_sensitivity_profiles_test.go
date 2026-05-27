package tests

import (
	"strings"
	"testing"

	"github.com/elevarq/arq-signals/internal/config"
	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// ---------------------------------------------------------------------------
// R098 / per-target sensitivity profiles — Filter + GatedIDsByReason behaviour
// + ValidateStrict.
//
// Spec: specifications/sensitivity-profiles.md
// ---------------------------------------------------------------------------

// containsID is a small test helper.
func containsID(queries []pgqueries.QueryDef, id string) bool {
	for _, q := range queries {
		if q.ID == id {
			return true
		}
	}
	return false
}

// TC-SENS-01: profile=restricted drops every HighSensitivity collector
// even when the daemon-wide gate is enabled.
func TestSensitivity_RestrictedDropsHighSensitivityCollectors(t *testing.T) {
	withGate := pgqueries.FilterParams{
		PGMajorVersion: 16, Extensions: []string{},
		HighSensitivityEnabled: true,
	}
	baseline := pgqueries.Filter(withGate)
	if !containsID(baseline, "pg_views_definitions_v1") {
		t.Fatalf("precondition: with daemon-wide gate on, baseline must include pg_views_definitions_v1")
	}

	restricted := withGate
	restricted.ProfileRestricted = true
	out := pgqueries.Filter(restricted)
	for _, q := range out {
		if q.HighSensitivity {
			t.Errorf("restricted profile must drop HighSensitivity collector %q", q.ID)
		}
	}
}

// TC-SENS-02: profile=custom with IncludeOnly keeps only listed IDs.
func TestSensitivity_CustomIncludeOnly(t *testing.T) {
	p := pgqueries.FilterParams{
		PGMajorVersion: 16, Extensions: []string{},
		IncludeOnly: map[string]bool{
			"pg_settings_v1":                true,
			"pg_stat_activity_v1":           true,
			"this_collector_does_not_exist": true,
		},
	}
	out := pgqueries.Filter(p)
	if len(out) == 0 {
		t.Fatal("custom IncludeOnly with valid IDs must yield non-empty result")
	}
	allowed := map[string]bool{"pg_settings_v1": true, "pg_stat_activity_v1": true}
	for _, q := range out {
		if !allowed[q.ID] {
			t.Errorf("custom IncludeOnly leaked unexpected collector %q", q.ID)
		}
	}
}

// TC-SENS-03: profile=custom with Exclude drops only listed IDs.
func TestSensitivity_CustomExclude(t *testing.T) {
	p := pgqueries.FilterParams{
		PGMajorVersion: 16, Extensions: []string{},
		HighSensitivityEnabled: true,
		Exclude:                map[string]bool{"pg_views_definitions_v1": true},
	}
	out := pgqueries.Filter(p)
	if containsID(out, "pg_views_definitions_v1") {
		t.Errorf("Exclude must drop pg_views_definitions_v1")
	}
	// Sanity: other collectors should still be present.
	if !containsID(out, "pg_settings_v1") {
		t.Errorf("Exclude must not affect unrelated collectors")
	}
}

// INV-SENS-01: profile NEVER widens eligibility. If the daemon-wide
// gate is OFF, a target's profile=default cannot pull in
// HighSensitivity collectors.
func TestSensitivity_ProfileNeverWidensBeyondDaemonWide(t *testing.T) {
	// Daemon-wide gate off. Even a custom profile that explicitly
	// includes a HighSensitivity collector must not pull it in.
	p := pgqueries.FilterParams{
		PGMajorVersion: 16, Extensions: []string{},
		HighSensitivityEnabled: false,
		IncludeOnly:            map[string]bool{"pg_views_definitions_v1": true},
	}
	out := pgqueries.Filter(p)
	if containsID(out, "pg_views_definitions_v1") {
		t.Error("profile must not widen beyond the daemon-wide HighSensitivity gate (INV-SENS-01)")
	}
}

// GatedIDsByReason classifies per-target drops under config_disabled.
func TestSensitivity_GatedByReason_RestrictedFallsUnderConfigDisabled(t *testing.T) {
	p := pgqueries.FilterParams{
		PGMajorVersion: 16, Extensions: []string{},
		HighSensitivityEnabled: true,
		ProfileRestricted:      true,
	}
	gated := pgqueries.GatedIDsByReason(p)
	configDisabled := gated[pgqueries.GateReasonConfigDisabled]
	if len(configDisabled) == 0 {
		t.Fatal("restricted profile must produce config_disabled gate entries")
	}
	// Every entry must be a HighSensitivity collector.
	found := false
	for _, id := range configDisabled {
		if id == "pg_views_definitions_v1" {
			found = true
		}
	}
	if !found {
		t.Errorf("config_disabled must include pg_views_definitions_v1; got %v", configDisabled)
	}
}

// FC-19: invalid profile value rejected at ValidateStrict.
func TestSensitivity_ValidateStrictRejectsInvalidProfile(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Targets = []config.TargetConfig{
		{
			Name: "bad-profile", Host: "h", Port: 5432, DBName: "d", User: "u",
			SSLMode: "disable", Enabled: true,
			Collectors: config.TargetCollectorConfig{Profile: "paranoid"},
		},
	}
	_, err := config.ValidateStrict(cfg)
	if err == nil {
		t.Fatal("ValidateStrict must reject unknown profile value")
	}
	if !strings.Contains(err.Error(), "paranoid") {
		t.Errorf("error must name the offending value; got %v", err)
	}
}

// FC-20: include + exclude on the same ID rejected.
func TestSensitivity_ValidateStrictRejectsIncludeExcludeOverlap(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Targets = []config.TargetConfig{
		{
			Name: "conflict", Host: "h", Port: 5432, DBName: "d", User: "u",
			SSLMode: "disable", Enabled: true,
			Collectors: config.TargetCollectorConfig{
				Profile: "custom",
				Include: []string{"pg_settings_v1"},
				Exclude: []string{"pg_settings_v1"},
			},
		},
	}
	_, err := config.ValidateStrict(cfg)
	if err == nil {
		t.Fatal("ValidateStrict must reject include+exclude on same ID")
	}
	if !strings.Contains(err.Error(), "pg_settings_v1") {
		t.Errorf("error must name the conflicting ID; got %v", err)
	}
}

// Empty / "default" profile is a no-op — same as no override.
func TestSensitivity_DefaultProfileIsNoOp(t *testing.T) {
	for _, profile := range []string{"", "default"} {
		cfg := config.DefaultConfig()
		cfg.Targets = []config.TargetConfig{
			{
				Name: "ok", Host: "h", Port: 5432, DBName: "d", User: "u",
				SSLMode: "disable", Enabled: true,
				Collectors: config.TargetCollectorConfig{Profile: profile},
			},
		}
		if _, err := config.ValidateStrict(cfg); err != nil {
			t.Errorf("profile=%q must validate cleanly; got %v", profile, err)
		}
	}
}
