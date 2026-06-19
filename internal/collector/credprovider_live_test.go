//go:build integration

package collector

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/elevarq/signals/internal/config"
	"github.com/elevarq/signals/internal/pgqueries"
)

// ---------------------------------------------------------------------------
// aws_rds_iam — live passwordless smoke (AC-AWS-008, #94).
//
// This is the only test that makes real AWS + RDS calls. It is doubly
// gated: the `integration` build tag AND SIGNALS_INTEGRATION_LIVE=1.
// It never runs in default CI. It mints a real RDS IAM token from the
// ambient AWS identity, connects to a real RDS/Aurora PostgreSQL target
// passwordlessly over verify-full TLS, runs the read-only discovery
// probe (the core of a snapshot), then forces a real token re-mint
// across the refresh skew and reconnects with the new token.
//
// Required env (in addition to SIGNALS_INTEGRATION_LIVE=1):
//
//	SIGNALS_TEST_RDS_HOST          RDS/Aurora endpoint hostname
//	SIGNALS_TEST_RDS_DBNAME        database to connect to
//	SIGNALS_TEST_RDS_USER          DB role granted rds_iam
//	SIGNALS_TEST_RDS_SSLROOTCERT   path to the RDS CA bundle (verify-full)
//	SIGNALS_TEST_RDS_REGION        optional; else resolved from env/IMDS
//	SIGNALS_TEST_RDS_PORT          optional; defaults to 5432
//
// Ambient AWS credentials must be present (SDK default chain) and the
// principal must allow rds-db:connect for SIGNALS_TEST_RDS_USER. Run with:
//
//	SIGNALS_INTEGRATION_LIVE=1 \
//	SIGNALS_TEST_RDS_HOST=mydb.abc123.us-east-1.rds.amazonaws.com \
//	SIGNALS_TEST_RDS_DBNAME=appdb SIGNALS_TEST_RDS_USER=monitor \
//	SIGNALS_TEST_RDS_SSLROOTCERT=/etc/ssl/rds-global-bundle.pem \
//	AWS_PROFILE=elevarq AWS_REGION=us-east-1 \
//	  go test -tags integration ./internal/collector/ -run Live_AWSRDSIAM -v
//
// Specification: features/signals/credential-provider-aws-rds-iam.md
// ---------------------------------------------------------------------------
func TestLive_AWSRDSIAMPasswordlessConnectAndRemint(t *testing.T) {
	if os.Getenv("SIGNALS_INTEGRATION_LIVE") != "1" {
		t.Skip("SIGNALS_INTEGRATION_LIVE != 1 — skipping live aws_rds_iam smoke")
	}
	host := os.Getenv("SIGNALS_TEST_RDS_HOST")
	dbname := os.Getenv("SIGNALS_TEST_RDS_DBNAME")
	user := os.Getenv("SIGNALS_TEST_RDS_USER")
	caFile := os.Getenv("SIGNALS_TEST_RDS_SSLROOTCERT")
	if host == "" || dbname == "" || user == "" || caFile == "" {
		t.Skip("SIGNALS_TEST_RDS_HOST/DBNAME/USER/SSLROOTCERT not all set — skipping live aws_rds_iam smoke")
	}
	port := 5432
	if p := os.Getenv("SIGNALS_TEST_RDS_PORT"); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil {
			t.Fatalf("SIGNALS_TEST_RDS_PORT %q is not an integer: %v", p, err)
		}
		port = n
	}

	tgt := config.TargetConfig{
		Name:            "live-rds-iam",
		Host:            host,
		Port:            port,
		DBName:          dbname,
		User:            user,
		SSLMode:         "verify-full",
		SSLRootCertFile: caFile,
		AuthMethod:      config.AuthMethodAWSRDSIAM,
		Region:          os.Getenv("SIGNALS_TEST_RDS_REGION"), // empty → env/IMDS
		Enabled:         true,
	}

	// Guard: the target must pass the same startup validation as
	// production (passwordless + verify-full).
	if _, err := config.ValidateStrict(config.Config{
		Env:      "prod",
		Database: config.DatabaseConfig{Path: "/tmp/signals-live-smoke.db"},
		API:      config.APIConfig{ListenAddr: "127.0.0.1:8099"},
		Signals: config.SignalsConfig{
			PollInterval:        time.Minute,
			TargetTimeout:       60 * time.Second,
			QueryTimeout:        10 * time.Second,
			MinSnapshotInterval: 60 * time.Second,
		},
		Targets: []config.TargetConfig{tgt},
	}); err != nil {
		t.Fatalf("live target fails startup validation: %v", err)
	}

	// Real minter + real region resolver, but an injectable clock so the
	// re-mint across the refresh skew is exercised without a 12-minute
	// wall-clock wait.
	clock := &fakeClock{t: time.Now().UTC()}
	r := &credentialResolver{
		cache:  newTokenCache(),
		minter: awsRDSTokenMinter{},
		region: resolveAWSRegion,
		now:    clock.now,
		logger: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// First resolution: mint a real token and connect passwordlessly.
	cred1, err := r.Resolve(ctx, tgt)
	if err != nil {
		t.Fatalf("first Resolve (mint): %v\n%s", err, AWSGrantGuidance(tgt))
	}
	if cred1.Password == "" {
		t.Fatal("minted token is empty")
	}
	liveConnectAndSnapshot(ctx, t, tgt, cred1, "initial token")

	// Within the skew → cached reuse, no new token.
	clock.advance(5 * time.Minute)
	reuse, err := r.Resolve(ctx, tgt)
	if err != nil {
		t.Fatalf("reuse Resolve: %v", err)
	}
	if reuse.Password != cred1.Password {
		t.Errorf("expected cached token reuse within skew; token changed")
	}

	// Cross the refresh skew → a genuine second mint, then reconnect
	// with the freshly minted token.
	clock.advance(8 * time.Minute) // now 13 min old > 12 min refreshAt
	cred2, err := r.Resolve(ctx, tgt)
	if err != nil {
		t.Fatalf("re-mint Resolve: %v", err)
	}
	if cred2.Password == cred1.Password {
		t.Error("expected a re-minted token after crossing the refresh skew; token unchanged")
	}
	liveConnectAndSnapshot(ctx, t, tgt, cred2, "re-minted token")
}

// liveConnectAndSnapshot opens a single read-only connection using the
// resolved credential as the password and runs the discovery probe — the
// read-only core of a collection snapshot — asserting it succeeds.
func liveConnectAndSnapshot(ctx context.Context, t *testing.T, tgt config.TargetConfig, cred Credential, label string) {
	t.Helper()
	cfg, err := BuildConnConfig(tgt)
	if err != nil {
		t.Fatalf("%s: build conn config: %v", label, err)
	}
	cfg.Password = cred.Password // the minted IAM token is the password

	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("%s: passwordless connect failed: %v", label, err)
	}
	defer conn.Close(ctx)

	tx, err := conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatalf("%s: begin read-only tx: %v", label, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	disc, err := pgqueries.Discover(ctx, tx)
	if err != nil {
		t.Fatalf("%s: discovery probe failed: %v", label, err)
	}
	if disc.MajorVersion == 0 {
		t.Fatalf("%s: discovery returned no server version", label)
	}
	t.Logf("%s: connected passwordlessly to PG %d (%d extensions)", label, disc.MajorVersion, len(disc.Extensions))
}
