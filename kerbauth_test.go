//go:build !windows
// +build !windows

package mssql

import (
	"reflect"
	"testing"

	"github.com/jcmturner/gokrb5/v8/client"
	"github.com/jcmturner/gokrb5/v8/config"
	"github.com/jcmturner/gokrb5/v8/credentials"
	"github.com/jcmturner/gokrb5/v8/keytab"
	"github.com/microsoft/go-mssqldb/msdsn"
)

func TestGetAuth(t *testing.T) {
	kerberos := getKerberos()

	p := getConfig(kerberos, "MSSQLSvc/mssql.domain.com:1433")

	got, _ := getAuthN(p)
	keytab := &krb5Auth{username: "",
		realm:      "domain.com",
		serverSPN:  "MSSQLSvc/mssql.domain.com:1433",
		port:       1433,
		krb5Config: kerberos.Config,
		krbKeytab:  kerberos.Keytab,
		krbCache:   kerberos.Cache,
		state:      0}

	res := reflect.DeepEqual(got, keytab)
	if !res {
		t.Errorf("Failed to get correct krb5Auth object\nExpected:%v\nRecieved:%v", keytab, got)
	}

	got, _ = getAuthN(p)
	keytab = &krb5Auth{username: "",
		realm:      "domain.com",
		serverSPN:  "MSSQLSvc/mssql.domain.com:1433",
		port:       1433,
		krb5Config: kerberos.Config,
		krbKeytab:  kerberos.Keytab,
		krbCache:   kerberos.Cache,
		state:      0}

	res = reflect.DeepEqual(got, keytab)
	if !res {
		t.Errorf("Failed to get correct krb5Auth object\nExpected:%v\nRecieved:%v", keytab, got)
	}

	_, val := getAuthN(getConfig(kerberos, "MSSQLSvc/mssql.domain.com"))
	if val {
		t.Errorf("Failed to get correct krb5Auth object: no port defined")
	}

	got, _ = getAuthN(getConfig(kerberos, "MSSQLSvc/mssql.domain.com:1433@DOMAIN.COM"))
	keytab = &krb5Auth{username: "",
		realm:      "DOMAIN.COM",
		serverSPN:  "MSSQLSvc/mssql.domain.com:1433",
		port:       1433,
		krb5Config: kerberos.Config,
		krbKeytab:  kerberos.Keytab,
		krbCache:   kerberos.Cache,
		state:      0}

	res = reflect.DeepEqual(got, keytab)
	if !res {
		t.Errorf("Failed to get correct krb5Auth object\nExpected:%v\nRecieved:%v", keytab, got)
	}

	_, val = getAuthN(getConfig(kerberos, "MSSQLSvc/mssql.domain.com:1433@domain.com@test"))
	if val {
		t.Errorf("Failed to get correct krb5Auth object due to incorrect serverSPN name")
	}

	_, val = getAuthN(getConfig(kerberos, "MSSQLSvc/mssql.domain.com:port@domain.com"))
	if val {
		t.Errorf("Failed to get correct krb5Auth object due to incorrect port")
	}

	_, val = getAuthN(getConfig(kerberos, "MSSQLSvc/mssql.domain.com:port"))
	if val {
		t.Errorf("Failed to get correct krb5Auth object due to incorrect port")
	}
}

func TestInitialBytes(t *testing.T) {
	kerberos := getKerberos()
	krbObj := &krb5Auth{username: "",
		realm:      "domain.com",
		serverSPN:  "MSSQLSvc/mssql.domain.com:1433",
		port:       1433,
		krb5Config: kerberos.Config,
		krbKeytab:  kerberos.Keytab,
		krbCache:   kerberos.Cache,
		state:      0,
	}

	_, err := krbObj.InitialBytes()
	if err == nil {
		t.Errorf("Initial Bytes expected to fail but it didn't")
	}

	krbObj.krbKeytab = nil
	_, err = krbObj.InitialBytes()
	if err == nil {
		t.Errorf("Initial Bytes expected to fail but it didn't")
	}

}

func TestNextBytes(t *testing.T) {
	ans := []byte{}
	kerberos := getKerberos()

	var krbObj auth = &krb5Auth{username: "",
		realm:      "domain.com",
		serverSPN:  "MSSQLSvc/mssql.domain.com:1433",
		port:       1433,
		krb5Config: kerberos.Config,
		krbKeytab:  kerberos.Keytab,
		krbCache:   kerberos.Cache,
		state:      0}

	_, err := krbObj.NextBytes(ans)
	if err == nil {
		t.Errorf("Next Byte expected to fail but it didn't")
	}
}

func TestFree(t *testing.T) {
	kerberos := getKerberos()
	kt := &keytab.Keytab{}
	c := &config.Config{}

	cl := client.NewWithKeytab("Administrator", "DOMAIN.COM", kt, c, client.DisablePAFXFAST(true))

	var krbObj auth = &krb5Auth{username: "",
		realm:      "domain.com",
		serverSPN:  "MSSQLSvc/mssql.domain.com:1433",
		port:       1433,
		krb5Config: kerberos.Config,
		krbKeytab:  kerberos.Keytab,
		krbCache:   kerberos.Cache,
		state:      0,
		krb5Client: cl,
	}
	krbObj.Free()
	cacheEntries := len(kerberos.Cache.GetEntries())
	if cacheEntries != 0 {
		t.Errorf("Client not destroyed")
	}
}

func getKerberos() *msdsn.Kerberos {
	return &msdsn.Kerberos{
		Config: &config.Config{},
		Keytab: &keytab.Keytab{},
		Cache:  &credentials.CCache{},
	}
}

func getConfig(kerberos *msdsn.Kerberos, ServerSPN string) msdsn.Config {
	return msdsn.Config{User: "",
		ServerSPN: ServerSPN,
		Kerberos:  kerberos,
	}
}
