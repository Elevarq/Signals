package pgqueries

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Discovery captures the connected target's identity, version, and
// installed-extension surface. R081 requires this to run before catalog
// filtering so each cycle's eligible query set reflects the *connected
// server's* version, not a configured assumption.
type Discovery struct {
	// ServerVersion is the long human-readable string from
	// `SELECT version()`, e.g. "PostgreSQL 18.2 on aarch64-…".
	ServerVersion string
	// ServerVersionNum is the integer encoding from
	// current_setting('server_version_num') — e.g. 180002 for 18.2,
	// 170004 for 17.4. Useful for minor-version checks.
	ServerVersionNum int
	// MajorVersion is the PG major (14, 15, 16, 17, 18, …) extracted
	// from ServerVersionNum / 10000.
	MajorVersion int
	// Database is the result of current_database() — informational
	// only; never used as a metric label or audit attribute.
	Database string
	// CurrentUser is the result of current_user — used by
	// rolecheck.go's safety validation; not surfaced in metrics.
	CurrentUser string
	// Extensions lists the names of installed extensions
	// (`pg_extension` rows). Drives extension-gated catalog filtering.
	Extensions []string
}

// SupportedMajors enumerates the PG majors with first-class catalog
// support in this build. Targets running a major outside this list
// still receive the version-agnostic catalog with a warning logged at
// startup; targets older than the minimum are not collected from.
var SupportedMajors = []int{14, 15, 16, 17, 18}

// MinSupportedMajor / MaxSupportedMajor define the explicit support
// window. Outside this window the collector either refuses (below) or
// falls back with a warning (above) — see IsSupportedMajor.
const (
	MinSupportedMajor       = 14
	MaxSupportedMajor       = 18
	ExperimentalFutureMajor = 19
)

// IsSupportedMajor returns true if the major has explicit catalog
// support, false if it's experimental (above MaxSupportedMajor) or
// unsupported (below MinSupportedMajor). Falls within the spec's
// "PG 14-18 supported, 19 experimental, others unsupported" model.
func IsSupportedMajor(major int) bool {
	return major >= MinSupportedMajor && major <= MaxSupportedMajor
}

// IsExperimentalMajor returns true for majors above the support
// window — currently PG 19. The collector still attempts collection
// against the highest supported catalog and emits a warning so
// operators see it.
func IsExperimentalMajor(major int) bool {
	return major > MaxSupportedMajor
}

// Discover runs a single small read-only probe to populate Discovery.
// Must be called inside a transaction whose session is already in
// read-only posture (R013/R017). The query is intentionally minimal:
// no joins, no catalog scans beyond pg_extension, no PII.
func Discover(ctx context.Context, tx pgx.Tx) (Discovery, error) {
	var d Discovery
	err := tx.QueryRow(ctx, `
		SELECT
			version(),
			current_setting('server_version_num')::int,
			current_database(),
			current_user
	`).Scan(&d.ServerVersion, &d.ServerVersionNum, &d.Database, &d.CurrentUser)
	if err != nil {
		return d, fmt.Errorf("discovery: %w", err)
	}
	d.MajorVersion = d.ServerVersionNum / 10000

	rows, err := tx.Query(ctx, `SELECT extname FROM pg_extension ORDER BY extname`)
	if err != nil {
		return d, fmt.Errorf("discovery: list extensions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return d, fmt.Errorf("discovery: scan extension: %w", err)
		}
		d.Extensions = append(d.Extensions, name)
	}
	if err := rows.Err(); err != nil {
		return d, fmt.Errorf("discovery: iterate extensions: %w", err)
	}

	return d, nil
}
