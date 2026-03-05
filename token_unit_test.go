package mssql

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRWCBuffer_Read(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		readSize int
		wantN    int
		wantErr  error
		wantData string
	}{
		{
			name:     "read all data",
			data:     []byte("hello world"),
			readSize: 11,
			wantN:    11,
			wantErr:  nil,
			wantData: "hello world",
		},
		{
			name:     "read partial data",
			data:     []byte("hello world"),
			readSize: 5,
			wantN:    5,
			wantErr:  nil,
			wantData: "hello",
		},
		{
			name:     "read empty buffer",
			data:     []byte{},
			readSize: 10,
			wantN:    0,
			wantErr:  io.EOF,
			wantData: "",
		},
		{
			name:     "read with larger buffer",
			data:     []byte("hi"),
			readSize: 100,
			wantN:    2,
			wantErr:  nil,
			wantData: "hi",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rwc := RWCBuffer{buffer: bytes.NewReader(tt.data)}
			buf := make([]byte, tt.readSize)
			n, err := rwc.Read(buf)

			assert.Equal(t, tt.wantN, n, "Read() n")
			assert.Equal(t, tt.wantErr, err, "Read() error")

			if n > 0 {
				assert.Equal(t, tt.wantData, string(buf[:n]), "Read() data")
			}
		})
	}
}

func TestRWCBuffer_Write(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantN   int
		wantErr error
	}{
		{
			name:    "write returns zero",
			data:    []byte("hello"),
			wantN:   0,
			wantErr: nil,
		},
		{
			name:    "write empty slice",
			data:    []byte{},
			wantN:   0,
			wantErr: nil,
		},
		{
			name:    "write nil slice",
			data:    nil,
			wantN:   0,
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rwc := RWCBuffer{buffer: bytes.NewReader([]byte{})}
			n, err := rwc.Write(tt.data)

			assert.Equal(t, tt.wantN, n, "Write() n")
			assert.Equal(t, tt.wantErr, err, "Write() error")
		})
	}
}

func TestRWCBuffer_Close(t *testing.T) {
	rwc := RWCBuffer{buffer: bytes.NewReader([]byte("test"))}
	err := rwc.Close()

	assert.NoError(t, err, "Close() error")

	// Verify we can still read after close (RWCBuffer.Close is a no-op)
	buf := make([]byte, 4)
	n, _ := rwc.Read(buf)
	assert.Equal(t, 4, n, "After Close(), Read() n")
}

func TestRWCBuffer_MultipleReads(t *testing.T) {
	data := []byte("hello world")
	rwc := RWCBuffer{buffer: bytes.NewReader(data)}

	// First read
	buf1 := make([]byte, 5)
	n1, err1 := rwc.Read(buf1)
	assert.Equal(t, 5, n1, "First Read() n")
	assert.NoError(t, err1, "First Read() error")
	assert.Equal(t, "hello", string(buf1), "First Read() data")

	// Second read
	buf2 := make([]byte, 6)
	n2, err2 := rwc.Read(buf2)
	assert.Equal(t, 6, n2, "Second Read() n")
	assert.NoError(t, err2, "Second Read() error")
	assert.Equal(t, " world", string(buf2), "Second Read() data")

	// Third read (EOF)
	buf3 := make([]byte, 1)
	n3, err3 := rwc.Read(buf3)
	assert.Equal(t, 0, n3, "Third Read() n")
	assert.Equal(t, io.EOF, err3, "Third Read() error")
}
