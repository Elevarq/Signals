package db

// Cross-branch migration-ledger equivalence (R-SCHEMA-08, #710 / F03).
//
// Two shipped release lines issued the SAME DDL under different migration
// ordinals: the production (1.x) line shipped 007_cycle_outcome_last_error.sql,
// while the POC/2.0 line allocated 007 to snapshot-engine identity
// (007_snapshot_engine.sql) and renumbered the cycle-outcome migration to
// 008_cycle_outcome_last_error.sql. The DDL is byte-identical — two additive
// columns (reason_code, message) on target_cycle_outcomes.
//
// A production store therefore records 007_cycle_outcome_last_error.sql in its
// ledger. Without reconciliation the POC binary (a) classifies that filename as
// unknown/newer and refuses startup (SchemaNewerError), and (b) if forced past
// that, re-runs its own 008 and fails with a duplicate-column error. This file
// is the explicit, append-only map that makes the two histories compatible.

// migrationEquivalents maps an EMBEDDED migration filename to the set of
// historically-shipped filenames that applied the SAME DDL under a different
// ordinal on another release line. Append-only: add an entry only when a
// renumbered-but-identical migration actually shipped. Keys are embedded
// migrations this binary carries; values are the legacy filenames a real store
// may record for the same change.
var migrationEquivalents = map[string][]string{
	"008_cycle_outcome_last_error.sql": {"007_cycle_outcome_last_error.sql"},
	// #716: the per-snapshot source-identity migration shipped on main (1.x) as
	// 008_snapshot_source_identity.sql and on this POC line as 009 (008 here is
	// cycle_outcome). The DDL is identical (ADD COLUMN source_* on snapshots +
	// backfill), so a 1.x store whose ledger records the main filename is
	// adopted and reconciled, not refused or re-ALTERed.
	"009_snapshot_source_identity.sql": {"008_snapshot_source_identity.sql"},
	// #729: the per-snapshot collection-time policy migration shipped on main
	// (1.x) as 009_snapshot_collection_policy.sql and on this POC line as 010
	// (the numbers run one ahead here because 007 is snapshot_engine). The DDL is
	// identical (ADD COLUMN collect_unsafe_mode / collect_high_sensitivity,
	// nullable, no backfill), so a store whose ledger records the main filename
	// is adopted, not re-ALTERed.
	"010_snapshot_collection_policy.sql": {"009_snapshot_collection_policy.sql"},
}

// recognizedLedgerFilenames returns every filename a store's ledger may
// legitimately record and still be adopted by this binary: the embedded
// migration set PLUS every enumerated historical equivalent. checkNotNewer uses
// this (rather than the bare embedded set) so a legacy production filename is
// not misclassified as an unknown/newer migration (R-SCHEMA-08 adoption).
func recognizedLedgerFilenames() map[string]struct{} {
	set := knownMigrationSet()
	for _, equivs := range migrationEquivalents {
		for _, e := range equivs {
			set[e] = struct{}{}
		}
	}
	return set
}

// priorEquivalentApplied reports whether any historical equivalent of the
// embedded migration `filename` is already recorded in `ledger` — i.e. the
// migration's DDL was already applied under an older ordinal on another release
// line. When true, the caller records the embedded filename as applied WITHOUT
// re-running the DDL (R-SCHEMA-08 reconcile), avoiding a duplicate-column error.
func priorEquivalentApplied(filename string, ledger map[string]struct{}) bool {
	for _, e := range migrationEquivalents[filename] {
		if _, ok := ledger[e]; ok {
			return true
		}
	}
	return false
}
