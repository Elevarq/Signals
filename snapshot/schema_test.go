package snapshot

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/elevarq/signals/internal/engineid"
)

// Tests derive from specifications/postgresql-snapshot-compat.acceptance.md
// (#528 regression gate: engine identity is additive and never changes the
// PostgreSQL snapshot shape).

func TestSetEngineIdentity(t *testing.T) {
	t.Run("TC-PGC-01 postgresql leaves engine fields empty", func(t *testing.T) {
		m := Metadata{PGVersion: "PostgreSQL 18.2 on aarch64"}
		m.SetEngineIdentity(engineid.EnginePostgreSQL, "18.2")
		if m.Engine != "" || m.EngineVersion != "" {
			t.Fatalf("PGC-R001: PostgreSQL must not set engine fields, got engine=%q version=%q", m.Engine, m.EngineVersion)
		}
		if m.PGVersion != "PostgreSQL 18.2 on aarch64" {
			t.Fatalf("PGVersion must be unchanged, got %q", m.PGVersion)
		}
	})

	t.Run("TC-PGC-02 singlestore sets engine fields", func(t *testing.T) {
		m := Metadata{}
		m.SetEngineIdentity(engineid.EngineSingleStore, "8.5.22")
		if m.Engine != engineid.EngineSingleStore || m.EngineVersion != "8.5.22" {
			t.Fatalf("PGC-R002: got engine=%q version=%q, want singlestore/8.5.22", m.Engine, m.EngineVersion)
		}
	})

	t.Run("TC-PGC-03 empty engine treated as postgresql", func(t *testing.T) {
		m := Metadata{}
		m.SetEngineIdentity("", "")
		if m.Engine != "" || m.EngineVersion != "" {
			t.Fatalf("empty engine must not write engine fields, got engine=%q", m.Engine)
		}
	})
}

func TestPostgreSQLMetadataMarshalsWithoutEngineKeys(t *testing.T) {
	// TC-PGC-04 / INV-PGC-01: a PostgreSQL snapshot stays byte-compatible with
	// signals-snapshot.v1 — no engine keys in the JSON.
	m := Metadata{
		SchemaVersion:    SchemaVersion,
		CollectorVersion: "1.2.3",
		PGVersion:        "PostgreSQL 17.4 on x86_64",
		Target:           TargetInfo{Name: "t", Host: "h", Port: 5432, DBName: "d"},
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	js := string(b)
	if strings.Contains(js, "\"engine\"") || strings.Contains(js, "engine_version") {
		t.Fatalf("INV-PGC-01: PostgreSQL JSON must not contain engine keys: %s", js)
	}
}

func TestHistoricalV1MetadataUnmarshals(t *testing.T) {
	// TC-PGC-05 / INV-PGC-02/03: a pre-engine v1 document still reads.
	const v1 = `{"schema_version":"signals-snapshot.v1","collector_version":"1.2.3","pg_version":"PostgreSQL 17.4 on x86_64","target":{"name":"t","host":"h","port":5432,"dbname":"d"}}`
	var m Metadata
	if err := json.Unmarshal([]byte(v1), &m); err != nil {
		t.Fatalf("INV-PGC-02: historical v1 must unmarshal, got %v", err)
	}
	if m.Engine != "" {
		t.Fatalf("legacy doc must leave Engine empty, got %q", m.Engine)
	}
	if m.PGVersion != "PostgreSQL 17.4 on x86_64" || m.Target.Port != 5432 {
		t.Fatalf("INV-PGC-03: existing v1 fields must be preserved, got %+v", m)
	}
}
