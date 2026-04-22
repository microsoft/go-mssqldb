package mssql

import (
	"bytes"
	"encoding/binary"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVectorElementTypeString(t *testing.T) {
	testCases := []struct {
		elementType VectorElementType
		expected    string
	}{
		{VectorElementFloat32, "FLOAT32"},
		{VectorElementFloat16, "FLOAT16"},
		{VectorElementType(0xFF), "UNKNOWN(0xFF)"},
	}

	for _, tc := range testCases {
		result := tc.elementType.String()
		assert.Equal(t, tc.expected, result, "String() for 0x%02X", tc.elementType)
	}
}

func TestVectorElementTypeBytesPerElement(t *testing.T) {
	assert.Equal(t, 4, VectorElementFloat32.BytesPerElement(), "Float32 bytes")
	assert.Equal(t, 2, VectorElementFloat16.BytesPerElement(), "Float16 bytes")
}

func TestVectorElementTypeIsValid(t *testing.T) {
	assert.True(t, VectorElementFloat32.IsValid(), "Float32 should be valid")
	assert.True(t, VectorElementFloat16.IsValid(), "Float16 should be valid")
	assert.False(t, VectorElementType(0x99).IsValid(), "Unknown type 0x99 should be invalid")
}

func TestVectorElementTypeMaxDimensions(t *testing.T) {
	assert.Equal(t, 1998, VectorElementFloat32.MaxDimensions(), "Float32 max dims")
	assert.Equal(t, 3996, VectorElementFloat16.MaxDimensions(), "Float16 max dims")
}

func TestVectorEncodeDecode(t *testing.T) {
	testCases := []struct {
		name   string
		vector Vector
	}{
		{
			name:   "empty vector",
			vector: Vector{ElementType: VectorElementFloat32, Data: []float32{}},
		},
		{
			name:   "single element",
			vector: Vector{ElementType: VectorElementFloat32, Data: []float32{1.0}},
		},
		{
			name:   "three elements",
			vector: Vector{ElementType: VectorElementFloat32, Data: []float32{1.0, 2.0, 3.0}},
		},
		{
			name:   "negative values",
			vector: Vector{ElementType: VectorElementFloat32, Data: []float32{-1.0, -2.5, 3.75}},
		},
		{
			name:   "small values",
			vector: Vector{ElementType: VectorElementFloat32, Data: []float32{0.001, 0.002, 0.003}},
		},
		{
			name:   "large values",
			vector: Vector{ElementType: VectorElementFloat32, Data: []float32{1000000.0, 2000000.0, 3000000.0}},
		},
		{
			name:   "mixed values",
			vector: Vector{ElementType: VectorElementFloat32, Data: []float32{0.0, -0.5, 1.5, 100.0, -100.0}},
		},
		{
			name:   "special values",
			vector: Vector{ElementType: VectorElementFloat32, Data: []float32{float32(math.Inf(1)), float32(math.Inf(-1)), float32(math.NaN())}},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Encode
			encoded, err := tc.vector.encodeToBytes()
			if err != nil {
				t.Fatalf("encodeToBytes failed: %v", err)
			}

			// Decode
			var decoded Vector
			err = decoded.decodeFromBytes(encoded)
			if err != nil {
				t.Fatalf("decodeFromBytes failed: %v", err)
			}

			// Compare element type
			if decoded.ElementType != tc.vector.ElementType {
				t.Fatalf("element type mismatch: got %v, want %v", decoded.ElementType, tc.vector.ElementType)
			}

			// Compare dimensions
			if len(decoded.Data) != len(tc.vector.Data) {
				t.Fatalf("length mismatch: got %d, want %d", len(decoded.Data), len(tc.vector.Data))
			}

			for i := range tc.vector.Data {
				// Handle NaN specially
				if math.IsNaN(float64(tc.vector.Data[i])) {
					if !math.IsNaN(float64(decoded.Data[i])) {
						t.Errorf("index %d: expected NaN, got %v", i, decoded.Data[i])
					}
				} else if decoded.Data[i] != tc.vector.Data[i] {
					t.Errorf("index %d: got %v, want %v", i, decoded.Data[i], tc.vector.Data[i])
				}
			}
		})
	}
}

func TestVectorFloat16EncodeDecode(t *testing.T) {
	testCases := []struct {
		name     string
		input    []float32
		expected []float32 // Expected after round-trip (may differ due to precision)
	}{
		{
			name:     "simple values",
			input:    []float32{1.0, 2.0, -2.0, 0.5},
			expected: []float32{1.0, 2.0, -2.0, 0.5},
		},
		{
			name:     "zero values",
			input:    []float32{0.0},
			expected: []float32{0.0},
		},
		{
			name:     "infinity",
			input:    []float32{float32(math.Inf(1)), float32(math.Inf(-1))},
			expected: []float32{float32(math.Inf(1)), float32(math.Inf(-1))},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			v := Vector{ElementType: VectorElementFloat16, Data: tc.input}

			// Encode
			encoded, err := v.encodeToBytes()
			if err != nil {
				t.Fatalf("encodeToBytes failed: %v", err)
			}

			// Verify header shows float16
			if encoded[4] != byte(VectorElementFloat16) {
				t.Errorf("element type byte: got 0x%02X, want 0x%02X", encoded[4], VectorElementFloat16)
			}

			// Decode
			var decoded Vector
			err = decoded.decodeFromBytes(encoded)
			if err != nil {
				t.Fatalf("decodeFromBytes failed: %v", err)
			}

			if decoded.ElementType != VectorElementFloat16 {
				t.Fatalf("element type mismatch: got %v, want float16", decoded.ElementType)
			}

			if len(decoded.Data) != len(tc.expected) {
				t.Fatalf("length mismatch: got %d, want %d", len(decoded.Data), len(tc.expected))
			}

			for i := range tc.expected {
				if math.IsNaN(float64(tc.expected[i])) {
					if !math.IsNaN(float64(decoded.Data[i])) {
						t.Errorf("index %d: expected NaN, got %v", i, decoded.Data[i])
					}
				} else if math.IsInf(float64(tc.expected[i]), 0) {
					if decoded.Data[i] != tc.expected[i] {
						t.Errorf("index %d: got %v, want %v", i, decoded.Data[i], tc.expected[i])
					}
				} else if decoded.Data[i] != tc.expected[i] {
					t.Errorf("index %d: got %v, want %v", i, decoded.Data[i], tc.expected[i])
				}
			}
		})
	}
}

func TestFloat32ToFloat16Conversion(t *testing.T) {
	testCases := []struct {
		name     string
		input    float32
		expected uint16
	}{
		{"positive 1.0", 1.0, 0x3C00},
		{"negative 2.0", -2.0, 0xC000},
		{"half 0.5", 0.5, 0x3800},
		{"zero", 0.0, 0x0000},
		{"positive infinity", float32(math.Inf(1)), 0x7C00},
		{"negative infinity", float32(math.Inf(-1)), 0xFC00},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := float32ToFloat16(tc.input)
			if result != tc.expected {
				t.Errorf("float32ToFloat16(%v): got 0x%04X, want 0x%04X", tc.input, result, tc.expected)
			}
		})
	}

	// Test NaN separately - IEEE 754 allows multiple valid NaN representations.
	// Any value with exponent=0x1F (all ones) and non-zero fraction is a valid NaN.
	// Positive NaN: 0x7C01-0x7FFF, Negative NaN: 0xFC01-0xFFFF
	nanResult := float32ToFloat16(float32(math.NaN()))
	// Use float16InfBits (0x7C00) as exponent mask and float16MantissaMask (0x03FF) for fraction
	if (nanResult&float16InfBits) != float16InfBits || (nanResult&float16MantissaMask) == 0 {
		t.Errorf("float32ToFloat16(NaN): got 0x%04X, want any NaN (exp=0x1F, non-zero fraction)", nanResult)
	}
}

func TestFloat16ToFloat32Conversion(t *testing.T) {
	testCases := []struct {
		name     string
		input    uint16
		expected float32
	}{
		{"positive 1.0", 0x3C00, 1.0},
		{"negative 2.0", 0xC000, -2.0},
		{"half 0.5", 0x3800, 0.5},
		{"zero", 0x0000, 0.0},
		{"positive infinity", 0x7C00, float32(math.Inf(1))},
		{"negative infinity", 0xFC00, float32(math.Inf(-1))},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := float16ToFloat32(tc.input)
			if result != tc.expected {
				t.Errorf("float16ToFloat32(0x%04X): got %v, want %v", tc.input, result, tc.expected)
			}
		})
	}

	// Test NaN separately
	nanResult := float16ToFloat32(0x7E00)
	if !math.IsNaN(float64(nanResult)) {
		t.Errorf("float16ToFloat32(0x7E00): got %v, want NaN", nanResult)
	}
}

func TestVectorHeader(t *testing.T) {
	v := Vector{ElementType: VectorElementFloat32, Data: []float32{1.0, 2.0, 3.0}}
	encoded, err := v.encodeToBytes()
	if err != nil {
		t.Fatalf("encodeToBytes failed: %v", err)
	}

	// Check header
	if encoded[0] != vectorMagic {
		t.Errorf("magic byte: got 0x%02X, want 0x%02X", encoded[0], vectorMagic)
	}
	if encoded[1] != vectorVersion {
		t.Errorf("version byte: got 0x%02X, want 0x%02X", encoded[1], vectorVersion)
	}

	dimensions := binary.LittleEndian.Uint16(encoded[2:4])
	if dimensions != 3 {
		t.Errorf("dimensions: got %d, want 3", dimensions)
	}

	if encoded[4] != byte(VectorElementFloat32) {
		t.Errorf("element type: got 0x%02X, want 0x%02X", encoded[4], VectorElementFloat32)
	}

	// Reserved bytes should be zero
	if encoded[5] != 0 || encoded[6] != 0 || encoded[7] != 0 {
		t.Errorf("reserved bytes should be zero: got 0x%02X 0x%02X 0x%02X", encoded[5], encoded[6], encoded[7])
	}

	// Check total size
	expectedSize := vectorHeaderSize + 3*4 // 8 + 12 = 20 bytes
	if len(encoded) != expectedSize {
		t.Errorf("total size: got %d, want %d", len(encoded), expectedSize)
	}
}

func TestVectorDecodeInvalidData(t *testing.T) {
	testCases := []struct {
		name string
		data []byte
	}{
		{
			name: "too short for header",
			data: []byte{0xA9, 0x01, 0x03, 0x00},
		},
		{
			name: "wrong magic byte",
			data: []byte{0xAA, 0x01, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		},
		{
			name: "wrong version",
			data: []byte{0xA9, 0x02, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		},
		{
			name: "unsupported element type",
			data: []byte{0xA9, 0x01, 0x01, 0x00, 0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, // type 0x02 is not supported
		},
		{
			name: "data too short for float32 dimensions",
			data: []byte{0xA9, 0x01, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, // says 3 dims but only 1 value
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var v Vector
			err := v.decodeFromBytes(tc.data)
			if err == nil {
				t.Error("expected error but got nil")
			}
		})
	}
}

func TestVectorNil(t *testing.T) {
	v := Vector{Data: nil}

	encoded, err := v.encodeToBytes()
	if err != nil {
		t.Fatalf("encodeToBytes failed: %v", err)
	}
	if encoded != nil {
		t.Errorf("nil data vector should encode to nil, got %v", encoded)
	}

	var decoded Vector
	err = decoded.decodeFromBytes(nil)
	if err != nil {
		t.Fatalf("decodeFromBytes(nil) failed: %v", err)
	}
	if decoded.Data != nil {
		t.Errorf("decoded nil should have nil Data, got %v", decoded.Data)
	}

	// Empty (non-nil) buffer should return an error (truncated/corrupt data)
	err = decoded.decodeFromBytes([]byte{})
	if err == nil {
		t.Fatal("decodeFromBytes([]) should fail for empty non-nil buffer")
	}
}

func TestVectorIsNull(t *testing.T) {
	v := Vector{}
	if !v.IsNull() {
		t.Error("empty Vector should be null")
	}

	v = Vector{Data: nil}
	if !v.IsNull() {
		t.Error("Vector with nil Data should be null")
	}

	v = Vector{ElementType: VectorElementFloat32, Data: []float32{1.0}}
	if v.IsNull() {
		t.Error("Vector with data should not be null")
	}
}

func TestVectorValue(t *testing.T) {
	v := Vector{ElementType: VectorElementFloat32, Data: []float32{1.0, 2.0, 3.0}}

	val, err := v.Value()
	if err != nil {
		t.Fatalf("Value() failed: %v", err)
	}

	// Value() returns a JSON string for SQL Server parameter binding
	jsonStr, ok := val.(string)
	if !ok {
		t.Fatalf("Value() should return string, got %T", val)
	}

	expectedJSON := "[1, 2, 3]"
	if jsonStr != expectedJSON {
		t.Errorf("Value() returned %q, expected %q", jsonStr, expectedJSON)
	}

	// Test that the JSON can be decoded back to a Vector
	var decoded Vector
	err = decoded.decodeFromJSON(jsonStr)
	if err != nil {
		t.Fatalf("decodeFromJSON failed: %v", err)
	}

	if len(decoded.Data) != len(v.Data) {
		t.Fatalf("length mismatch: got %d, want %d", len(decoded.Data), len(v.Data))
	}

	for i := range v.Data {
		if decoded.Data[i] != v.Data[i] {
			t.Errorf("index %d: got %v, want %v", i, decoded.Data[i], v.Data[i])
		}
	}
}

func TestVectorValueNaNRoundTrip(t *testing.T) {
	// Test that NaN values can round-trip through ToJSON/decodeFromJSON
	// ToJSON encodes NaN as null, decodeFromJSON should decode null back to NaN
	v := Vector{ElementType: VectorElementFloat32, Data: []float32{1.0, float32(math.NaN()), 3.0}}

	val, err := v.Value()
	if err != nil {
		t.Fatalf("Value() failed: %v", err)
	}

	jsonStr, ok := val.(string)
	if !ok {
		t.Fatalf("Value() should return string, got %T", val)
	}

	// NaN should be encoded as null
	expectedJSON := "[1, null, 3]"
	if jsonStr != expectedJSON {
		t.Errorf("Value() returned %q, expected %q", jsonStr, expectedJSON)
	}

	// Test that the JSON can be decoded back to a Vector with NaN
	var decoded Vector
	err = decoded.decodeFromJSON(jsonStr)
	if err != nil {
		t.Fatalf("decodeFromJSON failed: %v", err)
	}

	if len(decoded.Data) != len(v.Data) {
		t.Fatalf("length mismatch: got %d, want %d", len(decoded.Data), len(v.Data))
	}

	// First element should match
	if decoded.Data[0] != 1.0 {
		t.Errorf("index 0: got %v, want 1.0", decoded.Data[0])
	}

	// Second element should be NaN (null in JSON -> NaN)
	if !math.IsNaN(float64(decoded.Data[1])) {
		t.Errorf("index 1: got %v, want NaN", decoded.Data[1])
	}

	// Third element should match
	if decoded.Data[2] != 3.0 {
		t.Errorf("index 2: got %v, want 3.0", decoded.Data[2])
	}
}

func TestVectorValueInfRejected(t *testing.T) {
	// Test that Inf values cause Value() to return an error
	// (Inf cannot be losslessly round-tripped through JSON)
	v := Vector{ElementType: VectorElementFloat32, Data: []float32{1.0, float32(math.Inf(1)), 3.0}}

	_, err := v.Value()
	if err == nil {
		t.Fatal("Value() should return error for vector with Inf values")
	}

	// Negative infinity should also fail
	v = Vector{ElementType: VectorElementFloat32, Data: []float32{float32(math.Inf(-1))}}
	_, err = v.Value()
	if err == nil {
		t.Fatal("Value() should return error for vector with -Inf values")
	}
}

func TestVectorScan(t *testing.T) {
	original := Vector{ElementType: VectorElementFloat32, Data: []float32{1.0, 2.0, 3.0}}
	encoded, _ := original.encodeToBytes()

	var v Vector
	err := v.Scan(encoded)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if len(v.Data) != len(original.Data) {
		t.Fatalf("length mismatch: got %d, want %d", len(v.Data), len(original.Data))
	}

	for i := range original.Data {
		if v.Data[i] != original.Data[i] {
			t.Errorf("index %d: got %v, want %v", i, v.Data[i], original.Data[i])
		}
	}

	// Scan nil
	err = v.Scan(nil)
	if err != nil {
		t.Fatalf("Scan(nil) failed: %v", err)
	}
	if v.Data != nil {
		t.Errorf("Scan(nil) should set Data to nil")
	}
}

func TestVectorScanFromFloat64Slice(t *testing.T) {
	// Test scanning from []float64 to Vector
	float64Values := []float64{1.5, 2.5, 3.5}
	var v Vector
	err := v.Scan(float64Values)
	if err != nil {
		t.Fatalf("Scan([]float64) failed: %v", err)
	}

	if len(v.Data) != len(float64Values) {
		t.Fatalf("length mismatch: got %d, want %d", len(v.Data), len(float64Values))
	}

	for i, val := range float64Values {
		if v.Data[i] != float32(val) {
			t.Errorf("index %d: got %v, want %v", i, v.Data[i], float32(val))
		}
	}
}

func TestVectorScanFromFloat32Slice(t *testing.T) {
	// Test scanning from []float32 to Vector (should copy, not reference)
	float32Values := []float32{1.0, 2.0, 3.0}
	var v Vector
	err := v.Scan(float32Values)
	if err != nil {
		t.Fatalf("Scan([]float32) failed: %v", err)
	}

	if len(v.Data) != len(float32Values) {
		t.Fatalf("length mismatch: got %d, want %d", len(v.Data), len(float32Values))
	}

	// Modify original - should not affect scanned value
	float32Values[0] = 999.0
	if v.Data[0] == 999.0 {
		t.Error("Scan([]float32) should copy the slice, not reference it")
	}
}

func TestNullVector(t *testing.T) {
	// Valid value
	nv := NullVector{
		Vector: Vector{ElementType: VectorElementFloat32, Data: []float32{1.0, 2.0, 3.0}},
		Valid:  true,
	}

	val, err := nv.Value()
	if err != nil {
		t.Fatalf("Value() failed: %v", err)
	}
	if val == nil {
		t.Error("Value() should not be nil for valid NullVector")
	}

	// Null value
	nv = NullVector{Valid: false}
	val, err = nv.Value()
	if err != nil {
		t.Fatalf("Value() failed: %v", err)
	}
	if val != nil {
		t.Error("Value() should be nil for invalid NullVector")
	}

	// Scan valid
	encoded, _ := (Vector{ElementType: VectorElementFloat32, Data: []float32{1.0, 2.0}}).encodeToBytes()
	err = nv.Scan(encoded)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	if !nv.Valid {
		t.Error("NullVector should be valid after scanning data")
	}
	if len(nv.Vector.Data) != 2 {
		t.Errorf("Vector length: got %d, want 2", len(nv.Vector.Data))
	}

	// Scan nil
	err = nv.Scan(nil)
	if err != nil {
		t.Fatalf("Scan(nil) failed: %v", err)
	}
	if nv.Valid {
		t.Error("NullVector should not be valid after scanning nil")
	}
}

func TestNullVectorScanError(t *testing.T) {
	// Test that NullVector.Scan sets Valid=false on scan error
	nv := NullVector{
		Vector: Vector{ElementType: VectorElementFloat32, Data: []float32{1.0}},
		Valid:  true, // Start as valid
	}

	// Invalid vector data (wrong magic byte)
	invalidData := []byte{0xAA, 0x01, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	err := nv.Scan(invalidData)
	if err == nil {
		t.Error("Expected error for invalid vector data")
	}
	if nv.Valid {
		t.Error("NullVector.Valid should be false after scan error")
	}
}

func TestVectorScanUnsupportedType(t *testing.T) {
	var v Vector
	err := v.Scan(42) // int is not supported
	if err == nil {
		t.Error("Expected error for unsupported type")
	}
	if !strings.Contains(err.Error(), "cannot convert") {
		t.Errorf("Expected 'cannot convert' error, got: %v", err)
	}
}

func TestVectorDecodeEmptyJSON(t *testing.T) {
	var v Vector
	err := v.decodeFromJSON("[]")
	if err != nil {
		t.Fatalf("decodeFromJSON([]) failed: %v", err)
	}
	if v.Data == nil {
		t.Error("Empty array should produce non-nil slice")
	}
	if len(v.Data) != 0 {
		t.Error("Empty array should produce zero-length slice")
	}
}

func TestVectorDecodeNullJSON(t *testing.T) {
	var v Vector
	err := v.decodeFromJSON("null")
	if err != nil {
		t.Fatalf("decodeFromJSON(null) failed: %v", err)
	}
	if v.Data != nil {
		t.Error("JSON null should produce nil Data (SQL NULL)")
	}
}

func TestVectorDecodeInvalidJSON(t *testing.T) {
	var v Vector
	err := v.decodeFromJSON("not json")
	if err == nil {
		t.Error("Expected error for malformed JSON")
	}
}

func TestVectorString(t *testing.T) {
	testCases := []struct {
		vector   Vector
		expected string
	}{
		{Vector{}, "NULL"},
		{Vector{Data: nil}, "NULL"},
		{Vector{ElementType: VectorElementFloat32, Data: []float32{}}, "VECTOR(FLOAT32, 0) : []"},
		{Vector{ElementType: VectorElementFloat32, Data: []float32{1.0}}, "VECTOR(FLOAT32, 1) : [1]"},
		{Vector{ElementType: VectorElementFloat32, Data: []float32{1.0, 2.0, 3.0}}, "VECTOR(FLOAT32, 3) : [1, 2, 3]"},
		{Vector{ElementType: VectorElementFloat16, Data: []float32{1.5, -2.5}}, "VECTOR(FLOAT16, 2) : [1.5, -2.5]"},
	}

	for _, tc := range testCases {
		result := tc.vector.String()
		if result != tc.expected {
			t.Errorf("String() for %v: got %q, want %q", tc.vector, result, tc.expected)
		}
	}
}

func TestVectorDimensions(t *testing.T) {
	testCases := []struct {
		vector   Vector
		expected int
	}{
		{Vector{}, 0},
		{Vector{Data: nil}, 0},
		{Vector{ElementType: VectorElementFloat32, Data: []float32{}}, 0},
		{Vector{ElementType: VectorElementFloat32, Data: []float32{1.0}}, 1},
		{Vector{ElementType: VectorElementFloat32, Data: []float32{1.0, 2.0, 3.0}}, 3},
	}

	for _, tc := range testCases {
		result := tc.vector.Dimensions()
		if result != tc.expected {
			t.Errorf("Dimensions() for %v: got %d, want %d", tc.vector, result, tc.expected)
		}
	}
}

func TestNewVector(t *testing.T) {
	values := []float32{1.0, 2.0, 3.0}
	v, err := NewVector(values)
	if err != nil {
		t.Fatalf("NewVector failed: %v", err)
	}

	if len(v.Data) != len(values) {
		t.Fatalf("length mismatch: got %d, want %d", len(v.Data), len(values))
	}

	if v.ElementType != VectorElementFloat32 {
		t.Errorf("element type: got %v, want float32", v.ElementType)
	}

	// Verify it's a copy
	values[0] = 999.0
	if v.Data[0] == 999.0 {
		t.Error("NewVector should create a copy of the slice")
	}
}

func TestNewVectorWithType(t *testing.T) {
	values := []float32{1.0, 2.0, 3.0}

	v, err := NewVectorWithType(VectorElementFloat16, values)
	if err != nil {
		t.Fatalf("NewVectorWithType failed: %v", err)
	}

	if v.ElementType != VectorElementFloat16 {
		t.Errorf("element type: got %v, want float16", v.ElementType)
	}

	if len(v.Data) != len(values) {
		t.Fatalf("length mismatch: got %d, want %d", len(v.Data), len(values))
	}
}

func TestNewVectorFromFloat64(t *testing.T) {
	values := []float64{1.0, 2.0, 3.0}
	v, err := NewVectorFromFloat64(values)
	if err != nil {
		t.Fatalf("NewVectorFromFloat64 failed: %v", err)
	}

	if len(v.Data) != len(values) {
		t.Fatalf("length mismatch: got %d, want %d", len(v.Data), len(values))
	}

	if v.ElementType != VectorElementFloat32 {
		t.Errorf("element type: got %v, want float32", v.ElementType)
	}

	for i, val := range values {
		if v.Data[i] != float32(val) {
			t.Errorf("index %d: got %v, want %v", i, v.Data[i], float32(val))
		}
	}
}

func TestVectorToFloat64(t *testing.T) {
	v := Vector{ElementType: VectorElementFloat32, Data: []float32{1.0, 2.0, 3.0}}
	result := v.ToFloat64()

	if len(result) != len(v.Data) {
		t.Fatalf("length mismatch: got %d, want %d", len(result), len(v.Data))
	}

	for i, val := range v.Data {
		if result[i] != float64(val) {
			t.Errorf("index %d: got %v, want %v", i, result[i], float64(val))
		}
	}

	// nil data vector
	nilV := Vector{}
	nilResult := nilV.ToFloat64()
	if nilResult != nil {
		t.Error("ToFloat64() for nil data vector should return nil")
	}
}

func TestVectorValues(t *testing.T) {
	original := []float32{1.0, 2.0, 3.0}
	v := Vector{ElementType: VectorElementFloat32, Data: original}

	values := v.Values()

	if len(values) != len(original) {
		t.Fatalf("length mismatch: got %d, want %d", len(values), len(original))
	}

	// Verify it's a copy
	values[0] = 999.0
	if v.Data[0] == 999.0 {
		t.Error("Values() should return a copy")
	}

	// nil data
	nilV := Vector{}
	if nilV.Values() != nil {
		t.Error("Values() for nil data should return nil")
	}
}

func TestVectorMaxDimensions(t *testing.T) {
	// Test at maximum allowed for float32
	maxValues := make([]float32, vectorMaxDimensionsFloat32)
	v, err := NewVector(maxValues)
	if err != nil {
		t.Fatalf("NewVector at max dimensions failed: %v", err)
	}
	if len(v.Data) != vectorMaxDimensionsFloat32 {
		t.Errorf("length: got %d, want %d", len(v.Data), vectorMaxDimensionsFloat32)
	}

	// Test exceeding maximum for float32
	tooManyValues := make([]float32, vectorMaxDimensionsFloat32+1)
	_, err = NewVector(tooManyValues)
	if err == nil {
		t.Error("NewVector should fail when exceeding max dimensions")
	}

	// Test at maximum allowed for float16
	maxFloat16 := make([]float32, vectorMaxDimensionsFloat16)
	v, err = NewVectorWithType(VectorElementFloat16, maxFloat16)
	if err != nil {
		t.Fatalf("NewVectorWithType(float16) at max dimensions failed: %v", err)
	}

	// Test encode with oversized vector
	oversizedVector := Vector{ElementType: VectorElementFloat32, Data: tooManyValues}
	_, err = oversizedVector.encodeToBytes()
	if err == nil {
		t.Error("encodeToBytes should fail when exceeding max dimensions")
	}
}

func TestVectorBinaryFormat(t *testing.T) {
	// Test that encoding matches expected TDS format
	v := Vector{ElementType: VectorElementFloat32, Data: []float32{1.0, 2.0}}
	encoded, err := v.encodeToBytes()
	if err != nil {
		t.Fatalf("encodeToBytes failed: %v", err)
	}

	// Expected format:
	// Header: A9 01 02 00 00 00 00 00 (magic, version, 2 dims, float32 type, 3 reserved)
	// Data: 00 00 80 3F (1.0 as float32 LE), 00 00 00 40 (2.0 as float32 LE)
	expected := []byte{
		0xA9, 0x01, 0x02, 0x00, 0x00, 0x00, 0x00, 0x00, // Header
		0x00, 0x00, 0x80, 0x3F, // 1.0f
		0x00, 0x00, 0x00, 0x40, // 2.0f
	}

	if !bytes.Equal(encoded, expected) {
		t.Errorf("binary format mismatch:\ngot:  %v\nwant: %v", encoded, expected)
	}
}

func TestVectorFloat16BinaryFormat(t *testing.T) {
	// Test float16 binary encoding
	v := Vector{ElementType: VectorElementFloat16, Data: []float32{1.0, 2.0}}
	encoded, err := v.encodeToBytes()
	if err != nil {
		t.Fatalf("encodeToBytes failed: %v", err)
	}

	// Expected format:
	// Header: A9 01 02 00 01 00 00 00 (magic, version, 2 dims, float16 type=0x01, 3 reserved)
	// Data: 00 3C (1.0 as float16 LE), 00 40 (2.0 as float16 LE)
	expected := []byte{
		0xA9, 0x01, 0x02, 0x00, 0x01, 0x00, 0x00, 0x00, // Header
		0x00, 0x3C, // 1.0 as float16
		0x00, 0x40, // 2.0 as float16
	}

	if !bytes.Equal(encoded, expected) {
		t.Errorf("float16 binary format mismatch:\ngot:  %v\nwant: %v", encoded, expected)
	}

	// Verify total size (8 header + 2*2 data = 12 bytes)
	if len(encoded) != 12 {
		t.Errorf("total size: got %d, want 12", len(encoded))
	}
}

func TestVectorPrecisionLossWarning(t *testing.T) {
	// Track precision loss warnings
	var warnings []struct {
		index     int
		original  float64
		converted float32
	}

	// Set custom handler using the thread-safe setter
	SetVectorPrecisionLossHandler(func(index int, original float64, converted float32) {
		warnings = append(warnings, struct {
			index     int
			original  float64
			converted float32
		}{index, original, converted})
	})
	defer SetVectorPrecisionLossHandler(nil)

	// Test with value that loses precision
	preciseValue := 0.123456789012345 // More precision than float32 can hold
	_, err := NewVectorFromFloat64([]float64{1.0, preciseValue, 3.0})
	if err != nil {
		t.Fatalf("NewVectorFromFloat64 failed: %v", err)
	}

	// Should have exactly one warning (only first precision loss reported)
	if len(warnings) != 1 {
		t.Errorf("expected 1 warning, got %d", len(warnings))
	}
	if len(warnings) > 0 && warnings[0].index != 1 {
		t.Errorf("expected warning at index 1, got %d", warnings[0].index)
	}

	// Test with values that don't lose precision
	warnings = nil
	_, err = NewVectorFromFloat64([]float64{1.0, 2.0, 3.0})
	if err != nil {
		t.Fatalf("NewVectorFromFloat64 failed: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("expected no warnings for exact values, got %d", len(warnings))
	}

	// Test SetVectorPrecisionWarnings
	SetVectorPrecisionWarnings(false)
	// Handler should now be nil - verify by checking no warnings fire
	warnings = nil
	_, _ = NewVectorFromFloat64([]float64{preciseValue})
	if len(warnings) != 0 {
		t.Error("SetVectorPrecisionWarnings(false) should disable warnings")
	}
}

// TestVectorTypeFunctions tests the type-related functions for Vector type.
// These tests cover code paths in types.go that are otherwise only exercised
// during database operations.
func TestVectorTypeFunctions(t *testing.T) {
	// Test makeDecl for typeVectorN with float32
	t.Run("makeDecl float32", func(t *testing.T) {
		ti := typeInfo{TypeId: typeVectorN, Size: 20, Scale: 0} // 3 float32 = 12 bytes data + 8 bytes header = 20
		decl := makeDecl(ti)
		expected := "vector(3)"
		if decl != expected {
			t.Errorf("Expected makeDecl to return %q, got: %q", expected, decl)
		}
	})

	t.Run("makeDecl float16", func(t *testing.T) {
		ti := typeInfo{TypeId: typeVectorN, Size: 14, Scale: 1} // 3 float16 = 6 bytes data + 8 bytes header = 14
		decl := makeDecl(ti)
		expected := "vector(3, float16)"
		if decl != expected {
			t.Errorf("Expected makeDecl to return %q, got: %q", expected, decl)
		}
	})

	t.Run("makeDecl invalid size", func(t *testing.T) {
		// Test with size too small for header
		ti := typeInfo{TypeId: typeVectorN, Size: 4, Scale: 0}
		decl := makeDecl(ti)
		expected := "vector"
		if decl != expected {
			t.Errorf("Expected makeDecl to return %q, got: %q", expected, decl)
		}
	})

	t.Run("makeDecl zero payload", func(t *testing.T) {
		// Test with size == header (0 dimensions)
		ti := typeInfo{TypeId: typeVectorN, Size: 8, Scale: 0} // size == vectorHeaderSize, 0 dimensions
		decl := makeDecl(ti)
		// payloadSize == 0, so returns "vector"
		expected := "vector"
		if decl != expected {
			t.Errorf("Expected makeDecl to return %q for zero-payload, got: %q", expected, decl)
		}
	})

	t.Run("makeDecl misaligned payload", func(t *testing.T) {
		// Test with payload size that doesn't divide evenly by bytesPerElement
		ti := typeInfo{TypeId: typeVectorN, Size: 11, Scale: 0} // 8 header + 3 data (not divisible by 4)
		decl := makeDecl(ti)
		expected := "vector"
		if decl != expected {
			t.Errorf("Expected makeDecl to return %q for misaligned payload, got: %q", expected, decl)
		}
	})

	// Test makeGoLangTypeName for typeVectorN
	t.Run("makeGoLangTypeName", func(t *testing.T) {
		ti := typeInfo{TypeId: typeVectorN}
		typeName := makeGoLangTypeName(ti)
		if typeName != "VECTOR" {
			t.Errorf("Expected makeGoLangTypeName to return 'VECTOR', got: %s", typeName)
		}
	})

	// Test makeGoLangScanType for typeVectorN
	t.Run("makeGoLangScanType", func(t *testing.T) {
		// All vectors should return []byte (raw binary payload)
		ti := typeInfo{TypeId: typeVectorN, Scale: 0}
		scanType := makeGoLangScanType(ti)
		expected := "[]uint8" // []byte is an alias for []uint8
		if scanType.String() != expected {
			t.Errorf("Expected scan type %s for float32 Vector, got %s", expected, scanType.String())
		}

		// float16 vectors also return []byte
		ti16 := typeInfo{TypeId: typeVectorN, Scale: 1}
		scanType16 := makeGoLangScanType(ti16)
		if scanType16.String() != expected {
			t.Errorf("Expected scan type %s for float16 Vector, got %s", expected, scanType16.String())
		}
	})

	// Test makeGoLangTypeLength for typeVectorN
	t.Run("makeGoLangTypeLength float32", func(t *testing.T) {
		ti := typeInfo{TypeId: typeVectorN, Size: 20, Scale: 0} // 3 float32
		length, hasLength := makeGoLangTypeLength(ti)
		if !hasLength {
			t.Error("Expected makeGoLangTypeLength to return true for Vector")
		}
		expectedLength := int64(3) // Number of dimensions
		if length != expectedLength {
			t.Errorf("Expected length %d, got: %d", expectedLength, length)
		}
	})

	t.Run("makeGoLangTypeLength float16", func(t *testing.T) {
		ti := typeInfo{TypeId: typeVectorN, Size: 14, Scale: 1} // 3 float16
		length, hasLength := makeGoLangTypeLength(ti)
		if !hasLength {
			t.Error("Expected makeGoLangTypeLength to return true for Vector")
		}
		expectedLength := int64(3) // Number of dimensions
		if length != expectedLength {
			t.Errorf("Expected length %d, got: %d", expectedLength, length)
		}
	})

	t.Run("makeGoLangTypeLength invalid scale", func(t *testing.T) {
		// Unknown scale (not 0 or 1) should return false
		ti := typeInfo{TypeId: typeVectorN, Size: 20, Scale: 99}
		_, hasLength := makeGoLangTypeLength(ti)
		if hasLength {
			t.Error("Expected makeGoLangTypeLength to return false for unknown scale")
		}
	})

	t.Run("makeGoLangTypeLength PLP marker", func(t *testing.T) {
		// PLP/MAX marker (0xffff) should return false
		ti := typeInfo{TypeId: typeVectorN, Size: 0xffff, Scale: 0}
		_, hasLength := makeGoLangTypeLength(ti)
		if hasLength {
			t.Error("Expected makeGoLangTypeLength to return false for PLP marker")
		}
	})

	t.Run("makeGoLangTypeLength too small", func(t *testing.T) {
		// Size smaller than header should return false
		ti := typeInfo{TypeId: typeVectorN, Size: 4, Scale: 0}
		_, hasLength := makeGoLangTypeLength(ti)
		if hasLength {
			t.Error("Expected makeGoLangTypeLength to return false for size < header")
		}
	})

	t.Run("makeGoLangTypeLength misaligned", func(t *testing.T) {
		// Payload not divisible by bytesPerElement should return false
		ti := typeInfo{TypeId: typeVectorN, Size: 11, Scale: 0} // 8 header + 3 bytes (not divisible by 4)
		_, hasLength := makeGoLangTypeLength(ti)
		if hasLength {
			t.Error("Expected makeGoLangTypeLength to return false for misaligned payload")
		}
	})

	// Test makeGoLangTypePrecisionScale for typeVectorN
	t.Run("makeGoLangTypePrecisionScale", func(t *testing.T) {
		ti := typeInfo{TypeId: typeVectorN}
		prec, scale, hasPrecScale := makeGoLangTypePrecisionScale(ti)
		if hasPrecScale {
			t.Error("Expected makeGoLangTypePrecisionScale to return false for Vector")
		}
		if prec != 0 || scale != 0 {
			t.Errorf("Expected prec=0, scale=0, got prec=%d, scale=%d", prec, scale)
		}
	})
}

// TestConvertInputParameterVector tests the convertInputParameter function for Vector types.
func TestConvertInputParameterVector(t *testing.T) {
	t.Run("Vector value", func(t *testing.T) {
		v := Vector{ElementType: VectorElementFloat32, Data: []float32{1.0, 2.0, 3.0}}
		result, err := convertInputParameter(v)
		if err != nil {
			t.Fatalf("convertInputParameter(Vector) returned error: %v", err)
		}
		if converted, ok := result.(Vector); !ok {
			t.Errorf("Expected Vector type, got %T", result)
		} else if len(converted.Data) != 3 {
			t.Errorf("Expected 3 elements, got %d", len(converted.Data))
		}
	})

	t.Run("NullVector valid value", func(t *testing.T) {
		v := NullVector{Vector: Vector{ElementType: VectorElementFloat32, Data: []float32{1.0, 2.0}}, Valid: true}
		result, err := convertInputParameter(v)
		if err != nil {
			t.Fatalf("convertInputParameter(NullVector) returned error: %v", err)
		}
		if converted, ok := result.(NullVector); !ok {
			t.Errorf("Expected NullVector type, got %T", result)
		} else if !converted.Valid {
			t.Error("Expected Valid to be true")
		}
	})

	t.Run("NullVector null value", func(t *testing.T) {
		v := NullVector{Valid: false}
		result, err := convertInputParameter(v)
		if err != nil {
			t.Fatalf("convertInputParameter(NullVector) returned error: %v", err)
		}
		if converted, ok := result.(NullVector); !ok {
			t.Errorf("Expected NullVector type, got %T", result)
		} else if converted.Valid {
			t.Error("Expected Valid to be false")
		}
	})
}
