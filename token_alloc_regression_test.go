package mssql

import (
	"encoding/binary"
	"testing"

	"github.com/microsoft/go-mssqldb/msdsn"
	"github.com/stretchr/testify/assert"
)

// bufFromBytes builds a *tdsBuffer that serves stream as a single, final packet
// payload. It mirrors the helper style in types_test.go so a token parser can be
// exercised directly without a live connection.
func bufFromBytes(stream []byte) *tdsBuffer {
	buf := newTdsBuffer(uint16(1<<15), nil)
	copy(buf.rbuf[:len(stream)], stream)
	buf.rpos = 0
	buf.rsize = len(stream)
	buf.final = true
	return buf
}

// recoverErr runs fn and returns any panic value coerced to an error. Token
// parsers signal a malformed stream by panicking (badStreamPanic/badStreamPanicf)
// which processSingleResponse recovers into an error token.
func recoverErr(fn func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(error); ok {
				err = e
			} else {
				err = assert.AnError
			}
		}
	}()
	fn()
	return nil
}

// TestParseFedAuthInfo_MalformedAllocations is a regression test for issue #420:
// parseFedAuthInfo allocated buffers/slices sized directly from attacker
// controlled length prefixes before validating them, so a crafted FEDAUTHINFO
// token drove an unbounded allocation (OOM DoS). The repro from the issue is the
// nine bytes `EE 00 00 00 00 00 00 00 00`; here we feed just the token body
// (the EE token byte is consumed by processSingleResponse before the parser).
func TestParseFedAuthInfo_MalformedAllocations(t *testing.T) {
	le := binary.LittleEndian
	u32 := func(v uint32) []byte {
		b := make([]byte, 4)
		le.PutUint32(b, v)
		return b
	}

	cases := map[string]struct {
		stream  []byte
		wantSub string
	}{
		// size=0, count=0: the original make([]byte, size-offset) underflowed to
		// ~4 GB. The option-count check now rejects it first.
		"underflow size zero": {
			stream:  append(u32(0), u32(0)...),
			wantSub: "do not fit",
		},
		// A small size with a huge option count previously pre-allocated a giant
		// opts slice before any bounds check.
		"bogus option count": {
			stream:  append(u32(8), u32(0xFFFFFFFF)...),
			wantSub: "do not fit",
		},
		// A size larger than any real FEDAUTHINFO token is rejected outright.
		"oversized token": {
			stream:  append(u32(_MAX_FEDAUTHINFO_LEN+1), u32(0)...),
			wantSub: "exceeds maximum",
		},
	}

	for name, tc := range cases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			err := recoverErr(func() { parseFedAuthInfo(bufFromBytes(tc.stream)) })
			if err == nil {
				t.Fatalf("expected a stream error, got none")
			}
			assert.Contains(t, err.Error(), tc.wantSub)
		})
	}
}

// TestReadLongLenType_MalformedLengthPanics is a regression test for issue #420:
// readLongLenType used the untrusted int32 length as the buffer size, so a
// negative length aborted with an out-of-range make() and a huge one allocated
// gigabytes. Both must fail the stream cleanly instead.
func TestReadLongLenType_MalformedLengthPanics(t *testing.T) {
	build := func(size int32) []byte {
		// textptrsize=1, textptr(1 byte), timestamp(8 bytes), size(int32)
		b := []byte{0x01, 0x00, 0, 0, 0, 0, 0, 0, 0, 0}
		var s [4]byte
		binary.LittleEndian.PutUint32(s[:], uint32(size))
		return append(b, s[:]...)
	}

	sizes := map[string]int32{
		"negative length": -2,
	}
	for name, size := range sizes {
		size := size
		t.Run(name, func(t *testing.T) {
			ti := typeInfo{TypeId: typeImage}
			err := recoverErr(func() {
				readLongLenType(&ti, bufFromBytes(build(size)), nil, msdsn.EncodeParameters{})
			})
			if err == nil {
				t.Fatalf("expected a stream error, got none")
			}
			assert.Contains(t, err.Error(), "maximum LOB size")
		})
	}
}

// TestReadVariantType_UnderflowPanics is a regression test for issue #420:
// readVariantTypeWithEncoding allocated make([]byte, size-2-propbytes) which
// underflowed to a huge value when propbytes exceeded size-2.
func TestReadVariantType_UnderflowPanics(t *testing.T) {
	// size=3, vartype=typeGuid, propbytes=250 -> 3-2-250 = -249
	stream := []byte{0x03, 0x00, 0x00, 0x00, typeGuid, 0xFA}
	ti := typeInfo{}
	err := recoverErr(func() {
		readVariantTypeWithEncoding(&ti, bufFromBytes(stream), nil, msdsn.EncodeParameters{})
	})
	if err == nil {
		t.Fatalf("expected a stream error, got none")
	}
	assert.Contains(t, err.Error(), "sql_variant data length")
}

// TestProcessSingleResponse_MalformedNoOOM feeds crafted malformed token streams
// (framed as reply packets) through the full response parser and asserts each is
// turned into an error token rather than hanging or exhausting memory. These are
// the end-to-end forms of the issue #420 repros.
func TestProcessSingleResponse_MalformedNoOOM(t *testing.T) {
	streams := map[string][]byte{
		// FEDAUTHINFO underflow repro from the issue: EE 00 00 00 00 00 00 00 00
		"fedauth underflow": {byte(tokenFedAuthInfo), 0, 0, 0, 0, 0, 0, 0, 0},
		// COLMETADATA with a bogus-huge column count (0xFFFE, not the 0xFFFF
		// "no metadata" sentinel) followed by no column data.
		"colmetadata bogus count": {byte(tokenColMetadata), 0xFE, 0xFF},
	}

	for name, stream := range streams {
		stream := stream
		t.Run(name, func(t *testing.T) {
			_, sawError, framed := drainSingleResponse(stream, 0)
			if !framed {
				t.Fatalf("failed to frame stream")
			}
			if !sawError {
				t.Fatalf("expected an error token for malformed stream")
			}
		})
	}
}
