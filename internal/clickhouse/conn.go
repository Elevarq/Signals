package clickhouse

import (
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/elevarq/signals/internal/dbtls"
)

const (
	defaultPort           = 9000 // ClickHouse native protocol
	defaultConnectTimeout = 5 * time.Second
	defaultQueryTimeout   = 10 * time.Second
)

// ConnConfig describes a ClickHouse target.
type ConnConfig struct {
	Host           string
	Port           int
	User           string
	Password       string
	DBName         string
	ConnectTimeout time.Duration
	QueryTimeout   time.Duration
	// TLSMode is the PostgreSQL-style sslmode for the native connection (#558):
	// "" / "disable" → plaintext; "require" → encrypt without verification;
	// "verify-ca" → verify the chain against TLSRootCAFile (not the hostname);
	// "verify-full" → verify the chain AND the hostname. TLSRootCAFile is the PEM
	// CA bundle used by the verify modes. See specifications/clickhouse-connection-hardening.md.
	TLSMode       string
	TLSRootCAFile string
}

// resolveTLS maps the PostgreSQL-style TLSMode to the native driver's
// *tls.Config (#558, CCH-R001..R004). It returns (nil, nil) for plaintext
// (disable/empty), a non-nil config for every TLS-required mode, and fails
// closed (error) for a verify mode whose CA cannot be loaded or for an unknown
// sslmode — never a silent plaintext/unverified downgrade (INV-CCH-01).
func (c ConnConfig) resolveTLS() (*tls.Config, error) {
	switch strings.ToLower(strings.TrimSpace(c.TLSMode)) {
	case "", "disable", "disabled":
		return nil, nil
	case "require", "required":
		// Encryption only, certificate not verified (PostgreSQL `require`
		// semantics). TLS IS applied — never plaintext. verify-ca/verify-full are
		// the verifying modes.
		return &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: true}, nil // nosemgrep
	case "verify-ca":
		return dbtls.BuildVerifiedTLSConfig(c.TLSRootCAFile, c.Host, false)
	case "verify-full":
		return dbtls.BuildVerifiedTLSConfig(c.TLSRootCAFile, c.Host, true)
	default:
		return nil, fmt.Errorf("unknown sslmode %q for ClickHouse target (want disable/require/verify-ca/verify-full)", c.TLSMode)
	}
}

// ScalarQuery runs a query returning a single string cell. The seam that makes
// Identify unit-testable without a live server.
type ScalarQuery func(ctx context.Context, query string) (string, error)

// Open dials the ClickHouse server over the native protocol with a small bounded
// pool and verifies reachability with a ping inside ConnectTimeout. On any
// failure it returns an error and no usable handle (the caller records a
// collection failure; nothing is mislabeled). A non-ClickHouse server cannot
// complete the native handshake, so it fails here rather than being misread.
func Open(ctx context.Context, c ConnConfig) (*sql.DB, error) {
	port := c.Port
	if port == 0 {
		port = defaultPort
	}
	ct := c.ConnectTimeout
	if ct == 0 {
		ct = defaultConnectTimeout
	}
	qt := c.QueryTimeout
	if qt == 0 {
		qt = defaultQueryTimeout
	}
	// #558: resolve + apply the configured TLS BEFORE dialing. A verify mode with
	// unusable CA material fails closed here — the connection is never attempted
	// in plaintext (INV-CCH-01).
	tlsCfg, err := c.resolveTLS()
	if err != nil {
		return nil, fmt.Errorf("resolve TLS: %w", err)
	}
	opts := &clickhouse.Options{
		Addr: []string{net.JoinHostPort(c.Host, strconv.Itoa(port))},
		Auth: clickhouse.Auth{
			Database: c.DBName,
			Username: c.User,
			Password: c.Password,
		},
		DialTimeout: ct,
		ReadTimeout: qt,
		TLS:         tlsCfg,
	}
	db := clickhouse.OpenDB(opts)
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(5 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, ct)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, err
	}
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

// Identify confirms the server is ClickHouse and returns its version. A
// successful native connection that answers SELECT version() with a non-empty
// version IS the positive identification (ClickHouse speaks neither the pgx nor
// the MySQL wire protocol, so reaching this point already means the native
// handshake succeeded). Returns an error when the version is empty/unreadable.
func Identify(ctx context.Context, q ScalarQuery) (version string, err error) {
	v, err := q(ctx, "SELECT version()")
	if err != nil {
		return "", err
	}
	if v == "" {
		return "", fmt.Errorf("empty version() — not a ClickHouse server")
	}
	return v, nil
}
