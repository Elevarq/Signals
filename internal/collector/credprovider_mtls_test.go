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

	cert, err := (fileCertLoader{}).Load(certFile, keyFile, "")
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

	_, err := (fileCertLoader{}).Load(certFile, keyFile, "")
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

	if _, err := (fileCertLoader{}).Load(certFile, keyFile, ""); err == nil {
		t.Fatal("expected error for non-PEM key")
	}
}

// AC-MTLS-002 — an encrypted key loads with the right passphrase and fails with
// the wrong one (never echoing the passphrase or key).
func TestFileCertLoaderEncryptedKey(t *testing.T) {
	dir := t.TempDir()
	notAfter := time.Now().Add(time.Hour)
	certPEM, keyPEM := genCertKeyPEM(t, notAfter)

	block, _ := pem.Decode(keyPEM)
	if block == nil {
		t.Fatal("decode key pem")
	}
	//nolint:staticcheck // legacy PEM encryption is the stdlib path under test (NFR001)
	encBlock, err := x509.EncryptPEMBlock(rand.Reader, block.Type, block.Bytes, []byte("s3cret"), x509.PEMCipherAES256)
	if err != nil {
		t.Fatalf("encrypt key: %v", err)
	}
	encKeyPEM := pem.EncodeToMemory(encBlock)
	certFile, keyFile := writePair(t, dir, certPEM, encKeyPEM)
	passFile := filepath.Join(dir, "pass")

	// correct passphrase
	if err := os.WriteFile(passFile, []byte("s3cret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (fileCertLoader{}).Load(certFile, keyFile, passFile); err != nil {
		t.Fatalf("encrypted key with correct passphrase should load: %v", err)
	}

	// wrong passphrase
	if err := os.WriteFile(passFile, []byte("wrong"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = (fileCertLoader{}).Load(certFile, keyFile, passFile)
	if err == nil {
		t.Fatal("expected error for wrong passphrase")
	}
	if strings.Contains(err.Error(), "wrong") && strings.Contains(err.Error(), "s3cret") {
		t.Errorf("error must not echo the passphrase: %v", err)
	}
}

// AC-MTLS-003 — the loader re-reads file content, so rotated material is picked
// up on the next call (no caching).
func TestFileCertLoaderRotation(t *testing.T) {
	dir := t.TempDir()
	a := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	certA, keyA := genCertKeyPEM(t, a)
	certFile, keyFile := writePair(t, dir, certA, keyA)

	c1, err := (fileCertLoader{}).Load(certFile, keyFile, "")
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

	c2, err := (fileCertLoader{}).Load(certFile, keyFile, "")
	if err != nil {
		t.Fatalf("load B: %v", err)
	}
	if c1.Leaf.NotAfter.Equal(c2.Leaf.NotAfter) {
		t.Error("rotation not observed: loader returned stale certificate")
	}
}

// AC-MTLS-009 (diagnostic facet) — BuildSafeDSN presents the client cert for
// doctor/conntest via libpq sslcert/sslkey params, and never leaks the
// passphrase (INV-MTLS-001).
func TestBuildSafeDSNMTLSIncludesCertNotPassphrase(t *testing.T) {
	tgt := config.TargetConfig{
		Name: "t", Host: "h", Port: 5432, DBName: "db", User: "u",
		SSLMode: "verify-full", SSLRootCertFile: "/ca.pem",
		AuthMethod: config.AuthMethodMTLS,
		SSLCert:    "/c.crt", SSLKey: "/c.key", SSLKeyPassphraseFile: "/k.pass",
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
	if strings.Contains(dsn, "/k.pass") || strings.Contains(dsn, "sslpassword") {
		t.Errorf("passphrase must not appear in the DSN: %s", dsn)
	}
}
