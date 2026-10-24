package mysqlwire

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

// Tests derive from specifications/singlestore-connection-hardening.acceptance.md.
// Hermetic TLS: a temporary CA + a local TLS listener — never public CA infra.

type testPKI struct {
	caPEMFile  string          // path to the trusted CA bundle
	otherCAPEM string          // path to an UNtrusted CA bundle
	serverCert tls.Certificate // leaf for "localhost"/127.0.0.1 signed by the trusted CA
}

func newCA(t *testing.T, cn string) (*x509.Certificate, *ecdsa.PrivateKey, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("gen CA key: %v", err)
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
		t.Fatalf("create CA cert: %v", err)
	}
	caCert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse CA cert: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return caCert, key, pemBytes
}

func newTestPKI(t *testing.T) testPKI {
	t.Helper()
	dir := t.TempDir()

	caCert, caKey, caPEM := newCA(t, "Signals Test CA")
	caFile := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(caFile, caPEM, 0o600); err != nil {
		t.Fatalf("write ca: %v", err)
	}

	_, _, otherPEM := newCA(t, "Other Untrusted CA")
	otherFile := filepath.Join(dir, "other-ca.pem")
	if err := os.WriteFile(otherFile, otherPEM, 0o600); err != nil {
		t.Fatalf("write other ca: %v", err)
	}

	// Leaf signed by the trusted CA, valid for localhost + 127.0.0.1.
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("gen leaf key: %v", err)
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
		t.Fatalf("create leaf cert: %v", err)
	}
	return testPKI{
		caPEMFile:  caFile,
		otherCAPEM: otherFile,
		serverCert: tls.Certificate{Certificate: [][]byte{leafDER}, PrivateKey: leafKey},
	}
}

// startTLSServer starts a local TLS listener presenting serverCert and returns
// its address; it closes on test cleanup.
func startTLSServer(t *testing.T, serverCert tls.Certificate) string {
	t.Helper()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		MinVersion:   tls.VersionTLS12,
	})
	if err != nil {
		t.Fatalf("tls.Listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				// Force the handshake, then close.
				if tc, ok := c.(*tls.Conn); ok {
					_ = tc.Handshake()
				}
				_ = c.Close()
			}(conn)
		}
	}()
	return ln.Addr().String()
}

// dialWith attempts a TLS handshake to addr using the given client config.
func dialWith(addr string, cfg *tls.Config) error {
	d := &net.Dialer{Timeout: 3 * time.Second}
	conn, err := tls.DialWithDialer(d, "tcp", addr, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	return conn.Handshake()
}

// TC-SCH-01 (SCH-R001): verify-full trusts the configured CA.
func TestBuildTLSConfig_VerifyFullTrustedCASucceeds(t *testing.T) {
	pki := newTestPKI(t)
	addr := startTLSServer(t, pki.serverCert)

	cfg, err := BuildTLSConfig(pki.caPEMFile, "localhost", true)
	if err != nil {
		t.Fatalf("BuildTLSConfig: %v", err)
	}
	if err := dialWith(addr, cfg); err != nil {
		t.Fatalf("verify-full with trusted CA + matching host should succeed: %v", err)
	}
}

// TC-SCH-02 (SCH-R001): verify-full rejects an untrusted CA and a hostname mismatch.
func TestBuildTLSConfig_VerifyFullRejectsUntrustedAndHostname(t *testing.T) {
	pki := newTestPKI(t)
	addr := startTLSServer(t, pki.serverCert)

	untrusted, err := BuildTLSConfig(pki.otherCAPEM, "localhost", true)
	if err != nil {
		t.Fatalf("BuildTLSConfig(untrusted): %v", err)
	}
	if err := dialWith(addr, untrusted); err == nil {
		t.Error("verify-full with an untrusted CA must fail")
	}

	wrongHost, err := BuildTLSConfig(pki.caPEMFile, "wrong.example", true)
	if err != nil {
		t.Fatalf("BuildTLSConfig(wrong host): %v", err)
	}
	if err := dialWith(addr, wrongHost); err == nil {
		t.Error("verify-full with a hostname mismatch must fail")
	}
}

// TC-SCH-03 (SCH-R002): verify-ca verifies the chain but ignores hostname.
func TestBuildTLSConfig_VerifyCAIgnoresHostnameButChecksChain(t *testing.T) {
	pki := newTestPKI(t)
	addr := startTLSServer(t, pki.serverCert)

	// Trusted CA, deliberately wrong hostname → must SUCCEED (hostname ignored).
	okCfg, err := BuildTLSConfig(pki.caPEMFile, "wrong.example", false)
	if err != nil {
		t.Fatalf("BuildTLSConfig: %v", err)
	}
	if err := dialWith(addr, okCfg); err != nil {
		t.Errorf("verify-ca with a trusted CA must succeed despite a hostname mismatch: %v", err)
	}

	// Untrusted CA → must FAIL (chain not trusted).
	badCfg, err := BuildTLSConfig(pki.otherCAPEM, "wrong.example", false)
	if err != nil {
		t.Fatalf("BuildTLSConfig(untrusted): %v", err)
	}
	if err := dialWith(addr, badCfg); err == nil {
		t.Error("verify-ca with an untrusted CA must fail")
	}
}

// TC-SCH-04 (SCH-R003): bad CA material fails closed.
func TestBuildTLSConfig_BadCAFailsClosed(t *testing.T) {
	if _, err := BuildTLSConfig(filepath.Join(t.TempDir(), "nope.pem"), "localhost", true); err == nil {
		t.Error("a non-existent CA file must error")
	}
	empty := filepath.Join(t.TempDir(), "empty.pem")
	if err := os.WriteFile(empty, []byte("not a certificate"), 0o600); err != nil {
		t.Fatalf("write empty: %v", err)
	}
	if _, err := BuildTLSConfig(empty, "localhost", true); err == nil {
		t.Error("a CA file with no parseable certificate must error")
	}
}

// TC-SCH-04 (SCH-R003): ConnConfig.resolveTLS propagates the CA error (fail closed).
func TestResolveTLS_FailsClosedOnBadCA(t *testing.T) {
	c := ConnConfig{Host: "h", TLS: "true", TLSRootCAFile: filepath.Join(t.TempDir(), "nope.pem"), TLSVerifyHostname: true}
	if _, err := c.resolveTLS(); err == nil {
		t.Error("resolveTLS must fail closed on an unreadable CA")
	}
	// No CA file → pass through the base key unchanged.
	c2 := ConnConfig{TLS: "preferred"}
	key, err := c2.resolveTLS()
	if err != nil || key != "preferred" {
		t.Errorf("resolveTLS without a CA must return the base key unchanged: key=%q err=%v", key, err)
	}
}
