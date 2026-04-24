//go:build go1.18
// +build go1.18

package akv

import (
	"context"
	"crypto/rand"
	"errors"
	"net/url"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/microsoft/go-mssqldb/aecmk"
	"github.com/microsoft/go-mssqldb/internal/akvkeys"
	"github.com/stretchr/testify/assert"
)

func TestAllowedPathAndEndpointWithPort(t *testing.T) {
	p := &Provider{
		credentials:      make(map[string]azcore.TokenCredential),
		AllowedLocations: []string{"myvault.vault.azure.net"},
	}

	tests := []struct {
		name             string
		masterKeyPath    string
		expectAllowed    bool
		expectEndpoint   string
		expectKeypath    []string
	}{
		{
			name:           "URL without port",
			masterKeyPath:  "https://myvault.vault.azure.net/keys/mykey/abc123",
			expectAllowed:  true,
			expectEndpoint: "https://myvault.vault.azure.net",
			expectKeypath:  []string{"mykey", "abc123"},
		},
		{
			name:           "URL with port 443",
			masterKeyPath:  "https://myvault.vault.azure.net:443/keys/mykey/abc123",
			expectAllowed:  true,
			expectEndpoint: "https://myvault.vault.azure.net",
			expectKeypath:  []string{"mykey", "abc123"},
		},
		{
			name:           "URL with non-standard port",
			masterKeyPath:  "https://myvault.vault.azure.net:8443/keys/mykey/abc123",
			expectAllowed:  true,
			expectEndpoint: "https://myvault.vault.azure.net:8443",
			expectKeypath:  []string{"mykey", "abc123"},
		},
		{
			name:          "URL with port not in allowed list",
			masterKeyPath: "https://other.vault.azure.net:443/keys/mykey/abc123",
			expectAllowed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endpoint, keypath, allowed := p.allowedPathAndEndpoint(tt.masterKeyPath)
			assert.Equal(t, tt.expectAllowed, allowed, "allowed")
			if tt.expectAllowed {
				assert.Equal(t, tt.expectEndpoint, endpoint, "endpoint")
				assert.Equal(t, tt.expectKeypath, keypath, "keypath")
			}
		})
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	client, vaultURL, err := akvkeys.GetTestAKV()
	if err != nil {
		t.Skip("No access to AKV")
	}
	cred, err := akvkeys.GetProviderCredential()
	if err != nil {
		t.Skip("No access to AKV")
	}
	name, err := akvkeys.CreateRSAKey(client)
	assert.NoError(t, err, "CreateRSAKey")
	defer akvkeys.DeleteRSAKey(client, name)
	keyPath, _ := url.JoinPath(vaultURL, name)
	t.Log("KeyPath:", keyPath)
	p := &KeyProvider
	p.SetCertificateCredential("", cred)
	plainKey := make([]byte, 32)
	_, _ = rand.Read(plainKey)
	t.Log("Plainkey:", plainKey)
	encryptedKey, err := p.EncryptColumnEncryptionKey(context.Background(), keyPath, aecmk.KeyEncryptionAlgorithm, plainKey)
	if err != nil {
		if unwrapped := errors.Unwrap(err); unwrapped != nil {
			t.Logf("Inner error: %+v", unwrapped)
		}
	}
	if assert.NoError(t, err, "EncryptColumnEncryptionKey") {
		t.Log("Encryptedkey:", encryptedKey)
		assert.NotEqualValues(t, plainKey, encryptedKey, "encryptedKey is the same as plainKey")
		decryptedKey, err := p.DecryptColumnEncryptionKey(context.Background(), keyPath, aecmk.KeyEncryptionAlgorithm, encryptedKey)
		if assert.NoError(t, err, "DecryptColumnEncryptionKey") {
			assert.Equalf(t, plainKey, decryptedKey, "decryptedkey doesn't match plainKey. %v : %v", decryptedKey, plainKey)
		}
	}
}
