//go:build go1.18
// +build go1.18

package akv

import (
	"context"
	"crypto/rand"
	"errors"
	"net/url"
	"testing"

	"github.com/microsoft/go-mssqldb/aecmk"
	"github.com/microsoft/go-mssqldb/internal/akvkeys"
	"github.com/stretchr/testify/assert"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	client, vaultURL, err := akvkeys.GetTestAKV(t)
	if err != nil {
		t.Skip("No access to AKV")
	}
	name, err := akvkeys.CreateRSAKey(client)
	assert.NoError(t, err, "CreateRSAKey")
	defer akvkeys.DeleteRSAKey(client, name)
	keyPath, _ := url.JoinPath(vaultURL, name)
	t.Log("KeyPath:", keyPath)
	p := &KeyProvider
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
