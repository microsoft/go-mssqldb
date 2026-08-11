package mssql

import (
	"testing"
)

// FuzzParseDAC feeds arbitrary SQL Server Browser (UDP port 1434) responses to
// parseDAC. The reply is fully attacker-controllable — UDP is connectionless,
// so anything able to answer before the real Browser service wins the race —
// and it is parsed during connect() with no recover() anywhere in the call
// stack. The parser must therefore never panic, whatever bytes arrive.
func FuzzParseDAC(f *testing.F) {
	// A spec-shaped CLNT_UCAST_DAC reply: SVR_RESP (0x05), 2-byte length,
	// protocol version byte, then the little-endian TCP port at offset 4.
	f.Add([]byte{0x05, 0x03, 0x00, 0x01, 0x9A, 0x05}, "SQLEXPRESS")

	// Truncated, empty and non-SVR_RESP variants.
	f.Add([]byte{}, "SQLEXPRESS")
	f.Add([]byte{0x05}, "SQLEXPRESS")
	f.Add([]byte{0x05, 0x03, 0x00, 0x01, 0x9A}, "SQLEXPRESS")
	f.Add([]byte{0x04, 0x03, 0x00, 0x01, 0x9A, 0x05}, "SQLEXPRESS")

	// Longer-than-expected reply and an empty instance name.
	f.Add([]byte{0x05, 0x03, 0x00, 0x01, 0x9A, 0x05, 0x00, 0x00}, "SQLEXPRESS")
	f.Add([]byte{0x05, 0x03, 0x00, 0x01, 0x9A, 0x05}, "")

	f.Fuzz(func(_ *testing.T, msg []byte, instance string) {
		// Return value is ignored; the property under test is that parsing an
		// untrusted Browser response completes without a panic.
		_ = parseDAC(msg, instance)
	})
}

// FuzzParseInstances feeds arbitrary SQL Server Browser CLNT_UCAST_EX /
// CLNT_UCAST_INST responses to parseInstances, which runs on the same
// untrusted UDP path as parseDAC.
func FuzzParseInstances(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x05, 0x00, 0x00})
	f.Add([]byte("\x05\x2a\x00InstanceName;MSSQLSERVER;tcp;1433;;"))
	f.Add([]byte("\x05\x00\x00InstanceName;"))
	f.Add([]byte("\x05\x00\x00;;;;;;"))

	f.Fuzz(func(_ *testing.T, msg []byte) {
		_ = parseInstances(msg)
	})
}
