//go:build go1.25
// +build go1.25

package msdsn

import (
	"crypto/tls"
	"testing"
)

// TestTLSSignatureSchemesGo125 verifies that TLS signature schemes are properly
// configured in Go 1.25+ to avoid SHA-1 related handshake failures.
func TestTLSSignatureSchemesGo125(t *testing.T) {
	// Create a TLS config using SetupTLS
	config, err := SetupTLS("", false, "testhost", "")
	if err != nil {
		t.Fatalf("SetupTLS failed: %v", err)
	}

	// Verify that signature schemes are configured
	if len(config.SignatureSchemes) == 0 {
		t.Fatal("Expected SignatureSchemes to be configured, but it's empty")
	}

	// Verify that SHA-1 based signature schemes are not included
	for _, scheme := range config.SignatureSchemes {
		if scheme == tls.PKCS1WithSHA1 || scheme == tls.ECDSAWithSHA1 {
			t.Errorf("SHA-1 signature scheme %v should not be included in Go 1.25+", scheme)
		}
	}

	// Verify that modern signature schemes are included
	expectedSchemes := []tls.SignatureScheme{
		tls.ECDSAWithP256AndSHA256,
		tls.PKCS1WithSHA256,
		tls.PSSWithSHA256,
	}

	for _, expected := range expectedSchemes {
		found := false
		for _, scheme := range config.SignatureSchemes {
			if scheme == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected signature scheme %v to be included", expected)
		}
	}

	t.Logf("TLS SignatureSchemes configured: %v", config.SignatureSchemes)
}

// TestConfigureTLSSignatureSchemes tests the configureTLSSignatureSchemes function
func TestConfigureTLSSignatureSchemes(t *testing.T) {
	config := &tls.Config{}

	// Initially, SignatureSchemes should be empty
	if len(config.SignatureSchemes) != 0 {
		t.Fatalf("Expected initial SignatureSchemes to be empty, got %d", len(config.SignatureSchemes))
	}

	// Call configureTLSSignatureSchemes
	configureTLSSignatureSchemes(config)

	// Verify that signature schemes are now configured
	if len(config.SignatureSchemes) == 0 {
		t.Fatal("Expected SignatureSchemes to be configured after calling configureTLSSignatureSchemes")
	}

	// Verify no SHA-1 schemes
	for _, scheme := range config.SignatureSchemes {
		if scheme == tls.PKCS1WithSHA1 || scheme == tls.ECDSAWithSHA1 {
			t.Errorf("SHA-1 signature scheme %v should not be configured", scheme)
		}
	}

	t.Logf("Configured %d signature schemes", len(config.SignatureSchemes))
}
