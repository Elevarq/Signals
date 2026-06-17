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
// secret_store — live passwordless smoke (AC-SECRET-011 required;
// AC-SECRET-013 optional rotation, #97).
//
// This is the only secret_store test that makes real cloud calls. It is
// doubly gated: the `integration` build tag AND ARQ_SIGNALS_INTEGRATION_LIVE=1.
// It never runs in default CI. It fetches a real database password from the
// secret store inferred from ARQ_TEST_SECRET_REF using the collector's
// ambient workload identity, connects to a self-managed PostgreSQL
// passwordlessly over verify-full TLS, and runs the read-only discovery probe
// (the core of a snapshot).
//
// Required env (in addition to ARQ_SIGNALS_INTEGRATION_LIVE=1):
//
//	ARQ_TEST_SECRET_REF         secret reference (AWS ARN / Key Vault URI /
//	                            GCP Secret Manager resource); shape selects
//	                            the backend
//	ARQ_TEST_SECRET_HOST        PostgreSQL host
//	ARQ_TEST_SECRET_DBNAME      database to connect to
//	ARQ_TEST_SECRET_USER        DB role whose password is the stored secret
//	ARQ_TEST_SECRET_SSLROOTCERT path to the server-CA bundle (verify-full)
//	ARQ_TEST_SECRET_JSON_KEY    optional; JSON key to extract (e.g. "password")
//	ARQ_TEST_SECRET_PORT        optional; defaults to 5432
//	ARQ_TEST_SECRET_ROTATE      optional; "1" runs the AC-SECRET-013 opt-in
//	                            rotation step (see below)
//
// The collector's ambient identity must be allowed to read the secret for
// whichever backend the ref selects (AWS: secretsmanager:GetSecretValue;
// Azure: Key Vault Secrets User "get"; GCP: secretmanager.versions.access).
// All three backends are production-wired. Run, for the AWS demo path:
//
//	ARQ_SIGNALS_INTEGRATION_LIVE=1 \
//	ARQ_TEST_SECRET_REF=arn:aws:secretsmanager:eu-west-1:123456789012:secret:prod/pg/monitor-AbCdEf \
//	ARQ_TEST_SECRET_HOST=db.internal ARQ_TEST_SECRET_DBNAME=appdb \
//	ARQ_TEST_SECRET_USER=monitor ARQ_TEST_SECRET_SSLROOTCERT=/etc/ssl/rds-ca.pem \
//	ARQ_TEST_SECRET_JSON_KEY=password \
//	  go test -tags integration ./internal/collector/ -run Live_SecretStore -v
//
// Specification: features/arq-signals/credential-provider-secret-store.md
// ---------------------------------------------------------------------------
func TestLive_SecretStorePasswordlessConnect(t *testing.T) {
	if os.Getenv("ARQ_SIGNALS_INTEGRATION_LIVE") != "1" {
		t.Skip("ARQ_SIGNALS_INTEGRATION_LIVE != 1 — skipping live secret_store smoke")
	}
	ref := os.Getenv("ARQ_TEST_SECRET_REF")
	host := os.Getenv("ARQ_TEST_SECRET_HOST")
	dbname := os.Getenv("ARQ_TEST_SECRET_DBNAME")
	user := os.Getenv("ARQ_TEST_SECRET_USER")
	caFile := os.Getenv("ARQ_TEST_SECRET_SSLROOTCERT")
	if ref == "" || host == "" || dbname == "" || user == "" || caFile == "" {
		t.Skip("ARQ_TEST_SECRET_REF/HOST/DBNAME/USER/SSLROOTCERT not all set — skipping live secret_store smoke")
	}
	port := 5432
	if p := os.Getenv("ARQ_TEST_SECRET_PORT"); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil {
			t.Fatalf("ARQ_TEST_SECRET_PORT %q is not an integer: %v", p, err)
		}
		port = n
	}

	tgt := config.TargetConfig{
		Name:            "live-secret-store",
		Host:            host,
		Port:            port,
		DBName:          dbname,
		User:            user,
		SSLMode:         "verify-full",
		SSLRootCertFile: caFile,
		AuthMethod:      config.AuthMethodSecretStore,
		SecretRef:       ref,
		SecretJSONKey:   os.Getenv("ARQ_TEST_SECRET_JSON_KEY"), // empty → raw value
		Enabled:         true,
	}

	// Guard: the target must pass the same startup validation as production
	// (passwordless + verify-full + a recognised secret_ref).
	if _, err := config.ValidateStrict(config.Config{
		Env:      "prod",
		Database: config.DatabaseConfig{Path: "/tmp/arq-signals-live-secret-smoke.db"},
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

	// Real production fetcher (all three backends wired); the ref shape selects
	// which one runs. Injectable clock so the optional rotation step can force
	// a re-fetch deterministically.
	clock := &fakeClock{t: time.Now().UTC()}
	r := &credentialResolver{
		cache: newTokenCache(),
		secretFetcher: productionSecretFetcher{
			aws:   awsSecretsManagerFetcher{},
			azure: azureKeyVaultFetcher{},
			gcp:   gcpSecretManagerFetcher{},
		},
		now:    clock.now,
		logger: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// AC-SECRET-011 (required): fetch the secret, connect passwordlessly,
	// collect one snapshot.
	cred, err := r.Resolve(ctx, tgt)
	if err != nil {
		t.Fatalf("Resolve (fetch): %v\n%s", err, SecretStoreGuidance(tgt))
	}
	if cred.Password == "" {
		t.Fatal("fetched secret is empty")
	}
	liveConnectAndSnapshot(ctx, t, tgt, cred, "fetched secret")

	// AC-SECRET-013 (optional, opt-in): after the operator rotates the stored
	// secret out-of-band, force a re-fetch and reconnect with the new value.
	// Rotation mutates the live vault and is therefore NOT part of the
	// required path; rotation-on-reconnect is fully covered in CI by the
	// unit-level cache test.
	if os.Getenv("ARQ_TEST_SECRET_ROTATE") != "1" {
		t.Log("ARQ_TEST_SECRET_ROTATE != 1 — skipping the optional AC-SECRET-013 live rotation step")
		return
	}
	t.Log("AC-SECRET-013: rotate the stored secret now; the next Resolve must pick up the new value without a restart")
	// With no max_cache_ttl the secret re-fetches on every reconnect; advance
	// the clock as well so this holds even if a TTL/max_cache_ttl is set.
	clock.advance(24 * time.Hour)
	rotated, err := r.Resolve(ctx, tgt)
	if err != nil {
		t.Fatalf("Resolve (post-rotation re-fetch): %v", err)
	}
	liveConnectAndSnapshot(ctx, t, tgt, rotated, "post-rotation secret")
}
