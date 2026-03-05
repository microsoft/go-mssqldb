package algorithms_test

import (
	"encoding/hex"
	"testing"

	"github.com/microsoft/go-mssqldb/internal/github.com/swisscom/mssql-always-encrypted/pkg/algorithms"
	"github.com/microsoft/go-mssqldb/internal/github.com/swisscom/mssql-always-encrypted/pkg/encryption"
	"github.com/microsoft/go-mssqldb/internal/github.com/swisscom/mssql-always-encrypted/pkg/keys"
	"github.com/stretchr/testify/assert"
)

func TestAeadAes256CbcHmac256Algorithm_Encrypt(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		plaintext  []byte
		encType    encryption.Type
		algVersion byte
	}{
		{
			name:       "simple plaintext deterministic",
			plaintext:  []byte("Hello, World!"),
			encType:    encryption.Deterministic,
			algVersion: 1,
		},
		{
			name:       "empty plaintext deterministic",
			plaintext:  []byte{},
			encType:    encryption.Deterministic,
			algVersion: 1,
		},
		{
			name:       "simple plaintext randomized",
			plaintext:  []byte("Hello, World!"),
			encType:    encryption.Randomized,
			algVersion: 1,
		},
		{
			name:       "binary data deterministic",
			plaintext:  []byte{0x00, 0x01, 0x02, 0x03, 0xFF, 0xFE},
			encType:    encryption.Deterministic,
			algVersion: 1,
		},
		{
			name:       "long plaintext randomized",
			plaintext:  make([]byte, 1000), // 1000 bytes of zeros
			encType:    encryption.Randomized,
			algVersion: 1,
		},
	}

	// Create a test key
	rootKey := make([]byte, 32)
	for i := range rootKey {
		rootKey[i] = byte(i)
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			key := keys.NewAeadAes256CbcHmac256(rootKey)
			alg := algorithms.NewAeadAes256CbcHmac256Algorithm(key, tc.encType, tc.algVersion)

			// Encrypt
			ciphertext, err := alg.Encrypt(tc.plaintext)
			if err != nil {
				t.Fatalf("Encrypt failed: %v", err)
			}

			// Ciphertext should be longer than plaintext (includes IV, auth tag, version byte)
			assert.Greater(t, len(ciphertext), len(tc.plaintext),
				"Ciphertext length should be greater than plaintext length")

			// First byte should be the algorithm version
			assert.Equal(t, tc.algVersion, ciphertext[0], "First byte should be algorithm version")

			// Decrypt should recover original plaintext
			decrypted, err := alg.Decrypt(ciphertext)
			if err != nil {
				t.Fatalf("Decrypt failed: %v", err)
			}

			assert.Equal(t, tc.plaintext, decrypted, "Decrypted data should match plaintext")
		})
	}
}

func TestAeadAes256CbcHmac256Algorithm_DeterministicSameOutput(t *testing.T) {
	t.Parallel()

	rootKey := make([]byte, 32)
	for i := range rootKey {
		rootKey[i] = byte(i)
	}
	key := keys.NewAeadAes256CbcHmac256(rootKey)
	alg := algorithms.NewAeadAes256CbcHmac256Algorithm(key, encryption.Deterministic, 1)

	plaintext := []byte("Same input should produce same output")

	ciphertext1, err := alg.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("First encrypt failed: %v", err)
	}

	ciphertext2, err := alg.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Second encrypt failed: %v", err)
	}

	// Deterministic encryption should produce same ciphertext
	assert.Equal(t, ciphertext1, ciphertext2, "Deterministic encryption should produce same ciphertext")
}

func TestAeadAes256CbcHmac256Algorithm_RandomizedDifferentOutput(t *testing.T) {
	t.Parallel()

	rootKey := make([]byte, 32)
	for i := range rootKey {
		rootKey[i] = byte(i)
	}
	key := keys.NewAeadAes256CbcHmac256(rootKey)
	alg := algorithms.NewAeadAes256CbcHmac256Algorithm(key, encryption.Randomized, 1)

	plaintext := []byte("Same input should produce different output")

	ciphertext1, err := alg.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("First encrypt failed: %v", err)
	}

	ciphertext2, err := alg.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Second encrypt failed: %v", err)
	}

	// Randomized encryption should produce different ciphertexts
	assert.NotEqual(t, ciphertext1, ciphertext2, "Randomized encryption produced identical ciphertexts")
}

func TestAeadAes256CbcHmac256Algorithm_DecryptErrors(t *testing.T) {
	t.Parallel()

	rootKey := make([]byte, 32)
	for i := range rootKey {
		rootKey[i] = byte(i)
	}
	key := keys.NewAeadAes256CbcHmac256(rootKey)
	alg := algorithms.NewAeadAes256CbcHmac256Algorithm(key, encryption.Deterministic, 1)

	testCases := []struct {
		name       string
		ciphertext []byte
		wantErr    bool
	}{
		{
			name:       "empty ciphertext",
			ciphertext: []byte{},
			wantErr:    true,
		},
		{
			name:       "too short ciphertext",
			ciphertext: []byte{1, 2, 3},
			wantErr:    true,
		},
		{
			name:       "wrong version byte",
			ciphertext: createDummyCiphertext(2, 65), // wrong version
			wantErr:    true,
		},
		{
			name:       "corrupted auth tag",
			ciphertext: createCorruptedCiphertext(),
			wantErr:    true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := alg.Decrypt(tc.ciphertext)
			if tc.wantErr {
				assert.Error(t, err, "Expected error but got nil")
			} else {
				assert.NoError(t, err, "Unexpected error")
			}
		})
	}
}

func createDummyCiphertext(version byte, length int) []byte {
	result := make([]byte, length)
	result[0] = version
	return result
}

func createCorruptedCiphertext() []byte {
	// Create a ciphertext that's long enough but has corrupted auth tag
	result := make([]byte, 65) // minimum length for AES-256-CBC-HMAC-SHA256
	result[0] = 1              // correct version
	return result
}

func TestAeadAes256CbcHmac256Algorithm_RoundTrip(t *testing.T) {
	t.Parallel()

	// Use a known key from the existing test
	rootKey, _ := hex.DecodeString("0ff9e45335df3dec7be0649f741e6ea870e9d49d16fe4be7437ce22489f48ead")
	key := keys.NewAeadAes256CbcHmac256(rootKey)

	testData := []struct {
		name      string
		plaintext []byte
		encType   encryption.Type
	}{
		{"short string", []byte("test"), encryption.Deterministic},
		{"unicode string", []byte("日本語テスト"), encryption.Randomized},
		{"binary zeros", make([]byte, 16), encryption.Deterministic},
		{"single byte", []byte{0x42}, encryption.Randomized},
	}

	for _, td := range testData {
		t.Run(td.name, func(t *testing.T) {
			alg := algorithms.NewAeadAes256CbcHmac256Algorithm(key, td.encType, 1)

			encrypted, err := alg.Encrypt(td.plaintext)
			if err != nil {
				t.Fatalf("Encrypt failed: %v", err)
			}

			decrypted, err := alg.Decrypt(encrypted)
			if err != nil {
				t.Fatalf("Decrypt failed: %v", err)
			}

			assert.Equal(t, td.plaintext, decrypted, "Decrypted data should match plaintext")
		})
	}
}
