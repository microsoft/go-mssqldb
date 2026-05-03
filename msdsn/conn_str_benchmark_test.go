package msdsn

import "testing"

func BenchmarkParse_URL(b *testing.B) {
	dsn := "sqlserver://user:password@localhost:1433?database=mydb&encrypt=true&TrustServerCertificate=true&connection+timeout=30"
	for i := 0; i < b.N; i++ {
		_, err := Parse(dsn)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParse_URL_Azure(b *testing.B) {
	dsn := "sqlserver://user:password@myserver.database.windows.net:1433?database=mydb&encrypt=true&TrustServerCertificate=false&connection+timeout=30&fedauth=ActiveDirectoryDefault"
	for i := 0; i < b.N; i++ {
		_, err := Parse(dsn)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParse_ADO(b *testing.B) {
	dsn := "server=localhost;user id=sa;password=secret;database=mydb;encrypt=true;TrustServerCertificate=true;connection timeout=30"
	for i := 0; i < b.N; i++ {
		_, err := Parse(dsn)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParse_URL_Minimal(b *testing.B) {
	dsn := "sqlserver://sa:pwd@localhost"
	for i := 0; i < b.N; i++ {
		_, err := Parse(dsn)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParse_URL_ManyParams(b *testing.B) {
	dsn := "sqlserver://user:password@localhost:1433?database=mydb&encrypt=true&TrustServerCertificate=true&connection+timeout=30&dial+timeout=10&keepAlive=30&failoverpartner=mirror&failoverport=1434&packet+size=16384&log=63&app+name=myapp"
	for i := 0; i < b.N; i++ {
		_, err := Parse(dsn)
		if err != nil {
			b.Fatal(err)
		}
	}
}
