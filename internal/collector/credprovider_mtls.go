package collector

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"strings"

	"github.com/elevarq/arq-signals/internal/config"
)

// certLoader loads and validates a client certificate + private key for the
// mtls provider (ARQ-SIGNALS-AUTH-MTLS-, #98). It is a seam so unit tests
// inject in-memory fixtures and read no operator key material (NFR003).
type certLoader interface {
	Load(certFile, keyFile, passphraseFile string) (*tls.Certificate, error)
}

// fileCertLoader is the production loader: it reads PEM cert/key from the
// filesystem and, when a passphrase file is given, decrypts a legacy
// PEM-encrypted key (stdlib only, NFR001).
type fileCertLoader struct{}

func (fileCertLoader) Load(certFile, keyFile, passphraseFile string) (*tls.Certificate, error) {
	certPEM, err := os.ReadFile(certFile)
	if err != nil {
		// Redacted: names the field + failure class, never file contents.
		return nil, fmt.Errorf("read sslcert: %w", err)
	}
	keyPEM, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, fmt.Errorf("read sslkey: %w", err)
	}
	if passphraseFile != "" {
		keyPEM, err = decryptKeyPEM(keyPEM, passphraseFile)
		if err != nil {
			return nil, err
		}
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		// Covers a non-PEM file, a mismatched cert/key pair, or an encrypted
		// key with no passphrase. The error never includes key bytes.
		return nil, fmt.Errorf("load client cert/key (invalid pair, bad PEM, or encrypted key without sslkey_passphrase_file): %w", err)
	}
	// Parse the leaf for the NotAfter metadata (advisory ExpiresAt, #98).
	if cert.Leaf == nil && len(cert.Certificate) > 0 {
		if leaf, perr := x509.ParseCertificate(cert.Certificate[0]); perr == nil {
			cert.Leaf = leaf
		}
	}
	return &cert, nil
}

// decryptKeyPEM decrypts a legacy PEM-encrypted private key with the
// passphrase in passphraseFile and returns an unencrypted PEM that
// tls.X509KeyPair accepts. An unencrypted key is returned unchanged.
func decryptKeyPEM(keyPEM []byte, passphraseFile string) ([]byte, error) {
	pass, err := os.ReadFile(passphraseFile)
	if err != nil {
		return nil, fmt.Errorf("read sslkey_passphrase_file: %w", err)
	}
	pass = []byte(strings.TrimRight(string(pass), "\r\n"))
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return nil, fmt.Errorf("sslkey is not valid PEM")
	}
	//nolint:staticcheck // x509.IsEncryptedPEMBlock/DecryptPEMBlock are the only
	// stdlib path for legacy PEM-encrypted keys (NFR001: stdlib-only, no dep).
	if !x509.IsEncryptedPEMBlock(block) {
		return keyPEM, nil // not encrypted; passphrase not needed
	}
	//nolint:staticcheck // see note above
	der, err := x509.DecryptPEMBlock(block, pass)
	if err != nil {
		// Never echo the passphrase or key material.
		return nil, fmt.Errorf("decrypt sslkey: wrong passphrase or unsupported key encryption")
	}
	return pem.EncodeToMemory(&pem.Block{Type: block.Type, Bytes: der}), nil
}

// resolveMTLS loads the client cert/key for an mtls target (#98). The
// certificate-kind credential is re-read on every connection so a rotated
// cert/key is picked up without a daemon restart (INV-MTLS-003); there is no
// token to mint and nothing to cache. The private key material is carried in
// the Credential and applied to the TLS config by the caller — never logged
// (INV-MTLS-001).
func (r *credentialResolver) resolveMTLS(_ context.Context, tgt config.TargetConfig) (Credential, error) {
	cert, err := r.certLoader.Load(tgt.SSLCert, tgt.SSLKey, tgt.SSLKeyPassphraseFile)
	if err != nil {
		return Credential{}, redactError(err)
	}
	cred := Credential{Kind: CredKindCertificate, ClientCert: cert}
	if cert.Leaf != nil {
		cred.ExpiresAt = cert.Leaf.NotAfter // advisory only
	}
	return cred, nil
}
