package mssql

import (
	"database/sql/driver"
	"encoding/json"
	"testing"
	"time"

	"github.com/golang-sql/civil"
)

func TestNullDate(t *testing.T) {
	// Test Value() method
	t.Run("Value", func(t *testing.T) {
		// Valid case
		date := civil.Date{Year: 2023, Month: time.December, Day: 25}
		nullDate := NullDate{Date: date, Valid: true}
		val, err := nullDate.Value()
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if val != date {
			t.Errorf("Expected %v, got %v", date, val)
		}

		// Invalid case
		nullDate = NullDate{Valid: false}
		val, err = nullDate.Value()
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if val != nil {
			t.Errorf("Expected nil, got %v", val)
		}
	})

	// Test Scan() method
	t.Run("Scan", func(t *testing.T) {
		var nullDate NullDate

		// Scan nil value
		err := nullDate.Scan(nil)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if nullDate.Valid {
			t.Error("Expected Valid to be false")
		}

		// Scan time.Time value
		testTime := time.Date(2023, time.December, 25, 14, 30, 45, 0, time.UTC)
		err = nullDate.Scan(testTime)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if !nullDate.Valid {
			t.Error("Expected Valid to be true")
		}
		expectedDate := civil.DateOf(testTime)
		if nullDate.Date != expectedDate {
			t.Errorf("Expected %v, got %v", expectedDate, nullDate.Date)
		}

		// Scan invalid type
		err = nullDate.Scan("invalid")
		if err == nil {
			t.Error("Expected error for invalid type")
		}
		if nullDate.Valid {
			t.Error("Expected Valid to be false after error")
		}
	})

	// Test String() method
	t.Run("String", func(t *testing.T) {
		// Valid case
		date := civil.Date{Year: 2023, Month: time.December, Day: 25}
		nullDate := NullDate{Date: date, Valid: true}
		str := nullDate.String()
		if str != date.String() {
			t.Errorf("Expected %s, got %s", date.String(), str)
		}

		// Invalid case
		nullDate = NullDate{Valid: false}
		str = nullDate.String()
		if str != "NULL" {
			t.Errorf("Expected 'NULL', got %s", str)
		}
	})

	// Test JSON marshaling/unmarshaling
	t.Run("JSON", func(t *testing.T) {
		// Valid case
		date := civil.Date{Year: 2023, Month: time.December, Day: 25}
		nullDate := NullDate{Date: date, Valid: true}
		data, err := json.Marshal(nullDate)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}

		var unmarshaled NullDate
		err = json.Unmarshal(data, &unmarshaled)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if !unmarshaled.Valid || unmarshaled.Date != date {
			t.Errorf("Expected %v (valid), got %v (valid: %t)", date, unmarshaled.Date, unmarshaled.Valid)
		}

		// Invalid case
		nullDate = NullDate{Valid: false}
		data, err = json.Marshal(nullDate)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if string(data) != "null" {
			t.Errorf("Expected 'null', got %s", string(data))
		}

		err = json.Unmarshal([]byte("null"), &unmarshaled)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if unmarshaled.Valid {
			t.Error("Expected Valid to be false")
		}
	})
}

func TestNullDateTime(t *testing.T) {
	// Test Value() method
	t.Run("Value", func(t *testing.T) {
		// Valid case
		testTime := time.Date(2023, time.December, 25, 14, 30, 45, 0, time.UTC)
		dateTime := civil.DateTimeOf(testTime)
		nullDateTime := NullDateTime{DateTime: dateTime, Valid: true}
		val, err := nullDateTime.Value()
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if val != dateTime {
			t.Errorf("Expected %v, got %v", dateTime, val)
		}

		// Invalid case
		nullDateTime = NullDateTime{Valid: false}
		val, err = nullDateTime.Value()
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if val != nil {
			t.Errorf("Expected nil, got %v", val)
		}
	})

	// Test Scan() method
	t.Run("Scan", func(t *testing.T) {
		var nullDateTime NullDateTime

		// Scan nil value
		err := nullDateTime.Scan(nil)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if nullDateTime.Valid {
			t.Error("Expected Valid to be false")
		}

		// Scan time.Time value
		testTime := time.Date(2023, time.December, 25, 14, 30, 45, 0, time.UTC)
		err = nullDateTime.Scan(testTime)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if !nullDateTime.Valid {
			t.Error("Expected Valid to be true")
		}
		expectedDateTime := civil.DateTimeOf(testTime)
		if nullDateTime.DateTime != expectedDateTime {
			t.Errorf("Expected %v, got %v", expectedDateTime, nullDateTime.DateTime)
		}

		// Scan invalid type
		err = nullDateTime.Scan("invalid")
		if err == nil {
			t.Error("Expected error for invalid type")
		}
		if nullDateTime.Valid {
			t.Error("Expected Valid to be false after error")
		}
	})
}

func TestNullTime(t *testing.T) {
	// Test Value() method
	t.Run("Value", func(t *testing.T) {
		// Valid case
		testTime := time.Date(2023, time.December, 25, 14, 30, 45, 0, time.UTC)
		civilTime := civil.TimeOf(testTime)
		nullTime := NullTime{Time: civilTime, Valid: true}
		val, err := nullTime.Value()
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if val != civilTime {
			t.Errorf("Expected %v, got %v", civilTime, val)
		}

		// Invalid case
		nullTime = NullTime{Valid: false}
		val, err = nullTime.Value()
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if val != nil {
			t.Errorf("Expected nil, got %v", val)
		}
	})

	// Test Scan() method
	t.Run("Scan", func(t *testing.T) {
		var nullTime NullTime

		// Scan nil value
		err := nullTime.Scan(nil)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if nullTime.Valid {
			t.Error("Expected Valid to be false")
		}

		// Scan time.Time value
		testTime := time.Date(2023, time.December, 25, 14, 30, 45, 0, time.UTC)
		err = nullTime.Scan(testTime)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if !nullTime.Valid {
			t.Error("Expected Valid to be true")
		}
		expectedTime := civil.TimeOf(testTime)
		if nullTime.Time != expectedTime {
			t.Errorf("Expected %v, got %v", expectedTime, nullTime.Time)
		}

		// Scan invalid type
		err = nullTime.Scan("invalid")
		if err == nil {
			t.Error("Expected error for invalid type")
		}
		if nullTime.Valid {
			t.Error("Expected Valid to be false after error")
		}
	})
}

// Test that the types implement the required interfaces
func TestNullCivilTypesImplementInterfaces(t *testing.T) {
	var (
		_ driver.Valuer = NullDate{}
		_ driver.Valuer = NullDateTime{}
		_ driver.Valuer = NullTime{}
	)
	// Note: Scanner interface is verified by successful compilation of Scan methods
}