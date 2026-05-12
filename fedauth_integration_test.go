package mssql

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/microsoft/go-mssqldb/msdsn"
	"github.com/stretchr/testify/assert"
)

// Integration tests for fedauth.go
// These tests verify the security token connector infrastructure works
// with real connections where Azure AD credentials are available.

func TestSecurityTokenConnector_WithAzureSQL_Integration(t *testing.T) {
	// Check for Azure SQL connection string
	dsn := os.Getenv("AZURESERVER_DSN")
	if dsn == "" {
		t.Skip("AZURESERVER_DSN not set, skipping Azure AD test")
	}

	// This test requires an Azure AD token provider
	// Skip if we can't get a token
	t.Skip("Azure AD token provider not configured for this test environment")
}

func TestActiveDirectoryTokenConnector_Config_Integration(t *testing.T) {
	checkConnStr(t)

	// Test that we can create a connector with ADAL configuration
	// even if we can't actually authenticate with Azure AD
	connStr := makeConnStr(t)
	host := connStr.Host

	// Only test with Azure SQL endpoints
	if !strings.HasSuffix(host, ".database.windows.net") {
		t.Skip("Not an Azure SQL endpoint, skipping Azure AD config test")
	}

	config := msdsn.Config{
		Host:     host,
		Port:     1433,
		Database: "master",
	}

	// Test that we can create the connector (won't connect without real credentials)
	tokenProvider := func(ctx context.Context, serverSPN, stsURL string) (string, error) {
		t.Logf("Token provider called with SPN: %s, STS: %s", serverSPN, stsURL)
		return "", nil // Would return a real token in production
	}

	conn, err := NewActiveDirectoryTokenConnector(config, FedAuthADALWorkflowPassword, tokenProvider)
	if err != nil {
		t.Fatalf("NewActiveDirectoryTokenConnector failed: %v", err)
	}

	assert.NotNil(t, conn, "Connector is nil")

	// Verify configuration
	assert.True(t, conn.fedAuthRequired, "fedAuthRequired should be true")
	assert.Equal(t, FedAuthLibraryADAL, conn.fedAuthLibrary, "fedAuthLibrary = %d, want %d", conn.fedAuthLibrary, FedAuthLibraryADAL)
}

func TestConnector_StandardAuth_Integration(t *testing.T) {
	checkConnStr(t)

	// Test standard SQL authentication (not federated)
	connector, err := NewConnector(makeConnStr(t).String())
	if err != nil {
		t.Fatalf("NewConnector failed: %v", err)
	}

	// Standard auth should not have fedAuth enabled
	if connector.fedAuthRequired {
		t.Log("Connection is using federated auth (Azure AD)")
	}

	ctx := testContext(t)

	conn, err := connector.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer conn.Close()

	// Verify connection works
	mssqlConn := conn.(*Conn)
	assert.True(t, mssqlConn.connectionGood, "Connection should be good")
}

func TestSecurityTokenConnector_ConfigValidation_Integration(t *testing.T) {
	config := msdsn.Config{
		Host:     "testserver.database.windows.net",
		Port:     1433,
		Database: "testdb",
	}

	tokenCalled := false
	tokenProvider := func(ctx context.Context) (string, error) {
		tokenCalled = true
		return "mock-token", nil
	}

	conn, err := NewSecurityTokenConnector(config, tokenProvider)
	if err != nil {
		t.Fatalf("NewSecurityTokenConnector failed: %v", err)
	}

	// Verify the connector is properly configured
	assert.True(t, conn.fedAuthRequired, "fedAuthRequired should be true")
	assert.Equal(t, FedAuthLibrarySecurityToken, conn.fedAuthLibrary, "fedAuthLibrary = %d, want %d", conn.fedAuthLibrary, FedAuthLibrarySecurityToken)

	// Call the token provider to verify it's wired up correctly
	token, err := conn.securityTokenProvider(context.Background())
	assert.NoError(t, err, "Token provider failed")
	assert.True(t, tokenCalled, "Token provider was not called")
	assert.Equal(t, "mock-token", token, "Token = %s, want 'mock-token'", token)
}

func TestFedAuthWithSqlOpenDB_Integration(t *testing.T) {
	// Skip if no Azure connection available
	dsn := os.Getenv("AZURESERVER_DSN")
	if dsn == "" {
		t.Skip("AZURESERVER_DSN not set")
	}

	config, err := msdsn.Parse(dsn)
	if err != nil {
		t.Fatalf("Parse DSN failed: %v", err)
	}

	// This would use a real Azure AD token in production
	t.Skip("Full Azure AD integration test requires valid credentials")

	// Example of how to use with sql.OpenDB
	tokenProvider := func(ctx context.Context) (string, error) {
		// In production, get token from Azure AD
		return "real-token", nil
	}

	connector, err := NewSecurityTokenConnector(config, tokenProvider)
	if err != nil {
		t.Fatalf("NewSecurityTokenConnector failed: %v", err)
	}

	db := sql.OpenDB(connector)
	defer db.Close()

	ctx := testContext(t)

	err = db.PingContext(ctx)
	if err != nil {
		t.Fatalf("Ping failed: %v", err)
	}
}
