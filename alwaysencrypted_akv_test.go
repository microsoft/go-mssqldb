//go:build go1.18
// +build go1.18

package mssql

import (
	"net/url"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azkeys"

	"github.com/microsoft/go-mssqldb/aecmk"
	"github.com/microsoft/go-mssqldb/aecmk/akv"
	"github.com/microsoft/go-mssqldb/internal/akvkeys"
	"github.com/stretchr/testify/assert"
)

type akvProviderTest struct {
	client  *azkeys.Client
	keyName string
}

func (p *akvProviderTest) ProvisionMasterKey(t *testing.T) string {
	t.Helper()
	client, vaultUrl, err := akvkeys.GetTestAKV()
	if err != nil {
		t.Skip("Unable to access AKV")
	}
	name, err := akvkeys.CreateRSAKey(client)
	assert.NoError(t, err, "CreateRSAKey")
	keyPath, _ := url.JoinPath(vaultUrl, name)
	p.client = client
	p.keyName = name
	return keyPath
}

func (p *akvProviderTest) DeleteMasterKey(t *testing.T) {
	t.Helper()
	err := akvkeys.DeleteRSAKey(p.client, p.keyName)
	assert.NoError(t, err, "DeleteRSAKey")
}

func (p *akvProviderTest) GetProvider(t *testing.T) aecmk.ColumnEncryptionKeyProvider {
	t.Helper()
	return &akv.AkvKeyProvider
}

func (p *akvProviderTest) Name() string {
	return aecmk.AzureKeyVaultKeyProvider
}

func init() {
	addProviderTest(&akvProviderTest{})
}
