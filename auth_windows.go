package mssql

import "github.com/microsoft/go-mssqldb/msdsn"

func getAuthN(user, password, serverSPN, workstation string, _ map[string]interface{}) (auth auth, authOk bool) {
	auth, authOk = getAuth(user, password, serverSPN, workstation)
	return
}

func getKrbParams(krb msdsn.KerberosConfig) (krbParams map[string]interface{}, err error) {
	return
}
