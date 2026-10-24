package export

import (
	"time"

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
