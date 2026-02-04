package mssql

import (
	"bytes"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// mockConn implements a basic net.Conn for testing
type mockConn struct {
	*bytes.Buffer
	closed bool
}

func (m *mockConn) Close() error {
	m.closed = true
	return nil
}

func (m *mockConn) LocalAddr() net.Addr                { return nil }
func (m *mockConn) RemoteAddr() net.Addr               { return nil }
func (m *mockConn) SetDeadline(t time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockConn) SetWriteDeadline(t time.Time) error { return nil }

// errorConn is a mock that always returns errors
type errorConn struct {
	mockConn
}

func (e *errorConn) Write(b []byte) (int, error) {
	return 0, errors.New("mock write error")
}

func TestTlsHandshakeConn_FinishPacket(t *testing.T) {
	tests := []struct {
		name          string
		packetPending bool
		wantFinished  bool
		wantData      []byte // Expected data written to buffer
	}{
		{
			name:          "no pending packet",
			packetPending: false,
			wantFinished:  false,
			wantData:      nil,
		},
		{
			name:          "pending packet success",
			packetPending: true,
			wantFinished:  true,
			wantData:      []byte{byte(packPrelogin), 1, 0, 8, 0, 0, 1, 0}, // Header for empty packet
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock connection with a buffer
			mockConn := &mockConn{Buffer: &bytes.Buffer{}}
			buf := newTdsBuffer(defaultPacketSize, mockConn)

			conn := &tlsHandshakeConn{
				buf:           buf,
				packetPending: tt.packetPending,
			}

			// If we expect a pending packet, begin one
			if tt.packetPending {
				buf.BeginPacket(packPrelogin, false)
			}

			finished, err := conn.FinishPacket()

			if err != nil {
				t.Errorf("FinishPacket() unexpected error = %v", err)
			}
			if finished != tt.wantFinished {
				t.Errorf("FinishPacket() finished = %v, want %v", finished, tt.wantFinished)
			}

			// Verify packetPending is cleared after successful finish
			if tt.packetPending && conn.packetPending {
				t.Error("FinishPacket() did not clear packetPending flag")
			}

			// Check if correct data was written
			if tt.wantData != nil {
				written := mockConn.Bytes()
				if !bytes.Equal(written, tt.wantData) {
					t.Errorf("FinishPacket() wrote %v, want %v", written, tt.wantData)
				}
			}
		})
	}
}

func TestTlsHandshakeConn_FinishPacket_Error(t *testing.T) {
	// Test error handling when buf.FinishPacket() fails
	errorConn := &errorConn{mockConn{Buffer: &bytes.Buffer{}}}
	buf := newTdsBuffer(defaultPacketSize, errorConn)

	conn := &tlsHandshakeConn{
		buf:           buf,
		packetPending: true,
	}

	// Begin a packet
	buf.BeginPacket(packPrelogin, false)

	finished, err := conn.FinishPacket()

	if err == nil {
		t.Error("FinishPacket() expected error but got nil")
	}
	if finished {
		t.Error("FinishPacket() should return false on error")
	}
	// Verify packetPending is NOT cleared on error
	if !conn.packetPending {
		t.Error("FinishPacket() should NOT clear packetPending on error")
	}

	// Verify error wrapping
	if err != nil && err.Error() != "cannot send handshake packet: mock write error" {
		t.Errorf("FinishPacket() error = %v, want proper error wrapping", err)
	}
}

func TestTimeoutConn_Read(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
		data    []byte
		wantN   int
		wantErr bool
	}{
		{
			name:    "read with timeout",
			timeout: 5 * time.Second,
			data:    []byte("test data"),
			wantN:   9,
			wantErr: false,
		},
		{
			name:    "read without timeout",
			timeout: 0,
			data:    []byte("test"),
			wantN:   4,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockConn{Buffer: bytes.NewBuffer(tt.data)}
			tc := newTimeoutConn(mock, tt.timeout)

			buf := make([]byte, 100)
			n, err := tc.Read(buf)

			if tt.wantErr {
				assert.Error(t, err, "Read()")
			} else {
				assert.NoError(t, err, "Read()")
			}
			assert.Equal(t, tt.wantN, n, "Read() bytes read")
		})
	}
}

func TestTimeoutConn_Write(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
		data    []byte
		wantN   int
		wantErr bool
	}{
		{
			name:    "write with timeout",
			timeout: 5 * time.Second,
			data:    []byte("test data"),
			wantN:   9,
			wantErr: false,
		},
		{
			name:    "write without timeout",
			timeout: 0,
			data:    []byte("test"),
			wantN:   4,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockConn{Buffer: &bytes.Buffer{}}
			tc := newTimeoutConn(mock, tt.timeout)

			n, err := tc.Write(tt.data)

			if tt.wantErr {
				assert.Error(t, err, "Write()")
			} else {
				assert.NoError(t, err, "Write()")
			}
			assert.Equal(t, tt.wantN, n, "Write() bytes written")
		})
	}
}

func TestTimeoutConn_Close(t *testing.T) {
	mock := &mockConn{Buffer: &bytes.Buffer{}}
	tc := newTimeoutConn(mock, 5*time.Second)

	err := tc.Close()
	assert.NoError(t, err, "Close()")
	assert.True(t, mock.closed, "Close() should close underlying connection")
}

func TestTimeoutConn_Addr(t *testing.T) {
	mock := &mockConn{Buffer: &bytes.Buffer{}}
	tc := newTimeoutConn(mock, 5*time.Second)

	assert.Nil(t, tc.LocalAddr(), "LocalAddr() should return nil from mockConn")
	assert.Nil(t, tc.RemoteAddr(), "RemoteAddr() should return nil from mockConn")
}

func TestTlsHandshakeConn_Close(t *testing.T) {
	mock := &mockConn{Buffer: &bytes.Buffer{}}
	buf := newTdsBuffer(defaultPacketSize, mock)
	conn := &tlsHandshakeConn{buf: buf}

	err := conn.Close()
	assert.NoError(t, err, "Close()")
	assert.True(t, mock.closed, "Close() should close underlying connection")
}

func TestTlsHandshakeConn_Addr(t *testing.T) {
	mock := &mockConn{Buffer: &bytes.Buffer{}}
	buf := newTdsBuffer(defaultPacketSize, mock)
	conn := &tlsHandshakeConn{buf: buf}

	assert.Nil(t, conn.LocalAddr(), "LocalAddr() should return nil")
	assert.Nil(t, conn.RemoteAddr(), "RemoteAddr() should return nil")
}

func TestTlsHandshakeConn_SetDeadline(t *testing.T) {
	mock := &mockConn{Buffer: &bytes.Buffer{}}
	buf := newTdsBuffer(defaultPacketSize, mock)
	conn := &tlsHandshakeConn{buf: buf}

	deadline := time.Now().Add(5 * time.Second)
	
	assert.NoError(t, conn.SetDeadline(deadline), "SetDeadline()")
	assert.NoError(t, conn.SetReadDeadline(deadline), "SetReadDeadline()")
	assert.NoError(t, conn.SetWriteDeadline(deadline), "SetWriteDeadline()")
}
