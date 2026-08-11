package mssql

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"

	"github.com/microsoft/go-mssqldb/msdsn"
)

// preloginTransport serves a fixed byte stream as the server side of a TDS
// connection. Writes are discarded because PRELOGIN parsing never replies.
type preloginTransport struct {
	r io.Reader
}

func (t *preloginTransport) Read(p []byte) (int, error)  { return t.r.Read(p) }
func (t *preloginTransport) Write(p []byte) (int, error) { return len(p), nil }
func (t *preloginTransport) Close() error                { return nil }

// framePrelogin wraps body in a final PRELOGIN reply packet header.
func framePrelogin(body []byte) []byte {
	packet := make([]byte, headerSize, headerSize+len(body))
	packet[0] = byte(packReply)
	packet[1] = 1 // final packet
	binary.BigEndian.PutUint16(packet[2:], uint16(headerSize+len(body)))
	return append(packet, body...)
}

// FuzzReadPrelogin feeds arbitrary PRELOGIN response bodies to the parser.
// The PRELOGIN exchange happens before authentication and before TLS is
// negotiated, and readPrelogin is called from connect() without any recover(),
// so a panic here takes down the entire client process rather than failing a
// single connection attempt. The parser must therefore only ever return a
// result or an error, never panic.
func FuzzReadPrelogin(f *testing.F) {
	seeds := [][]byte{
		// terminator only
		{preloginTERMINATOR},
		// encryption option followed by terminator
		{preloginENCRYPTION, 0, 6, 0, 1, preloginTERMINATOR, encryptNotSup},
		// zero-length encryption option followed by terminator and padding
		{preloginENCRYPTION, 0, 6, 0, 0, preloginTERMINATOR, 0},
		// version, encryption, instopt, threadid, mars
		{
			0, 0, 26, 0, 6,
			1, 0, 32, 0, 1,
			2, 0, 33, 0, 1,
			3, 0, 34, 0, 4,
			4, 0, 38, 0, 1,
			preloginTERMINATOR,
			16, 0, 7, 208, 0, 0,
			encryptNotSup,
			0,
			0, 0, 0, 0,
			0,
		},
		// option header truncated mid-record
		{0, 0, 6, 0},
		// offset past the end of the buffer
		{0, 0xFF, 0xFF, 0, 1, preloginTERMINATOR},
		// length past the end of the buffer
		{0, 0, 6, 0xFF, 0, preloginTERMINATOR},
		// no terminator at all
		{0, 0, 6, 0, 1, 0, 0},
		{},
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, body []byte) {
		// A TDS packet length is a uint16, so oversized bodies cannot be framed.
		if len(body) > int(^uint16(0))-headerSize {
			t.Skip()
		}
		transport := &preloginTransport{r: bytes.NewReader(framePrelogin(body))}
		buf := newTdsBuffer(defaultPacketSize, transport)
		// return the 64 KiB backing buffer to bufpool so each iteration reuses it
		defer buf.bufClose()
		fields, err := readPrelogin(buf)
		if err != nil {
			return
		}
		// Every returned value must be a real sub-slice of the response body.
		for token, value := range fields {
			if len(value) > len(body) {
				t.Fatalf("prelogin option %d returned %d bytes from a %d byte response", token, len(value), len(body))
			}
		}
		_, _ = interpretPreloginResponse(msdsn.Config{}, &featureExtFedAuth{FedAuthLibrary: FedAuthLibraryReserved}, fields)
	})
}
