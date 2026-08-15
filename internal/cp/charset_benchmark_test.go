package cp

import (
	"bytes"
	"testing"
)

// Benchmarks for CharsetToUTF8, the per-row varchar/char decode hot path.

var charSetToUTF8Out string

func BenchmarkCharsetToUTF8ASCIIShort(b *testing.B) {
	c := Collation{SortId: 50} // cp1252
	s := []byte("Hello, World!")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		charSetToUTF8Out = CharsetToUTF8(c, s)
	}
}

func BenchmarkCharsetToUTF8ASCIILong(b *testing.B) {
	c := Collation{SortId: 50}                      // cp1252
	s := bytes.Repeat([]byte("Hello, World! "), 10) // 140 bytes
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		charSetToUTF8Out = CharsetToUTF8(c, s)
	}
}

func BenchmarkCharsetToUTF8Extended(b *testing.B) {
	c := Collation{SortId: 50}       // cp1252
	s := []byte{'c', 'a', 'f', 0xe9} // café encoded in cp1252
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		charSetToUTF8Out = CharsetToUTF8(c, s)
	}
}
