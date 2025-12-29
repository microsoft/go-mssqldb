//go:build !go1.25
// +build !go1.25

package msdsn

import (
	"crypto/tls"
)

// configureTLSSignatureSchemes is a no-op for Go versions before 1.25.
// Pre-1.25 Go versions allow SHA-1 signature algorithms by default,
// so no explicit configuration is needed.
func configureTLSSignatureSchemes(config *tls.Config) {
	// No configuration needed for Go < 1.25
}
