// +build windows

package mssql

import "github.com/microsoft/go-mssqldb/auth/winsspi"

func init() {
	defaultAuthProvider = winsspi.AuthProvider
}
