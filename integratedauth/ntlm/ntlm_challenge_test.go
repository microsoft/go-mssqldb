package ntlm

import (
	"encoding/binary"
	"testing"
)

// newChallengeMessage builds a minimal, well-formed NTLM Type 2 (CHALLENGE)
// message of the requested total size (>= 32). flags is written to bytes[20:24].
func newChallengeMessage(size int, flags uint32) []byte {
	if size < minChallengeMessageSize {
		size = minChallengeMessageSize
	}
	msg := make([]byte, size)
	copy(msg, []byte("NTLMSSP\x00"))
	binary.LittleEndian.PutUint32(msg[8:12], _CHALLENGE_MESSAGE)
	binary.LittleEndian.PutUint32(msg[20:24], flags)
	return msg
}

// TestNextBytesShortMessage verifies that a truncated challenge message is
// rejected with an error instead of panicking with an out-of-bounds slice
// read. Regression test for the NTLM challenge DoS.
func TestNextBytesShortMessage(t *testing.T) {
	auth := &Auth{Domain: "DOMAIN", UserName: "user", Password: "pw", ChannelBinding: []byte{}}

	for size := 0; size < minChallengeMessageSize; size++ {
		bytes := make([]byte, size)
		// Fill with a valid signature/type where possible so the length
		// check is the only thing preventing a slice panic.
		copy(bytes, []byte("NTLMSSP\x00"))
		if size >= 12 {
			binary.LittleEndian.PutUint32(bytes[8:12], _CHALLENGE_MESSAGE)
		}

		out, err := auth.NextBytes(bytes)
		if err != errorNTLM {
			t.Fatalf("size=%d: expected errorNTLM, got err=%v", size, err)
		}
		if out != nil {
			t.Fatalf("size=%d: expected nil output on error, got %v", size, out)
		}
	}
}

// TestNextBytesMinimumSizeNoPanic ensures a 32-byte challenge (the minimum
// valid size) is processed without panicking for both the NTLMv1 and the
// extended-session-security code paths.
func TestNextBytesMinimumSizeNoPanic(t *testing.T) {
	auth := &Auth{Domain: "DOMAIN", UserName: "user", Password: "pw", ChannelBinding: []byte{}}

	// NTLMv1 path (no extended session security flag).
	if _, err := auth.NextBytes(newChallengeMessage(minChallengeMessageSize, 0)); err != nil {
		t.Fatalf("v1 path returned error: %v", err)
	}

	// Extended session security path without target info.
	flags := uint32(_NEGOTIATE_EXTENDED_SESSIONSECURITY)
	if _, err := auth.NextBytes(newChallengeMessage(minChallengeMessageSize, flags)); err != nil {
		t.Fatalf("extended session security path returned error: %v", err)
	}
}

// TestNextBytesTargetInfoShortMessage exercises the NTLMv2 target-info path
// (extended session security + target info flags), where getNTLMv2TargetInfoFields
// reads the target-information header at offsets 42-48. A challenge that is long
// enough to enter NextBytes (>= 32 bytes) but shorter than 48 bytes must return
// an error without panicking. Regression test for the target-info bounds guard.
func TestNextBytesTargetInfoShortMessage(t *testing.T) {
	auth := &Auth{Domain: "DOMAIN", UserName: "user", Password: "pw", ChannelBinding: []byte{}}
	flags := uint32(_NEGOTIATE_EXTENDED_SESSIONSECURITY | _NEGOTIATE_TARGET_INFO)

	for size := minChallengeMessageSize; size < 48; size++ {
		msg := make([]byte, size)
		copy(msg, []byte("NTLMSSP\x00"))
		binary.LittleEndian.PutUint32(msg[8:12], _CHALLENGE_MESSAGE)
		binary.LittleEndian.PutUint32(msg[20:24], flags)

		out, err := auth.NextBytes(msg)
		if err == nil {
			t.Fatalf("size=%d: expected error for truncated target-info message, got nil", size)
		}
		if out != nil {
			t.Fatalf("size=%d: expected nil output on error, got %v", size, out)
		}
	}
}
