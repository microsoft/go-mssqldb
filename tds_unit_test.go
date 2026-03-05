package mssql

import (
	"encoding/binary"
	"testing"

	"github.com/microsoft/go-mssqldb/msdsn"
	"github.com/stretchr/testify/assert"
)

func TestParseDAC(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		msg      []byte
		instance string
	}{
		{
			name:     "valid DAC response",
			msg:      []byte{5, 0, 0, 0, 0, 0x59, 0x05}, // Port 1369 (0x0559) little-endian
			instance: "testinstance",
		},
		{
			name:     "empty message",
			msg:      []byte{},
			instance: "testinstance",
		},
		{
			name:     "wrong first byte",
			msg:      []byte{4, 0, 0, 0, 0, 0x59, 0x05},
			instance: "testinstance",
		},
		{
			name:     "too short message",
			msg:      []byte{5, 0, 0, 0, 0},
			instance: "testinstance",
		},
		{
			name:     "case insensitive instance",
			msg:      createValidDACResponse(1433),
			instance: "MyInstance",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Just verify we don't panic
			_ = parseDAC(tt.msg, tt.instance)
		})
	}
}

// Helper to create a valid DAC response message
func createValidDACResponse(port uint16) []byte {
	msg := make([]byte, 7) // parseDAC expects 6 bytes + buffer, uses index 5
	msg[0] = 5
	binary.LittleEndian.PutUint16(msg[5:], port)
	return msg
}

func TestParseInstances(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		msg  []byte
	}{
		{
			name: "empty message",
			msg:  []byte{},
		},
		{
			name: "wrong first byte",
			msg:  []byte{4, 0, 0, 0},
		},
		{
			name: "too short message",
			msg:  []byte{5, 0},
		},
		{
			name: "single instance response",
			// Format: 0x05 + 2 bytes length + semicolon-delimited key-value pairs
			msg: createBrowserResponse("MSSQLSERVER", "1433", "sql/query"),
		},
		{
			name: "no instances just header",
			msg:  []byte{5, 0, 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Just verify we don't panic
			_ = parseInstances(tt.msg)
		})
	}
}

// createBrowserResponse creates a mock SQL Browser response message
func createBrowserResponse(instanceName, tcpPort, pipeName string) []byte {
	// Format: key1;value1;key2;value2;;key1;value1;...
	data := "InstanceName;" + instanceName + ";tcp;" + tcpPort + ";np;" + pipeName + ";;"
	msg := make([]byte, 3+len(data))
	msg[0] = 5
	msg[1] = byte(len(data) & 0xFF)
	msg[2] = byte(len(data) >> 8)
	copy(msg[3:], data)
	return msg
}

func TestStr2ucs2(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected []byte
	}{
		{
			name:     "empty string",
			input:    "",
			expected: []byte{},
		},
		{
			name:     "simple ASCII",
			input:    "A",
			expected: []byte{0x41, 0x00}, // 'A' in UTF-16LE
		},
		{
			name:     "hello",
			input:    "hello",
			expected: []byte{0x68, 0x00, 0x65, 0x00, 0x6c, 0x00, 0x6c, 0x00, 0x6f, 0x00},
		},
		{
			name:     "unicode character",
			input:    "日",
			expected: []byte{0xe5, 0x65}, // U+65E5 in little-endian
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := str2ucs2(tt.input)
			assert.Equal(t, tt.expected, result, "str2ucs2(%q)", tt.input)
		})
	}
}

func TestManglePassword(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "empty password",
			input: "",
		},
		{
			name:  "simple password",
			input: "password",
		},
		{
			name:  "complex password",
			input: "P@$$w0rd!123",
		},
		{
			name:  "unicode password",
			input: "пароль", // Russian word for "password"
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := manglePassword(tt.input)
			// Result should be 2x the rune count (UTF-16)
			expected := len(str2ucs2(tt.input))
			assert.Len(t, result, expected, "manglePassword(%q) length", tt.input)
		})
	}
}

func TestIsEncryptedFlag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		value    uint16
		expected bool
	}{
		{name: "not encrypted", value: 0, expected: false},
		{name: "encrypted flag set", value: colFlagEncrypted, expected: true},
		{name: "other flags only", value: 0x0001, expected: false},
		{name: "multiple flags with encrypted", value: colFlagEncrypted | 0x0001, expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isEncryptedFlag(tt.value)
			assert.Equal(t, tt.expected, result, "isEncryptedFlag(%d)", tt.value)
		})
	}
}

func TestColumnStructIsEncrypted(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		flags    uint16
		expected bool
	}{
		{name: "not encrypted", flags: 0, expected: false},
		{name: "encrypted", flags: colFlagEncrypted, expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			col := columnStruct{Flags: tt.flags}
			result := col.isEncrypted()
			assert.Equal(t, tt.expected, result, "columnStruct.isEncrypted()")
		})
	}
}

func TestKeySliceSorting(t *testing.T) {
	t.Parallel()

	keys := keySlice{3, 1, 2}

	// Check Len
	assert.Equal(t, 3, keys.Len(), "keySlice.Len()")

	// Check Less
	assert.True(t, keys.Less(1, 0), "keySlice.Less(1, 0) should be true (1 < 3)")

	// Check Swap
	keys.Swap(0, 1)
	assert.Equal(t, uint8(1), keys[0], "keySlice[0] after swap")
	assert.Equal(t, uint8(3), keys[1], "keySlice[1] after swap")
}

func TestLoginHeaderCreation(t *testing.T) {
	t.Parallel()

	hdr := loginHeader{
		TDSVersion:   verTDS74,
		PacketSize:   4096,
		ClientPID:    12345,
		OptionFlags1: fUseDB,
	}

	assert.Equal(t, uint32(verTDS74), hdr.TDSVersion, "loginHeader.TDSVersion")
	assert.Equal(t, uint32(4096), hdr.PacketSize, "loginHeader.PacketSize")
}

func TestFeatureExtsAdd(t *testing.T) {
	t.Parallel()

	t.Run("add nil feature", func(t *testing.T) {
		var fe featureExts
		err := fe.Add(nil)
		assert.NoError(t, err, "Add(nil) should return nil")
	})

	t.Run("add first feature", func(t *testing.T) {
		var fe featureExts
		ce := &featureExtColumnEncryption{}
		err := fe.Add(ce)
		assert.NoError(t, err, "Add()")
		assert.NotNil(t, fe.features, "Add() should initialize the features map")
		assert.Len(t, fe.features, 1, "features length")
	})

	t.Run("add duplicate feature returns error", func(t *testing.T) {
		var fe featureExts
		ce := &featureExtColumnEncryption{}
		_ = fe.Add(ce)
		// Try to add the same feature ID again
		err := fe.Add(ce)
		assert.Error(t, err, "Add() should return error for duplicate feature ID")
	})

	t.Run("add multiple different features", func(t *testing.T) {
		var fe featureExts
		ce := &featureExtColumnEncryption{}
		fa := &featureExtFedAuth{}
		_ = fe.Add(ce)
		err := fe.Add(fa)
		assert.NoError(t, err, "Add() for second feature")
		assert.Len(t, fe.features, 2, "features length")
	})
}

func TestColEncryptionFeatureExtID(t *testing.T) {
	t.Parallel()
	ce := featureExtColumnEncryption{}
	assert.Equal(t, byte(4), ce.featureID(), "featureExtColumnEncryption.featureID()")
}

func TestFedAuthFeatureExtID(t *testing.T) {
	t.Parallel()

	fa := featureExtFedAuth{}
	assert.Equal(t, byte(2), fa.featureID(), "featureExtFedAuth.featureID()")
}

func TestColumnStructOriginalTypeInfo(t *testing.T) {
	t.Parallel()

	// Non-encrypted column
	col := columnStruct{
		Flags: 0,
		ti: typeInfo{
			TypeId: typeInt4,
		},
	}
	result := col.originalTypeInfo()
	assert.Equal(t, uint8(typeInt4), result.TypeId, "originalTypeInfo().TypeId")

	// Encrypted column
	cryptoTi := typeInfo{TypeId: typeBigVarChar}
	col2 := columnStruct{
		Flags: colFlagEncrypted,
		ti: typeInfo{
			TypeId: typeInt4,
		},
		cryptoMeta: &cryptoMetadata{
			typeInfo: cryptoTi,
		},
	}
	result2 := col2.originalTypeInfo()
	assert.Equal(t, uint8(typeBigVarChar), result2.TypeId, "originalTypeInfo().TypeId for encrypted")
}

func TestBrowserDataType(t *testing.T) {
	t.Parallel()

	data := msdsn.BrowserData{}
	data["INSTANCE1"] = map[string]string{
		"tcp": "1433",
		"np":  `\\.\pipe\sql\query`,
	}

	assert.Len(t, data, 1, "BrowserData length")
	assert.Equal(t, "1433", data["INSTANCE1"]["tcp"], "BrowserData[INSTANCE1][tcp]")
}
