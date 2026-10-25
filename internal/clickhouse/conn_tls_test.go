package clickhouse

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Tests derive from specifications/clickhouse-connection-hardening.acceptance.md (#558).

func writeTestCA(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "ch-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create ca: %v", err)
	}
	p := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(p, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("write ca: %v", err)
	}
	return p
}

// TC-CCH-01: disable/empty sslmode is plaintext (nil config, no error).
func TestResolveTLS_DisablePlaintext(t *testing.T) {
	for _, mode := range []string{"", "disable", "DISABLE", " disabled "} {
		cfg, err := ConnConfig{Host: "h", TLSMode: mode}.resolveTLS()
		if err != nil {
			t.Errorf("mode %q: unexpected error %v", mode, err)
		}
		if cfg != nil {
			t.Errorf("mode %q: want nil tls.Config (plaintext), got %+v", mode, cfg)
		}
	}
}

// TC-CCH-02: require applies TLS without verification (non-nil, skip-verify).
func TestResolveTLS_RequireEncryptsNoVerify(t *testing.T) {
	cfg, err := ConnConfig{Host: "h", TLSMode: "require"}.resolveTLS()
	if err != nil {
		t.Fatalf("require: %v", err)
	}
	if cfg == nil {
		t.Fatal("require must apply TLS (non-nil), never plaintext")
	}
	if !cfg.InsecureSkipVerify {
		t.Error("require is encryption-only (InsecureSkipVerify true)")
	}
}

// TC-CCH-03: verify-full verifies chain + hostname (ServerName set).
func TestResolveTLS_VerifyFullSetsServerName(t *testing.T) {
	ca := writeTestCA(t)
	cfg, err := ConnConfig{Host: "ch.example", TLSMode: "verify-full", TLSRootCAFile: ca}.resolveTLS()
	if err != nil {
		t.Fatalf("verify-full: %v", err)
	}
	if cfg == nil || cfg.ServerName != "ch.example" {
		t.Errorf("verify-full must set ServerName to host; got %+v", cfg)
	}
	if cfg.InsecureSkipVerify {
		t.Error("verify-full must not skip verification")
	}
}

// TC-CCH-04: verify-ca verifies chain, not hostname (no ServerName, custom verifier).
func TestResolveTLS_VerifyCAChainNotHostname(t *testing.T) {
	ca := writeTestCA(t)
	cfg, err := ConnConfig{Host: "ch.example", TLSMode: "verify-ca", TLSRootCAFile: ca}.resolveTLS()
	if err != nil {
		t.Fatalf("verify-ca: %v", err)
	}
	if cfg == nil {
		t.Fatal("verify-ca must apply TLS")
	}
	if cfg.ServerName != "" {
		t.Errorf("verify-ca must not set ServerName (hostname skipped), got %q", cfg.ServerName)
	}
	if !cfg.InsecureSkipVerify || cfg.VerifyPeerCertificate == nil {
		t.Error("verify-ca must verify the chain via a custom VerifyPeerCertificate")
	}
}

// TC-CCH-05: a verify mode with a bad/missing CA fails closed at Open (no plaintext).
func TestOpen_VerifyModeBadCAFailsClosed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for _, mode := range []string{"verify-ca", "verify-full"} {
		_, err := Open(ctx, ConnConfig{Host: "127.0.0.1", Port: 9, TLSMode: mode, TLSRootCAFile: "/nonexistent/ca.pem"})
		if err == nil {
			t.Errorf("mode %q with missing CA must fail closed, got nil error", mode)
			continue
		}
		if !strings.Contains(err.Error(), "resolve TLS") {
			t.Errorf("mode %q: want a TLS-resolve (fail-closed) error before dialing, got %v", mode, err)
		}
	}
}

// TC-CCH-06: unknown sslmode is a hard error (never silent plaintext).
func TestResolveTLS_UnknownModeErrors(t *testing.T) {
	cfg, err := ConnConfig{Host: "h", TLSMode: "bogus"}.resolveTLS()
	if err == nil {
		t.Error("unknown sslmode must be a hard error")
	}
	if cfg != nil {
		t.Error("unknown sslmode must not return a config")
	}
}
