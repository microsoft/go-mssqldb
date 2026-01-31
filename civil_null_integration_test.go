package mssql

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/golang-sql/civil"
)

// TestNullCivilTypesIntegration tests the nullable civil types with actual database operations
// This test requires a SQL Server connection
func TestNullCivilTypesIntegration(t *testing.T) {
	checkConnStr(t)

	tl := testLogger{t: t}
	defer tl.StopLogging()

	conn, logger := open(t)
	defer conn.Close()
	defer logger.StopLogging()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Test civil null types as OUT parameters
	t.Run("OUT parameters", func(t *testing.T) {
		// Test NullDate OUT parameter
		t.Run("NullDate", func(t *testing.T) {
			var nullDate NullDate

			// Test NULL value
			_, err := conn.ExecContext(ctx, "SELECT @p1 = NULL", sql.Out{Dest: &nullDate})
			if err != nil {
				t.Fatalf("Failed to execute query with NULL: %v", err)
			}
			if nullDate.Valid {
				t.Error("Expected NullDate to be invalid (NULL)")
			}

			// Test valid value
			_, err = conn.ExecContext(ctx, "SELECT @p1 = '2023-12-25'", sql.Out{Dest: &nullDate})
			if err != nil {
				t.Fatalf("Failed to execute query with date: %v", err)
			}
			if !nullDate.Valid {
				t.Error("Expected NullDate to be valid")
			}
			expectedDate := civil.Date{Year: 2023, Month: time.December, Day: 25}
			if nullDate.Date != expectedDate {
				t.Errorf("Expected %v, got %v", expectedDate, nullDate.Date)
			}
		})

		// Test NullDateTime OUT parameter
		t.Run("NullDateTime", func(t *testing.T) {
			var nullDateTime NullDateTime

			// Test NULL value
			_, err := conn.ExecContext(ctx, "SELECT @p1 = NULL", sql.Out{Dest: &nullDateTime})
			if err != nil {
				t.Fatalf("Failed to execute query with NULL: %v", err)
			}
			if nullDateTime.Valid {
				t.Error("Expected NullDateTime to be invalid (NULL)")
			}

			// Test valid value
			_, err = conn.ExecContext(ctx, "SELECT @p1 = '2023-12-25 14:30:45'", sql.Out{Dest: &nullDateTime})
			if err != nil {
				t.Fatalf("Failed to execute query with datetime: %v", err)
			}
			if !nullDateTime.Valid {
				t.Error("Expected NullDateTime to be valid")
			}
			// Check that the date and time components are correct
			if nullDateTime.DateTime.Date.Year != 2023 ||
				nullDateTime.DateTime.Date.Month != time.December ||
				nullDateTime.DateTime.Date.Day != 25 ||
				nullDateTime.DateTime.Time.Hour != 14 ||
				nullDateTime.DateTime.Time.Minute != 30 ||
				nullDateTime.DateTime.Time.Second != 45 {
				t.Errorf("Unexpected datetime value: %v", nullDateTime.DateTime)
			}
		})

		// Test NullTime OUT parameter
		t.Run("NullTime", func(t *testing.T) {
			var nullTime NullTime

			// Test NULL value
			_, err := conn.ExecContext(ctx, "SELECT @p1 = NULL", sql.Out{Dest: &nullTime})
			if err != nil {
				t.Fatalf("Failed to execute query with NULL: %v", err)
			}
			if nullTime.Valid {
				t.Error("Expected NullTime to be invalid (NULL)")
			}

			// Test valid value
			_, err = conn.ExecContext(ctx, "SELECT @p1 = '14:30:45'", sql.Out{Dest: &nullTime})
			if err != nil {
				t.Fatalf("Failed to execute query with time: %v", err)
			}
			if !nullTime.Valid {
				t.Error("Expected NullTime to be valid")
			}
			if nullTime.Time.Hour != 14 || nullTime.Time.Minute != 30 || nullTime.Time.Second != 45 {
				t.Errorf("Expected time 14:30:45, got %02d:%02d:%02d",
					nullTime.Time.Hour, nullTime.Time.Minute, nullTime.Time.Second)
			}
		})
	})

	// Test civil null types as input parameters
	t.Run("Input parameters", func(t *testing.T) {
		// Test NullDate input parameter
		t.Run("NullDate", func(t *testing.T) {
			// Test NULL value
			nullDate := NullDate{Valid: false}
			var result *time.Time
			err := conn.QueryRowContext(ctx, "SELECT @p1", nullDate).Scan(&result)
			if err != nil {
				t.Fatalf("Failed to query with NULL NullDate: %v", err)
			}
			if result != nil {
				t.Error("Expected result to be nil for NULL input")
			}

			// Test valid value
			nullDate = NullDate{Date: civil.Date{Year: 2023, Month: time.December, Day: 25}, Valid: true}
			err = conn.QueryRowContext(ctx, "SELECT @p1", nullDate).Scan(&result)
			if err != nil {
				t.Fatalf("Failed to query with valid NullDate: %v", err)
			}
			if result == nil {
				t.Error("Expected result to be non-nil for valid input")
			} else {
				expectedTime := time.Date(2023, time.December, 25, 0, 0, 0, 0, result.Location())
				if !result.Equal(expectedTime) {
					t.Errorf("Expected %v, got %v", expectedTime, *result)
				}
			}
		})

		// Test NullDateTime input parameter
		t.Run("NullDateTime", func(t *testing.T) {
			// Test NULL value
			nullDateTime := NullDateTime{Valid: false}
			var result *time.Time
			err := conn.QueryRowContext(ctx, "SELECT @p1", nullDateTime).Scan(&result)
			if err != nil {
				t.Fatalf("Failed to query with NULL NullDateTime: %v", err)
			}
			if result != nil {
				t.Error("Expected result to be nil for NULL input")
			}

			// Test valid value
			testTime := time.Date(2023, time.December, 25, 14, 30, 45, 0, time.UTC)
			nullDateTime = NullDateTime{DateTime: civil.DateTimeOf(testTime), Valid: true}
			err = conn.QueryRowContext(ctx, "SELECT @p1", nullDateTime).Scan(&result)
			if err != nil {
				t.Fatalf("Failed to query with valid NullDateTime: %v", err)
			}
			if result == nil {
				t.Error("Expected result to be non-nil for valid input")
			}
		})

		// Test NullTime input parameter
		t.Run("NullTime", func(t *testing.T) {
			// Test NULL value
			nullTime := NullTime{Valid: false}
			var result *time.Time
			err := conn.QueryRowContext(ctx, "SELECT @p1", nullTime).Scan(&result)
			if err != nil {
				t.Fatalf("Failed to query with NULL NullTime: %v", err)
			}
			if result != nil {
				t.Error("Expected result to be nil for NULL input")
			}

			// Test valid value
			testTime := time.Date(2023, time.December, 25, 14, 30, 45, 0, time.UTC)
			nullTime = NullTime{Time: civil.TimeOf(testTime), Valid: true}
			err = conn.QueryRowContext(ctx, "SELECT @p1", nullTime).Scan(&result)
			if err != nil {
				t.Fatalf("Failed to query with valid NullTime: %v", err)
			}
			if result == nil {
				t.Error("Expected result to be non-nil for valid input")
			}
		})
	})
}
