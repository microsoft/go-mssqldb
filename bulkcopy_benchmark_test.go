package mssql

import (
	"testing"
	"time"
)

// Benchmarks for bulk copy parameter encoding — the hot path for every row in bulk insert.

func benchBulkCol(typeId byte, size int) columnStruct {
	return columnStruct{
		ti: typeInfo{
			TypeId: typeId,
			Size:   size,
		},
	}
}

func BenchmarkBulkMakeParam_Int64(b *testing.B) {
	bulk := &Bulk{cn: &Conn{sess: &tdsSession{encoding: benchmarkEncoding()}}}
	col := benchBulkCol(typeInt8, 8)

	for i := 0; i < b.N; i++ {
		_, err := bulk.makeParam(int64(1234567890), col)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBulkMakeParam_Int32(b *testing.B) {
	bulk := &Bulk{cn: &Conn{sess: &tdsSession{encoding: benchmarkEncoding()}}}
	col := benchBulkCol(typeInt4, 4)

	for i := 0; i < b.N; i++ {
		_, err := bulk.makeParam(int64(12345), col)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBulkMakeParam_Float64(b *testing.B) {
	bulk := &Bulk{cn: &Conn{sess: &tdsSession{encoding: benchmarkEncoding()}}}
	col := benchBulkCol(typeFlt8, 8)

	for i := 0; i < b.N; i++ {
		_, err := bulk.makeParam(float64(3.14159), col)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBulkMakeParam_String_NVarChar(b *testing.B) {
	bulk := &Bulk{cn: &Conn{sess: &tdsSession{encoding: benchmarkEncoding()}}}
	col := benchBulkCol(typeNVarChar, 200)

	val := "hello world benchmark test string"
	for i := 0; i < b.N; i++ {
		_, err := bulk.makeParam(val, col)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBulkMakeParam_String_VarChar(b *testing.B) {
	bulk := &Bulk{cn: &Conn{sess: &tdsSession{encoding: benchmarkEncoding()}}}
	col := benchBulkCol(typeBigVarChar, 200)

	val := "hello world benchmark test string"
	for i := 0; i < b.N; i++ {
		_, err := bulk.makeParam(val, col)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBulkMakeParam_DateTime(b *testing.B) {
	bulk := &Bulk{cn: &Conn{sess: &tdsSession{encoding: benchmarkEncoding()}}}
	col := benchBulkCol(typeDateTime, 8)

	val := time.Date(2024, 6, 15, 14, 30, 45, 0, time.UTC)
	for i := 0; i < b.N; i++ {
		_, err := bulk.makeParam(val, col)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBulkMakeParam_DateTime2(b *testing.B) {
	bulk := &Bulk{cn: &Conn{sess: &tdsSession{encoding: benchmarkEncoding()}}}
	col := columnStruct{
		ti: typeInfo{
			TypeId: typeDateTime2N,
			Size:   8,
			Scale:  7,
		},
	}

	val := time.Date(2024, 6, 15, 14, 30, 45, 123456789, time.UTC)
	for i := 0; i < b.N; i++ {
		_, err := bulk.makeParam(val, col)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBulkMakeParam_Bool(b *testing.B) {
	bulk := &Bulk{cn: &Conn{sess: &tdsSession{encoding: benchmarkEncoding()}}}
	col := benchBulkCol(typeBitN, 1)

	for i := 0; i < b.N; i++ {
		_, err := bulk.makeParam(true, col)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBulkMakeParam_MultiColumn(b *testing.B) {
	bulk := &Bulk{
		cn: &Conn{sess: &tdsSession{encoding: benchmarkEncoding()}},
		bulkColumns: []columnStruct{
			benchBulkCol(typeIntN, 8),
			benchBulkCol(typeNVarChar, 100),
			benchBulkCol(typeFltN, 8),
			benchBulkCol(typeBitN, 1),
		},
	}

	values := []interface{}{int64(42), "test string", float64(3.14), true}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for colIdx, val := range values {
			_, err := bulk.makeParam(val, bulk.bulkColumns[colIdx])
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}
