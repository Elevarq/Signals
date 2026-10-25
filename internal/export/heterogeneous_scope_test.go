package export

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/elevarq/signals/internal/db"
)

// Tests derive from specifications/multi-engine-report-contract.acceptance.md
// (TC-MERC-08, MERC-R009/INV-MERC-04, #622). They drive the REAL production
// exporter (Builder.WriteTo) over stores populated through the REAL persistence
// API (InsertCollectionAtomic via seedEngineRun) — no hand-inserted ZIP rows —
// proving that mixed-engine evidence can never be emitted under one engine
// identity, while scoped and homogeneous exports are unaffected.

// exportErr runs the real exporter and returns only the error.
func exportErr(store *db.DB, opts Options) error {
	return NewBuilder(store, "inst").WriteTo(&bytes.Buffer{}, opts)
}

func seedTarget(t *testing.T, store *db.DB, name string, port int) int64 {
	t.Helper()
	id, err := store.UpsertTarget(name, "h", port, "db", "u", "disable", "none", "", true)
	if err != nil {
		t.Fatalf("UpsertTarget %s: %v", name, err)
	}
	return id
}

// TC-MERC-08: an unscoped export over a two-engine store is rejected.
func TestUnscopedHeterogeneousExportRejected(t *testing.T) {
	store := openReportTestDB(t)
	pgT := seedTarget(t, store, "pg", 5432)
	ssT := seedTarget(t, store, "ss", 3306)
	seedEngineRun(t, store, "pg-snap-1", pgT, "postgresql", "pg_stat_database_v1", "2026-10-24T00:00:00Z", "PostgreSQL 18.0")
	seedEngineRun(t, store, "ss-snap-1", ssT, "singlestore", "singlestore_identity_v1", "2026-10-24T00:00:01Z", "9.1.1")

	// default (unscoped) scope.
	if err := exportErr(store, Options{}); !errors.Is(err, ErrHeterogeneousScope) {
		t.Errorf("unscoped default export over mixed store: got %v, want ErrHeterogeneousScope", err)
	}
	// --all (unscoped) scope.
	if err := exportErr(store, Options{All: true}); !errors.Is(err, ErrHeterogeneousScope) {
		t.Errorf("unscoped --all export over mixed store: got %v, want ErrHeterogeneousScope", err)
	}
}

// TC-MERC-08: an explicit TargetID export from a heterogeneous store emits only
// that target's engine, never the other.
func TestScopedExportFromHeterogeneousStoreIsSingleEngine(t *testing.T) {
	store := openReportTestDB(t)
	pgT := seedTarget(t, store, "pg", 5432)
	ssT := seedTarget(t, store, "ss", 3306)
	seedEngineRun(t, store, "pg-snap-1", pgT, "postgresql", "pg_stat_database_v1", "2026-10-24T00:00:00Z", "PostgreSQL 18.0")
	seedEngineRun(t, store, "ss-snap-1", ssT, "singlestore", "singlestore_identity_v1", "2026-10-24T00:00:01Z", "9.1.1")

	// Explicit SingleStore target: engine=singlestore, no PostgreSQL leak.
	var buf bytes.Buffer
	if err := NewBuilder(store, "inst").WriteTo(&buf, Options{TargetID: ssT}); err != nil {
		t.Fatalf("scoped SingleStore export: %v", err)
	}
	meta := exportJSONFile(t, store, Options{TargetID: ssT}, "metadata.json")
	if meta["engine"] != "singlestore" {
		t.Errorf("scoped export engine=%v, want singlestore", meta["engine"])
	}
	cs := exportJSONFile(t, store, Options{TargetID: ssT}, "collector_status.json")
	for _, id := range collectorIDs(t, cs) {
		if len(id) >= 3 && id[:3] == "pg_" {
			t.Errorf("scoped SingleStore export leaked PostgreSQL collector %q", id)
		}
	}

	// Explicit PostgreSQL target: engine metadata is postgresql (no engine_version).
	pgMeta := exportJSONFile(t, store, Options{TargetID: pgT}, "metadata.json")
	if pgMeta["engine"] != "postgresql" {
		t.Errorf("scoped PostgreSQL export engine=%v, want postgresql", pgMeta["engine"])
	}
	if _, present := pgMeta["engine_version"]; present {
		t.Errorf("scoped PostgreSQL export must not carry engine_version")
	}
}

// TC-MERC-08: homogeneous unscoped exports still succeed (backward compatibility).
func TestUnscopedHomogeneousExportSucceeds(t *testing.T) {
	t.Run("postgresql two targets", func(t *testing.T) {
		store := openReportTestDB(t)
		a := seedTarget(t, store, "pg-a", 5432)
		b := seedTarget(t, store, "pg-b", 5432)
		seedEngineRun(t, store, "pg-a-1", a, "postgresql", "pg_stat_database_v1", "2026-10-24T00:00:00Z", "PostgreSQL 18.0")
		seedEngineRun(t, store, "pg-b-1", b, "postgresql", "pg_stat_database_v1", "2026-10-24T00:00:01Z", "PostgreSQL 18.0")
		if err := exportErr(store, Options{All: true}); err != nil {
			t.Errorf("homogeneous PostgreSQL --all export: unexpected error %v", err)
		}
	})

	t.Run("legacy empty-engine PostgreSQL", func(t *testing.T) {
		store := openReportTestDB(t)
		a := seedTarget(t, store, "legacy", 5432)
		// Engine "" (legacy) normalizes to postgresql — single engine.
		seedEngineRun(t, store, "legacy-1", a, "", "pg_stat_database_v1", "2026-10-24T00:00:00Z", "PostgreSQL 15.0")
		if err := exportErr(store, Options{}); err != nil {
			t.Errorf("legacy empty-engine export: unexpected error %v", err)
		}
	})

	t.Run("single non-PostgreSQL target", func(t *testing.T) {
		store := openReportTestDB(t)
		a := seedTarget(t, store, "ch", 9000)
		seedEngineRun(t, store, "ch-1", a, "clickhouse", "clickhouse_identity_v1", "2026-10-24T00:00:00Z", "24.8")
		var buf bytes.Buffer
		if err := NewBuilder(store, "inst").WriteTo(&buf, Options{}); err != nil {
			t.Fatalf("single ClickHouse export: %v", err)
		}
		var meta map[string]any
		cs := exportJSONFile(t, store, Options{}, "metadata.json")
		b, _ := json.Marshal(cs)
		_ = json.Unmarshal(b, &meta)
		if meta["engine"] != "clickhouse" {
			t.Errorf("single ClickHouse export engine=%v, want clickhouse", meta["engine"])
		}
	})
}
