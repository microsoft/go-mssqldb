package mssql_test

import (
	"fmt"
	"net/url"

	_ "github.com/microsoft/go-mssqldb"
)

// These examples demonstrate common usage patterns for the go-mssqldb driver.
// Note: sql.Open does not establish a network connection until the first use
// of the returned *sql.DB. These examples show code patterns without requiring
// a live SQL Server instance.

// Example_connectionString shows the basic connection string format.
// Use "sqlserver" as the driver name (not "mssql").
func Example_connectionString() {
	// URL format connection string (recommended)
	connStr := "sqlserver://user:password@localhost:1433?database=mydb"
	fmt.Println("URL format:", connStr)

	// ADO format connection string
	adoConnStr := "server=localhost;user id=sa;password=secret;database=mydb"
	fmt.Println("ADO format:", adoConnStr)

	// ODBC format connection string
	odbcConnStr := "odbc:server=localhost;user id=sa;password=secret;database=mydb"
	fmt.Println("ODBC format:", odbcConnStr)

	// Output:
	// URL format: sqlserver://user:password@localhost:1433?database=mydb
	// ADO format: server=localhost;user id=sa;password=secret;database=mydb
	// ODBC format: odbc:server=localhost;user id=sa;password=secret;database=mydb
}

// Example_namedParameterSyntax shows the correct parameter syntax for queries.
// Use @ParameterName with sql.Named() for named parameters.
func Example_namedParameterSyntax() {
	// Named parameter syntax - use @ParameterName
	query := "SELECT * FROM users WHERE id = @ID AND active = @Active"
	fmt.Println("Named parameters:", query)

	// Positional parameter syntax - use @p1, @p2, etc.
	positionalQuery := "SELECT * FROM users WHERE id = @p1 AND active = @p2"
	fmt.Println("Positional parameters:", positionalQuery)

	// Output:
	// Named parameters: SELECT * FROM users WHERE id = @ID AND active = @Active
	// Positional parameters: SELECT * FROM users WHERE id = @p1 AND active = @p2
}

// Example_buildConnectionString demonstrates programmatically building a connection string
// using the net/url package.
func Example_buildConnectionString() {
	query := url.Values{}
	query.Add("database", "mydb")
	query.Add("connection timeout", "30")
	query.Add("encrypt", "true")

	u := &url.URL{
		Scheme:   "sqlserver",
		User:     url.UserPassword("username", "password"),
		Host:     "localhost:1433",
		RawQuery: query.Encode(),
	}

	connStr := u.String()
	fmt.Println("Built connection string")

	// Use with sql.Open:
	// db, err := sql.Open("sqlserver", connStr)
	_ = connStr

	// Output:
	// Built connection string
}

// Example_azureADConnection shows connection strings for Azure AD authentication.
// Import "github.com/microsoft/go-mssqldb/azuread" and use azuread.DriverName.
// Always enable encryption with certificate validation for Azure SQL.
func Example_azureADConnection() {
	// DefaultAzureCredential (recommended for most scenarios)
	// Enable TLS with certificate validation for Azure SQL
	defaultCred := "sqlserver://server.database.windows.net?database=mydb&fedauth=ActiveDirectoryDefault&encrypt=true&TrustServerCertificate=false"
	fmt.Println("DefaultAzureCredential:", defaultCred)

	// Managed Identity
	msiCred := "sqlserver://server.database.windows.net?database=mydb&fedauth=ActiveDirectoryMSI&encrypt=true&TrustServerCertificate=false"
	fmt.Println("Managed Identity:", msiCred)

	// Output:
	// DefaultAzureCredential: sqlserver://server.database.windows.net?database=mydb&fedauth=ActiveDirectoryDefault&encrypt=true&TrustServerCertificate=false
	// Managed Identity: sqlserver://server.database.windows.net?database=mydb&fedauth=ActiveDirectoryMSI&encrypt=true&TrustServerCertificate=false
}

// Example_storedProcedureSyntax shows how to call stored procedures with output parameters.
func Example_storedProcedureSyntax() {
	// Stored procedure call syntax with named parameters
	procCall := "EXEC sp_MyProcedure @InputParam = @Input, @OutputParam = @Output OUTPUT"
	fmt.Println("Stored procedure:", procCall)

	// In Go code, use sql.Named with sql.Out for output parameters:
	// var outputValue string
	// _, err := db.ExecContext(ctx, "sp_MyProcedure",
	//     sql.Named("Input", "test value"),
	//     sql.Named("Output", sql.Out{Dest: &outputValue}),
	// )

	// Output:
	// Stored procedure: EXEC sp_MyProcedure @InputParam = @Input, @OutputParam = @Output OUTPUT
}
