//go:build !windows && go1.13
// +build !windows,go1.13

package mssql

import (
	"fmt"
	"io/ioutil"
	"os"

	"github.com/jcmturner/gokrb5/v8/config"
	"github.com/jcmturner/gokrb5/v8/credentials"
	"github.com/jcmturner/gokrb5/v8/keytab"
	"github.com/microsoft/go-mssqldb/msdsn"
)

func getAuthN(user, password, serverSPN, workstation string, Kerberos *Kerberos) (auth auth, authOk bool) {
	if Kerberos != nil && Kerberos.Config != nil {
		auth, authOk = getKRB5Auth(user, serverSPN, Kerberos.Config, Kerberos.Keytab, Kerberos.Cache)
	} else {
		auth, authOk = getAuth(user, password, serverSPN, workstation)
	}
	return
}

func getKrbParams(krb msdsn.KerberosConfig) (krbParams *Kerberos, err error) {
	if krb.Krb5ConfFile != "" {
		krbParams = &Kerberos{}
		krbParams.Config, err = setupKerbConfig(krb.Krb5ConfFile)
		if err != nil {
			return nil, fmt.Errorf("cannot read kerberos config file: %w", err)
		}

		if krb.KrbCache != "" {
			krbParams.Cache, err = setupKerbCache(krb.KrbCache)
			if err != nil {
				return nil, fmt.Errorf("cannot read kerberos cache file: %w", err)
			}
		}

		if krb.KeytabFile != "" {
			krbParams.Keytab, err = setupKerbKeytab(krb.KeytabFile)
			if err != nil {
				return nil, fmt.Errorf("cannot read kerberos keytab file: %w", err)
			}
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
