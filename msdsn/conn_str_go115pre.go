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
	// Skip hostname validation by setting ServerName to empty string.
	// When ServerName is empty, Go's TLS implementation will skip hostname verification
	// but still verify the certificate chain against the RootCAs (configured in SetupTLS after this function returns).
	config.ServerName = ""
	return nil
}
