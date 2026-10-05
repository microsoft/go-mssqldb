//go:build !go1.12
// +build !go1.12

package msdsn

import "crypto/tls"

func TLSVersionFromString(minTLSVersion string) uint16 {
	switch minTLSVersion {
	case "1.0":
		return tls.VersionTLS10
	case "1.1":
		return tls.VersionTLS11
	case "1.2":
		return tls.VersionTLS12
	default:
		// The tls package default, unless it is the protocol number URL()
		// writes for a version above the highest one named here; see
		// tlsVersionFromNumber for why nothing lower is read that way.
		return tlsVersionFromNumber(minTLSVersion)
	}
}

// highestNamedTLSVersion is the highest version tlsmin can name on this build,
// and the bound above which tlsVersionFromNumber reads a number instead.
const highestNamedTLSVersion = tls.VersionTLS12

// tlsVersionToString is the inverse of TLSVersionFromString. It returns "" for
// the zero value, which stands for "use the tls package default" and therefore
// has no tlsmin spelling.
func tlsVersionToString(minTLSVersion uint16) string {
	switch minTLSVersion {
	case tls.VersionTLS10:
		return "1.0"
	case tls.VersionTLS11:
		return "1.1"
	case tls.VersionTLS12:
		return "1.2"
	}
	return ""
}
