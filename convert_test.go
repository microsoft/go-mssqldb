package mssql

import (
	"database/sql"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// extractDest extracts the value from a destination pointer for test comparison.
func extractDest(dest interface{}) interface{} {
	v := reflect.ValueOf(dest)
	if v.Kind() == reflect.Ptr && !v.IsNil() {
		return v.Elem().Interface()
	}
	return nil
}

func TestConvertAssign_String(t *testing.T) {
	tests := []struct {
		name    string
		src     interface{}
		dest    interface{}
		want    interface{}
		wantErr bool
	}{
		{
			name: "string to string",
			src:  "test",
			dest: new(string),
			want: "test",
		},
		{
			name: "string to []byte",
			src:  "test",
			dest: new([]byte),
			want: []byte("test"),
		},
		{
			name: "string to RawBytes",
			src:  "test",
			dest: new(sql.RawBytes),
			want: sql.RawBytes("test"),
		},
		{
			name:    "string to nil string pointer",
			src:     "test",
			dest:    (*string)(nil),
			wantErr: true,
		},
		{
			name:    "string to nil []byte pointer",
			src:     "test",
			dest:    (*[]byte)(nil),
			wantErr: true,
		},
		{
			name:    "string to nil RawBytes pointer",
			src:     "test",
			dest:    (*sql.RawBytes)(nil),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := convertAssign(tt.dest, tt.src)
			if tt.wantErr {
				assert.Error(t, err, "convertAssign()")
				return
			}
			assert.NoError(t, err, "convertAssign()")
			assert.Equal(t, tt.want, extractDest(tt.dest), "convertAssign()")
		})
	}
}

func TestConvertAssign_Bytes(t *testing.T) {
	tests := []struct {
		name    string
		src     interface{}
		dest    interface{}
		want    interface{}
		wantErr bool
	}{
		{
			name: "[]byte to string",
			src:  []byte("test"),
			dest: new(string),
			want: "test",
		},
		{
			name: "[]byte to interface{}",
			src:  []byte("test"),
			dest: new(interface{}),
			want: []byte("test"),
		},
		{
			name: "[]byte to []byte",
			src:  []byte("test"),
			dest: new([]byte),
			want: []byte("test"),
		},
		{
			name: "[]byte to RawBytes",
			src:  []byte("test"),
			dest: new(sql.RawBytes),
			want: sql.RawBytes("test"),
		},
		{
			name:    "[]byte to nil string pointer",
			src:     []byte("test"),
			dest:    (*string)(nil),
			wantErr: true,
		},
		{
			name:    "[]byte to nil interface pointer",
			src:     []byte("test"),
			dest:    (*interface{})(nil),
			wantErr: true,
		},
		{
			name:    "[]byte to nil []byte pointer",
			src:     []byte("test"),
			dest:    (*[]byte)(nil),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := convertAssign(tt.dest, tt.src)
			if tt.wantErr {
				assert.Error(t, err, "convertAssign()")
				return
			}
			assert.NoError(t, err, "convertAssign()")
			assert.Equal(t, tt.want, extractDest(tt.dest), "convertAssign()")
		})
	}
}

func TestConvertAssign_Time(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name    string
		src     interface{}
		dest    interface{}
		check   func(interface{}) bool
		wantErr bool
	}{
		{
			name: "time.Time to time.Time",
			src:  now,
			dest: new(time.Time),
			check: func(got interface{}) bool {
				return got.(time.Time).Equal(now)
			},
		},
		{
			name: "time.Time to string",
			src:  now,
			dest: new(string),
			check: func(got interface{}) bool {
				return got.(string) == now.Format(time.RFC3339Nano)
			},
		},
		{
			name: "time.Time to []byte",
			src:  now,
			dest: new([]byte),
			check: func(got interface{}) bool {
				return string(got.([]byte)) == now.Format(time.RFC3339Nano)
			},
		},
		{
			name: "time.Time to RawBytes",
			src:  now,
			dest: new(sql.RawBytes),
			check: func(got interface{}) bool {
				return string(got.(sql.RawBytes)) == now.Format(time.RFC3339Nano)
			},
		},
		{
			name:    "time.Time to nil []byte pointer",
			src:     now,
			dest:    (*[]byte)(nil),
			wantErr: true,
		},
		{
			name:    "time.Time to nil RawBytes pointer",
			src:     now,
			dest:    (*sql.RawBytes)(nil),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := convertAssign(tt.dest, tt.src)
			if tt.wantErr {
				assert.Error(t, err, "convertAssign()")
				return
			}
			assert.NoError(t, err, "convertAssign()")
			assert.True(t, tt.check(extractDest(tt.dest)), "convertAssign() validation failed")
		})
	}
}

func TestConvertAssign_Nil(t *testing.T) {
	tests := []struct {
		name    string
		dest    interface{}
		wantErr bool
	}{
		{
			name:    "nil to interface{}",
			dest:    new(interface{}),
			wantErr: false,
		},
		{
			name:    "nil to []byte",
			dest:    new([]byte),
			wantErr: false,
		},
		{
			name:    "nil to RawBytes",
			dest:    new(sql.RawBytes),
			wantErr: false,
		},
		{
			name:    "nil to nil interface pointer",
			dest:    (*interface{})(nil),
			wantErr: true,
		},
		{
			name:    "nil to nil []byte pointer",
			dest:    (*[]byte)(nil),
			wantErr: true,
		},
		{
			name:    "nil to nil RawBytes pointer",
			dest:    (*sql.RawBytes)(nil),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := convertAssign(tt.dest, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("convertAssign() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConvertAssign_Numeric(t *testing.T) {
	tests := []struct {
		name    string
		src     interface{}
		dest    interface{}
		want    interface{}
		wantErr bool
	}{
		// Bool conversions
		{
			name: "bool true to string",
			src:  true,
			dest: new(string),
			want: "true",
		},
		{
			name: "bool false to string",
			src:  false,
			dest: new(string),
			want: "false",
		},
		// Int conversions
		{
			name: "int to string",
			src:  42,
			dest: new(string),
			want: "42",
		},
		{
			name: "int8 to string",
			src:  int8(42),
			dest: new(string),
			want: "42",
		},
		{
			name: "int16 to string",
			src:  int16(42),
			dest: new(string),
			want: "42",
		},
		{
			name: "int32 to string",
			src:  int32(42),
			dest: new(string),
			want: "42",
		},
		{
			name: "int64 to string",
			src:  int64(42),
			dest: new(string),
			want: "42",
		},
		// Uint conversions
		{
			name: "uint to string",
			src:  uint(42),
			dest: new(string),
			want: "42",
		},
		{
			name: "uint8 to string",
			src:  uint8(42),
			dest: new(string),
			want: "42",
		},
		{
			name: "uint16 to string",
			src:  uint16(42),
			dest: new(string),
			want: "42",
		},
		{
			name: "uint32 to string",
			src:  uint32(42),
			dest: new(string),
			want: "42",
		},
		{
			name: "uint64 to string",
			src:  uint64(42),
			dest: new(string),
			want: "42",
		},
		// Float conversions
		{
			name: "float32 to string",
			src:  float32(3.14),
			dest: new(string),
			want: "3.14",
		},
		{
			name: "float64 to string",
			src:  float64(3.14),
			dest: new(string),
			want: "3.14",
		},
		// []byte conversions for numeric types
		{
			name: "int to []byte",
			src:  42,
			dest: new([]byte),
			want: []byte("42"),
		},
		{
			name: "uint to []byte",
			src:  uint(42),
			dest: new([]byte),
			want: []byte("42"),
		},
		{
			name: "float64 to []byte",
			src:  float64(3.14),
			dest: new([]byte),
			want: []byte("3.14"),
		},
		// RawBytes conversions
		{
			name: "int to RawBytes",
			src:  42,
			dest: new(sql.RawBytes),
			want: sql.RawBytes("42"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := convertAssign(tt.dest, tt.src)
			if tt.wantErr {
				assert.Error(t, err, "convertAssign()")
				return
			}
			assert.NoError(t, err, "convertAssign()")
			assert.Equal(t, tt.want, extractDest(tt.dest), "convertAssign()")
		})
	}
}

func TestConvertAssign_Bool(t *testing.T) {
	tests := []struct {
		name    string
		src     interface{}
		want    bool
		wantErr bool
	}{
		{
			name: "bool true",
			src:  true,
			want: true,
		},
		{
			name: "bool false",
			src:  false,
			want: false,
		},
		{
			name: "int 1 to bool",
			src:  int64(1),
			want: true,
		},
		{
			name: "int 0 to bool",
			src:  int64(0),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var dest bool
			err := convertAssign(&dest, tt.src)
			if tt.wantErr {
				assert.Error(t, err, "convertAssign()")
			} else {
				assert.NoError(t, err, "convertAssign()")
			}
			if tt.wantErr {
				return
			}
			if !tt.wantErr && dest != tt.want {
				t.Errorf("convertAssign() got = %v, want %v", dest, tt.want)
			}
		})
	}
}

func TestConvertAssign_Interface(t *testing.T) {
	tests := []struct {
		name string
		src  interface{}
		want interface{}
	}{
		{
			name: "string to interface{}",
			src:  "test",
			want: "test",
		},
		{
			name: "int to interface{}",
			src:  42,
			want: 42,
		},
		{
			name: "[]byte to interface{}",
			src:  []byte("test"),
			want: []byte("test"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var dest interface{}
			err := convertAssign(&dest, tt.src)
			assert.NoError(t, err, "convertAssign()")
			if err != nil {
				return
			}
			assert.Equal(t, tt.want, dest, "convertAssign()")
		})
	}
}

func TestConvertAssign_Errors(t *testing.T) {
	tests := []struct {
		name string
		dest interface{}
		src  interface{}
	}{
		{
			name: "not a pointer",
			dest: "not a pointer",
			src:  "value",
		},
		{
			name: "nil pointer",
			dest: (*int)(nil),
			src:  42,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := convertAssign(tt.dest, tt.src)
			assert.Error(t, err, "convertAssign() expected error but got nil")
		})
	}
}

func TestConvertAssign_StringToInt(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		dest    interface{}
		check   func(interface{}) bool
		wantErr bool
	}{
		{
			name: "string to int",
			src:  "42",
			dest: new(int),
			check: func(got interface{}) bool {
				return got.(int) == 42
			},
		},
		{
			name: "string to int8",
			src:  "42",
			dest: new(int8),
			check: func(got interface{}) bool {
				return got.(int8) == 42
			},
		},
		{
			name: "string to int16",
			src:  "42",
			dest: new(int16),
			check: func(got interface{}) bool {
				return got.(int16) == 42
			},
		},
		{
			name: "string to int32",
			src:  "42",
			dest: new(int32),
			check: func(got interface{}) bool {
				return got.(int32) == 42
			},
		},
		{
			name: "string to int64",
			src:  "42",
			dest: new(int64),
			check: func(got interface{}) bool {
				return got.(int64) == 42
			},
		},
		{
			name:    "invalid string to int",
			src:     "not a number",
			dest:    new(int),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := convertAssign(tt.dest, tt.src)
			if tt.wantErr {
				assert.Error(t, err, "convertAssign()")
				return
			}
			assert.NoError(t, err, "convertAssign()")
			assert.True(t, tt.check(extractDest(tt.dest)), "convertAssign() validation failed")
		})
	}
}

func TestConvertAssign_StringToUint(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		dest    interface{}
		check   func(interface{}) bool
		wantErr bool
	}{
		{
			name: "string to uint",
			src:  "42",
			dest: new(uint),
			check: func(got interface{}) bool {
				return got.(uint) == 42
			},
		},
		{
			name: "string to uint8",
			src:  "42",
			dest: new(uint8),
			check: func(got interface{}) bool {
				return got.(uint8) == 42
			},
		},
		{
			name: "string to uint16",
			src:  "42",
			dest: new(uint16),
			check: func(got interface{}) bool {
				return got.(uint16) == 42
			},
		},
		{
			name: "string to uint32",
			src:  "42",
			dest: new(uint32),
			check: func(got interface{}) bool {
				return got.(uint32) == 42
			},
		},
		{
			name: "string to uint64",
			src:  "42",
			dest: new(uint64),
			check: func(got interface{}) bool {
				return got.(uint64) == 42
			},
		},
		{
			name:    "invalid string to uint",
			src:     "not a number",
			dest:    new(uint),
			wantErr: true,
		},
		{
			name:    "negative string to uint",
			src:     "-42",
			dest:    new(uint),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := convertAssign(tt.dest, tt.src)
			if tt.wantErr {
				assert.Error(t, err, "convertAssign()")
				return
			}
			assert.NoError(t, err, "convertAssign()")
			assert.True(t, tt.check(extractDest(tt.dest)), "convertAssign() validation failed")
		})
	}
}

func TestConvertAssign_StringToFloat(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		dest    interface{}
		check   func(interface{}) bool
		wantErr bool
	}{
		{
			name: "string to float32",
			src:  "3.14",
			dest: new(float32),
			check: func(got interface{}) bool {
				return got.(float32) > 3.13 && got.(float32) < 3.15
			},
		},
		{
			name: "string to float64",
			src:  "3.14",
			dest: new(float64),
			check: func(got interface{}) bool {
				return got.(float64) > 3.13 && got.(float64) < 3.15
			},
		},
		{
			name:    "invalid string to float",
			src:     "not a number",
			dest:    new(float64),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := convertAssign(tt.dest, tt.src)
			if tt.wantErr {
				assert.Error(t, err, "convertAssign()")
			} else {
				assert.NoError(t, err, "convertAssign()")
			}
			if tt.wantErr {
				return
			}
			assert.True(t, tt.check(extractDest(tt.dest)), "convertAssign() validation failed")
		})
	}
}

func TestConvertAssign_StringDest(t *testing.T) {
	tests := []struct {
		name string
		src  interface{}
		want string
	}{
		{
			name: "string src to string dest",
			src:  "test",
			want: "test",
		},
		{
			name: "[]byte src to string dest",
			src:  []byte("test"),
			want: "test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var dest string
			err := convertAssign(&dest, tt.src)
			assert.NoError(t, err, "convertAssign()")
			if err != nil {
				return
			}
			assert.Equal(t, tt.want, dest, "convertAssign()")
		})
	}
}

func TestConvertAssign_PointerDest(t *testing.T) {
	tests := []struct {
		name    string
		src     interface{}
		dest    interface{}
		wantNil bool
	}{
		{
			name:    "nil to pointer",
			src:     nil,
			dest:    new(*int),
			wantNil: true,
		},
		{
			name:    "value to pointer",
			src:     42,
			dest:    new(*int),
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := convertAssign(tt.dest, tt.src)
			assert.NoError(t, err, "convertAssign()")
			if err != nil {
				return
			}
			destVal := reflect.ValueOf(tt.dest).Elem()
			if tt.wantNil {
				if !destVal.IsNil() {
					t.Error("convertAssign() expected nil pointer")
				}
			} else {
				if destVal.IsNil() {
					t.Error("convertAssign() expected non-nil pointer")
				}
			}
		})
	}
}

func TestCloneBytes(t *testing.T) {
	tests := []struct {
		name string
		b    []byte
		want []byte
	}{
		{
			name: "clone byte slice",
			b:    []byte("test"),
			want: []byte("test"),
		},
		{
			name: "clone empty byte slice",
			b:    []byte{},
			want: []byte{},
		},
		{
			name: "clone nil byte slice",
			b:    nil,
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cloneBytes(tt.b)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("cloneBytes() = %v, want %v", got, tt.want)
			}
			// Verify it's a different slice
			if tt.b != nil && len(tt.b) > 0 && &got[0] == &tt.b[0] {
				t.Error("cloneBytes() did not create a new slice")
			}
		})
	}
}

func TestAsString(t *testing.T) {
	tests := []struct {
		name string
		src  interface{}
		want string
	}{
		{
			name: "string",
			src:  "test",
			want: "test",
		},
		{
			name: "[]byte",
			src:  []byte("test"),
			want: "test",
		},
		{
			name: "int",
			src:  42,
			want: "42",
		},
		{
			name: "int8",
			src:  int8(42),
			want: "42",
		},
		{
			name: "int16",
			src:  int16(42),
			want: "42",
		},
		{
			name: "int32",
			src:  int32(42),
			want: "42",
		},
		{
			name: "int64",
			src:  int64(42),
			want: "42",
		},
		{
			name: "uint",
			src:  uint(42),
			want: "42",
		},
		{
			name: "uint8",
			src:  uint8(42),
			want: "42",
		},
		{
			name: "uint16",
			src:  uint16(42),
			want: "42",
		},
		{
			name: "uint32",
			src:  uint32(42),
			want: "42",
		},
		{
			name: "uint64",
			src:  uint64(42),
			want: "42",
		},
		{
			name: "float32",
			src:  float32(3.14),
			want: "3.14",
		},
		{
			name: "float64",
			src:  float64(3.14),
			want: "3.14",
		},
		{
			name: "bool true",
			src:  true,
			want: "true",
		},
		{
			name: "bool false",
			src:  false,
			want: "false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := asString(tt.src)
			if got != tt.want {
				t.Errorf("asString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAsBytes(t *testing.T) {
	tests := []struct {
		name   string
		buf    []byte
		src    interface{}
		wantOk bool
	}{
		{
			name:   "int",
			buf:    nil,
			src:    42,
			wantOk: true,
		},
		{
			name:   "uint",
			buf:    nil,
			src:    uint(42),
			wantOk: true,
		},
		{
			name:   "float32",
			buf:    nil,
			src:    float32(3.14),
			wantOk: true,
		},
		{
			name:   "float64",
			buf:    nil,
			src:    float64(3.14),
			wantOk: true,
		},
		{
			name:   "bool",
			buf:    nil,
			src:    true,
			wantOk: true,
		},
		{
			name:   "string",
			buf:    nil,
			src:    "test",
			wantOk: true,
		},
		{
			name:   "unsupported type",
			buf:    nil,
			src:    struct{}{},
			wantOk: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rv := reflect.ValueOf(tt.src)
			_, ok := asBytes(tt.buf, rv)
			if ok != tt.wantOk {
				t.Errorf("asBytes() ok = %v, want %v", ok, tt.wantOk)
			}
		})
	}
}
