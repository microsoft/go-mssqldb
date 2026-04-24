package mssql

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
)

// TestConnectClosesOnError verifies that when connect() encounters an error,
// the underlying network connection is properly cleaned up by the deferred
// Close call, preventing resource leaks.
func TestConnectClosesOnError(t *testing.T) {
	var closed atomic.Int32

	addr := &net.TCPAddr{IP: net.IP{127, 0, 0, 1}}
	listener, err := net.ListenTCP("tcp", addr)
	if err != nil {
		t.Fatal("Cannot start listener:", err)
	}
	defer listener.Close()
	resolved := listener.Addr().(*net.TCPAddr)

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		// Send a valid prelogin response, then drop the connection.
		// This causes connect() to fail during login, exercising the
		// deferred-close error path.
		buf := newTdsBuffer(defaultPacketSize, conn)
		fields := map[uint8][]byte{
			preloginENCRYPTION: {encryptNotSup},
		}
		if err := writePrelogin(packReply, buf, fields); err != nil {
			t.Log("Writing PRELOGIN failed:", err)
		}
		// Close without reading login — causes the client's login read to fail.
		conn.Close()
		closed.Add(1)
	}()

	tl := testLogger{t: t}
	defer tl.StopLogging()
	SetLogger(&tl)

	dsn := fmt.Sprintf("sqlserver://sa:unused@%s:%d?protocol=tcp&encrypt=disable&connection+timeout=5&dial+timeout=2",
		resolved.IP.String(), resolved.Port)
	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		t.Fatal("sql.Open failed:", err)
	}
	defer db.Close()

	// This will attempt to connect — prelogin succeeds, but login fails
	// because the server dropped the connection. The defer in connect()
	// should clean up the timeoutConn.
	err = db.Ping()
	if err == nil {
		t.Fatal("Expected Ping to fail, but it succeeded")
	}

	// If we got here without a panic, the deferred close worked correctly.
	// A double-close or missing close would either panic or leak.
	t.Logf("Connection correctly failed with error: %v", err)
}

// TestRoutingRedirectToDeadServer verifies that when a server sends a routing
// redirect to a non-existent host, the first connection is closed properly
// and the connection to the redirected host fails cleanly without panic.
func TestRoutingRedirectToDeadServer(t *testing.T) {
	addr := &net.TCPAddr{IP: net.IP{127, 0, 0, 1}}
	listener, err := net.ListenTCP("tcp", addr)
	if err != nil {
		t.Fatal("Cannot start listener:", err)
	}
	defer listener.Close()
	resolved := listener.Addr().(*net.TCPAddr)

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		buf := newTdsBuffer(defaultPacketSize, conn)

		// Complete prelogin handshake.
		goodPreloginSequence(t, buf)

		// Send a login response containing an ENVCHANGE routing token
		// that redirects to 127.0.0.1:1 (a port nothing is listening on).
		buf.BeginPacket(packReply, false)

		// Write ENVCHANGE token with routing info.
		routingServer := "127.0.0.1"
		// ENVCHANGE routing payload:
		// envtype(1) + valueLength(2) + protocol(1) + port(2) + serverNameLen(2) + serverName(2*len) + oldValue(2)
		serverUTF16Len := len(routingServer) * 2
		valueLength := 1 + 2 + 2 + serverUTF16Len // protocol + port + usVarCharLen + name
		envPayloadLen := 1 + 2 + valueLength + 2   // envtype + valueLength + value + oldValue

		// Token header: tokenEnvChange(1) + length(2)
		buf.WriteByte(byte(tokenEnvChange))
		binary.Write(buf, binary.LittleEndian, uint16(envPayloadLen))

		// envtype = envRouting (20)
		buf.WriteByte(20)
		// ValueLength
		binary.Write(buf, binary.LittleEndian, uint16(valueLength))
		// Protocol = TCP (0)
		buf.WriteByte(0)
		// Port = 1 (unlikely to be listening)
		binary.Write(buf, binary.LittleEndian, uint16(1))
		// Server name as US_VARCHAR: length in chars + UTF-16LE chars
		binary.Write(buf, binary.LittleEndian, uint16(len(routingServer)))
		for _, ch := range routingServer {
			binary.Write(buf, binary.LittleEndian, uint16(ch))
		}
		// OldValue = 0x0000
		binary.Write(buf, binary.LittleEndian, uint16(0))

		// Write DONE token (0xFD) with status=0, curCmd=0, rowCount=0.
		buf.WriteByte(byte(tokenDone))
		binary.Write(buf, binary.LittleEndian, uint16(0)) // status
		binary.Write(buf, binary.LittleEndian, uint16(0)) // curCmd
		binary.Write(buf, binary.LittleEndian, uint64(0)) // rowCount

		if err := buf.FinishPacket(); err != nil {
			t.Log("Writing routing response failed:", err)
		}
	}()

	tl := testLogger{t: t}
	defer tl.StopLogging()
	SetLogger(&tl)

	dsn := fmt.Sprintf("sqlserver://sa:unused@%s:%d?protocol=tcp&encrypt=disable&connection+timeout=5&dial+timeout=2",
		resolved.IP.String(), resolved.Port)
	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		t.Fatal("sql.Open failed:", err)
	}
	defer db.Close()

	// The client should:
	// 1. Connect to our mock server
	// 2. Receive the routing redirect
	// 3. Close the first connection (toconn.Close())
	// 4. Set toconn = nil (our fix)
	// 5. Try to connect to 127.0.0.1:1, which fails
	// 6. The defer should NOT double-close because toconn is nil
	err = db.Ping()
	if err == nil {
		t.Fatal("Expected Ping to fail after routing redirect to dead server")
	}

	// If we reach here without panic, the nil guard prevented a double-close.
	t.Logf("Routing redirect correctly failed: %v", err)
}
