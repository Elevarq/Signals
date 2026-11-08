// Engine-neutral per-target collection-profile enforcement (R098 / SP-R010,
// #709 / F02).
//
// The per-target profile (restricted / custom include+exclude) is a privacy and
// collection-cost control, not a PostgreSQL feature. PostgreSQL enforces it via
// pgqueries.Filter; the MySQL-wire (MySQL/MariaDB/SingleStore) and ClickHouse
// native sessions used to execute their ENTIRE engine catalog and consult only
// the daemon-wide sensitivity flag, so a `restricted` target still ran
// high-sensitivity collectors and retained SQL text its profile forbade
// (INV-SENS-01 breach for non-PostgreSQL targets).
//
// These helpers are the shared, engine-neutral boundary: a session filters its
// catalog by the target profile BEFORE executing any query, and records the
// dropped collectors as skipped/config_disabled exactly as the PostgreSQL path
// does — so the per-target outcome inventory stays truthful.

package collector

import (
	"github.com/elevarq/signals/internal/config"
	"github.com/elevarq/signals/internal/db"
	"github.com/elevarq/signals/internal/pgqueries"
)

// profileFilterCatalog splits an engine's collector catalog into the subset the
// target's per-target profile ADMITS (to be executed) and the collectors it
// DROPS, using the SAME gate semantics as pgqueries.Filter (R098 / SP-R010):
//   - default / empty: admit the whole catalog (the daemon-wide sensitivity
//     gate still applies inside each engine's Collect via redaction).
//   - restricted: drop every high-sensitivity collector.
//   - custom: with a non-empty include, admit ONLY the listed IDs; with an
//     exclude, drop the listed IDs.
//
// id and highSens project the engine's concrete collector type onto the two
// fields the gate needs, so the policy is identical across engines rather than
// re-implemented per engine. A profile NEVER widens eligibility (INV-SENS-01 /
// INV-SENS-05): a custom include naming only non-existent collectors admits
// NONE — it never silently falls back to the full catalog.
func profileFilterCatalog[T any](cat []T, tgt config.TargetConfig, id func(T) string, highSens func(T) bool) (eligible, gated []T) {
	switch tgt.Collectors.Profile {
	case "", config.ProfileDefault:
		return cat, nil
	}

	inc := make(map[string]bool, len(tgt.Collectors.Include))
	for _, s := range tgt.Collectors.Include {
		inc[s] = true
	}
	exc := make(map[string]bool, len(tgt.Collectors.Exclude))
	for _, s := range tgt.Collectors.Exclude {
		exc[s] = true
	}

	for _, c := range cat {
		keep := true
		switch tgt.Collectors.Profile {
		case config.ProfileRestricted:
			if highSens(c) {
				keep = false
			}
		case config.ProfileCustom:
			if len(inc) > 0 && !inc[id(c)] {
				keep = false
			}
			if exc[id(c)] {
				keep = false
			}
		}
		if keep {
			eligible = append(eligible, c)
		} else {
			gated = append(gated, c)
		}
	}
	return eligible, gated
}

// profileSkippedRuns builds one skipped/config_disabled QueryRun per
// profile-dropped collector so the per-target outcome inventory is truthful —
// the deliberate omission is recorded exactly as the PostgreSQL path records a
// gated collector (R098 / SP-R010), not left as a silent gap. newID mints a
// fresh run ID per row (the engine sessions already carry a ULID minter).
func profileSkippedRuns(targetID int64, snapID, version, collectedAt string, gatedIDs []string, newID func() string) []db.QueryRun {
	runs := make([]db.QueryRun, 0, len(gatedIDs))
	for _, gid := range gatedIDs {
		runs = append(runs, db.QueryRun{
			ID:          newID(),
			TargetID:    targetID,
			SnapshotID:  snapID,
			QueryID:     gid,
			CollectedAt: collectedAt,
			PGVersion:   version,
			CreatedAt:   collectedAt,
			Status:      "skipped",
			Reason:      pgqueries.GateReasonConfigDisabled,
		})
	}
	return runs
}
