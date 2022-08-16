// +build !windows

package mssql

import (
	"github.com/microsoft/go-mssqldb/integratedauth"
	_ "github.com/microsoft/go-mssqldb/integratedauth/ntlm"
)

func init() {
	integratedauth.DefaultProviderName = "ntlm"
}
