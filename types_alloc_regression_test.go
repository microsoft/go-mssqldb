package mssql

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/microsoft/go-mssqldb/msdsn"
	"github.com/stretchr/testify/assert"
)

func colMetadataVarBinary(count, size uint16) []byte {
	stream := binary.LittleEndian.AppendUint16([]byte{byte(tokenColMetadata)}, count)
	for i := uint16(0); i < count; i++ {
		stream = binary.LittleEndian.AppendUint32(stream, 0)
		stream = binary.LittleEndian.AppendUint16(stream, 0)
		stream = append(stream, typeBigVarBin)
		stream = binary.LittleEndian.AppendUint16(stream, size)
		stream = append(stream, 0)
	}
	return stream
}

func plpChunks(size uint64, chunks ...[]byte) []byte {
	stream := binary.LittleEndian.AppendUint64(nil, size)
	if size == _PLP_NULL {
		return stream
	}
	for _, chunk := range chunks {
		stream = binary.LittleEndian.AppendUint32(stream, uint32(len(chunk)))
		stream = append(stream, chunk...)
	}
	return binary.LittleEndian.AppendUint32(stream, 0)
}

func TestReadTypeInfo_DefersValueBuffers(t *testing.T) {
	cases := []struct {
		name     string
		typeID   byte
		metadata []byte
		size     int
	}{
		{"fixed", typeInt4, nil, 4},
		{"byte", typeVarBinary, []byte{255}, 255},
		{"date", typeDateN, nil, 3},
		{"time", typeTimeN, []byte{7}, 5},
		{"decimal", typeDecimalN, []byte{17, 38, 0}, 17},
		{"binary", typeBigVarBin, []byte{0xfe, 0xff}, 65534},
		{"unicode", typeNVarChar, []byte{0xfe, 0xff, 0, 0, 0, 0, 0}, 65534},
		{"udt", typeUdt, []byte{0xff, 0xff, 0, 0, 0, 0, 0}, 65535},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ti := readTypeInfo(bufFromBytes(tc.metadata), tc.typeID, nil, msdsn.EncodeParameters{})
			assert.Equal(t, tc.size, ti.Size)
			assert.Zero(t, cap(ti.Buffer), "metadata must not reserve storage for column values")
			assert.NotNil(t, ti.Reader)
		})
	}
}

func TestParseColMetadata72_DefersValueBuffers(t *testing.T) {
	// Scale the reported 65534-column input down to 64 columns so the unfixed
	// regression allocates only 4 MiB, not 4 GiB, while exercising the same path.
	const count = 64
	stream := colMetadataVarBinary(count, 0xfffe)
	columns := parseColMetadata72(bufFromBytes(stream[1:]), &tdsSession{})
	if len(columns) != count {
		t.Fatalf("got %d columns, want %d", len(columns), count)
	}
	for i, column := range columns {
		assert.Equal(t, 65534, column.ti.Size)
		if cap(column.ti.Buffer) != 0 {
			t.Fatalf("column %d reserved %d bytes before any row was read", i, cap(column.ti.Buffer))
		}
	}
}

func TestReadPLPType_LargeHintDoesNotPreallocate(t *testing.T) {
	for _, size := range []uint64{16 << 20, _MAX_PLP_LEN} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			r := bufFromBytes(plpChunks(size))
			ti := typeInfo{TypeId: typeBigVarBin}
			value := readPLPType(&ti, r, nil, msdsn.EncodeParameters{})
			data, ok := value.([]byte)
			if !ok || data == nil || len(data) != 0 {
				t.Fatalf("expected a non-nil empty binary value, got %T", value)
			}
			if cap(data) != 0 {
				t.Fatalf("empty PLP value retained %d bytes from its advertised length", cap(data))
			}
			_, err := r.ReadByte()
			assert.ErrorIs(t, err, io.EOF)
		})
	}
}

func TestValueReaders_LazyBufferReuse(t *testing.T) {
	shortValues := []byte{3, 0, 'a', 'b', 'c', 5, 0, 'd', 'e', 'f', 'g', 'h', 1, 0, 'i', 0, 0, 0xff, 0xff}
	cases := []struct {
		name      string
		typeID    byte
		metadata  []byte
		stream    []byte
		want      []interface{}
		maxBuffer int
	}{
		{"fixed", typeInt4, nil, []byte{41, 0, 0, 0, 42, 0, 0, 0}, []interface{}{int64(41), int64(42)}, 4},
		{"byte", typeVarBinary, []byte{255}, []byte{3, 'a', 'b', 'c', 5, 'd', 'e', 'f', 'g', 'h', 1, 'i', 0},
			[]interface{}{[]byte("abc"), []byte("defgh"), []byte("i"), nil}, 5},
		{"short", typeBigVarBin, []byte{0xfe, 0xff}, shortValues,
			[]interface{}{[]byte("abc"), []byte("defgh"), []byte("i"), []byte{}, nil}, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ti := readTypeInfo(bufFromBytes(tc.metadata), tc.typeID, nil, msdsn.EncodeParameters{})
			stream := append(append([]byte{}, tc.stream...), tc.stream...)
			r := bufFromBytes(stream)
			for attempt := 0; attempt < 2; attempt++ {
				var got []interface{}
				for range tc.want {
					got = append(got, ti.Reader(&ti, r, nil, msdsn.EncodeParameters{}))
				}
				// Compare after every value is read to detect reuse corrupting earlier rows.
				assert.Equal(t, tc.want, got)
				assert.Equal(t, tc.maxBuffer, cap(ti.Buffer))
			}
			assert.Equal(t, len(stream), r.rpos)
		})
	}
}

func TestValueReaders_RejectOversizedValues(t *testing.T) {
	for _, typeID := range []byte{typeVarBinary, typeBigVarBin} {
		t.Run(fmt.Sprint(typeID), func(t *testing.T) {
			metadata := []byte{4}
			stream := []byte{5, 1, 2, 3, 4, 5}
			if typeID == typeBigVarBin {
				metadata = []byte{4, 0}
				stream = []byte{5, 0, 1, 2, 3, 4, 5}
			}
			ti := readTypeInfo(bufFromBytes(metadata), typeID, nil, msdsn.EncodeParameters{})
			err := recoverErr(func() { ti.Reader(&ti, bufFromBytes(stream), nil, msdsn.EncodeParameters{}) })
			assertStreamError(t, err)
			assert.Contains(t, err.Error(), "exceeds declared type size")
			assert.Zero(t, cap(ti.Buffer))
		})
	}
}

func TestReadPLPBytes_CumulativeLimit(t *testing.T) {
	const maxLen = 16
	first := []byte("12345678")
	second := []byte("abcdefgh")
	for _, size := range []uint64{0, maxLen, _UNKNOWN_PLP_LEN} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			// Exercise the production chunk loop with a small limit rather than
			// allocating the protocol maximum of 2 GiB.
			stream := plpChunks(size, first, second)
			r := bufFromBytes(append(append([]byte{}, stream...), stream...))
			for attempt := 0; attempt < 2; attempt++ {
				assert.Equal(t, []byte("12345678abcdefgh"), readPLPBytes(r, maxLen))
			}
			_, err := r.ReadByte()
			assert.ErrorIs(t, err, io.EOF)

			r = bufFromBytes(plpChunks(size, first, second, []byte("!")))
			err = recoverErr(func() { readPLPBytes(r, maxLen) })
			assertStreamError(t, err)
			assert.Contains(t, err.Error(), "remaining LOB size")
			assert.Equal(t, 36, r.rpos, "reject the final chunk before reading its payload")
		})
	}
}

func TestReadPLPBytes_LengthsAndTruncation(t *testing.T) {
	const maxLen = 16
	cases := []struct {
		name   string
		stream []byte
	}{
		{"oversized hint", plpChunks(maxLen + 1)},
		{"oversized chunk", binary.LittleEndian.AppendUint32(
			binary.LittleEndian.AppendUint64(nil, _UNKNOWN_PLP_LEN), 0xffffffff)},
		{"truncated chunk", append(binary.LittleEndian.AppendUint32(
			binary.LittleEndian.AppendUint64(nil, 4), 4), 1, 2)},
		{"missing terminator", plpChunks(2, []byte{1, 2})[:14]},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := recoverErr(func() { readPLPBytes(bufFromBytes(tc.stream), maxLen) })
			assertStreamError(t, err)
		})
	}
	for _, size := range []uint64{0, _UNKNOWN_PLP_LEN} {
		value := readPLPBytes(bufFromBytes(plpChunks(size)), maxLen)
		assert.NotNil(t, value)
		assert.Empty(t, value)
	}
	assert.Nil(t, readPLPBytes(bufFromBytes(plpChunks(_PLP_NULL)), maxLen))
}

func TestReadPLPBytes_PacketBoundaries(t *testing.T) {
	stream := plpChunks(_UNKNOWN_PLP_LEN, []byte("abc"), []byte("defgh"))
	for chunk := 1; chunk < len(stream); chunk++ {
		t.Run(fmt.Sprint(chunk), func(t *testing.T) {
			packets, ok := frameReplyPackets(stream, chunk)
			if !ok {
				t.Fatal("failed to frame PLP data")
			}
			sess := newFuzzSession(packets)
			defer sess.buf.bufClose()
			if _, err := sess.buf.BeginRead(); err != nil {
				t.Fatal(err)
			}
			assert.Equal(t, []byte("abcdefgh"), readPLPBytes(sess.buf, 16))
			_, err := sess.buf.ReadByte()
			assert.ErrorIs(t, err, io.EOF)
		})
	}
}

func TestValueReaders_DecryptedBuffers(t *testing.T) {
	payload := []byte{1, 2, 3}
	for _, size := range []uint16{3, 0xffff} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			metadata := binary.LittleEndian.AppendUint16(nil, size)
			ti := readTypeInfo(bufFromBytes(metadata), typeBigVarBin, nil, msdsn.EncodeParameters{})
			ti.Buffer = append([]byte{}, payload...)
			r := &tdsBuffer{rbuf: payload, rsize: len(payload), final: true}
			got := ti.Reader(&ti, r, &cryptoMetadata{}, msdsn.EncodeParameters{})
			assert.Equal(t, payload, got)
		})
	}
}

func TestProcessSingleResponse_DeferredMetadataAndPLP(t *testing.T) {
	stream := colMetadataVarBinary(1, 0xffff)
	stream = append(stream, byte(tokenRow))
	stream = append(stream, plpChunks(_MAX_PLP_LEN)...)
	stream = append(stream, doneToken(tokenDone, 0)...)
	for _, chunk := range []int{0, 1, 3} {
		_, _, sawError, framed := drainSingleResponse(stream, chunk, true)
		if !framed || sawError {
			t.Fatalf("empty PLP with a maximum-length hint failed, chunk=%d", chunk)
		}
	}

	stream = colMetadataVarBinary(64, 0xfffe)
	stream = append(stream, byte(tokenRow))
	for i := 0; i < 64; i++ {
		stream = append(stream, 1, 0, byte(i))
	}
	stream = append(stream, doneToken(tokenDone, 0)...)
	_, _, sawError, framed := drainSingleResponse(stream, 3, true)
	if !framed || sawError {
		t.Fatal("small values with large metadata maxima failed")
	}

	metadata := colMetadataVarBinary(1, 0xffff)
	for _, chunkSize := range []uint32{_MAX_PLP_LEN + 1, 0xffffffff} {
		stream = append(append([]byte{}, metadata...), byte(tokenRow))
		stream = binary.LittleEndian.AppendUint64(stream, _UNKNOWN_PLP_LEN)
		stream = binary.LittleEndian.AppendUint32(stream, chunkSize)
		tokens, _, sawError, framed := drainSingleResponse(stream, 1, true)
		if !framed || !sawError {
			t.Fatal("oversized PLP chunk did not fail")
		}
		assert.Contains(t, tokens, "error:mssql.StreamError")
		assert.Contains(t, strings.Join(tokens, "\n"), "remaining LOB size")
	}
}

func TestReadPLPType_Null(t *testing.T) {
	for _, typeID := range []byte{typeBigVarBin, typeImage, typeNVarChar, typeXml, typeUdt} {
		ti := typeInfo{TypeId: typeID}
		assert.Nil(t, readPLPType(&ti, bufFromBytes(plpChunks(_PLP_NULL)), nil, msdsn.EncodeParameters{}))
	}
}

func TestReadPLPType_ChunkValues(t *testing.T) {
	payload := bytes.Repeat([]byte("a"), 70<<10)
	stream := plpChunks(uint64(len(payload)), payload[:1000], payload[1000:])
	packets, ok := frameReplyPackets(stream, 1024)
	if !ok {
		t.Fatal("failed to frame PLP value")
	}
	sess := newFuzzSession(packets)
	defer sess.buf.bufClose()
	if _, err := sess.buf.BeginRead(); err != nil {
		t.Fatal(err)
	}
	ti := typeInfo{TypeId: typeBigVarBin}
	assert.Equal(t, payload, readPLPType(&ti, sess.buf, nil, msdsn.EncodeParameters{}))
}

func TestParseRow_EmptyPLPValuesDoNotReserveHints(t *testing.T) {
	const count = 64
	metadata := colMetadataVarBinary(count, 0xffff)
	columns := parseColMetadata72(bufFromBytes(metadata[1:]), &tdsSession{})
	stream := bytes.Repeat(plpChunks(_MAX_PLP_LEN), count*2)
	r := bufFromBytes(stream)
	row := make([]interface{}, count)
	for attempt := 0; attempt < 2; attempt++ {
		if err := parseRow(context.Background(), r, &tdsSession{}, columns, row); err != nil {
			t.Fatal(err)
		}
		for i, value := range row {
			data, ok := value.([]byte)
			if !ok || data == nil || len(data) != 0 || cap(data) != 0 {
				t.Fatalf("empty PLP column %d must not retain memory from its length hint", i)
			}
		}
	}
	assert.Equal(t, len(stream), r.rpos)
}
