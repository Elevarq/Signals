package collector

import (
	"bufio"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/elevarq/arq-signals/internal/config"
)

// AppName is the single source of truth for the PostgreSQL
// `application_name` runtime parameter set by every connection Arq
// Signals opens. The same value is referenced by the
// pg_stat_statements_v1 self-filter so the collector's own probe
// queries do not appear in customer workload analysis (R106).
//
// AppName is appended to the PostgreSQL DSN as a key=value parameter
// in BuildSafeDSN, so it must remain simple: lowercase ASCII, with no
// whitespace, single quotes, or backslashes. Otherwise the unquoted
// key=value DSN parser may misparse the value and break diagnostic
// connections (doctor C3/C4, conntest).
const AppName = "arq-signals"

// BuildConnConfig creates a pgx.ConnConfig from structured target fields,
// resolving the password at call time from the configured secret source.
// Passwords are never cached — they are read fresh on every call to support rotation.
func BuildConnConfig(tgt config.TargetConfig) (*pgx.ConnConfig, error) {
	port := tgt.Port
	if port == 0 {
		port = 5432
	}

	host := tgt.Host
	if host == "" {
		return nil, fmt.Errorf("target %s: host is required", tgt.Name)
	}

	// Build a postgres:// URL so net/url handles all escaping. The previous
	// fmt.Sprintf("host=%s ... dbname=%s ...") form interpolated user-provided
	// fields without quoting; a dbname containing a space (or worse, an embedded
	// `key=value` pair) would have been parsed as additional connection options
	// rather than as a literal value.
	u := &url.URL{
		Scheme: "postgres",
		Host:   net.JoinHostPort(host, strconv.Itoa(port)),
	}
	if tgt.User != "" {
		u.User = url.User(tgt.User)
	}
	if tgt.DBName != "" {
		u.Path = "/" + tgt.DBName
	}
	q := u.Query()
	if tgt.SSLMode != "" {
		q.Set("sslmode", tgt.SSLMode)
	}
	if tgt.SSLRootCertFile != "" {
		q.Set("sslrootcert", tgt.SSLRootCertFile)
	}
	u.RawQuery = q.Encode()

	cfg, err := pgx.ParseConfig(u.String())
	if err != nil {
		return nil, fmt.Errorf("target %s: parse config: %w", tgt.Name, err)
	}

	// Resolve password from configured secret source.
	password, err := ResolvePassword(tgt)
	if err != nil {
		return nil, fmt.Errorf("target %s: resolve password: %w", tgt.Name, redactError(err))
	}
	if password != "" {
		cfg.Password = password
	}

	cfg.RuntimeParams["application_name"] = AppName
	cfg.RuntimeParams["default_transaction_read_only"] = "on"

	return cfg, nil
}

// BuildSafeDSN constructs a key=value DSN for diagnostic / read-only
// connection attempts (doctor C4, conntest R096). Returns the
// password-resolution error so callers can distinguish "couldn't
// resolve password" (operator config problem) from "auth failed at
// the server" (PG-level rejection).
//
// Honours sslmode default of "prefer". connect_timeout is pinned at
// 3s — diagnostic callers want a tight bound; production daemon
// uses its own per-target-timeout configuration on the live pool.
//
// Password is appended only when ResolvePassword returns a non-empty
// value (peer/trust auth otherwise). No URL encoding — the key=value
// format escapes via quoting inside the consumer (pgx.ParseConfig).
func BuildSafeDSN(tgt config.TargetConfig) (string, error) {
	sslmode := tgt.SSLMode
	if sslmode == "" {
		sslmode = "prefer"
	}
	password, err := ResolvePassword(tgt)
	if err != nil {
		return "", err
	}
	parts := []string{
		"host=" + tgt.Host,
		"port=" + strconv.Itoa(tgt.Port),
		"dbname=" + tgt.DBName,
		"user=" + tgt.User,
		"sslmode=" + sslmode,
		"connect_timeout=3",
		"application_name=" + AppName,
	}
	if password != "" {
		parts = append(parts, "password="+password)
	}
	return strings.Join(parts, " "), nil
}

// ResolvePassword reads the password from the configured secret source.
// Returns empty string if no secret source is configured (peer/trust auth).
func ResolvePassword(tgt config.TargetConfig) (string, error) {
	switch {
	case tgt.PasswordFile != "":
		return readPasswordFile(tgt.PasswordFile)
	case tgt.PasswordEnv != "":
		return readPasswordEnv(tgt.PasswordEnv)
	case tgt.PgpassFile != "":
		port := tgt.Port
		if port == 0 {
			port = 5432
		}
		return readPgpass(tgt.PgpassFile, tgt.Host, port, tgt.DBName, tgt.User)
	default:
		return "", nil
	}
}

func readPasswordFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read password_file: %w", err)
	}
	// Trim trailing newline (common in Docker secrets).
	return strings.TrimRight(string(data), "\n\r"), nil
}

func readPasswordEnv(envVar string) (string, error) {
	val, ok := os.LookupEnv(envVar)
	if !ok {
		return "", fmt.Errorf("password_env %q is not set", envVar)
	}
	return val, nil
}

// readPgpass reads a pgpass-format file and returns the matching password.
// Format: hostname:port:database:username:password
// Wildcard (*) matches any value in that field.
func readPgpass(path string, host string, port int, dbname string, user string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open pgpass_file: %w", err)
	}
	defer f.Close()

	portStr := strconv.Itoa(port)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)

		// Skip comments and empty lines.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := parsePgpassLine(line)
		if len(fields) != 5 {
			continue
		}

		if pgpassFieldMatch(fields[0], host) &&
			pgpassFieldMatch(fields[1], portStr) &&
			pgpassFieldMatch(fields[2], dbname) &&
			pgpassFieldMatch(fields[3], user) {
			return fields[4], nil
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read pgpass_file: %w", err)
	}

	return "", fmt.Errorf("no matching entry in pgpass_file %s for %s:%d/%s@%s", path, host, port, dbname, user)
}

// parsePgpassLine splits a pgpass line into fields, handling escaped colons (\:)
// and escaped backslashes (\\).
func parsePgpassLine(line string) []string {
	var fields []string
	var current strings.Builder
	escaped := false

	for _, ch := range line {
		if escaped {
			current.WriteRune(ch)
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == ':' {
			fields = append(fields, current.String())
			current.Reset()
			continue
		}
		current.WriteRune(ch)
	}
	fields = append(fields, current.String())
	return fields
}

func pgpassFieldMatch(pattern, value string) bool {
	return pattern == "*" || pattern == value
}

// RedactError wraps an error to ensure passwords don't leak into error messages.
// It replaces the original error message with a generic one if it might contain
// secrets. Exported for use by other internal packages (e.g. internal/doctor)
// that surface password-resolution errors in operator-visible output.
func RedactError(err error) error {
	return redactError(err)
}

// redactError wraps an error to ensure passwords don't leak into error messages.
// It replaces the original error message with a generic one if it might contain secrets.
func redactError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	// If the error might contain a password value, redact it.
	if strings.Contains(msg, "password") || strings.Contains(msg, "secret") {
		return fmt.Errorf("credential resolution failed (details redacted)")
	}
	return err
}

// RedactDSN takes a connection string that might contain embedded credentials
// and returns a safe version for logging.
func RedactDSN(dsn string) string {
	// Handle postgres:// URL format.
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		// Find the userinfo section and redact password.
		if atIdx := strings.Index(dsn, "@"); atIdx != -1 {
			prefix := dsn[:strings.Index(dsn, "//")+2]
			userinfo := dsn[len(prefix):atIdx]
			rest := dsn[atIdx:]
			if colonIdx := strings.Index(userinfo, ":"); colonIdx != -1 {
				return prefix + userinfo[:colonIdx] + ":****" + rest
			}
		}
	}
	// Handle key=value format.
	if strings.Contains(dsn, "password=") {
		parts := strings.Fields(dsn)
		for i, part := range parts {
			if strings.HasPrefix(part, "password=") {
				parts[i] = "password=****"
			}
		}
		return strings.Join(parts, " ")
	}
	return dsn
}

// SafeTargetAddr returns a loggable host:port string for the target.
func SafeTargetAddr(tgt config.TargetConfig) string {
	port := tgt.Port
	if port == 0 {
		port = 5432
	}
	return net.JoinHostPort(tgt.Host, strconv.Itoa(port))
}
