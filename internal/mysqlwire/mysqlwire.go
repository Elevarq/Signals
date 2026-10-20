// Package mysqlwire is the shared MySQL-wire connector for Signals: it opens
// a bounded, read-only-by-use connection to a MySQL-protocol server (MySQL or
// SingleStore), reads the identity probe variables, and delegates engine
// classification to internal/engineid.
//
// It is the shared foundation for MySQL (Elevarq/Signals#525) and SingleStore
// (#526): #535 (shared wire) + #548 (connect + identify SingleStore). This
// package never classifies an engine itself (INV-WIRE-03) and never issues a
// write (INV-WIRE-01). PostgreSQL uses the separate pgx path and never reaches
// here.
//
// Specification: specifications/singlestore-wire-connect.md
// (WIRE-R001..R005, INV-WIRE-01..03).
package mysqlwire

import (
	"context"
	"database/sql"
	"net"
	"strconv"
	"time"

	"github.com/elevarq/signals/internal/engineid"
	"github.com/go-sql-driver/mysql"
)

const (
	defaultPort           = 3306
	defaultTLS            = "preferred"
	defaultConnectTimeout = 5 * time.Second
	defaultQueryTimeout   = 10 * time.Second
)

// ConnConfig describes a MySQL-wire target. Self-contained for the POC;
// mapping from config.TargetConfig is a later increment.
type ConnConfig struct {
	Host           string
	Port           int
	User           string
	Password       string
	DBName         string
	TLS            string // go-sql-driver key: true/false/skip-verify/preferred
	ConnectTimeout time.Duration
	QueryTimeout   time.Duration
}

// ScalarQuery runs a query that yields a single string cell. It is the seam
// that makes Identify unit-testable without a live server.
type ScalarQuery func(ctx context.Context, query string) (string, error)

// driverConfig builds a go-sql-driver *mysql.Config from ConnConfig, applying
// the defaults and the injection-safe InterpolateParams=false (WIRE-R002).
func (c ConnConfig) driverConfig() *mysql.Config {
	port := c.Port
	if port == 0 {
		port = defaultPort // WIRE-R001
	}
	tls := c.TLS
	if tls == "" {
		tls = defaultTLS // WIRE-R005
	}
	ct := c.ConnectTimeout
	if ct == 0 {
		ct = defaultConnectTimeout
	}
	qt := c.QueryTimeout
	if qt == 0 {
		qt = defaultQueryTimeout
	}

	dc := mysql.NewConfig()
	dc.Net = "tcp"
	dc.Addr = net.JoinHostPort(c.Host, strconv.Itoa(port))
	dc.User = c.User
	dc.Passwd = c.Password
	dc.DBName = c.DBName
	dc.TLSConfig = tls
	dc.Timeout = ct
	dc.ReadTimeout = qt
	dc.InterpolateParams = false // WIRE-R002: no client-side interpolation
	return dc
}

// BuildDSN returns the go-sql-driver DSN for a ConnConfig.
func BuildDSN(c ConnConfig) string {
	return c.driverConfig().FormatDSN()
}

// Redacted returns a DSN with the password blanked — the only DSN form
// intended for logging (INV-WIRE-02).
func Redacted(c ConnConfig) string {
	dc := c.driverConfig()
	dc.Passwd = ""
	return dc.FormatDSN()
}

// Open dials the server with a small bounded pool and verifies reachability
// with a ping inside ConnectTimeout. On any failure it returns an error and
// no usable handle (the caller records a collection failure; nothing is
// mislabeled).
func Open(ctx context.Context, c ConnConfig) (*sql.DB, error) {
	db, err := OpenDSN(BuildDSN(c))
	if err != nil {
		return nil, err
	}
	ct := c.ConnectTimeout
	if ct == 0 {
		ct = defaultConnectTimeout
	}
	pingCtx, cancel := context.WithTimeout(ctx, ct)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// OpenDSN opens a bounded pool for an already-formed DSN (used by the
// integration test and by Open).
func OpenDSN(dsn string) (*sql.DB, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(5 * time.Minute)
	return db, nil
}

// NewScalar adapts a *sql.DB into a ScalarQuery that reads a single string.
func NewScalar(db *sql.DB) ScalarQuery {
	return func(ctx context.Context, query string) (string, error) {
		var s string
		if err := db.QueryRowContext(ctx, query).Scan(&s); err != nil {
			return "", err
		}
		return s, nil
	}
}

// Identify reads the three identity probe variables over the MySQL wire and
// resolves the engine via engineid.Resolve. A failed @@memsql_version read is
// tolerated and treated as absent (WIRE-R003) — only SingleStore exposes that
// variable, so every other server rejects it. Only SELECTs are issued
// (WIRE-R004, INV-WIRE-01); classification is delegated (INV-WIRE-03).
func Identify(ctx context.Context, q ScalarQuery) (engine string, version string, err error) {
	var probe engineid.Probe
	// Tolerated: non-SingleStore servers reject this unknown variable.
	if v, e := q(ctx, "SELECT @@memsql_version"); e == nil {
		probe.MemsqlVersion = v
	}
	if v, e := q(ctx, "SELECT @@version_comment"); e == nil {
		probe.VersionComment = v
	}
	if v, e := q(ctx, "SELECT VERSION()"); e == nil {
		probe.Version = v
	}
	return engineid.Resolve(probe)
}
