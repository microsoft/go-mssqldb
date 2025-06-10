//go:build go1.18
// +build go1.18

package azuread

import (
	"context"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"

	mssql "github.com/microsoft/go-mssqldb"
	"github.com/microsoft/go-mssqldb/msdsn"
)

func TestValidateParameters(t *testing.T) {
	passphrase := "somesecret"
	accessToken := "some-access-token"
	certificatepath := "/user/cert/cert.pfx"
	appid := "applicationclientid=someguid"
	certprop := "clientcertpath=" + certificatepath
	tests := []struct {
		name     string
		dsn      string
		expected *azureFedAuthConfig
	}{
		{
			name:     "no fed auth configured",
			dsn:      "server=someserver",
			expected: &azureFedAuthConfig{fedAuthLibrary: mssql.FedAuthLibraryReserved},
		},
		{
			name: "application with cert/key",
			dsn:  `sqlserver://service-principal-id%40tenant-id:somesecret@someserver.database.windows.net?fedauth=ActiveDirectoryApplication&` + certprop + "&" + appid,
			expected: &azureFedAuthConfig{
				fedAuthLibrary:      mssql.FedAuthLibraryADAL,
				clientID:            "service-principal-id",
				tenantID:            "tenant-id",
				certificatePath:     certificatepath,
				clientSecret:        passphrase,
				adalWorkflow:        mssql.FedAuthADALWorkflowPassword,
				fedAuthWorkflow:     ActiveDirectoryApplication,
				applicationClientID: "someguid",
			},
		},
		{
			name: "application with cert/key missing tenant id",
			dsn:  "server=someserver.database.windows.net;fedauth=ActiveDirectoryApplication;user id=service-principal-id;password=somesecret;" + certprop + ";" + appid,
			expected: &azureFedAuthConfig{
				fedAuthLibrary:      mssql.FedAuthLibraryADAL,
				clientID:            "service-principal-id",
				certificatePath:     certificatepath,
				clientSecret:        passphrase,
				adalWorkflow:        mssql.FedAuthADALWorkflowPassword,
				fedAuthWorkflow:     ActiveDirectoryApplication,
				applicationClientID: "someguid",
			},
		},
		{
			name: "application with secret",
			dsn:  "server=someserver.database.windows.net;fedauth=ActiveDirectoryServicePrincipal;user id=service-principal-id@tenant-id;password=somesecret;",
			expected: &azureFedAuthConfig{
				clientID:        "service-principal-id",
				tenantID:        "tenant-id",
				clientSecret:    passphrase,
				adalWorkflow:    mssql.FedAuthADALWorkflowPassword,
				fedAuthWorkflow: ActiveDirectoryServicePrincipal,
			},
		},
		{
			name: "user with password",
			dsn:  "server=someserver.database.windows.net;fedauth=ActiveDirectoryPassword;user id=azure-ad-user@example.com;password=somesecret;" + appid,
			expected: &azureFedAuthConfig{
				adalWorkflow:        mssql.FedAuthADALWorkflowPassword,
				user:                "azure-ad-user@example.com",
				password:            passphrase,
				applicationClientID: "someguid",
				fedAuthWorkflow:     ActiveDirectoryPassword,
			},
		},
		{
			name: "managed identity without client id",
			dsn:  "server=someserver.database.windows.net;fedauth=ActiveDirectoryMSI",
			expected: &azureFedAuthConfig{
				adalWorkflow:    mssql.FedAuthADALWorkflowMSI,
				fedAuthWorkflow: ActiveDirectoryMSI,
			},
		},
		{
			name: "managed identity with client id",
			dsn:  "server=someserver.database.windows.net;fedauth=ActiveDirectoryManagedIdentity;user id=identity-client-id",
			expected: &azureFedAuthConfig{
				adalWorkflow:    mssql.FedAuthADALWorkflowMSI,
				clientID:        "identity-client-id",
				fedAuthWorkflow: ActiveDirectoryManagedIdentity,
			},
		},
		{
			name: "managed identity with resource id",
			dsn:  "server=someserver.database.windows.net;fedauth=ActiveDirectoryManagedIdentity;resource id=/subscriptions/{guid}/resourceGroups/{resource-group-name}/{resource-provider-namespace}/{resource-type}/{resource-name}",
			expected: &azureFedAuthConfig{
				adalWorkflow:    mssql.FedAuthADALWorkflowMSI,
				resourceID:      "/subscriptions/{guid}/resourceGroups/{resource-group-name}/{resource-provider-namespace}/{resource-type}/{resource-name}",
				fedAuthWorkflow: ActiveDirectoryManagedIdentity,
			},
		},
		{
			name: "application with access token",
			dsn:  "server=someserver.database.windows.net;fedauth=ActiveDirectoryServicePrincipalAccessToken;password=some-access-token;",
			expected: &azureFedAuthConfig{
				password:        accessToken,
				adalWorkflow:    mssql.FedAuthADALWorkflowNone,
				fedAuthWorkflow: ActiveDirectoryServicePrincipalAccessToken,
				fedAuthLibrary:  mssql.FedAuthLibrarySecurityToken,
			},
		},
		{
			name: "azure developer cli",
			dsn:  "server=someserver.database.windows.net;fedauth=ActiveDirectoryAzureDeveloperCli",
			expected: &azureFedAuthConfig{
				adalWorkflow:    mssql.FedAuthADALWorkflowPassword,
				fedAuthWorkflow: ActiveDirectoryAzureDeveloperCli,
			},
		},
		{
			name: "azure pipelines",
			dsn:  "server=someserver.database.windows.net;fedauth=ActiveDirectoryAzurePipelines;user id=service-principal-id@tenant-id;serviceconnectionid=connection-id;systemtoken=system-token",
			expected: &azureFedAuthConfig{
				clientID:            "service-principal-id",
				tenantID:            "tenant-id",
				serviceConnectionID: "connection-id",
				systemAccessToken:   "system-token",
				adalWorkflow:        mssql.FedAuthADALWorkflowPassword,
				fedAuthWorkflow:     ActiveDirectoryAzurePipelines,
			},
		},
		{
			name: "environment credential",
			dsn:  "server=someserver.database.windows.net;fedauth=ActiveDirectoryEnvironment",
			expected: &azureFedAuthConfig{
				adalWorkflow:    mssql.FedAuthADALWorkflowPassword,
				fedAuthWorkflow: ActiveDirectoryEnvironment,
			},
		},
		{
			name: "workload identity",
			dsn:  "server=someserver.database.windows.net;fedauth=ActiveDirectoryWorkloadIdentity",
			expected: &azureFedAuthConfig{
				adalWorkflow:    mssql.FedAuthADALWorkflowPassword,
				fedAuthWorkflow: ActiveDirectoryWorkloadIdentity,
			},
		},
		{
			name: "workload identity with user id",
			dsn:  "server=someserver.database.windows.net;fedauth=ActiveDirectoryWorkloadIdentity;user id=service-principal-id@tenant-id",
			expected: &azureFedAuthConfig{
				clientID:        "service-principal-id",
				tenantID:        "tenant-id", 
				adalWorkflow:    mssql.FedAuthADALWorkflowPassword,
				fedAuthWorkflow: ActiveDirectoryWorkloadIdentity,
			},
		},
		{
			name: "client assertion",
			dsn:  "server=someserver.database.windows.net;fedauth=ActiveDirectoryClientAssertion;user id=service-principal-id@tenant-id;clientassertion=assertion-token",
			expected: &azureFedAuthConfig{
				clientID:        "service-principal-id",
				tenantID:        "tenant-id",
				clientAssertion: "assertion-token",
				adalWorkflow:    mssql.FedAuthADALWorkflowPassword,
				fedAuthWorkflow: ActiveDirectoryClientAssertion,
			},
		},
		{
			name: "on behalf of with secret",
			dsn:  "server=someserver.database.windows.net;fedauth=ActiveDirectoryOnBehalfOf;user id=service-principal-id@tenant-id;password=somesecret;userassertion=user-token",
			expected: &azureFedAuthConfig{
				clientID:      "service-principal-id",
				tenantID:      "tenant-id",
				clientSecret:  passphrase,
				userAssertion: "user-token",
				adalWorkflow:  mssql.FedAuthADALWorkflowPassword,
				fedAuthWorkflow: ActiveDirectoryOnBehalfOf,
			},
		},
	}
	for _, tst := range tests {
		config, err := parse(tst.dsn)
		if tst.expected == nil {
			if err == nil {
				t.Errorf("No error returned when error expected in test case '%s'", tst.name)
			}
			continue
		}
		if err != nil {
			t.Errorf("Error returned when none expected in test case '%s': %v", tst.name, err)
			continue
		}
		if tst.expected.fedAuthLibrary != mssql.FedAuthLibraryReserved {
			if tst.expected.fedAuthLibrary == 0 {
				tst.expected.fedAuthLibrary = mssql.FedAuthLibraryADAL
			}
		}
		// mssqlConfig is not idempotent due to pointers in it, plus we aren't testing its correctness here
		config.mssqlConfig = msdsn.Config{}
		if !reflect.DeepEqual(config, tst.expected) {
			t.Errorf("Captured parameters do not match in test case '%s'. Expected:%+v, Actual:%+v", tst.name, tst.expected, config)
		}
	}
}

func TestProvideActiveDirectoryTokenValidations(t *testing.T) {
	nonExistentCertPath := os.TempDir() + "non_existent_cert.pem"

	f, err := os.CreateTemp("", "malformed_cert.pem")
	if err != nil {
		t.Fatalf("create temporary file: %v", err)
	}
	if err = f.Truncate(0); err != nil {
		t.Fatalf("truncate temporary file: %v", err)
	}
	if _, err = f.Write([]byte("malformed")); err != nil {
		t.Fatalf("write to temporary file: %v", err)
	}
	if err = f.Close(); err != nil {
		t.Fatalf("close temporary file: %v", err)
	}
	malformedCertPath := f.Name()
	t.Cleanup(func() { _ = os.Remove(malformedCertPath) })

	tests := []struct {
		name                string
		dsn                 string
		expectedErr         error
		expectedErrContains string
	}{
		{
			name: "ActiveDirectoryServicePrincipal_cert_not_found",
			dsn: `sqlserver://someserver.database.windows.net?` +
				`user id=` + url.QueryEscape("my-app-id@my-tenant-id") + "&" +
				`fedauth=ActiveDirectoryServicePrincipal` + "&" +
				`clientcertpath=` + nonExistentCertPath + "&" +
				`applicationclientid=someguid`,
			expectedErr: fs.ErrNotExist,
		},
		{
			name: "ActiveDirectoryServicePrincipal_cert_malformed",
			dsn: `sqlserver://someserver.database.windows.net?` +
				`user id=` + url.QueryEscape("my-app-id@my-tenant-id") + "&" +
				`fedauth=ActiveDirectoryServicePrincipal` + "&" +
				`clientcertpath=` + malformedCertPath + "&" +
				`applicationclientid=someguid`,
			expectedErrContains: "error reading P12 data",
		},
	}
	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			config, err := parse(tst.dsn)
			if err != nil {
				t.Errorf("Unexpected parse error: %v", err)
				return
			}
			_, err = config.provideActiveDirectoryToken(context.Background(), "", "authority/tenant")
			if err == nil {
				t.Errorf("Expected error but got nil")
				return
			}
			if tst.expectedErr != nil {
				if !errors.Is(err, tst.expectedErr) {
					t.Errorf("Expected error '%v' but got err = %v", tst.expectedErr, err)
				}
			}
			if tst.expectedErrContains != "" {
				if !strings.Contains(err.Error(), tst.expectedErrContains) {
					return
				}
			}
		})
	}
}

func TestValidateParametersErrors(t *testing.T) {
	tests := []struct {
		name                string
		dsn                 string
		expectedErrContains string
	}{
		{
			name: "ActiveDirectoryAzurePipelines_missing_serviceconnectionid",
			dsn: `sqlserver://someserver.database.windows.net?` +
				`user id=` + url.QueryEscape("my-app-id@my-tenant-id") + "&" +
				`fedauth=ActiveDirectoryAzurePipelines` + "&" +
				`systemtoken=token`,
			expectedErrContains: "Must provide 'serviceconnectionid' parameter",
		},
		{
			name: "ActiveDirectoryAzurePipelines_missing_systemtoken",
			dsn: `sqlserver://someserver.database.windows.net?` +
				`user id=` + url.QueryEscape("my-app-id@my-tenant-id") + "&" +
				`fedauth=ActiveDirectoryAzurePipelines` + "&" +
				`serviceconnectionid=conn-id`,
			expectedErrContains: "Must provide 'systemtoken' parameter",
		},
		{
			name: "ActiveDirectoryClientAssertion_missing_clientassertion",
			dsn: `sqlserver://someserver.database.windows.net?` +
				`user id=` + url.QueryEscape("my-app-id@my-tenant-id") + "&" +
				`fedauth=ActiveDirectoryClientAssertion`,
			expectedErrContains: "Must provide 'clientassertion' parameter",
		},
		{
			name: "ActiveDirectoryOnBehalfOf_missing_userassertion",
			dsn: `sqlserver://someserver.database.windows.net?` +
				`user id=` + url.QueryEscape("my-app-id@my-tenant-id") + "&" +
				`fedauth=ActiveDirectoryOnBehalfOf` + "&" +
				`password=secret`,
			expectedErrContains: "Must provide 'userassertion' parameter",
		},
		{
			name: "ActiveDirectoryOnBehalfOf_missing_client_auth",
			dsn: `sqlserver://someserver.database.windows.net?` +
				`user id=` + url.QueryEscape("my-app-id@my-tenant-id") + "&" +
				`fedauth=ActiveDirectoryOnBehalfOf` + "&" +
				`userassertion=user-token`,
			expectedErrContains: "Must provide one of 'password', 'clientcertpath', or 'clientassertion'",
		},
	}
	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			_, err := parse(tst.dsn)
			if err == nil {
				t.Errorf("Expected parse error but got nil")
				return
			}
			if !strings.Contains(err.Error(), tst.expectedErrContains) {
				t.Errorf("Expected error to contain '%s' but got '%s'", tst.expectedErrContains, err.Error())
			}
		})
	}
}
