package mssql

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
			
			assert.NotNil(t, result.entries, "newCekTable() returned nil entries slice")
			assert.Equal(t, tt.expected, len(result.entries), "newCekTable(%d) entries length", tt.size)
			assert.Equal(t, tt.expected, cap(result.entries), "newCekTable(%d) entries capacity", tt.size)
			
			// Verify all entries are zero-initialized
			for i, entry := range result.entries {
				assert.Zero(t, entry.databaseID, "entry %d databaseID should be zero", i)
				assert.Zero(t, entry.keyId, "entry %d keyId should be zero", i)
				assert.Zero(t, entry.keyVersion, "entry %d keyVersion should be zero", i)
				assert.Nil(t, entry.mdVersion, "entry %d mdVersion should be nil", i)
				assert.Zero(t, entry.valueCount, "entry %d valueCount should be zero", i)
				assert.Nil(t, entry.cekValues, "entry %d cekValues should be nil", i)
			}
		})
	}
}
