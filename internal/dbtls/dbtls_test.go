package dbtls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Hermetic TLS verification tests for BuildVerifiedTLSConfig: a server presenting
// a cert signed by a test private CA succeeds under the configured trust root;
// an untrusted CA and a hostname mismatch fail under verify-full; verify-ca
// ignores the hostname but still checks the chain; bad CA material fails closed.

type testPKI struct {
	caPEMFile  string // trusted CA bundle path
	otherCAPEM string // untrusted CA bundle path
	serverCert tls.Certificate
}

func mustCA(t *testing.T, cn string) (*x509.Certificate, *ecdsa.PrivateKey, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ca key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create ca: %v", err)
	}
	crt, _ := x509.ParseCertificate(der)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return crt, key, pemBytes
}

func writeFile(t *testing.T, dir, name string, b []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

func newTestPKI(t *testing.T) testPKI {
	t.Helper()
	dir := t.TempDir()
	caCert, caKey, caPEM := mustCA(t, "signals-test-ca")
	_, _, otherPEM := mustCA(t, "signals-other-ca")

	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("leaf key: %v", err)
	}
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, caCert, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create leaf: %v", err)
	}
	leafPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})
	keyDER, _ := x509.MarshalECPrivateKey(leafKey)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	serverCert, err := tls.X509KeyPair(leafPEM, keyPEM)
	if err != nil {
		t.Fatalf("server keypair: %v", err)
	}
	return testPKI{
		caPEMFile:  writeFile(t, dir, "ca.pem", caPEM),
		otherCAPEM: writeFile(t, dir, "other.pem", otherPEM),
		serverCert: serverCert,
	}
}

// startTLSServer starts a TLS listener presenting serverCert and returns its
// addr; it accepts one handshake and closes.
func startTLSServer(t *testing.T, cert tls.Certificate) string {
	t.Helper()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatalf("tls.Listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			if tc, ok := c.(*tls.Conn); ok {
				_ = tc.Handshake()
			}
			_ = c.Close()
		}
	}()
	return ln.Addr().String()
}

// dialVerify dials addr with cfg and returns the handshake error (nil = trusted).
func dialVerify(addr string, cfg *tls.Config) error {
	d := &net.Dialer{Timeout: 3 * time.Second}
	conn, err := tls.DialWithDialer(d, "tcp", addr, cfg)
	if err != nil {
		return err
	}
	_ = conn.Close()
	return nil
}

func TestBuildVerified_VerifyFullTrustedCASucceeds(t *testing.T) {
	pki := newTestPKI(t)
	addr := startTLSServer(t, pki.serverCert)
	cfg, err := BuildVerifiedTLSConfig(pki.caPEMFile, "localhost", true)
	if err != nil {
		t.Fatalf("BuildVerifiedTLSConfig: %v", err)
	}
	if err := dialVerify(addr, cfg); err != nil {
		t.Errorf("verify-full with trusted CA + matching host must succeed, got %v", err)
	}
}

func TestBuildVerified_VerifyFullRejectsUntrustedAndHostname(t *testing.T) {
	pki := newTestPKI(t)
	addr := startTLSServer(t, pki.serverCert)

	untrusted, err := BuildVerifiedTLSConfig(pki.otherCAPEM, "localhost", true)
	if err != nil {
		t.Fatalf("build untrusted: %v", err)
	}
	if err := dialVerify(addr, untrusted); err == nil {
		t.Error("verify-full with an untrusted CA must fail")
	}

	wrongHost, err := BuildVerifiedTLSConfig(pki.caPEMFile, "wrong.example", true)
	if err != nil {
		t.Fatalf("build wrong host: %v", err)
	}
	if err := dialVerify(addr, wrongHost); err == nil {
		t.Error("verify-full with a hostname mismatch must fail")
	}
}

func TestBuildVerified_VerifyCAIgnoresHostnameButChecksChain(t *testing.T) {
	pki := newTestPKI(t)
	addr := startTLSServer(t, pki.serverCert)

	// verify-ca: a wrong hostname still succeeds (hostname not checked) as long
	// as the chain is trusted.
	okCfg, err := BuildVerifiedTLSConfig(pki.caPEMFile, "wrong.example", false)
	if err != nil {
		t.Fatalf("build verify-ca: %v", err)
	}
	if err := dialVerify(addr, okCfg); err != nil {
		t.Errorf("verify-ca with trusted CA must succeed despite hostname mismatch, got %v", err)
	}

	// verify-ca: an untrusted CA still fails (chain IS checked).
	badCfg, err := BuildVerifiedTLSConfig(pki.otherCAPEM, "wrong.example", false)
	if err != nil {
		t.Fatalf("build verify-ca untrusted: %v", err)
	}
	if err := dialVerify(addr, badCfg); err == nil {
		t.Error("verify-ca with an untrusted CA must fail (chain is verified)")
	}
}

func TestBuildVerified_FailsClosedOnBadCA(t *testing.T) {
	if _, err := BuildVerifiedTLSConfig("/nonexistent/ca.pem", "localhost", true); err == nil {
		t.Error("missing CA file must fail closed (error), never a permissive config")
	}
	dir := t.TempDir()
	junk := writeFile(t, dir, "junk.pem", []byte("not a certificate"))
	if _, err := BuildVerifiedTLSConfig(junk, "localhost", true); err == nil {
		t.Error("unparseable CA must fail closed (error)")
	}
}
