package mssql

import (
	"fmt"
	"net"
	"time"
)

type timeoutConn struct {
	c net.Conn
	// timeout bounds both Read and Write by default. disableReadTimeout,
	// once set via disableTimeout(), stops Read from re-arming its
	// deadline (see disableTimeout doc comment below), while Write keeps
	// being bounded by timeout unconditionally, as a safety net against a
	// stuck send regardless of whether the read timeout was disabled.
	timeout            time.Duration
	disableReadTimeout bool
}

func newTimeoutConn(conn net.Conn, timeout time.Duration) *timeoutConn {
	return &timeoutConn{
		c:       conn,
		timeout: timeout,
	}
}

func (c *timeoutConn) Read(b []byte) (n int, err error) {
	if c.timeout > 0 && !c.disableReadTimeout {
		err = c.c.SetReadDeadline(time.Now().Add(c.timeout))
		if err != nil {
			return
		}
	}
	return c.c.Read(b)
}

func (c *timeoutConn) Write(b []byte) (n int, err error) {
	if c.timeout > 0 {
		// Always bound writes by timeout, even after disableTimeout(): a
		// request write can still block indefinitely if the server stops
		// consuming (e.g. a full TCP send window), and nothing observes
		// the caller's context until a response reader exists to receive
		// an ATTENTION acknowledgement. Bounding writes here guarantees
		// they cannot hang forever, independent of the read-timeout
		// behavior controlled by disableTimeout().
		err = c.c.SetWriteDeadline(time.Now().Add(c.timeout))
		if err != nil {
			return
		}
	}
	return c.c.Write(b)
}

// disableTimeout stops the connect timeout from being (re)applied as a
// socket *read* deadline, and clears any read deadline left over from the
// last login-phase Read call. It must be called once the login handshake
// has completed successfully, so that subsequent command execution is
// governed exclusively by the caller-supplied context.Context deadlines
// instead of being cut short by the connection timeout. Write deadlines are
// deliberately left untouched (see Write above): only the read side, which
// is what blocks for the duration of a long-running command, is affected.
func (c *timeoutConn) disableTimeout() error {
	c.disableReadTimeout = true
	if c.timeout <= 0 {
		// No deadline was ever armed by this wrapper (ConnTimeout==0), so
		// there is nothing to clear. Some net.Conn implementations
		// (e.g. from a custom Connector.Dialer) may not support deadlines
		// at all; avoid calling SetReadDeadline unless we know we
		// previously set one.
		return nil
	}
	return c.c.SetReadDeadline(time.Time{})
}

func (c timeoutConn) Close() error {
	return c.c.Close()
}

func (c timeoutConn) LocalAddr() net.Addr {
	return c.c.LocalAddr()
}

func (c timeoutConn) RemoteAddr() net.Addr {
	return c.c.RemoteAddr()
}

func (c timeoutConn) SetDeadline(t time.Time) error {
	return c.c.SetDeadline(t)
}

func (c timeoutConn) SetReadDeadline(t time.Time) error {
	return c.c.SetReadDeadline(t)
}

func (c timeoutConn) SetWriteDeadline(t time.Time) error {
	return c.c.SetWriteDeadline(t)
}

// this connection is used during TLS Handshake
// TDS protocol requires TLS handshake messages to be sent inside TDS packets
type tlsHandshakeConn struct {
	buf           *tdsBuffer
	packetPending bool
	continueRead  bool
}

func (c *tlsHandshakeConn) Read(b []byte) (n int, err error) {
	var finished bool
	finished, err = c.FinishPacket()

	if err != nil {
		return
	}

	if finished {
		c.continueRead = false
	}

	if !c.continueRead {
		var packet packetType
		packet, err = c.buf.BeginRead()
		if err != nil {
			err = fmt.Errorf("cannot read handshake packet: %w", err)
			return
		}
		// Older endpoints send back header type 4 instead of 18
		if packet != packPrelogin && packet != packReply {
			err = fmt.Errorf("unexpected packet %d, expecting prelogin", packet)
			return
		}
		c.continueRead = true
	}
	return c.buf.Read(b)
}

func (c *tlsHandshakeConn) Write(b []byte) (n int, err error) {
	if !c.packetPending {
		c.buf.BeginPacket(packPrelogin, false)
		c.packetPending = true
	}
	return c.buf.Write(b)
}

// FinishPacket flushes the current plaintext packet boundary before the
// underlying Conn is replaced during the TLS handshake upgrade.
func (c *tlsHandshakeConn) FinishPacket() (bool, error) {
	if c.packetPending {
		err := c.buf.FinishPacket()
		if err != nil {
			err = fmt.Errorf("cannot send handshake packet: %w", err)
			return false, err
		}
		c.packetPending = false
		return true, nil
	}
	return false, nil
}

func (c *tlsHandshakeConn) Close() error {
	return c.buf.transport.Close()
}

func (c *tlsHandshakeConn) LocalAddr() net.Addr {
	return nil
}

func (c *tlsHandshakeConn) RemoteAddr() net.Addr {
	return nil
}

func (c *tlsHandshakeConn) SetDeadline(_ time.Time) error {
	return nil
}

func (c *tlsHandshakeConn) SetReadDeadline(_ time.Time) error {
	return nil
}

func (c *tlsHandshakeConn) SetWriteDeadline(_ time.Time) error {
	return nil
}

// this connection just delegates all methods to it's wrapped connection
// it also allows switching underlying connection on the fly
// it is needed because tls.Conn does not allow switching underlying connection
type passthroughConn struct {
	c net.Conn
}

func (c passthroughConn) Read(b []byte) (n int, err error) {
	return c.c.Read(b)
}

func (c passthroughConn) Write(b []byte) (n int, err error) {
	return c.c.Write(b)
}

func (c passthroughConn) Close() error {
	return c.c.Close()
}

func (c passthroughConn) LocalAddr() net.Addr {
	return c.c.LocalAddr()
}

func (c passthroughConn) RemoteAddr() net.Addr {
	return c.c.RemoteAddr()
}

func (c passthroughConn) SetDeadline(t time.Time) error {
	return c.c.SetDeadline(t)
}

func (c passthroughConn) SetReadDeadline(t time.Time) error {
	return c.c.SetReadDeadline(t)
}

func (c passthroughConn) SetWriteDeadline(t time.Time) error {
	return c.c.SetWriteDeadline(t)
}
