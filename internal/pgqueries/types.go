package pgqueries

import "time"

// ResultKind describes the shape of a query's result.
type ResultKind string

const (
	ResultScalar ResultKind = "scalar"
	ResultRowset ResultKind = "rowset"
)

// RetentionClass hints how long results should be kept.
type RetentionClass string

const (
	RetentionShort  RetentionClass = "short"
	RetentionMedium RetentionClass = "medium"
	RetentionLong   RetentionClass = "long"
)

// Cadence controls how often a query should be executed.
type Cadence time.Duration

const (
	Cadence5m     Cadence = Cadence(5 * time.Minute)
	Cadence15m    Cadence = Cadence(15 * time.Minute)
	Cadence1h     Cadence = Cadence(1 * time.Hour)
	Cadence6h     Cadence = Cadence(6 * time.Hour)
	CadenceDaily  Cadence = Cadence(24 * time.Hour)
	CadenceWeekly Cadence = Cadence(7 * 24 * time.Hour)
)

// CadenceDefault is used when a query has zero-value cadence.
const CadenceDefault = Cadence1h

// Duration returns the cadence as a time.Duration.
// Returns CadenceDefault if zero.
func (c Cadence) Duration() time.Duration {
	if c == 0 {
		return time.Duration(CadenceDefault)
	}
	return time.Duration(c)
}

// String returns a human-readable label for the cadence.
func (c Cadence) String() string {
	if c == 0 {
		c = CadenceDefault
	}
	switch c {
	case Cadence5m:
		return "5m"
	case Cadence15m:
		return "15m"
	case Cadence1h:
		return "1h"
	case Cadence6h:
		return "6h"
	case CadenceDaily:
		return "24h"
	case CadenceWeekly:
		return "7d"
	default:
		return time.Duration(c).String()
	}
}

// QueryDef defines a single versioned SQL query.
type QueryDef struct {
	ID                string
	Category          string
	RequiresExtension string
	// RequiresExtensionMinVersion optionally raises the floor for the
	// extension named in RequiresExtension (R115). Dotted-numeric
	// comparison ("2.14" vs installed "2.27.2"); evaluated only when
	// RequiresExtension is set and installed. Fail-open: when the
	// installed version is unknown to discovery or unparsable, the
	// gate does not block — the run-time `object_missing` error class
	// catches genuinely missing objects instead.
	RequiresExtensionMinVersion string
	SQL                         string
	MinPGVersion                int
	ResultKind                  ResultKind
	RetentionClass              RetentionClass
	Timeout                     time.Duration
	Cadence                     Cadence
	// HighSensitivity flags collectors that emit application-authored
	// SQL text (view/matview/trigger/function definitions) or live
	// pg_stat_activity statement text (long-running txns, blocking
	// locks, idle-in-txn, wraparound blockers). Per R075 these run by
	// default (collect-everything default); operators opt **out** by
	// setting `high_sensitivity_collectors_enabled = false`. The
	// opt-out behavior depends on SensitiveColumns (see below).
	HighSensitivity bool
	// SensitiveColumns lists the columns to NULL-out when the operator
	// has opted out of high-sensitivity collection (R075, redact path).
	// Non-empty SensitiveColumns puts the collector on the **redact**
	// path: it still runs when opted out, but the listed columns are
	// zeroed in every persisted row so non-sensitive columns survive.
	// Empty/nil SensitiveColumns keeps the historical **skip** path:
	// the collector is dropped from the eligible set when opted out and
	// recorded `status=skipped, reason=config_disabled`. Used by Filter
	// and by the collector's per-row redaction step.
	SensitiveColumns []string
	// OwnerOnlyDegrade marks collectors that read a system catalog whose
	// PUBLIC SELECT is revoked — specifically pg_statistic_ext_data (the
	// same posture as pg_statistic). A least-privilege monitoring role
	// (pg_monitor / pg_read_all_stats) cannot read it and gets a hard
	// permission-denied (SQLSTATE 42501) on the relation; access requires
	// superuser or an explicit GRANT. For these collectors a
	// permission-denied error is an EXPECTED privilege boundary, not a
	// fault — the run is recorded `status=skipped,
	// reason=privilege_owner_only` rather than failed, so the cycle is
	// not reported partial. See
	// specifications/owner_only_privilege_degradation.md (#200).
	OwnerOnlyDegrade bool
}

// FilterParams controls which queries are eligible for a given target.
type FilterParams struct {
	PGMajorVersion int
	Extensions     []string
	// ExtensionVersions maps installed extension name → extversion
	// (from discovery, R115). Consulted only by collectors that set
	// RequiresExtensionMinVersion. A nil/empty map, or a missing
	// entry for an installed extension, never blocks eligibility
	// (fail-open).
	ExtensionVersions      map[string]string
	HighSensitivityEnabled bool

	// R098: optional per-target profile overrides. Empty values
	// mean "no per-target filter" — caller gets the daemon-wide
	// eligibility. Profile NEVER widens eligibility beyond
	// HighSensitivityEnabled (INV-SENS-01).
	//
	// ProfileRestricted = drop every QueryDef with HighSensitivity=true.
	// IncludeOnly = if non-nil and non-empty, keep ONLY listed IDs.
	// Exclude = drop listed IDs.
	ProfileRestricted bool
	IncludeOnly       map[string]bool
	Exclude           map[string]bool

	// #128: per-collector opt-in flag for pg_stats_array_range_v1
	// (per-element MCV + range histograms). Layered ON TOP OF
	// HighSensitivityEnabled — both must be true for the
	// collector to run. When HighSensitivityEnabled=false this
	// flag has no effect.
	CollectArrayRangeHistograms bool
}

// RequiresArrayRangeOptIn is the closed list of QueryDef IDs that
// require the #128 per-collector opt-in flag in addition to the
// daemon-wide HighSensitivityEnabled floor. Centralised here so
// the filter logic is auditable without grepping for ID strings.
var RequiresArrayRangeOptIn = map[string]bool{
	"pg_stats_array_range_v1": true,
}
