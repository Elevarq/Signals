package db

import (
	"errors"
	"fmt"
	"sort"
)

// Store schema version — forward-only / no-downgrade / fail-closed-on-newer
// policy for the SQLite store (Elevarq/Signals#502).
//
// Spec: specifications/store-schema-version.md
//
// The binary's schema identity is derived entirely from its embedded
// migration set (internal/db/migrations.go) — there is no hand-maintained
// version constant that can drift from it (R-SCHEMA-01). The store records
// which migrations it has applied in the schema_migrations ledger. If the
// ledger names a migration the binary does not ship, the store was migrated
// forward by a NEWER binary than the one now opening it; the older binary
// must refuse to operate it (INV-SCHEMA-01).

// ErrSchemaNewer is the sentinel for the fail-closed newer-store refusal.
// Callers can match it with errors.Is; *SchemaNewerError carries the detail.
var ErrSchemaNewer = errors.New("store schema is newer than this binary understands")

// SchemaNewerError is returned by Migrate when the store's schema_migrations
// ledger records at least one migration filename that is not in the binary's
// embedded migration set (R-SCHEMA-03). It is a recognizable, typed,
// fail-closed error: no migration is applied and the store is left untouched
// (R-SCHEMA-04 / INV-SCHEMA-02).
type SchemaNewerError struct {
	// StoredMax is the greatest (latest) migration filename recorded in the
	// store's ledger — the store's schema ceiling.
	StoredMax string
	// BinaryMax is the greatest migration filename this binary ships — the
	// binary's schema ceiling.
	BinaryMax string
	// Unknown lists every ledger filename not shipped by this binary, sorted.
	Unknown []string
}

func (e *SchemaNewerError) Error() string {
	return fmt.Sprintf(
		"%s: store recorded migration %q but this binary's latest known migration is %q "+
			"(unknown to this binary: %v). Signals migrations are forward-only and never "+
			"downgrade: run the newer Signals binary against this store, or restore the "+
			"pre-upgrade backup of /data/signals.db (or the durable data-volume snapshot) "+
			"and run this binary against that. Refusing to run to avoid operating a store "+
			"whose schema is ahead of this binary.",
		ErrSchemaNewer.Error(), e.StoredMax, e.BinaryMax, e.Unknown,
	)
}

// Unwrap lets errors.Is(err, ErrSchemaNewer) match a *SchemaNewerError.
func (e *SchemaNewerError) Unwrap() error { return ErrSchemaNewer }

// MaxKnownMigration returns the binary's schema ceiling: the
// lexicographically greatest embedded migration filename (R-SCHEMA-01).
// Migration filenames are zero-padded ordinal-prefixed, so lexical order is
// migration order. Returns "" when the binary ships no migrations.
func MaxKnownMigration() string {
	names := MigrationFilenames()
	if len(names) == 0 {
		return ""
	}
	// MigrationFilenames already returns sorted names, but sort defensively.
	sort.Strings(names)
	return names[len(names)-1]
}

// knownMigrationSet returns the set of embedded migration filenames for
// membership tests.
func knownMigrationSet() map[string]struct{} {
	names := MigrationFilenames()
	set := make(map[string]struct{}, len(names))
	for _, n := range names {
		set[n] = struct{}{}
	}
	return set
}

// checkNotNewer reads the store's schema_migrations ledger and, if any
// recorded filename is not in the binary's embedded set, returns a
// *SchemaNewerError (R-SCHEMA-02 / R-SCHEMA-03). It performs only a read of
// the ledger — never any mutation (R-SCHEMA-04).
//
// A legacy/unversioned store (no schema_migrations table, or an empty ledger)
// is not newer: the caller ensures the table exists, and an empty ledger
// yields no unknown rows, so this returns nil and the normal forward path
// adopts the store (R-SCHEMA-05).
func (d *DB) checkNotNewer() error {
	rows, err := d.sql.Query("SELECT filename FROM schema_migrations")
	if err != nil {
		return fmt.Errorf("read schema_migrations ledger: %w", err)
	}
	defer rows.Close()

	known := knownMigrationSet()
	var unknown []string
	for rows.Next() {
		var filename string
		if err := rows.Scan(&filename); err != nil {
			return fmt.Errorf("scan schema_migrations ledger: %w", err)
		}
		if _, ok := known[filename]; !ok {
			unknown = append(unknown, filename)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate schema_migrations ledger: %w", err)
	}

	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return &SchemaNewerError{
		StoredMax: unknown[len(unknown)-1],
		BinaryMax: MaxKnownMigration(),
		Unknown:   unknown,
	}
}
