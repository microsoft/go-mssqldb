package mssql

import (
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
