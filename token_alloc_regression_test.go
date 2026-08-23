package mssql

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/microsoft/go-mssqldb/msdsn"
	"github.com/stretchr/testify/assert"
)

// bufFromBytes builds a *tdsBuffer that serves stream as a single, final packet
// payload. It mirrors the helper style in types_test.go so a token parser can be
// exercised directly without a live connection.
func bufFromBytes(stream []byte) *tdsBuffer {
	buf := newTdsBuffer(uint16(1<<15), nil)
	if len(stream) > len(buf.rbuf) {
		panic(fmt.Sprintf("bufFromBytes: stream of %d bytes exceeds read buffer of %d bytes", len(stream), len(buf.rbuf)))
	}
	copy(buf.rbuf[:len(stream)], stream)
	buf.rpos = 0
	buf.rsize = len(stream)
	buf.final = true
	return buf
}

// recoverErr runs fn and returns any panic value coerced to an error. Token
// parsers signal a malformed stream by panicking (badStreamPanic) which
// processSingleResponse recovers into an error token.
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
	// err is set by the deferred recover above; a normal return leaves it nil.
	return
}

// assertStreamError fails unless err is a StreamError. The allocation guards
// must panic StreamError (not a plain error) so Conn.checkBadConn marks the
// connection bad and drops it from the pool on a malformed stream.
func assertStreamError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected a StreamError, got none")
	}
	if _, ok := err.(StreamError); !ok {
		t.Fatalf("expected StreamError, got %T: %v", err, err)
	}
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
			assertStreamError(t, err)
			assert.Contains(t, err.Error(), tc.wantSub)
		})
	}
}

// TestReadLongLenType_MalformedLengthPanics is a regression test for issue #420:
// readLongLenType used the untrusted int32 length directly as the make() size,
// so a negative length aborted with an out-of-range make() and a huge length
// preallocated gigabytes before any data arrived. The reader now rejects a
// negative length and grows the buffer with the bytes actually received, so a
// truncated huge length fails the stream cleanly instead of preallocating.
func TestReadLongLenType_MalformedLengthPanics(t *testing.T) {
	build := func(size int32, data []byte) []byte {
		// textptrsize=1, textptr(1 byte), timestamp(8 bytes), size(int32), data
		b := []byte{0x01, 0x00, 0, 0, 0, 0, 0, 0, 0, 0}
		var s [4]byte
		binary.LittleEndian.PutUint32(s[:], uint32(size))
		b = append(b, s[:]...)
		return append(b, data...)
	}

	cases := map[string]struct {
		stream  []byte
		wantSub string
	}{
		"negative length": {
			stream:  build(-2, nil),
			wantSub: "maximum LOB size",
		},
		"truncated huge length": {
			// Advertise ~2 GiB but supply only a few bytes: the reader must fail
			// the stream instead of preallocating gigabytes (issue #420).
			stream:  build(0x7FFFFFFF, []byte{0x01, 0x02, 0x03}),
			wantSub: "failed",
		},
	}
	for name, tc := range cases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			ti := typeInfo{TypeId: typeImage}
			err := recoverErr(func() {
				readLongLenType(&ti, bufFromBytes(tc.stream), nil, msdsn.EncodeParameters{})
			})
			assertStreamError(t, err)
			assert.Contains(t, err.Error(), tc.wantSub)
		})
	}
}

// TestReadVariantType_UnderflowPanics is a regression test for issue #420:
// readVariantTypeWithEncoding allocated make([]byte, size-2-propbytes) which
// underflowed to a huge value when propbytes exceeded size-2, and also allowed
// an implausibly large size (a sql_variant is capped at ~8 KB on the wire).
func TestReadVariantType_UnderflowPanics(t *testing.T) {
	build := func(size int32, vartype byte, propbytes byte) []byte {
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, uint32(size))
		return append(b, vartype, propbytes)
	}

	cases := map[string][]byte{
		// size=3, propbytes=250 -> 3-2-250 = -249 (underflow)
		"underflow": build(3, typeGuid, 250),
		// size just past the sql_variant ceiling -> a bounded but multi-KB+
		// datalen that must still be rejected, not allocated.
		"oversize": build(_MAX_VARIANT_LEN+3, typeGuid, 0),
	}
	for name, stream := range cases {
		stream := stream
		t.Run(name, func(t *testing.T) {
			ti := typeInfo{}
			err := recoverErr(func() {
				readVariantTypeWithEncoding(&ti, bufFromBytes(stream), nil, msdsn.EncodeParameters{})
			})
			assertStreamError(t, err)
			assert.Contains(t, err.Error(), "sql_variant data length")
		})
	}
}

// TestParseColMetadata72_BogusCountPanics is a regression test for issue #420:
// parseColMetadata72 allocated make([]columnStruct, count) directly from the
// attacker-controlled uint16 column count. Even within the uint16 ceiling a
// count of 0xFFFE preallocates many MiB of columnStruct backing array. The
// reader now grows the slice as each column is actually parsed, so a bogus
// count with no backing data fails the stream as a StreamError (EOF while
// reading the first column) instead of preallocating.
func TestParseColMetadata72_BogusCountPanics(t *testing.T) {
	// A bogus-huge column count (0xFFFE, not the 0xFFFF "no metadata" sentinel)
	// with no column data behind it. Parsing the first column must run past the
	// end of the stream and panic a StreamError rather than allocating a giant
	// slice up front.
	stream := []byte{0xFE, 0xFF}
	err := recoverErr(func() {
		parseColMetadata72(bufFromBytes(stream), &tdsSession{})
	})
	assertStreamError(t, err)
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
			_, _, sawError, framed := drainSingleResponse(stream, 0, false)
			if !framed {
				t.Fatalf("failed to frame stream")
			}
			if !sawError {
				t.Fatalf("expected an error token for malformed stream")
			}
		})
	}
}
