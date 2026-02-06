package mssql

import (
	"encoding/hex"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFeatureExtAck(t *testing.T) {
	spacesRE := regexp.MustCompile(`\s+`)

	tests := []string{
		"  FF",
		"  01 03 00 00 00 AB CD EF FF",
		"  02 00 00 00 00 FF\n",
		"  02 20 00 00 00 00 01 02  03 04 05 06 07 08 09 0A\n" +
			"0B 0C 0D 0E 0F 10 11 12  13 14 15 16 17 18 19 1A\n" +
			"1B 1C 1D 1E 1F FF\n",
		"  02 40 00 00 00 00 01 02  03 04 05 06 07 08 09 0A\n" +
			"0B 0C 0D 0E 0F 10 11 12  13 14 15 16 17 18 19 1A\n" +
			"1B 1C 1D 1E 1F 20 21 22  23 24 25 26 27 28 29 2A\n" +
			"2B 2C 2D 2E 2F 30 31 32  33 34 35 36 37 38 39 3A\n" +
			"3B 3C 3D 3E 3F FF\n",
	}

	for _, tst := range tests {
		b, err := hex.DecodeString(spacesRE.ReplaceAllString(tst, ""))
		if err != nil {
			t.Log(err)
			t.FailNow()
		}

		r := &tdsBuffer{
			packetSize: len(b),
			rbuf:       b,
			rpos:       0,
			rsize:      len(b),
		}

		parseFeatureExtAck(r)
	}
}

// TestParseFeatureExtAckJSON tests JSON feature acknowledgement parsing.
func TestParseFeatureExtAckJSON(t *testing.T) {
	spacesRE := regexp.MustCompile(`\s+`)

	// JSON feature ack: feature ID 0x0D, length 1, version 1, terminator 0xFF
	// Format: [featureID:1][length:4 little-endian][data:length][FF terminator]
	jsonAck := "0D 01 00 00 00 01 FF"

	b, err := hex.DecodeString(spacesRE.ReplaceAllString(jsonAck, ""))
	require.NoError(t, err, "Failed to decode hex")

	r := &tdsBuffer{
		packetSize: len(b),
		rbuf:       b,
		rpos:       0,
		rsize:      len(b),
	}

	ack := parseFeatureExtAck(r)

	// Verify JSON feature was parsed
	version, ok := ack[featExtJSONSUPPORT]
	assert.True(t, ok, "Expected featExtJSONSUPPORT in ack map")
	v, ok := version.(byte)
	assert.True(t, ok, "Expected byte type for JSON version")
	assert.Equal(t, jsonSupportVersion, v, "JSON version")
}

// TestParseFeatureExtAckMultiple tests parsing multiple features including JSON.
func TestParseFeatureExtAckMultiple(t *testing.T) {
	spacesRE := regexp.MustCompile(`\s+`)

	// Column encryption (04) + JSON (0D) + terminator
	// Column encryption: 04 [len:4 = 01 00 00 00] [version:1 = 01]
	// JSON: 0D [len:4 = 01 00 00 00] [version:1 = 01]
	multiAck := "04 01 00 00 00 01 0D 01 00 00 00 01 FF"

	b, err := hex.DecodeString(spacesRE.ReplaceAllString(multiAck, ""))
	require.NoError(t, err, "Failed to decode hex")

	r := &tdsBuffer{
		packetSize: len(b),
		rbuf:       b,
		rpos:       0,
		rsize:      len(b),
	}

	ack := parseFeatureExtAck(r)

	// Verify column encryption feature was parsed (returns colAckStruct)
	colAck, ok := ack[featExtCOLUMNENCRYPTION]
	assert.True(t, ok, "Expected featExtCOLUMNENCRYPTION in ack map")
	cas, ok := colAck.(colAckStruct)
	assert.True(t, ok, "Expected colAckStruct type for column encryption")
	assert.Equal(t, 1, cas.Version, "column encryption version")

	// Verify JSON feature was parsed
	version, ok := ack[featExtJSONSUPPORT]
	assert.True(t, ok, "Expected featExtJSONSUPPORT in ack map")
	v, ok := version.(byte)
	assert.True(t, ok, "Expected byte type for JSON version")
	assert.Equal(t, jsonSupportVersion, v, "JSON version")
}
