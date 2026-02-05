package mssql

import (
	"context"
	"database/sql/driver"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/microsoft/go-mssqldb/msdsn"
)

// vectorPrecisionLossHandler is a callback function that is invoked when float64 to float32
// conversion results in precision loss. The callback receives the index of the value,
// the original float64 value, and the converted float32 value.
// Use SetVectorPrecisionLossHandler to set this in a thread-safe way.
// By default, no handler is set (warnings are silent).
var vectorPrecisionLossHandler func(index int, original float64, converted float32)
var vectorPrecisionMu sync.Mutex

// SetVectorPrecisionLossHandler sets the global precision loss handler in a thread-safe way.
// Passing nil disables precision loss callbacks.
func SetVectorPrecisionLossHandler(handler func(index int, original float64, converted float32)) {
	vectorPrecisionMu.Lock()
	defer vectorPrecisionMu.Unlock()
	vectorPrecisionLossHandler = handler
}

// SetVectorPrecisionWarnings enables or disables precision loss warnings when converting
// float64 values to float32 for vector operations. When enabled, a warning is logged via
// the driver's logging infrastructure (SetLogger/SetContextLogger) when precision loss is
// detected (at most one warning per vector operation). If no driver logger is configured,
// the warning is silently discarded.
func SetVectorPrecisionWarnings(enabled bool) {
	if enabled {
		SetVectorPrecisionLossHandler(defaultPrecisionLossHandler)
	} else {
		SetVectorPrecisionLossHandler(nil)
	}
}

// defaultPrecisionLossHandler is the default handler that logs precision loss warnings.
// It routes warnings through the driver's logging infrastructure (SetLogger/SetContextLogger)
// so that applications can capture them alongside other driver messages. If no driver logger
// is configured, the message is silently discarded. Applications that want custom handling
// should use SetVectorPrecisionLossHandler to set their own function.
func defaultPrecisionLossHandler(index int, original float64, converted float32) {
	msg := fmt.Sprintf("vector precision loss at index %d: float64(%v) -> float32(%v)", index, original, converted)
	driverInstance.logger.Log(context.Background(), msdsn.LogMessages, msg)
}

// checkFloat64PrecisionLoss checks if converting float64 to float32 loses precision
// and invokes the precision loss handler on the first value that loses precision.
// Only reports the first loss for performance with large vectors.
func checkFloat64PrecisionLoss(values []float64, converted []float32) {
	vectorPrecisionMu.Lock()
	handler := vectorPrecisionLossHandler
	vectorPrecisionMu.Unlock()
	if handler == nil {
		return
	}
	for i, orig := range values {
		// Check if round-trip conversion loses precision
		if float64(converted[i]) != orig && !math.IsNaN(orig) {
			handler(i, orig, converted[i])
			return // Only report first precision loss
		}
	}
}

// VectorElementType represents the element type of a SQL Server Vector.
// SQL Server 2025 supports float32 (default) and float16.
type VectorElementType byte

const (
	// VectorElementFloat32 represents 32-bit single-precision floating point (4 bytes per element).
	// This is the default element type for SQL Server vectors.
	VectorElementFloat32 VectorElementType = 0x00

	// VectorElementFloat16 represents 16-bit half-precision floating point (2 bytes per element).
	// Note: float16 is a preview feature in SQL Server requiring PREVIEW_FEATURES = ON.
	// Currently, float16 vector *parameters* are transmitted as JSON payloads over TDS rather than as
	// binary vector values. This does not affect how server-to-client results are encoded on the wire.
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

// IsValid returns true if the element type is a supported vector element type.
func (t VectorElementType) IsValid() bool {
	return t == VectorElementFloat32 || t == VectorElementFloat16
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
// The Vector struct exposes its fields for direct access; however, callers should avoid
// modifying Data after the Vector has been passed to a database operation, as concurrent
// mutation may lead to unpredictable results.
//
// Example usage:
//
//	// Creating a vector with float32 values (default)
//	v, err := mssql.NewVector([]float32{1.0, 2.0, 3.0})
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Using in a query
//	_, err = db.Exec("INSERT INTO embeddings (embedding) VALUES (@p1)", v)
//
//	// Reading from a query
//	var result mssql.Vector
//	err = db.QueryRow("SELECT embedding FROM embeddings WHERE id = 1").Scan(&result)
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

// IsNull reports whether the vector represents a SQL NULL value.
// It returns true when Data is nil; an empty but non-nil slice is not considered NULL.
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
		v.Data = copyFloat32Slice(val)
		return nil
	case []float64:
		// Convert float64 to float32 (may lose precision)
		v.ElementType = VectorElementFloat32
		v.Data = make([]float32, len(val))
		for i, f := range val {
			v.Data[i] = float32(f)
		}
		checkFloat64PrecisionLoss(val, v.Data)
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
// Returns an error if the vector contains Infinity values (which cannot be represented in JSON).
func (v Vector) Value() (driver.Value, error) {
	if v.Data == nil {
		return nil, nil
	}
	// Check for Inf values which cannot be round-tripped through JSON
	for _, val := range v.Data {
		if math.IsInf(float64(val), 0) {
			return nil, errors.New("mssql: vector contains Infinity values which cannot be encoded as JSON parameter")
		}
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

	if err := nv.Vector.Scan(src); err != nil {
		// Ensure the NullVector remains invalid on scan failure.
		nv.Valid = false
		return err
	}

	nv.Valid = true
	return nil
}

// Value implements the driver.Valuer interface for NullVector.
func (nv NullVector) Value() (driver.Value, error) {
	if !nv.Valid {
		return nil, nil
	}
	return nv.Vector.Value()
}

// String returns a string representation of the Vector.
// Format: "VECTOR(FLOAT32, 3) : [1, 2, 3]"
// The format uses uppercase type names for consistency with JDBC and SqlClient drivers.
func (v Vector) String() string {
	if v.Data == nil {
		return "NULL"
	}

	// Use strings.Builder for better performance with large vectors.
	// Capacity estimate: ~12 chars per float for typical values.
	var sb strings.Builder
	sb.Grow(len(v.Data)*12 + 30) // ~12 chars/float + prefix overhead
	sb.WriteString("VECTOR(")
	sb.WriteString(v.ElementType.String())
	sb.WriteString(", ")
	sb.WriteString(strconv.Itoa(len(v.Data)))
	sb.WriteString(") : [")
	for i, val := range v.Data {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(strconv.FormatFloat(float64(val), 'g', -1, 32))
	}
	sb.WriteByte(']')
	return sb.String()
}

// ToJSON returns the Vector as a JSON array string suitable for SQL Server parameter binding.
// Format: "[1.0, 2.0, 3.0]"
// Returns an empty string for nil/NULL vectors or if the vector contains Infinity values.
// This format is used when sending vectors as parameters via RPC calls,
// following the backward compatibility approach used by SqlClient.
// Note: NaN values are encoded as JSON null; Infinity values return empty string
// since they cannot be losslessly round-tripped through JSON.
func (v Vector) ToJSON() string {
	if v.Data == nil {
		return ""
	}

	// Use strings.Builder for better performance with large vectors
	var sb strings.Builder
	sb.Grow(len(v.Data)*12 + 2) // Estimate: ~12 chars per float + brackets
	sb.WriteByte('[')
	for i, val := range v.Data {
		if i > 0 {
			sb.WriteString(", ")
		}
		f := float64(val)
		if math.IsInf(f, 0) {
			// Infinity cannot be represented in JSON and would round-trip as NaN.
			// Return empty string to avoid silently encoding Infinity; callers should
			// treat this as a serialization failure when v.Data is non-nil.
			return ""
		}
		if math.IsNaN(f) {
			// JSON does not support NaN as a numeric literal.
			// Encode as null; decodeFromJSON will convert back to NaN.
			sb.WriteString("null")
		} else {
			sb.WriteString(strconv.FormatFloat(f, 'g', -1, 32))
		}
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
	return copyFloat32Slice(v.Data)
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
	// Treat nil buffer as SQL NULL vector.
	if buf == nil {
		v.Data = nil
		v.ElementType = VectorElementFloat32
		return nil
	}

	// A non-nil but empty buffer is considered invalid/truncated data.
	if len(buf) == 0 {
		return errors.New("mssql: vector data too short")
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
	if !elementType.IsValid() {
		return fmt.Errorf("mssql: unsupported vector element type: 0x%02X", elementType)
	}

	bytesPerElement := elementType.BytesPerElement()
	expectedDataSize := vectorHeaderSize + dimensions*bytesPerElement
	if len(buf) != expectedDataSize {
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
// Note: JSON null values are decoded as NaN since JSON does not support
// special floating-point values (NaN, Inf) as numeric literals.
// This allows round-tripping of NaN values through ToJSON()/decodeFromJSON().
func (v *Vector) decodeFromJSON(jsonStr string) error {
	// Trim whitespace
	jsonStr = strings.TrimSpace(jsonStr)

	// Check for empty array - return empty slice (not nil) to distinguish from NULL
	if jsonStr == "[]" {
		v.Data = make([]float32, 0)
		v.ElementType = VectorElementFloat32
		return nil
	}

	// Check for JSON null - represents SQL NULL (Data == nil)
	if jsonStr == "null" {
		v.Data = nil
		v.ElementType = VectorElementFloat32
		return nil
	}

	// Parse JSON array using []*float64 to handle null values
	// (null is used to represent NaN/Inf since JSON doesn't support them)
	var values []*float64
	if err := json.Unmarshal([]byte(jsonStr), &values); err != nil {
		return fmt.Errorf("mssql: failed to parse vector JSON: %w", err)
	}

	// Convert to float32, mapping null to NaN
	data := make([]float32, len(values))
	for i, val := range values {
		if val == nil {
			// null represents NaN (JSON doesn't support NaN/Inf literals)
			data[i] = float32(math.NaN())
		} else {
			data[i] = float32(*val)
		}
	}

	v.ElementType = VectorElementFloat32
	v.Data = data
	return nil
}

// float32ToFloat16 converts a float32 value to float16 (IEEE 754 half-precision).
// This follows the same algorithm used by JDBC's VectorUtils.floatToFloat16().
//
// IEEE 754 half-precision format (16 bits):
//   - Sign: 1 bit (bit 15)
//   - Exponent: 5 bits (bits 10-14), bias = 15
//   - Mantissa: 10 bits (bits 0-9)
//
// Constants used:
//   - float16ExpMax (31): Maximum exponent value (all 1s = infinity/NaN)
//   - float16InfBits (0x7C00): Positive infinity bit pattern
//   - float16NaNBits (0x7E00): Quiet NaN bit pattern (canonical form)
//   - float16MantissaMask (0x03FF): Mask for 10-bit mantissa
const (
	float16ExpMax       = 31     // Maximum exponent value (5 bits all 1s)
	float16InfBits      = 0x7C00 // Positive infinity: sign=0, exp=11111, mant=0
	float16NaNBits      = 0x7E00 // Quiet NaN: sign=0, exp=11111, mant=1000000000
	float16MantissaMask = 0x03FF // 10-bit mantissa mask
)

func float32ToFloat16(value float32) uint16 {
	bits := math.Float32bits(value)

	sign := (bits >> 31) & 0x1
	exponent := int((bits >> 23) & 0xFF)
	mantissa := bits & 0x7FFFFF

	// NaN or Infinity
	if exponent == 0xFF {
		if mantissa != 0 {
			return uint16((sign << 15) | float16NaNBits) // NaN
		}
		return uint16((sign << 15) | float16InfBits) // Infinity
	}

	// Zero (preserve signed zero)
	if (bits & 0x7FFFFFFF) == 0 {
		return uint16(sign << 15)
	}

	// Convert exponent (bias 127 -> bias 15)
	halfExponent := exponent - 127 + 15

	// Overflow → Infinity
	if halfExponent >= float16ExpMax {
		return uint16((sign << 15) | float16InfBits)
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

	// Rounding: check bit 12 (first discarded bit) and lower 12 bits (0xFFF mask).
	// The 0xFFF mask captures the 12 bits that are lost when truncating from
	// float32's 23-bit mantissa to float16's 10-bit mantissa.
	roundBit := (mantissa >> 12) & 1
	lostBits := mantissa & 0xFFF

	if roundBit == 1 && (lostBits != 0 || (mant&1) == 1) {
		mant++
		if mant == 0x400 { // Mantissa overflow
			mant = 0
			halfExponent++
			if halfExponent >= float16ExpMax {
				return uint16((sign << 15) | float16InfBits)
			}
		}
	}

	return uint16((uint32(sign) << 15) | (uint32(halfExponent) << 10) | uint32(mant))
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

// copyFloat32Slice returns a copy of the input slice.
func copyFloat32Slice(src []float32) []float32 {
	dst := make([]float32, len(src))
	copy(dst, src)
	return dst
}

// newVectorFromFloat32 creates a Vector from float32 slice with dimension validation.
func newVectorFromFloat32(elementType VectorElementType, values []float32) (Vector, error) {
	max := elementType.MaxDimensions()
	if len(values) > max {
		return Vector{}, fmt.Errorf("mssql: vector dimensions %d exceeds maximum %d for %s", len(values), max, elementType)
	}
	return Vector{ElementType: elementType, Data: copyFloat32Slice(values)}, nil
}

// NewVector creates a new Vector with float32 element type from a slice of float32 values.
// Returns an error if the number of dimensions exceeds the maximum allowed.
func NewVector(values []float32) (Vector, error) {
	return newVectorFromFloat32(VectorElementFloat32, values)
}

// NewVectorWithType creates a new Vector with the specified element type from a slice of float32 values.
// Returns an error if the element type is unsupported or the number of dimensions exceeds the maximum allowed.
func NewVectorWithType(elementType VectorElementType, values []float32) (Vector, error) {
	if !elementType.IsValid() {
		return Vector{}, fmt.Errorf("mssql: unsupported vector element type %d", elementType)
	}
	return newVectorFromFloat32(elementType, values)
}

// NewVectorFromFloat64 creates a new Vector with float32 element type from a slice of float64 values.
// The values are converted to float32, which may result in precision loss.
// If SetVectorPrecisionLossHandler has been called or SetVectorPrecisionWarnings(true) was called,
// a warning will be generated for the first value that loses precision.
// Returns an error if the number of dimensions exceeds the maximum allowed.
func NewVectorFromFloat64(values []float64) (Vector, error) {
	max := VectorElementFloat32.MaxDimensions()
	if len(values) > max {
		return Vector{}, fmt.Errorf("mssql: vector dimensions %d exceeds maximum %d for %s", len(values), max, VectorElementFloat32)
	}
	data := make([]float32, len(values))
	for i, val := range values {
		data[i] = float32(val)
	}
	checkFloat64PrecisionLoss(values, data)
	return Vector{ElementType: VectorElementFloat32, Data: data}, nil
}

// ToFloat64 returns the vector values as a slice of float64.
// This is a widening conversion from the stored float32 values.
// Useful for interfacing with Go libraries that use float64 (e.g., gonum).
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
