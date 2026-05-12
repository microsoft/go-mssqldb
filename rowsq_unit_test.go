//go:build go1.10
// +build go1.10

package mssql

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRowsq_ColumnTypeScanType(t *testing.T) {
	tests := []struct {
		name     string
		typeId   byte
		wantType reflect.Type
	}{
		{
			name:     "int4",
			typeId:   typeInt4,
			wantType: reflect.TypeOf(int64(0)),
		},
		{
			name:     "int8",
			typeId:   typeInt8,
			wantType: reflect.TypeOf(int64(0)),
		},
		{
			name:     "float4",
			typeId:   typeFlt4,
			wantType: reflect.TypeOf(float64(0)),
		},
		{
			name:     "float8",
			typeId:   typeFlt8,
			wantType: reflect.TypeOf(float64(0)),
		},
		{
			name:     "bit",
			typeId:   typeBitN,
			wantType: reflect.TypeOf(true),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Rowsq{
				cols: []columnStruct{
					{
						ti: typeInfo{TypeId: tt.typeId},
					},
				},
			}
			got := r.ColumnTypeScanType(0)
			assert.Equal(t, tt.wantType, got, "ColumnTypeScanType()")
		})
	}
}

func TestRowsq_ColumnTypeDatabaseTypeName(t *testing.T) {
	tests := []struct {
		name     string
		typeId   byte
		wantName string
	}{
		{
			name:     "int4",
			typeId:   typeInt4,
			wantName: "INT",
		},
		{
			name:     "int8",
			typeId:   typeInt8,
			wantName: "BIGINT",
		},
		{
			name:     "float8",
			typeId:   typeFlt8,
			wantName: "FLOAT",
		},
		{
			name:     "bit",
			typeId:   typeBitN,
			wantName: "BIT",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Rowsq{
				cols: []columnStruct{
					{
						ti: typeInfo{TypeId: tt.typeId},
					},
				},
			}
			got := r.ColumnTypeDatabaseTypeName(0)
			assert.Equal(t, tt.wantName, got, "ColumnTypeDatabaseTypeName()")
		})
	}
}

func TestRowsq_ColumnTypeLength(t *testing.T) {
	tests := []struct {
		name    string
		typeId  byte
		size    int
		wantLen int64
		wantOk  bool
	}{
		{
			name:    "int4 not variable length",
			typeId:  typeInt4,
			size:    4,
			wantLen: 0,
			wantOk:  false,
		},
		{
			name:    "bigvarchar variable length",
			typeId:  typeBigVarChar,
			size:    100,
			wantLen: 100,
			wantOk:  true,
		},
		{
			name:    "nvarchar variable length",
			typeId:  typeNVarChar,
			size:    50,
			wantLen: 25, // nvarchar uses 2 bytes per character, so size is divided by 2
			wantOk:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Rowsq{
				cols: []columnStruct{
					{
						ti: typeInfo{TypeId: tt.typeId, Size: tt.size},
					},
				},
			}
			gotLen, gotOk := r.ColumnTypeLength(0)
			assert.Equal(t, tt.wantLen, gotLen, "ColumnTypeLength() length")
			assert.Equal(t, tt.wantOk, gotOk, "ColumnTypeLength() ok")
		})
	}
}

func TestRowsq_ColumnTypePrecisionScale(t *testing.T) {
	tests := []struct {
		name      string
		typeId    byte
		prec      uint8
		scale     uint8
		wantPrec  int64
		wantScale int64
		wantOk    bool
	}{
		{
			name:      "int4 no precision/scale",
			typeId:    typeInt4,
			prec:      0,
			scale:     0,
			wantPrec:  0,
			wantScale: 0,
			wantOk:    false,
		},
		{
			name:      "decimal has precision/scale",
			typeId:    typeDecimalN,
			prec:      18,
			scale:     4,
			wantPrec:  18,
			wantScale: 4,
			wantOk:    true,
		},
		{
			name:      "numeric has precision/scale",
			typeId:    typeNumericN,
			prec:      10,
			scale:     2,
			wantPrec:  10,
			wantScale: 2,
			wantOk:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Rowsq{
				cols: []columnStruct{
					{
						ti: typeInfo{TypeId: tt.typeId, Prec: tt.prec, Scale: tt.scale},
					},
				},
			}
			gotPrec, gotScale, gotOk := r.ColumnTypePrecisionScale(0)
			assert.Equal(t, tt.wantPrec, gotPrec, "ColumnTypePrecisionScale() precision")
			assert.Equal(t, tt.wantScale, gotScale, "ColumnTypePrecisionScale() scale")
			assert.Equal(t, tt.wantOk, gotOk, "ColumnTypePrecisionScale() ok")
		})
	}
}

func TestRowsq_ColumnTypeNullable(t *testing.T) {
	tests := []struct {
		name         string
		flags        uint16
		wantNullable bool
		wantOk       bool
	}{
		{
			name:         "nullable column",
			flags:        colFlagNullable,
			wantNullable: true,
			wantOk:       true,
		},
		{
			name:         "non-nullable column",
			flags:        0,
			wantNullable: false,
			wantOk:       true,
		},
		{
			name:         "nullable with other flags",
			flags:        colFlagNullable | colFlagEncrypted,
			wantNullable: true,
			wantOk:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Rowsq{
				cols: []columnStruct{
					{
						Flags: tt.flags,
					},
				},
			}
			gotNullable, gotOk := r.ColumnTypeNullable(0)
			assert.Equal(t, tt.wantNullable, gotNullable, "ColumnTypeNullable() nullable")
			assert.Equal(t, tt.wantOk, gotOk, "ColumnTypeNullable() ok")
		})
	}
}
