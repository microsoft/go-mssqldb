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
	
	// Create a certificate pool with the provided certificate as the root CA
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(pem)
	
	// We must use InsecureSkipVerify=true for this specific edge case because
	// normal verification will fail for certificates with ":" in the CN.
	// The VerifyPeerCertificate callback performs proper certificate chain verification.
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
		
		// Verify the certificate chain against the provided root CA
		opts := x509.VerifyOptions{
			Roots:         roots,
			Intermediates: x509.NewCertPool(),
		}
		_, err = cert.Verify(opts)
		return err
	}
	return nil
}

// setupTLSCertificateOnly validates the certificate chain without checking the hostname
func setupTLSCertificateOnly(config *tls.Config, pem []byte) error {
	// Skip hostname validation by setting ServerName to empty string
	// The certificate chain will still be verified against RootCAs (set later in SetupTLS)
	// This is the secure way to skip hostname validation without using InsecureSkipVerify
	config.ServerName = ""
	return nil
}
