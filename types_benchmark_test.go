package mssql

import (
	"encoding/binary"
	"math"
	"testing"
	"time"

	"github.com/microsoft/go-mssqldb/internal/cp"
	"github.com/microsoft/go-mssqldb/msdsn"
)

// Benchmarks for type reading functions — the hot path for decoding every column value from the wire.

var sideeffect interface{}

func benchmarkEncoding() msdsn.EncodeParameters {
	return msdsn.EncodeParameters{Timezone: time.UTC}
}

func BenchmarkReadFixedType_Int64(b *testing.B) {
	ti := typeInfo{TypeId: typeInt8, Size: 8, Buffer: make([]byte, 8)}
	binary.LittleEndian.PutUint64(ti.Buffer, 1234567890)
	ti.Reader = readFixedType

	buf := newTdsBuffer(512, nil)
	enc := benchmarkEncoding()

	for i := 0; i < b.N; i++ {
		// Simulate reading from buffer by resetting the pre-filled buffer
		copy(buf.rbuf[:8], ti.Buffer)
		buf.rpos = 0
		buf.rsize = 8
		sideeffect = ti.Reader(&ti, buf, nil, enc)
	}
}

func BenchmarkReadFixedType_Int32(b *testing.B) {
	ti := typeInfo{TypeId: typeInt4, Size: 4, Buffer: make([]byte, 4)}
	binary.LittleEndian.PutUint32(ti.Buffer, 42)
	ti.Reader = readFixedType

	buf := newTdsBuffer(512, nil)
	enc := benchmarkEncoding()

	for i := 0; i < b.N; i++ {
		copy(buf.rbuf[:4], ti.Buffer)
		buf.rpos = 0
		buf.rsize = 4
		sideeffect = ti.Reader(&ti, buf, nil, enc)
	}
}

func BenchmarkReadFixedType_Float64(b *testing.B) {
	ti := typeInfo{TypeId: typeFlt8, Size: 8, Buffer: make([]byte, 8)}
	binary.LittleEndian.PutUint64(ti.Buffer, math.Float64bits(3.14159))
	ti.Reader = readFixedType

	buf := newTdsBuffer(512, nil)
	enc := benchmarkEncoding()

	for i := 0; i < b.N; i++ {
		copy(buf.rbuf[:8], ti.Buffer)
		buf.rpos = 0
		buf.rsize = 8
		sideeffect = ti.Reader(&ti, buf, nil, enc)
	}
}

func BenchmarkReadFixedType_DateTime(b *testing.B) {
	ti := typeInfo{TypeId: typeDateTime, Size: 8, Buffer: make([]byte, 8)}
	// Encode a valid datetime
	binary.LittleEndian.PutUint32(ti.Buffer[0:4], 43000) // days since 1900
	binary.LittleEndian.PutUint32(ti.Buffer[4:8], 9000000)
	ti.Reader = readFixedType

	buf := newTdsBuffer(512, nil)
	enc := benchmarkEncoding()

	for i := 0; i < b.N; i++ {
		copy(buf.rbuf[:8], ti.Buffer)
		buf.rpos = 0
		buf.rsize = 8
		sideeffect = ti.Reader(&ti, buf, nil, enc)
	}
}

func BenchmarkReadByteLenType_IntN_8(b *testing.B) {
	// Simulate reading an 8-byte INTNTYPE (int64)
	data := make([]byte, 9) // 1 byte length + 8 bytes data
	data[0] = 8
	binary.LittleEndian.PutUint64(data[1:], 9876543210)

	ti := typeInfo{TypeId: typeIntN, Size: 8, Buffer: make([]byte, 8)}
	ti.Reader = readByteLenTypeWithEncoding

	buf := newTdsBuffer(512, nil)
	enc := benchmarkEncoding()

	for i := 0; i < b.N; i++ {
		copy(buf.rbuf[:9], data)
		buf.rpos = 0
		buf.rsize = 9
		sideeffect = ti.Reader(&ti, buf, nil, enc)
	}
}

func BenchmarkReadByteLenType_IntN_4(b *testing.B) {
	data := make([]byte, 5) // 1 byte length + 4 bytes data
	data[0] = 4
	binary.LittleEndian.PutUint32(data[1:], 12345)

	ti := typeInfo{TypeId: typeIntN, Size: 4, Buffer: make([]byte, 4)}
	ti.Reader = readByteLenTypeWithEncoding

	buf := newTdsBuffer(512, nil)
	enc := benchmarkEncoding()

	for i := 0; i < b.N; i++ {
		copy(buf.rbuf[:5], data)
		buf.rpos = 0
		buf.rsize = 5
		sideeffect = ti.Reader(&ti, buf, nil, enc)
	}
}

func BenchmarkReadByteLenType_FloatN_8(b *testing.B) {
	data := make([]byte, 9)
	data[0] = 8
	binary.LittleEndian.PutUint64(data[1:], math.Float64bits(2.71828))

	ti := typeInfo{TypeId: typeFltN, Size: 8, Buffer: make([]byte, 8)}
	ti.Reader = readByteLenTypeWithEncoding

	buf := newTdsBuffer(512, nil)
	enc := benchmarkEncoding()

	for i := 0; i < b.N; i++ {
		copy(buf.rbuf[:9], data)
		buf.rpos = 0
		buf.rsize = 9
		sideeffect = ti.Reader(&ti, buf, nil, enc)
	}
}

func BenchmarkReadByteLenType_BitN(b *testing.B) {
	data := []byte{1, 1} // length=1, value=true

	ti := typeInfo{TypeId: typeBitN, Size: 1, Buffer: make([]byte, 1)}
	ti.Reader = readByteLenTypeWithEncoding

	buf := newTdsBuffer(512, nil)
	enc := benchmarkEncoding()

	for i := 0; i < b.N; i++ {
		copy(buf.rbuf[:2], data)
		buf.rpos = 0
		buf.rsize = 2
		sideeffect = ti.Reader(&ti, buf, nil, enc)
	}
}

func BenchmarkReadByteLenType_Null(b *testing.B) {
	data := []byte{0} // length=0 means NULL

	ti := typeInfo{TypeId: typeIntN, Size: 8, Buffer: make([]byte, 8)}
	ti.Reader = readByteLenTypeWithEncoding

	buf := newTdsBuffer(512, nil)
	enc := benchmarkEncoding()

	for i := 0; i < b.N; i++ {
		copy(buf.rbuf[:1], data)
		buf.rpos = 0
		buf.rsize = 1
		sideeffect = ti.Reader(&ti, buf, nil, enc)
	}
}

func BenchmarkReadShortLenType_NVarChar_Short(b *testing.B) {
	// "hello" in UTF-16LE = 10 bytes
	str := []byte{0x68, 0x00, 0x65, 0x00, 0x6c, 0x00, 0x6c, 0x00, 0x6f, 0x00}
	data := make([]byte, 2+len(str))
	binary.LittleEndian.PutUint16(data, uint16(len(str)))
	copy(data[2:], str)

	ti := typeInfo{TypeId: typeNVarChar, Size: 100, Buffer: make([]byte, 100)}
	ti.Reader = readShortLenType

	buf := newTdsBuffer(512, nil)
	enc := benchmarkEncoding()

	for i := 0; i < b.N; i++ {
		copy(buf.rbuf[:len(data)], data)
		buf.rpos = 0
		buf.rsize = len(data)
		sideeffect = ti.Reader(&ti, buf, nil, enc)
	}
}

func BenchmarkReadShortLenType_NVarChar_Medium(b *testing.B) {
	// 50-char ASCII string in UTF-16LE = 100 bytes
	str := make([]byte, 100)
	for i := 0; i < 50; i++ {
		str[i*2] = byte('A' + (i % 26))
		str[i*2+1] = 0
	}
	data := make([]byte, 2+len(str))
	binary.LittleEndian.PutUint16(data, uint16(len(str)))
	copy(data[2:], str)

	ti := typeInfo{TypeId: typeNVarChar, Size: 200, Buffer: make([]byte, 200)}
	ti.Reader = readShortLenType

	buf := newTdsBuffer(512, nil)
	enc := benchmarkEncoding()

	for i := 0; i < b.N; i++ {
		copy(buf.rbuf[:len(data)], data)
		buf.rpos = 0
		buf.rsize = len(data)
		sideeffect = ti.Reader(&ti, buf, nil, enc)
	}
}

func BenchmarkReadShortLenType_VarBinary(b *testing.B) {
	// 64 bytes of binary data
	str := make([]byte, 64)
	for i := range str {
		str[i] = byte(i)
	}
	data := make([]byte, 2+len(str))
	binary.LittleEndian.PutUint16(data, uint16(len(str)))
	copy(data[2:], str)

	ti := typeInfo{TypeId: typeBigVarBin, Size: 100, Buffer: make([]byte, 100)}
	ti.Reader = readShortLenType

	buf := newTdsBuffer(512, nil)
	enc := benchmarkEncoding()

	for i := 0; i < b.N; i++ {
		copy(buf.rbuf[:len(data)], data)
		buf.rpos = 0
		buf.rsize = len(data)
		sideeffect = ti.Reader(&ti, buf, nil, enc)
	}
}

func BenchmarkReadByteLenType_VarChar(b *testing.B) {
	// "hello world" as ASCII with default collation
	str := []byte("hello world")
	data := make([]byte, 1+len(str))
	data[0] = byte(len(str))
	copy(data[1:], str)

	ti := typeInfo{TypeId: typeVarChar, Size: 50, Buffer: make([]byte, 50), Collation: cp.Collation{LcidAndFlags: 0x0409}}
	ti.Reader = readByteLenTypeWithEncoding

	buf := newTdsBuffer(512, nil)
	enc := benchmarkEncoding()

	for i := 0; i < b.N; i++ {
		copy(buf.rbuf[:len(data)], data)
		buf.rpos = 0
		buf.rsize = len(data)
		sideeffect = ti.Reader(&ti, buf, nil, enc)
	}
}

func BenchmarkDecodeDateTime(b *testing.B) {
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint32(buf[0:4], 43000)
	binary.LittleEndian.PutUint32(buf[4:8], 9000000)
	loc := time.UTC

	for i := 0; i < b.N; i++ {
		sideeffect = decodeDateTime(buf, loc)
	}
}

func BenchmarkDecodeDateTim4(b *testing.B) {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint16(buf[0:2], 20000)
	binary.LittleEndian.PutUint16(buf[2:4], 720)
	loc := time.UTC

	for i := 0; i < b.N; i++ {
		sideeffect = decodeDateTim4(buf, loc)
	}
}

func BenchmarkEncodeDateTime(b *testing.B) {
	t := time.Date(2024, 6, 15, 14, 30, 45, 123456789, time.UTC)
	for i := 0; i < b.N; i++ {
		sideeffect = encodeDateTime(t)
	}
}
