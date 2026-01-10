//go:build !go1.15
// +build !go1.15

package msdsn

import (
	"bytes"
	"crypto/tls"
	"encoding/pem"
	"fmt"
)

func setupTLSCommonName(config *tls.Config, pemData []byte) error {
	// Prior to Go 1.15, the TLS allowed ":" when checking the hostname.
	// See https://golang.org/issue/40748 for details.
	return skipSetup
}

// setupTLSCertificateOnly validates that the server certificate matches the provided certificate
func setupTLSCertificateOnly(config *tls.Config, pemData []byte) error {
	// To match the behavior of Microsoft.Data.SqlClient, we simply compare the raw bytes
	// of the server's certificate with the provided certificate file.
	
	// Parse the expected certificate from the PEM data
	block, _ := pem.Decode(pemData)
	if block == nil {
		return fmt.Errorf("failed to decode PEM certificate")
	}
	// Store the raw certificate bytes (DER format) for comparison
	expectedCertBytes := block.Bytes
	
	config.InsecureSkipVerify = true
	config.VerifyPeerCertificate = func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
		if len(rawCerts) == 0 {
			return fmt.Errorf("no peer certificates provided")
		}
		
		// Compare the server's certificate bytes with the expected certificate bytes
		serverCertBytes := rawCerts[0]
		
		if !bytes.Equal(serverCertBytes, expectedCertBytes) {
			return fmt.Errorf("server certificate doesn't match the provided certificate")
		}
		
		return nil
	}
	return nil
}
