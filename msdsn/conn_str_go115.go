//go:build go1.15
// +build go1.15

package msdsn

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
)

func setupTLSCommonName(config *tls.Config, pem []byte) error {
	// fix for https://github.com/denisenkom/go-mssqldb/issues/704
	// A SSL/TLS certificate Common Name (CN) containing the ":" character
	// (which is a non-standard character) will cause normal verification to fail.
	// We use VerifyPeerCertificate to perform custom verification.
	// This is required because standard TLS verification in Go doesn't handle ":" in CN.
	//
	// Security note: InsecureSkipVerify is safe here because:
	// 1. The VerifyPeerCertificate callback performs full certificate chain validation
	// 2. The certificate must be signed by the user-provided CA (in pem)
	// 3. The CN is explicitly validated against the expected ServerName
	
	// Create a certificate pool with the provided certificate as the root CA
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(pem)
	
	// We must use InsecureSkipVerify=true for this specific edge case because
	// normal verification will fail for certificates with ":" in the CN.
	// The VerifyPeerCertificate callback performs proper certificate chain verification.
	// nosemgrep: go.lang.security.audit.net.use-tls.use-tls
	config.InsecureSkipVerify = true
	config.VerifyPeerCertificate = func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
		if len(rawCerts) == 0 {
			return fmt.Errorf("no peer certificates provided")
		}
		
		// Parse the peer certificate
		cert, err := x509.ParseCertificate(rawCerts[0])
		if err != nil {
			return fmt.Errorf("failed to parse certificate: %w", err)
		}
		
		// Check the common name matches the expected server name
		commonName := cert.Subject.CommonName
		if commonName != config.ServerName {
			return fmt.Errorf("invalid certificate name %q, expected %q", commonName, config.ServerName)
		}
		
		// Build intermediates pool from the peer certificates (excluding the first one which is the server cert)
		intermediates := x509.NewCertPool()
		if len(rawCerts) > 1 {
			for i := 1; i < len(rawCerts); i++ {
				intermediateCert, err := x509.ParseCertificate(rawCerts[i])
				if err != nil {
					return fmt.Errorf("failed to parse intermediate certificate: %w", err)
				}
				intermediates.AddCert(intermediateCert)
			}
		}
		
		// Verify the certificate chain against the provided root CA
		opts := x509.VerifyOptions{
			Roots:         roots,
			Intermediates: intermediates,
		}
		_, err = cert.Verify(opts)
		return err
	}
	return nil
}
