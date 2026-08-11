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

func TestQuoteMultiPartID(t *testing.T) {
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
			name:     "schema qualified",
			input:    "dbo.tablename",
			expected: "[dbo].[tablename]",
		},
		{
			name:     "database qualified",
			input:    "mydb.dbo.tablename",
			expected: "[mydb].[dbo].[tablename]",
		},
		{
			name:     "server qualified",
			input:    "srv.mydb.dbo.tablename",
			expected: "[srv].[mydb].[dbo].[tablename]",
		},
		{
			name:     "default schema is preserved",
			input:    "mydb..tablename",
			expected: "[mydb]..[tablename]",
		},
		{
			name:     "already delimited",
			input:    "[dbo].[tablename]",
			expected: "[dbo].[tablename]",
		},
		{
			name:     "partially delimited",
			input:    "dbo.[table name]",
			expected: "[dbo].[table name]",
		},
		{
			name:     "delimited part containing a dot",
			input:    "[dbo].[my.table]",
			expected: "[dbo].[my.table]",
		},
		{
			name:     "delimited part containing an escaped bracket",
			input:    "[dbo].[weird]]name]",
			expected: "[dbo].[weird]]name]",
		},
		{
			name:     "double quoted parts",
			input:    `"dbo"."table name"`,
			expected: "[dbo].[table name]",
		},
		{
			name:     "double quoted part containing an escaped quote",
			input:    `"say ""hi"""`,
			expected: `[say "hi"]`,
		},
		{
			name:     "undelimited name needing delimiters",
			input:    "table name",
			expected: "[table name]",
		},
		{
			name:     "undelimited name containing a bracket",
			input:    "weird]name",
			expected: "[weird]]name]",
		},
		{
			name:     "reserved word",
			input:    "Order",
			expected: "[Order]",
		},
		{
			name:     "temp table",
			input:    "#temptable",
			expected: "[#temptable]",
		},
		{
			name:     "whitespace around parts",
			input:    " dbo . tablename ",
			expected: "[dbo].[tablename]",
		},
		{
			name:     "empty name",
			input:    "",
			expected: "",
		},
		{
			name:     "trailing statement",
			input:    "Orders; DROP TABLE dbo.Secrets;--",
			expected: "[Orders; DROP TABLE dbo].[Secrets;--]",
		},
		{
			name:     "trailing statement between delimited parts",
			input:    "[Orders] SET FMTONLY OFF; PRINT 1;--[x]",
			expected: "[[Orders]] SET FMTONLY OFF; PRINT 1;--[x]]]",
		},
		{
			name:     "unterminated delimiter",
			input:    "[Orders",
			expected: "[[Orders]",
		},
		{
			name:     "unterminated delimiter after a valid part",
			input:    "dbo.[Orders",
			expected: "[dbo].[[Orders]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := quoteMultiPartID(tt.input)
			assert.Equal(t, tt.expected, result, "quoteMultiPartID(%q)", tt.input)
		})
	}
}

func TestQuoteBulkOrder(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple column",
			input:    "id",
			expected: "[id]",
		},
		{
			name:     "column needing delimiters",
			input:    "order id",
			expected: "[order id]",
		},
		{
			name:     "already delimited column",
			input:    "[order id]",
			expected: "[order id]",
		},
		{
			name:     "ascending",
			input:    "id ASC",
			expected: "[id] ASC",
		},
		{
			name:     "descending",
			input:    "id DESC",
			expected: "[id] DESC",
		},
		{
			name:     "lower case direction is normalized",
			input:    "id desc",
			expected: "[id] DESC",
		},
		{
			name:     "delimited column with a direction",
			input:    "[order id] DESC",
			expected: "[order id] DESC",
		},
		{
			name:     "surrounding whitespace",
			input:    "  id   ASC  ",
			expected: "[id] ASC",
		},
		{
			name:     "undelimited column ending in a direction keyword",
			input:    "sort desc",
			expected: "[sort] DESC",
		},
		{
			name:     "delimited column ending in a direction keyword",
			input:    "[sort desc]",
			expected: "[sort desc]",
		},
		{
			name:     "trailing statement",
			input:    "id) WITH (TABLOCK); PRINT 1;--",
			expected: "[id) WITH (TABLOCK); PRINT 1;--]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := quoteBulkOrder(tt.input)
			assert.Equal(t, tt.expected, result, "quoteBulkOrder(%q)", tt.input)
		})
	}
}

func TestSplitMultiPartID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "single part",
			input:    "tablename",
			expected: []string{"tablename"},
		},
		{
			name:     "three parts",
			input:    "mydb.dbo.tablename",
			expected: []string{"mydb", "dbo", "tablename"},
		},
		{
			name:     "empty middle part",
			input:    "mydb..tablename",
			expected: []string{"mydb", "", "tablename"},
		},
		{
			name:     "dot inside brackets is not a separator",
			input:    "[dbo].[my.table]",
			expected: []string{"[dbo]", "[my.table]"},
		},
		{
			name:     "dot inside double quotes is not a separator",
			input:    `"my.table"`,
			expected: []string{`"my.table"`},
		},
		{
			name:     "escaped bracket does not end the part",
			input:    "[my]].table].x",
			expected: []string{"[my]].table]", "x"},
		},
		{
			name:     "escaped quote does not end the part",
			input:    `"my"".table".x`,
			expected: []string{`"my"".table"`, "x"},
		},
		{
			name:     "empty input",
			input:    "",
			expected: []string{""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, splitMultiPartID(tt.input), "splitMultiPartID(%q)", tt.input)
		})
	}
}

func TestUnquoteID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		ok       bool
	}{
		{name: "brackets", input: "[tablename]", expected: "tablename", ok: true},
		{name: "double quotes", input: `"tablename"`, expected: "tablename", ok: true},
		{name: "escaped bracket", input: "[weird]]name]", expected: "weird]name", ok: true},
		{name: "escaped quote", input: `"weird""name"`, expected: `weird"name`, ok: true},
		{name: "empty delimited", input: "[]", expected: "", ok: true},
		{name: "opening bracket inside brackets", input: "[a[b]", expected: "a[b", ok: true},
		{name: "undelimited", input: "tablename", ok: false},
		{name: "unterminated", input: "[tablename", ok: false},
		{name: "mismatched delimiters", input: `[tablename"`, ok: false},
		{name: "not a single identifier", input: "[a] PRINT 1 --[b]", ok: false},
		{name: "too short", input: "[", ok: false},
		{name: "empty", input: "", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, ok := unquoteID(tt.input)
			assert.Equal(t, tt.ok, ok, "unquoteID(%q) ok", tt.input)
			if tt.ok {
				assert.Equal(t, tt.expected, result, "unquoteID(%q)", tt.input)
			}
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
