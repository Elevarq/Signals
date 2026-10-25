// Package dbtls builds verifying *tls.Config values for Signals' database
// connectors from a PEM CA bundle, with the two PostgreSQL-style verification
// modes Signals supports:
//
//   - verify-full (verifyHostname == true): the certificate chain AND the
//     hostname are checked (standard Go verification with ServerName set).
//   - verify-ca  (verifyHostname == false): the chain is verified against the
//     configured CA but the hostname is NOT checked. Go has no built-in
//     "chain without hostname" mode, so this disables default verification and
//     performs the chain check in VerifyPeerCertificate instead — stricter than
//     the default for the hostname dimension only, never unverified.
//
// It is engine-neutral — the ClickHouse native backend (#558) uses it without
// depending on the MySQL-wire package, keeping engine identity distinct from
// wire protocol. The logic mirrors the proven MySQL-wire verify path
// (mysqlwire.BuildTLSConfig, SCH-R001/R002/R003); that package retains its own
// equivalent copy for now and can converge on this neutral home in a later
// cleanup. It fails closed (returns an error, never a permissive config) when
// the CA material is missing or unparseable.
package dbtls

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
)

// BuildVerifiedTLSConfig loads caFile into a RootCAs pool and returns a verifying
// *tls.Config. verifyHostname selects verify-full (true) vs verify-ca (false),
// per the package doc. It fails closed when caFile is unreadable or carries no
// parseable certificate.
func BuildVerifiedTLSConfig(caFile, serverName string, verifyHostname bool) (*tls.Config, error) {
	pemBytes, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read CA file %q: %w", caFile, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemBytes) {
		return nil, fmt.Errorf("CA file %q contains no parseable certificate", caFile)
	}
	cfg := &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12} // nosemgrep
	if verifyHostname {
		cfg.ServerName = serverName
		return cfg, nil
	}
	// verify-ca: verify the chain against the configured CA, skip ONLY the
	// hostname check. InsecureSkipVerify disables the default verification so
	// our VerifyPeerCertificate (below) can run the chain check without a
	// DNS-name constraint; the connection is NOT unverified. The chain
	// verification immediately below is what makes verify-ca safe; the inline
	// suppression covers the generic "TLS verification disabled" rules.
	cfg.InsecureSkipVerify = true // nosemgrep
	cfg.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		certs := make([]*x509.Certificate, 0, len(rawCerts))
		for _, raw := range rawCerts {
			crt, perr := x509.ParseCertificate(raw)
			if perr != nil {
				return perr
			}
			certs = append(certs, crt)
		}
		if len(certs) == 0 {
			return errors.New("no peer certificate presented")
		}
		inter := x509.NewCertPool()
		for _, crt := range certs[1:] {
			inter.AddCert(crt)
		}
		_, verr := certs[0].Verify(x509.VerifyOptions{Roots: pool, Intermediates: inter})
		return verr
	}
	return cfg, nil
}
