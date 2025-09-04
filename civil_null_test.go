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
		expectedTime := date.In(time.UTC)
		if val != expectedTime {
			t.Errorf("Expected %v, got %v", expectedTime, val)
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
		expectedTime := dateTime.In(time.UTC)
		if val != expectedTime {
			t.Errorf("Expected %v, got %v", expectedTime, val)
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
		expectedTime := time.Date(1, 1, 1, civilTime.Hour, civilTime.Minute, civilTime.Second, civilTime.Nanosecond, time.UTC)
		if val != expectedTime {
			t.Errorf("Expected %v, got %v", expectedTime, val)
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

// TestNullCivilTypesParameterEncoding tests that nullable civil types are properly encoded
// as typed NULL parameters rather than untyped NULLs, which is important for OUT parameters
func TestNullCivilTypesParameterEncoding(t *testing.T) {
	// Create a mock connection and statement for testing
	c := &Conn{}
	c.sess = &tdsSession{}
	c.sess.loginAck.TDSVersion = verTDS74  // Use modern TDS version
	s := &Stmt{c: c}

	t.Run("NullDate parameter encoding", func(t *testing.T) {
		// Test valid NullDate
		validDate := NullDate{Date: civil.Date{Year: 2023, Month: time.December, Day: 25}, Valid: true}
		param, err := s.makeParam(validDate)
		if err != nil {
			t.Errorf("Unexpected error for valid NullDate: %v", err)
		}
		if param.ti.TypeId != typeDateN {
			t.Errorf("Expected TypeId %v for valid NullDate, got %v", typeDateN, param.ti.TypeId)
		}
		if len(param.buffer) == 0 {
			t.Error("Expected non-empty buffer for valid NullDate")
		}

		// Test invalid NullDate (NULL)
		nullDate := NullDate{Valid: false}
		param, err = s.makeParam(nullDate)
		if err != nil {
			t.Errorf("Unexpected error for NULL NullDate: %v", err)
		}
		if param.ti.TypeId != typeDateN {
			t.Errorf("Expected TypeId %v for NULL NullDate, got %v", typeDateN, param.ti.TypeId)
		}
		if param.ti.TypeId == typeNull {
			t.Error("NULL NullDate should not use untyped NULL (typeNull)")
		}
		if len(param.buffer) != 0 {
			t.Error("Expected empty buffer for NULL NullDate")
		}
		if param.ti.Size != 3 {
			t.Errorf("Expected Size 3 for NULL NullDate, got %v", param.ti.Size)
		}
	})

	t.Run("NullDateTime parameter encoding", func(t *testing.T) {
		// Test valid NullDateTime
		testTime := time.Date(2023, time.December, 25, 14, 30, 45, 0, time.UTC)
		validDateTime := NullDateTime{DateTime: civil.DateTimeOf(testTime), Valid: true}
		param, err := s.makeParam(validDateTime)
		if err != nil {
			t.Errorf("Unexpected error for valid NullDateTime: %v", err)
		}
		if param.ti.TypeId != typeDateTime2N {
			t.Errorf("Expected TypeId %v for valid NullDateTime, got %v", typeDateTime2N, param.ti.TypeId)
		}
		if len(param.buffer) == 0 {
			t.Error("Expected non-empty buffer for valid NullDateTime")
		}

		// Test invalid NullDateTime (NULL)
		nullDateTime := NullDateTime{Valid: false}
		param, err = s.makeParam(nullDateTime)
		if err != nil {
			t.Errorf("Unexpected error for NULL NullDateTime: %v", err)
		}
		if param.ti.TypeId != typeDateTime2N {
			t.Errorf("Expected TypeId %v for NULL NullDateTime, got %v", typeDateTime2N, param.ti.TypeId)
		}
		if param.ti.TypeId == typeNull {
			t.Error("NULL NullDateTime should not use untyped NULL (typeNull)")
		}
		if len(param.buffer) != 0 {
			t.Error("Expected empty buffer for NULL NullDateTime")
		}
		if param.ti.Scale != 7 {
			t.Errorf("Expected Scale 7 for NULL NullDateTime, got %v", param.ti.Scale)
		}
	})

	t.Run("NullTime parameter encoding", func(t *testing.T) {
		// Test valid NullTime
		testTime := time.Date(2023, time.December, 25, 14, 30, 45, 0, time.UTC)
		validTime := NullTime{Time: civil.TimeOf(testTime), Valid: true}
		param, err := s.makeParam(validTime)
		if err != nil {
			t.Errorf("Unexpected error for valid NullTime: %v", err)
		}
		if param.ti.TypeId != typeTimeN {
			t.Errorf("Expected TypeId %v for valid NullTime, got %v", typeTimeN, param.ti.TypeId)
		}
		if len(param.buffer) == 0 {
			t.Error("Expected non-empty buffer for valid NullTime")
		}

		// Test invalid NullTime (NULL)
		nullTime := NullTime{Valid: false}
		param, err = s.makeParam(nullTime)
		if err != nil {
			t.Errorf("Unexpected error for NULL NullTime: %v", err)
		}
		if param.ti.TypeId != typeTimeN {
			t.Errorf("Expected TypeId %v for NULL NullTime, got %v", typeTimeN, param.ti.TypeId)
		}
		if param.ti.TypeId == typeNull {
			t.Error("NULL NullTime should not use untyped NULL (typeNull)")
		}
		if len(param.buffer) != 0 {
			t.Error("Expected empty buffer for NULL NullTime")
		}
		if param.ti.Scale != 7 {
			t.Errorf("Expected Scale 7 for NULL NullTime, got %v", param.ti.Scale)
		}
	})

	// Test pointer types (as used in OUT parameters)
	t.Run("Pointer NullDate parameter encoding", func(t *testing.T) {
		// Test valid *NullDate
		validDate := &NullDate{Date: civil.Date{Year: 2023, Month: time.December, Day: 25}, Valid: true}
		param, err := s.makeParam(validDate)
		if err != nil {
			t.Errorf("Unexpected error for valid *NullDate: %v", err)
		}
		if param.ti.TypeId != typeDateN {
			t.Errorf("Expected TypeId %v for valid *NullDate, got %v", typeDateN, param.ti.TypeId)
		}
		if len(param.buffer) == 0 {
			t.Error("Expected non-empty buffer for valid *NullDate")
		}

		// Test invalid *NullDate (NULL)
		nullDate := &NullDate{Valid: false}
		param, err = s.makeParam(nullDate)
		if err != nil {
			t.Errorf("Unexpected error for NULL *NullDate: %v", err)
		}
		if param.ti.TypeId != typeDateN {
			t.Errorf("Expected TypeId %v for NULL *NullDate, got %v", typeDateN, param.ti.TypeId)
		}
		if param.ti.TypeId == typeNull {
			t.Error("NULL *NullDate should not use untyped NULL (typeNull)")
		}
		if len(param.buffer) != 0 {
			t.Error("Expected empty buffer for NULL *NullDate")
		}
		if param.ti.Size != 3 {
			t.Errorf("Expected Size 3 for NULL *NullDate, got %v", param.ti.Size)
		}
	})

	t.Run("Pointer NullDateTime parameter encoding", func(t *testing.T) {
		// Test valid *NullDateTime
		testTime := time.Date(2023, time.December, 25, 14, 30, 45, 0, time.UTC)
		validDateTime := &NullDateTime{DateTime: civil.DateTimeOf(testTime), Valid: true}
		param, err := s.makeParam(validDateTime)
		if err != nil {
			t.Errorf("Unexpected error for valid *NullDateTime: %v", err)
		}
		if param.ti.TypeId != typeDateTime2N {
			t.Errorf("Expected TypeId %v for valid *NullDateTime, got %v", typeDateTime2N, param.ti.TypeId)
		}
		if len(param.buffer) == 0 {
			t.Error("Expected non-empty buffer for valid *NullDateTime")
		}

		// Test invalid *NullDateTime (NULL)
		nullDateTime := &NullDateTime{Valid: false}
		param, err = s.makeParam(nullDateTime)
		if err != nil {
			t.Errorf("Unexpected error for NULL *NullDateTime: %v", err)
		}
		if param.ti.TypeId != typeDateTime2N {
			t.Errorf("Expected TypeId %v for NULL *NullDateTime, got %v", typeDateTime2N, param.ti.TypeId)
		}
		if param.ti.TypeId == typeNull {
			t.Error("NULL *NullDateTime should not use untyped NULL (typeNull)")
		}
		if len(param.buffer) != 0 {
			t.Error("Expected empty buffer for NULL *NullDateTime")
		}
		if param.ti.Scale != 7 {
			t.Errorf("Expected Scale 7 for NULL *NullDateTime, got %v", param.ti.Scale)
		}
	})

	t.Run("Pointer NullTime parameter encoding", func(t *testing.T) {
		// Test valid *NullTime
		testTime := time.Date(2023, time.December, 25, 14, 30, 45, 0, time.UTC)
		validTime := &NullTime{Time: civil.TimeOf(testTime), Valid: true}
		param, err := s.makeParam(validTime)
		if err != nil {
			t.Errorf("Unexpected error for valid *NullTime: %v", err)
		}
		if param.ti.TypeId != typeTimeN {
			t.Errorf("Expected TypeId %v for valid *NullTime, got %v", typeTimeN, param.ti.TypeId)
		}
		if len(param.buffer) == 0 {
			t.Error("Expected non-empty buffer for valid *NullTime")
		}

		// Test invalid *NullTime (NULL)
		nullTime := &NullTime{Valid: false}
		param, err = s.makeParam(nullTime)
		if err != nil {
			t.Errorf("Unexpected error for NULL *NullTime: %v", err)
		}
		if param.ti.TypeId != typeTimeN {
			t.Errorf("Expected TypeId %v for NULL *NullTime, got %v", typeTimeN, param.ti.TypeId)
		}
		if param.ti.TypeId == typeNull {
			t.Error("NULL *NullTime should not use untyped NULL (typeNull)")
		}
		if len(param.buffer) != 0 {
			t.Error("Expected empty buffer for NULL *NullTime")
		}
		if param.ti.Scale != 7 {
			t.Errorf("Expected Scale 7 for NULL *NullTime, got %v", param.ti.Scale)
		}
	})
}
