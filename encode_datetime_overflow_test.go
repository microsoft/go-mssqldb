package mssql

import (
	"encoding/binary"
	"testing"
	"time"
)

// TestEncodeDateTimeOverflow specifically tests that encodeDateTime
// correctly handles day overflow when nanosToThreeHundredthsOfASecond 
// returns 300 (representing 1 full second).
func TestEncodeDateTimeOverflow(t *testing.T) {
	testCases := []struct {
		name     string
		input    time.Time
		expected time.Time
	}{
		{
			name:     "998.35ms rounds to next day",
			input:    time.Date(2025, 1, 1, 23, 59, 59, 998_350_000, time.UTC),
			expected: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "998.35ms rounds to the next second",
			input:    time.Date(2025, 1, 1, 23, 59, 58, 998_350_000, time.UTC),
			expected: time.Date(2025, 1, 1, 23, 59, 59, 0, time.UTC),
		},
		{
			name:     "999.999ms rounds to next day",
			input:    time.Date(2025, 1, 1, 23, 59, 59, 999_999_999, time.UTC),
			expected: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "exactly midnight stays midnight",
			input:    time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
			expected: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Encode the time
			encoded := encodeDateTime(tc.input)
			
			// Verify round-trip decoding gives the expected result
			decoded := decodeDateTime(encoded)
			
			if !decoded.Equal(tc.expected) {
				t.Errorf("Expected decoded time %v, got %v", tc.expected, decoded)
			}
		})
	}
}

// TestEncodeDateTimeMaxDateOverflow tests that overflow at the maximum
// supported date is handled correctly.
func TestEncodeDateTimeMaxDateOverflow(t *testing.T) {
	// Test time very close to end of 9999 that might overflow
	maxTime := time.Date(9999, 12, 31, 23, 59, 59, 998_350_000, time.UTC)
	
	// Encode the time
	encoded := encodeDateTime(maxTime)
	
	// Decode it back
	decoded := decodeDateTime(encoded)
	
	// Should be clamped to the maximum possible datetime value
	// SQL Server datetime max is 9999-12-31 23:59:59.997
	if decoded.Year() != 9999 || decoded.Month() != 12 || decoded.Day() != 31 {
		t.Errorf("Expected max date to remain 9999-12-31, got %v", decoded)
	}
}

// TestEncodeDateTimeNoOverflow verifies that times that don't cause
// overflow still work correctly.
func TestEncodeDateTimeNoOverflow(t *testing.T) {
	// Test case that should not trigger overflow: 997ms
	normalTime := time.Date(2025, 1, 1, 23, 59, 59, 997_000_000, time.UTC)
	
	// Encode the time
	encoded := encodeDateTime(normalTime)
	
	// Decode the days and time portions
	days := int32(binary.LittleEndian.Uint32(encoded[0:4]))
	tm := binary.LittleEndian.Uint32(encoded[4:8])
	
	// Calculate expected values
	basedays := gregorianDays(1900, 1)
	expectedDays := gregorianDays(2025, 1) - basedays // Should still be Jan 1st
	
	if days != int32(expectedDays) {
		t.Errorf("Expected days %d, got %d", expectedDays, days)
	}
	
	// tm should be less than a full day's worth
	if tm >= 300*86400 {
		t.Errorf("tm %d should be less than a full day (%d)", tm, 300*86400)
	}
	
	// Verify round-trip decoding
	decoded := decodeDateTime(encoded)
	
	// The decoded time should be on the same day (Jan 1st)
	if decoded.Day() != 1 || decoded.Month() != 1 || decoded.Year() != 2025 {
		t.Errorf("Expected decoded time to be on 2025-01-01, got %v", decoded)
	}
}