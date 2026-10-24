package engineid

import "testing"

// Tests derive from specifications/singlestore-engine-identity.acceptance.md.
// One case per TC-SSID-*.
func TestResolve(t *testing.T) {
	cases := []struct {
		name        string // acceptance TC id
		probe       Probe
		wantEngine  string
		wantVersion string
		wantErr     bool
	}{
		{
			name:        "TC-SSID-01 singlestore via memsql_version",
			probe:       Probe{MemsqlVersion: "8.5.22", VersionComment: "SingleStore", Version: "5.7.32"},
			wantEngine:  EngineSingleStore,
			wantVersion: "8.5.22",
		},
		{
			name:        "TC-SSID-02 mysql no singlestore signal",
			probe:       Probe{VersionComment: "MySQL Community Server - GPL", Version: "8.0.36"},
			wantEngine:  EngineMySQL,
			wantVersion: "8.0.36",
		},
		{
			name:        "TC-SSID-03 singlestore via version_comment only",
			probe:       Probe{VersionComment: "SingleStore Managed Service", Version: "5.7.32"},
			wantEngine:  EngineSingleStore,
			wantVersion: "5.7.32",
		},
		{
			name:    "TC-SSID-04 indeterminate bare version fails closed",
			probe:   Probe{Version: "5.7.32"},
			wantErr: true,
		},
		{
			name:    "TC-SSID-05 empty probe fails closed",
			probe:   Probe{},
			wantErr: true,
		},
		{
			name:        "TC-SSID-06 memsql_version wins over mysql comment",
			probe:       Probe{MemsqlVersion: "8.1.0", VersionComment: "MySQL Community Server - GPL", Version: "5.7.32"},
			wantEngine:  EngineSingleStore,
			wantVersion: "8.1.0",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			engine, version, err := Resolve(tc.probe)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got engine=%q version=%q", engine, version)
				}
				if engine != "" {
					t.Fatalf("INV-SSID-03: error case must not guess an engine, got %q", engine)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if engine != tc.wantEngine {
				t.Fatalf("engine = %q, want %q", engine, tc.wantEngine)
			}
			if version != tc.wantVersion {
				t.Fatalf("version = %q, want %q", version, tc.wantVersion)
			}
			// INV-SSID-02: a memsql_version present must never be mysql.
			if tc.probe.MemsqlVersion != "" && engine == EngineMySQL {
				t.Fatalf("INV-SSID-02 violated: memsql_version present but engine=mysql")
			}
		})
	}
}
