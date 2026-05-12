package mssql

import (
	"context"
	"errors"
	"testing"

	"github.com/microsoft/go-mssqldb/msdsn"
	"github.com/stretchr/testify/assert"
)

func TestFedAuthLibraryConstants(t *testing.T) {
	// Verify the constant values match expected TDS protocol values
	tests := []struct {
		name     string
		value    int
		expected int
	}{
		{"FedAuthLibraryLiveIDCompactToken", FedAuthLibraryLiveIDCompactToken, 0x00},
		{"FedAuthLibrarySecurityToken", FedAuthLibrarySecurityToken, 0x01},
		{"FedAuthLibraryADAL", FedAuthLibraryADAL, 0x02},
		{"FedAuthLibraryReserved", FedAuthLibraryReserved, 0x7F},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.value, "%s = 0x%02X, want 0x%02X", tt.name, tt.value, tt.expected)
		})
	}
}

func TestFedAuthADALWorkflowConstants(t *testing.T) {
	// Verify the constant values match expected TDS protocol values
	tests := []struct {
		name     string
		value    int
		expected int
	}{
		{"FedAuthADALWorkflowPassword", FedAuthADALWorkflowPassword, 0x01},
		{"FedAuthADALWorkflowIntegrated", FedAuthADALWorkflowIntegrated, 0x02},
		{"FedAuthADALWorkflowMSI", FedAuthADALWorkflowMSI, 0x03},
		{"FedAuthADALWorkflowNone", FedAuthADALWorkflowNone, 0x04},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.value, "%s = 0x%02X, want 0x%02X", tt.name, tt.value, tt.expected)
		})
	}
}

func TestNewSecurityTokenConnector(t *testing.T) {
	config := msdsn.Config{
		Host:     "testserver.database.windows.net",
		Port:     1433,
		Database: "testdb",
	}

	tests := []struct {
		name          string
		tokenProvider func(ctx context.Context) (string, error)
		wantErr       bool
		errContains   string
	}{
		{
			name: "valid token provider",
			tokenProvider: func(ctx context.Context) (string, error) {
				return "test-token", nil
			},
			wantErr: false,
		},
		{
			name:          "nil token provider",
			tokenProvider: nil,
			wantErr:       true,
			errContains:   "tokenProvider cannot be nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, err := NewSecurityTokenConnector(config, tt.tokenProvider)

			if tt.wantErr {
				assert.Error(t, err, "NewSecurityTokenConnector() expected error but got nil")
				if tt.errContains != "" {
					assert.Equal(t, "mssql: "+tt.errContains, err.Error(), "NewSecurityTokenConnector() error = %v, want to contain %q", err, tt.errContains)
				}
				return
			}

			assert.NoError(t, err, "NewSecurityTokenConnector() unexpected error")
			assert.NotNil(t, conn, "NewSecurityTokenConnector() returned nil connector")
			assert.True(t, conn.fedAuthRequired, "fedAuthRequired should be true")
			assert.Equal(t, FedAuthLibrarySecurityToken, conn.fedAuthLibrary, "fedAuthLibrary = %d, want %d", conn.fedAuthLibrary, FedAuthLibrarySecurityToken)
			assert.NotNil(t, conn.securityTokenProvider, "securityTokenProvider should not be nil")

			// Verify the token provider works
			token, err := conn.securityTokenProvider(context.Background())
			assert.NoError(t, err, "securityTokenProvider() error")
			assert.Equal(t, "test-token", token, "securityTokenProvider() = %q, want %q", token, "test-token")
		})
	}
}

func TestNewActiveDirectoryTokenConnector(t *testing.T) {
	config := msdsn.Config{
		Host:     "testserver.database.windows.net",
		Port:     1433,
		Database: "testdb",
	}

	tests := []struct {
		name          string
		adalWorkflow  byte
		tokenProvider func(ctx context.Context, serverSPN, stsURL string) (string, error)
		wantErr       bool
		errContains   string
	}{
		{
			name:         "valid token provider with password workflow",
			adalWorkflow: FedAuthADALWorkflowPassword,
			tokenProvider: func(ctx context.Context, serverSPN, stsURL string) (string, error) {
				return "ad-token", nil
			},
			wantErr: false,
		},
		{
			name:         "valid token provider with integrated workflow",
			adalWorkflow: FedAuthADALWorkflowIntegrated,
			tokenProvider: func(ctx context.Context, serverSPN, stsURL string) (string, error) {
				return "integrated-token", nil
			},
			wantErr: false,
		},
		{
			name:         "valid token provider with MSI workflow",
			adalWorkflow: FedAuthADALWorkflowMSI,
			tokenProvider: func(ctx context.Context, serverSPN, stsURL string) (string, error) {
				return "msi-token", nil
			},
			wantErr: false,
		},
		{
			name:          "nil token provider",
			adalWorkflow:  FedAuthADALWorkflowPassword,
			tokenProvider: nil,
			wantErr:       true,
			errContains:   "tokenProvider cannot be nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, err := NewActiveDirectoryTokenConnector(config, tt.adalWorkflow, tt.tokenProvider)

			if tt.wantErr {
				assert.Error(t, err, "NewActiveDirectoryTokenConnector() expected error but got nil")
				if tt.errContains != "" {
					assert.Equal(t, "mssql: "+tt.errContains, err.Error(), "NewActiveDirectoryTokenConnector() error = %v, want to contain %q", err, tt.errContains)
				}
				return
			}

			assert.NoError(t, err, "NewActiveDirectoryTokenConnector() unexpected error")
			assert.NotNil(t, conn, "NewActiveDirectoryTokenConnector() returned nil connector")
			assert.True(t, conn.fedAuthRequired, "fedAuthRequired should be true")

			assert.Equal(t, FedAuthLibraryADAL, conn.fedAuthLibrary, "fedAuthLibrary = %d, want %d", conn.fedAuthLibrary, FedAuthLibraryADAL)
			assert.Equal(t, tt.adalWorkflow, conn.fedAuthADALWorkflow, "fedAuthADALWorkflow = %d, want %d", conn.fedAuthADALWorkflow, tt.adalWorkflow)
			assert.NotNil(t, conn.adalTokenProvider, "adalTokenProvider should not be nil")

			// Verify the token provider works
			token, err := conn.adalTokenProvider(context.Background(), "testSPN", "https://sts.test.com")
			assert.NoError(t, err, "adalTokenProvider() error")
			assert.NotEmpty(t, token, "adalTokenProvider() returned empty token")
		})
	}
}

func TestSecurityTokenConnector_TokenProviderError(t *testing.T) {
	config := msdsn.Config{
		Host:     "testserver.database.windows.net",
		Port:     1433,
		Database: "testdb",
	}

	expectedErr := errors.New("token retrieval failed")
	tokenProvider := func(ctx context.Context) (string, error) {
		return "", expectedErr
	}

	conn, err := NewSecurityTokenConnector(config, tokenProvider)
	assert.NoError(t, err, "NewSecurityTokenConnector() unexpected error")

	// Verify the token provider returns error correctly
	_, err = conn.securityTokenProvider(context.Background())
	assert.Equal(t, expectedErr, err, "securityTokenProvider() error = %v, want %v", err, expectedErr)
}

func TestActiveDirectoryTokenConnector_TokenProviderError(t *testing.T) {
	config := msdsn.Config{
		Host:     "testserver.database.windows.net",
		Port:     1433,
		Database: "testdb",
	}

	expectedErr := errors.New("AD token retrieval failed")
	tokenProvider := func(ctx context.Context, serverSPN, stsURL string) (string, error) {
		return "", expectedErr
	}

	conn, err := NewActiveDirectoryTokenConnector(config, FedAuthADALWorkflowPassword, tokenProvider)
	assert.NoError(t, err, "NewActiveDirectoryTokenConnector() unexpected error")

	// Verify the token provider returns error correctly
	_, err = conn.adalTokenProvider(context.Background(), "testSPN", "https://sts.test.com")
	assert.Equal(t, expectedErr, err, "adalTokenProvider() error = %v, want %v", err, expectedErr)
}

func TestActiveDirectoryTokenConnector_SPNAndSTSPassedCorrectly(t *testing.T) {
	config := msdsn.Config{
		Host:     "testserver.database.windows.net",
		Port:     1433,
		Database: "testdb",
	}

	var receivedSPN, receivedSTS string
	tokenProvider := func(ctx context.Context, serverSPN, stsURL string) (string, error) {
		receivedSPN = serverSPN
		receivedSTS = stsURL
		return "token", nil
	}

	conn, err := NewActiveDirectoryTokenConnector(config, FedAuthADALWorkflowPassword, tokenProvider)
	assert.NoError(t, err, "NewActiveDirectoryTokenConnector() unexpected error")

	expectedSPN := "https://database.windows.net"
	expectedSTS := "https://login.microsoftonline.com/tenant"

	_, err = conn.adalTokenProvider(context.Background(), expectedSPN, expectedSTS)
	assert.NoError(t, err, "adalTokenProvider() unexpected error")
	assert.Equal(t, expectedSPN, receivedSPN, "adalTokenProvider() received SPN = %q, want %q", receivedSPN, expectedSPN)
	assert.Equal(t, expectedSTS, receivedSTS, "adalTokenProvider() received STS = %q, want %q", receivedSTS, expectedSTS)
}
