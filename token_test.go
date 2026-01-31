package mssql

import (
	"encoding/hex"
	"regexp"
	"testing"
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
	if err != nil {
		t.Fatalf("Failed to decode hex: %v", err)
	}

	r := &tdsBuffer{
		packetSize: len(b),
		rbuf:       b,
		rpos:       0,
		rsize:      len(b),
	}

	ack := parseFeatureExtAck(r)

	// Verify JSON feature was parsed
	if version, ok := ack[featExtJSONSUPPORT]; !ok {
		t.Error("Expected featExtJSONSUPPORT in ack map")
	} else if v, ok := version.(byte); !ok {
		t.Errorf("Expected byte type for JSON version, got %T", version)
	} else if v != jsonSupportVersion {
		t.Errorf("Expected JSON version %#x, got %#x", jsonSupportVersion, v)
	}
}

// TestParseFeatureExtAckMultiple tests parsing multiple features including JSON.
func TestParseFeatureExtAckMultiple(t *testing.T) {
	spacesRE := regexp.MustCompile(`\s+`)

	// Column encryption (04) + JSON (0D) + terminator
	// Column encryption: 04 [len:4 = 01 00 00 00] [version:1 = 01]
	// JSON: 0D [len:4 = 01 00 00 00] [version:1 = 01]
	multiAck := "04 01 00 00 00 01 0D 01 00 00 00 01 FF"

	b, err := hex.DecodeString(spacesRE.ReplaceAllString(multiAck, ""))
	if err != nil {
		t.Fatalf("Failed to decode hex: %v", err)
	}

	r := &tdsBuffer{
		packetSize: len(b),
		rbuf:       b,
		rpos:       0,
		rsize:      len(b),
	}

	ack := parseFeatureExtAck(r)

	// Verify column encryption feature was parsed (returns colAckStruct)
	if colAck, ok := ack[featExtCOLUMNENCRYPTION]; !ok {
		t.Error("Expected featExtCOLUMNENCRYPTION in ack map")
	} else if cas, ok := colAck.(colAckStruct); !ok {
		t.Errorf("Expected colAckStruct type for column encryption, got %T", colAck)
	} else if cas.Version != 1 {
		t.Errorf("Expected column encryption version 1, got %d", cas.Version)
	}

	// Verify JSON feature was parsed
	if version, ok := ack[featExtJSONSUPPORT]; !ok {
		t.Error("Expected featExtJSONSUPPORT in ack map")
	} else if v, ok := version.(byte); !ok {
		t.Errorf("Expected byte type for JSON version, got %T", version)
	} else if v != jsonSupportVersion {
		t.Errorf("Expected JSON version %#x, got %#x", jsonSupportVersion, v)
	}
}
