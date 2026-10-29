package collector

// Deterministic (no-Docker) gate for the semantic-collector-qualification
// lifecycle (specifications/semantic-collector-qualification.md). It proves the
// REQUIRED-semantic-assertion property without a live database: each engine's
// representative fixture mirrors the SHAPE of the real exported rows, exactly as
// replay_tier1_qual_test.go does for the MySQL/MariaDB Replay Tier-1 payload.
// The live tests (//go:build *_live) run the same contracts against the real
// production export; these assert the lifecycle gate + the verifier logic.

import (
	"strings"
	"testing"

	"github.com/elevarq/signals/internal/config"
)

// ssGoodTableStats mirrors a faithful SingleStore table-statistics export.
func ssGoodTableStats() QualExport {
	return QualExport{PayloadByQuery: map[string][]map[string]any{
		"singlestore_table_statistics_v1": {
			{"DATABASE_NAME": "app", "TABLE_NAME": "orders", "PARTITION_TYPE": "Master", "STORAGE_TYPE": "INMEMORY_ROWSTORE", "ROWS": "120000", "MEMORY_USE": "4194304"},
			{"DATABASE_NAME": "app", "TABLE_NAME": "customers", "PARTITION_TYPE": "Master", "STORAGE_TYPE": "COLUMNSTORE", "ROWS": "48000", "MEMORY_USE": "1048576"},
		},
	}}
}

// chGoodIdentity mirrors a faithful ClickHouse identity export (version + uptime).
func chGoodIdentity() QualExport {
	return QualExport{PayloadByQuery: map[string][]map[string]any{
		"clickhouse_identity_v1": {
			{"version": "24.8.4.13", "uptime_s": "123456"},
		},
	}}
}

// pgGoodBloat mirrors a faithful PostgreSQL bloat-estimate export.
func pgGoodBloat() QualExport {
	return QualExport{PayloadByQuery: map[string][]map[string]any{
		"bloat_estimate_v1": {
			{"schemaname": "public", "tablename": "orders", "n_live_tup": "120000"},
			{"schemaname": "public", "tablename": "customers", "n_live_tup": "48000"},
		},
	}}
}

// tier1Fixture builds a faithful Replay Tier-1 export keyed for the given engine
// (<engine>_columns_v1 / <engine>_index_inventory_v1), mirroring goodTier1Export
// but engine-prefixed so VerifyReplayTier1Evidence reads the right collector ids.
// COLUMN_TYPE "bigint" matches both MySQL-native and MariaDB's "bigint(20)" via
// the verifier's prefix check.
func tier1Fixture(engine string) QualExport {
	return QualExport{PayloadByQuery: map[string][]map[string]any{
		engine + "_columns_v1": {
			col("id", "1", "bigint", "NO"),
			col("tenant_id", "2", "bigint", "NO"),
			col("created_at", "3", "datetime", "NO"),
			col("email", "4", "varchar(255)", "NO"),
			col("note", "5", "varchar(255)", "YES"),
			col("status", "6", "int", "NO"),
		},
		engine + "_index_inventory_v1": {
			idx("PRIMARY", "1", "id", "0"),
			idx("uq_email", "1", "email", "0"),
			idx("idx_status", "1", "status", "1"),
			idx("idx_tenant_created", "1", "tenant_id", "1"),
			idx("idx_tenant_created", "2", "created_at", "1"),
		},
	}}
}

// goodFixtureFor returns the representative passing fixture for an engine.
func goodFixtureFor(engine string) QualExport {
	switch engine {
	case config.EngineMySQL, config.EngineMariaDB:
		return tier1Fixture(engine)
	case config.EngineSingleStore:
		return ssGoodTableStats()
	case config.EngineClickHouse:
		return chGoodIdentity()
	case config.EnginePostgreSQL:
		return pgGoodBloat()
	default:
		return QualExport{}
	}
}

// TC-SCQ-01: every engine carries a passing representative semantic assertion.
func TestSemanticQual_RepresentativeProofPerEngine(t *testing.T) {
	reg := SemanticQualRegistry()
	for _, engine := range config.SupportedEngines {
		contracts := reg[engine]
		var semantic *SemanticContract
		for i := range contracts {
			if contracts[i].Level == QualSemanticallyQualified {
				semantic = &contracts[i]
				break
			}
		}
		if semantic == nil {
			t.Fatalf("engine %q has no semantically_qualified representative (SCQ-R004)", engine)
		}
		v := semantic.EffectiveVerifier()
		if v == nil {
			t.Fatalf("engine %q representative %q has no value assertion (SCQ-R003)", engine, semantic.CollectorID)
		}
		if err := v(goodFixtureFor(engine)); err != nil {
			t.Fatalf("engine %q representative %q must pass its faithful fixture, got: %v", engine, semantic.CollectorID, err)
		}
	}
}

// TC-SCQ-02: no semantically_qualified contract relies on status-only evidence.
func TestSemanticQual_SemanticallyQualifiedHasTeeth(t *testing.T) {
	for engine, contracts := range SemanticQualRegistry() {
		for _, c := range contracts {
			if c.Level != QualSemanticallyQualified {
				continue
			}
			if c.EffectiveVerifier() == nil {
				t.Errorf("engine %q collector %q is semantically_qualified but has neither Fields nor a dedicated Verifier (SCQ-R003)", engine, c.CollectorID)
			}
		}
	}
}

// TC-SCQ-03: a tampered value fails the verifier deterministically, per engine.
func TestSemanticQual_TamperedValueFails(t *testing.T) {
	tamper := map[string]func(QualExport){
		// MySQL/MariaDB: break the ORDINAL_POSITION contiguity the Tier-1 verifier asserts.
		config.EngineMySQL:       func(z QualExport) { z.PayloadByQuery["mysql_columns_v1"][0]["ORDINAL_POSITION"] = "9" },
		config.EngineMariaDB:     func(z QualExport) { z.PayloadByQuery["mariadb_columns_v1"][0]["ORDINAL_POSITION"] = "9" },
		config.EngineSingleStore: func(z QualExport) { z.PayloadByQuery["singlestore_table_statistics_v1"][0]["ROWS"] = "not-a-number" },
		config.EngineClickHouse:  func(z QualExport) { z.PayloadByQuery["clickhouse_identity_v1"][0]["uptime_s"] = "not-a-number" },
		config.EnginePostgreSQL:  func(z QualExport) { z.PayloadByQuery["bloat_estimate_v1"][0]["n_live_tup"] = "lots" },
	}
	for _, engine := range config.SupportedEngines {
		// For MySQL/MariaDB the Tier-1 fixture is shared across both engines, so
		// rebuild per engine to avoid cross-test mutation.
		z := goodFixtureFor(engine)
		tamper[engine](z)
		semantic := semanticRep(t, engine)
		if err := semantic.EffectiveVerifier()(z); err == nil {
			t.Fatalf("engine %q: a tampered value must fail the verifier (SCQ-R005), but it passed", engine)
		}
	}
}

// TC-SCQ-04: a successful, shape-valid collection with a wrong value is rejected.
func TestSemanticQual_ExecutionWithoutValuesIsNotQualification(t *testing.T) {
	c := SemanticContract{
		CollectorID: "clickhouse_parts_v1", Engine: config.EngineClickHouse,
		Level:  QualSemanticallyQualified,
		Fields: []FieldContract{{Name: "parts", Kind: KindNumeric, Unit: "count", Required: true}},
	}
	// Collector "succeeded" and returned a row, but parts is non-numeric.
	z := QualExport{PayloadByQuery: map[string][]map[string]any{
		"clickhouse_parts_v1": {{"database": "app", "table": "events", "parts": "many"}},
	}}
	if err := VerifySemanticContract(z, c); err == nil {
		t.Fatal("status-success-but-wrong-value must be rejected on value grounds (SCQ-R002/INV-1)")
	}
}

// TC-SCQ-05: levels are distinct and registry entries report their real level.
func TestSemanticQual_LevelsAreDistinctAndHonest(t *testing.T) {
	seen := map[string]bool{}
	for _, l := range []CollectorQualLevel{QualExecuted, QualStructurallyValidated, QualSemanticallyQualified} {
		if seen[l.String()] {
			t.Fatalf("qualification levels must be distinct strings, duplicate: %q", l.String())
		}
		seen[l.String()] = true
	}
	if QualExecuted.String() == QualSemanticallyQualified.String() {
		t.Fatal("'collector executed' must be distinguishable from 'semantic contract validated' (SCQ-R007)")
	}
	// Every registry entry reports the level it actually declares (honesty).
	for _, contracts := range SemanticQualRegistry() {
		for _, c := range contracts {
			line := SemanticQualReportLine(c)
			if !strings.HasSuffix(line, c.Level.String()) {
				t.Errorf("report line %q does not report declared level %q", line, c.Level)
			}
		}
	}
}

// TC-SCQ-06: zero rows honor the contract's declared legitimacy.
func TestSemanticQual_ZeroRowLegitimacy(t *testing.T) {
	empty := QualExport{PayloadByQuery: map[string][]map[string]any{"x_v1": {}}}
	strict := SemanticContract{CollectorID: "x_v1", Engine: config.EngineClickHouse,
		Fields: []FieldContract{{Name: "v", Kind: KindNumeric, Required: true}}, ZeroRowLegit: false}
	if err := VerifySemanticContract(empty, strict); err == nil {
		t.Fatal("zero rows under ZeroRowLegit=false must fail (SCQ-R002)")
	}
	lenient := strict
	lenient.ZeroRowLegit = true
	if err := VerifySemanticContract(empty, lenient); err != nil {
		t.Fatalf("zero rows under ZeroRowLegit=true must pass, got: %v", err)
	}
}

// semanticRep returns an engine's first semantically_qualified contract.
func semanticRep(t *testing.T, engine string) SemanticContract {
	t.Helper()
	for _, c := range SemanticQualRegistry()[engine] {
		if c.Level == QualSemanticallyQualified {
			return c
		}
	}
	t.Fatalf("no semantically_qualified contract for engine %q", engine)
	return SemanticContract{}
}
