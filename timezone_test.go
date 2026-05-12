package mssql

import (
	"testing"
	"time"

	"github.com/microsoft/go-mssqldb/msdsn"
	"github.com/stretchr/testify/assert"
)

func TestGetTimezone(t *testing.T) {
	tests := []struct {
		name     string
		conn     *Conn
		expected *time.Location
	}{
		{
			name:     "nil connection returns UTC",
			conn:     nil,
			expected: time.UTC,
		},
		{
			name: "connection with nil session returns UTC",
			conn: &Conn{
				sess: nil,
			},
			expected: time.UTC,
		},
		{
			name: "connection with session that has nil encoding timezone returns UTC",
			conn: &Conn{
				sess: &tdsSession{
					encoding: msdsn.EncodeParameters{},
				},
			},
			expected: time.UTC,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getTimezone(tt.conn)
			assert.Equal(t, tt.expected, result, "getTimezone()")
		})
	}
}

func TestGetTimezoneWithLocation(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("America/New_York timezone not available")
	}

	conn := &Conn{
		sess: &tdsSession{
			encoding: msdsn.EncodeParameters{
				Timezone: loc,
			},
		},
	}

	result := getTimezone(conn)
	assert.Equal(t, loc, result, "getTimezone() with location")
}
