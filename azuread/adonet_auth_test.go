//go:build go1.18
// +build go1.18

package azuread

import (
	"strings"
	"testing"

	mssql "github.com/microsoft/go-mssqldb"
)

// TestADONetAuthenticationNames tests that ADO.NET authentication method names are correctly normalized
func TestADONetAuthenticationNames(t *testing.T) {
	tests := []struct {
		name             string
		dsn              string
		expectedWorkflow string
		shouldError      bool
		errorContains    string
	}{
		{
			name:             "Sql Password should be ignored (not Azure AD)",
			dsn:              "server=someserver.database.windows.net;authentication=Sql Password;user id=user;password=pwd",
			expectedWorkflow: "",
		},
		{
			name:             "Active Directory Password",
			dsn:              "server=someserver.database.windows.net;authentication=Active Directory Password;user id=user;password=pwd;applicationclientid=guid",
			expectedWorkflow: ActiveDirectoryPassword,
		},
		{
			name:             "Active Directory Integrated",
			dsn:              "server=someserver.database.windows.net;authentication=Active Directory Integrated",
			expectedWorkflow: ActiveDirectoryIntegrated,
		},
		{
			name:             "Active Directory Interactive",
			dsn:              "server=someserver.database.windows.net;authentication=Active Directory Interactive;applicationclientid=guid",
			expectedWorkflow: ActiveDirectoryInteractive,
		},
		{
			name:             "Active Directory Service Principal",
			dsn:              "server=someserver.database.windows.net;authentication=Active Directory Service Principal;user id=client-id@tenant-id;password=secret",
			expectedWorkflow: ActiveDirectoryServicePrincipal,
		},
		{
			name:             "Active Directory Device Code Flow",
			dsn:              "server=someserver.database.windows.net;authentication=Active Directory Device Code Flow;applicationclientid=guid",
			expectedWorkflow: ActiveDirectoryDeviceCode,
		},
		{
			name:             "Active Directory Managed Identity",
			dsn:              "server=someserver.database.windows.net;authentication=Active Directory Managed Identity",
			expectedWorkflow: ActiveDirectoryManagedIdentity,
		},
		{
			name:             "Active Directory MSI",
			dsn:              "server=someserver.database.windows.net;authentication=Active Directory MSI",
			expectedWorkflow: ActiveDirectoryMSI,
		},
		{
			name:             "Active Directory Default",
			dsn:              "server=someserver.database.windows.net;authentication=Active Directory Default",
			expectedWorkflow: ActiveDirectoryDefault,
		},
		{
			name:             "Active Directory Workload Identity",
			dsn:              "server=someserver.database.windows.net;authentication=Active Directory Workload Identity",
			expectedWorkflow: ActiveDirectoryWorkloadIdentity,
		},
		{
			name:             "Mixed case should work",
			dsn:              "server=someserver.database.windows.net;authentication=ACTIVE DIRECTORY DEFAULT",
			expectedWorkflow: ActiveDirectoryDefault,
		},
		{
			name:             "Extra spaces should be handled",
			dsn:              "server=someserver.database.windows.net;authentication=  Active Directory Default  ",
			expectedWorkflow: ActiveDirectoryDefault,
		},
		{
			name:             "Original names still work - ActiveDirectoryPassword",
			dsn:              "server=someserver.database.windows.net;authentication=ActiveDirectoryPassword;user id=user;password=pwd;applicationclientid=guid",
			expectedWorkflow: ActiveDirectoryPassword,
		},
		{
			name:             "Original names still work - ActiveDirectoryDefault",
			dsn:              "server=someserver.database.windows.net;authentication=ActiveDirectoryDefault",
			expectedWorkflow: ActiveDirectoryDefault,
		},
	}

	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			config, err := parse(tst.dsn)
			if tst.shouldError {
				if err == nil {
					t.Errorf("Expected parse error but got nil")
					return
				}
				if !strings.Contains(err.Error(), tst.errorContains) {
					t.Errorf("Expected error to contain '%s' but got '%s'", tst.errorContains, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if tst.expectedWorkflow == "" {
				// For "Sql Password", fedAuthLibrary should be reserved (not Azure AD)
				if config.fedAuthLibrary != mssql.FedAuthLibraryReserved {
					t.Errorf("Expected fedAuthLibrary to be FedAuthLibraryReserved for Sql Password, got %d", config.fedAuthLibrary)
				}
			} else {
				if config.fedAuthWorkflow != tst.expectedWorkflow {
					t.Errorf("Expected workflow %s, got %s", tst.expectedWorkflow, config.fedAuthWorkflow)
				}
			}
		})
	}
}
