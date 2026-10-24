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
	case config.EngineSingleStore:
		cat := singlestore.Catalog()
		out := make([]reportCollector, 0, len(cat))
		for _, c := range cat {
			// SingleStore collectors carry no cadence; leave Cadence/CadenceString
			// zero/empty so freshness enrichment is skipped (CadenceString == "").
			out = append(out, reportCollector{ID: c.ID})
		}
		return out
	default:
		return nil
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
