//go:build !windows && go1.13
// +build !windows,go1.13

package mssql

import (
	"io/ioutil"
	"os"

	"github.com/jcmturner/gokrb5/v8/config"
	"github.com/jcmturner/gokrb5/v8/credentials"
	"github.com/jcmturner/gokrb5/v8/keytab"
)

func getAuthN(user, password, serverSPN, workstation string, Kerberos map[string]interface{}) (auth auth, authOk bool) {
	if Kerberos != nil && Kerberos["Config"] != nil {
		auth, authOk = getKRB5Auth(user, serverSPN, Kerberos["Config"].(*config.Config), Kerberos["Keytab"].(*keytab.Keytab), Kerberos["Cache"].(*credentials.CCache))
	} else {
		auth, authOk = getAuth(user, password, serverSPN, workstation)
	}
	return
}

func getKrbParams(krb map[string]interface{}) (krbParams map[string]interface{}, err error) {
	if _, ok := krb["Krb5ConfFile"]; ok {
		krbParams = make(map[string]interface{})
		krbParams["Config"], err = setupKerbConfig(krb["Krb5ConfFile"].(string))
		if err != nil {
			return nil, err
		}

		krbParams["Keytab"], err = setupKerbKeytab(krb["KeytabFile"].(string))
		if err != nil {
			return nil, err
		}

		krbParams["Cache"], err = setupKerbCache(krb["KrbCache"].(string))
		if err != nil {
			return nil, err
		}
	}
	return krbParams, nil
}

func setupKerbConfig(krb5configPath string) (*config.Config, error) {
	krb5CnfFile, err := os.Open(krb5configPath)
	if err != nil {
		return nil, err
	}
	c, err := config.NewFromReader(krb5CnfFile)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func setupKerbCache(kerbCCahePath string) (*credentials.CCache, error) {
	cache, err := credentials.LoadCCache(kerbCCahePath)
	if err != nil {
		return nil, err
	}
	return cache, nil
}

func setupKerbKeytab(keytabFilePath string) (*keytab.Keytab, error) {
	var kt = &keytab.Keytab{}
	keytabConf, err := ioutil.ReadFile(keytabFilePath)
	if err != nil {
		return nil, err
	}
	if err = kt.Unmarshal([]byte(keytabConf)); err != nil {
		return nil, err
	}
	return kt, nil
}
