package mssql

import (
	"reflect"
	"testing"
	"time"

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
		{"typeVariant", typeInfo{TypeId: typeVariant}, reflect.TypeOf(nil)},
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
		{"typeTimeN", typeInfo{TypeId: typeTimeN, Scale: 5}, "time"},
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
