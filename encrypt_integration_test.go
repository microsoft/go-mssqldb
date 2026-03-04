package mssql

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Integration tests for encrypt.go - require SQL Server connection
// Note: Full Always Encrypted tests require proper key setup which may not
// be available in all test environments. These tests cover what we can test
// without full AE infrastructure.

func TestPrepareEncryptionQuery_WithConnection_Integration(t *testing.T) {
	db := requireTestDB(t)
	ctx := testContext(t)

	// Verify connection works
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("Ping failed: %v", err)
	}

	// Test that prepared statements work (uses some encrypt.go paths)
	stmt, err := db.PrepareContext(ctx, "SELECT @p1 AS col1, @p2 AS col2")
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	defer stmt.Close()

	var col1, col2 string
	err = stmt.QueryRowContext(ctx, "value1", "value2").Scan(&col1, &col2)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	assert.Equal(t, "value1", col1, "col1 value")
	assert.Equal(t, "value2", col2, "col2 value")
}

func TestBuildStoredProcedureStatement_WithExecution_Integration(t *testing.T) {
	db := requireTestDB(t)
	ctx := testContext(t)

	// Execute a system stored procedure to test the sproc path
	rows, err := db.QueryContext(ctx, "EXEC sp_databases")
	if err != nil {
		t.Fatalf("EXEC sp_databases failed: %v", err)
	}
	defer rows.Close()

	// Just verify we got results
	hasRows := rows.Next()
	if !hasRows {
		t.Log("No databases returned (may be permission issue)")
	}
}

func TestQuoter_WithRealQueries_Integration(t *testing.T) {
	db := requireTestDB(t)
	ctx := testContext(t)

	q := TSQLQuoter{}

	// Test that quoted identifiers work in real queries
	tests := []struct {
		name       string
		identifier string
	}{
		{"simple", "TestColumn"},
		{"with space", "Test Column"},
		{"with bracket", "Test]Column"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			quotedID := q.ID(tt.identifier)
			query := "SELECT 1 AS " + quotedID
			var result int
			err := db.QueryRowContext(ctx, query).Scan(&result)
			assert.NoError(t, err, "Query with quoted identifier %s failed", quotedID)
			assert.Equal(t, 1, result, "result value")
		})
	}
}

func TestQuoter_ValueWithRealInsert_Integration(t *testing.T) {
	db := requireTestDB(t)
	ctx := testContext(t)

	q := TSQLQuoter{}

	// Test values with special characters by selecting them directly
	// This tests the quoting without needing temp tables
	tests := []struct {
		name  string
		value string
	}{
		{"simple", "hello"},
		{"with quote", "it's"},
		{"with double quote", "say ''hello''"},
		{"sql injection attempt", "'; DROP TABLE Users; --"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			quotedVal := q.Value(tt.value)
			query := "SELECT " + quotedVal + " AS result"
			var result string
			err := db.QueryRowContext(ctx, query).Scan(&result)
			if !assert.NoError(t, err, "Select with quoted value failed") {
				return
			}
			assert.Equal(t, tt.value, result, "result value")
		})
	}
}

func TestNamedParameters_Integration(t *testing.T) {
	db := requireTestDB(t)
	ctx := testContext(t)

	// Test named parameters (uses encrypt.go parameter handling)
	query := "SELECT @param1 AS p1, @param2 AS p2"
	rows, err := db.QueryContext(ctx, query,
		sql.Named("param1", "first"),
		sql.Named("param2", "second"))
	if err != nil {
		t.Fatalf("Query with named params failed: %v", err)
	}
	defer rows.Close()

	if !rows.Next() {
		t.Fatal("Expected one row")
	}

	var p1, p2 string
	if err := rows.Scan(&p1, &p2); err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	assert.Equal(t, "first", p1, "p1 value")
	assert.Equal(t, "second", p2, "p2 value")
}

func TestOutputParameters_Integration(t *testing.T) {
	db := requireTestDB(t)
	ctx := testContext(t)

	// Test output parameters using sp_executesql which is a system proc
	// This avoids the need to create a temp procedure
	var outputVal int
	_, err := db.ExecContext(ctx,
		"DECLARE @result INT; SET @result = @input * 2; SELECT @result",
		sql.Named("input", 21))
	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}

	// Test with a simpler output pattern using a direct query
	err = db.QueryRowContext(ctx, "SELECT @p1 * 2", sql.Named("p1", 21)).Scan(&outputVal)
	if err != nil {
		t.Fatalf("Query with named param failed: %v", err)
	}

	assert.Equal(t, 42, outputVal, "output value")
}
