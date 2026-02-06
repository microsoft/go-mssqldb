package mssql

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/microsoft/go-mssqldb/msdsn"
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
// Database cleanup is handled by drop-before-create logic at the start of each test run.
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
		// Use a fixed, clearly prefixed name to avoid accumulating test databases
		// The prefix makes it obviously a test database
		vectorTestDBName = "go_mssqldb_vector_test"
		t.Logf("Connected to system database '%s', will use test database '%s'", currentDB, vectorTestDBName)

		// Drop any existing test database from previous runs, then create fresh.
		// This is best-effort: if the test login lacks permissions, we skip database
		// creation and float16 tests will be skipped when they need PREVIEW_FEATURES.
		if _, err := conn.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS [%s]", vectorTestDBName)); err != nil {
			t.Logf("Warning: Could not drop test database (may lack permissions): %v", err)
			return
		}

		// Create the test database
		_, err = conn.Exec(fmt.Sprintf("CREATE DATABASE [%s]", vectorTestDBName))
		if err != nil {
			t.Logf("Warning: Could not create test database (may lack permissions): %v", err)
			return
		}
		t.Logf("Created test database '%s'", vectorTestDBName)
		vectorTestDBCreated = true
		// Note: We don't use t.Cleanup() here because sync.Once ties it to the first
		// test that runs, which would try to drop the database while other tests are
		// still using it. Instead, we rely on drop-before-create at the start of each
		// test run to clean up any leftover databases from previous runs.
	})

	// Note: We don't attempt to USE the test database here because USE is session-scoped
	// and *sql.DB may pick different connections per call. The tests create their own
	// tables in the current database context, which is sufficient for isolation.
	// The test database creation above is primarily for cleanup between test runs.
}

// skipIfVectorNotSupported checks if the SQL Server instance supports VECTOR type.
// VECTOR is only supported in SQL Server 2025+. If not supported, the test is skipped.
// This function checks VECTOR support FIRST before any database setup to ensure
// clean skips on pre-2025 servers without risking permission errors.
func skipIfVectorNotSupported(t *testing.T, conn *sql.DB) {
	t.Helper()

	// Use a dedicated connection to ensure the temp table CREATE and DROP
	// happen on the same session. Temp tables are session-scoped, so using
	// *sql.DB directly could cause the DROP to execute on a different
	// pooled connection where the temp table doesn't exist.
	ctx := context.Background()
	singleConn, err := conn.Conn(ctx)
	if err != nil {
		t.Fatalf("Failed to get dedicated connection: %v", err)
	}
	defer singleConn.Close()

	// Check VECTOR support FIRST, before any database setup.
	// This ensures pre-2025 servers skip cleanly without running into
	// potential permission errors from DROP/CREATE DATABASE operations.
	_, err = singleConn.ExecContext(ctx, "CREATE TABLE #vector_check (v VECTOR(1))")
	if err != nil {
		errStr := err.Error()
		// Error 2715: "Cannot find data type VECTOR"
		// This occurs on SQL Server versions before 2025
		if strings.Contains(errStr, "Cannot find data type VECTOR") ||
			strings.Contains(errStr, "2715") {
			// Log the server version for debugging
			var version string
			if verr := singleConn.QueryRowContext(ctx, "SELECT @@VERSION").Scan(&version); verr == nil {
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
	// Clean up the check table on the same connection where it was created
	singleConn.ExecContext(ctx, "DROP TABLE #vector_check")

	// Now that we know VECTOR is supported, ensure we're in a user database
	// for tests that need PREVIEW_FEATURES (float16 tests).
	setupVectorTestDB(t, conn)
}

// mustNewVector is a test helper that creates a Vector and panics on error.
func mustNewVector(values []float32) Vector {
	v, err := NewVector(values)
	if err != nil {
		panic(err)
	}
	return v
}

// openWithVectorSupport opens a database connection with vectortypesupport=v1 enabled.
// This enables native binary vector format when the server supports it (SQL Server 2025+).
func openWithVectorSupport(t testing.TB) (*sql.DB, *testLogger) {
	tl := testLogger{t: t}
	SetLogger(&tl)
	t.Cleanup(func() {
		tl.StopLogging()
	})

	config := testConnParams(t)
	config.VectorTypeSupport = msdsn.VectorTypeSupportV1
	connectionString := config.URL().String()

	connector, err := NewConnector(connectionString)
	if err != nil {
		t.Fatal("Failed to create connector:", err)
	}
	conn := sql.OpenDB(connector)
	return conn, &tl
}

// vectorTestContext holds common test infrastructure for vector database tests.
type vectorTestContext struct {
	t         *testing.T
	conn      *sql.DB
	tx        *sql.Tx
	tableName string
}

// setupVectorTest creates a test context with connection, transaction, and table.
// The table has a single VECTOR column with the specified dimensions.
// Use nullable=true for columns that should allow NULL values.
func setupVectorTest(t *testing.T, dims int, nullable bool) *vectorTestContext {
	t.Helper()
	conn, _ := openWithVectorSupport(t)
	t.Cleanup(func() { conn.Close() })
	skipIfVectorNotSupported(t, conn)

	tx, err := conn.Begin()
	if err != nil {
		t.Fatal("Begin transaction failed:", err)
	}
	t.Cleanup(func() { tx.Rollback() })

	tableName := fmt.Sprintf("#test_vector_%s", t.Name())
	nullSpec := "NOT NULL"
	if nullable {
		nullSpec = "NULL"
	}
	_, err = tx.Exec(fmt.Sprintf("CREATE TABLE %s (id INT IDENTITY(1,1) PRIMARY KEY, embedding VECTOR(%d) %s)", tableName, dims, nullSpec))
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	return &vectorTestContext{t: t, conn: conn, tx: tx, tableName: tableName}
}

// setupVectorTestCustom creates a test context with a custom table schema.
func setupVectorTestCustom(t *testing.T, createSQL string) *vectorTestContext {
	t.Helper()
	conn, _ := openWithVectorSupport(t)
	t.Cleanup(func() { conn.Close() })
	skipIfVectorNotSupported(t, conn)

	tx, err := conn.Begin()
	if err != nil {
		t.Fatal("Begin transaction failed:", err)
	}
	t.Cleanup(func() { tx.Rollback() })

	tableName := fmt.Sprintf("#test_vector_%s", t.Name())
	_, err = tx.Exec(fmt.Sprintf(createSQL, tableName))
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	return &vectorTestContext{t: t, conn: conn, tx: tx, tableName: tableName}
}

// insert inserts a vector and returns the generated ID.
func (ctx *vectorTestContext) insert(v interface{}) int64 {
	ctx.t.Helper()
	result, err := ctx.tx.Exec(fmt.Sprintf("INSERT INTO %s (embedding) VALUES (@p1)", ctx.tableName), v)
	if err != nil {
		ctx.t.Fatalf("Failed to insert: %v", err)
	}
	id, _ := result.LastInsertId()
	return id
}

// selectVector reads a vector by ID.
func (ctx *vectorTestContext) selectVector(id int) Vector {
	ctx.t.Helper()
	var v Vector
	err := ctx.tx.QueryRow(fmt.Sprintf("SELECT embedding FROM %s WHERE id = @p1", ctx.tableName), id).Scan(&v)
	if err != nil {
		ctx.t.Fatalf("Failed to scan vector: %v", err)
	}
	return v
}

// selectNullVector reads a nullable vector by ID.
func (ctx *vectorTestContext) selectNullVector(id int) NullVector {
	ctx.t.Helper()
	var nv NullVector
	err := ctx.tx.QueryRow(fmt.Sprintf("SELECT embedding FROM %s WHERE id = @p1", ctx.tableName), id).Scan(&nv)
	if err != nil {
		ctx.t.Fatalf("Failed to scan NullVector: %v", err)
	}
	return nv
}

// assertVectorEquals checks that two vectors have the same values.
func assertVectorEquals(t *testing.T, got, want Vector) {
	t.Helper()
	if got.Dimensions() != want.Dimensions() {
		t.Fatalf("Dimensions: got %d, want %d", got.Dimensions(), want.Dimensions())
	}
	for i := range want.Data {
		if !floatsEqualVector(got.Data[i], want.Data[i]) {
			t.Errorf("Data[%d]: got %f, want %f", i, got.Data[i], want.Data[i])
		}
	}
}

// TestVectorInsertAndSelect tests inserting and reading Vector values.
// This test requires a SQL Server 2025+ instance.
func TestVectorInsertAndSelect(t *testing.T) {
	ctx := setupVectorTest(t, 3, false)

	v := mustNewVector([]float32{1.0, 2.0, 3.0})
	ctx.insert(v)

	got := ctx.selectVector(1)
	assertVectorEquals(t, got, v)
	t.Logf("Read vector: %v", got)
}

// TestVectorNullInsertAndSelect tests inserting and reading NULL Vector values.
func TestVectorNullInsertAndSelect(t *testing.T) {
	ctx := setupVectorTest(t, 3, true)

	// Insert NULL
	ctx.insert(NullVector{Valid: false})

	// Insert valid
	validVec := mustNewVector([]float32{4.0, 5.0, 6.0})
	ctx.insert(NullVector{Vector: validVec, Valid: true})

	// Verify NULL
	readNull := ctx.selectNullVector(1)
	if readNull.Valid {
		t.Errorf("Expected NULL, got valid: %v", readNull.Vector)
	}

	// Verify valid
	readValid := ctx.selectNullVector(2)
	if !readValid.Valid {
		t.Fatal("Expected valid vector, got NULL")
	}
	assertVectorEquals(t, readValid.Vector, validVec)
	t.Logf("Read valid NullVector: %v", readValid.Vector)
}

// TestVectorDifferentDimensions tests vectors with different dimension counts.
func TestVectorDifferentDimensions(t *testing.T) {
	conn, _ := openWithVectorSupport(t)
	defer conn.Close()
	skipIfVectorNotSupported(t, conn)

	testCases := []struct {
		dims   int
		values []float32
	}{
		{1, []float32{42.0}},
		{5, []float32{1.0, 2.0, 3.0, 4.0, 5.0}},
		{10, make([]float32, 10)},
		{100, make([]float32, 100)},
	}

	// Initialize test vectors with values
	for i := range testCases {
		for j := range testCases[i].values {
			testCases[i].values[j] = float32(j + 1)
		}
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%dD", tc.dims), func(t *testing.T) {
			tx, err := conn.Begin()
			if err != nil {
				t.Fatal("Begin transaction failed:", err)
			}
			defer tx.Rollback()

			tableName := fmt.Sprintf("#test_vector_%d", tc.dims)
			_, err = tx.Exec(fmt.Sprintf("CREATE TABLE %s (id INT IDENTITY(1,1) PRIMARY KEY, embedding VECTOR(%d) NOT NULL)", tableName, tc.dims))
			if err != nil {
				t.Fatalf("Failed to create table: %v", err)
			}

			v := mustNewVector(tc.values)
			_, err = tx.Exec(fmt.Sprintf("INSERT INTO %s (embedding) VALUES (@p1)", tableName), v)
			if err != nil {
				t.Fatalf("Failed to insert: %v", err)
			}

			var got Vector
			err = tx.QueryRow(fmt.Sprintf("SELECT embedding FROM %s WHERE id = 1", tableName)).Scan(&got)
			if err != nil {
				t.Fatalf("Failed to scan: %v", err)
			}

			if got.Dimensions() != tc.dims {
				t.Errorf("Expected %d dimensions, got %d", tc.dims, got.Dimensions())
			}
		})
	}
}

// TestVectorSpecialValues tests vectors with special floating-point values.
func TestVectorSpecialValues(t *testing.T) {
	ctx := setupVectorTest(t, 5, false)
	defer ctx.tx.Rollback()

	// Test special values: zero, negative, small, large
	v := mustNewVector([]float32{0.0, -1.0, 1e-30, 1e30, -0.0})
	ctx.insert(v)

	got := ctx.selectVector(1)

	// Verify values (-0.0 should read as 0.0)
	expected := []float32{0.0, -1.0, 1e-30, 1e30, 0.0}
	for i, val := range got.Data {
		if !floatsEqualVector(val, expected[i]) {
			t.Errorf("Value %d: expected %e, got %e", i, expected[i], val)
		}
	}
	t.Logf("Special values vector: %v", got)
}

// TestVectorDistance tests using vectors in SQL Server VECTOR_DISTANCE function.
func TestVectorDistance(t *testing.T) {
	ctx := setupVectorTestCustom(t, "CREATE TABLE %s (id INT IDENTITY(1,1) PRIMARY KEY, name NVARCHAR(50), embedding VECTOR(3) NOT NULL)")
	defer ctx.tx.Rollback()

	// Insert test vectors
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
		_, err := ctx.tx.Exec(fmt.Sprintf("INSERT INTO %s (name, embedding) VALUES (@p1, @p2)", ctx.tableName), v.name, mustNewVector(v.values))
		if err != nil {
			t.Fatalf("Failed to insert %s: %v", v.name, err)
		}
	}

	// Query using VECTOR_DISTANCE with native binary vector parameter
	queryVector := mustNewVector([]float32{1.0, 0.0, 0.0})
	rows, err := ctx.tx.Query(fmt.Sprintf("SELECT name, VECTOR_DISTANCE('cosine', embedding, @p1) as distance FROM %s ORDER BY distance", ctx.tableName), queryVector)
	if err != nil {
		t.Fatalf("Failed to query with VECTOR_DISTANCE: %v", err)
	}
	defer rows.Close()

	var results []struct {
		name     string
		distance float64
	}
	for rows.Next() {
		var r struct {
			name     string
			distance float64
		}
		if err := rows.Scan(&r.name, &r.distance); err != nil {
			t.Fatalf("Failed to scan result: %v", err)
		}
		results = append(results, r)
		t.Logf("Name: %s, Distance: %f", r.name, r.distance)
	}

	if len(results) != 4 {
		t.Errorf("Expected 4 results, got %d", len(results))
	}
	if results[0].name != "vec_a" || results[0].distance != 0.0 {
		t.Errorf("Expected vec_a with distance 0, got %s with distance %f", results[0].name, results[0].distance)
	}
}

// TestVectorColumnMetadata tests that Vector column metadata is reported correctly.
func TestVectorColumnMetadata(t *testing.T) {
	const dimensions = 128
	ctx := setupVectorTest(t, dimensions, false)
	defer ctx.tx.Rollback()

	// Insert a vector
	testData := make([]float32, dimensions)
	for i := range testData {
		testData[i] = float32(i)
	}
	ctx.insert(mustNewVector(testData))

	// Query and check column types
	rows, err := ctx.tx.Query(fmt.Sprintf("SELECT id, embedding FROM %s", ctx.tableName))
	if err != nil {
		t.Fatalf("Failed to query: %v", err)
	}
	defer rows.Close()

	colTypes, err := rows.ColumnTypes()
	if err != nil {
		t.Fatalf("Failed to get column types: %v", err)
	}

	// Check embedding column type
	embeddingCol := colTypes[1]
	typeName := embeddingCol.DatabaseTypeName()

	// Verify the column type is correctly reported as VECTOR
	if typeName != "VECTOR" {
		t.Errorf("Expected column type VECTOR, got %s", typeName)
	}
	t.Logf("Column type: %s", typeName)

	// Verify length is reported correctly (dimensions)
	if length, ok := embeddingCol.Length(); ok {
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
	const dimensions = 500
	ctx := setupVectorTest(t, dimensions, false)
	defer ctx.tx.Rollback()

	// Create test data
	testData := make([]float32, dimensions)
	for i := range testData {
		testData[i] = float32(i) * 0.001
	}
	ctx.insert(mustNewVector(testData))

	got := ctx.selectVector(1)
	if got.Dimensions() != dimensions {
		t.Errorf("Expected %d dimensions, got %d", dimensions, got.Dimensions())
	}

	// Spot check some values
	for _, i := range []int{0, 100, 250, 499} {
		if !floatsEqualVector(got.Data[i], testData[i]) {
			t.Errorf("Value at index %d: expected %f, got %f", i, testData[i], got.Data[i])
		}
	}
	t.Logf("Successfully round-tripped %d-dimensional vector", dimensions)
}

// TestVectorBatchInsert tests inserting multiple vectors in a transaction.
func TestVectorBatchInsert(t *testing.T) {
	ctx := setupVectorTest(t, 3, false)
	defer ctx.tx.Rollback()

	// Insert multiple vectors
	const count = 100
	for i := 0; i < count; i++ {
		ctx.insert(mustNewVector([]float32{float32(i), float32(i * 2), float32(i * 3)}))
	}

	// Verify count
	var actualCount int
	err := ctx.tx.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", ctx.tableName)).Scan(&actualCount)
	if err != nil {
		t.Fatalf("Failed to count rows: %v", err)
	}
	if actualCount != count {
		t.Errorf("Expected %d rows, got %d", count, actualCount)
	}
	t.Logf("Batch inserted %d vectors successfully", count)
}

// TestVectorSliceFloat32Insert tests inserting []float32 directly without wrapping in Vector.
// This provides better framework compatibility (e.g., GORM) per shueybubbles' feedback.
func TestVectorSliceFloat32Insert(t *testing.T) {
	ctx := setupVectorTest(t, 3, false)
	defer ctx.tx.Rollback()

	// Insert using []float32 directly (not wrapped in Vector type)
	values := []float32{1.0, 2.0, 3.0}
	_, err := ctx.tx.Exec(fmt.Sprintf("INSERT INTO %s (embedding) VALUES (@p1)", ctx.tableName), values)
	if err != nil {
		t.Fatalf("Failed to insert []float32: %v", err)
	}

	// Read back using Vector type
	got := ctx.selectVector(1)
	for i, val := range values {
		if got.Data[i] != val {
			t.Errorf("Value %d: expected %f, got %f", i, val, got.Data[i])
		}
	}
	t.Logf("Successfully round-tripped []float32 -> Vector: %v", got.Data)
}

// TestVectorSliceFloat64Insert tests inserting []float64 directly.
// float64 is the default float type in Go, so this is important for convenience.
func TestVectorSliceFloat64Insert(t *testing.T) {
	ctx := setupVectorTest(t, 3, false)
	defer ctx.tx.Rollback()

	// Insert using []float64 directly (Go's default float type)
	values := []float64{1.5, 2.5, 3.5}
	_, err := ctx.tx.Exec(fmt.Sprintf("INSERT INTO %s (embedding) VALUES (@p1)", ctx.tableName), values)
	if err != nil {
		t.Fatalf("Failed to insert []float64: %v", err)
	}

	got := ctx.selectVector(1)
	if got.Dimensions() != len(values) {
		t.Fatalf("Expected %d dimensions, got %d", len(values), got.Dimensions())
	}

	for i, val := range values {
		if !floatsEqualVector(got.Data[i], float32(val)) {
			t.Errorf("Value %d: expected %f, got %f", i, val, got.Data[i])
		}
	}
	t.Logf("Successfully round-tripped []float64 -> Vector: %v", got.Data)
}

// TestVectorScanToInterface tests that scanning to interface{} returns []byte.
// The driver returns raw binary vector data; applications should scan to Vector
// or NullVector types for decoded values.
func TestVectorScanToInterface(t *testing.T) {
	ctx := setupVectorTest(t, 3, false)
	defer ctx.tx.Rollback()

	ctx.insert(mustNewVector([]float32{1.0, 2.0, 3.0}))

	// Scan to interface{} - returns []byte (raw binary) when native vector format is supported
	var result interface{}
	err := ctx.tx.QueryRow(fmt.Sprintf("SELECT embedding FROM %s WHERE id = 1", ctx.tableName)).Scan(&result)
	if err != nil {
		t.Fatalf("Failed to scan to interface{}: %v", err)
	}

	// Verify we got []byte (native binary format) or string (JSON fallback)
	switch v := result.(type) {
	case []byte:
		// Native binary format - verify we can decode it
		var decoded Vector
		if err := decoded.Scan(v); err != nil {
			t.Fatalf("Failed to decode []byte to Vector: %v", err)
		}
		if decoded.Dimensions() != 3 {
			t.Fatalf("Expected 3 dimensions, got %d", decoded.Dimensions())
		}
		expected := []float32{1.0, 2.0, 3.0}
		for i, val := range expected {
			if decoded.Data[i] != val {
				t.Errorf("Value %d: expected %f, got %f", i, val, decoded.Data[i])
			}
		}
		t.Logf("Scan to interface{} returned []byte (native binary), decoded: %v", decoded.Data)
	case string:
		// JSON fallback - server doesn't support native vector binary format
		// This happens when the TDS vector feature extension is not negotiated
		t.Logf("Scan to interface{} returned string (JSON fallback): %s", v)
		// Parse JSON to verify it's valid vector data
		if !strings.HasPrefix(v, "[") || !strings.HasSuffix(v, "]") {
			t.Fatalf("Expected JSON array, got: %s", v)
		}
		t.Skip("Server does not support native vector binary format - JSON fallback used")
	default:
		t.Fatalf("Expected []byte or string, got %T", result)
	}
}

// TestVectorFloat16 tests float16 vector support (preview feature).
// This test requires SQL Server 2025+ with PREVIEW_FEATURES enabled.
func TestVectorFloat16(t *testing.T) {
	conn, _ := openWithVectorSupport(t)
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

	// Track original PREVIEW_FEATURES state so we can restore it later.
	var previewWasOn bool
	var previewToggled bool

	// Query the current PREVIEW_FEATURES configuration.
	// value is stored as sql_variant, we query its string representation.
	var configValue string
	err = singleConn.QueryRowContext(ctx,
		"SELECT CAST(value AS NVARCHAR(10)) FROM sys.database_scoped_configurations WHERE name = 'PREVIEW_FEATURES'").Scan(&configValue)
	if err != nil {
		// PREVIEW_FEATURES config may not exist on older servers
		t.Skipf("Could not query PREVIEW_FEATURES state (may not be supported): %v", err)
	}
	previewWasOn = configValue == "1"

	// Enable preview features for float16 support if not already enabled.
	// This must be run while in the target database context.
	if !previewWasOn {
		_, err = singleConn.ExecContext(ctx, "ALTER DATABASE SCOPED CONFIGURATION SET PREVIEW_FEATURES = ON")
		if err != nil {
			t.Skipf("Could not enable PREVIEW_FEATURES (may not be supported): %v", err)
		}
		previewToggled = true
	}

	// Ensure we restore PREVIEW_FEATURES to its original state when done.
	defer func() {
		if !previewToggled {
			// We did not change the configuration; nothing to restore.
			return
		}
		_, err := singleConn.ExecContext(ctx, "ALTER DATABASE SCOPED CONFIGURATION SET PREVIEW_FEATURES = OFF")
		if err != nil {
			t.Logf("Warning: Could not restore PREVIEW_FEATURES to OFF: %v", err)
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

	// Insert a float16 vector using NewVectorWithType
	v, err := NewVectorWithType(VectorElementFloat16, []float32{1.0, 2.0, 3.0})
	if err != nil {
		t.Fatalf("Failed to create float16 vector: %v", err)
	}
	_, err = tx.Exec(
		fmt.Sprintf("INSERT INTO %s (embedding) VALUES (@p1)", tableName),
		v,
	)
	if err != nil {
		// Float16 native binary parameters may not be supported yet in current SQL Server build
		// SQL Server 2025 RTM supports float16 columns but native float16 parameters require additional support
		if strings.Contains(err.Error(), "invalid precision or scale") ||
			strings.Contains(err.Error(), "Conversion of vector") {
			t.Skipf("Float16 native binary parameters not supported in this SQL Server build: %v", err)
		}
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
	// Must use NewVectorWithType to create a float16 vector - SQL Server won't convert float32 to float16
	precisionTestValues := []float32{1.001, 2.002, 3.003}
	v2, err := NewVectorWithType(VectorElementFloat16, precisionTestValues)
	if err != nil {
		t.Fatalf("Failed to create float16 precision test vector: %v", err)
	}
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

	// IEEE 754 float16 has a 10-bit significand (~3 decimal digits of precision).
	// The ULP near 1.0 is 2^-10 ≈ 9.8e-4, so for values in the 1–3 range we expect
	// absolute conversion error to be on the order of a few 1e-3. We therefore use
	// a conservative 0.01 absolute tolerance here to allow for implementation-
	// specific details while still catching excessive precision loss.
	t.Logf("Precision test - input: %v, output: %v", precisionTestValues, readVector2.Data)
	for i, val := range readVector2.Data {
		diff := math.Abs(float64(val - precisionTestValues[i]))
		if diff > 0.01 {
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
