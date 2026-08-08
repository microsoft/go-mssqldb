package msdsn

import "testing"

// FuzzParse exercises the connection string parser with arbitrary input.
// Parse must never panic; it may only return a value and an error.
func FuzzParse(f *testing.F) {
	seeds := []string{
		"sqlserver://user:pass@localhost:1433?database=master",
		"server=localhost;user id=sa;password=secret;database=mydb",
		"sqlserver://server.database.windows.net?database=mydb&encrypt=true",
		"odbc:server=localhost;database=master",
		"server=localhost;connection timeout=30;encrypt=true",
		"sqlserver://localhost/SQLEXPRESS?database=mydb",
		"",
		"server=localhost;port=notanumber",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, dsn string) {
		_, _ = Parse(dsn)
	})
}
