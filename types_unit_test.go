package mssql

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/microsoft/go-mssqldb/msdsn"
	"github.com/stretchr/testify/assert"
)

func TestDecodeDateTim4(t *testing.T) {
	tests := []struct {
		name     string
		days     uint16
		mins     uint16
		expected time.Time
	}{
		{
			name:     "epoch - Jan 1, 1900 00:00",
			days:     0,
			mins:     0,
			expected: time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "one day after epoch",
			days:     1,
			mins:     0,
			expected: time.Date(1900, 1, 2, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "noon on epoch",
			days:     0,
			mins:     720, // 12 hours * 60 minutes
			expected: time.Date(1900, 1, 1, 0, 720, 0, 0, time.UTC),
		},
		{
			name:     "Jan 1, 2000 00:00",
			days:     36524, // days from 1900-01-01 to 2000-01-01
			mins:     0,
			expected: time.Date(1900, 1, 1+36524, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "with minutes",
			days:     100,
			mins:     123,
			expected: time.Date(1900, 1, 101, 0, 123, 0, 0, time.UTC),
		},
		{
			name:     "end of day",
			days:     0,
			mins:     1439, // 23:59
			expected: time.Date(1900, 1, 1, 0, 1439, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := make([]byte, 4)
			binary.LittleEndian.PutUint16(buf[0:2], tt.days)
			binary.LittleEndian.PutUint16(buf[2:4], tt.mins)

			result := decodeDateTim4(buf, time.UTC)
			assert.True(t, result.Equal(tt.expected), "decodeDateTim4() = %v, want %v", result, tt.expected)
		})
	}
}

func TestEncodeDateTim4(t *testing.T) {
	tests := []struct {
		name         string
		input        time.Time
		expectedDays uint16
		expectedMins uint16
	}{
		{
			name:         "epoch - Jan 1, 1900 00:00",
			input:        time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC),
			expectedDays: 0,
			expectedMins: 0,
		},
		{
			name:         "one day after epoch",
			input:        time.Date(1900, 1, 2, 0, 0, 0, 0, time.UTC),
			expectedDays: 1,
			expectedMins: 0,
		},
		{
			name:         "noon on Jan 1, 1900",
			input:        time.Date(1900, 1, 1, 12, 0, 0, 0, time.UTC),
			expectedDays: 0,
			expectedMins: 720,
		},
		{
			name:         "2:03 PM",
			input:        time.Date(1900, 1, 1, 14, 3, 0, 0, time.UTC),
			expectedDays: 0,
			expectedMins: 843, // 14*60 + 3
		},
		{
			name:         "date before epoch clamps to zero",
			input:        time.Date(1899, 12, 1, 12, 30, 0, 0, time.UTC), // 31 days before epoch
			expectedDays: 0,
			expectedMins: 0,
		},
		{
			name:         "Jan 1, 2000 at 6:30 AM",
			input:        time.Date(2000, 1, 1, 6, 30, 0, 0, time.UTC),
			expectedDays: 36524,
			expectedMins: 390, // 6*60 + 30
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := encodeDateTim4(tt.input, time.UTC)

			days := binary.LittleEndian.Uint16(result[0:2])
			mins := binary.LittleEndian.Uint16(result[2:4])

			assert.Equal(t, tt.expectedDays, days, "encodeDateTim4() days")
			assert.Equal(t, tt.expectedMins, mins, "encodeDateTim4() mins")
		})
	}
}

func TestEncodeDateTim4_Roundtrip(t *testing.T) {
	testTimes := []time.Time{
		time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(1900, 6, 15, 12, 30, 0, 0, time.UTC),
		time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 7, 4, 14, 30, 0, 0, time.UTC),
		time.Date(2050, 12, 31, 23, 59, 0, 0, time.UTC),
	}

	for _, tt := range testTimes {
		t.Run(tt.Format("2006-01-02 15:04"), func(t *testing.T) {
			encoded := encodeDateTim4(tt, time.UTC)
			decoded := decodeDateTim4(encoded, time.UTC)

			// SmallDateTime only stores minutes, so we compare date and hour:minute
			assert.Equal(t, tt.Year(), decoded.Year(), "Year")
			assert.Equal(t, tt.Month(), decoded.Month(), "Month")
			assert.Equal(t, tt.Day(), decoded.Day(), "Day")
			assert.Equal(t, tt.Hour(), decoded.Hour(), "Hour")
			assert.Equal(t, tt.Minute(), decoded.Minute(), "Minute")
		})
	}
}

func TestDecodeDateTime(t *testing.T) {
	tests := []struct {
		name     string
		days     int32
		ths      uint32 // three-hundredths of a second
		expected time.Time
	}{
		{
			name:     "epoch - Jan 1, 1900 00:00:00",
			days:     0,
			ths:      0,
			expected: time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "one day after epoch",
			days:     1,
			ths:      0,
			expected: time.Date(1900, 1, 2, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "one second after epoch",
			days:     0,
			ths:      300, // 1 second = 300 three-hundredths
			expected: time.Date(1900, 1, 1, 0, 0, 1, 0, time.UTC),
		},
		{
			name:     "one minute after epoch",
			days:     0,
			ths:      18000, // 60 seconds * 300
			expected: time.Date(1900, 1, 1, 0, 1, 0, 0, time.UTC),
		},
		{
			name:     "one hour after epoch",
			days:     0,
			ths:      1080000, // 3600 seconds * 300
			expected: time.Date(1900, 1, 1, 1, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := make([]byte, 8)
			binary.LittleEndian.PutUint32(buf[0:4], uint32(tt.days))
			binary.LittleEndian.PutUint32(buf[4:8], tt.ths)

			result := decodeDateTime(buf, time.UTC)
			// Compare without nanoseconds for the basic tests
			assert.Equal(t, tt.expected.Year(), result.Year(), "Year")
			assert.Equal(t, tt.expected.Month(), result.Month(), "Month")
			assert.Equal(t, tt.expected.Day(), result.Day(), "Day")
			assert.Equal(t, tt.expected.Hour(), result.Hour(), "Hour")
			assert.Equal(t, tt.expected.Minute(), result.Minute(), "Minute")
			assert.Equal(t, tt.expected.Second(), result.Second(), "Second")
		})
	}
}

func TestThreeHundredthsConversions(t *testing.T) {
	tests := []struct {
		name string
		ths  int
	}{
		{"zero", 0},
		{"one", 1},
		{"ten", 10},
		{"hundred", 100},
		{"max in second", 299},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ns := threeHundredthsOfASecondToNanos(tt.ths)
			back := nanosToThreeHundredthsOfASecond(ns)

			// Allow for rounding differences
			assert.InDelta(t, tt.ths, back, 1, "roundtrip: ths=%d -> ns=%d -> ths=%d", tt.ths, ns, back)
		})
	}
}

func TestNanosToThreeHundredthsOfASecond(t *testing.T) {
	tests := []struct {
		name     string
		ns       int
		expected int
	}{
		{"zero nanoseconds", 0, 0},
		{"one millisecond", 1000000, 0},          // 1ms = 0.3 three-hundredths, rounds to 0
		{"ten milliseconds", 10000000, 3},        // 10ms = 3 three-hundredths
		{"100 milliseconds", 100000000, 30},      // 100ms = 30 three-hundredths
		{"half second", 500000000, 150},          // 500ms = 150 three-hundredths
		{"one second minus 1ns", 999999999, 300}, // rounds to 300
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := nanosToThreeHundredthsOfASecond(tt.ns)
			assert.Equal(t, tt.expected, result, "nanosToThreeHundredthsOfASecond(%d)", tt.ns)
		})
	}
}

func TestThreeHundredthsOfASecondToNanos(t *testing.T) {
	tests := []struct {
		name     string
		ths      int
		expected int
	}{
		{"zero", 0, 0},
		{"one three-hundredth", 1, 3000000},   // ~3.33ms rounded
		{"three hundredths", 3, 10000000},     // 10ms
		{"thirty hundredths", 30, 100000000},  // 100ms
		{"150 (half second)", 150, 500000000}, // 500ms
		{"300 (one second)", 300, 1000000000}, // 1s
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := threeHundredthsOfASecondToNanos(tt.ths)
			// Allow some tolerance due to rounding
			assert.InDelta(t, tt.expected, result, 1000000, "threeHundredthsOfASecondToNanos(%d)", tt.ths)
		})
	}
}

func TestGregorianDays(t *testing.T) {
	tests := []struct {
		name    string
		year    int
		yearDay int
	}{
		{"Jan 1, 1900", 1900, 1},
		{"Jan 1, 2000", 2000, 1},
		{"Dec 31, 1999 (non-leap)", 1999, 365},
		{"Dec 31, 2000 (leap year)", 2000, 366},
		{"Jan 1, 1753 (min datetime)", 1753, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := gregorianDays(tt.year, tt.yearDay)
			// Just verify it doesn't panic and returns something reasonable
			assert.Positive(t, result, "gregorianDays(%d, %d)", tt.year, tt.yearDay)
		})
	}

	// Test that days increase over time
	t.Run("days increase chronologically", func(t *testing.T) {
		d1900 := gregorianDays(1900, 1)
		d2000 := gregorianDays(2000, 1)
		d2025 := gregorianDays(2025, 1)

		assert.Greater(t, d2000, d1900, "Year 2000 should have more days than 1900")
		assert.Greater(t, d2025, d2000, "Year 2025 should have more days than 2000")
	})
}

func TestDecodeMoney(t *testing.T) {
	testCases := []struct {
		name string
		buf  []byte
		want string // Expected decimal string representation
	}{
		{
			name: "zero",
			buf:  []byte{0, 0, 0, 0, 0, 0, 0, 0},
			want: "0.0000",
		},
		{
			name: "one dollar (10000 scaled)",
			// money is stored as int64 / 10000, in weird byte order: buf[4:8] then buf[0:4]
			// 10000 = 0x00002710
			// So low 4 bytes at buf[4:8] = 0x10, 0x27, 0x00, 0x00
			// High 4 bytes at buf[0:4] = 0x00, 0x00, 0x00, 0x00
			buf:  []byte{0x00, 0x00, 0x00, 0x00, 0x10, 0x27, 0x00, 0x00},
			want: "1.0000",
		},
		{
			name: "negative one dollar",
			// -10000 in two's complement = 0xFFFFFFFFFFFFD8F0
			// High 4 bytes: 0xFF, 0xFF, 0xFF, 0xFF
			// Low 4 bytes: 0xF0, 0xD8, 0xFF, 0xFF
			buf:  []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xF0, 0xD8, 0xFF, 0xFF},
			want: "-1.0000",
		},
		{
			name: "ten dollars (100000 scaled)",
			// 100000 = 0x000186A0
			buf:  []byte{0x00, 0x00, 0x00, 0x00, 0xA0, 0x86, 0x01, 0x00},
			want: "10.0000",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeMoney(tc.buf)
			gotStr := string(got)
			assert.Equal(t, tc.want, gotStr, "decodeMoney()")
		})
	}
}

func TestDecodeMoney4(t *testing.T) {
	testCases := []struct {
		name string
		buf  []byte
		want string
	}{
		{
			name: "zero",
			buf:  []byte{0, 0, 0, 0},
			want: "0.0000",
		},
		{
			name: "one dollar",
			// 10000 in little endian = 0x10, 0x27, 0x00, 0x00
			buf:  []byte{0x10, 0x27, 0x00, 0x00},
			want: "1.0000",
		},
		{
			name: "negative one dollar",
			// -10000 in little endian as int32 = 0xF0, 0xD8, 0xFF, 0xFF
			buf:  []byte{0xF0, 0xD8, 0xFF, 0xFF},
			want: "-1.0000",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeMoney4(tc.buf)
			gotStr := string(got)
			assert.Equal(t, tc.want, gotStr, "decodeMoney4()")
		})
	}
}

func TestDecodeGuid(t *testing.T) {
	testCases := []struct {
		name           string
		buf            []byte
		guidConversion bool
		want           []byte
	}{
		{
			name:           "no conversion",
			buf:            []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
			guidConversion: false,
			want:           []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		},
		{
			name: "with conversion swaps byte order",
			// Input is big-endian for first 8 bytes
			buf:            []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 9, 10, 11, 12, 13, 14, 15, 16},
			guidConversion: true,
			// First 4 bytes: 0x01020304 (BE) -> 0x04030201 (LE)
			// Next 2 bytes: 0x0506 (BE) -> 0x0605 (LE)
			// Next 2 bytes: 0x0708 (BE) -> 0x0807 (LE)
			// Remaining 8 bytes unchanged
			want: []byte{0x04, 0x03, 0x02, 0x01, 0x06, 0x05, 0x08, 0x07, 9, 10, 11, 12, 13, 14, 15, 16},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			encoding := msdsn.EncodeParameters{GuidConversion: tc.guidConversion}
			got := decodeGuid(tc.buf, encoding)
			assert.Equal(t, tc.want, got, "decodeGuid()")
		})
	}
}

func TestDecodeDateInt(t *testing.T) {
	testCases := []struct {
		name string
		buf  []byte
		want int
	}{
		{
			name: "day zero",
			buf:  []byte{0, 0, 0},
			want: 0,
		},
		{
			name: "day one",
			buf:  []byte{1, 0, 0},
			want: 1,
		},
		{
			name: "day 256",
			buf:  []byte{0, 1, 0},
			want: 256,
		},
		{
			name: "day 65536",
			buf:  []byte{0, 0, 1},
			want: 65536,
		},
		{
			name: "max value 0xFFFFFF",
			buf:  []byte{0xFF, 0xFF, 0xFF},
			want: 16777215,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeDateInt(tc.buf)
			assert.Equal(t, tc.want, got, "decodeDateInt(%v)", tc.buf)
		})
	}
}

func TestDecodeDate(t *testing.T) {
	loc := time.UTC
	testCases := []struct {
		name string
		buf  []byte
		want time.Time
	}{
		{
			name: "year 1 day 1",
			buf:  []byte{0, 0, 0},
			want: time.Date(1, 1, 1, 0, 0, 0, 0, loc),
		},
		{
			name: "day 365 is year 1 day 366",
			buf:  []byte{0x6D, 0x01, 0x00},              // 365
			want: time.Date(1, 1, 366, 0, 0, 0, 0, loc), // Go normalizes to next year
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeDate(tc.buf, loc)
			assert.True(t, got.Equal(tc.want), "decodeDate() = %v, want %v", got, tc.want)
		})
	}
}

func TestEncodeDate(t *testing.T) {
	testCases := []struct {
		name string
		val  time.Time
		want []byte
	}{
		{
			name: "year 1 jan 1",
			val:  time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC),
			want: []byte{0, 0, 0},
		},
		{
			name: "year 2000 jan 1",
			val:  time.Date(2000, 1, 1, 12, 30, 45, 0, time.UTC),
			// gregorianDays(2000, 1) = 730119 = 0x0B2407
			want: []byte{0x07, 0x24, 0x0B},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := encodeDate(tc.val)
			assert.Equal(t, tc.want, got, "encodeDate()")
		})
	}
}

func TestCalcTimeSize(t *testing.T) {
	testCases := []struct {
		scale int
		want  int
	}{
		{0, 3},
		{1, 3},
		{2, 3},
		{3, 4},
		{4, 4},
		{5, 5},
		{6, 5},
		{7, 5},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("scale_%d", tc.scale), func(t *testing.T) {
			got := calcTimeSize(tc.scale)
			assert.Equal(t, tc.want, got, "calcTimeSize(%d)", tc.scale)
		})
	}
}

func TestDecodeDateTime2(t *testing.T) {
	loc := time.UTC
	testCases := []struct {
		name  string
		scale uint8
		buf   []byte
		want  time.Time
	}{
		{
			name:  "year 1 day 1 midnight scale 0",
			scale: 0,
			buf:   []byte{0, 0, 0, 0, 0, 0}, // 3 bytes time + 3 bytes date
			want:  time.Date(1, 1, 1, 0, 0, 0, 0, loc),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeDateTime2(tc.scale, tc.buf, loc)
			assert.True(t, got.Equal(tc.want), "decodeDateTime2() = %v, want %v", got, tc.want)
		})
	}
}

func TestDateTime2Function(t *testing.T) {
	testCases := []struct {
		name        string
		time        time.Time
		wantDays    int
		wantSeconds int
		wantNs      int
	}{
		{
			name:        "year 1 jan 1 midnight",
			time:        time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC),
			wantDays:    0,
			wantSeconds: 0,
			wantNs:      0,
		},
		{
			name:        "year 2000 noon",
			time:        time.Date(2000, 1, 1, 12, 0, 0, 0, time.UTC),
			wantDays:    730119,
			wantSeconds: 43200, // 12 * 3600
			wantNs:      0,
		},
		{
			name:        "before epoch clamps to zero",
			time:        time.Date(0, 1, 1, 12, 30, 45, 123, time.UTC),
			wantDays:    0,
			wantSeconds: 0,
			wantNs:      0,
		},
		{
			name:        "after max clamps to max",
			time:        time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC),
			wantDays:    3652058, // max days
			wantSeconds: 86399,   // 23*3600 + 59*60 + 59
			wantNs:      999999900,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			days, seconds, ns := dateTime2(tc.time)
			assert.Equal(t, tc.wantDays, days, "dateTime2() days")
			assert.Equal(t, tc.wantSeconds, seconds, "dateTime2() seconds")
			assert.Equal(t, tc.wantNs, ns, "dateTime2() ns")
		})
	}
}

func TestEncodeDateTime2_Roundtrip(t *testing.T) {
	loc := time.UTC
	testCases := []struct {
		name  string
		val   time.Time
		scale int
	}{
		{
			name:  "simple date scale 0",
			val:   time.Date(2020, 6, 15, 14, 30, 0, 0, loc),
			scale: 0,
		},
		{
			name:  "with nanoseconds scale 7",
			val:   time.Date(2020, 6, 15, 14, 30, 45, 123456700, loc),
			scale: 7,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			encoded := encodeDateTime2(tc.val, tc.scale)
			decoded := decodeDateTime2(uint8(tc.scale), encoded, loc)

			// Truncate expected time to scale precision
			expectedNs := tc.val.Nanosecond()
			divisor := int(math.Pow10(9 - tc.scale))
			expectedNs = (expectedNs / divisor) * divisor

			expectedTime := time.Date(
				tc.val.Year(), tc.val.Month(), tc.val.Day(),
				tc.val.Hour(), tc.val.Minute(), tc.val.Second(),
				expectedNs, loc,
			)

			assert.True(t, decoded.Equal(expectedTime), "roundtrip failed: got %v, want %v", decoded, expectedTime)
		})
	}
}

func TestEncodeDateTimeOffset_Roundtrip(t *testing.T) {
	testCases := []struct {
		name  string
		val   time.Time
		scale int
	}{
		{
			name:  "UTC time scale 0",
			val:   time.Date(2020, 6, 15, 14, 30, 0, 0, time.UTC),
			scale: 0,
		},
		{
			name:  "positive offset scale 7",
			val:   time.Date(2020, 6, 15, 14, 30, 45, 123456700, time.FixedZone("", 5*3600+30*60)), // +05:30
			scale: 7,
		},
		{
			name:  "negative offset scale 3",
			val:   time.Date(2020, 6, 15, 10, 0, 0, 0, time.FixedZone("", -8*3600)), // -08:00
			scale: 3,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			encoded := encodeDateTimeOffset(tc.val, tc.scale)
			decoded := decodeDateTimeOffset(uint8(tc.scale), encoded)

			// Verify the decoded time represents the same instant
			assert.True(t, decoded.Equal(tc.val), "roundtrip failed: got %v, want %v", decoded, tc.val)

			// Verify offset is preserved
			_, decodedOffset := decoded.Zone()
			_, originalOffset := tc.val.Zone()
			assert.Equal(t, originalOffset, decodedOffset, "offset mismatch")
		})
	}
}

func TestEncodeDateTime(t *testing.T) {
	testCases := []struct {
		name string
		val  time.Time
	}{
		{
			name: "epoch Jan 1 1900 midnight",
			val:  time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "Jan 1 2000 noon",
			val:  time.Date(2000, 1, 1, 12, 0, 0, 0, time.UTC),
		},
		{
			name: "with seconds",
			val:  time.Date(2020, 6, 15, 14, 30, 45, 0, time.UTC),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			encoded := encodeDateTime(tc.val)
			assert.Len(t, encoded, 8, "encodeDateTime() byte length")
			// Verify roundtrip
			decoded := decodeDateTime(encoded, time.UTC)
			// Allow for 1/300th second rounding
			diff := decoded.Sub(tc.val)
			if diff < 0 {
				diff = -diff
			}
			assert.LessOrEqual(t, diff, 4*time.Millisecond, "roundtrip mismatch: got %v, want %v (diff: %v)", decoded, tc.val, diff)
		})
	}
}

func TestDecodeTime(t *testing.T) {
	loc := time.UTC
	testCases := []struct {
		name  string
		scale uint8
		buf   []byte
		want  time.Time
	}{
		{
			name:  "midnight scale 0",
			scale: 0,
			buf:   []byte{0, 0, 0},
			want:  time.Date(1, 1, 1, 0, 0, 0, 0, loc),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeTime(tc.scale, tc.buf, loc)
			assert.True(t, got.Equal(tc.want), "decodeTime() = %v, want %v", got, tc.want)
		})
	}
}

func TestEncodeTime_Roundtrip(t *testing.T) {
	// Note: encodeTimeInt always writes 5 bytes, so we must use scale >= 5
	// to avoid buffer overflow
	testCases := []struct {
		name   string
		hour   int
		minute int
		second int
		ns     int
		scale  int
	}{
		{
			name:   "midnight scale 5",
			hour:   0,
			minute: 0,
			second: 0,
			ns:     0,
			scale:  5,
		},
		{
			name:   "noon scale 5",
			hour:   12,
			minute: 0,
			second: 0,
			ns:     0,
			scale:  5,
		},
		{
			name:   "with seconds scale 5",
			hour:   14,
			minute: 30,
			second: 45,
			ns:     0,
			scale:  5,
		},
		{
			name:   "with nanoseconds scale 7",
			hour:   10,
			minute: 15,
			second: 30,
			ns:     123456700,
			scale:  7,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			encoded := encodeTime(tc.hour, tc.minute, tc.second, tc.ns, tc.scale)
			decoded := decodeTime(uint8(tc.scale), encoded, time.UTC)

			// Build expected time (time only, date is Jan 1, year 1)
			expectedSeconds := tc.hour*3600 + tc.minute*60 + tc.second
			decodedSeconds := decoded.Hour()*3600 + decoded.Minute()*60 + decoded.Second()

			assert.Equal(t, expectedSeconds, decodedSeconds, "roundtrip seconds")
		})
	}
}

func TestDecodeDecimal(t *testing.T) {
	testCases := []struct {
		name  string
		prec  uint8
		scale uint8
		buf   []byte
		want  string
	}{
		{
			name:  "positive zero",
			prec:  10,
			scale: 2,
			// sign=1 (positive), then 4 bytes of zero
			buf:  []byte{1, 0, 0, 0, 0},
			want: "0.00",
		},
		{
			name:  "negative zero",
			prec:  10,
			scale: 2,
			// sign=0 (negative), then 4 bytes of zero
			buf:  []byte{0, 0, 0, 0, 0},
			want: "0.00",
		},
		{
			name:  "positive 100",
			prec:  10,
			scale: 0,
			// sign=1 (positive), value=100 in little-endian
			buf:  []byte{1, 100, 0, 0, 0},
			want: "100",
		},
		{
			name:  "positive 12.34",
			prec:  10,
			scale: 2,
			// sign=1 (positive), value=1234 (0x04D2) in little-endian
			buf:  []byte{1, 0xD2, 0x04, 0, 0},
			want: "12.34",
		},
		{
			name:  "negative 12.34",
			prec:  10,
			scale: 2,
			// sign=0 (negative), value=1234 (0x04D2) in little-endian
			buf:  []byte{0, 0xD2, 0x04, 0, 0},
			want: "-12.34",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeDecimal(tc.prec, tc.scale, tc.buf)
			gotStr := string(got)
			assert.Equal(t, tc.want, gotStr, "decodeDecimal()")
		})
	}
}

func TestDecodeTimeInt(t *testing.T) {
	testCases := []struct {
		name      string
		scale     uint8
		buf       []byte
		wantSec   int
		wantNsMin int // minimum expected ns
		wantNsMax int // maximum expected ns
	}{
		{
			name:      "midnight scale 0",
			scale:     0,
			buf:       []byte{0, 0, 0},
			wantSec:   0,
			wantNsMin: 0,
			wantNsMax: 0,
		},
		{
			name:      "one unit scale 0",
			scale:     0,
			buf:       []byte{1, 0, 0}, // value=1, which is 1 unit at scale 0
			wantSec:   1,               // at scale 0, 1 unit = 1 second
			wantNsMin: 0,
			wantNsMax: 0,
		},
		{
			name:      "ten seconds scale 0",
			scale:     0,
			buf:       []byte{10, 0, 0}, // value=10
			wantSec:   10,
			wantNsMin: 0,
			wantNsMax: 0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotSec, gotNs := decodeTimeInt(tc.scale, tc.buf)
			assert.Equal(t, tc.wantSec, gotSec, "decodeTimeInt() sec")
			assert.GreaterOrEqual(t, gotNs, tc.wantNsMin, "decodeTimeInt() ns min")
			assert.LessOrEqual(t, gotNs, tc.wantNsMax, "decodeTimeInt() ns max")
		})
	}
}

func TestDecodeUcs2(t *testing.T) {
	testCases := []struct {
		name  string
		input []byte
		want  string
	}{
		{
			name:  "empty",
			input: []byte{},
			want:  "",
		},
		{
			name: "ASCII hello",
			// "hello" in UCS-2 LE
			input: []byte{0x68, 0x00, 0x65, 0x00, 0x6C, 0x00, 0x6C, 0x00, 0x6F, 0x00},
			want:  "hello",
		},
		{
			name: "single A",
			// "A" in UCS-2 LE
			input: []byte{0x41, 0x00},
			want:  "A",
		},
		{
			name: "unicode smiley",
			// "☺" (U+263A) in UCS-2 LE
			input: []byte{0x3A, 0x26},
			want:  "☺",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeUcs2(tc.input)
			assert.Equal(t, tc.want, got, "decodeUcs2()")
		})
	}
}

func TestDecodeNChar(t *testing.T) {
	testCases := []struct {
		name  string
		input []byte
		want  string
	}{
		{
			name:  "empty",
			input: []byte{},
			want:  "",
		},
		{
			name:  "ASCII test",
			input: []byte{0x74, 0x00, 0x65, 0x00, 0x73, 0x00, 0x74, 0x00},
			want:  "test",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeNChar(tc.input)
			assert.Equal(t, tc.want, got, "decodeNChar()")
		})
	}
}

func TestDecodeUdt(t *testing.T) {
	ti := typeInfo{TypeId: typeUdt}
	input := []byte{1, 2, 3, 4, 5}
	got := decodeUdt(ti, input)

	assert.Equal(t, input, got, "decodeUdt()")
}

func TestDecodeXml(t *testing.T) {
	ti := typeInfo{TypeId: typeXml}
	// "<a/>" in UCS-2 LE
	input := []byte{0x3C, 0x00, 0x61, 0x00, 0x2F, 0x00, 0x3E, 0x00}
	want := "<a/>"

	got := decodeXml(ti, input)
	assert.Equal(t, want, got, "decodeXml()")
}

func TestWriteGuidType(t *testing.T) {
	tests := []struct {
		name           string
		size           int
		buf            []byte
		guidConversion bool
		wantLen        int
	}{
		{
			name:           "null guid - size 0",
			size:           0x00,
			buf:            []byte{},
			guidConversion: false,
			wantLen:        1, // just the length byte (0)
		},
		{
			name:           "valid guid - size 16 without conversion",
			size:           0x10,
			buf:            []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10},
			guidConversion: false,
			wantLen:        17, // 1 length byte + 16 data bytes
		},
		{
			name:           "valid guid - size 16 with conversion",
			size:           0x10,
			buf:            []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10},
			guidConversion: true,
			wantLen:        17, // 1 length byte + 16 data bytes
		},
		{
			name:           "empty buffer with size 16",
			size:           0x10,
			buf:            []byte{},
			guidConversion: false,
			wantLen:        17, // 1 length byte (0) + 16 zero-filled data bytes (size=16 path)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var w bytes.Buffer
			ti := typeInfo{Size: tt.size}
			enc := msdsn.EncodeParameters{GuidConversion: tt.guidConversion}

			err := writeGuidType(&w, ti, tt.buf, enc)
			assert.NoError(t, err, "writeGuidType()")

			assert.Equal(t, tt.wantLen, w.Len(), "writeGuidType() byte count")

			// Verify length byte is correct
			result := w.Bytes()
			if len(result) > 0 {
				assert.Equal(t, len(tt.buf), int(result[0]), "writeGuidType() length byte")
			}
		})
	}
}
