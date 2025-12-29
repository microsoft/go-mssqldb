//go:build go1.25
// +build go1.25

package msdsn

import (
	"crypto/tls"
)

// configureTLSSignatureSchemes sets signature schemes for Go 1.25+
// to comply with RFC 9155 which disallows SHA-1 signature algorithms.
// This explicitly configures modern signature schemes to avoid TLS handshake
// failures with servers that might default to SHA-1.
func configureTLSSignatureSchemes(config *tls.Config) {
	// Set modern signature schemes explicitly, excluding SHA-1 based algorithms
	// These are safe and widely supported signature schemes for TLS 1.2 and 1.3
	config.SignatureSchemes = []tls.SignatureScheme{
		// ECDSA algorithms (preferred for modern systems)
		tls.ECDSAWithP256AndSHA256,
		tls.ECDSAWithP384AndSHA384,
		tls.ECDSAWithP521AndSHA512,

		// RSASSA-PSS algorithms (preferred for RSA)
		tls.PSSWithSHA256,
		tls.PSSWithSHA384,
		tls.PSSWithSHA512,

		// RSASSA-PKCS1-v1_5 algorithms (widely compatible)
		tls.PKCS1WithSHA256,
		tls.PKCS1WithSHA384,
		tls.PKCS1WithSHA512,

		// EdDSA algorithms
		tls.Ed25519,
	}
}
