package mssql

import (
	"encoding/binary"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/microsoft/go-mssqldb/msdsn"
	"github.com/stretchr/testify/assert"
)

func TestMakeGoLangScanType(t *testing.T) {
	tests := []struct {
		name     string
		typeInfo typeInfo
		expected reflect.Type
	}{
		{"typeInt1", typeInfo{TypeId: typeInt1}, reflect.TypeOf(int64(0))},
		{"typeInt2", typeInfo{TypeId: typeInt2}, reflect.TypeOf(int64(0))},
		{"typeInt4", typeInfo{TypeId: typeInt4}, reflect.TypeOf(int64(0))},
		{"typeInt8", typeInfo{TypeId: typeInt8}, reflect.TypeOf(int64(0))},
		{"typeFlt4", typeInfo{TypeId: typeFlt4}, reflect.TypeOf(float64(0))},
		{"typeFlt8", typeInfo{TypeId: typeFlt8}, reflect.TypeOf(float64(0))},
		{"typeVarChar", typeInfo{TypeId: typeVarChar}, reflect.TypeOf("")},
		{"typeNVarChar", typeInfo{TypeId: typeNVarChar}, reflect.TypeOf("")},
		{"typeDateTime", typeInfo{TypeId: typeDateTime}, reflect.TypeOf(time.Time{})},
		{"typeDateTim4", typeInfo{TypeId: typeDateTim4}, reflect.TypeOf(time.Time{})},
		{"typeIntN size 1", typeInfo{TypeId: typeIntN, Size: 1}, reflect.TypeOf(int64(0))},
		{"typeIntN size 2", typeInfo{TypeId: typeIntN, Size: 2}, reflect.TypeOf(int64(0))},
		{"typeIntN size 4", typeInfo{TypeId: typeIntN, Size: 4}, reflect.TypeOf(int64(0))},
		{"typeIntN size 8", typeInfo{TypeId: typeIntN, Size: 8}, reflect.TypeOf(int64(0))},
		{"typeFltN size 4", typeInfo{TypeId: typeFltN, Size: 4}, reflect.TypeOf(float64(0))},
		{"typeFltN size 8", typeInfo{TypeId: typeFltN, Size: 8}, reflect.TypeOf(float64(0))},
		{"typeBigVarBin", typeInfo{TypeId: typeBigVarBin}, reflect.TypeOf([]byte{})},
		{"typeBit", typeInfo{TypeId: typeBit}, reflect.TypeOf(true)},
		{"typeBitN", typeInfo{TypeId: typeBitN}, reflect.TypeOf(true)},
		{"typeDecimalN", typeInfo{TypeId: typeDecimalN}, reflect.TypeOf([]byte{})},
		{"typeNumericN", typeInfo{TypeId: typeNumericN}, reflect.TypeOf([]byte{})},
		{"typeMoney", typeInfo{TypeId: typeMoney, Size: 8}, reflect.TypeOf([]byte{})},
		{"typeMoney4", typeInfo{TypeId: typeMoney4, Size: 4}, reflect.TypeOf([]byte{})},
		{"typeMoneyN size 4", typeInfo{TypeId: typeMoneyN, Size: 4}, reflect.TypeOf([]byte{})},
		{"typeMoneyN size 8", typeInfo{TypeId: typeMoneyN, Size: 8}, reflect.TypeOf([]byte{})},
		{"typeDateTimeN size 4", typeInfo{TypeId: typeDateTimeN, Size: 4}, reflect.TypeOf(time.Time{})},
		{"typeDateTimeN size 8", typeInfo{TypeId: typeDateTimeN, Size: 8}, reflect.TypeOf(time.Time{})},
		{"typeDateTime2N", typeInfo{TypeId: typeDateTime2N}, reflect.TypeOf(time.Time{})},
		{"typeDateN", typeInfo{TypeId: typeDateN}, reflect.TypeOf(time.Time{})},
		{"typeTimeN", typeInfo{TypeId: typeTimeN}, reflect.TypeOf(time.Time{})},
		{"typeDateTimeOffsetN", typeInfo{TypeId: typeDateTimeOffsetN}, reflect.TypeOf(time.Time{})},
		{"typeBigVarChar", typeInfo{TypeId: typeBigVarChar}, reflect.TypeOf("")},
		{"typeBigChar", typeInfo{TypeId: typeBigChar}, reflect.TypeOf("")},
		{"typeNChar", typeInfo{TypeId: typeNChar}, reflect.TypeOf("")},
		{"typeGuid", typeInfo{TypeId: typeGuid}, reflect.TypeOf([]byte{})},
		{"typeXml", typeInfo{TypeId: typeXml}, reflect.TypeOf("")},
		{"typeText", typeInfo{TypeId: typeText}, reflect.TypeOf("")},
		{"typeNText", typeInfo{TypeId: typeNText}, reflect.TypeOf("")},
		{"typeImage", typeInfo{TypeId: typeImage}, reflect.TypeOf([]byte{})},
		{"typeBigBinary", typeInfo{TypeId: typeBigBinary}, reflect.TypeOf([]byte{})},
		{"typeVariant", typeInfo{TypeId: typeVariant}, reflect.TypeOf((*interface{})(nil)).Elem()},
		{"typeUdt", typeInfo{TypeId: typeUdt}, reflect.TypeOf([]byte{})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := makeGoLangScanType(tt.typeInfo)
			assert.Equal(t, tt.expected, got, "makeGoLangScanType()")
		})
	}
}

func TestMakeGoLangTypeName(t *testing.T) {
	tests := []struct {
		name       string
		typeInfo   typeInfo
		typeString string
	}{
		{"typeInt1", typeInfo{TypeId: typeInt1}, "TINYINT"},
		{"typeInt2", typeInfo{TypeId: typeInt2}, "SMALLINT"},
		{"typeInt4", typeInfo{TypeId: typeInt4}, "INT"},
		{"typeInt8", typeInfo{TypeId: typeInt8}, "BIGINT"},
		{"typeFlt4", typeInfo{TypeId: typeFlt4}, "REAL"},
		{"typeFlt8", typeInfo{TypeId: typeFlt8}, "FLOAT"},
		{"typeDateTime", typeInfo{TypeId: typeDateTime}, "DATETIME"},
		{"typeDateTim4", typeInfo{TypeId: typeDateTim4}, "SMALLDATETIME"},
		{"typeBigBinary", typeInfo{TypeId: typeBigBinary}, "BINARY"},
		{"typeIntN size 1", typeInfo{TypeId: typeIntN, Size: 1}, "TINYINT"},
		{"typeIntN size 2", typeInfo{TypeId: typeIntN, Size: 2}, "SMALLINT"},
		{"typeIntN size 4", typeInfo{TypeId: typeIntN, Size: 4}, "INT"},
		{"typeIntN size 8", typeInfo{TypeId: typeIntN, Size: 8}, "BIGINT"},
		{"typeFltN size 4", typeInfo{TypeId: typeFltN, Size: 4}, "REAL"},
		{"typeFltN size 8", typeInfo{TypeId: typeFltN, Size: 8}, "FLOAT"},
		{"typeBit", typeInfo{TypeId: typeBit}, "BIT"},
		{"typeBitN", typeInfo{TypeId: typeBitN}, "BIT"},
		{"typeDecimalN", typeInfo{TypeId: typeDecimalN}, "DECIMAL"},
		{"typeNumericN", typeInfo{TypeId: typeNumericN}, "DECIMAL"},
		{"typeMoney", typeInfo{TypeId: typeMoney, Size: 8}, "MONEY"},
		{"typeMoney4", typeInfo{TypeId: typeMoney4, Size: 4}, "SMALLMONEY"},
		{"typeMoneyN size 4", typeInfo{TypeId: typeMoneyN, Size: 4}, "SMALLMONEY"},
		{"typeMoneyN size 8", typeInfo{TypeId: typeMoneyN, Size: 8}, "MONEY"},
		{"typeDateTimeN size 4", typeInfo{TypeId: typeDateTimeN, Size: 4}, "SMALLDATETIME"},
		{"typeDateTimeN size 8", typeInfo{TypeId: typeDateTimeN, Size: 8}, "DATETIME"},
		{"typeDateTime2N", typeInfo{TypeId: typeDateTime2N}, "DATETIME2"},
		{"typeDateN", typeInfo{TypeId: typeDateN}, "DATE"},
		{"typeTimeN", typeInfo{TypeId: typeTimeN}, "TIME"},
		{"typeDateTimeOffsetN", typeInfo{TypeId: typeDateTimeOffsetN}, "DATETIMEOFFSET"},
		{"typeBigVarBin", typeInfo{TypeId: typeBigVarBin}, "VARBINARY"},
		{"typeBigVarChar", typeInfo{TypeId: typeBigVarChar}, "VARCHAR"},
		{"typeBigChar", typeInfo{TypeId: typeBigChar}, "CHAR"},
		{"typeNVarChar", typeInfo{TypeId: typeNVarChar}, "NVARCHAR"},
		{"typeNChar", typeInfo{TypeId: typeNChar}, "NCHAR"},
		{"typeVarChar", typeInfo{TypeId: typeVarChar}, "VARCHAR"},
		{"typeGuid", typeInfo{TypeId: typeGuid}, "UNIQUEIDENTIFIER"},
		{"typeXml", typeInfo{TypeId: typeXml}, "XML"},
		{"typeText", typeInfo{TypeId: typeText}, "TEXT"},
		{"typeNText", typeInfo{TypeId: typeNText}, "NTEXT"},
		{"typeImage", typeInfo{TypeId: typeImage}, "IMAGE"},
		{"typeVariant", typeInfo{TypeId: typeVariant}, "SQL_VARIANT"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer handlePanic(t)
			got := makeGoLangTypeName(tt.typeInfo)
			assert.Equal(t, tt.typeString, got, "makeGoLangTypeName()")
		})
	}
}

func TestMakeGoLangTypeLength(t *testing.T) {
	tests := []struct {
		name       string
		typeInfo   typeInfo
		typeLen    int64
		typeVarLen bool
	}{
		{"typeDateTime", typeInfo{TypeId: typeDateTime}, 0, false},
		{"typeDateTim4", typeInfo{TypeId: typeDateTim4}, 0, false},
		{"typeBigVarChar max", typeInfo{TypeId: typeBigVarChar, Size: 0xffff}, 2147483645, true},
		{"typeBigVarChar 10", typeInfo{TypeId: typeBigVarChar, Size: 10}, 10, true},
		{"typeBigBinary 30", typeInfo{TypeId: typeBigBinary, Size: 30}, 30, true},
		{"typeNVarChar max", typeInfo{TypeId: typeNVarChar, Size: 0xffff}, 1073741822, true},
		{"typeNVarChar 20", typeInfo{TypeId: typeNVarChar, Size: 20}, 10, true},
		{"typeBigVarBin max", typeInfo{TypeId: typeBigVarBin, Size: 0xffff}, 2147483645, true},
		{"typeBigVarBin 50", typeInfo{TypeId: typeBigVarBin, Size: 50}, 50, true},
		{"typeBigChar 100", typeInfo{TypeId: typeBigChar, Size: 100}, 100, true},
		{"typeNChar 40", typeInfo{TypeId: typeNChar, Size: 40}, 20, true},
		{"typeVarChar 25", typeInfo{TypeId: typeVarChar, Size: 25}, 25, true},
		{"typeText", typeInfo{TypeId: typeText}, 2147483647, true},
		{"typeNText", typeInfo{TypeId: typeNText}, 1073741823, true},
		{"typeImage", typeInfo{TypeId: typeImage}, 2147483647, true},
		{"typeXml", typeInfo{TypeId: typeXml}, 1073741822, true},
		{"typeInt4 not variable", typeInfo{TypeId: typeInt4}, 0, false},
		{"typeDecimalN not variable", typeInfo{TypeId: typeDecimalN}, 0, false},
		{"typeGuid", typeInfo{TypeId: typeGuid}, 0, false},
		{"typeVariant", typeInfo{TypeId: typeVariant}, 0, false},
		{"typeUdt hierarchyid", typeInfo{TypeId: typeUdt, UdtInfo: udtInfo{TypeName: "hierarchyid"}}, 892, true},
		{"typeUdt geography", typeInfo{TypeId: typeUdt, UdtInfo: udtInfo{TypeName: "geography"}}, 2147483647, true},
		{"typeUdt geometry", typeInfo{TypeId: typeUdt, UdtInfo: udtInfo{TypeName: "geometry"}}, 2147483647, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer handlePanic(t)
			n, v := makeGoLangTypeLength(tt.typeInfo)
			assert.Equal(t, tt.typeVarLen, v, "makeGoLangTypeLength() varLen")
			assert.Equal(t, tt.typeLen, n, "makeGoLangTypeLength() length")
		})
	}
}

func TestMakeGoLangTypePrecisionScale(t *testing.T) {
	tests := []struct {
		name     string
		typeInfo typeInfo
		prec     int64
		scale    int64
		hasPrec  bool
	}{
		{"typeDateTime", typeInfo{TypeId: typeDateTime}, 0, 0, false},
		{"typeDateTim4", typeInfo{TypeId: typeDateTim4}, 0, 0, false},
		{"typeBigBinary", typeInfo{TypeId: typeBigBinary}, 0, 0, false},
		{"typeDecimalN", typeInfo{TypeId: typeDecimalN, Prec: 18, Scale: 4}, 18, 4, true},
		{"typeNumericN", typeInfo{TypeId: typeNumericN, Prec: 38, Scale: 10}, 38, 10, true},
		{"typeMoneyN size 4", typeInfo{TypeId: typeMoneyN, Size: 4}, 0, 0, false},
		{"typeMoneyN size 8", typeInfo{TypeId: typeMoneyN, Size: 8}, 0, 0, false},
		{"typeMoney", typeInfo{TypeId: typeMoney, Size: 8}, 0, 0, false},
		{"typeMoney4", typeInfo{TypeId: typeMoney4, Size: 4}, 0, 0, false},
		{"typeDateTime2N", typeInfo{TypeId: typeDateTime2N, Prec: 27, Scale: 7}, 27, 7, true},
		{"typeDateTimeOffsetN", typeInfo{TypeId: typeDateTimeOffsetN, Prec: 34, Scale: 5}, 34, 5, true},
		{"typeTimeN", typeInfo{TypeId: typeTimeN, Prec: 16, Scale: 3}, 16, 3, true},
		{"typeInt4", typeInfo{TypeId: typeInt4}, 0, 0, false},
		{"typeBit", typeInfo{TypeId: typeBit}, 0, 0, false},
		{"typeFltN size 4", typeInfo{TypeId: typeFltN, Size: 4}, 0, 0, false},
		{"typeFltN size 8", typeInfo{TypeId: typeFltN, Size: 8}, 0, 0, false},
		{"typeDateTimeN size 4", typeInfo{TypeId: typeDateTimeN, Size: 4}, 0, 0, false},
		{"typeDateTimeN size 8", typeInfo{TypeId: typeDateTimeN, Size: 8}, 0, 0, false},
		{"typeDateN", typeInfo{TypeId: typeDateN}, 0, 0, false},
		{"typeBigVarBin", typeInfo{TypeId: typeBigVarBin}, 0, 0, false},
		{"typeVarChar", typeInfo{TypeId: typeVarChar}, 0, 0, false},
		{"typeNVarChar", typeInfo{TypeId: typeNVarChar}, 0, 0, false},
		{"typeGuid", typeInfo{TypeId: typeGuid}, 0, 0, false},
		{"typeXml", typeInfo{TypeId: typeXml}, 0, 0, false},
		{"typeText", typeInfo{TypeId: typeText}, 0, 0, false},
		{"typeNText", typeInfo{TypeId: typeNText}, 0, 0, false},
		{"typeImage", typeInfo{TypeId: typeImage}, 0, 0, false},
		{"typeVariant", typeInfo{TypeId: typeVariant}, 0, 0, false},
		{"typeUdt", typeInfo{TypeId: typeUdt}, 0, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer handlePanic(t)
			prec, scale, hasPrec := makeGoLangTypePrecisionScale(tt.typeInfo)
			assert.Equal(t, tt.hasPrec, hasPrec, "makeGoLangTypePrecisionScale() hasPrec")
			assert.Equal(t, tt.prec, prec, "makeGoLangTypePrecisionScale() prec")
			assert.Equal(t, tt.scale, scale, "makeGoLangTypePrecisionScale() scale")
		})
	}
}

func TestMakeDecl(t *testing.T) {
	tests := []struct {
		name     string
		typeInfo typeInfo
		expected string
	}{
		{"varchar(max)", typeInfo{TypeId: typeVarChar, Size: 0xffff}, "varchar(max)"},
		{"varchar(8000)", typeInfo{TypeId: typeVarChar, Size: 8000}, "varchar(8000)"},
		{"varchar(4001)", typeInfo{TypeId: typeVarChar, Size: 4001}, "varchar(4001)"},
		{"nvarchar(max)", typeInfo{TypeId: typeNVarChar, Size: 0xffff}, "nvarchar(max)"},
		{"nvarchar(4000)", typeInfo{TypeId: typeNVarChar, Size: 8000}, "nvarchar(4000)"},
		{"nvarchar(2001)", typeInfo{TypeId: typeNVarChar, Size: 4002}, "nvarchar(2001)"},
		{"varbinary(max)", typeInfo{TypeId: typeBigVarBin, Size: 0xffff}, "varbinary(max)"},
		{"varbinary(8000)", typeInfo{TypeId: typeBigVarBin, Size: 8000}, "varbinary(8000)"},
		{"varbinary(4001)", typeInfo{TypeId: typeBigVarBin, Size: 4001}, "varbinary(4001)"},
		{"typeNull", typeInfo{TypeId: typeNull}, "nvarchar(1)"},
		{"typeInt1", typeInfo{TypeId: typeInt1}, "tinyint"},
		{"typeInt2", typeInfo{TypeId: typeInt2}, "smallint"},
		{"typeInt4", typeInfo{TypeId: typeInt4}, "int"},
		{"typeInt8", typeInfo{TypeId: typeInt8}, "bigint"},
		{"typeFlt4", typeInfo{TypeId: typeFlt4}, "real"},
		{"typeFlt8", typeInfo{TypeId: typeFlt8}, "float"},
		{"typeBit", typeInfo{TypeId: typeBit}, "bit"},
		{"typeBitN", typeInfo{TypeId: typeBitN}, "bit"},
		{"typeBigBinary 50", typeInfo{TypeId: typeBigBinary, Size: 50}, "binary(50)"},
		{"typeIntN size 1", typeInfo{TypeId: typeIntN, Size: 1}, "tinyint"},
		{"typeIntN size 2", typeInfo{TypeId: typeIntN, Size: 2}, "smallint"},
		{"typeIntN size 4", typeInfo{TypeId: typeIntN, Size: 4}, "int"},
		{"typeIntN size 8", typeInfo{TypeId: typeIntN, Size: 8}, "bigint"},
		{"typeFltN size 4", typeInfo{TypeId: typeFltN, Size: 4}, "real"},
		{"typeFltN size 8", typeInfo{TypeId: typeFltN, Size: 8}, "float"},
		{"typeDecimalN", typeInfo{TypeId: typeDecimalN, Prec: 18, Scale: 4}, "decimal(18, 4)"},
		{"typeDecimal", typeInfo{TypeId: typeDecimal, Prec: 10, Scale: 2}, "decimal(10, 2)"},
		{"typeNumericN", typeInfo{TypeId: typeNumericN, Prec: 20, Scale: 5}, "numeric(20, 5)"},
		{"typeNumeric", typeInfo{TypeId: typeNumeric, Prec: 15, Scale: 3}, "numeric(15, 3)"},
		{"typeMoney4", typeInfo{TypeId: typeMoney4}, "smallmoney"},
		{"typeMoney", typeInfo{TypeId: typeMoney}, "money"},
		{"typeMoneyN size 4", typeInfo{TypeId: typeMoneyN, Size: 4}, "smallmoney"},
		{"typeMoneyN size 8", typeInfo{TypeId: typeMoneyN, Size: 8}, "money"},
		{"typeDateTime", typeInfo{TypeId: typeDateTime}, "datetime"},
		{"typeDateTim4", typeInfo{TypeId: typeDateTim4}, "smalldatetime"},
		{"typeDateTimeN size 4", typeInfo{TypeId: typeDateTimeN, Size: 4}, "smalldatetime"},
		{"typeDateTimeN size 8", typeInfo{TypeId: typeDateTimeN, Size: 8}, "datetime"},
		{"typeDateTime2N", typeInfo{TypeId: typeDateTime2N, Scale: 7}, "datetime2(7)"},
		{"typeDateN", typeInfo{TypeId: typeDateN}, "date"},
		{"typeTimeN size 5", typeInfo{TypeId: typeTimeN, Scale: 5}, "time(5)"},
		{"typeTimeN size 7", typeInfo{TypeId: typeTimeN, Scale: 7}, "time"},
		{"typeDateTimeOffsetN", typeInfo{TypeId: typeDateTimeOffsetN, Scale: 3}, "datetimeoffset(3)"},
		{"typeText", typeInfo{TypeId: typeText}, "text"},
		{"typeNText", typeInfo{TypeId: typeNText}, "ntext"},
		{"typeBigVarChar 100", typeInfo{TypeId: typeBigVarChar, Size: 100}, "varchar(100)"},
		{"typeBigVarChar max", typeInfo{TypeId: typeBigVarChar, Size: 0xffff}, "varchar(max)"},
		{"typeBigChar 50", typeInfo{TypeId: typeBigChar, Size: 50}, "char(50)"},
		{"typeNChar 30", typeInfo{TypeId: typeNChar, Size: 60}, "nchar(30)"},
		{"typeGuid", typeInfo{TypeId: typeGuid}, "uniqueidentifier"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer handlePanic(t)
			got := makeDecl(tt.typeInfo)
			assert.Equal(t, tt.expected, got, "makeDecl()")
		})
	}
}

func handlePanic(t *testing.T) {
	if r := recover(); r != nil {
		assert.Fail(t, "recovered panic", "%v", r)
	}
}

// TestUnknownTypeDoesNotPanic verifies that ColumnType methods return safe
// defaults instead of panicking when encountering an unknown type ID. See #31.
func TestUnknownTypeDoesNotPanic(t *testing.T) {
	unknown := typeInfo{TypeId: 123}

	t.Run("ScanType", func(t *testing.T) {
		defer handlePanic(t)
		got := makeGoLangScanType(unknown)
		assert.Equal(t, reflect.TypeOf((*interface{})(nil)).Elem(), got)
	})
	t.Run("TypeName", func(t *testing.T) {
		defer handlePanic(t)
		got := makeGoLangTypeName(unknown)
		assert.Equal(t, "", got)
	})
	t.Run("Decl", func(t *testing.T) {
		defer handlePanic(t)
		got := makeDecl(unknown)
		assert.Equal(t, "", got)
	})
	t.Run("TypeLength", func(t *testing.T) {
		defer handlePanic(t)
		n, ok := makeGoLangTypeLength(unknown)
		assert.Equal(t, int64(0), n)
		assert.False(t, ok)
	})
	t.Run("PrecisionScale", func(t *testing.T) {
		defer handlePanic(t)
		prec, scale, ok := makeGoLangTypePrecisionScale(unknown)
		assert.Equal(t, int64(0), prec)
		assert.Equal(t, int64(0), scale)
		assert.False(t, ok)
	})
	t.Run("UdtTypeLength", func(t *testing.T) {
		defer handlePanic(t)
		// An unrecognized UDT is still variable length, so report the max
		// length readVarLen parsed from COLMETADATA rather than claiming the
		// column has no length.
		udt := typeInfo{TypeId: typeUdt, Size: 8000, UdtInfo: udtInfo{TypeName: "someunknown"}}
		n, ok := makeGoLangTypeLength(udt)
		assert.Equal(t, int64(8000), n)
		assert.True(t, ok)
	})
	t.Run("UdtTypeLengthMax", func(t *testing.T) {
		defer handlePanic(t)
		udt := typeInfo{TypeId: typeUdt, Size: 0xffff, UdtInfo: udtInfo{TypeName: "someunknown"}}
		n, ok := makeGoLangTypeLength(udt)
		assert.Equal(t, int64(2147483647), n)
		assert.True(t, ok)
	})
}

// TestReadFixedType_Flt4_ReturnsFloat64 verifies that REAL (typeFlt4) values
// returned by readFixedType are widened to float64, matching the scan type
// advertised by ColumnTypeScanType and the database/sql contract. Returning
// float32 here previously caused downstream scan failures for *float64 destinations.
func TestReadFixedType_Flt4_ReturnsFloat64(t *testing.T) {
	const want = float32(3.14)

	ti := typeInfo{TypeId: typeFlt4, Size: 4, Buffer: make([]byte, 4)}
	binary.LittleEndian.PutUint32(ti.Buffer, math.Float32bits(want))

	buf := newTdsBuffer(512, nil)
	copy(buf.rbuf[:4], ti.Buffer)
	buf.rpos = 0
	buf.rsize = 4

	got := readFixedType(&ti, buf, nil, msdsn.EncodeParameters{Timezone: time.UTC})

	gotF64, ok := got.(float64)
	if !ok {
		t.Fatalf("readFixedType returned %T, want float64", got)
	}
	assert.Equal(t, float64(want), gotF64)
}

// plpStream builds a PLP byte stream carrying payload but advertising
// advertisedSize as its total length, which a server may set larger than the
// data that follows.
func plpStream(advertisedSize uint64, payload []byte) []byte {
	out := make([]byte, 0, 8+4+len(payload)+4)
	var sz [8]byte
	binary.LittleEndian.PutUint64(sz[:], advertisedSize)
	out = append(out, sz[:]...)

	var chunk [4]byte
	binary.LittleEndian.PutUint32(chunk[:], uint32(len(payload)))
	out = append(out, chunk[:]...)
	out = append(out, payload...)

	// PLP terminator: a zero-length chunk.
	out = append(out, 0, 0, 0, 0)
	return out
}

// readPLPStream feeds a prebuilt PLP stream through readPLPType.
func readPLPStream(stream []byte) interface{} {
	buf := newTdsBuffer(uint16(len(stream)), nil)
	copy(buf.rbuf[:len(stream)], stream)
	buf.rpos = 0
	buf.rsize = len(stream)
	buf.final = true

	ti := typeInfo{TypeId: typeBigVarBin}
	return readPLPType(&ti, buf, nil, msdsn.EncodeParameters{})
}

// TestReadPLPType_OversizedLengthPanics is a regression test for issue #218:
// readPLPType used the untrusted advertised length as the initial buffer
// capacity, so a crafted size aborted the process with an OOM. A (max) LOB tops
// out at 2 GB - 1 byte, so anything larger is a malformed stream and must be
// rejected before it reaches make().
func TestReadPLPType_OversizedLengthPanics(t *testing.T) {
	sizes := map[string]uint64{
		"72 petabytes":     0x00FFFFFFFFFFFFFF,
		"one over the max": _MAX_PLP_LEN + 1,
	}

	for name, size := range sizes {
		t.Run(name, func(t *testing.T) {
			defer func() {
				v := recover()
				if v == nil {
					t.Fatalf("expected panic for PLP length %d", size)
				}
				err, ok := v.(error)
				if !ok {
					t.Fatalf("recovered %T, want error", v)
				}
				assert.Contains(t, err.Error(), "exceeds the maximum LOB size")
			}()

			readPLPStream(plpStream(size, []byte("actual payload is tiny")))
		})
	}
}

// TestReadPLPType_OverAdvertisedLengthAccepted verifies a size within the limit
// is still read normally even when it overstates the payload that follows,
// which a legitimate server is allowed to do. The size stays well below
// _MAX_PLP_LEN so the test does not preallocate 2 GB.
func TestReadPLPType_OverAdvertisedLengthAccepted(t *testing.T) {
	payload := []byte("short payload, large advertised size")
	got := readPLPStream(plpStream(1<<20, payload))

	gotBytes, ok := got.([]byte)
	if !ok {
		t.Fatalf("readPLPType returned %T, want []byte", got)
	}
	assert.Equal(t, payload, gotBytes)
}

// TestReadPLPType_UnknownLength verifies the _UNKNOWN_PLP_LEN path still decodes
// correctly.
func TestReadPLPType_UnknownLength(t *testing.T) {
	payload := []byte("streamed without a known length")
	stream := plpStream(_UNKNOWN_PLP_LEN, payload)

	buf := newTdsBuffer(uint16(len(stream)), nil)
	copy(buf.rbuf[:len(stream)], stream)
	buf.rpos = 0
	buf.rsize = len(stream)
	buf.final = true

	ti := typeInfo{TypeId: typeBigVarBin}
	got := readPLPType(&ti, buf, nil, msdsn.EncodeParameters{})

	gotBytes, ok := got.([]byte)
	if !ok {
		t.Fatalf("readPLPType returned %T, want []byte", got)
	}
	assert.Equal(t, payload, gotBytes)
}

// readVarLenSize drives readVarLen for a byte-len type advertising the given size.
func readVarLenSize(typeId uint8, size byte) *typeInfo {
	buf := newTdsBuffer(512, nil)
	buf.rbuf[0] = size
	buf.rpos = 0
	buf.rsize = 1
	buf.final = true

	ti := typeInfo{TypeId: typeId}
	readVarLen(&ti, buf, nil, msdsn.EncodeParameters{})
	return &ti
}

// TestReadVarLen_InvalidFixedWidthSizeRejected is a regression test for #364:
// the ColumnType helpers switch on typeInfo.Size for the fixed-width nullable
// types and panic outside any recover, so a server advertising an unsupported
// size crashed the caller's goroutine rather than failing the stream.
func TestReadVarLen_InvalidFixedWidthSizeRejected(t *testing.T) {
	cases := []struct {
		name   string
		typeId uint8
		size   byte
	}{
		{"UNIQUEIDENTIFIER size 0", typeGuid, 0},
		{"INTNTYPE size 3", typeIntN, 3},
		{"BITNTYPE size 0", typeBitN, 0},
		{"FLNNTYPE size 2", typeFltN, 2},
		{"MONEYN size 3", typeMoneyN, 3},
		{"DATETIMEN size 5", typeDateTimeN, 5},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				v := recover()
				if v == nil {
					t.Fatalf("expected panic for %s", tc.name)
				}
				// Must be a StreamError: checkBadConn only drops the connection
				// for that type, and a desynced connection must not be reused.
				se, ok := v.(StreamError)
				if !ok {
					t.Fatalf("recovered %T, want StreamError", v)
				}
				assert.Contains(t, se.Error(), "invalid size")
			}()

			readVarLenSize(tc.typeId, tc.size)
		})
	}
}

// TestReadVarLen_ValidFixedWidthSizesAccepted keeps the new guard from
// rejecting sizes a legitimate server sends.
func TestReadVarLen_ValidFixedWidthSizesAccepted(t *testing.T) {
	cases := []struct {
		name   string
		typeId uint8
		size   byte
	}{
		{"UNIQUEIDENTIFIER size 16", typeGuid, 16},
		{"INTNTYPE size 1", typeIntN, 1},
		{"INTNTYPE size 2", typeIntN, 2},
		{"INTNTYPE size 4", typeIntN, 4},
		{"INTNTYPE size 8", typeIntN, 8},
		{"BITNTYPE size 1", typeBitN, 1},
		{"FLNNTYPE size 4", typeFltN, 4},
		{"FLNNTYPE size 8", typeFltN, 8},
		{"MONEYN size 4", typeMoneyN, 4},
		{"MONEYN size 8", typeMoneyN, 8},
		{"DATETIMEN size 4", typeDateTimeN, 4},
		{"DATETIMEN size 8", typeDateTimeN, 8},
		{"VARCHAR keeps its caller-defined width", typeVarChar, 3},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ti := readVarLenSize(tc.typeId, tc.size)
			assert.Equal(t, int(tc.size), ti.Size)
		})
	}
}
