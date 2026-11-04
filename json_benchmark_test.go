package mssql

import (
	"fmt"
	"strings"
	"testing"
)

// Benchmarks for the JSON type's parameter and scan paths.
//
// Two asymmetries are worth measuring. makeJsonParam hands the caller's bytes
// straight to the wire when the server negotiated JSON support, but converts
// through str2ucs2 when it did not. And JSON.Scan appends into its existing
// buffer while NullJSON.Scan allocates a fresh one per row.
//
// The UCS2 conversion itself is already covered by the Str2ucs2 and Ucs22str
// benchmarks; these measure the per-parameter and per-row cost around it.

// benchJSONDoc builds a JSON object of roughly the requested size.
func benchJSONDoc(approxBytes int) []byte {
	var b strings.Builder
	b.WriteString(`{"id":1,"tags":[`)
	for i := 0; b.Len() < approxBytes; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `"tag-%04d"`, i)
	}
	b.WriteString(`]}`)
	return []byte(b.String())
}

func benchJSONStmt(jsonSupported bool) *Stmt {
	return &Stmt{c: &Conn{sess: &tdsSession{jsonSupported: jsonSupported}}}
}

// Native support sends the caller's bytes without conversion.
func BenchmarkEncodeJSONParam_Native_1KB(b *testing.B) {
	stmt := benchJSONStmt(true)
	doc := benchJSONDoc(1024)

	b.SetBytes(int64(len(doc)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if p := stmt.makeJsonParam(doc, true); p.ti.TypeId != typeJson {
			b.Fatalf("TypeId = %#x, want %#x", p.ti.TypeId, typeJson)
		}
	}
}

// Without it the payload is re-encoded as UTF-16LE for nvarchar(max).
func BenchmarkEncodeJSONParam_Fallback_1KB(b *testing.B) {
	stmt := benchJSONStmt(false)
	doc := benchJSONDoc(1024)

	b.SetBytes(int64(len(doc)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if p := stmt.makeJsonParam(doc, true); p.ti.TypeId != typeNVarChar {
			b.Fatalf("TypeId = %#x, want %#x", p.ti.TypeId, typeNVarChar)
		}
	}
}

// JSON.Scan appends into the existing buffer, so repeated scans reuse capacity.
func BenchmarkDecodeJSONScan_1KB(b *testing.B) {
	doc := benchJSONDoc(1024)
	// Boxed outside the loop: database/sql hands Scan an already-boxed value, so
	// timing the conversion here would report an allocation callers do not pay.
	var src interface{} = doc

	var dst JSON
	b.SetBytes(int64(len(doc)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := dst.Scan(src); err != nil {
			b.Fatal(err)
		}
	}
}

// NullJSON.Scan copies into a fresh slice so it never retains a driver buffer.
func BenchmarkDecodeNullJSONScan_1KB(b *testing.B) {
	doc := benchJSONDoc(1024)
	var src interface{} = doc

	var dst NullJSON
	b.SetBytes(int64(len(doc)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := dst.Scan(src); err != nil {
			b.Fatal(err)
		}
	}
}
