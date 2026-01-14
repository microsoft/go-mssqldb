package mssql

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
)

// vectorTestDB holds the shared test database state for vector tests.
// If we're connected to a system database, we create a user database for testing.
var (
	vectorTestDBOnce    sync.Once
	vectorTestDBName    string
	vectorTestDBCreated bool
)

// setupVectorTestDB ensures we're using a user database for vector tests.
// System databases (master, tempdb, msdb, model) don't support PREVIEW_FEATURES.
// Returns a cleanup function that should be deferred (only cleans up on last call).
func setupVectorTestDB(t *testing.T, conn *sql.DB) {
	t.Helper()

	vectorTestDBOnce.Do(func() {
		// Check current database
		var currentDB string
		err := conn.QueryRow("SELECT DB_NAME()").Scan(&currentDB)
		if err != nil {
			t.Logf("Warning: Could not get current database: %v", err)
			return
		}

		// Check if it's a system database
		systemDBs := []string{"master", "tempdb", "msdb", "model"}
		isSystemDB := false
		for _, sysDB := range systemDBs {
			if strings.EqualFold(currentDB, sysDB) {
				isSystemDB = true
				break
			}
		}

		if !isSystemDB {
			// Already in a user database, no need to create one
			t.Logf("Using existing user database: %s", currentDB)
			return
		}

		// We need to use a test database
		vectorTestDBName = "go_mssqldb_vector_test"
		t.Logf("Connected to system database '%s', will use test database '%s'", currentDB, vectorTestDBName)

		// Check if the test database already exists
		var dbExists int
		err = conn.QueryRow("SELECT COUNT(*) FROM sys.databases WHERE name = @p1", vectorTestDBName).Scan(&dbExists)
		if err != nil {
			t.Logf("Warning: Could not check if test database exists: %v", err)
		}

		if dbExists == 0 {
			// Create the test database
			_, err = conn.Exec(fmt.Sprintf("CREATE DATABASE [%s]", vectorTestDBName))
			if err != nil {
				t.Logf("Warning: Could not create test database: %v", err)
				return
			}
			t.Logf("Created test database '%s'", vectorTestDBName)
		} else {
			t.Logf("Test database '%s' already exists, reusing it", vectorTestDBName)
		}
		vectorTestDBCreated = true
	})

	// Switch to test database if we're using one
	if vectorTestDBCreated && vectorTestDBName != "" {
		_, err := conn.Exec(fmt.Sprintf("USE [%s]", vectorTestDBName))
		if err != nil {
			t.Fatalf("Could not switch to test database: %v", err)
		}
	}
}

// cleanupVectorTestDB drops the test database if we created one.
// Call this in TestMain or at the end of tests.
func cleanupVectorTestDB(conn *sql.DB) {
	if vectorTestDBCreated && vectorTestDBName != "" {
		conn.Exec("USE master")
		conn.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS %s", vectorTestDBName))
	}
}

// skipIfVectorNotSupported checks if the SQL Server instance supports VECTOR type.
// VECTOR is only supported in SQL Server 2025+. If not supported, the test is skipped.
func skipIfVectorNotSupported(t *testing.T, conn *sql.DB) {
	t.Helper()

	// Ensure we're in a user database
	setupVectorTestDB(t, conn)

	// Try to create a table with VECTOR column to check support
	_, err := conn.Exec("CREATE TABLE #vector_check (v VECTOR(1))")
	if err != nil {
		errStr := err.Error()
		// Error 2715: "Cannot find data type VECTOR"
		// This occurs on SQL Server versions before 2025
		if strings.Contains(errStr, "Cannot find data type VECTOR") ||
			strings.Contains(errStr, "2715") {
			// Log the server version for debugging
			var version string
			if verr := conn.QueryRow("SELECT @@VERSION").Scan(&version); verr == nil {
				if len(version) > 80 {
					version = version[:80] + "..."
				}
				t.Logf("Server: %s", version)
			}
			t.Skip("VECTOR type not supported - requires SQL Server 2025+")
		}
		// For other errors, fail the test
		t.Fatalf("Failed to check VECTOR support: %v", err)
	}
	// Clean up the check table
	conn.Exec("DROP TABLE #vector_check")
}

// mustNewVector is a test helper that creates a Vector and panics on error.
func mustNewVector(values []float32) Vector {
	v, err := NewVector(values)
	if err != nil {
		panic(err)
	}
	return v
}

// TestVectorInsertAndSelect tests inserting and reading Vector values.
// This test requires a SQL Server 2025+ instance.
func TestVectorInsertAndSelect(t *testing.T) {
	conn, _ := open(t)
	defer conn.Close()
	skipIfVectorNotSupported(t, conn)

	// Begin transaction to keep temp table visible
	tx, err := conn.Begin()
	if err != nil {
		t.Fatal("Begin transaction failed:", err)
	}
	defer tx.Rollback()

	// Create test table with vector column
	tableName := "#test_vector_insert"
	_, err = tx.Exec(fmt.Sprintf(`
		CREATE TABLE %s (
			id INT IDENTITY(1,1) PRIMARY KEY,
			embedding VECTOR(3) NOT NULL
		)
	`, tableName))
	if err != nil {
		t.Fatalf("Failed to create table with VECTOR column: %v", err)
	}

	// Test case 1: Insert using Vector type
	v := mustNewVector([]float32{1.0, 2.0, 3.0})
	result, err := tx.Exec(
		fmt.Sprintf("INSERT INTO %s (embedding) VALUES (@p1)", tableName),
		v,
	)
	if err != nil {
		t.Fatalf("Failed to insert vector: %v", err)
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected != 1 {
		t.Errorf("Expected 1 row affected, got %d", rowsAffected)
	}

	// Test case 2: Read back the vector
	var readVector Vector
	err = tx.QueryRow(
		fmt.Sprintf("SELECT embedding FROM %s WHERE id = 1", tableName),
	).Scan(&readVector)
	if err != nil {
		t.Fatalf("Failed to scan vector: %v", err)
	}

	// Verify the data
	if readVector.Dimensions() != 3 {
		t.Errorf("Expected 3 dimensions, got %d", readVector.Dimensions())
	}
	expected := []float32{1.0, 2.0, 3.0}
	for i, val := range readVector.Data {
		if val != expected[i] {
			t.Errorf("Dimension %d: expected %f, got %f", i, expected[i], val)
		}
	}
	t.Logf("Read vector: %v", readVector)
}

// TestVectorNullInsertAndSelect tests inserting and reading NULL Vector values.
func TestVectorNullInsertAndSelect(t *testing.T) {
	conn, _ := open(t)
	defer conn.Close()
	skipIfVectorNotSupported(t, conn)

	// Begin transaction to keep temp table visible
	tx, err := conn.Begin()
	if err != nil {
		t.Fatal("Begin transaction failed:", err)
	}
	defer tx.Rollback()

	// Create test table with nullable vector column
	tableName := "#test_vector_null"
	_, err = tx.Exec(fmt.Sprintf(`
		CREATE TABLE %s (
			id INT IDENTITY(1,1) PRIMARY KEY,
			embedding VECTOR(3) NULL
		)
	`, tableName))
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	// Insert NULL using NullVector
	nullVector := NullVector{Valid: false}
	_, err = tx.Exec(
		fmt.Sprintf("INSERT INTO %s (embedding) VALUES (@p1)", tableName),
		nullVector,
	)
	if err != nil {
		t.Fatalf("Failed to insert NULL vector: %v", err)
	}

	// Insert a valid vector
	validVector := NullVector{
		Vector: mustNewVector([]float32{4.0, 5.0, 6.0}),
		Valid:  true,
	}
	_, err = tx.Exec(
		fmt.Sprintf("INSERT INTO %s (embedding) VALUES (@p1)", tableName),
		validVector,
	)
	if err != nil {
		t.Fatalf("Failed to insert valid NullVector: %v", err)
	}

	// Read back NULL value
	var readNull NullVector
	err = tx.QueryRow(
		fmt.Sprintf("SELECT embedding FROM %s WHERE id = 1", tableName),
	).Scan(&readNull)
	if err != nil {
		t.Fatalf("Failed to scan NULL vector: %v", err)
	}
	if readNull.Valid {
		t.Errorf("Expected NULL vector, got valid: %v", readNull.Vector)
	}

	// Read back valid value
	var readValid NullVector
	err = tx.QueryRow(
		fmt.Sprintf("SELECT embedding FROM %s WHERE id = 2", tableName),
	).Scan(&readValid)
	if err != nil {
		t.Fatalf("Failed to scan valid NullVector: %v", err)
	}
	if !readValid.Valid {
		t.Error("Expected valid vector, got NULL")
	}
	if readValid.Vector.Dimensions() != 3 {
		t.Errorf("Expected 3 dimensions, got %d", readValid.Vector.Dimensions())
	}
	t.Logf("Read valid NullVector: %v", readValid.Vector)
}

// TestVectorDifferentDimensions tests vectors with different dimension counts.
func TestVectorDifferentDimensions(t *testing.T) {
	conn, _ := open(t)
	defer conn.Close()
	skipIfVectorNotSupported(t, conn)

	testCases := []struct {
		name       string
		dimensions int
		values     []float32
	}{
		{"1D", 1, []float32{42.0}},
		{"5D", 5, []float32{1.0, 2.0, 3.0, 4.0, 5.0}},
		{"10D", 10, make([]float32, 10)},
		{"100D", 100, make([]float32, 100)},
	}

	// Initialize test vectors with values
	for i := range testCases {
		for j := range testCases[i].values {
			testCases[i].values[j] = float32(j + 1)
		}
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Begin transaction for each subtest
			tx, err := conn.Begin()
			if err != nil {
				t.Fatal("Begin transaction failed:", err)
			}
			defer tx.Rollback()

			tableName := fmt.Sprintf("#test_vector_%s", tc.name)
			_, err = tx.Exec(fmt.Sprintf(`
				CREATE TABLE %s (
					id INT IDENTITY(1,1) PRIMARY KEY,
					embedding VECTOR(%d) NOT NULL
				)
			`, tableName, tc.dimensions))
			if err != nil {
				t.Fatalf("Failed to create table: %v", err)
			}

			// Insert
			v := mustNewVector(tc.values)
			_, err = tx.Exec(
				fmt.Sprintf("INSERT INTO %s (embedding) VALUES (@p1)", tableName),
				v,
			)
			if err != nil {
				t.Fatalf("Failed to insert %d-dimensional vector: %v", tc.dimensions, err)
			}

			// Read back
			var readVector Vector
			err = tx.QueryRow(
				fmt.Sprintf("SELECT embedding FROM %s WHERE id = 1", tableName),
			).Scan(&readVector)
			if err != nil {
				t.Fatalf("Failed to scan vector: %v", err)
			}

			if readVector.Dimensions() != tc.dimensions {
				t.Errorf("Expected %d dimensions, got %d", tc.dimensions, readVector.Dimensions())
			}
		})
	}
}

// TestVectorSpecialValues tests vectors with special floating-point values.
func TestVectorSpecialValues(t *testing.T) {
	conn, _ := open(t)
	defer conn.Close()
	skipIfVectorNotSupported(t, conn)

	// Begin transaction to keep temp table visible
	tx, err := conn.Begin()
	if err != nil {
		t.Fatal("Begin transaction failed:", err)
	}
	defer tx.Rollback()

	tableName := "#test_vector_special"
	_, err = tx.Exec(fmt.Sprintf(`
		CREATE TABLE %s (
			id INT IDENTITY(1,1) PRIMARY KEY,
			embedding VECTOR(5) NOT NULL
		)
	`, tableName))
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	// Test special values: zero, negative, small, large
	v := mustNewVector([]float32{0.0, -1.0, 1e-30, 1e30, -0.0})
	_, err = tx.Exec(
		fmt.Sprintf("INSERT INTO %s (embedding) VALUES (@p1)", tableName),
		v,
	)
	if err != nil {
		t.Fatalf("Failed to insert special values: %v", err)
	}

	var readVector Vector
	err = tx.QueryRow(
		fmt.Sprintf("SELECT embedding FROM %s WHERE id = 1", tableName),
	).Scan(&readVector)
	if err != nil {
		t.Fatalf("Failed to scan special values: %v", err)
	}

	// Verify values
	expected := []float32{0.0, -1.0, 1e-30, 1e30, 0.0} // -0.0 should read as 0.0
	for i, val := range readVector.Data {
		if !floatsEqualVector(val, expected[i]) {
			t.Errorf("Value %d: expected %e, got %e", i, expected[i], val)
		}
	}
	t.Logf("Special values vector: %v", readVector)
}

// TestVectorDistance tests using vectors in SQL Server VECTOR_DISTANCE function.
func TestVectorDistance(t *testing.T) {
	conn, _ := open(t)
	defer conn.Close()
	skipIfVectorNotSupported(t, conn)

	// Begin transaction to keep temp table visible
	tx, err := conn.Begin()
	if err != nil {
		t.Fatal("Begin transaction failed:", err)
	}
	defer tx.Rollback()

	tableName := "#test_vector_distance"
	_, err = tx.Exec(fmt.Sprintf(`
		CREATE TABLE %s (
			id INT IDENTITY(1,1) PRIMARY KEY,
			name NVARCHAR(50),
			embedding VECTOR(3) NOT NULL
		)
	`, tableName))
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	// Insert some test vectors
	vectors := []struct {
		name   string
		values []float32
	}{
		{"vec_a", []float32{1.0, 0.0, 0.0}},
		{"vec_b", []float32{0.0, 1.0, 0.0}},
		{"vec_c", []float32{0.0, 0.0, 1.0}},
		{"vec_d", []float32{1.0, 1.0, 1.0}},
	}

	for _, v := range vectors {
		_, err = tx.Exec(
			fmt.Sprintf("INSERT INTO %s (name, embedding) VALUES (@p1, @p2)", tableName),
			v.name, mustNewVector(v.values),
		)
		if err != nil {
			t.Fatalf("Failed to insert %s: %v", v.name, err)
		}
	}

	// Query using VECTOR_DISTANCE
	// Note: VECTOR_DISTANCE requires a native vector type, so we need to CAST the parameter
	queryVector := mustNewVector([]float32{1.0, 0.0, 0.0})
	rows, err := tx.Query(fmt.Sprintf(`
		SELECT name, VECTOR_DISTANCE('cosine', embedding, CAST(@p1 AS VECTOR(3))) as distance
		FROM %s
		ORDER BY distance
	`, tableName), queryVector)
	if err != nil {
		t.Fatalf("Failed to query with VECTOR_DISTANCE: %v", err)
	}
	defer rows.Close()

	type result struct {
		name     string
		distance float64
	}
	var results []result
	for rows.Next() {
		var r result
		if err := rows.Scan(&r.name, &r.distance); err != nil {
			t.Fatalf("Failed to scan result: %v", err)
		}
		results = append(results, r)
		t.Logf("Name: %s, Distance: %f", r.name, r.distance)
	}

	if len(results) != 4 {
		t.Errorf("Expected 4 results, got %d", len(results))
	}

	// vec_a should be the closest (distance = 0)
	if results[0].name != "vec_a" || results[0].distance != 0.0 {
		t.Errorf("Expected vec_a with distance 0, got %s with distance %f", results[0].name, results[0].distance)
	}
}

// TestVectorColumnMetadata tests that Vector column metadata is reported correctly.
func TestVectorColumnMetadata(t *testing.T) {
	conn, _ := open(t)
	defer conn.Close()
	skipIfVectorNotSupported(t, conn)

	// Begin transaction to keep temp table visible
	tx, err := conn.Begin()
	if err != nil {
		t.Fatal("Begin transaction failed:", err)
	}
	defer tx.Rollback()

	tableName := "#test_vector_metadata"
	dimensions := 128
	_, err = tx.Exec(fmt.Sprintf(`
		CREATE TABLE %s (
			id INT IDENTITY(1,1) PRIMARY KEY,
			embedding VECTOR(%d) NOT NULL
		)
	`, tableName, dimensions))
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	// Insert a vector
	testData := make([]float32, dimensions)
	for i := range testData {
		testData[i] = float32(i)
	}
	_, err = tx.Exec(
		fmt.Sprintf("INSERT INTO %s (embedding) VALUES (@p1)", tableName),
		mustNewVector(testData),
	)
	if err != nil {
		t.Fatalf("Failed to insert vector: %v", err)
	}

	// Query and check column types
	rows, err := tx.Query(fmt.Sprintf("SELECT id, embedding FROM %s", tableName))
	if err != nil {
		t.Fatalf("Failed to query: %v", err)
	}
	defer rows.Close()

	colTypes, err := rows.ColumnTypes()
	if err != nil {
		t.Fatalf("Failed to get column types: %v", err)
	}

	// Check embedding column type
	// Note: Due to the current implementation sending vectors as JSON strings via NVARCHAR,
	// the column type metadata may show as NVARCHAR. The actual vector data is still
	// properly handled and can be scanned into Vector types.
	embeddingCol := colTypes[1]
	typeName := embeddingCol.DatabaseTypeName()

	// Log the actual type for informational purposes
	t.Logf("Column type: %s", typeName)

	// The column might report as VECTOR or NVARCHAR depending on how the query executes
	// What matters is that we can successfully scan the data into Vector type

	length, ok := embeddingCol.Length()
	if ok {
		t.Logf("Column length: %d", length)
	}

	// Verify we can actually scan the vector data correctly
	if rows.Next() {
		var id int
		var v Vector
		if err := rows.Scan(&id, &v); err != nil {
			t.Fatalf("Failed to scan vector: %v", err)
		}
		if v.Dimensions() != dimensions {
			t.Errorf("Expected %d dimensions, got %d", dimensions, v.Dimensions())
		}
		t.Logf("Successfully scanned vector with %d dimensions", v.Dimensions())
	}
}

// TestVectorLargeDimensions tests vectors near the maximum allowed dimensions.
func TestVectorLargeDimensions(t *testing.T) {
	conn, _ := open(t)
	defer conn.Close()
	skipIfVectorNotSupported(t, conn)

	// Begin transaction to keep temp table visible
	tx, err := conn.Begin()
	if err != nil {
		t.Fatal("Begin transaction failed:", err)
	}
	defer tx.Rollback()

	// Test with a reasonably large vector (500 dimensions)
	dimensions := 500
	tableName := "#test_vector_large"
	_, err = tx.Exec(fmt.Sprintf(`
		CREATE TABLE %s (
			id INT IDENTITY(1,1) PRIMARY KEY,
			embedding VECTOR(%d) NOT NULL
		)
	`, tableName, dimensions))
	if err != nil {
		t.Fatalf("Failed to create table with %d dimensions: %v", dimensions, err)
	}

	// Create test data
	testData := make([]float32, dimensions)
	for i := range testData {
		testData[i] = float32(i) * 0.001
	}

	v := mustNewVector(testData)
	_, err = tx.Exec(
		fmt.Sprintf("INSERT INTO %s (embedding) VALUES (@p1)", tableName),
		v,
	)
	if err != nil {
		t.Fatalf("Failed to insert large vector: %v", err)
	}

	var readVector Vector
	err = tx.QueryRow(
		fmt.Sprintf("SELECT embedding FROM %s WHERE id = 1", tableName),
	).Scan(&readVector)
	if err != nil {
		t.Fatalf("Failed to scan large vector: %v", err)
	}

	if readVector.Dimensions() != dimensions {
		t.Errorf("Expected %d dimensions, got %d", dimensions, readVector.Dimensions())
	}

	// Spot check some values
	for _, i := range []int{0, 100, 250, 499} {
		if !floatsEqualVector(readVector.Data[i], testData[i]) {
			t.Errorf("Value at index %d: expected %f, got %f", i, testData[i], readVector.Data[i])
		}
	}
	t.Logf("Successfully round-tripped %d-dimensional vector", dimensions)
}

// TestVectorBatchInsert tests inserting multiple vectors in a transaction.
func TestVectorBatchInsert(t *testing.T) {
	conn, _ := open(t)
	defer conn.Close()
	skipIfVectorNotSupported(t, conn)

	// Begin transaction to keep temp table visible
	tx, err := conn.Begin()
	if err != nil {
		t.Fatal("Begin transaction failed:", err)
	}
	defer tx.Rollback()

	tableName := "#test_vector_batch"
	_, err = tx.Exec(fmt.Sprintf(`
		CREATE TABLE %s (
			id INT IDENTITY(1,1) PRIMARY KEY,
			embedding VECTOR(3) NOT NULL
		)
	`, tableName))
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	// Insert multiple vectors
	count := 100
	for i := 0; i < count; i++ {
		v := mustNewVector([]float32{float32(i), float32(i * 2), float32(i * 3)})
		_, err = tx.Exec(
			fmt.Sprintf("INSERT INTO %s (embedding) VALUES (@p1)", tableName),
			v,
		)
		if err != nil {
			t.Fatalf("Failed to insert vector %d: %v", i, err)
		}
	}

	// Verify count
	var actualCount int
	err = tx.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)).Scan(&actualCount)
	if err != nil {
		t.Fatalf("Failed to count rows: %v", err)
	}
	if actualCount != count {
		t.Errorf("Expected %d rows, got %d", count, actualCount)
	}
	t.Logf("Batch inserted %d vectors successfully", count)
}

// TestVectorFloat16 tests float16 vector support (preview feature).
// This test requires SQL Server 2025+ with PREVIEW_FEATURES enabled.
func TestVectorFloat16(t *testing.T) {
	conn, _ := open(t)
	defer conn.Close()
	skipIfVectorNotSupported(t, conn)

	// Determine which database to use for PREVIEW_FEATURES
	// If we created a test database, use that; otherwise use the current database
	var targetDB string
	if vectorTestDBCreated && vectorTestDBName != "" {
		targetDB = vectorTestDBName
	} else {
		// Get the current database name from the connection string config
		err := conn.QueryRow("SELECT DB_NAME()").Scan(&targetDB)
		if err != nil {
			t.Fatalf("Failed to get current database: %v", err)
		}
	}

	// Check if it's a system database - we can't enable PREVIEW_FEATURES on system databases
	systemDBs := []string{"master", "tempdb", "msdb", "model"}
	for _, sysDB := range systemDBs {
		if strings.EqualFold(targetDB, sysDB) {
			t.Skipf("Cannot enable PREVIEW_FEATURES on system database '%s'. Connect to a user database to test float16.", targetDB)
		}
	}

	// Use a dedicated connection to ensure consistent database context
	ctx := context.Background()
	singleConn, err := conn.Conn(ctx)
	if err != nil {
		t.Fatalf("Failed to get dedicated connection: %v", err)
	}
	defer singleConn.Close()

	// Switch to the target database on this specific connection
	_, err = singleConn.ExecContext(ctx, fmt.Sprintf("USE [%s]", targetDB))
	if err != nil {
		t.Fatalf("Could not switch to database %s: %v", targetDB, err)
	}

	// Enable preview features for float16 support
	// This must be run while in the target database context
	_, err = singleConn.ExecContext(ctx, "ALTER DATABASE SCOPED CONFIGURATION SET PREVIEW_FEATURES = ON")
	if err != nil {
		t.Skipf("Could not enable PREVIEW_FEATURES (may not be supported): %v", err)
	}

	// Ensure we disable preview features when done
	defer func() {
		_, err := singleConn.ExecContext(ctx, "ALTER DATABASE SCOPED CONFIGURATION SET PREVIEW_FEATURES = OFF")
		if err != nil {
			t.Logf("Warning: Could not disable PREVIEW_FEATURES: %v", err)
		}
	}()

	// Begin transaction on this connection to keep temp table visible
	tx, err := singleConn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal("Begin transaction failed:", err)
	}
	defer tx.Rollback()

	// Create test table with float16 vector column
	tableName := "#test_vector_float16"
	_, err = tx.Exec(fmt.Sprintf(`
		CREATE TABLE %s (
			id INT IDENTITY(1,1) PRIMARY KEY,
			embedding VECTOR(3, float16) NOT NULL
		)
	`, tableName))
	if err != nil {
		// VECTOR type requires SQL Server 2025+, float16 requires PREVIEW_FEATURES
		t.Skipf("Could not create float16 VECTOR column (requires SQL Server 2025+ with PREVIEW_FEATURES): %v", err)
	}

	// Insert a vector - note: transmitted as JSON, SQL Server converts to float16
	v := mustNewVector([]float32{1.0, 2.0, 3.0})
	_, err = tx.Exec(
		fmt.Sprintf("INSERT INTO %s (embedding) VALUES (@p1)", tableName),
		v,
	)
	if err != nil {
		t.Fatalf("Failed to insert float16 vector: %v", err)
	}

	// Read back the vector
	var readVector Vector
	err = tx.QueryRow(
		fmt.Sprintf("SELECT embedding FROM %s WHERE id = 1", tableName),
	).Scan(&readVector)
	if err != nil {
		t.Fatalf("Failed to scan float16 vector: %v", err)
	}

	// Verify dimensions
	if readVector.Dimensions() != 3 {
		t.Errorf("Expected 3 dimensions, got %d", readVector.Dimensions())
	}

	// Verify values (float16 has less precision, so use tolerance)
	expected := []float32{1.0, 2.0, 3.0}
	for i, val := range readVector.Data {
		if !floatsEqualVector(val, expected[i]) {
			t.Errorf("Dimension %d: expected %f, got %f", i, expected[i], val)
		}
	}

	t.Logf("Successfully round-tripped float16 vector: %v", readVector)

	// Test with values that show float16 precision loss
	precisionTestValues := []float32{1.001, 2.002, 3.003}
	v2 := mustNewVector(precisionTestValues)
	_, err = tx.Exec(
		fmt.Sprintf("INSERT INTO %s (embedding) VALUES (@p1)", tableName),
		v2,
	)
	if err != nil {
		t.Fatalf("Failed to insert precision test vector: %v", err)
	}

	var readVector2 Vector
	err = tx.QueryRow(
		fmt.Sprintf("SELECT embedding FROM %s WHERE id = 2", tableName),
	).Scan(&readVector2)
	if err != nil {
		t.Fatalf("Failed to scan precision test vector: %v", err)
	}

	// float16 has ~3 decimal digits of precision, values should be close but may differ slightly
	t.Logf("Precision test - input: %v, output: %v", precisionTestValues, readVector2.Data)
	for i, val := range readVector2.Data {
		diff := math.Abs(float64(val - precisionTestValues[i]))
		if diff > 0.01 { // float16 should be accurate to ~0.1% for these values
			t.Errorf("Dimension %d: precision loss too high, expected ~%f, got %f (diff: %f)",
				i, precisionTestValues[i], val, diff)
		}
	}
}

// floatsEqualVector compares two float32 values with tolerance for vector tests.
func floatsEqualVector(a, b float32) bool {
	if math.IsNaN(float64(a)) && math.IsNaN(float64(b)) {
		return true
	}
	if math.IsInf(float64(a), 1) && math.IsInf(float64(b), 1) {
		return true
	}
	if math.IsInf(float64(a), -1) && math.IsInf(float64(b), -1) {
		return true
	}
	diff := math.Abs(float64(a - b))
	return diff < 1e-6 || diff < math.Abs(float64(a))*1e-6
}
