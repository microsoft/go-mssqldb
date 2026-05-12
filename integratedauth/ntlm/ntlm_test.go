package ntlm

import (
	"encoding/hex"
	"testing"
	"time"

	"github.com/microsoft/go-mssqldb/integratedauth"
	"github.com/microsoft/go-mssqldb/msdsn"
	"github.com/stretchr/testify/assert"
)

func TestLMOWFv1(t *testing.T) {
	hash := lmHash("Password")
	val := [21]byte{
		0xe5, 0x2c, 0xac, 0x67, 0x41, 0x9a, 0x9a, 0x22,
		0x4a, 0x3b, 0x10, 0x8f, 0x3f, 0xa6, 0xcb, 0x6d,
		0, 0, 0, 0, 0,
	}
	assert.Equal(t, val, hash, "LM hash mismatch")
}

func TestNTLMOWFv1(t *testing.T) {
	hash := ntlmHash("Password")
	val := [21]byte{
		0xa4, 0xf4, 0x9c, 0x40, 0x65, 0x10, 0xbd, 0xca, 0xb6, 0x82, 0x4e, 0xe7, 0xc3, 0x0f, 0xd8, 0x52,
		0, 0, 0, 0, 0,
	}
	assert.Equal(t, val, hash, "NTLM hash mismatch")
}

func TestNTLMv1Response(t *testing.T) {
	challenge := [8]byte{
		0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef,
	}
	nt := ntResponse(challenge, "Password")
	val := [24]byte{
		0x67, 0xc4, 0x30, 0x11, 0xf3, 0x02, 0x98, 0xa2, 0xad, 0x35, 0xec, 0xe6, 0x4f, 0x16, 0x33, 0x1c,
		0x44, 0xbd, 0xbe, 0xd9, 0x27, 0x84, 0x1f, 0x94,
	}
	assert.Equal(t, val, nt, "NTLMv1 response mismatch")
}

func TestLMv1Response(t *testing.T) {
	challenge := [8]byte{
		0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef,
	}
	nt := lmResponse(challenge, "Password")
	val := [24]byte{
		0x98, 0xde, 0xf7, 0xb8, 0x7f, 0x88, 0xaa, 0x5d, 0xaf, 0xe2, 0xdf, 0x77, 0x96, 0x88, 0xa1, 0x72,
		0xde, 0xf1, 0x1c, 0x7d, 0x5c, 0xcd, 0xef, 0x13,
	}
	assert.Equal(t, val, nt, "LMv1 response mismatch")
}

func TestNTLMSessionResponse(t *testing.T) {
	challenge := [8]byte{
		0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef,
	}
	nonce := [8]byte{
		0xaa, 0xaa, 0xaa, 0xaa, 0xaa, 0xaa, 0xaa, 0xaa,
	}
	nt := ntlmSessionResponse(nonce, challenge, "Password")
	val := [24]byte{
		0x75, 0x37, 0xf8, 0x03, 0xae, 0x36, 0x71, 0x28, 0xca, 0x45, 0x82, 0x04, 0xbd, 0xe7, 0xca, 0xf8,
		0x1e, 0x97, 0xed, 0x26, 0x83, 0x26, 0x72, 0x32,
	}
	assert.Equal(t, val, nt, "NTLM session response mismatch")
}

func TestNTLMV2Response(t *testing.T) {
	target := "DOMAIN"
	username := "user"
	password := "SecREt01"
	challenge := [8]byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef}
	targetInformationBlock, _ := hex.DecodeString("02000c0044004f004d00410049004e0001000c005300450052005600450052000400140064006f006d00610069006e002e0063006f006d00030022007300650072007600650072002e0064006f006d00610069006e002e0063006f006d0000000000")
	nonceBytes, _ := hex.DecodeString("ffffff0011223344")
	var nonce [8]byte
	copy(nonce[:8], nonceBytes[:])
	timestamp, err := time.Parse(time.RFC3339, "2006-01-02T15:04:05Z")
	if err != nil {
		panic(err)
	}

	expectedNTLMV2Response, _ := hex.DecodeString("5c788afec59c1fef3f90bf6ea419c02501010000000000000fc4a4d5fdf6b200ffffff00112233440000000002000c0044004f004d00410049004e0001000c005300450052005600450052000400140064006f006d00610069006e002e0063006f006d00030022007300650072007600650072002e0064006f006d00610069006e002e0063006f006d000000000000000000")
	expectedLMV2Response, _ := hex.DecodeString("d6e6152ea25d03b7c6ba6629c2d6aaf0ffffff0011223344")
	ntlmV2Response, lmV2Response := getNTLMv2AndLMv2ResponsePayloads(target, username, password, challenge, nonce, targetInformationBlock, timestamp)
	assert.Equal(t, expectedNTLMV2Response, ntlmV2Response, "NTLMv2 response mismatch")
	assert.Equal(t, expectedLMV2Response, lmV2Response, "LMv2 response mismatch")
}

func TestGetNTLMv2TargetInfoFields(t *testing.T) {
	type2Message, _ := hex.DecodeString("4e544c4d53535000020000000600060038000000058289026999bc21067c77f40000000000000000ac00ac003e0000000a0039380000000f4600570042000200060046005700420001000c00590037004100410041003400040022006000700065002e00610058006e0071006e0070006e00650074002e0063006f006d00030030007900370041004100410034002e006000700065002e00610058006e0071006e0070006e00650074002e0063006f006d00050024006100610058006d002e00610058006e0071006e0070006e00650074002e0063006f006d00070008007d9647e8aed6d50100000000")
	info, err := getNTLMv2TargetInfoFields(type2Message)
	assert.NoError(t, err, "Expected no error")

	expectedResponseLength := 172
	assert.Len(t, info, expectedResponseLength, "Info length mismatch")
}

func TestGetNTLMv2TargetInfoFieldsInvalidMessage(t *testing.T) {
	type2Message, _ := hex.DecodeString("4e544c4d53535000020000000600060038000000058289026999bc21067c77f40000000000000000ac00ac003e0000000a0039380000000f4600570042000200060046005700420001000c00590037004100410041003400040022006000700065002e00610058006e0071006e0070006e00650074002e0063006f006d00030030007900370041004100410034002e006000700065002e00610058006e0071006e0070006e00650074002e0063006f006d00050024006100610058006d002e00610058006e0071006e0070006e00650074002e0063006f006d00070008007")
	_, err := getNTLMv2TargetInfoFields(type2Message)
	assert.Error(t, err, "Expected error for invalid message")
}

func TestUtf16le(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []byte
	}{
		{"empty", "", nil},
		{"ascii", "ABC", []byte{0x41, 0x00, 0x42, 0x00, 0x43, 0x00}},
		{"password", "Password", []byte{0x50, 0x00, 0x61, 0x00, 0x73, 0x00, 0x73, 0x00, 0x77, 0x00, 0x6f, 0x00, 0x72, 0x00, 0x64, 0x00}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := utf16le(tc.input)
			assert.Equal(t, tc.expected, result, "utf16le(%q) mismatch", tc.input)
		})
	}
}

func TestAuth_InitialBytes(t *testing.T) {
	auth := &Auth{
		Domain:      "DOMAIN",
		UserName:    "user",
		Password:    "password",
		Workstation: "WORKSTATION",
	}
	msg, err := auth.InitialBytes()
	assert.NoError(t, err, "InitialBytes() error")
	// Check NTLMSSP signature
	assert.Equal(t, "NTLMSSP", string(msg[:7]), "Expected NTLMSSP signature")
	// Check null terminator
	assert.Equal(t, byte(0), msg[7], "Expected null terminator at position 7")
	// Check message type (should be 1 for NEGOTIATE_MESSAGE)
	msgType := uint32(msg[8]) | uint32(msg[9])<<8 | uint32(msg[10])<<16 | uint32(msg[11])<<24
	assert.Equal(t, uint32(1), msgType, "Expected message type 1")
}

func TestAuth_Free(t *testing.T) {
	auth := &Auth{
		Domain:   "DOMAIN",
		UserName: "user",
		Password: "password",
	}
	// Free should not panic and do nothing
	auth.Free()
}

func TestAuth_SetChannelBinding_MD5(t *testing.T) {
	auth := &Auth{}
	// Create a channel binding that is not TLSExporter type
	cb := &integratedauth.ChannelBindings{
		Type:            integratedauth.ChannelBindingsTypeTLSServerEndPoint,
		ApplicationData: []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
	}
	auth.SetChannelBinding(cb)
	// Should use MD5 hash for non-TLSExporter types
	assert.NotEmpty(t, auth.ChannelBinding, "Expected channel binding to be set")
}

func TestAuth_SetChannelBinding_TLSExporter(t *testing.T) {
	auth := &Auth{}
	appData := []byte("application data for exporter")
	cb := &integratedauth.ChannelBindings{
		Type:            integratedauth.ChannelBindingsTypeTLSExporter,
		ApplicationData: appData,
	}
	auth.SetChannelBinding(cb)
	assert.Equal(t, appData, auth.ChannelBinding, "Expected application data")
}

func TestGetAuth(t *testing.T) {
	tests := []struct {
		name    string
		user    string
		wantErr bool
	}{
		{
			name:    "valid domain user",
			user:    "DOMAIN\\username",
			wantErr: false,
		},
		{
			name:    "valid with complex domain",
			user:    "MY.DOMAIN.COM\\myuser",
			wantErr: false,
		},
		{
			name:    "invalid no backslash",
			user:    "username",
			wantErr: true,
		},
		{
			name:    "invalid empty",
			user:    "",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			config := msdsn.Config{
				User:        tc.user,
				Password:    "password",
				Workstation: "WORKSTATION",
			}
			auth, err := getAuth(config)
			if tc.wantErr {
				assert.Error(t, err, "Expected error")
			} else {
				assert.NoError(t, err, "Unexpected error")
				assert.NotNil(t, auth, "Expected auth")
			}
		})
	}
}

func TestBuildNTLMResponsePayload(t *testing.T) {
	lm := make([]byte, 24)
	nt := make([]byte, 24)
	flags := uint32(0xe2088297)

	msg, err := buildNTLMResponsePayload(lm, nt, flags, "DOMAIN", "WORKSTATION", "user")
	assert.NoError(t, err, "buildNTLMResponsePayload error")

	// Check NTLMSSP signature
	assert.Equal(t, "NTLMSSP", string(msg[:7]), "Expected NTLMSSP signature")

	// Check message type (should be 3 for AUTHENTICATE_MESSAGE)
	msgType := uint32(msg[8]) | uint32(msg[9])<<8 | uint32(msg[10])<<16 | uint32(msg[11])<<24
	assert.Equal(t, uint32(3), msgType, "Expected message type 3")

	// Message should be long enough
	assert.GreaterOrEqual(t, len(msg), 88, "Message too short")
}

func TestNextBytes(t *testing.T) {
	auth := &Auth{
		Domain:         "DOMAIN",
		UserName:       "user",
		Password:       "password",
		Workstation:    "WORKSTATION",
		ChannelBinding: []byte{},
	}

	// Test with invalid signature
	invalidSig := make([]byte, 32)
	copy(invalidSig, "INVALID\x00")
	_, err := auth.NextBytes(invalidSig)
	assert.Error(t, err, "Expected error for invalid signature")

	// Test with invalid message type
	invalidType := make([]byte, 32)
	copy(invalidType, "NTLMSSP\x00")
	// Set message type to something other than CHALLENGE_MESSAGE (2)
	invalidType[8] = 1
	_, err = auth.NextBytes(invalidType)
	assert.Error(t, err, "Expected error for invalid message type")
}

func TestNextBytesWithChallengeMessage(t *testing.T) {
	auth := &Auth{
		Domain:         "DOMAIN",
		UserName:       "user",
		Password:       "password",
		Workstation:    "WORKSTATION",
		ChannelBinding: []byte{},
	}

	// Create a minimal valid Type 2 (CHALLENGE) message
	// NTLMSSP signature + message type + challenge fields
	challengeMsg := make([]byte, 56)
	copy(challengeMsg, "NTLMSSP\x00")
	// Message type 2 (CHALLENGE_MESSAGE)
	challengeMsg[8] = 2
	challengeMsg[9] = 0
	challengeMsg[10] = 0
	challengeMsg[11] = 0
	// Flags at offset 20-24 (no extended session security)
	challengeMsg[20] = 0x97
	challengeMsg[21] = 0x82
	challengeMsg[22] = 0x08
	challengeMsg[23] = 0xe2
	// Challenge at offset 24-32
	for i := 24; i < 32; i++ {
		challengeMsg[i] = byte(i)
	}

	msg, err := auth.NextBytes(challengeMsg)
	assert.NoError(t, err, "NextBytes error")
	assert.NotNil(t, msg, "Expected message")
	// Check NTLMSSP signature in response
	if len(msg) > 7 {
		assert.Equal(t, "NTLMSSP", string(msg[:7]), "Response should have NTLMSSP signature")
	}
}
