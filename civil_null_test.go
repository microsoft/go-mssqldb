//go:build go1.9
// +build go1.9

package mssql

import (
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
