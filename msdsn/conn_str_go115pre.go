//go:build !go1.15
// +build !go1.15

package msdsn

import "crypto/tls"

func setupTLSCommonName(config *tls.Config, pem []byte) error {
	// Prior to Go 1.15, the TLS allowed ":" when checking the hostname.
	// See https://golang.org/issue/40748 for details.
	return skipSetup
}

// setupTLSCertificateOnly validates the certificate chain without checking the hostname
func setupTLSCertificateOnly(config *tls.Config, pem []byte) error {
	// Prior to Go 1.15, we can't use VerifyConnection callback
	// So we rely on InsecureSkipVerify being set in SetupTLS
	return nil
}
