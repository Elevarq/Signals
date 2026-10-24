// Package engineid resolves the identity of a database server reached over
// the MySQL wire protocol into a concrete engine (SingleStore or MySQL),
// capturing its version, or failing closed when the identity is
// indeterminate.
//
// SingleStore speaks the MySQL wire protocol, so a shared connector reaches
// both engines — but they must never be confused (Elevarq/Signals#526:
// "Never report SingleStore as MySQL"). This package is the pure,
// deterministic rule both the connector and the snapshot persistence depend
// on. PostgreSQL is out of scope: it is reached over its own protocol and
// identified by pgqueries.Discovery, and never reaches this resolver.
//
// Specification: specifications/singlestore-engine-identity.md
// (SS-ID-R001..R004, INV-SSID-01..03).
package engineid

import (
	"errors"
	"strings"
)

// Engine identity constants. The values are stable, language-neutral, and
// are what a snapshot declares as its engine.
const (
	EnginePostgreSQL  = "postgresql"
	EngineMySQL       = "mysql"
	EngineMariaDB     = "mariadb"
	EngineSingleStore = "singlestore"
)

// ErrIndeterminate is returned when a probe carries no recognizable engine
// signal. Per INV-SSID-03 the resolver never guesses — an unrecognized
// server is always an error, never a labeled engine.
var ErrIndeterminate = errors.New("indeterminate engine identity")

// Probe holds the MySQL-wire server variables used to decide engine
// identity. An empty string means the variable was absent or could not be
// read. The values are read by the connector; this package performs no I/O.
type Probe struct {
	// MemsqlVersion is `SELECT @@memsql_version`. Present only on
	// SingleStore; empty on MySQL/MariaDB.
	MemsqlVersion string
	// VersionComment is `SELECT @@version_comment`, e.g. "SingleStore",
	// "MemSQL", "MySQL Community Server - GPL", "mariadb.org binary
	// distribution".
	VersionComment string
	// Version is `SELECT VERSION()` — a MySQL-compatible version string
	// that, on its own, cannot distinguish SingleStore from MySQL.
	Version string
}

// Resolve maps a Probe to an engine identity and version, or returns an
// error when the identity is indeterminate (fail closed).
//
// Precedence (specification SS-ID-R001..R004):
//  1. @@memsql_version present            -> singlestore (version = memsql_version)
//  2. SingleStore/MemSQL named in strings -> singlestore (version = Version|VersionComment)
//  3. MariaDB named in strings            -> mariadb     (version = Version|VersionComment)
//  4. MySQL named in strings (not MariaDB) -> mysql       (version = Version|VersionComment)
//  5. otherwise                           -> ErrIndeterminate
func Resolve(p Probe) (engine string, version string, err error) {
	// SS-ID-R001: @@memsql_version exists only on SingleStore and wins over
	// everything, including a misleading MySQL-compatible comment
	// (INV-SSID-02).
	if strings.TrimSpace(p.MemsqlVersion) != "" {
		return EngineSingleStore, strings.TrimSpace(p.MemsqlVersion), nil
	}

	comment := strings.ToLower(p.VersionComment)
	ver := strings.ToLower(p.Version)
	contains := func(needle string) bool {
		return strings.Contains(comment, needle) || strings.Contains(ver, needle)
	}

	// SS-ID-R002: explicit SingleStore/MemSQL branding (INV-SSID-01).
	if contains("singlestore") || contains("memsql") {
		return EngineSingleStore, firstNonEmpty(p.Version, p.VersionComment), nil
	}

	// SS-ID-R003: MariaDB is a DISTINCT engine from MySQL. Checked before the
	// MySQL branch: a MariaDB server brands itself "mariadb" in VersionComment
	// ("mariadb.org binary distribution") and/or Version ("10.11.6-MariaDB"),
	// and must never fold into `mysql`.
	if contains("mariadb") {
		return EngineMariaDB, firstNonEmpty(p.Version, p.VersionComment), nil
	}

	// SS-ID-R005: MySQL (not MariaDB), with no SingleStore signal above.
	if contains("mysql") {
		return EngineMySQL, firstNonEmpty(p.Version, p.VersionComment), nil
	}

	// SS-ID-R004 / INV-SSID-03: never guess.
	return "", "", ErrIndeterminate
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
