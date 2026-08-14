package mssql

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestParseLoginAck_MalformedSizePanics is a regression test for issue #427:
// parseLoginAck read a uint16 size, allocated a buffer, and then unconditionally
// indexed into it (buf[0], buf[1:5], buf[5], buf[size-4:], and a slice sized by
// an embedded prog-name length). A short or inconsistent token drove an index /
// slice out-of-range runtime panic, which processSingleResponse's recover() does
// not catch, crashing the driver. The parser must now reject such tokens with a
// recoverable StreamError (badStreamPanic) instead.
func TestParseLoginAck_MalformedSizePanics(t *testing.T) {
	// buildShort frames a LOGINACK body of the given size with `size` zero bytes
	// behind the uint16 length prefix, so ReadFull succeeds but the fixed layout
	// (>=10 bytes) is not satisfied.
	buildShort := func(size int) []byte {
		stream := []byte{byte(size), byte(size >> 8)}
		stream = append(stream, make([]byte, size)...)
		return stream
	}

	cases := map[string]struct {
		stream  []byte
		wantSub string
	}{
		// size=0: buf[0] previously panicked with index out of range.
		"size zero": {stream: buildShort(0), wantSub: "too short"},
		// size=3: buf[1:5] (TDS version) previously sliced out of range.
		"size three": {stream: buildShort(3), wantSub: "too short"},
		// size=5: buf[5] (prognamelen) previously indexed out of range.
		"size five": {stream: buildShort(5), wantSub: "too short"},
		// size=9: one byte short of the 10-byte minimum fixed layout.
		"size nine": {stream: buildShort(9), wantSub: "too short"},
		// size=10 but prognamelen=0xFF: the prog name slice (1+4+1 .. +255*2)
		// previously ran past the end of buf; byte arithmetic on prognamelen*2
		// could also overflow. int arithmetic now rejects it.
		"progname overrun": {
			// interface(1)+TDSVersion(4)+prognamelen(0xFF)+4 trailing bytes = 10.
			stream:  []byte{0x0A, 0x00, 0x01, 0, 0, 0, 0, 0xFF, 0, 0, 0, 0},
			wantSub: "exceeds token size",
		},
	}

	for name, tc := range cases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			err := recoverErr(func() { parseLoginAck(bufFromBytes(tc.stream)) })
			if err == nil {
				t.Fatalf("expected a stream error, got none")
			}
			assert.Contains(t, err.Error(), tc.wantSub)
		})
	}
}

// TestParseLoginAck_WellFormed confirms the guard does not reject a valid
// LOGINACK token.
func TestParseLoginAck_WellFormed(t *testing.T) {
	body := loginAckBody("go-mssqldb", -1)
	ack := parseLoginAck(bufFromBytes(body))
	assert.Equal(t, "go-mssqldb", ack.ProgName)
}
