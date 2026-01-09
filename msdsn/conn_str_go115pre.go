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
	// Prior to Go 1.15, we don't have VerifyPeerCertificate callback.
	// We must use InsecureSkipVerify=true to skip hostname validation.
	// The certificate will still be verified against RootCAs (set in SetupTLS after this function).
	config.InsecureSkipVerify = true
	return nil
}
