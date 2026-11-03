package collector

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

	"github.com/elevarq/signals/internal/config"
)

// genCertKeyPEM produces a self-signed cert + its EC private key as PEM, in
// test only (NFR003 — no operator key material is read).
func genCertKeyPEM(t *testing.T, notAfter time.Time) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "signals"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM
}

// writePair writes cert+key PEM to files in dir, returning their paths.
func writePair(t *testing.T, dir string, certPEM, keyPEM []byte) (certFile, keyFile string) {
	t.Helper()
	certFile = filepath.Join(dir, "client.crt")
	keyFile = filepath.Join(dir, "client.key")
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	return certFile, keyFile
}

// AC-MTLS-001 — a valid cert/key pair loads; ExpiresAt = cert NotAfter.
func TestFileCertLoaderLoadsValidPair(t *testing.T) {
	dir := t.TempDir()
	notAfter := time.Now().Add(48 * time.Hour).Truncate(time.Second)
	certPEM, keyPEM := genCertKeyPEM(t, notAfter)
	certFile, keyFile := writePair(t, dir, certPEM, keyPEM)

	cert, err := (fileCertLoader{}).Load(certFile, keyFile)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cert.Leaf == nil {
		t.Fatal("expected leaf parsed for NotAfter metadata")
	}
	if !cert.Leaf.NotAfter.Equal(notAfter) {
		t.Errorf("NotAfter = %v, want %v", cert.Leaf.NotAfter, notAfter)
	}
}

// AC-MTLS-001 — resolveMTLS returns a certificate-kind credential carrying the
// cert, with advisory ExpiresAt = NotAfter.
func TestResolveMTLSCertKindAndExpiry(t *testing.T) {
	dir := t.TempDir()
	notAfter := time.Now().Add(72 * time.Hour).Truncate(time.Second)
	certPEM, keyPEM := genCertKeyPEM(t, notAfter)
	certFile, keyFile := writePair(t, dir, certPEM, keyPEM)

	r := &credentialResolver{certLoader: fileCertLoader{}, now: time.Now}
	tgt := config.TargetConfig{Name: "t", AuthMethod: config.AuthMethodMTLS, SSLCert: certFile, SSLKey: keyFile}

	cred, err := r.resolveMTLS(context.Background(), tgt)
	if err != nil {
		t.Fatalf("resolveMTLS: %v", err)
	}
	if cred.Kind != CredKindCertificate {
		t.Errorf("Kind = %v, want CredKindCertificate", cred.Kind)
	}
	if cred.ClientCert == nil {
		t.Error("ClientCert is nil")
	}
	if cred.Password != "" {
		t.Error("certificate-kind credential must carry no password")
	}
	if !cred.ExpiresAt.Equal(notAfter) {
		t.Errorf("ExpiresAt = %v, want %v", cred.ExpiresAt, notAfter)
	}
}

// AC-MTLS-007 — a mismatched cert/key pair fails with an error that does not
// leak key material.
func TestFileCertLoaderMismatchedPair(t *testing.T) {
	dir := t.TempDir()
	certPEM, _ := genCertKeyPEM(t, time.Now().Add(time.Hour))
	_, otherKeyPEM := genCertKeyPEM(t, time.Now().Add(time.Hour))
	certFile, keyFile := writePair(t, dir, certPEM, otherKeyPEM)

	_, err := (fileCertLoader{}).Load(certFile, keyFile)
	if err == nil {
		t.Fatal("expected error for mismatched cert/key pair")
	}
	if strings.Contains(err.Error(), "PRIVATE KEY") || strings.Contains(err.Error(), string(otherKeyPEM)) {
		t.Errorf("error leaks key material: %v", err)
	}
}

// AC-MTLS-007 — a non-PEM key file fails (redacted).
func TestFileCertLoaderNonPEMKey(t *testing.T) {
	dir := t.TempDir()
	certPEM, _ := genCertKeyPEM(t, time.Now().Add(time.Hour))
	certFile, keyFile := writePair(t, dir, certPEM, []byte("not a pem key"))

	if _, err := (fileCertLoader{}).Load(certFile, keyFile); err == nil {
		t.Fatal("expected error for non-PEM key")
	}
}

// AC-MTLS-002 (#654, FC-MTLS-003) — an encrypted private key is rejected with a
// clear, redaction-safe message, detected from the PEM headers without any
// decrypt attempt. Covers both the legacy RFC-1423 form (Proc-Type/DEK-Info)
// and the PKCS#8 ENCRYPTED PRIVATE KEY block type. No deprecated x509
// encrypted-PEM API is used (that is exactly what #654 removes).
func TestFileCertLoaderEncryptedKeyRejected(t *testing.T) {
	dir := t.TempDir()
	certPEM, keyPEM := genCertKeyPEM(t, time.Now().Add(time.Hour))
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		t.Fatal("decode key pem")
	}

	cases := map[string][]byte{
		// Legacy RFC-1423 encrypted PEM — reconstructed from headers only
		// (never encrypted via the deprecated API).
		"legacy-rfc1423": pem.EncodeToMemory(&pem.Block{
			Type: block.Type,
			Headers: map[string]string{
				"Proc-Type": "4,ENCRYPTED",
				"DEK-Info":  "AES-256-CBC,0123456789ABCDEF0123456789ABCDEF",
			},
			Bytes: block.Bytes,
		}),
		// PKCS#8 encrypted key — distinguished by its block type.
		"pkcs8-encrypted": pem.EncodeToMemory(&pem.Block{
			Type:  "ENCRYPTED PRIVATE KEY",
			Bytes: block.Bytes,
		}),
	}

	for name, encKeyPEM := range cases {
		t.Run(name, func(t *testing.T) {
			certFile, keyFile := writePair(t, dir, certPEM, encKeyPEM)
			_, err := (fileCertLoader{}).Load(certFile, keyFile)
			if err == nil {
				t.Fatal("expected error for an encrypted private key")
			}
			if !strings.Contains(err.Error(), "encrypted private key") || !strings.Contains(err.Error(), "not supported") {
				t.Errorf("error must clearly state encrypted keys are unsupported, got: %v", err)
			}
			// Must never leak the (pseudo) key bytes.
			if strings.Contains(err.Error(), string(block.Bytes)) {
				t.Errorf("error leaks key material: %v", err)
			}
		})
	}
}

// AC-MTLS-003 — the loader re-reads file content, so rotated material is picked
// up on the next call (no caching).
func TestFileCertLoaderRotation(t *testing.T) {
	dir := t.TempDir()
	a := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	certA, keyA := genCertKeyPEM(t, a)
	certFile, keyFile := writePair(t, dir, certA, keyA)

	c1, err := (fileCertLoader{}).Load(certFile, keyFile)
	if err != nil {
		t.Fatalf("load A: %v", err)
	}

	b := time.Now().Add(240 * time.Hour).Truncate(time.Second)
	certB, keyB := genCertKeyPEM(t, b)
	if err := os.WriteFile(certFile, certB, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, keyB, 0o600); err != nil {
		t.Fatal(err)
	}

	c2, err := (fileCertLoader{}).Load(certFile, keyFile)
	if err != nil {
		t.Fatalf("load B: %v", err)
	}
	if c1.Leaf.NotAfter.Equal(c2.Leaf.NotAfter) {
		t.Error("rotation not observed: loader returned stale certificate")
	}
}

// AC-MTLS-009 (diagnostic facet) — BuildSafeDSN presents the client cert for
// doctor/conntest via libpq sslcert/sslkey params, and never emits an
// sslpassword field (INV-MTLS-001; encrypted keys are unsupported, #654).
func TestBuildSafeDSNMTLSIncludesCertNotPassphrase(t *testing.T) {
	tgt := config.TargetConfig{
		Name: "t", Host: "h", Port: 5432, DBName: "db", User: "u",
		SSLMode: "verify-full", SSLRootCertFile: "/ca.pem",
		AuthMethod: config.AuthMethodMTLS,
		SSLCert:    "/c.crt", SSLKey: "/c.key",
	}
	dsn, err := BuildSafeDSN(tgt)
	if err != nil {
		t.Fatalf("BuildSafeDSN: %v", err)
	}
	for _, want := range []string{"sslcert='/c.crt'", "sslkey='/c.key'", "sslrootcert='/ca.pem'"} {
		if !strings.Contains(dsn, want) {
			t.Errorf("DSN missing %q: %s", want, dsn)
		}
	}
	if strings.Contains(dsn, "sslpassword") {
		t.Errorf("no key passphrase field must appear in the DSN: %s", dsn)
	}
}
