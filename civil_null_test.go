package mssql

import (
	"bytes"
	"database/sql/driver"
	"testing"
	"time"

	"github.com/golang-sql/civil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNullDate_Scan(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		input     interface{}
		wantDate  civil.Date
		wantValid bool
		wantErr   bool
	}{
		{
			name:      "nil value",
			input:     nil,
			wantDate:  civil.Date{},
			wantValid: false,
		},
		{
			name:      "valid time.Time",
			input:     time.Date(2023, 12, 25, 14, 30, 0, 0, time.UTC),
			wantDate:  civil.Date{Year: 2023, Month: 12, Day: 25},
			wantValid: true,
		},
		{
			name:    "invalid type",
			input:   "not a time",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var n NullDate
			err := n.Scan(tc.input)

			if tc.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.wantValid, n.Valid)
			if tc.wantValid {
				assert.Equal(t, tc.wantDate, n.Date)
			}
		})
	}
}

func TestNullDate_Value(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		nullDate  NullDate
		wantValue driver.Value
	}{
		{
			name:      "invalid returns nil",
			nullDate:  NullDate{Valid: false},
			wantValue: nil,
		},
		{
			name:      "valid returns civil.Date",
			nullDate:  NullDate{Date: civil.Date{Year: 2023, Month: 12, Day: 25}, Valid: true},
			wantValue: civil.Date{Year: 2023, Month: 12, Day: 25},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.nullDate.Value()
			require.NoError(t, err)
			assert.Equal(t, tc.wantValue, got)
		})
	}
}

func TestNullDateTime_Scan(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		input        interface{}
		wantDateTime civil.DateTime
		wantValid    bool
		wantErr      bool
	}{
		{
			name:         "nil value",
			input:        nil,
			wantDateTime: civil.DateTime{},
			wantValid:    false,
		},
		{
			name:  "valid time.Time",
			input: time.Date(2023, 12, 25, 14, 30, 45, 0, time.UTC),
			wantDateTime: civil.DateTime{
				Date: civil.Date{Year: 2023, Month: 12, Day: 25},
				Time: civil.Time{Hour: 14, Minute: 30, Second: 45},
			},
			wantValid: true,
		},
		{
			name:    "invalid type",
			input:   123,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var n NullDateTime
			err := n.Scan(tc.input)

			if tc.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.wantValid, n.Valid)
			if tc.wantValid {
				assert.Equal(t, tc.wantDateTime, n.DateTime)
			}
		})
	}
}

func TestNullDateTime_Value(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		nullDateTime NullDateTime
		wantValue    driver.Value
	}{
		{
			name:         "invalid returns nil",
			nullDateTime: NullDateTime{Valid: false},
			wantValue:    nil,
		},
		{
			name: "valid returns civil.DateTime",
			nullDateTime: NullDateTime{
				DateTime: civil.DateTime{
					Date: civil.Date{Year: 2023, Month: 12, Day: 25},
					Time: civil.Time{Hour: 14, Minute: 30, Second: 45},
				},
				Valid: true,
			},
			wantValue: civil.DateTime{
				Date: civil.Date{Year: 2023, Month: 12, Day: 25},
				Time: civil.Time{Hour: 14, Minute: 30, Second: 45},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.nullDateTime.Value()
			require.NoError(t, err)
			assert.Equal(t, tc.wantValue, got)
		})
	}
}

func TestNullTime_Scan(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		input     interface{}
		wantTime  civil.Time
		wantValid bool
		wantErr   bool
	}{
		{
			name:      "nil value",
			input:     nil,
			wantTime:  civil.Time{},
			wantValid: false,
		},
		{
			name:      "valid time.Time",
			input:     time.Date(2023, 12, 25, 14, 30, 45, 123000000, time.UTC),
			wantTime:  civil.Time{Hour: 14, Minute: 30, Second: 45, Nanosecond: 123000000},
			wantValid: true,
		},
		{
			name:    "invalid type",
			input:   []byte{1, 2, 3},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var n NullTime
			err := n.Scan(tc.input)

			if tc.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.wantValid, n.Valid)
			if tc.wantValid {
				assert.Equal(t, tc.wantTime, n.Time)
			}
		})
	}
}

func TestNullTime_Value(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		nullTime  NullTime
		wantValue driver.Value
	}{
		{
			name:      "invalid returns nil",
			nullTime:  NullTime{Valid: false},
			wantValue: nil,
		},
		{
			name:      "valid returns civil.Time",
			nullTime:  NullTime{Time: civil.Time{Hour: 14, Minute: 30, Second: 45}, Valid: true},
			wantValue: civil.Time{Hour: 14, Minute: 30, Second: 45},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.nullTime.Value()
			require.NoError(t, err)
			assert.Equal(t, tc.wantValue, got)
		})
	}
}

func TestNullDate_String(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "NULL", NullDate{Valid: false}.String())
	assert.Equal(t, "2023-12-25", NullDate{Date: civil.Date{Year: 2023, Month: 12, Day: 25}, Valid: true}.String())
}

func TestNullDateTime_String(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "NULL", NullDateTime{Valid: false}.String())
	assert.Equal(t, "2023-12-25T14:30:45", NullDateTime{
		DateTime: civil.DateTime{
			Date: civil.Date{Year: 2023, Month: 12, Day: 25},
			Time: civil.Time{Hour: 14, Minute: 30, Second: 45},
		},
		Valid: true,
	}.String())
}

func TestNullTime_String(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "NULL", NullTime{Valid: false}.String())
	assert.Equal(t, "14:30:45", NullTime{Time: civil.Time{Hour: 14, Minute: 30, Second: 45}, Valid: true}.String())
}

func TestNullDate_MarshalText(t *testing.T) {
	t.Parallel()
	text, err := NullDate{Valid: false}.MarshalText()
	require.NoError(t, err)
	assert.Equal(t, []byte("null"), text)

	text, err = NullDate{Date: civil.Date{Year: 2023, Month: 12, Day: 25}, Valid: true}.MarshalText()
	require.NoError(t, err)
	assert.Equal(t, []byte("2023-12-25"), text)
}

func TestNullDateTime_MarshalText(t *testing.T) {
	t.Parallel()
	text, err := NullDateTime{Valid: false}.MarshalText()
	require.NoError(t, err)
	assert.Equal(t, []byte("null"), text)

	text, err = NullDateTime{
		DateTime: civil.DateTime{
			Date: civil.Date{Year: 2023, Month: 12, Day: 25},
			Time: civil.Time{Hour: 14, Minute: 30, Second: 45},
		},
		Valid: true,
	}.MarshalText()
	require.NoError(t, err)
	assert.Equal(t, []byte("2023-12-25T14:30:45"), text)
}

func TestNullTime_MarshalText(t *testing.T) {
	t.Parallel()
	text, err := NullTime{Valid: false}.MarshalText()
	require.NoError(t, err)
	assert.Equal(t, []byte("null"), text)

	text, err = NullTime{Time: civil.Time{Hour: 14, Minute: 30, Second: 45}, Valid: true}.MarshalText()
	require.NoError(t, err)
	assert.Equal(t, []byte("14:30:45"), text)
}

func TestNullDate_UnmarshalText(t *testing.T) {
	t.Parallel()
	var n NullDate
	require.NoError(t, n.UnmarshalText([]byte("null")))
	assert.False(t, n.Valid)

	require.NoError(t, n.UnmarshalText([]byte("2023-12-25")))
	assert.True(t, n.Valid)
	assert.Equal(t, civil.Date{Year: 2023, Month: 12, Day: 25}, n.Date)

	assert.Error(t, n.UnmarshalText([]byte("not-a-date")))
}

func TestNullDateTime_UnmarshalText(t *testing.T) {
	t.Parallel()
	var n NullDateTime
	require.NoError(t, n.UnmarshalText([]byte("null")))
	assert.False(t, n.Valid)

	require.NoError(t, n.UnmarshalText([]byte("2023-12-25T14:30:45")))
	assert.True(t, n.Valid)
	assert.Equal(t, civil.DateTime{
		Date: civil.Date{Year: 2023, Month: 12, Day: 25},
		Time: civil.Time{Hour: 14, Minute: 30, Second: 45},
	}, n.DateTime)

	assert.Error(t, n.UnmarshalText([]byte("not-a-datetime")))
}

func TestNullTime_UnmarshalText(t *testing.T) {
	t.Parallel()
	var n NullTime
	require.NoError(t, n.UnmarshalText([]byte("null")))
	assert.False(t, n.Valid)

	require.NoError(t, n.UnmarshalText([]byte("14:30:45")))
	assert.True(t, n.Valid)
	assert.Equal(t, civil.Time{Hour: 14, Minute: 30, Second: 45}, n.Time)

	assert.Error(t, n.UnmarshalText([]byte("not-a-time")))
}

func TestConvertInputParameter_NullCivilTypes(t *testing.T) {
	t.Parallel()

	t.Run("NullDate valid", func(t *testing.T) {
		input := NullDate{Date: civil.Date{Year: 2023, Month: 12, Day: 25}, Valid: true}
		got, err := convertInputParameter(input)
		require.NoError(t, err)
		assert.Equal(t, civil.Date{Year: 2023, Month: 12, Day: 25}, got)
	})

	t.Run("NullDate invalid", func(t *testing.T) {
		input := NullDate{Valid: false}
		got, err := convertInputParameter(input)
		require.NoError(t, err)
		assert.Equal(t, input, got)
	})

	t.Run("NullDateTime valid", func(t *testing.T) {
		input := NullDateTime{
			DateTime: civil.DateTime{
				Date: civil.Date{Year: 2023, Month: 12, Day: 25},
				Time: civil.Time{Hour: 14, Minute: 30, Second: 45},
			},
			Valid: true,
		}
		got, err := convertInputParameter(input)
		require.NoError(t, err)
		assert.Equal(t, input.DateTime, got)
	})

	t.Run("NullDateTime invalid", func(t *testing.T) {
		input := NullDateTime{Valid: false}
		got, err := convertInputParameter(input)
		require.NoError(t, err)
		assert.Equal(t, input, got)
	})

	t.Run("NullTime valid", func(t *testing.T) {
		input := NullTime{Time: civil.Time{Hour: 14, Minute: 30, Second: 45}, Valid: true}
		got, err := convertInputParameter(input)
		require.NoError(t, err)
		assert.Equal(t, civil.Time{Hour: 14, Minute: 30, Second: 45}, got)
	})

	t.Run("NullTime invalid", func(t *testing.T) {
		input := NullTime{Valid: false}
		got, err := convertInputParameter(input)
		require.NoError(t, err)
		assert.Equal(t, input, got)
	})
}

func TestMakeParam_NullCivilTypes(t *testing.T) {
	t.Parallel()
	s := &Stmt{}

	t.Run("NullDate null", func(t *testing.T) {
		res, err := s.makeParam(NullDate{Valid: false})
		require.NoError(t, err)
		assert.Equal(t, uint8(typeDateN), res.ti.TypeId)
		assert.Equal(t, 3, res.ti.Size)
		assert.Empty(t, res.buffer)
	})

	t.Run("NullDateTime null", func(t *testing.T) {
		res, err := s.makeParam(NullDateTime{Valid: false})
		require.NoError(t, err)
		assert.Equal(t, uint8(typeDateTime2N), res.ti.TypeId)
		assert.Equal(t, uint8(7), res.ti.Scale)
		assert.Equal(t, 8, res.ti.Size)
		assert.Empty(t, res.buffer)
	})

	t.Run("NullTime null", func(t *testing.T) {
		res, err := s.makeParam(NullTime{Valid: false})
		require.NoError(t, err)
		assert.Equal(t, uint8(typeTimeN), res.ti.TypeId)
		assert.Equal(t, uint8(7), res.ti.Scale)
		assert.Equal(t, 5, res.ti.Size)
		assert.Empty(t, res.buffer)
	})
}

func TestCreateZeroType_NullCivilTypes(t *testing.T) {
	t.Parallel()
	tvp := TVP{}

	t.Run("NullDate returns zero civil.Date", func(t *testing.T) {
		result := tvp.createZeroType(NullDate{})
		assert.Equal(t, civil.Date{}, result)
	})

	t.Run("NullDateTime returns zero civil.DateTime", func(t *testing.T) {
		result := tvp.createZeroType(NullDateTime{})
		assert.Equal(t, civil.DateTime{}, result)
	})

	t.Run("NullTime returns zero civil.Time", func(t *testing.T) {
		result := tvp.createZeroType(NullTime{})
		assert.Equal(t, civil.Time{}, result)
	})
}

func TestVerifyStandardTypeOnNull_NullCivilTypes(t *testing.T) {
	t.Parallel()
	tvp := TVP{}

	t.Run("NullDate null writes null byte", func(t *testing.T) {
		var buf bytes.Buffer
		result := tvp.verifyStandardTypeOnNull(&buf, NullDate{Valid: false})
		assert.True(t, result)
		assert.Equal(t, []byte{0}, buf.Bytes())
	})

	t.Run("NullDate valid returns false", func(t *testing.T) {
		var buf bytes.Buffer
		result := tvp.verifyStandardTypeOnNull(&buf, NullDate{Valid: true})
		assert.False(t, result)
		assert.Empty(t, buf.Bytes())
	})

	t.Run("NullDateTime null writes null byte", func(t *testing.T) {
		var buf bytes.Buffer
		result := tvp.verifyStandardTypeOnNull(&buf, NullDateTime{Valid: false})
		assert.True(t, result)
		assert.Equal(t, []byte{0}, buf.Bytes())
	})

	t.Run("NullDateTime valid returns false", func(t *testing.T) {
		var buf bytes.Buffer
		result := tvp.verifyStandardTypeOnNull(&buf, NullDateTime{Valid: true})
		assert.False(t, result)
		assert.Empty(t, buf.Bytes())
	})

	t.Run("NullTime null writes null byte", func(t *testing.T) {
		var buf bytes.Buffer
		result := tvp.verifyStandardTypeOnNull(&buf, NullTime{Valid: false})
		assert.True(t, result)
		assert.Equal(t, []byte{0}, buf.Bytes())
	})

	t.Run("NullTime valid returns false", func(t *testing.T) {
		var buf bytes.Buffer
		result := tvp.verifyStandardTypeOnNull(&buf, NullTime{Valid: true})
		assert.False(t, result)
		assert.Empty(t, buf.Bytes())
	})
}
