package aecmk

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Mock provider for testing
type mockProvider struct {
	decryptFunc       func(ctx context.Context, keyPath string, alg string, encryptedCek []byte) ([]byte, error)
	encryptFunc       func(ctx context.Context, keyPath string, alg string, cek []byte) ([]byte, error)
	signFunc          func(ctx context.Context, keyPath string, allowEnclave bool) ([]byte, error)
	verifyFunc        func(ctx context.Context, keyPath string, allowEnclave bool) (*bool, error)
	keyLifetimeResult *time.Duration
}

func (m *mockProvider) DecryptColumnEncryptionKey(ctx context.Context, masterKeyPath string, encryptionAlgorithm string, encryptedCek []byte) ([]byte, error) {
	if m.decryptFunc != nil {
		return m.decryptFunc(ctx, masterKeyPath, encryptionAlgorithm, encryptedCek)
	}
	return []byte("decrypted-key"), nil
}

func (m *mockProvider) EncryptColumnEncryptionKey(ctx context.Context, masterKeyPath string, encryptionAlgorithm string, cek []byte) ([]byte, error) {
	if m.encryptFunc != nil {
		return m.encryptFunc(ctx, masterKeyPath, encryptionAlgorithm, cek)
	}
	return []byte("encrypted-key"), nil
}

func (m *mockProvider) SignColumnMasterKeyMetadata(ctx context.Context, masterKeyPath string, allowEnclaveComputations bool) ([]byte, error) {
	if m.signFunc != nil {
		return m.signFunc(ctx, masterKeyPath, allowEnclaveComputations)
	}
	return []byte("signature"), nil
}

func (m *mockProvider) VerifyColumnMasterKeyMetadata(ctx context.Context, masterKeyPath string, allowEnclaveComputations bool) (*bool, error) {
	if m.verifyFunc != nil {
		return m.verifyFunc(ctx, masterKeyPath, allowEnclaveComputations)
	}
	result := true
	return &result, nil
}

func (m *mockProvider) KeyLifetime() *time.Duration {
	return m.keyLifetimeResult
}

func TestError_Error(t *testing.T) {
	t.Parallel()
	err := &Error{
		Operation: Decryption,
		msg:       "test error message",
	}
	assert.Equal(t, "test error message", err.Error())
}

func TestError_Unwrap(t *testing.T) {
	t.Parallel()
	innerErr := errors.New("inner error")
	err := &Error{
		Operation: Encryption,
		msg:       "outer error",
		err:       innerErr,
	}
	assert.Equal(t, innerErr, err.Unwrap(), "Unwrap did not return inner error")
}

func TestError_Unwrap_Nil(t *testing.T) {
	t.Parallel()
	err := &Error{
		Operation: Validation,
		msg:       "error without cause",
		err:       nil,
	}
	assert.Nil(t, err.Unwrap())
}

func TestNewError(t *testing.T) {
	t.Parallel()
	innerErr := errors.New("cause")
	err := NewError(Decryption, "decryption failed", innerErr)

	aecmkErr, ok := err.(*Error)
	assert.True(t, ok, "expected *Error type")
	assert.Equal(t, Decryption, aecmkErr.Operation)
	assert.Equal(t, "decryption failed", aecmkErr.Error())
	assert.ErrorIs(t, err, innerErr, "error chain should contain inner error")
}

func TestKeyPathNotAllowed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path      string
		operation Operation
	}{
		{"/invalid/path", Decryption},
		{"http://evil.com/key", Encryption},
		{"", Validation},
	}
	for _, tc := range tests {
		err := KeyPathNotAllowed(tc.path, tc.operation)
		aecmkErr, ok := err.(*Error)
		assert.True(t, ok, "expected *Error type")
		assert.Equal(t, tc.operation, aecmkErr.Operation)
		assert.Nil(t, aecmkErr.Unwrap(), "KeyPathNotAllowed should have nil wrapped error")
	}
}

func TestOperationConstants(t *testing.T) {
	t.Parallel()
	// Verify operation constants are distinct
	assert.NotEqual(t, Decryption, Encryption, "operation constants should be distinct")
	assert.NotEqual(t, Encryption, Validation, "operation constants should be distinct")
	assert.NotEqual(t, Decryption, Validation, "operation constants should be distinct")
}

func TestProviderConstants(t *testing.T) {
	t.Parallel()
	// Verify provider name constants
	assert.Equal(t, "MSSQL_CERTIFICATE_STORE", CertificateStoreKeyProvider)
	assert.Equal(t, "AZURE_KEY_VAULT", AzureKeyVaultKeyProvider)
	assert.Equal(t, "RSA_OAEP", KeyEncryptionAlgorithm)
}

func TestNewCekProvider(t *testing.T) {
	t.Parallel()
	mock := &mockProvider{}
	cp := NewCekProvider(mock)
	assert.NotNil(t, cp, "NewCekProvider returned nil")
	assert.Equal(t, mock, cp.Provider, "Provider not set correctly")
	assert.NotNil(t, cp.decryptedKeys, "decryptedKeys cache not initialized")
}

func TestCekProvider_GetDecryptedKey_Fresh(t *testing.T) {
	t.Parallel()
	decryptCalled := 0
	mock := &mockProvider{
		decryptFunc: func(ctx context.Context, keyPath string, alg string, encryptedCek []byte) ([]byte, error) {
			decryptCalled++
			return []byte("decrypted"), nil
		},
	}
	cp := NewCekProvider(mock)
	ctx := context.Background()

	key, err := cp.GetDecryptedKey(ctx, "path1", []byte("encrypted"))
	assert.NoError(t, err)
	assert.Equal(t, "decrypted", string(key))
	assert.Equal(t, 1, decryptCalled, "expected decrypt called once")
}

func TestCekProvider_GetDecryptedKey_Cached(t *testing.T) {
	t.Parallel()
	decryptCalled := 0
	mock := &mockProvider{
		decryptFunc: func(ctx context.Context, keyPath string, alg string, encryptedCek []byte) ([]byte, error) {
			decryptCalled++
			return []byte("decrypted"), nil
		},
	}
	cp := NewCekProvider(mock)
	ctx := context.Background()

	// First call - should decrypt
	_, _ = cp.GetDecryptedKey(ctx, "path1", []byte("encrypted"))
	// Second call - should use cache
	key, err := cp.GetDecryptedKey(ctx, "path1", []byte("encrypted"))
	assert.NoError(t, err)
	assert.Equal(t, "decrypted", string(key))
	assert.Equal(t, 1, decryptCalled, "expected decrypt called once (cached)")
}

func TestCekProvider_GetDecryptedKey_ExpiredCache(t *testing.T) {
	t.Parallel()
	decryptCalled := 0
	shortLifetime := 1 * time.Millisecond
	mock := &mockProvider{
		decryptFunc: func(ctx context.Context, keyPath string, alg string, encryptedCek []byte) ([]byte, error) {
			decryptCalled++
			return []byte("decrypted"), nil
		},
		keyLifetimeResult: &shortLifetime,
	}
	cp := NewCekProvider(mock)
	ctx := context.Background()

	// First call - should decrypt
	_, _ = cp.GetDecryptedKey(ctx, "path1", []byte("encrypted"))
	// Wait for cache to expire
	time.Sleep(10 * time.Millisecond)
	// Second call - should decrypt again (cache expired)
	_, _ = cp.GetDecryptedKey(ctx, "path1", []byte("encrypted"))
	assert.Equal(t, 2, decryptCalled, "expected decrypt called twice (cache expired)")
}

func TestCekProvider_GetDecryptedKey_Error(t *testing.T) {
	t.Parallel()
	expectedErr := errors.New("decrypt failed")
	mock := &mockProvider{
		decryptFunc: func(ctx context.Context, keyPath string, alg string, encryptedCek []byte) ([]byte, error) {
			return nil, expectedErr
		},
	}
	cp := NewCekProvider(mock)
	ctx := context.Background()

	_, err := cp.GetDecryptedKey(ctx, "path1", []byte("encrypted"))
	assert.Equal(t, expectedErr, err)
}

func TestCekProvider_GetDecryptedKey_DifferentPaths(t *testing.T) {
	t.Parallel()
	decryptCalled := 0
	mock := &mockProvider{
		decryptFunc: func(ctx context.Context, keyPath string, alg string, encryptedCek []byte) ([]byte, error) {
			decryptCalled++
			return []byte("key-" + keyPath), nil
		},
	}
	cp := NewCekProvider(mock)
	ctx := context.Background()

	key1, _ := cp.GetDecryptedKey(ctx, "path1", []byte("encrypted"))
	key2, _ := cp.GetDecryptedKey(ctx, "path2", []byte("encrypted"))

	assert.Equal(t, "key-path1", string(key1))
	assert.Equal(t, "key-path2", string(key2))
	assert.Equal(t, 2, decryptCalled, "expected decrypt called twice for different paths")
}

func TestColumnEncryptionKeyLifetime_Default(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 2*time.Hour, ColumnEncryptionKeyLifetime, "expected default lifetime of 2 hours")
}

func TestRegisterCekProvider(t *testing.T) {
	// Create a mock provider
	mock := &mockProvider{}

	// Register with a unique name
	uniqueName := "test_register_provider_unique_123"
	err := RegisterCekProvider(uniqueName, mock)
	assert.NoError(t, err, "RegisterCekProvider failed")

	// Try to register again with the same name - should fail
	err = RegisterCekProvider(uniqueName, mock)
	assert.Error(t, err, "Expected error when registering duplicate provider")
}

func TestGetGlobalCekProviders(t *testing.T) {
	providers := GetGlobalCekProviders()
	assert.NotNil(t, providers, "GetGlobalCekProviders returned nil")
	// Should have at least the providers registered during init
	// (pfx provider is registered in localcert)
}
