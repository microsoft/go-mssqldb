package mssql

import (
	"bytes"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type closeCountingConn struct {
	net.Conn
	closeCalls atomic.Int32
	closeWait  <-chan struct{}
	closeErr   error
}

func (c *closeCountingConn) Close() error {
	c.closeCalls.Add(1)
	if c.closeWait != nil {
		<-c.closeWait
	}
	err := c.Conn.Close()
	if c.closeErr != nil {
		return c.closeErr
	}
	return err
}

func TestTimeoutConn_CloseOnce(t *testing.T) {
	for _, closeErr := range []error{nil, errors.New("transport close failed")} {
		name := "success"
		if closeErr != nil {
			name = "error"
		}
		t.Run(name, func(t *testing.T) {
			raw := &closeCountingConn{
				Conn:     &mockConn{Buffer: &bytes.Buffer{}},
				closeErr: closeErr,
			}
			conn := newTimeoutConn(raw, 0)

			n, err := conn.Write([]byte("data"))
			require.NoError(t, err)
			require.Equal(t, 4, n)
			data := make([]byte, 4)
			_, err = io.ReadFull(conn, data)
			require.NoError(t, err)
			require.Equal(t, "data", string(data))
			assert.Zero(t, raw.closeCalls.Load(), "healthy I/O must not close the transport")

			// Strict TLS uses conn.c, while the other paths retain conn.
			closers := []io.Closer{
				conn,
				conn.c,
				&passthroughConn{c: conn},
				&tlsHandshakeConn{buf: &tdsBuffer{transport: conn}},
			}
			for range 2 {
				for _, closer := range closers {
					assert.ErrorIs(t, closer.Close(), closeErr)
					assert.EqualValues(t, 1, raw.closeCalls.Load(),
						"all wrappers must share one close for the transport's lifetime")
				}
			}
		})
	}
}

func TestSendAttentionWithTimeout_ClosesTransportOnce(t *testing.T) {
	for _, closeErr := range []error{nil, errors.New("transport close failed")} {
		name := "success"
		if closeErr != nil {
			name = "error"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				client, server := net.Pipe()
				defer client.Close()
				defer server.Close()
				closeWait := make(chan struct{})
				releaseClose := sync.OnceFunc(func() { close(closeWait) })
				defer releaseClose()

				raw := &closeCountingConn{
					Conn:      client,
					closeWait: closeWait,
					closeErr:  closeErr,
				}
				transport := newTimeoutConn(raw, 0)
				bufferReturned := false
				conn := &Conn{sess: &tdsSession{buf: &tdsBuffer{
					transport: transport,
					bufClose:  func() { bufferReturned = true },
				}}}

				// The peer never reads, and Close cannot finish until released.
				err := sendAttentionWithTimeout(transport, time.Second)
				require.ErrorContains(t, err, "attention write timed out")
				synctest.Wait()
				require.EqualValues(t, 1, raw.closeCalls.Load())
				assert.False(t, bufferReturned)

				result := make(chan error, 1)
				go func() { result <- conn.Close() }()
				releaseClose()
				synctest.Wait()
				require.Len(t, result, 1)
				assert.ErrorIs(t, <-result, closeErr)
				assert.True(t, bufferReturned)
				assert.EqualValues(t, 1, raw.closeCalls.Load(),
					"timeout cleanup and pool eviction must share one close")
				assert.ErrorIs(t, transport.Close(), closeErr)
				assert.EqualValues(t, 1, raw.closeCalls.Load(),
					"later closes must not retry the raw transport close")
			})
		})
	}
}
