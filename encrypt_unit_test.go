package mssql

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestColumnEncryptionType_Values(t *testing.T) {
	// Verify the constant values match expected TDS protocol values
	tests := []struct {
		name     string
		value    ColumnEncryptionType
		expected int
	}{
		{"PlainText", ColumnEncryptionPlainText, 0},
		{"Deterministic", ColumnEncryptionDeterministic, 1},
		{"Randomized", ColumnEncryptionRandomized, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, int(tt.value), "%s value mismatch", tt.name)
		})
	}
}

func TestAppendPrefixedParameterName(t *testing.T) {
	tests := []struct {
		name     string
		param    string
		expected string
	}{
		{
			name:     "parameter without @ prefix",
			param:    "param1",
			expected: "@param1",
		},
		{
			name:     "parameter with @ prefix",
			param:    "@param2",
			expected: "@param2",
		},
		{
			name:     "empty parameter",
			param:    "",
			expected: "",
		},
		{
			name:     "single character without @",
			param:    "p",
			expected: "@p",
		},
		{
			name:     "just @ symbol",
			param:    "@",
			expected: "@",
		},
		{
			name:     "parameter with special chars",
			param:    "my_param",
			expected: "@my_param",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := new(strings.Builder)
			appendPrefixedParameterName(b, tt.param)
			result := b.String()
			assert.Equal(t, tt.expected, result, "appendPrefixedParameterName(%q)", tt.param)
		})
	}
}

func TestCekData_Fields(t *testing.T) {
	// Test that cekData struct can be properly initialized
	cek := &cekData{
		ordinal:         1,
		database_id:     100,
		id:              200,
		version:         3,
		metadataVersion: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
		encryptedValue:  []byte{0xAA, 0xBB, 0xCC},
		cmkStoreName:    "AZURE_KEY_VAULT",
		cmkPath:         "https://myvault.vault.azure.net/keys/mykey/version",
		algorithm:       "RSA_OAEP",
		decryptedValue:  nil,
	}

	assert.Equal(t, 1, cek.ordinal, "ordinal")
	assert.Equal(t, 100, cek.database_id, "database_id")
	assert.Equal(t, 200, cek.id, "id")
	assert.Equal(t, 3, cek.version, "version")
	assert.Len(t, cek.metadataVersion, 8, "metadataVersion length")
	assert.Equal(t, "AZURE_KEY_VAULT", cek.cmkStoreName, "cmkStoreName")
	assert.Equal(t, "https://myvault.vault.azure.net/keys/mykey/version", cek.cmkPath, "cmkPath")
	assert.Equal(t, "RSA_OAEP", cek.algorithm, "algorithm")
}

func TestParameterEncData_Fields(t *testing.T) {
	// Test that parameterEncData struct can be properly initialized
	param := &parameterEncData{
		ordinal:     1,
		name:        "@param1",
		algorithm:   1,
		encType:     ColumnEncryptionDeterministic,
		cekOrdinal:  2,
		ruleVersion: 1,
	}

	assert.Equal(t, 1, param.ordinal, "ordinal")
	assert.Equal(t, "@param1", param.name, "name")
	assert.Equal(t, 1, param.algorithm, "algorithm")
	assert.Equal(t, ColumnEncryptionDeterministic, param.encType, "encType")
	assert.Equal(t, 2, param.cekOrdinal, "cekOrdinal")
	assert.Equal(t, 1, param.ruleVersion, "ruleVersion")
}

func TestParamMapEntry_Fields(t *testing.T) {
	cek := &cekData{
		ordinal:      1,
		cmkStoreName: "TEST_STORE",
	}
	param := &parameterEncData{
		ordinal: 1,
		name:    "@p1",
		encType: ColumnEncryptionRandomized,
	}

	entry := paramMapEntry{
		cek: cek,
		p:   param,
	}

	assert.Equal(t, cek, entry.cek, "cek pointer mismatch")
	assert.Equal(t, param, entry.p, "param pointer mismatch")
	assert.Equal(t, "TEST_STORE", entry.cek.cmkStoreName, "cek.cmkStoreName")
	assert.Equal(t, "@p1", entry.p.name, "p.name")
}

func TestBuildStoredProcedureStatementForColumnEncryption_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		proc     string
		args     []namedValue
		expected string
	}{
		{
			name:     "proc with schema",
			proc:     "dbo.myproc",
			args:     []namedValue{},
			expected: "EXEC [dbo.myproc]",
		},
		{
			name:     "proc with special characters",
			proc:     "my proc with spaces",
			args:     []namedValue{},
			expected: "EXEC [my proc with spaces]",
		},
		{
			name:     "proc with brackets",
			proc:     "my]proc",
			args:     []namedValue{},
			expected: "EXEC [my]]proc]",
		},
		{
			name: "numbered parameters",
			proc: "myproc",
			args: []namedValue{
				{Name: "", Ordinal: 1, Value: "value1"},
				{Name: "", Ordinal: 2, Value: "value2"},
			},
			expected: "EXEC [myproc] @p1, @p2",
		},
		{
			name: "mixed named and numbered parameters",
			proc: "myproc",
			args: []namedValue{
				{Name: "named1", Ordinal: 1, Value: "value1"},
				{Name: "", Ordinal: 2, Value: "value2"},
			},
			expected: "EXEC [myproc] @named1=@named1, @p2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildStoredProcedureStatementForColumnEncryption(tt.proc, tt.args)
			assert.Equal(t, tt.expected, result, "buildStoredProcedureStatementForColumnEncryption(%q, ...)", tt.proc)
		})
	}
}

func TestPrepareEncryptionQuery_Basic(t *testing.T) {
	s := &Stmt{}

	// Test with a simple non-proc query
	args := []namedValue{
		{Name: "p1", Ordinal: 1, Value: "test"},
	}

	newArgs, err := s.prepareEncryptionQuery(false, "SELECT * FROM table WHERE col = @p1", args)
	assert.NoError(t, err, "prepareEncryptionQuery failed")
	assert.Len(t, newArgs, 2, "expected 2 args")

	// First arg should be tsql
	assert.Equal(t, "tsql", newArgs[0].Name, "first arg name")

	// Second arg should be params
	assert.Equal(t, "params", newArgs[1].Name, "second arg name")
}
