//go:build !windows
// +build !windows

package mssql

import "github.com/microsoft/go-mssqldb/msdsn"

func getAuthN(p msdsn.Config) (auth auth, authOk bool) {
	if p.Kerberos != nil && p.Kerberos.Config != nil {
		auth, authOk = getKRB5Auth(p.User, p.ServerSPN, p.Kerberos.Config, p.Kerberos.Keytab, p.Kerberos.Cache)
	} else {
		auth, authOk = getAuth(p.User, p.Password, p.ServerSPN, p.Workstation)
	}
	return
}
