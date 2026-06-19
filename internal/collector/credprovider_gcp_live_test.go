//go:build integration

package collector

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/elevarq/signals/internal/config"
)

// ---------------------------------------------------------------------------
// gcp_cloudsql_iam — live passwordless smoke (AC-GCP-008, #96).
//
// This is the only GCP test that makes real Google OAuth2 + Cloud SQL
// calls. It is doubly gated: the `integration` build tag AND
// SIGNALS_INTEGRATION_LIVE=1. It never runs in default CI. It acquires
// a real Cloud SQL IAM access token from the collector's ambient Google
// identity (Application Default Credentials, optionally impersonating a
// service account), connects to a real Cloud SQL target passwordlessly over
// verify-full TLS (direct libpq path), runs the read-only discovery probe
// (the core of a snapshot), then forces a real token re-acquire across the
// refresh skew and reconnects with the new token.
//
// Required env (in addition to SIGNALS_INTEGRATION_LIVE=1):
//
//	SIGNALS_TEST_GCP_HOST          Cloud SQL instance IP / hostname
//	SIGNALS_TEST_GCP_DBNAME        database to connect to
//	SIGNALS_TEST_GCP_USER          DB role registered as a Cloud SQL IAM user
//	SIGNALS_TEST_GCP_SSLROOTCERT   path to the server-CA bundle (verify-full)
//	SIGNALS_TEST_GCP_IMPERSONATE   optional; service account to impersonate
//	SIGNALS_TEST_GCP_PORT          optional; defaults to 5432
//
// An ambient Google identity must be present (Application Default
// Credentials: gcloud auth application-default login, workload identity, or
// a service-account key via GOOGLE_APPLICATION_CREDENTIALS) and the DB role
// SIGNALS_TEST_GCP_USER must be a Cloud SQL IAM database user. Run:
//
//	SIGNALS_INTEGRATION_LIVE=1 \
//	SIGNALS_TEST_GCP_HOST=10.10.0.5 \
//	SIGNALS_TEST_GCP_DBNAME=appdb SIGNALS_TEST_GCP_USER=monitor@my-proj.iam \
//	SIGNALS_TEST_GCP_SSLROOTCERT=/etc/ssl/gcp-server-ca.pem \
//	  go test -tags integration ./internal/collector/ -run Live_GCPCloudSQLIAM -v
//
// Specification: features/signals/credential-provider-gcp-cloudsql-iam.md
// ---------------------------------------------------------------------------
func TestLive_GCPCloudSQLIAMPasswordlessConnectAndReacquire(t *testing.T) {
	if os.Getenv("SIGNALS_INTEGRATION_LIVE") != "1" {
		t.Skip("SIGNALS_INTEGRATION_LIVE != 1 — skipping live gcp_cloudsql_iam smoke")
	}
	host := os.Getenv("SIGNALS_TEST_GCP_HOST")
	dbname := os.Getenv("SIGNALS_TEST_GCP_DBNAME")
	user := os.Getenv("SIGNALS_TEST_GCP_USER")
	caFile := os.Getenv("SIGNALS_TEST_GCP_SSLROOTCERT")
	if host == "" || dbname == "" || user == "" || caFile == "" {
		t.Skip("SIGNALS_TEST_GCP_HOST/DBNAME/USER/SSLROOTCERT not all set — skipping live gcp_cloudsql_iam smoke")
	}
	port := 5432
	if p := os.Getenv("SIGNALS_TEST_GCP_PORT"); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil {
			t.Fatalf("SIGNALS_TEST_GCP_PORT %q is not an integer: %v", p, err)
		}
		port = n
	}

	tgt := config.TargetConfig{
		Name:                         "live-gcp-cloudsql-iam",
		Host:                         host,
		Port:                         port,
		DBName:                       dbname,
		User:                         user,
		SSLMode:                      "verify-full",
		SSLRootCertFile:              caFile,
		AuthMethod:                   config.AuthMethodGCPCloudSQLIAM,
		GCPImpersonateServiceAccount: os.Getenv("SIGNALS_TEST_GCP_IMPERSONATE"), // empty → ambient ADC
		Enabled:                      true,
	}

	// Guard: the target must pass the same startup validation as
	// production (passwordless + verify-full).
	if _, err := config.ValidateStrict(config.Config{
		Env:      "prod",
		Database: config.DatabaseConfig{Path: "/tmp/signals-live-gcp-smoke.db"},
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

	// Real minter, but an injectable clock so the re-acquire across the
	// refresh skew is exercised without a wall-clock wait near the token's
	// ~60 minute lifetime.
	clock := &fakeClock{t: time.Now().UTC()}
	r := &credentialResolver{
		cache:     newTokenCache(),
		gcpMinter: gcpADCTokenMinter{},
		now:       clock.now,
		logger:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// First resolution: acquire a real token and connect passwordlessly.
	cred1, err := r.Resolve(ctx, tgt)
	if err != nil {
		t.Fatalf("first Resolve (acquire): %v\n%s", err, GCPCloudSQLGuidance(tgt))
	}
	if cred1.Password == "" {
		t.Fatal("acquired token is empty")
	}
	liveConnectAndSnapshot(ctx, t, tgt, cred1, "initial token")

	// Within the skew → cached reuse, no new token.
	clock.advance(30 * time.Minute)
	reuse, err := r.Resolve(ctx, tgt)
	if err != nil {
		t.Fatalf("reuse Resolve: %v", err)
	}
	if reuse.Password != cred1.Password {
		t.Errorf("expected cached token reuse within skew; token changed")
	}

	// Cross the refresh skew → a genuine second acquire, then reconnect
	// with the freshly acquired token. The token lifetime drives the skew;
	// advancing past (ttl - skew) forces re-acquisition.
	clock.advance(time.Until(cred1.ExpiresAt.Add(time.Minute)) + 30*time.Minute)
	cred2, err := r.Resolve(ctx, tgt)
	if err != nil {
		t.Fatalf("re-acquire Resolve: %v", err)
	}
	liveConnectAndSnapshot(ctx, t, tgt, cred2, "re-acquired token")
}
