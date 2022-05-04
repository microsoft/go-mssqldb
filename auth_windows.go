// +build windows

package mssql

import "github.com/microsoft/go-mssqldb/integratedauth/winsspi"

func init() {
	defaultAuthProvider = winsspi.AuthProvider
}
