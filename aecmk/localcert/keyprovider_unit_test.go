//go:build go1.17
// +build go1.17

package localcert

import (
	"context"
	"testing"

	"github.com/microsoft/go-mssqldb/aecmk"
	"github.com/stretchr/testify/assert"
)

func TestValidateEncryptionAlgorithm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		algorithm string
		wantErr   bool
	}{
		{
			name:      "valid RSA_OAEP",
			algorithm: "RSA_OAEP",
			wantErr:   false,
		},
		{
			name:      "valid RSA_OAEP lowercase",
			algorithm: "rsa_oaep",
			wantErr:   false,
		},
		{
			name:      "valid RSA_OAEP mixed case",
			algorithm: "Rsa_Oaep",
			wantErr:   false,
		},
		{
			name:      "invalid algorithm",
			algorithm: "AES256",
			wantErr:   true,
		},
		{
			name:      "empty algorithm",
			algorithm: "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateEncryptionAlgorithm(aecmk.Encryption, tt.algorithm)
			if tt.wantErr {
				assert.Error(t, err, "Expected error but got nil")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateKeyPathLength(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		keyPath string
		wantErr bool
	}{
		{
			name:    "empty path",
			keyPath: "",
			wantErr: false,
		},
		{
			name:    "normal path",
			keyPath: "/path/to/cert.pfx",
			wantErr: false,
		},
		{
			name:    "path at max length",
			keyPath: string(make([]byte, 32767)),
			wantErr: false,
		},
		{
			name:    "path too long",
			keyPath: string(make([]byte, 32768)),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateKeyPathLength(aecmk.Encryption, tt.keyPath)
			if tt.wantErr {
				assert.Error(t, err, "Expected error but got nil")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestInvalidCertificatePathError(t *testing.T) {
	t.Parallel()

	inner := &aecmk.Error{}
	err := invalidCertificatePath("/bad/path", inner)

	icpErr, ok := err.(*InvalidCertificatePathError)
	assert.True(t, ok, "Expected *InvalidCertificatePathError")

	// Test Error()
	errStr := icpErr.Error()
	assert.NotEmpty(t, errStr, "Error() returned empty string")
	assert.Equal(t, "Invalid certificate path: /bad/path", errStr)

	// Test Unwrap()
	unwrapped := icpErr.Unwrap()
	assert.Equal(t, inner, unwrapped)
}

func TestProviderKeyLifetime(t *testing.T) {
	t.Parallel()

	p := &Provider{name: PfxKeyProviderName}
	result := p.KeyLifetime()
	assert.Nil(t, result)
}

func TestProviderSignColumnMasterKeyMetadata(t *testing.T) {
	t.Parallel()

	p := &Provider{name: PfxKeyProviderName}
	sig, err := p.SignColumnMasterKeyMetadata(context.Background(), "/path/to/key", false)
	assert.NoError(t, err)
	assert.Nil(t, sig)
}

func TestProviderVerifyColumnMasterKeyMetadata(t *testing.T) {
	t.Parallel()

	p := &Provider{name: PfxKeyProviderName}
	result, err := p.VerifyColumnMasterKeyMetadata(context.Background(), "/path/to/key", false)
	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestSetCertificatePassword(t *testing.T) {
	t.Parallel()

	p := Provider{
		name:      PfxKeyProviderName,
		passwords: make(map[string]string),
	}

	// Set password for specific location
	p.SetCertificatePassword("/path/to/cert.pfx", "mypassword")
	assert.Equal(t, "mypassword", p.passwords["/path/to/cert.pfx"], "Password not set correctly for specific path")

	// Set password for wildcard (empty location)
	p.SetCertificatePassword("", "defaultpassword")
	assert.Equal(t, "defaultpassword", p.passwords[wildcard], "Password not set correctly for wildcard")
}

func TestDecryptColumnEncryptionKeyErrors(t *testing.T) {
	t.Parallel()

	p := &Provider{
		name:             PfxKeyProviderName,
		passwords:        make(map[string]string),
		AllowedLocations: []string{},
	}

	ctx := context.Background()

	// Test invalid algorithm
	_, err := p.DecryptColumnEncryptionKey(ctx, "/path/to/cert.pfx", "INVALID_ALGO", []byte{1, 2, 3})
	assert.Error(t, err, "Expected error for invalid algorithm")

	// Test key path too long
	longPath := string(make([]byte, 32768))
	_, err = p.DecryptColumnEncryptionKey(ctx, longPath, "RSA_OAEP", []byte{1, 2, 3})
	assert.Error(t, err, "Expected error for too long key path")

	// Test non-existent file
	_, err = p.DecryptColumnEncryptionKey(ctx, "/non/existent/path.pfx", "RSA_OAEP", []byte{1, 2, 3})
	assert.Error(t, err, "Expected error for non-existent file")
}

func TestEncryptColumnEncryptionKeyErrors(t *testing.T) {
	t.Parallel()

	p := &Provider{
		name:             PfxKeyProviderName,
		passwords:        make(map[string]string),
		AllowedLocations: []string{},
	}

	ctx := context.Background()

	// Test invalid algorithm
	_, err := p.EncryptColumnEncryptionKey(ctx, "/path/to/cert.pfx", "INVALID_ALGO", []byte{1, 2, 3})
	assert.Error(t, err, "Expected error for invalid algorithm")

	// Test key path too long
	longPath := string(make([]byte, 32768))
	_, err = p.EncryptColumnEncryptionKey(ctx, longPath, "RSA_OAEP", []byte{1, 2, 3})
	assert.Error(t, err, "Expected error for too long key path")
}

func TestTryLoadCertificateNotAllowed(t *testing.T) {
	t.Parallel()

	p := &Provider{
		name:             PfxKeyProviderName,
		passwords:        make(map[string]string),
		AllowedLocations: []string{"/allowed/path"},
	}

	// Test with path not in allowed list
	_, _, err := p.tryLoadCertificate(aecmk.Decryption, "/not/allowed/path.pfx")
	assert.Error(t, err, "Expected error for non-allowed path")

	// Test with allowed path (will fail because file doesn't exist, but path check should pass)
	_, _, err = p.tryLoadCertificate(aecmk.Decryption, "/allowed/path/cert.pfx")
	// This will error because the file doesn't exist, but the path check passed
	assert.Error(t, err, "Expected error for non-existent file")
}
