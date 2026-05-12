package mssql

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"

	"github.com/microsoft/go-mssqldb/msdsn"
)

// Benchmarks for RPC parameter encoding — the hot path for every parameterized query.

func BenchmarkWriteTypeInfo_Int8(b *testing.B) {
	ti := typeInfo{TypeId: typeIntN, Size: 8}
	buf := new(bytes.Buffer)
	enc := msdsn.EncodeParameters{}

	for i := 0; i < b.N; i++ {
		buf.Reset()
		if err := writeTypeInfo(buf, &ti, false, enc); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWriteTypeInfo_NVarChar(b *testing.B) {
	ti := typeInfo{TypeId: typeNVarChar, Size: 100}
	buf := new(bytes.Buffer)
	enc := msdsn.EncodeParameters{}

	for i := 0; i < b.N; i++ {
		buf.Reset()
		if err := writeTypeInfo(buf, &ti, false, enc); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWriteTypeInfo_NVarCharMax(b *testing.B) {
	ti := typeInfo{TypeId: typeNVarChar, Size: 0}
	buf := new(bytes.Buffer)
	enc := msdsn.EncodeParameters{}

	for i := 0; i < b.N; i++ {
		buf.Reset()
		if err := writeTypeInfo(buf, &ti, false, enc); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWriteByteLenType(b *testing.B) {
	ti := typeInfo{TypeId: typeIntN, Size: 8}
	data := make([]byte, 8)
	binary.LittleEndian.PutUint64(data, 1234567890)
	buf := new(bytes.Buffer)
	enc := msdsn.EncodeParameters{}

	for i := 0; i < b.N; i++ {
		buf.Reset()
		if err := writeByteLenType(buf, ti, data, enc); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWriteShortLenType(b *testing.B) {
	ti := typeInfo{TypeId: typeNVarChar, Size: 100}
	data := make([]byte, 100)
	for i := range data {
		data[i] = byte(i)
	}
	buf := new(bytes.Buffer)
	enc := msdsn.EncodeParameters{}

	for i := 0; i < b.N; i++ {
		buf.Reset()
		if err := writeShortLenType(buf, ti, data, enc); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWritePLPType_Short(b *testing.B) {
	ti := typeInfo{TypeId: typeNVarChar, Size: 0}
	// 50-char string in UTF-16LE = 100 bytes
	data := make([]byte, 100)
	for i := 0; i < 50; i++ {
		data[i*2] = byte('A' + (i % 26))
		data[i*2+1] = 0
	}
	buf := new(bytes.Buffer)
	enc := msdsn.EncodeParameters{}

	for i := 0; i < b.N; i++ {
		buf.Reset()
		if err := writePLPType(buf, ti, data, enc); err != nil {
			b.Fatal(err)
		}
	}
}

type discardRWC struct{}

func (discardRWC) Read([]byte) (int, error)    { return 0, io.EOF }
func (discardRWC) Write(p []byte) (int, error) { return len(p), nil }
func (discardRWC) Close() error                { return nil }

func BenchmarkSendRpc_SingleIntParam(b *testing.B) {
	paramData := make([]byte, 8)
	binary.LittleEndian.PutUint64(paramData, 42)

	params := []param{
		{
			Name:  "@p1",
			Flags: 0,
			ti: typeInfo{
				TypeId: typeIntN,
				Size:   8,
				Writer: writeByteLenType,
			},
			buffer: paramData,
		},
	}
	headers := []headerStruct{
		{hdrtype: dataStmHdrTransDescr, data: transDescrHdr{0, 1}.pack()},
	}
	enc := msdsn.EncodeParameters{}

	for i := 0; i < b.N; i++ {
		tdsBuf := newTdsBuffer(defaultPacketSize, discardRWC{})
		if err := sendRpc(tdsBuf, headers, sp_ExecuteSql, 0, params, false, enc); err != nil {
			b.Fatal(err)
		}
	}
}
