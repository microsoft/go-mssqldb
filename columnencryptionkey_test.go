package mssql

import "testing"

func TestNewCekTable(t *testing.T) {
	tests := []struct {
		name     string
		size     uint16
		expected int
	}{
		{
			name:     "zero size",
			size:     0,
			expected: 0,
		},
		{
			name:     "single entry",
			size:     1,
			expected: 1,
		},
		{
			name:     "multiple entries",
			size:     10,
			expected: 10,
		},
		{
			name:     "large table",
			size:     1000,
			expected: 1000,
		},
		{
			name:     "max uint16",
			size:     65535,
			expected: 65535,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := newCekTable(tt.size)
			
			if result.entries == nil {
				t.Error("newCekTable() returned nil entries slice")
			}
			
			if len(result.entries) != tt.expected {
				t.Errorf("newCekTable(%d) created table with %d entries, want %d",
					tt.size, len(result.entries), tt.expected)
			}
			
			// Verify capacity matches length
			if cap(result.entries) != tt.expected {
				t.Errorf("newCekTable(%d) created table with capacity %d, want %d",
					tt.size, cap(result.entries), tt.expected)
			}
			
			// Verify all entries are zero-initialized
			for i, entry := range result.entries {
				if entry.databaseID != 0 || entry.keyId != 0 || entry.keyVersion != 0 ||
					entry.mdVersion != nil || entry.valueCount != 0 || entry.cekValues != nil {
					t.Errorf("newCekTable(%d) entry %d is not zero-initialized: %+v",
						tt.size, i, entry)
				}
			}
		})
	}
}
