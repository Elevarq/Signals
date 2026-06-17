// Package conntest implements the classified connection diagnostic
// surfaced via `signalsctl connect test` (R096). It is a thin layer over
// pgx/pgxpool that maps connection failures into one of nine
// well-defined categories — operator-actionable strings, not raw
// driver errors.
//
// Spec:        specifications/connect-test.md
// Acceptance:  specifications/connect-test.acceptance.md
package conntest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/elevarq/arq-signals/internal/collector"
	"github.com/elevarq/arq-signals/internal/config"
)

// Category enumerates the classification labels surfaced to operators.
// Lower-case wire form matches the JSON contract in the spec.
type Category string

const (
	CategoryOK              Category = "ok"
	CategoryDNS             Category = "dns"
	CategoryTCP             Category = "tcp"
	CategoryTLS             Category = "tls"
	CategoryAuth            Category = "auth"
	CategoryStartup         Category = "startup"
	CategoryRole            Category = "role"
	CategoryPasswordResolve Category = "password_resolve"
	CategoryConfig          Category = "config"
)

// Result is one connection attempt's outcome.
type Result struct {
	Target    string
	Category  Category
	Detail    string
	Host      string
	Port      int
	DBName    string
	Username  string
	PGVersion string
	Duration  time.Duration
}

// MarshalJSON honours the wire shape in the spec: duration as ms,
// category as the lowercase string enum, and per-attempt connection
// metadata at top level rather than nested in a sub-object.
func (r Result) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Target     string   `json:"target"`
		Category   Category `json:"category"`
		Detail     string   `json:"detail"`
		Host       string   `json:"host"`
		Port       int      `json:"port"`
		DBName     string   `json:"dbname"`
		Username   string   `json:"username"`
		PGVersion  string   `json:"pg_version,omitempty"`
		DurationMS int64    `json:"duration_ms"`
	}{
		Target: r.Target, Category: r.Category, Detail: r.Detail,
		Host: r.Host, Port: r.Port, DBName: r.DBName, Username: r.Username,
		PGVersion: r.PGVersion, DurationMS: r.Duration.Milliseconds(),
	})
}

// Summary mirrors the JSON wire shape.
type Summary struct {
	OK   int `json:"ok"`
	Fail int `json:"fail"`
}

// Report is the full output of a Run.
type Report struct {
	SchemaVersion string   `json:"schema_version"`
	GeneratedAt   string   `json:"generated_at"`
	Attempts      []Result `json:"attempts"`
	Summary       Summary  `json:"summary"`
}

// SchemaVersion is the wire-protocol version emitted by `--json`.
const SchemaVersion = "1"

// SupportedCategories enumerates every Category value. Used by tests
// that assert the enum is exhaustive.
var SupportedCategories = []Category{
	CategoryOK, CategoryDNS, CategoryTCP, CategoryTLS, CategoryAuth,
	CategoryStartup, CategoryRole, CategoryPasswordResolve, CategoryConfig,
}

// Options shapes one TestConnection invocation. Empty zero value is
// valid — every field has a documented default.
type Options struct {
	// ConnectTimeout caps the TCP/auth phase. Defaults to 3s when zero.
	ConnectTimeout time.Duration
}

func (o Options) connectTimeout() time.Duration {
	if o.ConnectTimeout > 0 {
		return o.ConnectTimeout
	}
	return 3 * time.Second
}

// Classify maps an arbitrary error returned by pgxpool / pgx into the
// operator-facing Category enum. Returns (CategoryOK, "") for nil.
//
// The function is pure — given the same error in, the same category
// and a stable-formatted Detail come out (INV-CONN-03).
func Classify(err error) (Category, string) {
	if err == nil {
		return CategoryOK, ""
	}

	// PG server errors — auth (28xxx) and session-init (3D000).
	// Match by SQLSTATE to avoid relying on free-form message text.
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "28P01", "28000":
			return CategoryAuth, fmt.Sprintf("SQLSTATE %s: %s", pgErr.Code, pgErr.Message)
		case "3D000":
			return CategoryStartup, fmt.Sprintf("SQLSTATE %s: %s", pgErr.Code, pgErr.Message)
		}
	}

	// DNS resolution — wrapped *net.DNSError.
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return CategoryDNS, fmt.Sprintf("resolve %s: %s", dnsErr.Name, dnsErr.Err)
	}

	msg := err.Error()

	// TLS — detect via well-known prefixes used by crypto/tls and
	// crypto/x509. The TLS package wraps in stable substrings so a
	// string match is the right tool here.
	if strings.Contains(msg, "tls:") || strings.Contains(msg, "x509:") {
		return CategoryTLS, msg
	}

	// TCP-layer errors. pgx wraps these in a few shapes, so check
	// for the underlying *net.OpError type first, then fall back to
	// string heuristics for the wrapped-by-pgx form ("failed to
	// connect to host=...: dial tcp ...: connect: connection refused").
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return CategoryTCP, opErr.Error()
	}
	for _, marker := range []string{
		"connection refused",
		"i/o timeout",
		"no route to host",
		"network is unreachable",
		"host is unreachable",
	} {
		if strings.Contains(msg, marker) {
			return CategoryTCP, msg
		}
	}

	// Fallback: unrecognised error shape. Surface the redacted
	// message under CategoryConfig — it's the safe-by-default bin
	// for "we got an error but don't recognise the pattern".
	return CategoryConfig, msg
}

// TestConnection attempts a single read-only connection to tgt and
// returns the classified result. Opens a short-lived pgxpool, runs
// SELECT version(), then collector.ValidateRoleSafety, and tears
// the pool down.
func TestConnection(ctx context.Context, tgt config.TargetConfig, opts Options) Result {
	start := time.Now()
	result := Result{
		Target: tgt.Name, Host: tgt.Host, Port: tgt.Port,
		DBName: tgt.DBName, Username: tgt.User,
	}

	dsn, err := collector.BuildSafeDSN(tgt)
	if err != nil {
		result.Category = CategoryPasswordResolve
		result.Detail = collector.RedactError(err).Error()
		result.Duration = time.Since(start)
		return result
	}

	dialCtx, cancel := context.WithTimeout(ctx, opts.connectTimeout())
	defer cancel()

	pool, err := pgxpool.New(dialCtx, dsn)
	if err != nil {
		cat, detail := Classify(err)
		result.Category = cat
		result.Detail = collector.RedactDSN(detail)
		result.Duration = time.Since(start)
		return result
	}
	defer pool.Close()

	return diagnosePool(dialCtx, pool, tgt, start, result)
}

// TestConnectionWithResolver runs the same classified diagnostic as
// TestConnection but resolves the connection credential through res rather
// than reading a password source. This exercises the cloud auth_methods
// (aws_rds_iam, azure_entra, gcp_cloudsql_iam, secret_store) and mtls over
// the credential the resolver mints/fetches/loads — the guided-connect
// orchestrator (#99) drives every method through this single path so it
// reuses, rather than reimplements, credential resolution and the role
// check (ARQ-SIGNALS-CONNECT-INV003). The caller sets tgt.SSLMode to
// verify-full for credential-bearing methods (ARQ-SIGNALS-CONNECT-INV005).
//
// A resolve failure is classified as CategoryPasswordResolve with a
// redacted detail; the credential value never appears in the Result
// (ARQ-SIGNALS-CONNECT-INV001).
func TestConnectionWithResolver(ctx context.Context, tgt config.TargetConfig, res collector.CredentialResolver, opts Options) Result {
	start := time.Now()
	result := Result{
		Target: tgt.Name, Host: tgt.Host, Port: tgt.Port,
		DBName: tgt.DBName, Username: tgt.User,
	}

	dialCtx, cancel := context.WithTimeout(ctx, opts.connectTimeout())
	defer cancel()

	cred, err := res.Resolve(dialCtx, tgt)
	if err != nil {
		result.Category = CategoryPasswordResolve
		result.Detail = collector.RedactError(err).Error()
		result.Duration = time.Since(start)
		return result
	}

	connConfig, err := collector.BuildConnConfigWithCredential(tgt, cred)
	if err != nil {
		result.Category = CategoryConfig
		result.Detail = collector.RedactError(err).Error()
		result.Duration = time.Since(start)
		return result
	}

	// pgxpool.NewWithConfig requires a Config produced by ParseConfig for
	// its pool-level defaults; we replace its ConnConfig with the
	// credential-bearing one and cap the pool at a single connection — the
	// diagnostic opens exactly one.
	poolCfg, err := pgxpool.ParseConfig("postgres://localhost")
	if err != nil {
		result.Category = CategoryConfig
		result.Detail = collector.RedactError(err).Error()
		result.Duration = time.Since(start)
		return result
	}
	poolCfg.ConnConfig = connConfig
	poolCfg.MaxConns = 1

	pool, err := pgxpool.NewWithConfig(dialCtx, poolCfg)
	if err != nil {
		cat, detail := Classify(err)
		result.Category = cat
		result.Detail = collector.RedactDSN(detail)
		result.Duration = time.Since(start)
		return result
	}
	defer pool.Close()

	return diagnosePool(dialCtx, pool, tgt, start, result)
}

// diagnosePool runs the shared post-connect diagnostic over an open pool:
// force a dial (Ping), read the server version, and run the role-safety
// check. It is the common tail of both TestConnection (password path) and
// TestConnectionWithResolver (resolver path) so the classification and the
// role check are defined once (INV-CONN-03). dialCtx already carries the
// connect timeout; start anchors the reported duration.
func diagnosePool(dialCtx context.Context, pool *pgxpool.Pool, tgt config.TargetConfig, start time.Time, result Result) Result {
	// Force the pool to actually dial. pgxpool is lazy — without an
	// explicit Ping the dial / auth phase only happens on the first query.
	if err := pool.Ping(dialCtx); err != nil {
		cat, detail := Classify(err)
		result.Category = cat
		result.Detail = collector.RedactDSN(detail)
		result.Duration = time.Since(start)
		return result
	}

	var pgVersion string
	if err := pool.QueryRow(dialCtx, "SELECT version()").Scan(&pgVersion); err != nil {
		cat, detail := Classify(err)
		result.Category = cat
		result.Detail = collector.RedactDSN(detail)
		result.Duration = time.Since(start)
		return result
	}
	result.PGVersion = parsePGVersion(pgVersion)

	safety, err := collector.ValidateRoleSafety(dialCtx, pool)
	if err != nil {
		result.Category = CategoryRole
		result.Detail = "role check: " + collector.RedactError(err).Error()
		result.Duration = time.Since(start)
		return result
	}
	if !safety.IsSafe() {
		result.Category = CategoryRole
		result.Detail = safety.Error()
		result.Duration = time.Since(start)
		return result
	}

	result.Duration = time.Since(start)
	result.Category = CategoryOK
	result.Detail = fmt.Sprintf("connected to %s:%d/%s as %s (PG %s) in %dms",
		tgt.Host, tgt.Port, tgt.DBName, tgt.User,
		result.PGVersion, result.Duration.Milliseconds())
	return result
}

// parsePGVersion extracts the X.Y form from "PostgreSQL 16.2 on ...".
// Falls back to the raw string on parse failure.
func parsePGVersion(full string) string {
	fields := strings.Fields(full)
	if len(fields) >= 2 && fields[0] == "PostgreSQL" {
		return fields[1]
	}
	return full
}

// Run executes a Report over either every enabled target in
// configPath (when targetName == "" and adhoc == nil), one target
// from config (targetName set), or an ad-hoc DSN (adhoc set).
//
// adhoc carries DSN fields: host, port, dbname, user, sslmode,
// password_env, password_file, pgpass_file. Missing required
// fields return a usage error (FC-CONN-02). The CLI translates
// the returned error to exit code 2.
//
// Multi-target output ordering matches config-declared target
// order regardless of completion timing (INV-CONN-04). Per-target
// dispatch runs in parallel with a small concurrency cap.
func Run(ctx context.Context, configPath, targetName string, adhoc map[string]string, opts Options) (Report, error) {
	report := Report{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
	}

	// Ad-hoc DSN path: ignores configPath entirely.
	if adhoc != nil {
		tgt, err := targetFromAdhoc(adhoc)
		if err != nil {
			return report, err
		}
		r := TestConnection(ctx, tgt, opts)
		report.Attempts = []Result{r}
		report.Summary = summarize(report.Attempts)
		return report, nil
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return report, fmt.Errorf("load config %s: %w", configPath, err)
	}

	// Filter targets per the inputs.
	var targets []config.TargetConfig
	if targetName != "" {
		for _, t := range cfg.Targets {
			if t.Name == targetName {
				targets = []config.TargetConfig{t}
				break
			}
		}
		if len(targets) == 0 {
			names := make([]string, 0, len(cfg.Targets))
			for _, t := range cfg.Targets {
				names = append(names, t.Name)
			}
			return report, fmt.Errorf("unknown target %q (configured: %s)", targetName, strings.Join(names, ", "))
		}
	} else {
		for _, t := range cfg.Targets {
			if t.Enabled {
				targets = append(targets, t)
			}
		}
	}

	// Pre-allocate results indexed by config slot so output order is
	// deterministic regardless of completion timing (INV-CONN-04).
	report.Attempts = make([]Result, len(targets))
	if len(targets) == 1 {
		report.Attempts[0] = TestConnection(ctx, targets[0], opts)
	} else {
		runParallel(ctx, targets, opts, report.Attempts)
	}
	report.Summary = summarize(report.Attempts)
	return report, nil
}

// runParallel dispatches per-target TestConnection calls into goroutines
// with a small concurrency cap. Results land in `out` keyed by slot.
//
// Honours parent ctx cancellation on the semaphore acquire (#96).
// If the caller cancels the context mid-dispatch, the loop stops
// scheduling new goroutines; already-running goroutines still
// complete because TestConnection's internal dialCtx is derived
// from the parent (cancellation propagates naturally inside it).
// A WaitGroup replaces the ad-hoc done channel for simpler
// shutdown semantics.
func runParallel(ctx context.Context, targets []config.TargetConfig, opts Options, out []Result) {
	const cap = 8
	sem := make(chan struct{}, cap)
	var wg sync.WaitGroup
	for i, t := range targets {
		// Honour context cancellation on the semaphore acquire so a
		// caller killing the test process during pause mid-dispatch
		// stops scheduling further work. Prior goroutines continue
		// until their TestConnection returns (bounded by 3s
		// connect_timeout).
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case sem <- struct{}{}:
		}
		i, t := i, t
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			out[i] = TestConnection(ctx, t, opts)
		}()
	}
	wg.Wait()
}

func targetFromAdhoc(adhoc map[string]string) (config.TargetConfig, error) {
	tgt := config.TargetConfig{Name: "<adhoc>", Enabled: true}
	if v, ok := adhoc["host"]; ok {
		tgt.Host = v
	}
	if v, ok := adhoc["port"]; ok {
		p, err := parsePort(v)
		if err != nil {
			return tgt, fmt.Errorf("--dsn: port: %w", err)
		}
		tgt.Port = p
	}
	if v, ok := adhoc["dbname"]; ok {
		tgt.DBName = v
	}
	if v, ok := adhoc["user"]; ok {
		tgt.User = v
	}
	if v, ok := adhoc["sslmode"]; ok {
		tgt.SSLMode = v
	}
	if v, ok := adhoc["password_env"]; ok {
		tgt.PasswordEnv = v
	}
	if v, ok := adhoc["password_file"]; ok {
		tgt.PasswordFile = v
	}
	if v, ok := adhoc["pgpass_file"]; ok {
		tgt.PgpassFile = v
	}
	for _, req := range []struct {
		name, value string
	}{
		{"host", tgt.Host}, {"dbname", tgt.DBName}, {"user", tgt.User},
	} {
		if req.value == "" {
			return tgt, fmt.Errorf("--dsn: %s is required", req.name)
		}
	}
	if tgt.Port == 0 {
		return tgt, fmt.Errorf("--dsn: port is required")
	}
	return tgt, nil
}

func parsePort(s string) (int, error) {
	var port int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("not a number: %q", s)
		}
		port = port*10 + int(c-'0')
	}
	if port == 0 || port > 65535 {
		return 0, fmt.Errorf("out of range: %q", s)
	}
	return port, nil
}

func summarize(attempts []Result) Summary {
	var s Summary
	for _, a := range attempts {
		if a.Category == CategoryOK {
			s.OK++
		} else {
			s.Fail++
		}
	}
	return s
}
