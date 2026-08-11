package mssql

import (
	"database/sql"
	"testing"
	"time"
)

// Benchmarks for convertAssign — the hot path called per-column per-row during Scan.

func BenchmarkConvertAssign_StringToString(b *testing.B) {
	src := "hello world"
	for i := 0; i < b.N; i++ {
		var dest string
		if err := convertAssign(&dest, src); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkConvertAssign_StringToBytes(b *testing.B) {
	src := "hello world test data for benchmark"
	for i := 0; i < b.N; i++ {
		var dest []byte
		if err := convertAssign(&dest, src); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkConvertAssign_BytesToString(b *testing.B) {
	src := []byte("hello world test data for benchmark")
	for i := 0; i < b.N; i++ {
		var dest string
		if err := convertAssign(&dest, src); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkConvertAssign_BytesToBytes(b *testing.B) {
	src := []byte("hello world test data for benchmark")
	for i := 0; i < b.N; i++ {
		var dest []byte
		if err := convertAssign(&dest, src); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkConvertAssign_Int64ToInt64(b *testing.B) {
	src := int64(1234567890)
	for i := 0; i < b.N; i++ {
		var dest int64
		if err := convertAssign(&dest, src); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkConvertAssign_Int64ToString(b *testing.B) {
	src := int64(1234567890)
	for i := 0; i < b.N; i++ {
		var dest string
		if err := convertAssign(&dest, src); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkConvertAssign_Float64ToFloat64(b *testing.B) {
	src := float64(3.14159265358979)
	for i := 0; i < b.N; i++ {
		var dest float64
		if err := convertAssign(&dest, src); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkConvertAssign_TimeToTime(b *testing.B) {
	src := time.Now()
	for i := 0; i < b.N; i++ {
		var dest time.Time
		if err := convertAssign(&dest, src); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkConvertAssign_TimeToString(b *testing.B) {
	src := time.Now()
	for i := 0; i < b.N; i++ {
		var dest string
		if err := convertAssign(&dest, src); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkConvertAssign_NilToBytes(b *testing.B) {
	for i := 0; i < b.N; i++ {
		var dest []byte
		if err := convertAssign(&dest, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkConvertAssign_Int64ToInterface(b *testing.B) {
	src := int64(42)
	for i := 0; i < b.N; i++ {
		var dest interface{}
		if err := convertAssign(&dest, src); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkConvertAssign_StringToRawBytes(b *testing.B) {
	src := "hello world test data for scan benchmark"
	for i := 0; i < b.N; i++ {
		var dest sql.RawBytes
		if err := convertAssign(&dest, src); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkConvertAssign_BoolToBool(b *testing.B) {
	src := true
	for i := 0; i < b.N; i++ {
		var dest bool
		if err := convertAssign(&dest, src); err != nil {
			b.Fatal(err)
		}
	}
}
