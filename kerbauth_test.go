//go:build !windows && go1.13
// +build !windows,go1.13

package mssql

import (
	"reflect"
	"testing"

	"github.com/jcmturner/gokrb5/v8/client"
	"github.com/jcmturner/gokrb5/v8/config"
	"github.com/jcmturner/gokrb5/v8/credentials"
	"github.com/jcmturner/gokrb5/v8/keytab"
)

func TestGetAuth(t *testing.T) {
	kerberos := getKerberos()

	got, _ := getAuthN("", "", "MSSQLSvc/mssql.domain.com:1433", "", kerberos)
	kt := &krb5Auth{username: "",
		realm:      "domain.com",
		serverSPN:  "MSSQLSvc/mssql.domain.com:1433",
		port:       1433,
		krb5Config: kerberos["Config"].(*config.Config),
		krbKeytab:  kerberos["Keytab"].(*keytab.Keytab),
		krbCache:   kerberos["Cache"].(*credentials.CCache),
		state:      0}

	res := reflect.DeepEqual(got, kt)
	if !res {
		t.Errorf("Failed to get correct krb5Auth object\nExpected:%v\nRecieved:%v", kt, got)
	}

	got, _ = getAuthN("", "", "MSSQLSvc/mssql.domain.com:1433", "", kerberos)
	kt = &krb5Auth{username: "",
		realm:      "domain.com",
		serverSPN:  "MSSQLSvc/mssql.domain.com:1433",
		port:       1433,
		krb5Config: kerberos["Config"].(*config.Config),
		krbKeytab:  kerberos["Keytab"].(*keytab.Keytab),
		krbCache:   kerberos["Cache"].(*credentials.CCache),
		state:      0}

	res = reflect.DeepEqual(got, kt)
	if !res {
		t.Errorf("Failed to get correct krb5Auth object\nExpected:%v\nRecieved:%v", kt, got)
	}

	_, val := getAuthN("", "", "MSSQLSvc/mssql.domain.com", "", kerberos)
	if val {
		t.Errorf("Failed to get correct krb5Auth object: no port defined")
	}

	got, _ = getAuthN("", "", "MSSQLSvc/mssql.domain.com:1433@DOMAIN.COM", "", kerberos)
	kt = &krb5Auth{username: "",
		realm:      "DOMAIN.COM",
		serverSPN:  "MSSQLSvc/mssql.domain.com:1433",
		port:       1433,
		krb5Config: kerberos["Config"].(*config.Config),
		krbKeytab:  kerberos["Keytab"].(*keytab.Keytab),
		krbCache:   kerberos["Cache"].(*credentials.CCache),
		state:      0}

	res = reflect.DeepEqual(got, kt)
	if !res {
		t.Errorf("Failed to get correct krb5Auth object\nExpected:%v\nRecieved:%v", kt, got)
	}

	_, val = getAuthN("", "", "MSSQLSvc/mssql.domain.com:1433@domain.com@test", "", kerberos)
	if val {
		t.Errorf("Failed to get correct krb5Auth object due to incorrect serverSPN name")
	}

	_, val = getAuthN("", "", "MSSQLSvc/mssql.domain.com:port@domain.com", "", kerberos)
	if val {
		t.Errorf("Failed to get correct krb5Auth object due to incorrect port")
	}

	_, val = getAuthN("", "", "MSSQLSvc/mssql.domain.com:port", "", kerberos)
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
		krb5Config: kerberos["Config"].(*config.Config),
		krbKeytab:  kerberos["Keytab"].(*keytab.Keytab),
		krbCache:   kerberos["Cache"].(*credentials.CCache),
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
		krb5Config: kerberos["Config"].(*config.Config),
		krbKeytab:  kerberos["Keytab"].(*keytab.Keytab),
		krbCache:   kerberos["Cache"].(*credentials.CCache),
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
		krb5Config: kerberos["Config"].(*config.Config),
		krbKeytab:  kerberos["Keytab"].(*keytab.Keytab),
		krbCache:   kerberos["Cache"].(*credentials.CCache),
		state:      0,
		krb5Client: cl,
	}
	krbObj.Free()
	cacheEntries := len(kerberos["Cache"].(*credentials.CCache).GetEntries())
	if cacheEntries != 0 {
		t.Errorf("Client not destroyed")
	}
}

func getKerberos() (krbParams map[string]interface{}) {
	krbParams = map[string]interface{}{
		"Config": &config.Config{},
		"Keytab": &keytab.Keytab{},
		"Cache":  &credentials.CCache{},
	}
	return
}
