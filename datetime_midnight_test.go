package mssql

import (
	"testing"
	"time"
)

// TestDatetimeNearMidnightBoundaries tests various times near midnight
// to ensure proper handling of day boundaries
func TestDatetimeNearMidnightBoundaries(t *testing.T) {
	testCases := []struct {
		name string
		time time.Time
	}{
		{
			name: "999ms before midnight",
			time: time.Date(2025, 1, 1, 23, 59, 59, 999_000_000, time.UTC),
		},
		{
			name: "997ms before midnight",
			time: time.Date(2025, 1, 1, 23, 59, 59, 997_000_000, time.UTC),
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Test encoding/decoding
			encoded := encodeDateTime(tc.time)
			decoded := decodeDateTime(encoded)
			
			t.Logf("Original: %s", tc.time.Format(time.RFC3339Nano))
			t.Logf("Decoded:  %s", decoded.Format(time.RFC3339Nano))
			
			// Verify the decoded time is reasonable
			diff := decoded.Sub(tc.time)
			if diff < 0 {
				diff = -diff
			}
			
			// Maximum acceptable difference for SQL Server datetime precision
			maxDiff := time.Duration(3333333) // ~3.33ms in nanoseconds
			if diff > maxDiff {
				t.Errorf("Time difference too large: %v > %v", diff, maxDiff)
			}
			
			// Ensure we don't have invalid day overflow
			// If the original time was on Jan 1st, the decoded time should be 
			// either Jan 1st or Jan 2nd (if it rounded up to midnight)
			origDay := tc.time.Day()
			decodedDay := decoded.Day()
			
			if decodedDay != origDay && decodedDay != origDay+1 {
				t.Errorf("Invalid day change: original day %d, decoded day %d", origDay, decodedDay)
			}
		})
	}
}