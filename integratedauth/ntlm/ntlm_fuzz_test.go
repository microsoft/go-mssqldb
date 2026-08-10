package ntlm

import (
	"encoding/binary"
	"testing"
)

// FuzzNextBytes feeds arbitrary server-controlled challenge messages to
// (*Auth).NextBytes. The function must never panic regardless of input; a
// malformed or truncated message must result in an error, not a crash. This
// guards against out-of-bounds slice reads such as the NTLM challenge DoS.
func FuzzNextBytes(f *testing.F) {
	// Empty / truncated messages (the original crash inputs).
	f.Add([]byte{})
	f.Add([]byte("NTLMSSP\x00"))

	// A minimal well-formed CHALLENGE message (NTLMv1 path).
	valid := make([]byte, 32)
	copy(valid, []byte("NTLMSSP\x00"))
	binary.LittleEndian.PutUint32(valid[8:12], _CHALLENGE_MESSAGE)
	f.Add(valid)

	// Extended session security variant.
	ess := make([]byte, 48)
	copy(ess, []byte("NTLMSSP\x00"))
	binary.LittleEndian.PutUint32(ess[8:12], _CHALLENGE_MESSAGE)
	binary.LittleEndian.PutUint32(ess[20:24], _NEGOTIATE_EXTENDED_SESSIONSECURITY|_NEGOTIATE_TARGET_INFO)
	f.Add(ess)

	auth := &Auth{Domain: "DOMAIN", UserName: "user", Password: "pw", ChannelBinding: []byte{}}
	// authCB exercises the TLS/EPA channel-binding path, which slices the
	// parsed target-information block and has its own out-of-bounds hazards.
	authCB := &Auth{Domain: "DOMAIN", UserName: "user", Password: "pw", ChannelBinding: []byte{0x01, 0x02, 0x03, 0x04}}

	f.Fuzz(func(_ *testing.T, msg []byte) {
		// Must not panic for either channel-binding configuration. Return
		// values are ignored; we only care that the calls complete without
		// an out-of-bounds slice read.
		_, _ = auth.NextBytes(msg)
		_, _ = authCB.NextBytes(msg)
	})
}
