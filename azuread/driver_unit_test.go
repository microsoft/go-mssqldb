//go:build go1.18
// +build go1.18

package azuread

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDriverRegistration(t *testing.T) {
	// The driver is registered in init(), verify DriverName is correct
	assert.Equal(t, "azuresql", DriverName, "DriverName")
}

func TestNewConnectorInvalidDSN(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		dsn     string
		wantErr bool
	}{
		{
			name:    "empty dsn",
			dsn:     "",
			wantErr: false, // empty DSN may be valid with defaults
		},
		{
			name:    "invalid fedauth value",
			dsn:     "server=localhost;fedauth=InvalidAuth",
			wantErr: true,
		},
		{
			name:    "missing required password for password flow",
			dsn:     "server=localhost;fedauth=ActiveDirectoryPassword;user id=user@example.com",
			wantErr: true,
		},
		{
			name:    "missing required user for service principal",
			dsn:     "server=localhost;fedauth=ActiveDirectoryServicePrincipal;password=secret",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewConnector(tt.dsn)
			if tt.wantErr {
				assert.Error(t, err, "Expected error")
			}
			// Note: Some DSNs may not error during connector creation, only during connect
		})
	}
}

func TestNewConnectorWithValidDSN(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		dsn     string
		wantErr bool
	}{
		{
			name:    "basic server",
			dsn:     "server=localhost",
			wantErr: false,
		},
		{
			name:    "server with database",
			dsn:     "server=localhost;database=testdb",
			wantErr: false,
		},
		{
			name:    "MSI auth",
			dsn:     "server=myserver.database.windows.net;fedauth=ActiveDirectoryMSI",
			wantErr: false,
		},
		{
			name:    "default auth",
			dsn:     "server=myserver.database.windows.net;fedauth=ActiveDirectoryDefault",
			wantErr: false,
		},
		{
			name:    "URL format",
			dsn:     "sqlserver://localhost?database=testdb",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			connector, err := NewConnector(tt.dsn)
			if tt.wantErr {
				assert.Error(t, err, "Expected error")
			} else {
				assert.NoError(t, err, "Unexpected error")
				assert.NotNil(t, connector, "Expected connector")
			}
		})
	}
}
