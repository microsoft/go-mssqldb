package mssql

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTSQLQuoter_ID(t *testing.T) {
	q := TSQLQuoter{}
	
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple identifier",
			input:    "tablename",
			expected: "[tablename]",
		},
		{
			name:     "identifier with closing bracket",
			input:    "table]name",
			expected: "[table]]name]",
		},
		{
			name:     "identifier with multiple closing brackets",
			input:    "tab]]le",
			expected: "[tab]]]]le]",
		},
		{
			name:     "empty identifier",
			input:    "",
			expected: "[]",
		},
		{
			name:     "multi-part name",
			input:    "schema.table",
			expected: "[schema.table]",
		},
		{
			name:     "special characters",
			input:    "table name",
			expected: "[table name]",
		},
		{
			name:     "SQL injection attempt",
			input:    "table]; DROP TABLE users; --",
			expected: "[table]]; DROP TABLE users; --]",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := q.ID(tt.input)
			assert.Equal(t, tt.expected, result, "ID(%q)", tt.input)
		})
	}
}

func TestTSQLQuoter_Value(t *testing.T) {
	q := TSQLQuoter{}
	
	tests := []struct {
		name     string
		input    interface{}
		expected string
	}{
		{
			name:     "string value",
			input:    "test",
			expected: "'test'",
		},
		{
			name:     "string with single quote",
			input:    "test's",
			expected: "'test''s'",
		},
		{
			name:     "string with multiple single quotes",
			input:    "O'Reilly's",
			expected: "'O''Reilly''s'",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "''",
		},
		{
			name:     "VarChar type",
			input:    VarChar("varchar value"),
			expected: "'varchar value'",
		},
		{
			name:     "VarCharMax type",
			input:    VarCharMax("varcharmax value"),
			expected: "'varcharmax value'",
		},
		{
			name:     "NVarCharMax type",
			input:    NVarCharMax("nvarcharmax value"),
			expected: "'nvarcharmax value'",
		},
		{
			name:     "VarChar with quotes",
			input:    VarChar("test's"),
			expected: "'test''s'",
		},
		{
			name:     "SQL injection attempt in string",
			input:    "'; DROP TABLE users; --",
			expected: "'''; DROP TABLE users; --'",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := q.Value(tt.input)
			assert.Equal(t, tt.expected, result, "Value(%v)", tt.input)
		})
	}
}

func TestTSQLQuoter_Value_Panic(t *testing.T) {
	q := TSQLQuoter{}
	
	tests := []struct {
		name  string
		input interface{}
	}{
		{
			name:  "unsupported int",
			input: 42,
		},
		{
			name:  "unsupported float",
			input: 3.14,
		},
		{
			name:  "unsupported bool",
			input: true,
		},
		{
			name:  "unsupported byte slice",
			input: []byte("test"),
		},
		{
			name:  "unsupported nil",
			input: nil,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Panics(t, func() {
				q.Value(tt.input)
			}, "Value(%v) should panic for unsupported type", tt.input)
		})
	}
}
