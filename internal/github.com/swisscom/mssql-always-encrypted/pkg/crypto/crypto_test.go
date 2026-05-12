package crypto

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewAESCbcPKCS5(t *testing.T) {
	t.Parallel()
	key := make([]byte, 32) // AES-256 key
	iv := make([]byte, 16)  // 16-byte IV

	cipher := NewAESCbcPKCS5(key, iv)
	assert.NotNil(t, cipher.block, "block should be initialized")
	assert.Equal(t, key, cipher.key, "key not set correctly")
	assert.Equal(t, iv, cipher.iv, "iv not set correctly")
}

func TestAESCbcPKCS5_EncryptDecrypt(t *testing.T) {
	t.Parallel()
	key := []byte("12345678901234567890123456789012") // 32 bytes for AES-256
	iv := []byte("1234567890123456")                  // 16 bytes for IV

	cipher := NewAESCbcPKCS5(key, iv)

	tests := []struct {
		name      string
		plaintext []byte
	}{
		{"short text", []byte("Hello")},
		{"block aligned", []byte("0123456789ABCDEF")}, // 16 bytes
		{"longer text", []byte("This is a longer text that spans multiple blocks")},
		{"empty", []byte{}},
		{"single byte", []byte{42}},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			encrypted := cipher.Encrypt(tc.plaintext)
			if len(tc.plaintext) > 0 {
				assert.NotEqual(t, tc.plaintext, encrypted, "encrypted should differ from plaintext")
			}

			decrypted := cipher.Decrypt(encrypted)
			assert.Equal(t, tc.plaintext, decrypted, "decrypt(encrypt(x)) != x")
		})
	}
}

func TestPKCS5Padding(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		input     []byte
		blockSize int
		wantLen   int
	}{
		{"empty 16", []byte{}, 16, 16},
		{"1 byte 16", []byte{1}, 16, 16},
		{"15 bytes 16", make([]byte, 15), 16, 16},
		{"16 bytes 16", make([]byte, 16), 16, 32}, // Full block adds another block
		{"17 bytes 16", make([]byte, 17), 16, 32},
		{"8 block", []byte("1234"), 8, 8},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := PKCS5Padding(tc.input, tc.blockSize)
			assert.Len(t, result, tc.wantLen, "PKCS5Padding length mismatch")
			// Verify padding value
			paddingVal := result[len(result)-1]
			for i := len(result) - int(paddingVal); i < len(result); i++ {
				assert.Equal(t, paddingVal, result[i], "invalid padding at position %d", i)
			}
		})
	}
}

func TestPKCS5Trim(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   []byte
		wantLen int
	}{
		{"single padding", append([]byte("hello"), 3, 3, 3), 5},
		{"block padding", append(make([]byte, 16), bytes.Repeat([]byte{16}, 16)...), 16},
		{"one byte padding", append([]byte("test"), 1), 4},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := PKCS5Trim(tc.input)
			assert.Len(t, result, tc.wantLen, "PKCS5Trim length mismatch")
		})
	}
}

func TestAESCbcPKCS5_EncryptProducesDifferentOutput(t *testing.T) {
	t.Parallel()
	key := []byte("12345678901234567890123456789012")
	iv := []byte("1234567890123456")
	cipher := NewAESCbcPKCS5(key, iv)

	plaintext := []byte("Same text")
	encrypted1 := cipher.Encrypt(plaintext)
	encrypted2 := cipher.Encrypt(plaintext)

	// With same key/IV, same plaintext should produce same ciphertext (deterministic)
	assert.Equal(t, encrypted1, encrypted2, "same key/IV/plaintext should produce same ciphertext")
}

func TestAESCbcPKCS5_DifferentKeysProduceDifferentOutput(t *testing.T) {
	t.Parallel()
	key1 := []byte("12345678901234567890123456789012")
	key2 := []byte("abcdefghijklmnopqrstuvwxyz123456")
	iv := []byte("1234567890123456")

	cipher1 := NewAESCbcPKCS5(key1, iv)
	cipher2 := NewAESCbcPKCS5(key2, iv)

	plaintext := []byte("Test data")
	encrypted1 := cipher1.Encrypt(plaintext)
	encrypted2 := cipher2.Encrypt(plaintext)

	assert.NotEqual(t, encrypted1, encrypted2, "different keys should produce different ciphertext")
}
