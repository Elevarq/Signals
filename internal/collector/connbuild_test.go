package collector

import (
	"crypto/tls"
	"strings"
	"testing"

	"github.com/elevarq/signals/internal/config"
)

// TestBuildConnConfigWithCredential_Password applies a password-kind
// credential without re-reading any password source.
func TestBuildConnConfigWithCredential_Password(t *testing.T) {
	tgt := config.TargetConfig{
		Name: "t", Host: "db.example.com", Port: 5432, DBName: "app",
		User: "signals", SSLMode: "verify-full",
	}
	cfg, err := BuildConnConfigWithCredential(tgt, Credential{Kind: CredKindPassword, Password: "the-token"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Password != "the-token" {
		t.Errorf("password not applied: %q", cfg.Password)
	}
	if cfg.RuntimeParams["application_name"] != AppName {
		t.Errorf("application_name not set: %q", cfg.RuntimeParams["application_name"])
	}
	if cfg.RuntimeParams["default_transaction_read_only"] != "on" {
		t.Error("default_transaction_read_only must be on")
	}
}

// TestBuildConnConfigWithCredential_Certificate applies a certificate-kind
// credential to the connection's TLS config.
func TestBuildConnConfigWithCredential_Certificate(t *testing.T) {
	tgt := config.TargetConfig{
		Name: "t", Host: "db.example.com", Port: 5432, DBName: "app",
		User: "signals", SSLMode: "verify-full",
	}
	cert := &tls.Certificate{}
	cfg, err := BuildConnConfigWithCredential(tgt, Credential{Kind: CredKindCertificate, ClientCert: cert})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.TLSConfig == nil || len(cfg.TLSConfig.Certificates) != 1 {
		t.Fatalf("client certificate not applied to TLS config: %+v", cfg.TLSConfig)
	}
	if cfg.Password != "" {
		t.Error("certificate credential must not set a password")
	}
}

// TestBuildConnConfigWithCredential_CertRequiresTLS rejects a certificate
// credential when TLS is disabled (no server verification).
func TestBuildConnConfigWithCredential_CertRequiresTLS(t *testing.T) {
	tgt := config.TargetConfig{
		Name: "t", Host: "db.example.com", Port: 5432, DBName: "app",
		User: "signals", SSLMode: "disable",
	}
	if _, err := BuildConnConfigWithCredential(tgt, Credential{Kind: CredKindCertificate, ClientCert: &tls.Certificate{}}); err == nil {
		t.Fatal("expected error for certificate credential without TLS")
	}
}

// TestBuildConnConfigWithCredential_MissingHost is a usage error.
func TestBuildConnConfigWithCredential_MissingHost(t *testing.T) {
	if _, err := BuildConnConfigWithCredential(config.TargetConfig{Name: "t"}, Credential{Kind: CredKindPassword}); err == nil {
		t.Fatal("expected error for missing host")
	}
}

// TestMTLSGuidance is the AC-MTLS-010 guidance: the exact pg_hba clause,
// the role, and no key material.
func TestMTLSGuidance(t *testing.T) {
	g := MTLSGuidance(config.TargetConfig{Name: "prod", DBName: "app", User: "signals"})
	for _, want := range []string{"hostssl", "clientcert=verify-full", "signals", "app", "ssl_ca_file"} {
		if !strings.Contains(g, want) {
			t.Errorf("guidance missing %q:\n%s", want, g)
		}
	}
	for _, forbidden := range []string{"PRIVATE KEY", "BEGIN", "passphrase="} {
		if strings.Contains(g, forbidden) {
			t.Errorf("guidance must not contain key material %q:\n%s", forbidden, g)
		}
	}
}
