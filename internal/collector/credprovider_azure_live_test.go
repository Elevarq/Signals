//go:build integration

package collector

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/elevarq/arq-signals/internal/config"
)

// ---------------------------------------------------------------------------
// azure_entra — live passwordless smoke (AC-AZURE-008, #95).
//
// This is the only Azure test that makes real Entra + Azure Database for
// PostgreSQL calls. It is doubly gated: the `integration` build tag AND
// ARQ_SIGNALS_INTEGRATION_LIVE=1. It never runs in default CI. It acquires
// a real Entra access token from the collector's ambient Azure identity,
// connects to a real Flexible Server target passwordlessly over
// verify-full TLS, runs the read-only discovery probe (the core of a
// snapshot), then forces a real token re-acquire across the refresh skew
// and reconnects with the new token.
//
// Required env (in addition to ARQ_SIGNALS_INTEGRATION_LIVE=1):
//
//	ARQ_TEST_AZ_HOST          Flexible Server endpoint hostname
//	ARQ_TEST_AZ_DBNAME        database to connect to
//	ARQ_TEST_AZ_USER          DB role mapped to the Entra principal
//	ARQ_TEST_AZ_SSLROOTCERT   path to the CA bundle (verify-full)
//	ARQ_TEST_AZ_CLIENT_ID     optional; user-assigned MI client id
//	ARQ_TEST_AZ_PORT          optional; defaults to 5432
//
// An ambient Azure identity must be present (DefaultAzureCredential chain:
// env / workload identity / managed identity / az login) and its principal
// must be mapped to ARQ_TEST_AZ_USER via pgaadauth_create_principal. Run:
//
//	ARQ_SIGNALS_INTEGRATION_LIVE=1 \
//	ARQ_TEST_AZ_HOST=mydb.postgres.database.azure.com \
//	ARQ_TEST_AZ_DBNAME=appdb ARQ_TEST_AZ_USER=monitor \
//	ARQ_TEST_AZ_SSLROOTCERT=/etc/ssl/azure-global-bundle.pem \
//	  go test -tags integration ./internal/collector/ -run Live_AzureEntra -v
//
// Specification: features/arq-signals/credential-provider-azure-entra.md
// ---------------------------------------------------------------------------
func TestLive_AzureEntraPasswordlessConnectAndReacquire(t *testing.T) {
	if os.Getenv("ARQ_SIGNALS_INTEGRATION_LIVE") != "1" {
		t.Skip("ARQ_SIGNALS_INTEGRATION_LIVE != 1 — skipping live azure_entra smoke")
	}
	host := os.Getenv("ARQ_TEST_AZ_HOST")
	dbname := os.Getenv("ARQ_TEST_AZ_DBNAME")
	user := os.Getenv("ARQ_TEST_AZ_USER")
	caFile := os.Getenv("ARQ_TEST_AZ_SSLROOTCERT")
	if host == "" || dbname == "" || user == "" || caFile == "" {
		t.Skip("ARQ_TEST_AZ_HOST/DBNAME/USER/SSLROOTCERT not all set — skipping live azure_entra smoke")
	}
	port := 5432
	if p := os.Getenv("ARQ_TEST_AZ_PORT"); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil {
			t.Fatalf("ARQ_TEST_AZ_PORT %q is not an integer: %v", p, err)
		}
		port = n
	}

	tgt := config.TargetConfig{
		Name:            "live-azure-entra",
		Host:            host,
		Port:            port,
		DBName:          dbname,
		User:            user,
		SSLMode:         "verify-full",
		SSLRootCertFile: caFile,
		AuthMethod:      config.AuthMethodAzureEntra,
		AzureClientID:   os.Getenv("ARQ_TEST_AZ_CLIENT_ID"), // empty → chain default
		Enabled:         true,
	}

	// Guard: the target must pass the same startup validation as
	// production (passwordless + verify-full).
	if _, err := config.ValidateStrict(config.Config{
		Env:      "prod",
		Database: config.DatabaseConfig{Path: "/tmp/arq-signals-live-azure-smoke.db"},
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
	// ~60-90 minute lifetime.
	clock := &fakeClock{t: time.Now().UTC()}
	r := &credentialResolver{
		cache:       newTokenCache(),
		azureMinter: azureEntraTokenMinter{},
		now:         clock.now,
		logger:      slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// First resolution: acquire a real token and connect passwordlessly.
	cred1, err := r.Resolve(ctx, tgt)
	if err != nil {
		t.Fatalf("first Resolve (acquire): %v\n%s", err, AzureEntraGuidance(tgt))
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
