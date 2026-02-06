//go:build go1.9
// +build go1.9

package mssql

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/microsoft/go-mssqldb/msdsn"
)

// requireNativeJSON checks if the server supports native JSON type and skips the test if not.
// The native JSON data type (type ID 0xF4) is available in:
// - SQL Server 2025 (version 17+) - preview
// - Azure SQL Database - generally available
// - Azure SQL Managed Instance - with Always-up-to-date update policy
func requireNativeJSON(t *testing.T, db *sql.DB, ctx context.Context) {
	t.Helper()
	var jsonTypeCount int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sys.types WHERE name = 'json'").Scan(&jsonTypeCount)
	if err != nil {
		t.Skipf("Could not determine JSON type support: %v", err)
	}
	if jsonTypeCount == 0 {
		t.Skipf("Native JSON type is not supported on this server (no 'json' type in sys.types)")
	}
}

// jsonTestContext holds common test infrastructure for JSON database tests.
type jsonTestContext struct {
	t   *testing.T
	db  *sql.DB
	ctx context.Context
}

// setupJSONTest creates a test context with database connection and context.
// If requireNative is true, skips if native JSON type is not supported.
func setupJSONTest(t *testing.T, requireNative bool) *jsonTestContext {
	t.Helper()
	checkConnStr(t)
	tl := testLogger{t: t}
	SetLogger(&tl)
	t.Cleanup(tl.StopLogging)

	db, err := sql.Open("sqlserver", makeConnStr(t).String())
	if err != nil {
		t.Fatalf("failed to open driver sqlserver: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	if requireNative {
		requireNativeJSON(t, db, ctx)
	}

	return &jsonTestContext{t: t, db: db, ctx: ctx}
}

// hasNativeJSON returns true if the server supports the native JSON type.
func (jtc *jsonTestContext) hasNativeJSON() bool {
	var count int
	err := jtc.db.QueryRowContext(jtc.ctx, "SELECT COUNT(*) FROM sys.types WHERE name = 'json'").Scan(&count)
	if err != nil {
		return false
	}
	return count > 0
}

// conn returns a dedicated connection for tests that need temp table persistence.
func (jtc *jsonTestContext) conn() *sql.Conn {
	conn, err := jtc.db.Conn(jtc.ctx)
	if err != nil {
		jtc.t.Fatalf("Failed to get connection: %v", err)
	}
	jtc.t.Cleanup(func() { conn.Close() })
	return conn
}

// TestJSONType tests the JSON type parameter encoding and decoding.
// Note: The native JSON type (type ID 0xF4) requires:
// - SQL Server 2025 (version 17+) - preview
// - Azure SQL Database - generally available
// - Azure SQL Managed Instance with Always-up-to-date update policy
func TestJSONType(t *testing.T) {
	jtc := setupJSONTest(t, true)

	t.Run("JSON parameter round-trip", func(t *testing.T) {
		jsonValue := json.RawMessage(`{"name":"test","value":123,"nested":{"key":"value"}}`)
		var result string

		// Test passing JSON as parameter and reading it back
		// Using ISJSON to verify it's valid JSON
		err := jtc.db.QueryRowContext(jtc.ctx, `
			SELECT @p1 AS json_result
			WHERE ISJSON(@p1) = 1
		`, JSON(jsonValue)).Scan(&result)
		if err != nil {
			t.Fatalf("Failed to execute JSON query: %v", err)
		}
		if result != string(jsonValue) {
			t.Errorf("JSON value mismatch.\nExpected: %s\nGot: %s", jsonValue, result)
		}
	})

	t.Run("JSON with special characters", func(t *testing.T) {
		jsonValue := json.RawMessage(`{"message":"Hello \"World\"","path":"C:\\test\\path","unicode":"日本語"}`)
		var result string

		err := jtc.db.QueryRowContext(jtc.ctx, `SELECT @p1`, JSON(jsonValue)).Scan(&result)
		if err != nil {
			t.Fatalf("Failed to execute JSON query with special chars: %v", err)
		}
		if result != string(jsonValue) {
			t.Errorf("JSON value mismatch.\nExpected: %s\nGot: %s", jsonValue, result)
		}
	})

	t.Run("JSON array", func(t *testing.T) {
		jsonValue := json.RawMessage(`[1,2,3,{"key":"value"},"string"]`)
		var result string

		err := jtc.db.QueryRowContext(jtc.ctx, `SELECT @p1`, JSON(jsonValue)).Scan(&result)
		if err != nil {
			t.Fatalf("Failed to execute JSON array query: %v", err)
		}
		if result != string(jsonValue) {
			t.Errorf("JSON array mismatch.\nExpected: %s\nGot: %s", jsonValue, result)
		}
	})

	t.Run("Large JSON", func(t *testing.T) {
		// Create a large JSON object (> 8000 chars to test PLP handling)
		var sb strings.Builder
		sb.WriteString(`{"data":"`)
		for i := 0; i < 10000; i++ {
			sb.WriteByte('x')
		}
		sb.WriteString(`"}`)
		largeValue := sb.String()

		var result string
		err := jtc.db.QueryRowContext(jtc.ctx, `SELECT @p1`, JSON(largeValue)).Scan(&result)
		if err != nil {
			t.Fatalf("Failed to execute large JSON query: %v", err)
		}
		if result != largeValue {
			t.Errorf("Large JSON length mismatch. Expected %d, got %d", len(largeValue), len(result))
		}
	})
}

// TestNullJSONType tests the NullJSON type for nullable JSON values.
func TestNullJSONType(t *testing.T) {
	jtc := setupJSONTest(t, true)

	t.Run("NullJSON with valid value", func(t *testing.T) {
		jsonValue := json.RawMessage(`{"valid":true}`)
		param := NullJSON{JSON: jsonValue, Valid: true}
		var result NullJSON

		err := jtc.db.QueryRowContext(jtc.ctx, `SELECT @p1`, param).Scan(&result)
		if err != nil {
			t.Fatalf("Failed to execute NullJSON query: %v", err)
		}
		if !result.Valid {
			t.Error("Expected Valid to be true")
		}
		if string(result.JSON) != string(jsonValue) {
			t.Errorf("NullJSON value mismatch.\nExpected: %s\nGot: %s", jsonValue, result.JSON)
		}
	})

	t.Run("NullJSON with NULL value", func(t *testing.T) {
		param := NullJSON{Valid: false}
		var result NullJSON

		err := jtc.db.QueryRowContext(jtc.ctx, `SELECT @p1`, param).Scan(&result)
		if err != nil {
			t.Fatalf("Failed to execute NULL NullJSON query: %v", err)
		}
		if result.Valid {
			t.Error("Expected Valid to be false for NULL value")
		}
	})

	t.Run("Scan NULL into NullJSON", func(t *testing.T) {
		var result NullJSON

		err := jtc.db.QueryRowContext(jtc.ctx, `SELECT NULL`).Scan(&result)
		if err != nil {
			t.Fatalf("Failed to scan NULL into NullJSON: %v", err)
		}
		if result.Valid {
			t.Error("Expected Valid to be false when scanning NULL")
		}
	})

	t.Run("Scan string into NullJSON", func(t *testing.T) {
		jsonValue := `{"scanned":true}`
		var result NullJSON

		err := jtc.db.QueryRowContext(jtc.ctx, `SELECT @p1`, jsonValue).Scan(&result)
		if err != nil {
			t.Fatalf("Failed to scan string into NullJSON: %v", err)
		}
		if !result.Valid {
			t.Error("Expected Valid to be true")
		}
		if string(result.JSON) != jsonValue {
			t.Errorf("Scanned value mismatch.\nExpected: %s\nGot: %s", jsonValue, result.JSON)
		}
	})
}

// TestNullJSONScanInterface tests the NullJSON.Scan method with various input types.
func TestNullJSONScanInterface(t *testing.T) {
	t.Run("Scan nil", func(t *testing.T) {
		var nj NullJSON
		err := nj.Scan(nil)
		if err != nil {
			t.Fatalf("Scan(nil) returned error: %v", err)
		}
		if nj.Valid {
			t.Error("Expected Valid to be false after scanning nil")
		}
		if nj.JSON != nil {
			t.Errorf("Expected nil JSON after scanning nil, got: %s", nj.JSON)
		}
	})

	t.Run("Scan string", func(t *testing.T) {
		var nj NullJSON
		jsonStr := `{"test":"value"}`
		err := nj.Scan(jsonStr)
		if err != nil {
			t.Fatalf("Scan(string) returned error: %v", err)
		}
		if !nj.Valid {
			t.Error("Expected Valid to be true after scanning string")
		}
		if string(nj.JSON) != jsonStr {
			t.Errorf("Expected JSON %s, got: %s", jsonStr, nj.JSON)
		}
	})

	t.Run("Scan []byte", func(t *testing.T) {
		var nj NullJSON
		jsonBytes := []byte(`{"test":"bytes"}`)
		err := nj.Scan(jsonBytes)
		if err != nil {
			t.Fatalf("Scan([]byte) returned error: %v", err)
		}
		if !nj.Valid {
			t.Error("Expected Valid to be true after scanning []byte")
		}
		if string(nj.JSON) != string(jsonBytes) {
			t.Errorf("Expected JSON %s, got: %s", string(jsonBytes), nj.JSON)
		}
	})

	t.Run("Scan unsupported type", func(t *testing.T) {
		var nj NullJSON
		err := nj.Scan(12345)
		if err == nil {
			t.Error("Expected error when scanning unsupported type")
		}
	})
}

// TestNullJSONValue tests the NullJSON.Value method.
func TestNullJSONValue(t *testing.T) {
	t.Run("Value with valid JSON", func(t *testing.T) {
		nj := NullJSON{JSON: json.RawMessage(`{"test":"value"}`), Valid: true}
		val, err := nj.Value()
		if err != nil {
			t.Fatalf("Value() returned error: %v", err)
		}
		str, ok := val.(string)
		if !ok {
			t.Fatalf("Expected string value, got %T", val)
		}
		if str != string(nj.JSON) {
			t.Errorf("Expected %s, got %s", nj.JSON, str)
		}
	})

	t.Run("Value with invalid (NULL) JSON", func(t *testing.T) {
		nj := NullJSON{Valid: false}
		val, err := nj.Value()
		if err != nil {
			t.Fatalf("Value() returned error: %v", err)
		}
		if val != nil {
			t.Errorf("Expected nil value for invalid NullJSON, got %v", val)
		}
	})
}

// TestJSONFallbackBehavior tests that JSON parameters work correctly on both
// SQL Server 2025+ (native JSON support) and previous versions (nvarchar fallback).
// This test verifies the jsonSupported flag and fallback logic.
func TestJSONFallbackBehavior(t *testing.T) {
	jtc := setupJSONTest(t, false) // Don't require native JSON

	hasNative := jtc.hasNativeJSON()
	if hasNative {
		t.Log("Server supports native JSON type - parameters declared as 'json'")
	} else {
		t.Log("Server does not support native JSON type - parameters fall back to 'nvarchar(max)'")
	}

	// Test 1: JSON parameter should work regardless of server version
	// On SQL Server 2025+: uses native json type declaration
	// On earlier versions: falls back to nvarchar(max) declaration
	t.Run("JSON parameter works on all supported versions", func(t *testing.T) {
		jsonValue := JSON(`{"test":"value","number":42}`)
		var result string

		err := jtc.db.QueryRowContext(jtc.ctx, `SELECT @p1`, jsonValue).Scan(&result)
		if err != nil {
			t.Fatalf("JSON parameter failed: %v", err)
		}
		if result != string(jsonValue) {
			t.Errorf("JSON value mismatch.\nExpected: %s\nGot: %s", jsonValue, result)
		}
	})

	// Test 2: NullJSON with valid value should work
	t.Run("NullJSON valid parameter works on all versions", func(t *testing.T) {
		param := NullJSON{JSON: json.RawMessage(`{"nullable":true}`), Valid: true}
		var result string

		err := jtc.db.QueryRowContext(jtc.ctx, `SELECT @p1`, param).Scan(&result)
		if err != nil {
			t.Fatalf("NullJSON parameter failed: %v", err)
		}
		if result != string(param.JSON) {
			t.Errorf("NullJSON value mismatch.\nExpected: %s\nGot: %s", param.JSON, result)
		}
	})

	// Test 3: NullJSON with NULL value should work
	t.Run("NullJSON NULL parameter works on all versions", func(t *testing.T) {
		param := NullJSON{Valid: false}
		var result sql.NullString

		err := jtc.db.QueryRowContext(jtc.ctx, `SELECT @p1`, param).Scan(&result)
		if err != nil {
			t.Fatalf("NullJSON NULL parameter failed: %v", err)
		}
		if result.Valid {
			t.Errorf("Expected NULL result, got: %s", result.String)
		}
	})

	// Test 4: JSON can be validated with ISJSON function (available since SQL Server 2016)
	// ISJSON is widely available - skip if not supported
	t.Run("JSON validated with ISJSON function", func(t *testing.T) {
		jsonValue := JSON(`{"valid":"json"}`)
		var isValidJSON int

		err := jtc.db.QueryRowContext(jtc.ctx, `SELECT ISJSON(@p1)`, jsonValue).Scan(&isValidJSON)
		if err != nil {
			t.Skipf("ISJSON not available on this server: %v", err)
		}
		if isValidJSON != 1 {
			t.Errorf("Expected ISJSON to return 1, got: %d", isValidJSON)
		}
	})

	// Test 5: JSON can be used with JSON_VALUE function (available since SQL Server 2016)
	// JSON_VALUE is widely available - skip if not supported
	t.Run("JSON works with JSON_VALUE function", func(t *testing.T) {
		jsonValue := JSON(`{"name":"testvalue","count":123}`)
		var extractedName string

		err := jtc.db.QueryRowContext(jtc.ctx, `SELECT JSON_VALUE(@p1, '$.name')`, jsonValue).Scan(&extractedName)
		if err != nil {
			t.Skipf("JSON_VALUE not available on this server: %v", err)
		}
		if extractedName != "testvalue" {
			t.Errorf("Expected 'testvalue', got: %s", extractedName)
		}
	})

	// Test 6: Large JSON (tests PLP handling)
	t.Run("Large JSON parameter works on all versions", func(t *testing.T) {
		// Create JSON larger than 8000 bytes to test PLP handling
		var sb strings.Builder
		sb.WriteString(`{"data":"`)
		for i := 0; i < 10000; i++ {
			sb.WriteByte('x')
		}
		sb.WriteString(`"}`)
		largeData := sb.String()

		jsonValue := JSON(largeData)
		var result string

		err := jtc.db.QueryRowContext(jtc.ctx, `SELECT @p1`, jsonValue).Scan(&result)
		if err != nil {
			t.Fatalf("Large JSON parameter failed: %v", err)
		}
		if len(result) != len(largeData) {
			t.Errorf("Large JSON length mismatch. Expected %d, got %d", len(largeData), len(result))
		}
	})

	// Test 7: Verify behavior based on JSON type support
	t.Run("JSON type support verification", func(t *testing.T) {
		// This test passes if we get here without errors - the fallback is working
		jsonValue := JSON(`{"version_test":true}`)
		var result string
		err := jtc.db.QueryRowContext(jtc.ctx, `SELECT @p1`, jsonValue).Scan(&result)
		if err != nil {
			t.Fatalf("JSON type support test failed: %v", err)
		}
	})
}

// TestJSONTableInsertAndSelect tests JSON with actual table operations.
func TestJSONTableInsertAndSelect(t *testing.T) {
	jtc := setupJSONTest(t, true)
	conn := jtc.conn()

	// Create test table with native JSON columns
	tableName := "#test_json_table"
	_, err := conn.ExecContext(jtc.ctx, `
		CREATE TABLE `+tableName+` (
			id INT IDENTITY(1,1) PRIMARY KEY,
			json_data JSON,
			nullable_json JSON
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create test table: %v", err)
	}

	t.Run("Insert and select JSON", func(t *testing.T) {
		jsonValue := json.RawMessage(`{"id":1,"name":"test","active":true}`)

		// Insert using JSON type
		_, err := conn.ExecContext(jtc.ctx, `
			INSERT INTO `+tableName+` (json_data, nullable_json)
			VALUES (@p1, @p2)
		`, JSON(jsonValue), NullJSON{Valid: false})
		if err != nil {
			t.Fatalf("Failed to insert JSON: %v", err)
		}

		// Read back
		var readJSON string
		var nullableJSON NullJSON
		err = conn.QueryRowContext(jtc.ctx, `SELECT json_data, nullable_json FROM `+tableName+` WHERE id = 1`).
			Scan(&readJSON, &nullableJSON)
		if err != nil {
			t.Fatalf("Failed to select JSON: %v", err)
		}

		if readJSON != string(jsonValue) {
			t.Errorf("JSON mismatch.\nExpected: %s\nGot: %s", jsonValue, readJSON)
		}
		if nullableJSON.Valid {
			t.Error("Expected nullable_json to be NULL")
		}
	})

	t.Run("Update with NullJSON", func(t *testing.T) {
		newJSON := json.RawMessage(`{"updated":true}`)
		_, err := conn.ExecContext(jtc.ctx, `
			UPDATE `+tableName+`
			SET nullable_json = @p1
			WHERE id = 1
		`, NullJSON{JSON: newJSON, Valid: true})
		if err != nil {
			t.Fatalf("Failed to update JSON: %v", err)
		}

		var result NullJSON
		err = conn.QueryRowContext(jtc.ctx, `SELECT nullable_json FROM `+tableName+` WHERE id = 1`).Scan(&result)
		if err != nil {
			t.Fatalf("Failed to select updated JSON: %v", err)
		}

		if !result.Valid {
			t.Error("Expected nullable_json to be valid after update")
		}
		if string(result.JSON) != string(newJSON) {
			t.Errorf("Updated JSON mismatch.\nExpected: %s\nGot: %s", newJSON, result.JSON)
		}
	})
}

// TestNullJSONScanJSONRawMessage tests scanning json.RawMessage into NullJSON.
func TestNullJSONScanJSONRawMessage(t *testing.T) {
	var nj NullJSON
	raw := json.RawMessage(`{"raw":"message"}`)
	err := nj.Scan(raw)
	if err != nil {
		t.Fatalf("Scan(json.RawMessage) returned error: %v", err)
	}
	if !nj.Valid {
		t.Error("Expected Valid to be true after scanning json.RawMessage")
	}
	if string(nj.JSON) != string(raw) {
		t.Errorf("Expected JSON %s, got: %s", string(raw), nj.JSON)
	}
	// Verify the copy is independent (modifying raw shouldn't affect nj.JSON)
	original := string(nj.JSON)
	raw[0] = '['
	if string(nj.JSON) != original {
		t.Error("NullJSON.JSON should be independent copy of input")
	}
}

// TestNullJSONScanBytesCopy verifies that Scan makes a copy of []byte input.
func TestNullJSONScanBytesCopy(t *testing.T) {
	var nj NullJSON
	input := []byte(`{"test":"copy"}`)
	err := nj.Scan(input)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	// Modify input and verify nj.JSON is unchanged
	original := string(nj.JSON)
	input[0] = '['
	if string(nj.JSON) != original {
		t.Error("NullJSON.JSON should be independent copy of input []byte")
	}
}

// TestJSONNativeSupport_SQL2025 tests native JSON type features that only work on SQL Server 2025+.
// These tests verify the TDS JSON feature negotiation and native JSON column support.
func TestJSONNativeSupport_SQL2025(t *testing.T) {
	jtc := setupJSONTest(t, true)
	conn := jtc.conn()

	t.Run("Native JSON column type", func(t *testing.T) {
		// Create table with native JSON column - this only works on SQL 2025+
		tableName := "#test_native_json_col"
		_, err := conn.ExecContext(jtc.ctx, `
			CREATE TABLE `+tableName+` (
				id INT IDENTITY(1,1) PRIMARY KEY,
				data JSON NOT NULL
			)
		`)
		if err != nil {
			t.Fatalf("Failed to create table with JSON column: %v", err)
		}

		// Insert and retrieve data
		testData := `{"native":true,"version":2025}`
		_, err = conn.ExecContext(jtc.ctx, `INSERT INTO `+tableName+` (data) VALUES (@p1)`, JSON(testData))
		if err != nil {
			t.Fatalf("Failed to insert into JSON column: %v", err)
		}

		var result string
		err = conn.QueryRowContext(jtc.ctx, `SELECT data FROM `+tableName).Scan(&result)
		if err != nil {
			t.Fatalf("Failed to select from JSON column: %v", err)
		}

		if result != testData {
			t.Errorf("JSON data mismatch.\nExpected: %s\nGot: %s", testData, result)
		}
	})

	t.Run("JSON type in column metadata", func(t *testing.T) {
		// Query a JSON literal and check that the driver correctly handles the response
		var result NullJSON
		err := conn.QueryRowContext(jtc.ctx, `SELECT CAST('{"test":1}' AS JSON)`).Scan(&result)
		if err != nil {
			t.Fatalf("Failed to query JSON literal: %v", err)
		}
		if !result.Valid {
			t.Error("Expected valid JSON result")
		}
		if string(result.JSON) != `{"test":1}` {
			t.Errorf("Expected {\"test\":1}, got: %s", result.JSON)
		}
	})

	t.Run("JSON with SQL Server JSON functions", func(t *testing.T) {
		// Test native JSON type works with JSON_MODIFY (SQL 2016+)
		original := `{"key":"value","count":0}`
		var modified string
		err := conn.QueryRowContext(jtc.ctx,
			`SELECT JSON_MODIFY(@p1, '$.count', 42)`,
			JSON(original)).Scan(&modified)
		if err != nil {
			t.Fatalf("JSON_MODIFY failed: %v", err)
		}

		// Verify the modification
		var count int
		err = conn.QueryRowContext(jtc.ctx, `SELECT JSON_VALUE(@p1, '$.count')`, JSON(modified)).Scan(&count)
		if err != nil {
			t.Fatalf("JSON_VALUE failed: %v", err)
		}
		if count != 42 {
			t.Errorf("Expected count=42, got: %d", count)
		}
	})
}

// TestJSONFallback_PreSQL2025 tests that JSON parameters work correctly on SQL Server
// versions that don't support the native JSON type (pre-2025).
// These tests verify the nvarchar(max) fallback behavior.
func TestJSONFallback_PreSQL2025(t *testing.T) {
	jtc := setupJSONTest(t, false)

	// Skip if server supports native JSON
	if jtc.hasNativeJSON() {
		t.Skipf("Server supports native JSON type (testing fallback requires no native JSON type)")
	}

	t.Logf("Testing fallback behavior on server without native JSON type")
	conn := jtc.conn()

	t.Run("JSON parameters work via nvarchar fallback", func(t *testing.T) {
		jsonValue := JSON(`{"fallback":"test","works":true}`)
		var result string

		err := conn.QueryRowContext(jtc.ctx, `SELECT @p1`, jsonValue).Scan(&result)
		if err != nil {
			t.Fatalf("JSON parameter failed on pre-2025 server: %v", err)
		}
		if result != string(jsonValue) {
			t.Errorf("JSON value mismatch.\nExpected: %s\nGot: %s", jsonValue, result)
		}
	})

	t.Run("JSON stored in nvarchar(max) column", func(t *testing.T) {
		// On pre-2025 servers, JSON is typically stored in nvarchar(max) columns
		tableName := "#test_json_nvarchar_col"
		_, err := conn.ExecContext(jtc.ctx, `
			CREATE TABLE `+tableName+` (
				id INT IDENTITY(1,1) PRIMARY KEY,
				json_data NVARCHAR(MAX)
			)
		`)
		if err != nil {
			t.Fatalf("Failed to create table: %v", err)
		}

		jsonValue := `{"stored":"in_nvarchar"}`
		_, err = conn.ExecContext(jtc.ctx, `INSERT INTO `+tableName+` (json_data) VALUES (@p1)`, JSON(jsonValue))
		if err != nil {
			t.Fatalf("Failed to insert JSON into nvarchar column: %v", err)
		}

		var result string
		err = conn.QueryRowContext(jtc.ctx, `SELECT json_data FROM `+tableName).Scan(&result)
		if err != nil {
			t.Fatalf("Failed to select JSON from nvarchar column: %v", err)
		}
		if result != jsonValue {
			t.Errorf("JSON mismatch.\nExpected: %s\nGot: %s", jsonValue, result)
		}
	})

	t.Run("JSON validated with ISJSON on fallback server", func(t *testing.T) {
		jsonValue := JSON(`{"valid":"json"}`)
		var isValid int
		err := conn.QueryRowContext(jtc.ctx, `SELECT ISJSON(@p1)`, jsonValue).Scan(&isValid)
		if err != nil {
			t.Skipf("ISJSON not available on this server: %v", err)
		}
		if isValid != 1 {
			t.Errorf("Expected ISJSON=1, got: %d", isValid)
		}
	})

	t.Run("Native JSON column fails on server without native JSON type", func(t *testing.T) {
		// Attempting to create a native JSON column should fail on pre-2025
		tableName := "#test_native_json_fail"
		_, err := conn.ExecContext(jtc.ctx, `
			CREATE TABLE `+tableName+` (
				id INT PRIMARY KEY,
				data JSON
			)
		`)
		if err == nil {
			t.Error("Expected error creating JSON column on pre-2025 server, but succeeded")
		} else {
			t.Logf("Expected error on pre-2025: %v", err)
		}
	})

	t.Run("NullJSON fallback behavior", func(t *testing.T) {
		// Test NullJSON with valid value
		param := NullJSON{JSON: json.RawMessage(`{"nullable":"fallback"}`), Valid: true}
		var result string
		err := conn.QueryRowContext(jtc.ctx, `SELECT @p1`, param).Scan(&result)
		if err != nil {
			t.Fatalf("NullJSON parameter failed: %v", err)
		}
		if result != string(param.JSON) {
			t.Errorf("NullJSON mismatch.\nExpected: %s\nGot: %s", param.JSON, result)
		}

		// Test NullJSON with NULL value
		nullParam := NullJSON{Valid: false}
		var nullResult sql.NullString
		err = conn.QueryRowContext(jtc.ctx, `SELECT @p1`, nullParam).Scan(&nullResult)
		if err != nil {
			t.Fatalf("NullJSON NULL parameter failed: %v", err)
		}
		if nullResult.Valid {
			t.Errorf("Expected NULL result, got: %s", nullResult.String)
		}
	})
}

// TestJSONPointerTypes tests *JSON and *NullJSON pointer type handling.
func TestJSONPointerTypes(t *testing.T) {
	t.Run("convertInputParameter with *JSON", func(t *testing.T) {
		// Non-nil *JSON
		jsonVal := JSON(`{"pointer":"test"}`)
		result, err := convertInputParameter(&jsonVal)
		if err != nil {
			t.Fatalf("convertInputParameter(*JSON) returned error: %v", err)
		}
		if converted, ok := result.(JSON); !ok {
			t.Errorf("Expected JSON type, got %T", result)
		} else if string(converted) != string(jsonVal) {
			t.Errorf("Expected %s, got %s", jsonVal, converted)
		}

		// nil *JSON - returns NullJSON{} to preserve JSON type
		var nilJSON *JSON
		result, err = convertInputParameter(nilJSON)
		if err != nil {
			t.Fatalf("convertInputParameter(nil *JSON) returned error: %v", err)
		}
		if converted, ok := result.(NullJSON); !ok {
			t.Errorf("Expected NullJSON type for nil *JSON, got %T", result)
		} else if converted.Valid {
			t.Errorf("Expected NullJSON.Valid=false for nil *JSON, got true")
		}
	})

	t.Run("convertInputParameter with *NullJSON", func(t *testing.T) {
		// Non-nil *NullJSON with valid value
		nullJSON := NullJSON{JSON: json.RawMessage(`{"pointer":"nulljson"}`), Valid: true}
		result, err := convertInputParameter(&nullJSON)
		if err != nil {
			t.Fatalf("convertInputParameter(*NullJSON) returned error: %v", err)
		}
		if converted, ok := result.(NullJSON); !ok {
			t.Errorf("Expected NullJSON type, got %T", result)
		} else if string(converted.JSON) != string(nullJSON.JSON) {
			t.Errorf("Expected %s, got %s", nullJSON.JSON, converted.JSON)
		}

		// nil *NullJSON - returns NullJSON{} to preserve JSON type
		var nilNullJSON *NullJSON
		result, err = convertInputParameter(nilNullJSON)
		if err != nil {
			t.Fatalf("convertInputParameter(nil *NullJSON) returned error: %v", err)
		}
		if converted, ok := result.(NullJSON); !ok {
			t.Errorf("Expected NullJSON type for nil *NullJSON, got %T", result)
		} else if converted.Valid {
			t.Errorf("Expected NullJSON.Valid=false for nil *NullJSON, got true")
		}
	})
}

// TestJSONGoLangScanType tests makeGoLangScanType for JSON type.
func TestJSONGoLangScanType(t *testing.T) {
	ti := typeInfo{TypeId: typeJson}
	scanType := makeGoLangScanType(ti)
	// JSON scan type should be string
	expectedType := "string"
	if scanType.String() != expectedType {
		t.Errorf("Expected scan type %s for JSON, got %s", expectedType, scanType.String())
	}
}

// TestJSONMarshalUnmarshal verifies that mssql.JSON properly implements
// MarshalJSON/UnmarshalJSON so it behaves like json.RawMessage rather than
// being treated as a byte slice (which would cause base64 encoding).
func TestJSONMarshalUnmarshal(t *testing.T) {
	t.Run("MarshalJSON", func(t *testing.T) {
		j := JSON(`{"key":"value"}`)
		data, err := json.Marshal(j)
		if err != nil {
			t.Fatalf("json.Marshal(JSON) returned error: %v", err)
		}
		// Should marshal as raw JSON, not base64
		expected := `{"key":"value"}`
		if string(data) != expected {
			t.Errorf("Expected %s, got %s", expected, string(data))
		}
	})

	t.Run("MarshalJSON nil", func(t *testing.T) {
		var j JSON
		data, err := json.Marshal(j)
		if err != nil {
			t.Fatalf("json.Marshal(nil JSON) returned error: %v", err)
		}
		if string(data) != "null" {
			t.Errorf("Expected null, got %s", string(data))
		}
	})

	t.Run("UnmarshalJSON", func(t *testing.T) {
		var j JSON
		err := json.Unmarshal([]byte(`{"key":"value"}`), &j)
		if err != nil {
			t.Fatalf("json.Unmarshal into JSON returned error: %v", err)
		}
		expected := `{"key":"value"}`
		if string(j) != expected {
			t.Errorf("Expected %s, got %s", expected, string(j))
		}
	})

	t.Run("JSON in struct", func(t *testing.T) {
		type wrapper struct {
			Data JSON `json:"data"`
		}
		w := wrapper{Data: JSON(`{"nested":"object"}`)}
		data, err := json.Marshal(w)
		if err != nil {
			t.Fatalf("json.Marshal(wrapper) returned error: %v", err)
		}
		expected := `{"data":{"nested":"object"}}`
		if string(data) != expected {
			t.Errorf("Expected %s, got %s", expected, string(data))
		}
	})
}

// TestJSONWireDecoding tests that JSON data can be decoded from UTF-16LE wire format.
// JSON uses the same wire encoding as NVarChar (UTF-16LE).
func TestJSONWireDecoding(t *testing.T) {
	// Test decoding UTF-16LE encoded JSON
	// The string `{"key":"value"}` in UTF-16LE
	jsonStr := `{"key":"value"}`
	utf16Data := str2ucs2(jsonStr)

	decoded := decodeNChar(utf16Data)
	if decoded != jsonStr {
		t.Errorf("Expected decoded JSON %q, got %q", jsonStr, decoded)
	}

	// Test with more complex JSON
	complexJSON := `{"name":"test","value":123,"nested":{"array":[1,2,3]}}`
	utf16Complex := str2ucs2(complexJSON)
	decodedComplex := decodeNChar(utf16Complex)
	if decodedComplex != complexJSON {
		t.Errorf("Expected decoded JSON %q, got %q", complexJSON, decodedComplex)
	}

	// Test empty JSON object
	emptyJSON := `{}`
	utf16Empty := str2ucs2(emptyJSON)
	decodedEmpty := decodeNChar(utf16Empty)
	if decodedEmpty != emptyJSON {
		t.Errorf("Expected decoded JSON %q, got %q", emptyJSON, decodedEmpty)
	}
}

// TestJSONTypeFunctions tests all type-related functions for JSON type.
// This covers makeDecl, makeGoLangTypeName, makeGoLangTypeLength, makeGoLangTypePrecisionScale
func TestJSONTypeFunctions(t *testing.T) {
	ti := typeInfo{TypeId: typeJson}

	t.Run("makeDecl", func(t *testing.T) {
		decl := makeDecl(ti)
		if decl != "json" {
			t.Errorf("Expected makeDecl to return 'json', got: %s", decl)
		}
	})

	t.Run("makeDecl with DeclTypeId", func(t *testing.T) {
		// Test DeclTypeId override - when TypeId is nvarchar but DeclTypeId is json
		tiWithDecl := typeInfo{TypeId: typeNVarChar, DeclTypeId: typeJson, Size: 0}
		decl := makeDecl(tiWithDecl)
		if decl != "json" {
			t.Errorf("Expected makeDecl with DeclTypeId to return 'json', got: %s", decl)
		}
	})

	t.Run("makeGoLangTypeName", func(t *testing.T) {
		typeName := makeGoLangTypeName(ti)
		if typeName != "JSON" {
			t.Errorf("Expected makeGoLangTypeName to return 'JSON', got: %s", typeName)
		}
	})

	t.Run("makeGoLangTypeLength", func(t *testing.T) {
		length, hasLength := makeGoLangTypeLength(ti)
		if !hasLength {
			t.Error("Expected makeGoLangTypeLength to return true for JSON")
		}
		expectedLength := int64(2147483645 / 2) // Same as nvarchar(max)
		if length != expectedLength {
			t.Errorf("Expected length %d, got: %d", expectedLength, length)
		}
	})

	t.Run("makeGoLangTypePrecisionScale", func(t *testing.T) {
		prec, scale, hasPrecScale := makeGoLangTypePrecisionScale(ti)
		if hasPrecScale {
			t.Error("Expected makeGoLangTypePrecisionScale to return false for JSON")
		}
		if prec != 0 || scale != 0 {
			t.Errorf("Expected prec=0, scale=0, got prec=%d, scale=%d", prec, scale)
		}
	})
}

// TestReadTypeInfoJSON tests reading JSON type metadata from TDS buffer.
// This exercises the typeJson case in readTypeInfo.
func TestReadTypeInfoJSON(t *testing.T) {
	// JSON type wire format: no additional metadata bytes after the type ID.
	// Unlike NVARCHAR which has a 2-byte size indicator, JSON is always a PLP type
	// with no size indicator in the column metadata. The type ID alone determines
	// the format, and readVarLen sets up the PLP reader without reading any bytes.
	data := []byte{} // JSON TYPE_INFO has no additional bytes

	r := &tdsBuffer{
		packetSize: 1, // minimum valid packet size
		rbuf:       data,
		rpos:       0,
		rsize:      len(data),
	}

	ti := readTypeInfo(r, typeJson, nil, msdsn.EncodeParameters{})

	// Verify type info was read correctly
	if ti.TypeId != typeJson {
		t.Errorf("Expected TypeId %#x, got %#x", typeJson, ti.TypeId)
	}

	// JSON uses PLP format, so Reader should be set to readPLPType
	if ti.Reader == nil {
		t.Error("Expected Reader to be set for JSON type")
	}

	// Verify no bytes were consumed from buffer (JSON has no metadata)
	if r.rpos != 0 {
		t.Errorf("Expected rpos=0 (no bytes consumed), got rpos=%d", r.rpos)
	}
}

// TestFeatureExtJsonSupport tests the featureExtJsonSupport struct methods.
func TestFeatureExtJsonSupport(t *testing.T) {
	f := &featureExtJsonSupport{}

	t.Run("featureID", func(t *testing.T) {
		id := f.featureID()
		if id != featExtJSONSUPPORT {
			t.Errorf("Expected featureID to be %#x, got %#x", featExtJSONSUPPORT, id)
		}
	})

	t.Run("toBytes", func(t *testing.T) {
		bytes := f.toBytes()
		if len(bytes) != 1 {
			t.Errorf("Expected toBytes to return 1 byte, got %d", len(bytes))
		}
		if bytes[0] != jsonSupportVersion {
			t.Errorf("Expected version byte %#x, got %#x", jsonSupportVersion, bytes[0])
		}
	})
}

// TestMakeParamJSON tests the makeParam function with JSON types.
// This covers the JSON, NullJSON, *JSON, and *NullJSON cases in mssql.go.
func TestMakeParamJSON(t *testing.T) {
	// Create a minimal Stmt for testing - we need sess to be non-nil for jsonSupported
	sess := &tdsSession{jsonSupported: true}
	conn := &Conn{sess: sess}
	stmt := &Stmt{c: conn}

	t.Run("JSON value", func(t *testing.T) {
		jsonVal := JSON(`{"test":"value"}`)
		param, err := stmt.makeParam(jsonVal)
		if err != nil {
			t.Fatalf("makeParam(JSON) returned error: %v", err)
		}
		// JSON uses nvarchar wire format with json type declaration
		if param.ti.TypeId != typeNVarChar {
			t.Errorf("Expected TypeId %#x (nvarchar), got %#x", typeNVarChar, param.ti.TypeId)
		}
		if param.ti.DeclTypeId != typeJson {
			t.Errorf("Expected DeclTypeId %#x (json), got %#x", typeJson, param.ti.DeclTypeId)
		}
	})

	t.Run("JSON nil value", func(t *testing.T) {
		// A nil JSON slice should be treated as SQL NULL, not empty string
		var jsonVal JSON = nil
		param, err := stmt.makeParam(jsonVal)
		if err != nil {
			t.Fatalf("makeParam(nil JSON) returned error: %v", err)
		}
		// JSON uses nvarchar wire format with json type declaration
		if param.ti.TypeId != typeNVarChar {
			t.Errorf("Expected TypeId %#x (nvarchar), got %#x", typeNVarChar, param.ti.TypeId)
		}
		if param.ti.DeclTypeId != typeJson {
			t.Errorf("Expected DeclTypeId %#x (json), got %#x", typeJson, param.ti.DeclTypeId)
		}
		// nil JSON should produce NULL (nil buffer), not empty string
		if param.buffer != nil {
			t.Error("Expected nil buffer for nil JSON value (SQL NULL)")
		}
	})

	t.Run("NullJSON with valid value", func(t *testing.T) {
		nullJSON := NullJSON{JSON: json.RawMessage(`{"valid":true}`), Valid: true}
		param, err := stmt.makeParam(nullJSON)
		if err != nil {
			t.Fatalf("makeParam(NullJSON) returned error: %v", err)
		}
		if param.ti.TypeId != typeNVarChar {
			t.Errorf("Expected TypeId %#x (nvarchar), got %#x", typeNVarChar, param.ti.TypeId)
		}
		if param.ti.DeclTypeId != typeJson {
			t.Errorf("Expected DeclTypeId %#x (json), got %#x", typeJson, param.ti.DeclTypeId)
		}
		if param.buffer == nil {
			t.Error("Expected non-nil buffer for valid NullJSON")
		}
	})

	t.Run("NullJSON with NULL value", func(t *testing.T) {
		nullJSON := NullJSON{Valid: false}
		param, err := stmt.makeParam(nullJSON)
		if err != nil {
			t.Fatalf("makeParam(NullJSON null) returned error: %v", err)
		}
		if param.ti.TypeId != typeNVarChar {
			t.Errorf("Expected TypeId %#x (nvarchar), got %#x", typeNVarChar, param.ti.TypeId)
		}
		if param.ti.DeclTypeId != typeJson {
			t.Errorf("Expected DeclTypeId %#x (json), got %#x", typeJson, param.ti.DeclTypeId)
		}
		if param.buffer != nil {
			t.Error("Expected nil buffer for NULL NullJSON")
		}
	})

	t.Run("*JSON non-nil", func(t *testing.T) {
		jsonVal := JSON(`{"pointer":"test"}`)
		// convertInputParameter unwraps *JSON to JSON value
		converted, err := convertInputParameter(&jsonVal)
		if err != nil {
			t.Fatalf("convertInputParameter(*JSON) returned error: %v", err)
		}
		param, err := stmt.makeParam(converted)
		if err != nil {
			t.Fatalf("makeParam(converted JSON) returned error: %v", err)
		}
		if param.ti.TypeId != typeNVarChar {
			t.Errorf("Expected TypeId %#x (nvarchar), got %#x", typeNVarChar, param.ti.TypeId)
		}
		if param.ti.DeclTypeId != typeJson {
			t.Errorf("Expected DeclTypeId %#x (json), got %#x", typeJson, param.ti.DeclTypeId)
		}
	})

	t.Run("*JSON nil", func(t *testing.T) {
		var nilJSON *JSON
		// convertInputParameter returns NullJSON{} for nil *JSON to preserve type
		converted, err := convertInputParameter(nilJSON)
		if err != nil {
			t.Fatalf("convertInputParameter(nil *JSON) returned error: %v", err)
		}
		param, err := stmt.makeParam(converted)
		if err != nil {
			t.Fatalf("makeParam(converted nil JSON) returned error: %v", err)
		}
		// nil *JSON should produce a JSON-typed NULL
		if param.ti.TypeId != typeNVarChar {
			t.Errorf("Expected TypeId %#x (nvarchar), got %#x", typeNVarChar, param.ti.TypeId)
		}
		if param.ti.DeclTypeId != typeJson {
			t.Errorf("Expected DeclTypeId %#x (json), got %#x", typeJson, param.ti.DeclTypeId)
		}
		if param.buffer != nil {
			t.Error("Expected nil buffer for nil *JSON")
		}
	})

	t.Run("*NullJSON non-nil", func(t *testing.T) {
		nullJSON := NullJSON{JSON: json.RawMessage(`{"pointer":"nulljson"}`), Valid: true}
		// convertInputParameter unwraps *NullJSON to NullJSON value
		converted, err := convertInputParameter(&nullJSON)
		if err != nil {
			t.Fatalf("convertInputParameter(*NullJSON) returned error: %v", err)
		}
		param, err := stmt.makeParam(converted)
		if err != nil {
			t.Fatalf("makeParam(converted NullJSON) returned error: %v", err)
		}
		if param.ti.TypeId != typeNVarChar {
			t.Errorf("Expected TypeId %#x (nvarchar), got %#x", typeNVarChar, param.ti.TypeId)
		}
		if param.ti.DeclTypeId != typeJson {
			t.Errorf("Expected DeclTypeId %#x (json), got %#x", typeJson, param.ti.DeclTypeId)
		}
	})

	t.Run("*NullJSON nil", func(t *testing.T) {
		var nilNullJSON *NullJSON
		// convertInputParameter returns NullJSON{} for nil *NullJSON to preserve type
		converted, err := convertInputParameter(nilNullJSON)
		if err != nil {
			t.Fatalf("convertInputParameter(nil *NullJSON) returned error: %v", err)
		}
		param, err := stmt.makeParam(converted)
		if err != nil {
			t.Fatalf("makeParam(converted nil NullJSON) returned error: %v", err)
		}
		// nil *NullJSON should produce a JSON-typed NULL
		if param.ti.TypeId != typeNVarChar {
			t.Errorf("Expected TypeId %#x (nvarchar), got %#x", typeNVarChar, param.ti.TypeId)
		}
		if param.ti.DeclTypeId != typeJson {
			t.Errorf("Expected DeclTypeId %#x (json), got %#x", typeJson, param.ti.DeclTypeId)
		}
		if param.buffer != nil {
			t.Error("Expected nil buffer for nil *NullJSON")
		}
	})
}

// TestMakeParamJSONWithoutServerSupport tests JSON param creation when server doesn't support JSON.
func TestMakeParamJSONWithoutServerSupport(t *testing.T) {
	// Create Stmt with jsonSupported=false
	sess := &tdsSession{jsonSupported: false}
	conn := &Conn{sess: sess}
	stmt := &Stmt{c: conn}

	t.Run("JSON without server support", func(t *testing.T) {
		jsonVal := JSON(`{"test":"fallback"}`)
		param, err := stmt.makeParam(jsonVal)
		if err != nil {
			t.Fatalf("makeParam(JSON) returned error: %v", err)
		}
		// Without server support, TypeId is nvarchar but DeclTypeId should not be set
		if param.ti.TypeId != typeNVarChar {
			t.Errorf("Expected TypeId %#x (nvarchar), got %#x", typeNVarChar, param.ti.TypeId)
		}
		if param.ti.DeclTypeId != 0 {
			t.Errorf("Expected DeclTypeId 0 without server support, got %#x", param.ti.DeclTypeId)
		}
	})

	t.Run("NullJSON without server support", func(t *testing.T) {
		nullJSON := NullJSON{JSON: json.RawMessage(`{"valid":true}`), Valid: true}
		param, err := stmt.makeParam(nullJSON)
		if err != nil {
			t.Fatalf("makeParam(NullJSON) returned error: %v", err)
		}
		if param.ti.TypeId != typeNVarChar {
			t.Errorf("Expected TypeId %#x (nvarchar), got %#x", typeNVarChar, param.ti.TypeId)
		}
		if param.ti.DeclTypeId != 0 {
			t.Errorf("Expected DeclTypeId 0 without server support, got %#x", param.ti.DeclTypeId)
		}
	})
}
