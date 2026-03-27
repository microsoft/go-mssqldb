package mssql

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDoneStructIsError(t *testing.T) {
	tests := []struct {
		name    string
		status  uint16
		errors  []Error
		wantErr bool
	}{
		{
			name:    "no error status and no errors",
			status:  0,
			errors:  nil,
			wantErr: false,
		},
		{
			name:    "doneError status set",
			status:  doneError,
			errors:  nil,
			wantErr: true,
		},
		{
			name:    "has errors in slice",
			status:  0,
			errors:  []Error{{Message: "test error"}},
			wantErr: true,
		},
		{
			name:    "both error status and errors",
			status:  doneError,
			errors:  []Error{{Message: "test error"}},
			wantErr: true,
		},
		{
			name:    "doneMore flag only",
			status:  doneMore,
			errors:  nil,
			wantErr: false,
		},
		{
			name:    "doneCount flag only",
			status:  doneCount,
			errors:  nil,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := doneStruct{
				Status: tt.status,
				errors: tt.errors,
			}
			assert.Equal(t, tt.wantErr, d.isError(), "doneStruct.isError()")
		})
	}
}

func TestDoneStructGetError(t *testing.T) {
	tests := []struct {
		name        string
		errors      []Error
		wantMessage string
		wantAllLen  int
	}{
		{
			name:        "no errors returns default message",
			errors:      nil,
			wantMessage: "Request failed but didn't provide reason",
			wantAllLen:  0,
		},
		{
			name:        "empty errors slice returns default message",
			errors:      []Error{},
			wantMessage: "Request failed but didn't provide reason",
			wantAllLen:  0,
		},
		{
			name:        "single error",
			errors:      []Error{{Message: "single error"}},
			wantMessage: "single error",
			wantAllLen:  1,
		},
		{
			name: "multiple errors returns last",
			errors: []Error{
				{Message: "first error"},
				{Message: "second error"},
				{Message: "third error"},
			},
			wantMessage: "third error",
			wantAllLen:  3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := doneStruct{errors: tt.errors}
			got := d.getError()

			assert.Equal(t, tt.wantMessage, got.Message, "doneStruct.getError().Message")

			if len(tt.errors) > 0 {
				assert.Len(t, got.All, tt.wantAllLen, "doneStruct.getError().All length")
			}
		})
	}
}

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

func makeFinalBuf(data []byte) *tdsBuffer {
	return &tdsBuffer{
		packetSize: len(data),
		rbuf:       data,
		rpos:       0,
		rsize:      len(data),
		final:      true,
	}
}

func TestParseColInfo(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "empty",
			data: []byte{0x00, 0x00}, // length=0, no data
		},
		{
			name: "with column info",
			// length=3, ColNum=1, TableNum=1, Status=0
			data: []byte{0x03, 0x00, 0x01, 0x01, 0x00},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parseColInfo(makeFinalBuf(tt.data))
		})
	}

	t.Run("truncated stream panics", func(t *testing.T) {
		defer func() {
			if v := recover(); v == nil {
				t.Fatal("expected panic for truncated COLINFO stream")
			}
		}()
		// size=4 but only 1 byte of payload follows; io.CopyN should hit EOF
		parseColInfo(makeFinalBuf([]byte{0x04, 0x00, 0x01}))
		t.Fatal("parseColInfo should have panicked")
	})
}

func TestParseTabName(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "empty",
			data: []byte{0x00, 0x00}, // length=0, no data
		},
		{
			name: "with table name",
			// length=4, "tabl"
			data: []byte{0x04, 0x00, 't', 'a', 'b', 'l'},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parseTabName(makeFinalBuf(tt.data))
		})
	}

	t.Run("truncated stream panics", func(t *testing.T) {
		defer func() {
			if v := recover(); v == nil {
				t.Fatal("expected panic for truncated TABNAME stream")
			}
		}()
		// size=4 but only 1 byte of payload follows; io.CopyN should hit EOF
		parseTabName(makeFinalBuf([]byte{0x04, 0x00, 'x'}))
		t.Fatal("parseTabName should have panicked")
	})
}

func TestTokenString(t *testing.T) {
	assert.Equal(t, "tokenTabName", tokenTabName.String())
	assert.Equal(t, "tokenColInfo", tokenColInfo.String())
}

// TestProcessSingleResponseWithTriggerTableTokens verifies that COLINFO (0xA5)
// and TABNAME (0xA4) tokens are handled without error. SQL Server sends these
// tokens when executing INSERT/UPDATE/DELETE on tables that have triggers.
func TestProcessSingleResponseWithTriggerTableTokens(t *testing.T) {
	// Construct a minimal valid TDS reply packet containing COLINFO + TABNAME
	// tokens followed by a DONE token.
	tokenStream := []byte{
		byte(tokenColInfo), 0x03, 0x00, 0x01, 0x01, 0x00, // COLINFO: length=3, ColNum=1, TableNum=1, Status=0
		byte(tokenTabName), 0x04, 0x00, 't', 'a', 'b', 'l', // TABNAME: length=4, "tabl"
		byte(tokenDone), 0x00, 0x00, 0x00, 0x00, // Status=doneFinal, CurCmd=0
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // RowCount=0
	}

	totalSize := 8 + len(tokenStream)
	packet := make([]byte, totalSize)
	packet[0] = byte(packReply)
	packet[1] = 0x01 // Status = final
	binary.BigEndian.PutUint16(packet[2:4], uint16(totalSize))
	packet[6] = 0x01 // PacketNo
	copy(packet[8:], tokenStream)

	sess := &tdsSession{
		buf: newTdsBuffer(defaultPacketSize, closableBuffer{bytes.NewBuffer(packet)}),
	}

	ch := make(chan tokenStruct, 10)
	processSingleResponse(context.Background(), sess, ch, outputs{})

	// Drain the channel and verify no errors were received.
	// processSingleResponse closes ch when it returns.
	// Before the fix, processSingleResponse would panic with
	// "unknown token type returned: token(165)" when it encountered tokenColInfo.
	for tok := range ch {
		if err, ok := tok.(error); ok {
			t.Fatalf("unexpected error processing COLINFO/TABNAME tokens: %v", err)
		}
	}
}
