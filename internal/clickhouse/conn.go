package clickhouse

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
)

const (
	defaultPort           = 9000 // ClickHouse native protocol
	defaultConnectTimeout = 5 * time.Second
	defaultQueryTimeout   = 10 * time.Second
)

// ConnConfig describes a ClickHouse target. Self-contained for the backend
// foundation; a full TLS/CA path (mirroring the MySQL-wire #585 work) is a later
// increment.
type ConnConfig struct {
	Host           string
	Port           int
	User           string
	Password       string
	DBName         string
	ConnectTimeout time.Duration
	QueryTimeout   time.Duration
	// TLS (native 9440 / CA material, mirroring the MySQL-wire #585 work) is a
	// later increment; CH-1 is plaintext against the loopback dev image.
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
	opts := &clickhouse.Options{
		Addr: []string{net.JoinHostPort(c.Host, strconv.Itoa(port))},
		Auth: clickhouse.Auth{
			Database: c.DBName,
			Username: c.User,
			Password: c.Password,
		},
		DialTimeout: ct,
		ReadTimeout: qt,
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
