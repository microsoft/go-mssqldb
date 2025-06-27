//go:build go1.18
// +build go1.18

package akvkeys

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"net/url"
	"os"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azkeys"
)

func GetTestAKV(t testing.TB) (client *azkeys.Client, u string, err error) {
	t.Helper()
	vaultName := os.Getenv("KEY_VAULT_NAME")
	if len(vaultName) == 0 {
		err = fmt.Errorf("KEY_VAULT_NAME is not set in the environment")
		return
	}
	vaultURL := fmt.Sprintf("https://%s.vault.azure.net/", url.PathEscape(vaultName))
	var cred azcore.TokenCredential
	cred, err = azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return
	}
	sc := os.Getenv("AZURESUBSCRIPTION_SERVICE_CONNECTION_ID")
	if len(sc) > 0 {
		t.Log("Using Azure Pipelines credential for AKV access")
		tenant := os.Getenv("AZURESUBSCRIPTION_TENANT_ID")
		clientID := os.Getenv("AZURESUBSCRIPTION_CLIENT_ID")
		token := os.Getenv("SYSTEM_ACCESSTOKEN")
		pcred, errp := azidentity.NewAzurePipelinesCredential(tenant, clientID, sc, token, nil)
		if errp == nil {
			chain := make([]azcore.TokenCredential, 2)
			chain[0] = pcred
			chain[1] = cred
			cred, err = azidentity.NewChainedTokenCredential(chain, nil)
			if err != nil {
				return
			}
		} else {
			t.Logf("Failed to create AzurePipelinesCredential: %v", errp)
			t.Log("Using DefaultAzureCredential for AKV access")
		}
	}

	client, err = azkeys.NewClient(vaultURL, cred, nil)
	if err != nil {
		return
	}
	u = vaultURL + "keys"
	return
}

func CreateRSAKey(client *azkeys.Client) (name string, err error) {
	kt := azkeys.KeyTypeRSA
	ks := int32(2048)
	rsaKeyParams := azkeys.CreateKeyParameters{
		Kty:     &kt,
		KeySize: &ks,
	}

	i, _ := rand.Int(rand.Reader, big.NewInt(1000000))
	name = fmt.Sprintf("go-mssqlkey%d", i)
	_, err = client.CreateKey(context.TODO(), name, rsaKeyParams, nil)
	if err != nil {
		_, err = client.RecoverDeletedKey(context.TODO(), name, &azkeys.RecoverDeletedKeyOptions{})
	}
	return
}

func DeleteRSAKey(client *azkeys.Client, name string) bool {
	_, err := client.DeleteKey(context.TODO(), name, nil)
	return err == nil
}
