package mssql

import "testing"

// Benchmarks for the VECTOR codecs.
//
// Only the paths makeVectorOrJSON can actually reach are covered. It sends
// native binary for float32 when the server negotiated vector support, and JSON
// otherwise — including for every float16 parameter, since TDS has no binary
// float16 parameter format yet. So encodeToBytes is benchmarked for float32
// only, while decodeFromBytes is benchmarked for both, because the server does
// return float16 columns in binary.
//
// 384 and 1536 are representative embedding widths (all-MiniLM and OpenAI
// ada-002), both within the 1998-dimension float32 limit.

func benchVector(elementType VectorElementType, dims int) Vector {
	data := make([]float32, dims)
	for i := range data {
		data[i] = float32(i) * 0.001
	}
	return Vector{ElementType: elementType, Data: data}
}

func benchmarkEncodeVector(b *testing.B, elementType VectorElementType, dims int) {
	v := benchVector(elementType, dims)
	wire, err := v.encodeToBytes()
	if err != nil {
		b.Fatal(err)
	}

	b.SetBytes(int64(len(wire)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := v.encodeToBytes(); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkDecodeVector(b *testing.B, elementType VectorElementType, dims int) {
	wire, err := benchVector(elementType, dims).encodeToBytes()
	if err != nil {
		b.Fatal(err)
	}

	var v Vector
	b.SetBytes(int64(len(wire)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := v.decodeFromBytes(wire); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncodeVector_Float32_384(b *testing.B) {
	benchmarkEncodeVector(b, VectorElementFloat32, 384)
}

func BenchmarkEncodeVector_Float32_1536(b *testing.B) {
	benchmarkEncodeVector(b, VectorElementFloat32, 1536)
}

func BenchmarkDecodeVector_Float32_384(b *testing.B) {
	benchmarkDecodeVector(b, VectorElementFloat32, 384)
}

func BenchmarkDecodeVector_Float32_1536(b *testing.B) {
	benchmarkDecodeVector(b, VectorElementFloat32, 1536)
}

func BenchmarkDecodeVector_Float16_384(b *testing.B) {
	benchmarkDecodeVector(b, VectorElementFloat16, 384)
}

func BenchmarkDecodeVector_Float16_1536(b *testing.B) {
	benchmarkDecodeVector(b, VectorElementFloat16, 1536)
}

// Value() serialises through ToJSON on every parameter bind.
func BenchmarkEncodeVector_JSON_1536(b *testing.B) {
	v := benchVector(VectorElementFloat32, 1536)

	b.SetBytes(int64(len(v.ToJSON())))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = v.ToJSON()
	}
}

// Scan takes this path when a vector column comes back as a JSON string.
func BenchmarkDecodeVector_JSON_1536(b *testing.B) {
	js := benchVector(VectorElementFloat32, 1536).ToJSON()

	var v Vector
	b.SetBytes(int64(len(js)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := v.decodeFromJSON(js); err != nil {
			b.Fatal(err)
		}
	}
}
