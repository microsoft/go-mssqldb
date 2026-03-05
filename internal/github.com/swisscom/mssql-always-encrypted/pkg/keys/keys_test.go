package keys

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewAeadAes256CbcHmac256(t *testing.T) {
	t.Parallel()
	rootKey := make([]byte, 32)
	for i := range rootKey {
		rootKey[i] = byte(i)
	}

	key := NewAeadAes256CbcHmac256(rootKey)

	assert.Equal(t, rootKey, key.RootKey(), "RootKey() should return original root key")
	assert.Len(t, key.EncryptionKey(), 32, "EncryptionKey() length should be 32")
	assert.Len(t, key.MacKey(), 32, "MacKey() length should be 32")
	assert.Len(t, key.IvKey(), 32, "IvKey() length should be 32")
}

func TestAeadAes256CbcHmac256_KeysAreDifferent(t *testing.T) {
	t.Parallel()
	rootKey := make([]byte, 32)
	for i := range rootKey {
		rootKey[i] = byte(i)
	}

	key := NewAeadAes256CbcHmac256(rootKey)

	// All derived keys should be different from each other
	assert.NotEqual(t, key.EncryptionKey(), key.MacKey(), "EncryptionKey and MacKey should be different")
	assert.NotEqual(t, key.EncryptionKey(), key.IvKey(), "EncryptionKey and IvKey should be different")
	assert.NotEqual(t, key.MacKey(), key.IvKey(), "MacKey and IvKey should be different")
	// Derived keys should be different from root key
	assert.NotEqual(t, key.EncryptionKey(), rootKey, "EncryptionKey should differ from root key")
}

func TestAeadAes256CbcHmac256_DeterministicDerivation(t *testing.T) {
	t.Parallel()
	rootKey := []byte("12345678901234567890123456789012")

	key1 := NewAeadAes256CbcHmac256(rootKey)
	key2 := NewAeadAes256CbcHmac256(rootKey)

	assert.Equal(t, key1.EncryptionKey(), key2.EncryptionKey(), "same root key should produce same encryption key")
	assert.Equal(t, key1.MacKey(), key2.MacKey(), "same root key should produce same mac key")
	assert.Equal(t, key1.IvKey(), key2.IvKey(), "same root key should produce same iv key")
}

func TestAeadAes256CbcHmac256_DifferentRootKeys(t *testing.T) {
	t.Parallel()
	rootKey1 := []byte("12345678901234567890123456789012")
	rootKey2 := []byte("abcdefghijklmnopqrstuvwxyz123456")

	key1 := NewAeadAes256CbcHmac256(rootKey1)
	key2 := NewAeadAes256CbcHmac256(rootKey2)

	assert.NotEqual(t, key1.EncryptionKey(), key2.EncryptionKey(), "different root keys should produce different encryption keys")
	assert.NotEqual(t, key1.MacKey(), key2.MacKey(), "different root keys should produce different mac keys")
	assert.NotEqual(t, key1.IvKey(), key2.IvKey(), "different root keys should produce different iv keys")
}

func TestAeadAes256CbcHmac256_ImplementsKeyInterface(t *testing.T) {
	t.Parallel()
	rootKey := make([]byte, 32)
	key := NewAeadAes256CbcHmac256(rootKey)

	// Verify the struct implements Key interface
	var _ Key = key
	var _ Key = &key
}
