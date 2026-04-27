package mssql

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"net"
	"testing"
	"time"
)

// TestConnectClosesOnError verifies that when connect() encounters an error
// during login, the deferred Close in connect() closes the client-side TCP
// connection. The mock server keeps its end open and detects the client close
// by waiting for an EOF/error on a Read.
func TestConnectClosesOnError(t *testing.T) {
	clientClosed := make(chan struct{})

	addr := &net.TCPAddr{IP: net.IP{127, 0, 0, 1}}
	listener, err := net.ListenTCP("tcp", addr)
	if err != nil {
		t.Fatal("Cannot start listener:", err)
	}
	defer listener.Close()
	resolved := listener.Addr().(*net.TCPAddr)

	go func() {
		defer close(clientClosed)
		conn, err := listener.Accept()
		if err != nil {
			t.Errorf("listener.Accept failed: %v", err)
			return
		}
		// Keep the server side open so we can detect the client close.
		defer conn.Close()

		buf := newTdsBuffer(defaultPacketSize, conn)

		// Read the PRELOGIN request from the client.
		packetType, err := buf.BeginRead()
		if err != nil {
			t.Errorf("Failed to read PRELOGIN request: %v", err)
			return
		}
		if packetType != packPrelogin {
			t.Errorf("Client sent non PRELOGIN packet type %d", packetType)
			return
		}
		fields := map[uint8][]byte{
			preloginENCRYPTION: {encryptNotSup},
		}
		if err := writePrelogin(packReply, buf, fields); err != nil {
			t.Errorf("Writing PRELOGIN failed: %v", err)
			return
		}

		// Read the LOGIN packet the client sends, then do NOT respond.
		// connect() will read nothing useful and fail during login.
		packetType, err = buf.BeginRead()
		if err != nil {
			t.Errorf("Failed to read LOGIN request: %v", err)
			return
		}
		if packetType != packLogin7 {
			t.Errorf("Client sent non LOGIN packet type %d", packetType)
			return
		}

		// Wait for the client to close its end. The deferred close in
		// connect() should cause this read to return an error/EOF.
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		_, err = conn.Read(make([]byte, 1))
		if err == nil {
			t.Error("expected server-side read to fail after client close")
		} else if ne, ok := err.(net.Error); ok && ne.Timeout() {
			t.Error("server-side read timed out; client did not close the connection")
		}
	}()

	dsn := fmt.Sprintf("sqlserver://sa:unused@%s:%d?protocol=tcp&encrypt=disable&connection+timeout=1&dial+timeout=2",
		resolved.IP.String(), resolved.Port)
	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		t.Fatal("sql.Open failed:", err)
	}
	defer db.Close()

	err = db.Ping()
	if err == nil {
		t.Fatal("Expected Ping to fail, but it succeeded")
	}

	// Wait for the server goroutine to confirm the client closed the conn.
	select {
	case <-clientClosed:
	case <-time.After(10 * time.Second):
		t.Fatal("server goroutine did not detect client close within timeout")
	}
}

// routingRedirectMockServer starts a goroutine that performs a prelogin/login
// handshake with a connecting client, then sends a routing redirect to
// 127.0.0.1:1 (a port nothing listens on). It returns a channel that closes
// when the server goroutine detects the client has closed the first connection.
func routingRedirectMockServer(t *testing.T, listener *net.TCPListener) <-chan struct{} {
	t.Helper()
	firstConnClosed := make(chan struct{})

	go func() {
		defer close(firstConnClosed)
		conn, err := listener.Accept()
		if err != nil {
			t.Errorf("listener.Accept failed: %v", err)
			return
		}
		defer conn.Close()

		buf := newTdsBuffer(defaultPacketSize, conn)

		// Prelogin handshake (cannot use t.Fatal from a goroutine).
		packetType, err := buf.BeginRead()
		if err != nil {
			t.Errorf("Failed to read PRELOGIN request: %v", err)
			return
		}
		if packetType != packPrelogin {
			t.Errorf("Client sent non PRELOGIN request packet type %d", packetType)
			return
		}
		fields := map[uint8][]byte{
			preloginENCRYPTION: {encryptNotSup},
		}
		if err := writePrelogin(packReply, buf, fields); err != nil {
			t.Errorf("Writing PRELOGIN response failed: %v", err)
			return
		}

		// Login handshake
		packetType, err = buf.BeginRead()
		if err != nil {
			t.Errorf("Failed to read LOGIN request: %v", err)
			return
		}
		if packetType != packLogin7 {
			t.Errorf("Client sent non LOGIN request packet type %d", packetType)
			return
		}

		// Send a login response containing an ENVCHANGE routing token
		// that redirects to 127.0.0.1:1 (a port nothing is listening on),
		// a loginAck so the login loop exits, and a DONE token.
		buf.BeginPacket(packReply, false)

		routingServer := "127.0.0.1"
		serverUTF16Len := len(routingServer) * 2
		valueLength := 1 + 2 + 2 + serverUTF16Len // protocol + port + usVarCharLen + name
		envPayloadLen := 1 + 2 + valueLength + 2  // envtype + valueLength + value + oldValue

		if err := buf.WriteByte(byte(tokenEnvChange)); err != nil {
			t.Errorf("write tokenEnvChange failed: %v", err)
			return
		}
		if err := binary.Write(buf, binary.LittleEndian, uint16(envPayloadLen)); err != nil {
			t.Errorf("write envPayloadLen failed: %v", err)
			return
		}
		if err := buf.WriteByte(envRouting); err != nil {
			t.Errorf("write envRouting failed: %v", err)
			return
		}
		if err := binary.Write(buf, binary.LittleEndian, uint16(valueLength)); err != nil {
			t.Errorf("write valueLength failed: %v", err)
			return
		}
		if err := buf.WriteByte(0); err != nil { // TCP
			t.Errorf("write protocol failed: %v", err)
			return
		}
		if err := binary.Write(buf, binary.LittleEndian, uint16(1)); err != nil { // port 1
			t.Errorf("write port failed: %v", err)
			return
		}
		if err := binary.Write(buf, binary.LittleEndian, uint16(len(routingServer))); err != nil {
			t.Errorf("write serverNameLen failed: %v", err)
			return
		}
		for _, ch := range routingServer {
			if err := binary.Write(buf, binary.LittleEndian, uint16(ch)); err != nil {
				t.Errorf("write serverName char failed: %v", err)
				return
			}
		}
		if err := binary.Write(buf, binary.LittleEndian, uint16(0)); err != nil { // old value
			t.Errorf("write oldValue failed: %v", err)
			return
		}

		// loginAck token so the login-response loop exits.
		if err := buf.WriteByte(byte(tokenLoginAck)); err != nil {
			t.Errorf("write tokenLoginAck failed: %v", err)
			return
		}
		if err := binary.Write(buf, binary.LittleEndian, uint16(10)); err != nil { // payload size
			t.Errorf("write loginAck size failed: %v", err)
			return
		}
		if err := buf.WriteByte(1); err != nil { // Interface = SQL_TSQL
			t.Errorf("write loginAck interface failed: %v", err)
			return
		}
		if err := binary.Write(buf, binary.BigEndian, uint32(0x74000004)); err != nil { // TDSVersion
			t.Errorf("write TDSVersion failed: %v", err)
			return
		}
		if err := buf.WriteByte(0); err != nil { // ProgNameLen = 0
			t.Errorf("write progNameLen failed: %v", err)
			return
		}
		if err := binary.Write(buf, binary.BigEndian, uint32(0)); err != nil { // ProgVer
			t.Errorf("write progVer failed: %v", err)
			return
		}

		// DONE token
		if err := buf.WriteByte(byte(tokenDone)); err != nil {
			t.Errorf("write tokenDone failed: %v", err)
			return
		}
		if err := binary.Write(buf, binary.LittleEndian, uint16(0)); err != nil { // status
			t.Errorf("write done status failed: %v", err)
			return
		}
		if err := binary.Write(buf, binary.LittleEndian, uint16(0)); err != nil { // curCmd
			t.Errorf("write done curCmd failed: %v", err)
			return
		}
		if err := binary.Write(buf, binary.LittleEndian, uint64(0)); err != nil { // rowCount
			t.Errorf("write done rowCount failed: %v", err)
			return
		}

		if err := buf.FinishPacket(); err != nil {
			t.Errorf("Writing routing response failed: %v", err)
			return
		}

		// Keep connection open and wait for client to close its end.
		// After processing the routing redirect, connect() calls
		// toconn.Close() and sets toconn = nil before dialing the new host.
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, err = conn.Read(make([]byte, 1))
		if err == nil {
			t.Error("expected server-side read to fail after client closed first connection")
		} else if ne, ok := err.(net.Error); ok && ne.Timeout() {
			t.Error("server-side read timed out; client did not close the first connection")
		}
	}()

	return firstConnClosed
}

// TestRoutingRedirectToDeadServer verifies that when a server sends a routing
// redirect to a non-existent host, the client closes the first connection
// (setting toconn = nil) before attempting to dial the redirected host.
// The mock server keeps its end open and detects the client-side close.
func TestRoutingRedirectToDeadServer(t *testing.T) {
	addr := &net.TCPAddr{IP: net.IP{127, 0, 0, 1}}
	listener, err := net.ListenTCP("tcp", addr)
	if err != nil {
		t.Fatal("Cannot start listener:", err)
	}
	defer listener.Close()
	resolved := listener.Addr().(*net.TCPAddr)

	firstConnClosed := routingRedirectMockServer(t, listener)

	dsn := fmt.Sprintf("sqlserver://sa:unused@%s:%d?protocol=tcp&encrypt=disable&connection+timeout=5&dial+timeout=2",
		resolved.IP.String(), resolved.Port)
	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		t.Fatal("sql.Open failed:", err)
	}
	defer db.Close()

	err = db.Ping()
	if err == nil {
		t.Fatal("Expected Ping to fail after routing redirect to dead server")
	}

	select {
	case <-firstConnClosed:
	case <-time.After(10 * time.Second):
		t.Fatal("server goroutine did not detect client close of first connection")
	}

	t.Logf("Routing redirect correctly failed: %v", err)
}

// TestRoutingRedirectClonesTLSConfig verifies that the routing redirect code
// path exercises TLSConfig.Clone() when TLSConfig is non-nil. Using
// encrypt=false (EncryptionOff) produces a non-nil TLSConfig without
// requiring a real TLS handshake. The test asserts that the first connection
// is closed after receiving the redirect and that Ping fails when the
// redirected host is unreachable.
func TestRoutingRedirectClonesTLSConfig(t *testing.T) {
	addr := &net.TCPAddr{IP: net.IP{127, 0, 0, 1}}
	listener, err := net.ListenTCP("tcp", addr)
	if err != nil {
		t.Fatal("Cannot start listener:", err)
	}
	defer listener.Close()
	resolved := listener.Addr().(*net.TCPAddr)

	firstConnClosed := routingRedirectMockServer(t, listener)

	// encrypt=false (EncryptionOff) creates a non-nil TLSConfig without
	// requiring a TLS handshake. The mock server responds with encryptNotSup,
	// so no TLS negotiation occurs. This exercises the TLSConfig.Clone()
	// path in the routing redirect code.
	dsn := fmt.Sprintf("sqlserver://sa:unused@%s:%d?protocol=tcp&encrypt=false&connection+timeout=5&dial+timeout=2",
		resolved.IP.String(), resolved.Port)
	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		t.Fatal("sql.Open failed:", err)
	}
	defer db.Close()

	err = db.Ping()
	if err == nil {
		t.Fatal("Expected Ping to fail after routing redirect to dead server")
	}

	select {
	case <-firstConnClosed:
	case <-time.After(10 * time.Second):
		t.Fatal("server goroutine did not detect client close of first connection")
	}
}

// TestEncryptStrictClosesOnTLSError verifies that when connect() is configured
// with encrypt=strict and the TLS handshake fails (because the server doesn't
// speak TLS), the deferred Close in connect() closes the TCP connection.
func TestEncryptStrictClosesOnTLSError(t *testing.T) {
	clientClosed := make(chan struct{})

	addr := &net.TCPAddr{IP: net.IP{127, 0, 0, 1}}
	listener, err := net.ListenTCP("tcp", addr)
	if err != nil {
		t.Fatal("Cannot start listener:", err)
	}
	defer listener.Close()
	resolved := listener.Addr().(*net.TCPAddr)

	go func() {
		defer close(clientClosed)
		conn, err := listener.Accept()
		if err != nil {
			t.Errorf("listener.Accept failed: %v", err)
			return
		}
		defer conn.Close()

		// Read the TLS ClientHello that the client sends for encrypt=strict.
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		buf := make([]byte, 1024)
		if _, err := conn.Read(buf); err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				t.Error("server-side initial read timed out waiting for TLS ClientHello")
			} else {
				t.Errorf("server-side initial read failed: %v", err)
			}
			return
		}

		// Send non-TLS data to cause the TLS handshake to fail immediately.
		if _, err := conn.Write([]byte("NOT TLS")); err != nil {
			t.Errorf("server-side write failed: %v", err)
			return
		}

		// Wait for the client to close its end. The TLS client may send
		// a TLS alert record before connect() returns and the defer closes
		// the connection, so drain any remaining data first.
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		drainBuf := make([]byte, 1024)
		for {
			_, readErr := conn.Read(drainBuf)
			if readErr != nil {
				if ne, ok := readErr.(net.Error); ok && ne.Timeout() {
					t.Error("server-side read timed out; client did not close the connection")
				}
				break
			}
		}
	}()

	dsn := fmt.Sprintf("sqlserver://sa:unused@%s:%d?protocol=tcp&encrypt=strict&connection+timeout=5&dial+timeout=2",
		resolved.IP.String(), resolved.Port)
	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		t.Fatal("sql.Open failed:", err)
	}
	defer db.Close()

	err = db.Ping()
	if err == nil {
		t.Fatal("Expected Ping to fail with encrypt=strict against non-TLS server")
	}

	select {
	case <-clientClosed:
	case <-time.After(10 * time.Second):
		t.Fatal("server goroutine did not detect client close within timeout")
	}
}
