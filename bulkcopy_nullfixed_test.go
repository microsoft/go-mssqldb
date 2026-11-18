package mssql

import (
	"bytes"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func nullFixedTestBulk(mid columnStruct) *Bulk {
	intN := func(name string) columnStruct {
		return columnStruct{ColName: name, ti: typeInfo{TypeId: typeIntN, Size: 4, Writer: writeByteLenType}}
	}
	return &Bulk{
		cn:          &Conn{sess: &tdsSession{encoding: benchmarkEncoding()}},
		bulkColumns: []columnStruct{intN("a"), mid, intN("b")},
	}
}

func TestBulkCopyNilIntoFixedLengthColumn(t *testing.T) {
	ts := time.Date(2020, 1, 2, 3, 4, 0, 0, time.UTC)
	tests := []struct {
		name   string
		typeId byte
		size   int
		valid  interface{}
	}{
		{"tinyint", typeInt1, 1, int64(1)},
		{"bit", typeBit, 1, true},
		{"smallint", typeInt2, 2, int64(1)},
		{"int", typeInt4, 4, int64(1)},
		{"bigint", typeInt8, 8, int64(1)},
		{"real", typeFlt4, 4, float64(1.5)},
		{"float", typeFlt8, 8, float64(1.5)},
		{"money", typeMoney, 8, decimal.NewFromFloat(1.5)},
		{"smallmoney", typeMoney4, 4, decimal.NewFromFloat(1.5)},
		{"datetime", typeDateTime, 8, ts},
		{"smalldatetime", typeDateTim4, 4, ts},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := nullFixedTestBulk(columnStruct{ColName: "v", ti: typeInfo{TypeId: tt.typeId, Size: tt.size, Writer: writeFixedType}})

			for _, null := range []interface{}{nil, sql.NullInt64{}} {
				data, err := b.makeRowData([]interface{}{int64(7), null, int64(9)})
				if err == nil {
					t.Fatalf("%T: expected error, got row bytes % x", null, data)
				}
				if !strings.Contains(err.Error(), `column "v"`) {
					t.Errorf("%T: error does not name the column: %v", null, err)
				}
			}

			data, err := b.makeRowData([]interface{}{int64(7), tt.valid, int64(9)})
			if err != nil {
				t.Fatalf("valid value: %v", err)
			}
			// row token + (len byte + 4) + fixed value + (len byte + 4)
			if want := 1 + 5 + tt.size + 5; len(data) != want {
				t.Errorf("valid value: got %d bytes, want %d: % x", len(data), want, data)
			}
		})
	}
}

// Nullable columns arrive as N-types and must keep encoding nil as a NULL marker.
func TestBulkCopyNilIntoNullableColumn(t *testing.T) {
	tests := []struct {
		name   string
		typeId byte
		size   int
	}{
		{"intN", typeIntN, 4},
		{"bitN", typeBitN, 1},
		{"fltN", typeFltN, 8},
		{"moneyN", typeMoneyN, 8},
		{"datetimeN", typeDateTimeN, 8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := nullFixedTestBulk(columnStruct{ColName: "v", ti: typeInfo{TypeId: tt.typeId, Size: tt.size, Writer: writeByteLenType}})
			data, err := b.makeRowData([]interface{}{int64(7), nil, int64(9)})
			if err != nil {
				t.Fatal(err)
			}
			want := []byte{byte(tokenRow), 4, 7, 0, 0, 0, 0, 4, 9, 0, 0, 0}
			if !bytes.Equal(data, want) {
				t.Errorf("got % x, want % x", data, want)
			}
		})
	}
}
