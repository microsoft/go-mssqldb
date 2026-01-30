package mssql_test

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/url"

	_ "github.com/microsoft/go-mssqldb"
)

// Example_basicConnection demonstrates connecting to SQL Server using the sqlserver driver.
func Example_basicConnection() {
	// Connection string using URL format
	connStr := "sqlserver://sa:YourPassword123@localhost:1433?database=master"

	db, err := sql.Open("sqlserver", connStr)
	if err != nil {
		log.Fatal("Error opening connection:", err)
	}
	defer db.Close()

	// Verify connection
	if err := db.Ping(); err != nil {
		log.Fatal("Error pinging database:", err)
	}

	fmt.Println("Connected successfully!")
}

// Example_queryWithNamedParameters demonstrates using named parameters in queries.
func Example_queryWithNamedParameters() {
	db, _ := sql.Open("sqlserver", "sqlserver://sa:password@localhost:1433?database=master")
	defer db.Close()

	ctx := context.Background()

	// Use @ParameterName syntax for named parameters
	rows, err := db.QueryContext(ctx,
		"SELECT id, name FROM users WHERE active = @Active AND department = @Dept",
		sql.Named("Active", true),
		sql.Named("Dept", "Engineering"),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int
		var name string
		rows.Scan(&id, &name)
		fmt.Printf("ID: %d, Name: %s\n", id, name)
	}
}

// Example_queryWithPositionalParameters demonstrates using positional parameters.
func Example_queryWithPositionalParameters() {
	db, _ := sql.Open("sqlserver", "sqlserver://sa:password@localhost:1433?database=master")
	defer db.Close()

	// Use @p1, @p2, etc. for positional parameters
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM users WHERE active = @p1", true).Scan(&count)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Active users: %d\n", count)
}

// Example_buildConnectionString demonstrates programmatically building a connection string.
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
	fmt.Println("Connection string:", connStr)

	db, err := sql.Open("sqlserver", connStr)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
}

// Example_storedProcedureWithOutput demonstrates calling a stored procedure with output parameters.
func Example_storedProcedureWithOutput() {
	db, _ := sql.Open("sqlserver", "sqlserver://sa:password@localhost:1433?database=master")
	defer db.Close()

	ctx := context.Background()

	var outputValue string
	_, err := db.ExecContext(ctx, "sp_MyProcedure",
		sql.Named("InputParam", "test value"),
		sql.Named("OutputParam", sql.Out{Dest: &outputValue}),
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Output value: %s\n", outputValue)
}
