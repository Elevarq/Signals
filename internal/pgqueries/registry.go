package pgqueries

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

var registry []QueryDef
var registryByID = map[string]*QueryDef{}

// overrideRegistry holds version-specific SQL overrides. Keyed first by
// PG major (14, 15, 16, 17, 18, …), then by logical query ID. R081:
// catalog_pgN.go init() functions populate this map; Filter() consults
// it when resolving the effective SQL for the connected server's major.
//
// A logical ID's *default* SQL lives in the version-agnostic catalog
// files (`catalog.go`, `catalog_io.go`, etc.). Overrides are exceptions
// for collectors whose underlying catalog view changed between PG
// majors (e.g. pg_stat_io's column rename in PG 18). Logical IDs stay
// stable across versions — consumers see the same query_id, only the
// SQL underneath differs.
var overrideRegistry = map[int]map[string]string{}

// validCadences is the set of allowed Cadence values (zero included as default).
var validCadences = map[Cadence]bool{
	0:             true,
	Cadence5m:     true,
	Cadence15m:    true,
	Cadence1h:     true,
	Cadence6h:     true,
	CadenceDaily:  true,
	CadenceWeekly: true,
}

// Register adds a query definition to the global registry.
// It panics if the query fails lint, has a duplicate ID, or uses an invalid cadence.
// Must be called from init().
func Register(q QueryDef) {
	if err := LintQuery(q.SQL); err != nil {
		panic(fmt.Sprintf("pgqueries.Register(%q): lint failed: %v", q.ID, err))
	}
	if _, exists := registryByID[q.ID]; exists {
		panic(fmt.Sprintf("pgqueries.Register(%q): duplicate ID", q.ID))
	}
	if !validCadences[q.Cadence] {
		panic(fmt.Sprintf("pgqueries.Register(%q): invalid cadence %v", q.ID, q.Cadence))
	}
	registry = append(registry, q)
	registryByID[q.ID] = &registry[len(registry)-1]
}

// RegisterOverride records SQL that replaces the default for a logical
// collector when the connected target runs the given PG major. R081:
// called from catalog_pgN.go init(); the SQL is lint-checked at
// registration time exactly like the default registry. Overriding a
// non-existent logical ID, or registering a duplicate override for the
// same (major, id) pair, panics — both conditions indicate a coding
// error in the per-major catalog files.
func RegisterOverride(major int, id, sql string) {
	if _, exists := registryByID[id]; !exists {
		panic(fmt.Sprintf("pgqueries.RegisterOverride(major=%d, id=%q): unknown logical id; must be registered in the default catalog first", major, id))
	}
	if err := LintQuery(sql); err != nil {
		panic(fmt.Sprintf("pgqueries.RegisterOverride(major=%d, id=%q): lint failed: %v", major, id, err))
	}
	if overrideRegistry[major] == nil {
		overrideRegistry[major] = map[string]string{}
	}
	if _, dup := overrideRegistry[major][id]; dup {
		panic(fmt.Sprintf("pgqueries.RegisterOverride(major=%d, id=%q): duplicate override", major, id))
	}
	overrideRegistry[major][id] = sql
}

// extVersionGateBlocks reports whether q's RequiresExtensionMinVersion
// gate blocks eligibility under p (R115). It returns true only when:
// the gate is configured, the required extension is installed, its
// version is known to discovery, both versions parse as dotted
// numerics, and installed < minimum. Every uncertain case — no
// version captured, unparsable string — fails OPEN (does not block):
// the run-time `object_missing` error classification catches genuinely
// missing objects instead, and fail-closed would silently drop
// coverage on exotic-but-working builds.
func extVersionGateBlocks(q QueryDef, p FilterParams) bool {
	if q.RequiresExtension == "" || q.RequiresExtensionMinVersion == "" {
		return false
	}
	installed, known := p.ExtensionVersions[q.RequiresExtension]
	if !known {
		return false
	}
	cmp, ok := compareDottedVersions(installed, q.RequiresExtensionMinVersion)
	return ok && cmp < 0
}

// compareDottedVersions compares two dotted-numeric version strings
// component-wise ("2.14" vs "2.27.2" → -1). Missing components count
// as zero ("2.14" == "2.14.0"). Each component is parsed as its
// leading decimal digits, so common suffixes ("2.14.0-dev") compare by
// their numeric prefix. Returns ok=false when either string yields no
// digits in some compared component — callers treat that as unknown.
func compareDottedVersions(a, b string) (cmp int, ok bool) {
	as := strings.Split(strings.TrimSpace(a), ".")
	bs := strings.Split(strings.TrimSpace(b), ".")
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		av, aok := leadingInt(as, i)
		bv, bok := leadingInt(bs, i)
		if !aok || !bok {
			return 0, false
		}
		if av != bv {
			if av < bv {
				return -1, true
			}
			return 1, true
		}
	}
	return 0, true
}

// leadingInt parses the leading decimal digits of parts[i]; an index
// past the end is zero (so "2.14" and "2.14.0" compare equal). A
// present-but-digitless component is not ok.
func leadingInt(parts []string, i int) (int, bool) {
	if i >= len(parts) {
		return 0, true
	}
	s := parts[i]
	j := 0
	for j < len(s) && s[j] >= '0' && s[j] <= '9' {
		j++
	}
	if j == 0 {
		return 0, false
	}
	v, err := strconv.Atoi(s[:j])
	if err != nil {
		return 0, false
	}
	return v, true
}

// resolveSQL returns the effective SQL for `id` against the connected
// server's major. If a version-specific override exists, that wins;
// otherwise the default SQL from Register is used.
func resolveSQL(id string, major int, defaultSQL string) string {
	if m := overrideRegistry[major]; m != nil {
		if sql, ok := m[id]; ok {
			return sql
		}
	}
	return defaultSQL
}

// HasOverride reports whether (major, id) has a registered override.
// Used by tests; not on the hot path.
func HasOverride(major int, id string) bool {
	if m := overrideRegistry[major]; m != nil {
		_, ok := m[id]
		return ok
	}
	return false
}

// All returns all registered queries sorted by ID.
func All() []QueryDef {
	out := make([]QueryDef, len(registry))
	copy(out, registry)
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

// Filter returns queries eligible for the given PG version and extensions.
// High-sensitivity queries are excluded unless p.HighSensitivityEnabled is
// set; the collector emits a skipped/config_disabled status for those
// separately so operators can see the gate is active.
//
// R081: each returned QueryDef has its `SQL` field already resolved
// against `overrideRegistry[p.PGMajorVersion]`. Callers do not need to
// know whether they're getting default or version-specific SQL; the
// logical ID is stable either way.
func Filter(p FilterParams) []QueryDef {
	extSet := make(map[string]bool, len(p.Extensions))
	for _, e := range p.Extensions {
		extSet[e] = true
	}

	var out []QueryDef
	for _, q := range registry {
		if q.MinPGVersion > 0 && p.PGMajorVersion < q.MinPGVersion {
			continue
		}
		if q.RequiresExtension != "" && !extSet[q.RequiresExtension] {
			continue
		}
		// R115: optional minimum version of the required extension.
		// Evaluated after presence (extension_missing wins when the
		// extension is absent entirely); fail-open on unknown or
		// unparsable versions.
		if extVersionGateBlocks(q, p) {
			continue
		}
		// R075 (revised 2026-05): when opted out, only collectors whose
		// row is *itself* the sensitive payload are dropped here
		// (SensitiveColumns empty/nil — the skip path). Collectors with
		// non-empty SensitiveColumns stay eligible; the collector loop
		// nulls those columns in each row at execution time (redact path).
		if q.HighSensitivity && !p.HighSensitivityEnabled && len(q.SensitiveColumns) == 0 {
			continue
		}
		// #128 — per-collector opt-in for pg_stats_array_range_v1
		// and any future entries in RequiresArrayRangeOptIn.
		// Layered AFTER the HighSensitivityEnabled floor so this
		// can only NARROW eligibility, never widen it.
		// INV-SENS-01 still holds.
		if RequiresArrayRangeOptIn[q.ID] && !p.CollectArrayRangeHistograms {
			continue
		}
		// R098 per-target profile gates, layered AFTER the daemon-
		// wide gates. profile NEVER widens eligibility (INV-SENS-01).
		if p.ProfileRestricted && q.HighSensitivity {
			continue
		}
		if len(p.IncludeOnly) > 0 && !p.IncludeOnly[q.ID] {
			continue
		}
		if p.Exclude[q.ID] {
			continue
		}
		// Copy and swap SQL with the resolved (override or default).
		resolved := q
		resolved.SQL = resolveSQL(q.ID, p.PGMajorVersion, q.SQL)
		out = append(out, resolved)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

// HighSensitivityIDs returns the IDs of all registered high-sensitivity
// queries that are eligible for the given PG version and extensions but
// are gated off by HighSensitivityEnabled. Used to emit
// status=skipped/reason=config_disabled entries in collector_status.json.
func HighSensitivityIDs(p FilterParams) []string {
	if p.HighSensitivityEnabled {
		return nil
	}
	extSet := make(map[string]bool, len(p.Extensions))
	for _, e := range p.Extensions {
		extSet[e] = true
	}
	var out []string
	for _, q := range registry {
		// Only the **skip-path** high-sensitivity collectors land here
		// — the redact-path ones (SensitiveColumns non-empty) keep
		// running with redacted columns and are NOT recorded as
		// skipped/config_disabled (R075 revised).
		if !q.HighSensitivity || len(q.SensitiveColumns) > 0 {
			continue
		}
		if q.MinPGVersion > 0 && p.PGMajorVersion < q.MinPGVersion {
			continue
		}
		if q.RequiresExtension != "" && !extSet[q.RequiresExtension] {
			continue
		}
		if extVersionGateBlocks(q, p) {
			continue
		}
		out = append(out, q.ID)
	}
	sort.Strings(out)
	return out
}

// Reason values for GatedIDsByReason. Stable wire constants — they
// land in collector_status.json and audit events.
const (
	GateReasonVersionUnsupported = "version_unsupported"
	GateReasonExtensionMissing   = "extension_missing"
	GateReasonConfigDisabled     = "config_disabled"
)

// GatedIDsByReason returns, for each gating reason, the IDs of
// registered collectors that are not eligible to run against the
// connected target. A collector that fails multiple gates appears
// under exactly one reason, ordered by precedence:
//
//  1. version_unsupported — MinPGVersion > p.PGMajorVersion
//  2. extension_missing  — RequiresExtension is not present
//  3. version_unsupported — extension present but older than
//     RequiresExtensionMinVersion (R115)
//  4. config_disabled    — HighSensitivity but not enabled
//
// This drives collector_status.json so the operator sees every
// registered collector accounted for in each cycle, never silently
// skipped. Output map keys are the constants above; values are
// sorted ascending by ID. Missing keys mean no collectors were
// gated for that reason.
func GatedIDsByReason(p FilterParams) map[string][]string {
	extSet := make(map[string]bool, len(p.Extensions))
	for _, e := range p.Extensions {
		extSet[e] = true
	}
	out := map[string][]string{}
	for _, q := range registry {
		switch {
		case q.MinPGVersion > 0 && p.PGMajorVersion < q.MinPGVersion:
			out[GateReasonVersionUnsupported] = append(out[GateReasonVersionUnsupported], q.ID)
		case q.RequiresExtension != "" && !extSet[q.RequiresExtension]:
			out[GateReasonExtensionMissing] = append(out[GateReasonExtensionMissing], q.ID)
		case extVersionGateBlocks(q, p):
			// R115: extension installed but older than the collector's
			// floor. Same operator-facing bucket as the PG-major gate —
			// the target's version is what makes it ineligible.
			out[GateReasonVersionUnsupported] = append(out[GateReasonVersionUnsupported], q.ID)
		case q.HighSensitivity && len(q.SensitiveColumns) == 0 && !p.HighSensitivityEnabled:
			// R075 revised: only skip-path collectors are reported as
			// config_disabled. Redact-path collectors keep running.
			out[GateReasonConfigDisabled] = append(out[GateReasonConfigDisabled], q.ID)
		case RequiresArrayRangeOptIn[q.ID] && !p.CollectArrayRangeHistograms:
			// #128 / issue #18: the array-range per-collector opt-in
			// narrows eligibility on top of HighSensitivityEnabled. When
			// the opt-in is off but the collector is otherwise eligible
			// it must still surface in collector_status.json as
			// skipped/config_disabled (EA-R001) — silent absence would
			// hide a coverage gap from operators.
			out[GateReasonConfigDisabled] = append(out[GateReasonConfigDisabled], q.ID)
		case p.ProfileRestricted && q.HighSensitivity:
			// R098: per-target restricted profile drops every
			// HighSensitivity collector. The reason channel is
			// the same operator-state bucket (config_disabled)
			// that EA-R001 already wires through to
			// collector_status.json.
			out[GateReasonConfigDisabled] = append(out[GateReasonConfigDisabled], q.ID)
		case len(p.IncludeOnly) > 0 && !p.IncludeOnly[q.ID]:
			out[GateReasonConfigDisabled] = append(out[GateReasonConfigDisabled], q.ID)
		case p.Exclude[q.ID]:
			out[GateReasonConfigDisabled] = append(out[GateReasonConfigDisabled], q.ID)
		}
	}
	for k := range out {
		sort.Strings(out[k])
	}
	return out
}

// ByID returns the query with the given ID, or nil if not found.
func ByID(id string) *QueryDef {
	return registryByID[id]
}
