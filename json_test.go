//go:build go1.9
// +build go1.9

package mssql

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
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
	tl := testLogger{t: t}
	SetLogger(&tl)
	t.Cleanup(tl.StopLogging)

	db := requireTestDB(t)
	ctx := testContext(t)

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

		// Normalize JSON for comparison: SQL Server may reformat whitespace/key ordering
		var expectedJSON, actualJSON interface{}
		if err := json.Unmarshal(jsonValue, &expectedJSON); err != nil {
			t.Fatalf("Failed to unmarshal expected JSON: %v", err)
		}
		if err := json.Unmarshal([]byte(readJSON), &actualJSON); err != nil {
			t.Fatalf("Failed to unmarshal actual JSON: %v", err)
		}
		expBytes, err := json.Marshal(expectedJSON)
		if err != nil {
			t.Fatalf("Failed to marshal expected JSON: %v", err)
		}
		actBytes, err := json.Marshal(actualJSON)
		if err != nil {
			t.Fatalf("Failed to marshal actual JSON: %v", err)
		}
		if !bytes.Equal(expBytes, actBytes) {
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
		// Normalize JSON for comparison
		var expectedUpd, actualUpd interface{}
		if err := json.Unmarshal(newJSON, &expectedUpd); err != nil {
			t.Fatalf("Failed to unmarshal expected JSON: %v", err)
		}
		if err := json.Unmarshal(result.JSON, &actualUpd); err != nil {
			t.Fatalf("Failed to unmarshal actual JSON: %v", err)
		}
		expUpdBytes, err := json.Marshal(expectedUpd)
		if err != nil {
			t.Fatalf("Failed to marshal expected JSON: %v", err)
		}
		actUpdBytes, err := json.Marshal(actualUpd)
		if err != nil {
			t.Fatalf("Failed to marshal actual JSON: %v", err)
		}
		if !bytes.Equal(expUpdBytes, actUpdBytes) {
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

		var expectedJSON any
		if err := json.Unmarshal([]byte(testData), &expectedJSON); err != nil {
			t.Fatalf("Failed to unmarshal expected JSON: %v", err)
		}
		normalizedExpected, err := json.Marshal(expectedJSON)
		if err != nil {
			t.Fatalf("Failed to normalize expected JSON: %v", err)
		}

		var resultJSON any
		if err := json.Unmarshal([]byte(result), &resultJSON); err != nil {
			t.Fatalf("Failed to unmarshal result JSON: %v", err)
		}
		normalizedResult, err := json.Marshal(resultJSON)
		if err != nil {
			t.Fatalf("Failed to normalize result JSON: %v", err)
		}

		if !bytes.Equal(normalizedResult, normalizedExpected) {
			t.Errorf("JSON data mismatch.\nExpected: %s\nGot: %s", normalizedExpected, normalizedResult)
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

// TestJSONWireDecoding tests that JSON data received from SQL Server as UTF-16LE
// is correctly decoded to a Go UTF-8 string. SQL Server sends JSON column data
// as UTF-16LE on the wire (consistent with XML and nvarchar types).
//
// This test exercises the actual readPLPType code path by constructing a TDS buffer
// with PLP-framed UTF-16LE data and calling readPLPType with typeJson, verifying
// the full decode pipeline rather than just str2ucs2/decodeUcs2 round-trip.
func TestJSONWireDecoding(t *testing.T) {
	tests := []struct {
		name string
		json string
	}{
		{"simple", `{"key":"value"}`},
		{"complex", `{"name":"test","value":123,"nested":{"array":[1,2,3]}}`},
		{"empty", `{}`},
		{"unicode", `{"emoji":"😀","cjk":"中文"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name+"_roundtrip", func(t *testing.T) {
			// Verify str2ucs2/decodeUcs2 round-trip (baseline correctness)
			utf16Data := str2ucs2(tt.json)
			decoded := decodeUcs2(utf16Data)
			if decoded != tt.json {
				t.Errorf("Expected decoded JSON %q, got %q", tt.json, decoded)
			}
		})

		t.Run(tt.name+"_readPLPType", func(t *testing.T) {
			// Build PLP-framed data as SQL Server would send it:
			//   uint64 total size (or 0xFFFFFFFFFFFFFFFE for unknown)
			//   uint32 chunk length
			//   []byte chunk data
			//   uint32 0 (terminator)
			utf16Data := str2ucs2(tt.json)

			// Calculate total PLP frame size: 8 (total) + 4 (chunk len) + data + 4 (terminator)
			var plpBuf bytes.Buffer
			// Total length (known)
			totalLen := uint64(len(utf16Data))
			if err := binary.Write(&plpBuf, binary.LittleEndian, totalLen); err != nil {
				t.Fatalf("failed to write PLP total length: %v", err)
			}
			// Single chunk
			if err := binary.Write(&plpBuf, binary.LittleEndian, uint32(len(utf16Data))); err != nil {
				t.Fatalf("failed to write PLP chunk length: %v", err)
			}
			if _, err := plpBuf.Write(utf16Data); err != nil {
				t.Fatalf("failed to write PLP chunk data: %v", err)
			}
			// Terminator
			if err := binary.Write(&plpBuf, binary.LittleEndian, uint32(0)); err != nil {
				t.Fatalf("failed to write PLP terminator: %v", err)
			}

			frameBytes := plpBuf.Bytes()
			r := &tdsBuffer{
				packetSize: len(frameBytes) + 100,
				rbuf:       frameBytes,
				rpos:       0,
				rsize:      len(frameBytes),
			}

			ti := &typeInfo{TypeId: typeJson}
			result := readPLPType(ti, r, nil, msdsn.EncodeParameters{})
			if result == nil {
				t.Fatal("readPLPType returned nil")
			}
			str, ok := result.(string)
			if !ok {
				t.Fatalf("readPLPType returned %T, expected string", result)
			}
			if str != tt.json {
				t.Errorf("readPLPType decoded %q, expected %q", str, tt.json)
			}
		})
	}

	t.Run("null_readPLPType", func(t *testing.T) {
		// PLP NULL sentinel: uint64(0xFFFFFFFFFFFFFFFF)
		var plpBuf bytes.Buffer
		if err := binary.Write(&plpBuf, binary.LittleEndian, uint64(0xFFFFFFFFFFFFFFFF)); err != nil {
			t.Fatalf("failed to write PLP NULL sentinel: %v", err)
		}

		frameBytes := plpBuf.Bytes()
		r := &tdsBuffer{
			packetSize: len(frameBytes) + 100,
			rbuf:       frameBytes,
			rpos:       0,
			rsize:      len(frameBytes),
		}

		ti := &typeInfo{TypeId: typeJson}
		result := readPLPType(ti, r, nil, msdsn.EncodeParameters{})
		if result != nil {
			t.Errorf("readPLPType for NULL should return nil, got %v", result)
		}
	})
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
		expectedLength := int64(2147483645) // consistent with varchar(max) length metadata
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

// TestParseFeatureExtAckJSON tests that parseFeatureExtAck correctly parses
// a JSON support acknowledgement from the server's feature ext ack response.
func TestParseFeatureExtAckJSON(t *testing.T) {
	t.Run("JSON ack with version 1", func(t *testing.T) {
		// Wire format: feature_id(1) + data_length(4) + data(1) + terminator(1)
		// 0x0D = featExtJSONSUPPORT, length=1, version=0x01, 0xFF=terminator
		data := []byte{0x0D, 0x01, 0x00, 0x00, 0x00, 0x01, 0xFF}
		r := &tdsBuffer{
			packetSize: len(data) + 10,
			rbuf:       data,
			rpos:       0,
			rsize:      len(data),
		}
		ack := parseFeatureExtAck(r)
		v, ok := ack[featExtJSONSUPPORT]
		if !ok {
			t.Fatal("Expected featExtJSONSUPPORT in ack map")
		}
		version, ok := v.(byte)
		if !ok {
			t.Fatalf("Expected byte value, got %T", v)
		}
		if version != jsonSupportVersion {
			t.Errorf("Expected version %#x, got %#x", jsonSupportVersion, version)
		}
	})

	t.Run("JSON ack with zero length (malformed)", func(t *testing.T) {
		// Malformed: feature_id=0x0D, length=0, terminator
		// Should silently skip, resulting in no JSON entry in the ack map.
		data := []byte{0x0D, 0x00, 0x00, 0x00, 0x00, 0xFF}
		r := &tdsBuffer{
			packetSize: len(data) + 10,
			rbuf:       data,
			rpos:       0,
			rsize:      len(data),
		}
		ack := parseFeatureExtAck(r)
		if _, ok := ack[featExtJSONSUPPORT]; ok {
			t.Error("Expected no featExtJSONSUPPORT entry for zero-length ack")
		}
	})

	t.Run("JSON ack combined with column encryption", func(t *testing.T) {
		// Column encryption (0x04) ack with version=1, no enclave, then JSON ack
		data := []byte{
			// Column encryption: feature=0x04, length=1, version=1
			0x04, 0x01, 0x00, 0x00, 0x00, 0x01,
			// JSON support: feature=0x0D, length=1, version=1
			0x0D, 0x01, 0x00, 0x00, 0x00, 0x01,
			// Terminator
			0xFF,
		}
		r := &tdsBuffer{
			packetSize: len(data) + 10,
			rbuf:       data,
			rpos:       0,
			rsize:      len(data),
		}
		ack := parseFeatureExtAck(r)
		if _, ok := ack[featExtJSONSUPPORT]; !ok {
			t.Error("Expected featExtJSONSUPPORT in ack map")
		}
		if _, ok := ack[featExtCOLUMNENCRYPTION]; !ok {
			t.Error("Expected featExtCOLUMNENCRYPTION in ack map")
		}
	})
}

// TestProcessFeatureExtAckJSON tests that processFeatureExtAck correctly sets
// sess.jsonSupported when the server acknowledges JSON support.
func TestProcessFeatureExtAckJSON(t *testing.T) {
	t.Run("JSON version 1 enables support", func(t *testing.T) {
		sess := &tdsSession{}
		ack := featureExtAck{featExtJSONSUPPORT: byte(0x01)}
		sess.processFeatureExtAck(ack)
		if !sess.jsonSupported {
			t.Error("Expected jsonSupported to be true after version 1 ack")
		}
	})

	t.Run("JSON version 0 does not enable support", func(t *testing.T) {
		sess := &tdsSession{}
		ack := featureExtAck{featExtJSONSUPPORT: byte(0x00)}
		sess.processFeatureExtAck(ack)
		if sess.jsonSupported {
			t.Error("Expected jsonSupported to be false for version 0")
		}
	})

	t.Run("wrong type does not enable support", func(t *testing.T) {
		sess := &tdsSession{}
		ack := featureExtAck{featExtJSONSUPPORT: "not a byte"}
		sess.processFeatureExtAck(ack)
		if sess.jsonSupported {
			t.Error("Expected jsonSupported to be false for wrong type")
		}
	})

	t.Run("empty ack does not enable support", func(t *testing.T) {
		sess := &tdsSession{}
		ack := featureExtAck{}
		sess.processFeatureExtAck(ack)
		if sess.jsonSupported {
			t.Error("Expected jsonSupported to be false for empty ack")
		}
	})

	t.Run("combined with column encryption", func(t *testing.T) {
		sess := &tdsSession{aeSettings: &alwaysEncryptedSettings{}}
		ack := featureExtAck{
			featExtJSONSUPPORT:      byte(0x01),
			featExtCOLUMNENCRYPTION: colAckStruct{Version: 1, EnclaveType: "VBS"},
		}
		sess.processFeatureExtAck(ack)
		if !sess.jsonSupported {
			t.Error("Expected jsonSupported to be true")
		}
		if !sess.alwaysEncrypted {
			t.Error("Expected alwaysEncrypted to be true")
		}
		if sess.aeSettings.enclaveType != "VBS" {
			t.Errorf("Expected enclaveType 'VBS', got %q", sess.aeSettings.enclaveType)
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
		// With server JSON support, uses native JSON type with UTF-8 encoding
		if param.ti.TypeId != typeJson {
			t.Errorf("Expected TypeId %#x (json), got %#x", typeJson, param.ti.TypeId)
		}
		// Buffer should be UTF-8 encoded
		expected := []byte(`{"test":"value"}`)
		if !bytes.Equal(param.buffer, expected) {
			t.Errorf("Expected UTF-8 buffer %v, got %v", expected, param.buffer)
		}
	})

	t.Run("JSON nil value", func(t *testing.T) {
		// A nil JSON slice should be treated as SQL NULL, not empty string
		var jsonVal JSON = nil
		param, err := stmt.makeParam(jsonVal)
		if err != nil {
			t.Fatalf("makeParam(nil JSON) returned error: %v", err)
		}
		if param.ti.TypeId != typeJson {
			t.Errorf("Expected TypeId %#x (json), got %#x", typeJson, param.ti.TypeId)
		}
		// nil JSON should produce NULL (nil buffer), not empty string
		if param.buffer != nil {
			t.Error("Expected nil buffer for nil JSON value (SQL NULL)")
		}
	})

	t.Run("JSON empty value", func(t *testing.T) {
		jsonVal := JSON("")
		param, err := stmt.makeParam(jsonVal)
		if err != nil {
			t.Fatalf("makeParam(JSON empty) returned error: %v", err)
		}
		if param.ti.TypeId != typeJson {
			t.Errorf("Expected TypeId %#x (json), got %#x", typeJson, param.ti.TypeId)
		}
		if param.buffer == nil {
			t.Error("Expected non-nil buffer for empty valid JSON")
		}
		if len(param.buffer) != 0 {
			t.Errorf("Expected empty buffer, got %d bytes", len(param.buffer))
		}
	})

	t.Run("NullJSON with valid value", func(t *testing.T) {
		nullJSON := NullJSON{JSON: json.RawMessage(`{"valid":true}`), Valid: true}
		param, err := stmt.makeParam(nullJSON)
		if err != nil {
			t.Fatalf("makeParam(NullJSON) returned error: %v", err)
		}
		if param.ti.TypeId != typeJson {
			t.Errorf("Expected TypeId %#x (json), got %#x", typeJson, param.ti.TypeId)
		}
		if param.buffer == nil {
			t.Error("Expected non-nil buffer for valid NullJSON")
		}
		// Buffer should be UTF-8 encoded
		expected := []byte(`{"valid":true}`)
		if !bytes.Equal(param.buffer, expected) {
			t.Errorf("Expected UTF-8 buffer %v, got %v", expected, param.buffer)
		}
	})

	t.Run("NullJSON with NULL value", func(t *testing.T) {
		nullJSON := NullJSON{Valid: false}
		param, err := stmt.makeParam(nullJSON)
		if err != nil {
			t.Fatalf("makeParam(NullJSON null) returned error: %v", err)
		}
		if param.ti.TypeId != typeJson {
			t.Errorf("Expected TypeId %#x (json), got %#x", typeJson, param.ti.TypeId)
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
		if param.ti.TypeId != typeJson {
			t.Errorf("Expected TypeId %#x (json), got %#x", typeJson, param.ti.TypeId)
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
		if param.ti.TypeId != typeJson {
			t.Errorf("Expected TypeId %#x (json), got %#x", typeJson, param.ti.TypeId)
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
		if param.ti.TypeId != typeJson {
			t.Errorf("Expected TypeId %#x (json), got %#x", typeJson, param.ti.TypeId)
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
		if param.ti.TypeId != typeJson {
			t.Errorf("Expected TypeId %#x (json), got %#x", typeJson, param.ti.TypeId)
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
		if param.ti.TypeId != typeNVarChar {
			t.Errorf("Expected TypeId %#x (nvarchar), got %#x", typeNVarChar, param.ti.TypeId)
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
	})
}

// TestJSONEmptyNonNil tests that JSON("") (empty non-nil) is sent as a non-NULL
// empty PLP payload. The server is responsible for validating JSON content;
// the driver does not silently coerce empty-but-non-nil values to SQL NULL.
func TestJSONEmptyNonNil(t *testing.T) {
	sess := &tdsSession{jsonSupported: true}
	conn := &Conn{sess: sess}
	stmt := &Stmt{c: conn}

	t.Run("empty JSON produces non-NULL empty buffer", func(t *testing.T) {
		jsonVal := JSON("")
		param, err := stmt.makeParam(jsonVal)
		if err != nil {
			t.Fatalf("makeParam(JSON(\"\")) returned error: %v", err)
		}
		if param.ti.TypeId != typeJson {
			t.Errorf("Expected TypeId %#x (json), got %#x", typeJson, param.ti.TypeId)
		}
		if param.buffer == nil {
			t.Error("Expected non-nil buffer for empty-but-non-nil JSON")
		}
		if len(param.buffer) != 0 {
			t.Errorf("Expected empty buffer, got %d bytes", len(param.buffer))
		}
	})

	t.Run("empty JSON via convertInputParameter", func(t *testing.T) {
		jsonVal := JSON("")
		converted, err := convertInputParameter(jsonVal)
		if err != nil {
			t.Fatalf("convertInputParameter(JSON(\"\")) returned error: %v", err)
		}
		if _, ok := converted.(JSON); !ok {
			t.Errorf("Expected JSON type after convertInputParameter, got %T", converted)
		}
	})
}

// TestBulkCopyJSONMakeParam tests the Bulk.makeParam function for JSON columns.
// In BulkCopy, JSON columns are converted to typeNVarChar in the metadata step,
// so data flows through the nvarchar case. String values are encoded as UTF-16LE;
// []byte values are passed through unchanged as already-encoded NVARCHAR data.
func TestBulkCopyJSONMakeParam(t *testing.T) {
	b := &Bulk{}
	// BulkCopy converts typeJson to typeNVarChar for wire metadata
	col := columnStruct{
		ti: typeInfo{TypeId: typeNVarChar},
	}

	t.Run("string value", func(t *testing.T) {
		param, err := b.makeParam(`{"key":"value"}`, col)
		if err != nil {
			t.Fatalf("Bulk.makeParam(string) returned error: %v", err)
		}
		// JSON bulk copy uses UTF-16LE encoding (string → UCS-2/UTF-16LE bytes)
		expected := str2ucs2(`{"key":"value"}`)
		if !bytes.Equal(param.buffer, expected) {
			t.Errorf("Expected UTF-16LE buffer %v, got %v", expected, param.buffer)
		}
		if param.ti.Size != len(expected) {
			t.Errorf("Expected ti.Size %d, got %d", len(expected), param.ti.Size)
		}
	})

	t.Run("[]byte value", func(t *testing.T) {
		// For nvarchar bulk copy, []byte is treated as already UTF-16LE/UCS-2 encoded.
		raw := str2ucs2(`{"key":"bytes"}`)
		param, err := b.makeParam(raw, col)
		if err != nil {
			t.Fatalf("Bulk.makeParam([]byte) returned error: %v", err)
		}
		if !bytes.Equal(param.buffer, raw) {
			t.Errorf("Expected pre-encoded buffer %v, got %v", raw, param.buffer)
		}
	})

	t.Run("JSON type value", func(t *testing.T) {
		jsonVal := JSON(`{"key":"json_type"}`)
		param, err := b.makeParam(jsonVal, col)
		if err != nil {
			t.Fatalf("Bulk.makeParam(JSON) returned error: %v", err)
		}
		expected := str2ucs2(`{"key":"json_type"}`)
		if !bytes.Equal(param.buffer, expected) {
			t.Errorf("Expected UTF-16LE buffer %v, got %v", expected, param.buffer)
		}
		if param.ti.Size != len(expected) {
			t.Errorf("Expected ti.Size %d, got %d", len(expected), param.ti.Size)
		}
	})

	t.Run("json.RawMessage value", func(t *testing.T) {
		rawMsg := json.RawMessage(`{"key":"raw_message"}`)
		param, err := b.makeParam(rawMsg, col)
		if err != nil {
			t.Fatalf("Bulk.makeParam(json.RawMessage) returned error: %v", err)
		}
		expected := str2ucs2(`{"key":"raw_message"}`)
		if !bytes.Equal(param.buffer, expected) {
			t.Errorf("Expected UTF-16LE buffer %v, got %v", expected, param.buffer)
		}
		if param.ti.Size != len(expected) {
			t.Errorf("Expected ti.Size %d, got %d", len(expected), param.ti.Size)
		}
	})

	t.Run("nil JSON produces NULL", func(t *testing.T) {
		param, err := b.makeParam(JSON(nil), col)
		if err != nil {
			t.Fatalf("Bulk.makeParam(JSON(nil)) returned error: %v", err)
		}
		if param.buffer != nil {
			t.Errorf("Expected nil buffer for JSON(nil), got %v", param.buffer)
		}
	})

	t.Run("nil json.RawMessage produces NULL", func(t *testing.T) {
		param, err := b.makeParam(json.RawMessage(nil), col)
		if err != nil {
			t.Fatalf("Bulk.makeParam(json.RawMessage(nil)) returned error: %v", err)
		}
		if param.buffer != nil {
			t.Errorf("Expected nil buffer for json.RawMessage(nil), got %v", param.buffer)
		}
	})

	t.Run("unsupported type", func(t *testing.T) {
		_, err := b.makeParam(complex(1, 2), col)
		if err == nil {
			t.Error("Expected error for unsupported type in nvarchar bulk copy")
		}
	})
}

// TestBulkCopyJSONIntegration tests BulkCopy with native JSON columns on SQL Server 2025+.
// Verifies the full pipeline: sendBulkCommand converts JSON to nvarchar(max),
// string data is encoded as UTF-16LE nvarchar, and SQL Server converts to JSON for storage.
func TestBulkCopyJSONIntegration(t *testing.T) {
	jtc := setupJSONTest(t, true) // requires native JSON

	// Use a single connection so the temp table is visible to all operations.
	conn := jtc.conn()

	tableName := "#test_bulkcopy_json"
	_, err := conn.ExecContext(jtc.ctx, "CREATE TABLE "+tableName+" (id int, data json)")
	if err != nil {
		t.Fatalf("CREATE TABLE failed: %v", err)
	}

	// Prepare test data - includes a NULL row to verify the NULL path
	testRows := []struct {
		id   int
		data interface{} // nil for NULL
	}{
		{1, `{"name":"alice","age":30}`},
		{2, `{"name":"bob","scores":[100,95,87]}`},
		{3, nil}, // NULL JSON value
		{4, `{"emoji":"😀","cjk":"中文","mixed":"hello 世界"}`},
	}

	// BulkCopy insert via transaction on the pinned connection
	txn, err := conn.BeginTx(jtc.ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx failed: %v", err)
	}

	stmt, err := txn.PrepareContext(jtc.ctx, CopyIn(tableName, BulkOptions{}, "id", "data"))
	if err != nil {
		t.Fatalf("PrepareContext CopyIn failed: %v", err)
	}

	for _, row := range testRows {
		_, err = stmt.ExecContext(jtc.ctx, row.id, row.data)
		if err != nil {
			t.Fatalf("Exec row %d failed: %v", row.id, err)
		}
	}
	_, err = stmt.ExecContext(jtc.ctx) // flush
	if err != nil {
		t.Fatalf("Exec flush failed: %v", err)
	}
	stmt.Close()
	err = txn.Commit()
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// Read back and verify on the same connection
	rows, err := conn.QueryContext(jtc.ctx, "SELECT id, data FROM "+tableName+" ORDER BY id")
	if err != nil {
		t.Fatalf("SELECT failed: %v", err)
	}
	defer rows.Close()

	idx := 0
	for rows.Next() {
		var id int
		var data sql.NullString
		if err := rows.Scan(&id, &data); err != nil {
			t.Fatalf("Scan row %d failed: %v", idx, err)
		}
		if idx >= len(testRows) {
			t.Fatalf("More rows returned than expected")
		}
		if id != testRows[idx].id {
			t.Errorf("Row %d: expected id %d, got %d", idx, testRows[idx].id, id)
		}
		if testRows[idx].data == nil {
			// Expect NULL
			if data.Valid {
				t.Errorf("Row %d: expected NULL, got %s", idx, data.String)
			}
		} else {
			if !data.Valid {
				t.Errorf("Row %d: expected non-NULL value, got NULL", idx)
			} else {
				// Normalize whitespace for comparison: SQL Server may reformat JSON
				var expected, actual interface{}
				if err := json.Unmarshal([]byte(testRows[idx].data.(string)), &expected); err != nil {
					t.Fatalf("Row %d: failed to unmarshal expected JSON %q: %v", idx, testRows[idx].data.(string), err)
				}
				if err := json.Unmarshal([]byte(data.String), &actual); err != nil {
					t.Fatalf("Row %d: failed to unmarshal actual JSON %q: %v", idx, data.String, err)
				}
				expectedBytes, err := json.Marshal(expected)
				if err != nil {
					t.Fatalf("Row %d: failed to marshal expected JSON value: %v", idx, err)
				}
				actualBytes, err := json.Marshal(actual)
				if err != nil {
					t.Fatalf("Row %d: failed to marshal actual JSON value: %v", idx, err)
				}
				if !bytes.Equal(expectedBytes, actualBytes) {
					t.Errorf("Row %d: JSON mismatch\n  expected: %s\n  actual:   %s", idx, testRows[idx].data, data.String)
				}
			}
		}
		idx++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows iteration error: %v", err)
	}
	if idx != len(testRows) {
		t.Errorf("Expected %d rows, got %d", len(testRows), idx)
	}
}

// TestJSONOutputParameterViaNvarchar tests JSON content passed through nvarchar
// output parameters from stored procedures. The procedure uses nvarchar(max)
// parameters because stored procedure JSON type parameters are a separate feature.
func TestJSONOutputParameterViaNvarchar(t *testing.T) {
	jtc := setupJSONTest(t, true) // requires native JSON

	// Use a single connection so the temp stored procedure is visible.
	conn := jtc.conn()

	// Create a stored procedure that outputs JSON via an nvarchar output param.
	procName := "#test_json_output_proc"
	_, err := conn.ExecContext(jtc.ctx, `
		CREATE PROCEDURE `+procName+` @input nvarchar(max), @output nvarchar(max) OUTPUT
		AS
		BEGIN
			SET @output = JSON_MODIFY(@input, '$.added', 'by_proc')
		END
	`)
	if err != nil {
		t.Fatalf("CREATE PROCEDURE failed: %v", err)
	}

	var output string
	_, err = conn.ExecContext(jtc.ctx, procName,
		sql.Named("input", `{"key":"value"}`),
		sql.Named("output", sql.Out{Dest: &output}),
	)
	if err != nil {
		t.Fatalf("ExecContext failed: %v", err)
	}

	// Verify the output contains the modification
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("Failed to parse output JSON %q: %v", output, err)
	}
	if result["key"] != "value" {
		t.Errorf("Expected key=value, got key=%v", result["key"])
	}
	if result["added"] != "by_proc" {
		t.Errorf("Expected added=by_proc, got added=%v", result["added"])
	}
}

func TestJSONScan(t *testing.T) {
	t.Run("scan from string", func(t *testing.T) {
		var j JSON
		err := j.Scan(`{"key":"value"}`)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(j, JSON(`{"key":"value"}`)) {
			t.Errorf("got %s", string(j))
		}
	})
	t.Run("scan from []byte", func(t *testing.T) {
		var j JSON
		src := []byte(`{"key":"value"}`)
		err := j.Scan(src)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(j, JSON(`{"key":"value"}`)) {
			t.Errorf("got %s", string(j))
		}
		// Verify it's a copy, not a reference to the original slice
		src[0] = 'X'
		if j[0] == 'X' {
			t.Error("Scan did not copy []byte data")
		}
	})
	t.Run("scan from json.RawMessage", func(t *testing.T) {
		var j JSON
		src := json.RawMessage(`{"key":"value"}`)
		err := j.Scan(src)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(j, JSON(`{"key":"value"}`)) {
			t.Errorf("got %s", string(j))
		}
		// Verify it's a copy, not a reference to the original slice
		src[0] = 'X'
		if j[0] == 'X' {
			t.Error("Scan did not copy json.RawMessage data")
		}
	})
	t.Run("scan nil value", func(t *testing.T) {
		var j JSON
		err := j.Scan(nil)
		if err == nil {
			t.Error("expected error scanning nil")
		}
	})
	t.Run("scan unsupported type", func(t *testing.T) {
		var j JSON
		err := j.Scan(123)
		if err == nil {
			t.Error("expected error scanning int")
		}
	})
	t.Run("scan on nil pointer", func(t *testing.T) {
		var j *JSON
		err := j.Scan(`{"key":"value"}`)
		if err == nil {
			t.Error("expected error scanning into nil pointer")
		}
	})
}

// TestUnmarshalJSONNilPointer tests calling UnmarshalJSON on a nil *JSON pointer.
// This exercises the defensive nil-pointer check in UnmarshalJSON.
func TestUnmarshalJSONNilPointer(t *testing.T) {
	var j *JSON
	err := j.UnmarshalJSON([]byte(`{"key":"value"}`))
	if err == nil {
		t.Fatal("expected error for UnmarshalJSON on nil pointer")
	}
	if !strings.Contains(err.Error(), "nil pointer") {
		t.Errorf("expected nil pointer error, got: %v", err)
	}
}

// TestMakeJsonParamFallbackEmptyData tests the nvarchar fallback path in makeJsonParam
// with empty but valid JSON data. This covers the len(data)==0 branch when jsonSupported=false.
func TestMakeJsonParamFallbackEmptyData(t *testing.T) {
	sess := &tdsSession{jsonSupported: false}
	conn := &Conn{sess: sess}
	stmt := &Stmt{c: conn}

	// Empty non-nil JSON: valid=true, len(data)==0
	jsonVal := JSON("")
	param, err := stmt.makeParam(jsonVal)
	if err != nil {
		t.Fatalf("makeParam(JSON) returned error: %v", err)
	}
	if param.ti.TypeId != typeNVarChar {
		t.Errorf("Expected TypeId %#x (nvarchar), got %#x", typeNVarChar, param.ti.TypeId)
	}
	if param.buffer == nil {
		t.Error("Expected non-nil buffer for empty valid JSON in fallback path")
	}
	if len(param.buffer) != 0 {
		t.Errorf("Expected empty buffer, got %d bytes", len(param.buffer))
	}
}

// TestMakeJsonParamFallbackNonEmptyData tests the nvarchar fallback path in makeJsonParam
// with non-empty valid JSON data. This covers the str2ucs2 branch when jsonSupported=false.
func TestMakeJsonParamFallbackNonEmptyData(t *testing.T) {
	sess := &tdsSession{jsonSupported: false}
	conn := &Conn{sess: sess}
	stmt := &Stmt{c: conn}

	jsonVal := JSON(`{"key":"value"}`)
	param, err := stmt.makeParam(jsonVal)
	if err != nil {
		t.Fatalf("makeParam(JSON) returned error: %v", err)
	}
	if param.ti.TypeId != typeNVarChar {
		t.Errorf("Expected TypeId %#x (nvarchar), got %#x", typeNVarChar, param.ti.TypeId)
	}
	expected := str2ucs2(`{"key":"value"}`)
	if !bytes.Equal(param.buffer, expected) {
		t.Errorf("Expected UTF-16LE buffer, got %v", param.buffer)
	}
}

// TestWriteVarLenJSON tests that writeVarLen with typeJson sets the PLP writer
// without writing a size prefix. JSON TYPE_INFO has no USHORTMAXLEN field.
func TestWriteVarLenJSON(t *testing.T) {
	var buf bytes.Buffer
	ti := &typeInfo{TypeId: typeJson}
	err := writeVarLen(&buf, ti, false, msdsn.EncodeParameters{})
	if err != nil {
		t.Fatalf("writeVarLen(typeJson) returned error: %v", err)
	}
	// writeVarLen for typeJson should write nothing to the buffer (no size prefix)
	if buf.Len() != 0 {
		t.Errorf("Expected no bytes written for typeJson, got %d bytes: %v", buf.Len(), buf.Bytes())
	}
	// It should set ti.Writer to writePLPType
	if ti.Writer == nil {
		t.Error("Expected ti.Writer to be set after writeVarLen(typeJson)")
	}
}

// TestFeatureExtsToBytesOrdering tests that featureExts.toBytes() produces deterministic
// output by sorting feature IDs. This exercises the sort-based serialization logic
// added to ensure reproducible login packets.
func TestFeatureExtsToBytesOrdering(t *testing.T) {
	t.Run("empty features", func(t *testing.T) {
		var fe featureExts
		result := fe.toBytes()
		if result != nil {
			t.Errorf("Expected nil for empty features, got %v", result)
		}
	})

	t.Run("single feature", func(t *testing.T) {
		var fe featureExts
		_ = fe.Add(&featureExtJsonSupport{})
		result := fe.toBytes()
		if result == nil {
			t.Fatal("Expected non-nil result")
		}
		// Should be: featureID(1) + dataLen(4) + data(1) + terminator(1) = 7 bytes
		if len(result) != 7 {
			t.Fatalf("Expected 7 bytes, got %d: %v", len(result), result)
		}
		if result[0] != featExtJSONSUPPORT {
			t.Errorf("Expected feature ID %#x, got %#x", featExtJSONSUPPORT, result[0])
		}
		dataLen := binary.LittleEndian.Uint32(result[1:5])
		if dataLen != 1 {
			t.Errorf("Expected data length 1, got %d", dataLen)
		}
		if result[5] != jsonSupportVersion {
			t.Errorf("Expected version %#x, got %#x", jsonSupportVersion, result[5])
		}
		if result[6] != 0xFF {
			t.Errorf("Expected terminator 0xFF, got %#x", result[6])
		}
	})

	t.Run("deterministic ordering with multiple features", func(t *testing.T) {
		// Column encryption (0x04) should always come before JSON (0x0D) in output
		var fe featureExts
		// Add in reverse order to verify sorting
		_ = fe.Add(&featureExtJsonSupport{})      // 0x0D
		_ = fe.Add(&featureExtColumnEncryption{}) // 0x04

		result := fe.toBytes()
		if result == nil {
			t.Fatal("Expected non-nil result")
		}

		// First feature should be column encryption (0x04)
		if result[0] != featExtCOLUMNENCRYPTION {
			t.Errorf("Expected first feature ID %#x (column encryption), got %#x", featExtCOLUMNENCRYPTION, result[0])
		}

		// Find the second feature header after the first feature's data
		firstDataLen := binary.LittleEndian.Uint32(result[1:5])
		secondFeatureOffset := 5 + int(firstDataLen)
		if secondFeatureOffset >= len(result) {
			t.Fatalf("Second feature offset %d exceeds result length %d", secondFeatureOffset, len(result))
		}
		if result[secondFeatureOffset] != featExtJSONSUPPORT {
			t.Errorf("Expected second feature ID %#x (JSON), got %#x", featExtJSONSUPPORT, result[secondFeatureOffset])
		}

		// Last byte should be terminator
		if result[len(result)-1] != 0xFF {
			t.Errorf("Expected terminator 0xFF, got %#x", result[len(result)-1])
		}

		// Run multiple times to verify determinism (map iteration is random)
		for i := 0; i < 10; i++ {
			again := fe.toBytes()
			if !bytes.Equal(result, again) {
				t.Errorf("Iteration %d produced different output: %v vs %v", i, result, again)
			}
		}
	})
}

// TestBulkCopyJSONTypeRemapping tests that remapBulkColumnType correctly converts
// JSON columns to NVARCHAR(max) for INSERT BULK operations.
func TestBulkCopyJSONTypeRemapping(t *testing.T) {
	tests := []struct {
		name     string
		input    typeInfo
		wantType byte
		wantSize int
	}{
		{"JSON to NVARCHAR(max)", typeInfo{TypeId: typeJson, Size: 100}, typeNVarChar, 0},
		{"XML to NVARCHAR", typeInfo{TypeId: typeXml, Size: 200}, typeNVarChar, 200},
		{"UDT to VARBINARY", typeInfo{TypeId: typeUdt, Size: 300}, typeBigVarBin, 300},
		{"NVARCHAR unchanged", typeInfo{TypeId: typeNVarChar, Size: 50}, typeNVarChar, 50},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ti := tt.input
			remapBulkColumnType(&ti)
			if ti.TypeId != tt.wantType {
				t.Errorf("TypeId = %#x, want %#x", ti.TypeId, tt.wantType)
			}
			if int(ti.Size) != tt.wantSize {
				t.Errorf("Size = %d, want %d", ti.Size, tt.wantSize)
			}
		})
	}

	// Verify makeParam works with a JSON-remapped column
	col := columnStruct{ColName: "data", ti: typeInfo{TypeId: typeJson, Size: 100}}
	remapBulkColumnType(&col.ti)
	b := &Bulk{}
	param, err := b.makeParam(`{"key":"value"}`, col)
	if err != nil {
		t.Fatalf("Bulk.makeParam after remapping returned error: %v", err)
	}
	expected := str2ucs2(`{"key":"value"}`)
	if !bytes.Equal(param.buffer, expected) {
		t.Errorf("Expected UTF-16LE buffer after remapping")
	}
}
