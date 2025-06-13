package mssql

import (
	"testing"
	"time"
)

// TestBulkCopyMidnightRounding tests that the bulk copy parameter creation
// handles datetime values near midnight correctly, specifically the issue
// reported where time.Date(2025, 1, 1, 23, 59, 59, 998_350_000, time.UTC)
// causes bulk insert to fail.
func TestBulkCopyMidnightRounding(t *testing.T) {
	// Create a mock column structure for datetime
	col := columnStruct{
		ti: typeInfo{
			TypeId: typeDateTime,
			Size:   8, // 8 bytes for datetime
		},
	}
	
	// Create a Bulk instance (simplified for testing)
	bulk := &Bulk{}
	
	// Test the problematic timestamp from the issue report
	problematicTime := time.Date(2025, 1, 1, 23, 59, 59, 998_350_000, time.UTC)
	
	// Test the makeParam function directly
	param, err := bulk.makeParam(problematicTime, col)
	if err != nil {
		t.Fatalf("makeParam failed for problematic time: %v", err)
	}
	
	// Verify the buffer is the correct size
	if len(param.buffer) != 8 {
		t.Fatalf("Expected buffer size 8, got %d", len(param.buffer))
	}
	
	// Decode the buffer to verify it doesn't produce invalid data
	decoded := decodeDateTime(param.buffer)
	t.Logf("Original time: %s", problematicTime.Format(time.RFC3339Nano))
	t.Logf("Encoded/decoded time: %s", decoded.Format(time.RFC3339Nano))
	
	// Verify the decoded time is reasonable
	diff := decoded.Sub(problematicTime)
	if diff < 0 {
		diff = -diff
	}
	
	// Maximum acceptable difference for SQL Server datetime precision (1/300th of a second)
	maxDiff := time.Duration(3333333) // ~3.33ms in nanoseconds
	if diff > maxDiff {
		t.Errorf("Time difference too large after encoding: %v > %v", diff, maxDiff)
	}
}

// TestBulkCopyNearMidnightBoundaries tests various times near midnight
// to ensure bulk copy parameter creation handles day boundaries correctly
func TestBulkCopyNearMidnightBoundaries(t *testing.T) {
	col := columnStruct{
		ti: typeInfo{
			TypeId: typeDateTime,
			Size:   8,
		},
	}
	
	bulk := &Bulk{}
	
	testCases := []struct {
		name string
		time time.Time
	}{
		{
			name: "exactly midnight",
			time: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "one nanosecond before midnight",
			time: time.Date(2025, 1, 1, 23, 59, 59, 999_999_999, time.UTC),
		},
		{
			name: "998.35ms before midnight (original issue)",
			time: time.Date(2025, 1, 1, 23, 59, 59, 998_350_000, time.UTC),
		},
		{
			name: "999ms before midnight",
			time: time.Date(2025, 1, 1, 23, 59, 59, 999_000_000, time.UTC),
		},
		{
			name: "997ms before midnight",
			time: time.Date(2025, 1, 1, 23, 59, 59, 997_000_000, time.UTC),
		},
		{
			name: "996ms before midnight",
			time: time.Date(2025, 1, 1, 23, 59, 59, 996_000_000, time.UTC),
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			param, err := bulk.makeParam(tc.time, col)
			if err != nil {
				t.Fatalf("makeParam failed: %v", err)
			}
			
			if len(param.buffer) != 8 {
				t.Fatalf("Expected buffer size 8, got %d", len(param.buffer))
			}
			
			decoded := decodeDateTime(param.buffer)
			t.Logf("Original: %s", tc.time.Format(time.RFC3339Nano))
			t.Logf("Decoded:  %s", decoded.Format(time.RFC3339Nano))
			
			// Verify the decoded time is within acceptable bounds
			diff := decoded.Sub(tc.time)
			if diff < 0 {
				diff = -diff
			}
			
			maxDiff := time.Duration(3333333) // ~3.33ms
			if diff > maxDiff {
				t.Errorf("Time difference too large: %v > %v", diff, maxDiff)
			}
		})
	}
}