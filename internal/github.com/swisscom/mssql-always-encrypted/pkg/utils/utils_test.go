package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConvertUTF16ToLittleEndianBytes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    []uint16
		expected []byte
	}{
		{"empty", []uint16{}, []byte{}},
		{"single char A", []uint16{0x0041}, []byte{0x41, 0x00}},
		{"ABC", []uint16{0x0041, 0x0042, 0x0043}, []byte{0x41, 0x00, 0x42, 0x00, 0x43, 0x00}},
		{"high byte", []uint16{0xFF00}, []byte{0x00, 0xFF}},
		{"max value", []uint16{0xFFFF}, []byte{0xFF, 0xFF}},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := ConvertUTF16ToLittleEndianBytes(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestProcessUTF16LE(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected []byte
	}{
		{"empty", "", []byte{}},
		{"ASCII", "ABC", []byte{0x41, 0x00, 0x42, 0x00, 0x43, 0x00}},
		{"hello", "hello", []byte{0x68, 0x00, 0x65, 0x00, 0x6c, 0x00, 0x6c, 0x00, 0x6f, 0x00}},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := ProcessUTF16LE(tc.input)
			assert.Equal(t, tc.expected, result, "ProcessUTF16LE(%q)", tc.input)
		})
	}
}

func TestProcessUTF16LE_RoundTrip(t *testing.T) {
	t.Parallel()
	testStrings := []string{
		"Hello, World!",
		"test123",
		"",
		"A",
	}

	for _, s := range testStrings {
		result := ProcessUTF16LE(s)
		// Verify length is correct (2 bytes per character)
		expectedLen := len(s) * 2
		assert.Len(t, result, expectedLen, "ProcessUTF16LE(%q) length", s)
	}
}
