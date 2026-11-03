package collector

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"strings"

	"github.com/elevarq/signals/internal/config"
)

// certLoader loads and validates a client certificate + private key for the
// mtls provider (SIGNALS-AUTH-MTLS-, #98). It is a seam so unit tests
// inject in-memory fixtures and read no operator key material (NFR003).
type certLoader interface {
	Load(certFile, keyFile string) (*tls.Certificate, error)
}

// fileCertLoader is the production loader: it reads an unencrypted PEM
// cert/key from the filesystem (stdlib only, NFR001). Encrypted private keys
// are not supported (#654) — see isEncryptedKeyPEM.
type fileCertLoader struct{}

func (fileCertLoader) Load(certFile, keyFile string) (*tls.Certificate, error) {
	certPEM, err := os.ReadFile(certFile)
	if err != nil {
		// Redacted: names the field + failure class, never file contents.
		return nil, fmt.Errorf("read sslcert: %w", err)
	}
	keyPEM, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, fmt.Errorf("read sslkey: %w", err)
	}
	// Encrypted private keys are not supported (#654, FC-MTLS-003): legacy
	// RFC-1423 PEM encryption is insecure by design and deprecated, and there
	// is no stdlib-only authenticated replacement (NFR001). Reject with a
	// clear, redaction-safe message — detected from the PEM headers, never by
	// attempting a decrypt. Supply an unencrypted key protected by the secret
	// store or filesystem permissions.
	if isEncryptedKeyPEM(keyPEM) {
		return nil, fmt.Errorf("sslkey is an encrypted private key, which is not supported; provide an unencrypted key protected by the secret store or filesystem permissions")
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		// Covers a non-PEM file or a mismatched cert/key pair. The error never
		// includes key bytes.
		return nil, fmt.Errorf("load client cert/key (invalid pair or bad PEM): %w", err)
	}
	// Parse the leaf for the NotAfter metadata (advisory ExpiresAt, #98).
	if cert.Leaf == nil && len(cert.Certificate) > 0 {
		if leaf, perr := x509.ParseCertificate(cert.Certificate[0]); perr == nil {
			cert.Leaf = leaf
		}
	}
	return &cert, nil
}

// isEncryptedKeyPEM reports whether keyPEM is an encrypted private key,
// detected purely from the PEM structure (no decrypt attempt, no deprecated
// x509 encrypted-PEM API): legacy RFC-1423 blocks carry a `Proc-Type: ...
// ENCRYPTED` / `DEK-Info` header, and PKCS#8 encrypted keys use the
// `ENCRYPTED PRIVATE KEY` block type. Both are unsupported (#654).
func isEncryptedKeyPEM(keyPEM []byte) bool {
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return false // not PEM at all → let tls.X509KeyPair produce the error
	}
	if block.Type == "ENCRYPTED PRIVATE KEY" {
		return true
	}
	if strings.Contains(block.Headers["Proc-Type"], "ENCRYPTED") {
		return true
	}
	_, hasDEK := block.Headers["DEK-Info"]
	return hasDEK
}

// resolveMTLS loads the client cert/key for an mtls target (#98). The
// certificate-kind credential is re-read on every connection so a rotated
// cert/key is picked up without a daemon restart (INV-MTLS-003); there is no
// token to mint and nothing to cache. The private key material is carried in
// the Credential and applied to the TLS config by the caller — never logged
// (INV-MTLS-001).
func (r *credentialResolver) resolveMTLS(_ context.Context, tgt config.TargetConfig) (Credential, error) {
	cert, err := r.certLoader.Load(tgt.SSLCert, tgt.SSLKey)
	if err != nil {
		return Credential{}, redactError(err)
	}
	cred := Credential{Kind: CredKindCertificate, ClientCert: cert}
	if cert.Leaf != nil {
		cred.ExpiresAt = cert.Leaf.NotAfter // advisory only
	}
	return cred, nil
}

// MTLSGuidance returns the operator remediation text for an mtls target
// whose client certificate was rejected because the server does not yet
// trust it — the certificate is loaded locally but the cluster has no
// matching pg_hba clientcert mapping or CA trust (AC-MTLS-010). It names
// the exact pg_hba.conf line and the CA-trust step, and contains no key
// material or passphrase (INV-MTLS-001).
func MTLSGuidance(tgt config.TargetConfig) string {
	return fmt.Sprintf(`mtls connection for target %q was rejected — the server does not trust the presented client certificate.
Verify both halves of certificate authentication:
  1. Add a hostssl line to pg_hba.conf that requires and maps the client cert,
     then reload the server (SELECT pg_reload_conf();):
       hostssl %s %s 0.0.0.0/0 cert clientcert=verify-full
     The certificate's Common Name (CN) must equal the database role %q
     (or add a pg_ident.conf map if they differ).
  2. Ensure the cluster trusts the certificate's issuing CA — append the CA
     certificate to the server's ssl_ca_file and reload.`,
		tgt.Name, tgt.DBName, tgt.User, tgt.User)
}
