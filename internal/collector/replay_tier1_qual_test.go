package collector

import (
	"strings"
	"testing"
)

// Deterministic (no-Docker) coverage of the #538 Replay Tier-1 semantic verifier
// VerifyReplayTier1Evidence. Mirrors the SHAPE of the exported payload rows
// (information_schema column names, string-typed cells) the production exporter
// writes for mysql_columns_v1 / mysql_index_inventory_v1 and the MariaDB
// equivalents. The live tests (//go:build *_live) exercise the real export;
// these assert the verifier's own logic — the composite-ordering and
// engine-native-type checks in particular.

func col(name, ord, ctype, nullable string) map[string]any {
	return map[string]any{
		"TABLE_SCHEMA": "signals_fixture", "TABLE_NAME": "replay_fixture",
		"COLUMN_NAME": name, "ORDINAL_POSITION": ord, "COLUMN_TYPE": ctype,
		"DATA_TYPE": strings.SplitN(ctype, "(", 2)[0], "IS_NULLABLE": nullable,
	}
}

func idx(name, seq, column, nonUnique string) map[string]any {
	return map[string]any{
		"TABLE_SCHEMA": "signals_fixture", "TABLE_NAME": "replay_fixture",
		"INDEX_NAME": name, "SEQ_IN_INDEX": seq, "COLUMN_NAME": column,
		"NON_UNIQUE": nonUnique, "INDEX_TYPE": "BTREE",
	}
}

// goodTier1Export builds a valid MySQL-shaped export (engine-native `bigint`).
func goodTier1Export() QualExport {
	return QualExport{PayloadByQuery: map[string][]map[string]any{
		"mysql_columns_v1": {
			col("id", "1", "bigint", "NO"),
			col("tenant_id", "2", "bigint", "NO"),
			col("created_at", "3", "datetime", "NO"),
			col("email", "4", "varchar(255)", "NO"),
			col("note", "5", "varchar(255)", "YES"),
			col("status", "6", "int", "NO"),
		},
		"mysql_index_inventory_v1": {
			idx("PRIMARY", "1", "id", "0"),
			idx("uq_email", "1", "email", "0"),
			idx("idx_status", "1", "status", "1"),
			idx("idx_tenant_created", "1", "tenant_id", "1"),
			idx("idx_tenant_created", "2", "created_at", "1"),
		},
	}}
}

// TC-MYC-09 / TC-MDC-10 (MYC-R009/MDC-R009): a faithful export passes.
func TestVerifyReplayTier1Evidence_Pass(t *testing.T) {
	if err := VerifyReplayTier1Evidence(goodTier1Export(), "mysql"); err != nil {
		t.Fatalf("faithful Tier-1 export must pass, got: %v", err)
	}
}

// MariaDB-native integer display width (bigint(20)/int(11)) must also pass — the
// type check is a prefix, never normalized to MySQL.
func TestVerifyReplayTier1Evidence_MariaDBNativeType(t *testing.T) {
	z := QualExport{PayloadByQuery: map[string][]map[string]any{
		"mariadb_columns_v1": {
			col("id", "1", "bigint(20)", "NO"),
			col("tenant_id", "2", "bigint(20)", "NO"),
			col("created_at", "3", "datetime", "NO"),
			col("email", "4", "varchar(255)", "NO"),
			col("note", "5", "varchar(255)", "YES"),
			col("status", "6", "int(11)", "NO"),
		},
		"mariadb_index_inventory_v1": {
			idx("PRIMARY", "1", "id", "0"),
			idx("uq_email", "1", "email", "0"),
			idx("idx_status", "1", "status", "1"),
			idx("idx_tenant_created", "1", "tenant_id", "1"),
			idx("idx_tenant_created", "2", "created_at", "1"),
		},
	}}
	if err := VerifyReplayTier1Evidence(z, "mariadb"); err != nil {
		t.Fatalf("MariaDB-native bigint(20)/int(11) must pass, got: %v", err)
	}
}

// The composite index ordering is load-bearing: swapping the two columns of
// idx_tenant_created MUST be caught (the exported evidence must preserve the
// exact (tenant_id, created_at) order).
func TestVerifyReplayTier1Evidence_CompositeOrderEnforced(t *testing.T) {
	z := goodTier1Export()
	z.PayloadByQuery["mysql_index_inventory_v1"] = []map[string]any{
		idx("PRIMARY", "1", "id", "0"),
		idx("uq_email", "1", "email", "0"),
		idx("idx_status", "1", "status", "1"),
		idx("idx_tenant_created", "1", "created_at", "1"), // swapped
		idx("idx_tenant_created", "2", "tenant_id", "1"),  // swapped
	}
	err := VerifyReplayTier1Evidence(z, "mysql")
	if err == nil {
		t.Fatal("swapped composite-index column order must fail")
	}
	if !strings.Contains(err.Error(), "idx_tenant_created") {
		t.Errorf("error must name the mis-ordered index, got: %v", err)
	}
}

// Nullability inversion and a missing table are caught.
func TestVerifyReplayTier1Evidence_NullabilityAndMissing(t *testing.T) {
	z := goodTier1Export()
	z.PayloadByQuery["mysql_columns_v1"][4] = col("note", "5", "varchar(255)", "NO") // note should be YES
	if err := VerifyReplayTier1Evidence(z, "mysql"); err == nil || !strings.Contains(err.Error(), "note") {
		t.Fatalf("nullability inversion on note must fail naming note, got: %v", err)
	}
	empty := QualExport{PayloadByQuery: map[string][]map[string]any{}}
	if err := VerifyReplayTier1Evidence(empty, "mysql"); err == nil {
		t.Fatal("absent evidence must fail, not pass vacuously")
	}
}
