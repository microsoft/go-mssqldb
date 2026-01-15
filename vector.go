package mssql

import (
	"database/sql/driver"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// VectorElementType represents the element type of a SQL Server Vector.
// SQL Server 2025 supports float32 (default) and float16.
type VectorElementType byte

const (
	// VectorElementFloat32 represents 32-bit single-precision floating point (4 bytes per element).
	// This is the default element type for SQL Server vectors.
	VectorElementFloat32 VectorElementType = 0x00

	// VectorElementFloat16 represents 16-bit half-precision floating point (2 bytes per element).
	// Note: float16 is a preview feature in SQL Server requiring PREVIEW_FEATURES = ON.
	// float16 vectors are currently transmitted as JSON over TDS, not binary.
	// The element type is determined by the SQL column definition, not the Go-side struct.
	VectorElementFloat16 VectorElementType = 0x01
)

// Vector type constants for SQL Server 2025+ Vector data type
const (
	// Vector header constants
	vectorMagic      = 0xA9 // Magic byte identifying vector data
	vectorVersion    = 0x01 // Current vector format version
	vectorHeaderSize = 8    // Size of vector header in bytes

	// Maximum dimensions for a vector
	// For float32: (8000 - 8) / 4 = 1998 dimensions
	// For float16: (8000 - 8) / 2 = 3996 dimensions
	vectorMaxDimensionsFloat32 = 1998
	vectorMaxDimensionsFloat16 = 3996
)

// String returns the string representation of the element type.
// Uses uppercase names (FLOAT32, FLOAT16) for consistency with JDBC and SqlClient drivers.
func (t VectorElementType) String() string {
	switch t {
	case VectorElementFloat32:
		return "FLOAT32"
	case VectorElementFloat16:
		return "FLOAT16"
	default:
		return fmt.Sprintf("UNKNOWN(0x%02X)", byte(t))
	}
}

// BytesPerElement returns the number of bytes per element for this type.
func (t VectorElementType) BytesPerElement() int {
	switch t {
	case VectorElementFloat32:
		return 4
	case VectorElementFloat16:
		return 2
	default:
		return 4 // Default to float32
	}
}

// MaxDimensions returns the maximum number of dimensions for this element type.
func (t VectorElementType) MaxDimensions() int {
	switch t {
	case VectorElementFloat32:
		return vectorMaxDimensionsFloat32
	case VectorElementFloat16:
		return vectorMaxDimensionsFloat16
	default:
		return vectorMaxDimensionsFloat32
	}
}

// Vector represents the SQL Server Vector data type introduced in SQL Server 2025.
// It stores an array of floating-point values, commonly used for AI/ML embedding scenarios.
//
// SQL Server supports two element types:
//   - float32 (default): 32-bit single-precision, up to 1998 dimensions
//   - float16: 16-bit half-precision, up to 3996 dimensions
//
// Example usage:
//
//	// Creating a vector with float32 values (default)
//	v := mssql.NewVector([]float32{1.0, 2.0, 3.0})
//
//	// Using in a query
//	_, err := db.Exec("INSERT INTO embeddings (embedding) VALUES (@p1)", v)
//
//	// Reading from a query
//	var result mssql.Vector
//	err := db.QueryRow("SELECT embedding FROM embeddings WHERE id = 1").Scan(&result)
type Vector struct {
	// ElementType specifies the precision of vector elements (float32 or float16).
	ElementType VectorElementType

	// Data contains the vector elements as float32 values.
	// For float16 vectors, values are converted to/from float32 during encoding/decoding.
	Data []float32
}

// NullVector represents a Vector that may be null.
// NullVector implements the Scanner interface so it can be used as a scan destination.
type NullVector struct {
	Vector Vector
	Valid  bool // Valid is true if Vector is not NULL
}

// IsNull returns true if the vector has no data.
func (v Vector) IsNull() bool {
	return v.Data == nil
}

// Scan implements the sql.Scanner interface for Vector.
func (v *Vector) Scan(src interface{}) error {
	if src == nil {
		v.Data = nil
		v.ElementType = VectorElementFloat32
		return nil
	}

	switch val := src.(type) {
	case []byte:
		return v.decodeFromBytes(val)
	case Vector:
		*v = val
		return nil
	case []float32:
		v.ElementType = VectorElementFloat32
		v.Data = make([]float32, len(val))
		copy(v.Data, val)
		return nil
	case []float64:
		// Convert float64 to float32 (may lose precision)
		v.ElementType = VectorElementFloat32
		v.Data = make([]float32, len(val))
		for i, f := range val {
			v.Data[i] = float32(f)
		}
		return nil
	case string:
		// Handle JSON array format: "[1.0, 2.0, 3.0]"
		return v.decodeFromJSON(val)
	default:
		return fmt.Errorf("mssql: cannot convert %T to Vector", src)
	}
}

// Value implements the driver.Valuer interface for Vector.
// Returns the vector as a JSON array string for SQL Server parameter binding.
func (v Vector) Value() (driver.Value, error) {
	if v.Data == nil {
		return nil, nil
	}
	return v.ToJSON(), nil
}

// Scan implements the sql.Scanner interface for NullVector.
func (nv *NullVector) Scan(src interface{}) error {
	if src == nil {
		nv.Valid = false
		nv.Vector = Vector{}
		return nil
	}

	nv.Valid = true
	return nv.Vector.Scan(src)
}

// Value implements the driver.Valuer interface for NullVector.
func (nv NullVector) Value() (driver.Value, error) {
	if !nv.Valid {
		return nil, nil
	}
	return nv.Vector.Value()
}

// String returns a string representation of the Vector.
// Format: "VECTOR(FLOAT32, 3) : [1.0, 2.0, 3.0]"
// The format uses uppercase type names for consistency with JDBC and SqlClient drivers.
func (v Vector) String() string {
	if v.Data == nil {
		return "NULL"
	}

	// Use strings.Builder for better performance with large vectors
	var sb strings.Builder
	sb.Grow(len(v.Data)*12 + 30) // Estimate: ~12 chars per float + prefix
	sb.WriteString("VECTOR(")
	sb.WriteString(v.ElementType.String())
	sb.WriteString(", ")
	sb.WriteString(strconv.Itoa(len(v.Data)))
	sb.WriteString(") : [")
	for i, val := range v.Data {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(strconv.FormatFloat(float64(val), 'f', -1, 32))
	}
	sb.WriteByte(']')
	return sb.String()
}

// ToJSON returns the Vector as a JSON array string suitable for SQL Server parameter binding.
// Format: "[1.0, 2.0, 3.0]"
// This format is used when sending vectors as parameters via RPC calls,
// following the backward compatibility approach used by SqlClient.
func (v Vector) ToJSON() string {
	if v.Data == nil {
		return "[]"
	}

	// Use strings.Builder for better performance with large vectors
	var sb strings.Builder
	sb.Grow(len(v.Data)*12 + 2) // Estimate: ~12 chars per float + brackets
	sb.WriteByte('[')
	for i, val := range v.Data {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(strconv.FormatFloat(float64(val), 'f', -1, 32))
	}
	sb.WriteByte(']')
	return sb.String()
}

// Dimensions returns the number of dimensions (elements) in the vector.
func (v Vector) Dimensions() int {
	return len(v.Data)
}

// Values returns a copy of the vector data as a float32 slice.
func (v Vector) Values() []float32 {
	if v.Data == nil {
		return nil
	}
	result := make([]float32, len(v.Data))
	copy(result, v.Data)
	return result
}

// encodeToBytes encodes the Vector to the TDS wire format.
// Wire format:
//   - Magic (1 byte): 0xA9
//   - Version (1 byte): 0x01
//   - Dimensions (2 bytes, little-endian): number of elements
//   - Type (1 byte): 0x00 for float32, 0x01 for float16
//   - Reserved (3 bytes): 0x00, 0x00, 0x00
//   - Data: float32 (4 bytes per element) or float16 (2 bytes per element), little-endian
func (v Vector) encodeToBytes() ([]byte, error) {
	if v.Data == nil {
		return nil, nil
	}

	dimensions := len(v.Data)
	maxDimensions := v.ElementType.MaxDimensions()
	if dimensions > maxDimensions {
		return nil, fmt.Errorf("mssql: vector dimensions %d exceeds maximum %d for %s",
			dimensions, maxDimensions, v.ElementType)
	}

	bytesPerElement := v.ElementType.BytesPerElement()
	totalSize := vectorHeaderSize + dimensions*bytesPerElement
	buf := make([]byte, totalSize)

	// Write header
	buf[0] = vectorMagic
	buf[1] = vectorVersion
	binary.LittleEndian.PutUint16(buf[2:4], uint16(dimensions))
	buf[4] = byte(v.ElementType)
	buf[5] = 0x00 // Reserved
	buf[6] = 0x00 // Reserved
	buf[7] = 0x00 // Reserved

	// Write data based on element type
	switch v.ElementType {
	case VectorElementFloat32:
		for i, val := range v.Data {
			offset := vectorHeaderSize + i*4
			binary.LittleEndian.PutUint32(buf[offset:offset+4], math.Float32bits(val))
		}
	case VectorElementFloat16:
		for i, val := range v.Data {
			offset := vectorHeaderSize + i*2
			binary.LittleEndian.PutUint16(buf[offset:offset+2], float32ToFloat16(val))
		}
	default:
		return nil, fmt.Errorf("mssql: unsupported vector element type: %s", v.ElementType)
	}

	return buf, nil
}

// decodeFromBytes decodes the Vector from the TDS wire format.
func (v *Vector) decodeFromBytes(buf []byte) error {
	if len(buf) == 0 {
		v.Data = nil
		v.ElementType = VectorElementFloat32
		return nil
	}

	if len(buf) < vectorHeaderSize {
		return errors.New("mssql: vector data too short for header")
	}

	// Validate header
	if buf[0] != vectorMagic {
		return fmt.Errorf("mssql: invalid vector magic byte: got 0x%02X, expected 0x%02X", buf[0], vectorMagic)
	}

	if buf[1] != vectorVersion {
		return fmt.Errorf("mssql: unsupported vector version: got 0x%02X, expected 0x%02X", buf[1], vectorVersion)
	}

	dimensions := int(binary.LittleEndian.Uint16(buf[2:4]))
	elementType := VectorElementType(buf[4])

	// Validate element type
	if elementType != VectorElementFloat32 && elementType != VectorElementFloat16 {
		return fmt.Errorf("mssql: unsupported vector element type: 0x%02X", elementType)
	}

	bytesPerElement := elementType.BytesPerElement()
	expectedDataSize := vectorHeaderSize + dimensions*bytesPerElement
	if len(buf) < expectedDataSize {
		return fmt.Errorf("mssql: vector data size mismatch: got %d bytes, expected %d bytes for %d %s dimensions",
			len(buf), expectedDataSize, dimensions, elementType)
	}

	// Decode values based on element type
	result := make([]float32, dimensions)
	switch elementType {
	case VectorElementFloat32:
		for i := 0; i < dimensions; i++ {
			offset := vectorHeaderSize + i*4
			result[i] = math.Float32frombits(binary.LittleEndian.Uint32(buf[offset : offset+4]))
		}
	case VectorElementFloat16:
		for i := 0; i < dimensions; i++ {
			offset := vectorHeaderSize + i*2
			result[i] = float16ToFloat32(binary.LittleEndian.Uint16(buf[offset : offset+2]))
		}
	}

	v.ElementType = elementType
	v.Data = result
	return nil
}

// decodeFromJSON decodes a Vector from a JSON array string.
// Format: "[1.0, 2.0, 3.0]"
func (v *Vector) decodeFromJSON(jsonStr string) error {
	// Trim whitespace
	jsonStr = strings.TrimSpace(jsonStr)

	// Check for empty array
	if jsonStr == "[]" {
		v.Data = nil
		v.ElementType = VectorElementFloat32
		return nil
	}

	// Parse JSON array
	var values []float64
	if err := json.Unmarshal([]byte(jsonStr), &values); err != nil {
		return fmt.Errorf("mssql: failed to parse vector JSON: %w", err)
	}

	// Convert to float32
	data := make([]float32, len(values))
	for i, val := range values {
		data[i] = float32(val)
	}

	v.ElementType = VectorElementFloat32
	v.Data = data
	return nil
}

// float32ToFloat16 converts a float32 value to float16 (IEEE 754 half-precision).
// This follows the same algorithm used by JDBC's VectorUtils.floatToFloat16().
func float32ToFloat16(value float32) uint16 {
	bits := math.Float32bits(value)

	sign := (bits >> 31) & 0x1
	exponent := int((bits >> 23) & 0xFF)
	mantissa := bits & 0x7FFFFF

	// NaN or Infinity
	if exponent == 0xFF {
		if mantissa != 0 {
			return uint16((sign << 15) | 0x7E00) // NaN
		}
		return uint16((sign << 15) | 0x7C00) // Infinity
	}

	// Zero (preserve signed zero)
	if (bits & 0x7FFFFFFF) == 0 {
		return uint16(sign << 15)
	}

	// Convert exponent (bias 127 -> bias 15)
	halfExponent := exponent - 127 + 15

	// Overflow → Infinity
	if halfExponent >= 31 {
		return uint16((sign << 15) | 0x7C00)
	}

	// Underflow → Subnormal or Zero
	if halfExponent <= 0 {
		if halfExponent < -10 {
			return uint16(sign << 15) // Too small → zero
		}

		// Convert to subnormal
		mantissa |= 0x800000
		shift := uint(1 - halfExponent)

		mant := mantissa >> (shift + 13)

		// Round to nearest-even
		roundBit := (mantissa >> (shift + 12)) & 1
		lostBits := mantissa & ((1 << (shift + 12)) - 1)

		if roundBit == 1 && (lostBits != 0 || (mant&1) == 1) {
			mant++
		}

		return uint16((sign << 15) | mant)
	}

	// Normal number
	mant := mantissa >> 13

	// Rounding
	roundBit := (mantissa >> 12) & 1
	lostBits := mantissa & 0xFFF

	if roundBit == 1 && (lostBits != 0 || (mant&1) == 1) {
		mant++
		if mant == 0x400 { // Mantissa overflow
			mant = 0
			halfExponent++
			if halfExponent >= 31 {
				return uint16((sign << 15) | 0x7C00)
			}
		}
	}

	return uint16((sign << 15) | (uint32(halfExponent) << 10) | mant)
}

// float16ToFloat32 converts a float16 (IEEE 754 half-precision) value to float32.
// This follows the same algorithm used by JDBC's VectorUtils.float16ToFloat().
func float16ToFloat32(value uint16) float32 {
	sign := (value >> 15) & 0x1
	exponent := int((value >> 10) & 0x1F)
	mantissa := uint32(value & 0x3FF)

	// Infinity or NaN
	if exponent == 31 {
		if mantissa != 0 {
			return float32(math.NaN())
		}
		if sign == 1 {
			return float32(math.Inf(-1))
		}
		return float32(math.Inf(1))
	}

	// Zero or subnormal
	if exponent == 0 {
		if mantissa == 0 {
			// Preserve signed zero
			if sign == 1 {
				return float32(math.Copysign(0, -1))
			}
			return 0
		}
		// Subnormal: normalize
		for (mantissa & 0x400) == 0 {
			mantissa <<= 1
			exponent--
		}
		mantissa &= 0x3FF
		exponent++
	}

	// Convert exponent bias (15 -> 127)
	exponent = exponent + (127 - 15)

	result := (uint32(sign) << 31) | (uint32(exponent) << 23) | (mantissa << 13)
	return math.Float32frombits(result)
}

// NewVector creates a new Vector with float32 element type from a slice of float32 values.
// Returns an error if the number of dimensions exceeds the maximum allowed.
func NewVector(values []float32) (Vector, error) {
	if len(values) > vectorMaxDimensionsFloat32 {
		return Vector{}, fmt.Errorf("mssql: vector dimensions %d exceeds maximum %d for float32",
			len(values), vectorMaxDimensionsFloat32)
	}
	data := make([]float32, len(values))
	copy(data, values)
	return Vector{
		ElementType: VectorElementFloat32,
		Data:        data,
	}, nil
}

// NewVectorWithType creates a new Vector with the specified element type from a slice of float32 values.
// Returns an error if the number of dimensions exceeds the maximum allowed for the element type.
func NewVectorWithType(elementType VectorElementType, values []float32) (Vector, error) {
	maxDimensions := elementType.MaxDimensions()
	if len(values) > maxDimensions {
		return Vector{}, fmt.Errorf("mssql: vector dimensions %d exceeds maximum %d for %s",
			len(values), maxDimensions, elementType)
	}
	data := make([]float32, len(values))
	copy(data, values)
	return Vector{
		ElementType: elementType,
		Data:        data,
	}, nil
}

// NewVectorFromFloat64 creates a new Vector with float32 element type from a slice of float64 values.
// The values are converted to float32, which may result in precision loss.
// Returns an error if the number of dimensions exceeds the maximum allowed.
func NewVectorFromFloat64(values []float64) (Vector, error) {
	if len(values) > vectorMaxDimensionsFloat32 {
		return Vector{}, fmt.Errorf("mssql: vector dimensions %d exceeds maximum %d for float32",
			len(values), vectorMaxDimensionsFloat32)
	}
	data := make([]float32, len(values))
	for i, val := range values {
		data[i] = float32(val)
	}
	return Vector{
		ElementType: VectorElementFloat32,
		Data:        data,
	}, nil
}

// ToFloat64 returns the vector values as a slice of float64.
// This is useful for interfacing with libraries that use float64.
func (v Vector) ToFloat64() []float64 {
	if v.Data == nil {
		return nil
	}
	result := make([]float64, len(v.Data))
	for i, val := range v.Data {
		result[i] = float64(val)
	}
	return result
}
