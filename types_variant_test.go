package mssql

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/microsoft/go-mssqldb/msdsn"
	"github.com/stretchr/testify/assert"
)

func variantStream(typeID byte, properties, data []byte) []byte {
	stream := binary.LittleEndian.AppendUint32(nil, uint32(2+len(properties)+len(data)))
	stream = append(stream, typeID, byte(len(properties)))
	stream = append(stream, properties...)
	return append(stream, data...)
}

func TestReadVariantType_RejectsInvalidWidths(t *testing.T) {
	fixed := []struct {
		typeID byte
		width  int
	}{
		{typeGuid, 16}, {typeBit, 1}, {typeInt1, 1}, {typeInt2, 2},
		{typeInt4, 4}, {typeInt8, 8}, {typeDateTime, 8}, {typeDateTim4, 4},
		{typeFlt4, 4}, {typeFlt8, 8}, {typeMoney4, 4}, {typeMoney, 8}, {typeDateN, 3},
	}
	for _, tc := range fixed {
		widths := []int{tc.width - 1, tc.width + 1}
		if tc.width > 1 {
			widths = append(widths, 0)
		}
		for _, width := range widths {
			t.Run(fmt.Sprintf("%02x/%d", tc.typeID, width), func(t *testing.T) {
				stream := variantStream(tc.typeID, nil, make([]byte, width))
				checkInvalidVariantBoundary(t, stream)
			})
		}
		t.Run(fmt.Sprintf("%02x/unexpected property", tc.typeID), func(t *testing.T) {
			checkInvalidVariantBoundary(t, variantStream(tc.typeID, []byte{0}, make([]byte, tc.width)))
		})
	}
	// Exact review repro: a one-byte bigint payload followed by another value.
	t.Run("bigint one byte", func(t *testing.T) {
		checkInvalidVariantBoundary(t, []byte{3, 0, 0, 0, typeInt8, 0, 0x12})
	})
	for _, typeID := range []byte{typeDecimalN, typeNumericN} {
		for _, width := range []int{0, 1, 4, 6, 18, 21} {
			t.Run(fmt.Sprintf("%02x/%d", typeID, width), func(t *testing.T) {
				checkInvalidVariantBoundary(t, variantStream(typeID, []byte{38, 0}, make([]byte, width)))
			})
		}
	}
	for _, typeID := range []byte{typeTimeN, typeDateTime2N, typeDateTimeOffsetN} {
		for _, scale := range []byte{0, 2, 3, 4, 5, 7} {
			width := []int{3, 3, 3, 4, 4, 5, 5, 5}[scale]
			switch typeID {
			case typeDateTime2N:
				width += 3
			case typeDateTimeOffsetN:
				width += 5
			}
			for _, length := range []int{width - 1, width + 1} {
				t.Run(fmt.Sprintf("%02x/scale%d/%d", typeID, scale, length), func(t *testing.T) {
					checkInvalidVariantBoundary(t, variantStream(typeID, []byte{scale}, make([]byte, length)))
				})
			}
		}
		for _, scale := range []byte{8, 255} {
			t.Run(fmt.Sprintf("%02x/scale%d", typeID, scale), func(t *testing.T) {
				checkInvalidVariantBoundary(t, variantStream(typeID, []byte{scale}, make([]byte, 10)))
			})
		}
	}
}

func checkInvalidVariantBoundary(t *testing.T, stream []byte) {
	t.Helper()
	sentinel := []byte{0xde, 0xad, 0xbe, 0xef, 1, 2, 3, 4, 0xa5}
	r := bufFromBytes(append(append([]byte{}, stream...), sentinel...))
	defer r.bufClose()
	err := recoverErr(func() {
		readVariantTypeWithEncoding(&typeInfo{}, r, nil, msdsn.EncodeParameters{})
	})
	if r.rpos > len(stream) {
		t.Fatalf("sql_variant consumed %d bytes past its boundary", r.rpos-len(stream))
	}
	assertStreamError(t, err)
	assert.Equal(t, sentinel, r.rbuf[len(stream):r.rsize])
	conn := &Conn{connectionGood: true}
	assert.Equal(t, err, conn.checkBadConn(context.Background(), err, false))
	assert.False(t, conn.connectionGood)
}

func TestReadVariantType_PropertyWidths(t *testing.T) {
	cases := []struct {
		typeID     byte
		properties []byte
		data       []byte
	}{
		{typeInt8, nil, make([]byte, 8)},
		{typeTimeN, []byte{7}, make([]byte, 5)},
		{typeDateTime2N, []byte{7}, make([]byte, 8)},
		{typeDateTimeOffsetN, []byte{7}, make([]byte, 10)},
		{typeDecimalN, []byte{9, 0}, []byte{1, 0, 0, 0, 0}},
		{typeNumericN, []byte{9, 0}, []byte{1, 0, 0, 0, 0}},
		{typeBigVarBin, []byte{8, 0}, []byte{1, 2}},
		{typeBigBinary, []byte{2, 0}, []byte{1, 2}},
		{typeBigVarChar, []byte{9, 4, 0, 0, 0, 8, 0}, []byte("hi")},
		{typeBigChar, []byte{9, 4, 0, 0, 0, 2, 0}, []byte("hi")},
		{typeNVarChar, []byte{9, 4, 0, 0, 0, 8, 0}, ucs2("hi")},
		{typeNChar, []byte{9, 4, 0, 0, 0, 4, 0}, ucs2("hi")},
	}
	for _, tc := range cases {
		for _, size := range []int{0, len(tc.properties) + 1} {
			if size == len(tc.properties) {
				continue
			}
			t.Run(fmt.Sprintf("%02x/%d", tc.typeID, size), func(t *testing.T) {
				props := make([]byte, size)
				copy(props, tc.properties)
				checkInvalidVariantBoundary(t, variantStream(tc.typeID, props, tc.data))
			})
		}
	}
}

func TestReadVariantType_UnicodeLengths(t *testing.T) {
	properties := []byte{9, 4, 0, 0, 0, 8, 0}
	for _, typeID := range []byte{typeNVarChar, typeNChar} {
		for length := 0; length <= 6; length++ {
			t.Run(fmt.Sprintf("%02x/%d", typeID, length), func(t *testing.T) {
				payload := ucs2("abc")[:length]
				value := variantStream(typeID, properties, payload)
				r := bufFromBytes(append(bytes.Repeat(value, 2), 0xa5))
				defer r.bufClose()
				ti := typeInfo{TypeId: typeVariant}
				conn := &Conn{connectionGood: true}
				for attempt := 0; attempt < 2; attempt++ {
					start := r.rpos
					var got interface{}
					err := recoverErr(func() {
						got = readVariantTypeWithEncoding(&ti, r, nil, msdsn.EncodeParameters{})
					})
					if length%2 != 0 {
						assertStreamError(t, err)
						assert.Contains(t, err.Error(), "UTF-16")
						assert.Equal(t, start+6, r.rpos, "reject before reading properties or payload")
						assert.Equal(t, err, conn.checkBadConn(context.Background(), err, false))
						assert.False(t, conn.connectionGood)
						return
					}
					assert.NoError(t, err)
					assert.Equal(t, "abc"[:length/2], got)
					assert.Equal(t, start+len(value), r.rpos)
					assert.Nil(t, conn.checkBadConn(context.Background(), err, false))
					assert.True(t, conn.connectionGood)
				}
				assert.Equal(t, byte(0xa5), r.byte())
			})
		}
	}
}

func TestProcessSingleResponse_VariantUnicodeLengths(t *testing.T) {
	properties := []byte{9, 4, 0, 0, 0, 8, 0}
	for _, typeID := range []byte{typeNVarChar, typeNChar} {
		for _, chunk := range []int{0, 1, 3} {
			t.Run(fmt.Sprintf("%02x/%d", typeID, chunk), func(t *testing.T) {
				value := variantStream(typeID, properties, []byte{'a'})
				tokens, _, sawError, framed := drainSingleResponse(variantResponse(value), chunk, true)
				if !framed || !sawError {
					t.Fatal("expected malformed Unicode variant to fail")
				}
				assert.Contains(t, tokens, "error:mssql.StreamError")
				for _, tok := range tokens {
					if strings.HasPrefix(tok, "row") {
						t.Fatalf("malformed Unicode variant emitted a row: %s", tok)
					}
				}
				value = variantStream(typeID, properties, ucs2("hi"))
				tokens, _, sawError, framed = drainSingleResponse(variantResponse(value), chunk, true)
				if !framed || sawError {
					t.Fatalf("valid Unicode variant failed: %v", tokens)
				}
				assert.Contains(t, tokens, "row[hi 123]")
			})
		}
	}
}

func TestReadVariantType_ValuesAndPacketBoundaries(t *testing.T) {
	guid := []byte{0xff, 0x19, 0x96, 0x6f, 0x86, 0x8b, 0x11, 0xd0, 0xb4, 0x2d, 0, 0xc0, 0x4f, 0xc9, 0x64, 0xff}
	type variantCase struct {
		typeID     byte
		properties []byte
		data       []byte
		want       interface{}
	}
	cases := []variantCase{
		{typeGuid, nil, guid, guid},
		{typeBit, nil, []byte{1}, true},
		{typeInt1, nil, []byte{42}, int64(42)},
		{typeInt2, nil, []byte{0xfe, 0xff}, int64(-2)},
		{typeInt4, nil, []byte{0xfd, 0xff, 0xff, 0xff}, int64(-3)},
		{typeInt8, nil, []byte{0xfc, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, int64(-4)},
		{typeDateTime, nil, make([]byte, 8), time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)},
		{typeDateTim4, nil, make([]byte, 4), time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)},
		{typeFlt4, nil, binary.LittleEndian.AppendUint32(nil, math.Float32bits(0.125)), float64(0.125)},
		{typeFlt8, nil, binary.LittleEndian.AppendUint64(nil, math.Float64bits(0.125)), float64(0.125)},
		{typeMoney4, nil, binary.LittleEndian.AppendUint32(nil, 12345), []byte("1.2345")},
		{typeMoney, nil, []byte{0, 0, 0, 0, 0x39, 0x30, 0, 0}, []byte("1.2345")},
		{typeDateN, nil, []byte{0, 0, 0}, time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)},
		{typeBigVarBin, []byte{8, 0}, []byte{1, 2}, []byte{1, 2}},
		{typeBigBinary, []byte{2, 0}, []byte{1, 2}, []byte{1, 2}},
		{typeBigVarBin, []byte{8, 0}, []byte{}, []byte{}},
		{typeBigVarChar, []byte{9, 4, 0, 0, 0, 8, 0}, []byte("hi"), "hi"},
		{typeBigChar, []byte{9, 4, 0, 0, 0, 2, 0}, []byte("hi"), "hi"},
		{typeNVarChar, []byte{9, 4, 0, 0, 0, 8, 0}, ucs2("hi"), "hi"},
		{typeNChar, []byte{9, 4, 0, 0, 0, 4, 0}, ucs2("hi"), "hi"},
		{typeBigVarChar, []byte{9, 4, 0, 0, 0, 8, 0}, []byte{}, ""},
		{typeNVarChar, []byte{9, 4, 0, 0, 0, 8, 0}, []byte{}, ""},
	}
	for _, typeID := range []byte{typeDecimalN, typeNumericN} {
		for _, width := range []int{5, 9, 13, 17} {
			data := make([]byte, width)
			data[1] = 5
			cases = append(cases, variantCase{typeID, []byte{38, 1}, data, []byte("-0.5")})
		}
	}
	for scale, width := range []int{3, 3, 3, 4, 4, 5, 5, 5} {
		cases = append(cases,
			variantCase{typeTimeN, []byte{byte(scale)}, make([]byte, width), time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)},
			variantCase{typeDateTime2N, []byte{byte(scale)}, make([]byte, width+3), time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)},
			variantCase{typeDateTimeOffsetN, []byte{byte(scale)}, make([]byte, width+5), time.Date(1, 1, 1, 0, 0, 0, 0, time.FixedZone("", 0))},
		)
	}
	for i, tc := range cases {
		for _, chunk := range []int{0, 1, 3} {
			t.Run(fmt.Sprintf("%02x/%d/%d", tc.typeID, i, chunk), func(t *testing.T) {
				value := variantStream(tc.typeID, tc.properties, tc.data)
				stream := append(bytes.Repeat(value, 2), 0xa5)
				packets, ok := frameReplyPackets(stream, chunk)
				if !ok {
					t.Fatal("failed to frame variant")
				}
				if chunk > 0 {
					packetCount := (len(stream) + chunk - 1) / chunk
					if len(packets) != len(stream)+packetCount*headerSize {
						t.Fatal("variant fixture did not use the requested packet boundaries")
					}
				}
				sess := newFuzzSession(packets)
				defer sess.buf.bufClose()
				if _, err := sess.buf.BeginRead(); err != nil {
					t.Fatal(err)
				}
				ti := typeInfo{TypeId: typeVariant}
				for attempt := 0; attempt < 2; attempt++ {
					assert.Equal(t, tc.want, readVariantTypeWithEncoding(&ti, sess.buf, nil, msdsn.EncodeParameters{Timezone: time.UTC}))
				}
				assert.Equal(t, byte(0xa5), sess.buf.byte())
			})
		}
	}
}

func TestReadVariantType_HeaderAndTruncation(t *testing.T) {
	for _, stream := range [][]byte{
		{1, 0, 0, 0, typeInt8},
		{0xff, 0xff, 0xff, 0xff},
		{0, 0, 0, 0x80},
		variantStream(0xff, nil, []byte{0}),
	} {
		t.Run(fmt.Sprintf("%x", stream), func(t *testing.T) {
			checkInvalidVariantBoundary(t, stream)
		})
	}
	value := variantStream(typeInt8, nil, make([]byte, 8))
	for end := 0; end < len(value); end++ {
		t.Run(fmt.Sprintf("truncated/%d", end), func(t *testing.T) {
			r := bufFromBytes(value[:end])
			defer r.bufClose()
			err := recoverErr(func() {
				readVariantTypeWithEncoding(&typeInfo{}, r, nil, msdsn.EncodeParameters{})
			})
			assertStreamError(t, err)
		})
	}
	r := bufFromBytes([]byte{0, 0, 0, 0, 0xa5})
	defer r.bufClose()
	assert.Nil(t, readVariantTypeWithEncoding(&typeInfo{}, r, nil, msdsn.EncodeParameters{}))
	assert.Equal(t, byte(0xa5), r.byte())
}

func variantResponse(value []byte) []byte {
	stream := []byte{
		byte(tokenColMetadata), 2, 0,
		0, 0, 0, 0, 0, 0, typeVariant,
	}
	stream = binary.LittleEndian.AppendUint32(stream, _MAX_VARIANT_LEN)
	stream = append(stream, 0, 0, 0, 0, 0, 0, 0, typeInt4, 0, byte(tokenRow))
	stream = append(stream, value...)
	stream = binary.LittleEndian.AppendUint32(stream, 123)
	return append(stream, doneToken(tokenDone, 0)...)
}

func TestProcessSingleResponse_VariantBoundary(t *testing.T) {
	valid := variantStream(typeInt8, nil, binary.LittleEndian.AppendUint64(nil, 42))
	for _, chunk := range []int{0, 1, 3} {
		tokens, _, sawError, framed := drainSingleResponse(variantResponse(valid), chunk, true)
		if !framed || sawError {
			t.Fatalf("valid variant response failed: %v", tokens)
		}
		assert.Contains(t, tokens, "row[42 123]")

		invalid := variantStream(typeInt8, nil, []byte{0x12})
		tokens, _, sawError, framed = drainSingleResponse(variantResponse(invalid), chunk, true)
		if !framed || !sawError {
			t.Fatal("malformed variant response did not fail")
		}
		assert.Contains(t, tokens, "error:mssql.StreamError")
		for _, tok := range tokens {
			if strings.HasPrefix(tok, "row") {
				t.Fatalf("malformed variant emitted a row before failing: %s", tok)
			}
		}
	}
}

func TestReadVariantType_ReturnValueBoundary(t *testing.T) {
	header := []byte{0, 0, 0, 1, 0, 0, 0, 0, 0, 0, typeVariant}
	header = binary.LittleEndian.AppendUint32(header, _MAX_VARIANT_LEN)
	for _, width := range []int{1, 8} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			value := variantStream(typeInt8, nil, make([]byte, width))
			stream := append(append([]byte{}, header...), value...)
			boundary := len(stream)
			stream = append(stream, 0xa5, 1, 2, 3, 4, 5, 6, 7)
			r := bufFromBytes(stream)
			defer r.bufClose()
			err := recoverErr(func() {
				got := parseReturnValue(r, &tdsSession{})
				assert.Equal(t, int64(0), got.Value)
			})
			if width == 1 {
				assertStreamError(t, err)
				assert.LessOrEqual(t, r.rpos, boundary)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, boundary, r.rpos)
				assert.Equal(t, byte(0xa5), r.byte())
			}
		})
	}
}

func TestReadVariantType_NbcRowBoundary(t *testing.T) {
	for _, width := range []int{1, 8} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			value := variantStream(typeInt8, nil, make([]byte, width))
			metadata := bufFromBytes(variantResponse(value)[1:])
			defer metadata.bufClose()
			columns := parseColMetadata72(metadata, &tdsSession{})
			stream := append([]byte{0}, value...)
			boundary := len(stream)
			stream = binary.LittleEndian.AppendUint32(stream, 123)
			stream = append(stream, 0xa5, 1, 2, 3, 4, 5, 6, 7)
			r := bufFromBytes(stream)
			defer r.bufClose()
			row := make([]interface{}, 2)
			err := recoverErr(func() {
				if err := parseNbcRow(context.Background(), r, &tdsSession{}, columns, row); err != nil {
					t.Fatal(err)
				}
			})
			if width == 1 {
				assertStreamError(t, err)
				assert.LessOrEqual(t, r.rpos, boundary)
				assert.Equal(t, []interface{}{nil, nil}, row)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, []interface{}{int64(0), int64(123)}, row)
				assert.Equal(t, byte(0xa5), r.byte())
			}
		})
	}
}
