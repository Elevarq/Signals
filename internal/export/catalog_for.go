package export

import (
	"time"

	"github.com/elevarq/signals/internal/clickhouse"
	"github.com/elevarq/signals/internal/config"
	"github.com/elevarq/signals/internal/pgqueries"
	"github.com/elevarq/signals/internal/singlestore"
)

// reportCollector is the engine-neutral reporting view of one collector: its id
// and (where the engine has the concept) its cadence. SingleStore has no
// cadence concept, so CadenceString is "" and freshness enrichment is skipped
// for it.
type reportCollector struct {
	ID            string
	Cadence       time.Duration
	CadenceString string
}

// catalogFor returns the reporting catalog of the given engine (MKR-R001,
// MKR-R002, INV-MKR-03). It is the SINGLE seam every export enumeration
// (freshness, never_run, query_catalog filtering) goes through, so a
// non-PostgreSQL export never inherits the PostgreSQL catalog. An unrecognized
// engine yields an empty catalog — never a PostgreSQL fallback.
func catalogFor(engine string) []reportCollector {
	switch engine {
	case config.EnginePostgreSQL, "":
		all := pgqueries.All()
		out := make([]reportCollector, 0, len(all))
		for _, q := range all {
			out = append(out, reportCollector{
				ID:            q.ID,
				Cadence:       time.Duration(q.Cadence),
				CadenceString: q.Cadence.String(),
			})
		}
		return out
	case config.EngineClickHouse:
		// ClickHouse is its own backend (not MySQL-wire); its reporting catalog
		// comes from the clickhouse package. No cadence concept → freshness
		// enrichment is skipped (CadenceString == "").
		cat := clickhouse.Catalog()
		out := make([]reportCollector, 0, len(cat))
		for _, c := range cat {
			out = append(out, reportCollector{ID: c.ID})
		}
		return out
	default:
		// Every MySQL-wire engine (singlestore, mysql, …) goes through the SINGLE
		// catalog registry, so the reporting seam stays in lockstep with the
		// collection catalog and a new wire engine needs no edit here. An
		// unrecognized engine yields nil (no PostgreSQL fallback). Wire
		// collectors carry no cadence, so CadenceString stays empty and freshness
		// enrichment is skipped.
		cat := singlestore.CatalogFor(engine)
		if cat == nil {
			return nil
		}
		out := make([]reportCollector, 0, len(cat))
		for _, c := range cat {
			out = append(out, reportCollector{ID: c.ID})
		}
		return out
	}
}

// engineOf reports the engine an export describes — the scoped snapshot's
// engine, defaulting empty/legacy to PostgreSQL (the same rule metadata.json's
// engine field uses).
func engineOf(scope *exportScope) string {
	if len(scope.snapshots) > 0 && scope.snapshots[0].Engine != "" {
		return scope.snapshots[0].Engine
	}
	return config.EnginePostgreSQL
}

// setExportVersionFields writes the server-version identity onto an export
// row (snapshots.ndjson / query_runs.ndjson / query_results.ndjson) per
// MKR-R008 (#531). The version is stored in the legacy `PGVersion` column for
// every engine, but it must NOT be serialized as `pg_version` for a
// non-PostgreSQL engine — a real SingleStore export was emitting
// engine_version 9.1.1 as `pg_version` (the 2026-10-23 forensic defect).
//
//   - PostgreSQL (incl. the empty/legacy sentinel): keep `pg_version` as the
//     carrier so historical PostgreSQL archives/consumers stay byte-identical
//     (INV-MKR-02). No engine_version field is added.
//   - Any other engine: drop `pg_version` entirely and carry the truthful
//     identity as explicit `engine` + `engine_version`.
func setExportVersionFields(row map[string]any, engine, version string) {
	if engine == "" || engine == config.EnginePostgreSQL {
		row["pg_version"] = version
		return
	}
	row["engine"] = engine
	row["engine_version"] = version
}
