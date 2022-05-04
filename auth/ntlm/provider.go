package ntlm

import "github.com/microsoft/go-mssqldb/auth"

// AuthProvider handles NTLM SSPI Windows Authentication
var AuthProvider auth.Provider = auth.ProviderFunc(getAuth)
