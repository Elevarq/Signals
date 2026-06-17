package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeSelfSignedCert generates a throwaway P-256 self-signed cert for
// 127.0.0.1 and writes cert.pem / key.pem into dir, returning their
// paths. Test-only.
func writeSelfSignedCert(t *testing.T, dir string) (certPath, keyPath string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "signals-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return certPath, keyPath
}

// TC-SIG-129 — R113: with both TLS files configured the API negotiates
// TLS (min 1.2) and serves /health over HTTPS. The same Server fields
// Start() reads are exercised here.
func TestServerServesTLSWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := writeSelfSignedCert(t, dir)

	deps := &Deps{TLSCertFile: certPath, TLSKeyFile: keyPath}
	srv := NewServer("127.0.0.1:0", 5*time.Second, 5*time.Second, "test-token", deps)

	// NewServer must pin a minimum protocol version of TLS 1.2.
	if srv.httpServer.TLSConfig == nil {
		t.Fatal("TLSConfig must be set when TLS files are configured")
	}
	if srv.httpServer.TLSConfig.MinVersion != tls.VersionTLS12 {
		t.Fatalf("MinVersion = %#x, want TLS 1.2 (%#x)", srv.httpServer.TLSConfig.MinVersion, tls.VersionTLS12)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	// Serve via the same fields Start() uses.
	go func() { _ = srv.httpServer.ServeTLS(ln, srv.tlsCertFile, srv.tlsKeyFile) }()
	t.Cleanup(func() { _ = srv.httpServer.Close() })

	client := &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			// Self-signed cert: skip verification, but require TLS 1.2+.
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12},
		},
	}

	url := "https://" + ln.Addr().String() + "/health"
	var resp *http.Response
	// The goroutine may not have completed the handshake setup on the
	// very first dial; retry briefly.
	deadline := time.Now().Add(3 * time.Second)
	for {
		resp, err = client.Get(url)
		if err == nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("HTTPS GET /health failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.TLS == nil {
		t.Fatal("response was not served over TLS")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET /health: status %d, body %q", resp.StatusCode, body)
	}
}

// TC-SIG-129 — R113: with no TLS files the server stays plain HTTP
// (TLSConfig nil), preserving the loopback default.
func TestServerPlainHTTPWhenNoTLS(t *testing.T) {
	srv := NewServer("127.0.0.1:0", 5*time.Second, 5*time.Second, "test-token", &Deps{})
	if srv.httpServer.TLSConfig != nil {
		t.Fatal("TLSConfig must be nil when no TLS files are configured")
	}
	if srv.tlsCertFile != "" || srv.tlsKeyFile != "" {
		t.Fatal("TLS file paths must be empty for the plain-HTTP path")
	}
}
