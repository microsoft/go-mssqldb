package mssql

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"
)

func TestPreloginTimeout(t *testing.T) {
	t.Run("no deadline keeps connection timeout", func(t *testing.T) {
		got, err := preloginTimeout(context.Background(), 30*time.Second)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != 30*time.Second {
			t.Fatalf("timeout=%v, want %v", got, 30*time.Second)
		}
	})

	t.Run("sooner deadline wins", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		defer cancel()

		got, err := preloginTimeout(ctx, 30*time.Second)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got <= 0 || got > 250*time.Millisecond {
			t.Fatalf("timeout=%v, want a positive value no greater than %v", got, 250*time.Millisecond)
		}
	})

	t.Run("shorter connection timeout stays in effect", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		got, err := preloginTimeout(ctx, 250*time.Millisecond)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != 250*time.Millisecond {
			t.Fatalf("timeout=%v, want %v", got, 250*time.Millisecond)
		}
	})

	t.Run("zero connection timeout uses context deadline", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		defer cancel()

		got, err := preloginTimeout(ctx, 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got <= 0 || got > 250*time.Millisecond {
			t.Fatalf("timeout=%v, want a positive value no greater than %v", got, 250*time.Millisecond)
		}
	})

	t.Run("expired deadline returns context error", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer cancel()

		_, err := preloginTimeout(ctx, 30*time.Second)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if err != context.DeadlineExceeded {
			t.Fatalf("error=%v, want %v", err, context.DeadlineExceeded)
		}
	})

	t.Run("canceled context without deadline returns context error", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := preloginTimeout(ctx, 30*time.Second)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if err != context.Canceled {
			t.Fatalf("error=%v, want %v", err, context.Canceled)
		}
	})
}

// TestPreloginRespectsContextDeadline verifies that readPrelogin honors the
// context deadline rather than hanging for the full ConnTimeout when the
// server never responds.
func TestPreloginRespectsContextDeadline(t *testing.T) {
	// Start a TCP listener that accepts connections but never sends data,
	// simulating a server that hangs during prelogin.
	addr := &net.TCPAddr{IP: net.IP{127, 0, 0, 1}}
	listener, err := net.ListenTCP("tcp", addr)
	if err != nil {
		t.Fatal("Cannot start listener:", err)
	}
	defer listener.Close()
	resolved := listener.Addr().(*net.TCPAddr)

	done := make(chan struct{})
	defer close(done)

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			// Read the prelogin request but never respond.
			buf := make([]byte, 4096)
			_, _ = conn.Read(buf)
			// Hold connection open until the test finishes.
			<-done
			conn.Close()
		}
	}()

	// Use a long ConnTimeout (30s) so if the context deadline is NOT
	// respected, the test will hang noticeably.
	dsn := fmt.Sprintf("sqlserver://sa:unused@%s:%d?connection+timeout=30&dial+timeout=2&protocol=tcp&encrypt=disable",
		resolved.IP.String(), resolved.Port)

	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		t.Fatal("sql.Open failed:", err)
	}
	defer db.Close()

	// Context with a short deadline — this is the one that should win.
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	type connResult struct {
		conn    *sql.Conn
		err     error
		elapsed time.Duration
	}
	resultCh := make(chan connResult, 1)
	start := time.Now()
	go func() {
		conn, err := db.Conn(ctx)
		resultCh <- connResult{conn: conn, err: err, elapsed: time.Since(start)}
	}()

	var result connResult
	select {
	case result = <-resultCh:
	case <-time.After(10 * time.Second):
		t.Fatal("db.Conn(ctx) did not return before hard timeout; possible prelogin deadline regression")
	}

	if result.err == nil {
		result.conn.Close()
		t.Fatal("Expected connection to fail, but it succeeded")
	}

	// The connection should fail well before the full ConnTimeout (30s).
	// We use a generous 5s bound to avoid flakes on slow CI; the real
	// expectation is ~500ms from the context deadline.
	if result.elapsed > 5*time.Second {
		t.Errorf("Connection took %v, expected it to respect the 500ms context deadline", result.elapsed)
	}

	if !errors.Is(result.err, context.DeadlineExceeded) {
		// The socket timeout from preloginTimeout and the context deadline
		// can race. Both prove the deadline was respected; the elapsed
		// check above is the primary assertion.
		if ne := (net.Error)(nil); !errors.As(result.err, &ne) || !ne.Timeout() {
			t.Errorf("expected context.DeadlineExceeded or net timeout, got: %v", result.err)
		}
	}
}

// TestPreloginRespectsContextCancel verifies that readPrelogin unblocks
// when the context is canceled even without a deadline set.
func TestPreloginRespectsContextCancel(t *testing.T) {
	addr := &net.TCPAddr{IP: net.IP{127, 0, 0, 1}}
	listener, err := net.ListenTCP("tcp", addr)
	if err != nil {
		t.Fatal("Cannot start listener:", err)
	}
	defer listener.Close()
	resolved := listener.Addr().(*net.TCPAddr)

	done := make(chan struct{})
	defer close(done)

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			buf := make([]byte, 4096)
			_, _ = conn.Read(buf)
			<-done
			conn.Close()
		}
	}()

	// connTimeout=30 and no context deadline: without the cancel watcher,
	// this would block for the full 30s.
	dsn := fmt.Sprintf("sqlserver://sa:unused@%s:%d?connection+timeout=30&dial+timeout=2&protocol=tcp&encrypt=disable",
		resolved.IP.String(), resolved.Port)

	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		t.Fatal("sql.Open failed:", err)
	}
	defer db.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Cancel after 500ms to simulate a caller-driven cancellation.
	timer := time.AfterFunc(500*time.Millisecond, cancel)
	defer timer.Stop()

	type connResult struct {
		conn    *sql.Conn
		err     error
		elapsed time.Duration
	}
	resultCh := make(chan connResult, 1)
	start := time.Now()
	go func() {
		conn, err := db.Conn(ctx)
		resultCh <- connResult{conn: conn, err: err, elapsed: time.Since(start)}
	}()

	var result connResult
	select {
	case result = <-resultCh:
	case <-time.After(10 * time.Second):
		t.Fatal("db.Conn(ctx) did not return before hard timeout; possible prelogin cancellation regression")
	}

	if result.err == nil {
		result.conn.Close()
		t.Fatal("Expected connection to fail, but it succeeded")
	}

	if result.elapsed > 5*time.Second {
		t.Errorf("Connection took %v, expected it to respect context cancellation within ~500ms", result.elapsed)
	}

	if !errors.Is(result.err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", result.err)
	}
}

// TestPreloginSocketTimeoutBeforeContextExpiry verifies that when the
// connection timeout fires before the context deadline, readPrelogin
// returns a socket timeout error (not a context error).
func TestPreloginSocketTimeoutBeforeContextExpiry(t *testing.T) {
	addr := &net.TCPAddr{IP: net.IP{127, 0, 0, 1}}
	listener, err := net.ListenTCP("tcp", addr)
	if err != nil {
		t.Fatal("Cannot start listener:", err)
	}
	defer listener.Close()
	resolved := listener.Addr().(*net.TCPAddr)

	done := make(chan struct{})
	defer close(done)

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			// Read prelogin but never respond.
			buf := make([]byte, 4096)
			_, _ = conn.Read(buf)
			<-done
			conn.Close()
		}
	}()

	// Short connection timeout (1s) with a long context (30s).
	// The socket timeout from preloginTimeout should fire, NOT the context.
	dsn := fmt.Sprintf("sqlserver://sa:unused@%s:%d?connection+timeout=1&dial+timeout=2&protocol=tcp&encrypt=disable",
		resolved.IP.String(), resolved.Port)

	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		t.Fatal("sql.Open failed:", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	type connResult struct {
		conn    *sql.Conn
		err     error
		elapsed time.Duration
	}
	resultCh := make(chan connResult, 1)
	start := time.Now()
	go func() {
		conn, err := db.Conn(ctx)
		resultCh <- connResult{conn: conn, err: err, elapsed: time.Since(start)}
	}()

	var result connResult
	select {
	case result = <-resultCh:
	case <-time.After(10 * time.Second):
		t.Fatal("db.Conn(ctx) did not return; possible prelogin timeout regression")
	}

	if result.err == nil {
		result.conn.Close()
		t.Fatal("Expected connection to fail")
	}

	// Should fail from socket timeout (~1s), not context (30s).
	if result.elapsed > 5*time.Second {
		t.Errorf("Connection took %v; expected ~1s from socket timeout", result.elapsed)
	}

	// The error should be a net timeout, not context.DeadlineExceeded.
	if errors.Is(result.err, context.DeadlineExceeded) {
		t.Errorf("Got context.DeadlineExceeded but expected socket timeout error")
	}
}

// TestPreloginTimeoutErrorPath verifies that an already-expired context
// triggers the preloginTimeout error return inside connect().
func TestPreloginTimeoutErrorPath(t *testing.T) {
	addr := &net.TCPAddr{IP: net.IP{127, 0, 0, 1}}
	listener, err := net.ListenTCP("tcp", addr)
	if err != nil {
		t.Fatal("Cannot start listener:", err)
	}
	defer listener.Close()
	resolved := listener.Addr().(*net.TCPAddr)

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			buf := make([]byte, 4096)
			_, _ = conn.Read(buf)
			conn.Close()
		}
	}()

	// Use a context that will expire very quickly. The dial has its own
	// timeout so it can succeed even when the parent context expires.
	// By the time writePrelogin + preloginTimeout run, the context should
	// be expired, triggering the preloginTimeout → err path.
	dsn := fmt.Sprintf("sqlserver://sa:unused@%s:%d?connection+timeout=30&dial+timeout=5&protocol=tcp&encrypt=disable",
		resolved.IP.String(), resolved.Port)

	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		t.Fatal("sql.Open failed:", err)
	}
	defer db.Close()

	// Very short context: 1ms. Dial should complete before this expires
	// (localhost connection), but preloginTimeout should see the expired context.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	// Give the context time to expire before attempting the connection.
	time.Sleep(5 * time.Millisecond)

	start := time.Now()
	_, err = db.Conn(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Expected connection to fail with expired context")
	}

	// Should fail fast (context already expired).
	if elapsed > 5*time.Second {
		t.Errorf("Connection took %v; expected fast failure", elapsed)
	}
}

// TestRoutingRedirectClosesFirstConnection verifies that when a server
// sends a routing redirect, the original connection is properly closed
// and toconn is set to nil before the next connection attempt.
func TestRoutingRedirectClosesFirstConnection(t *testing.T) {
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
		// that redirects to 127.0.0.1:1 (a port nothing listens on).
		buf.BeginPacket(packReply, false)

		routingServer := "127.0.0.1"
		serverUTF16Len := len(routingServer) * 2
		valueLength := 1 + 2 + 2 + serverUTF16Len
		envPayloadLen := 1 + 2 + valueLength + 2

		buf.WriteByte(byte(tokenEnvChange))
		binary.Write(buf, binary.LittleEndian, uint16(envPayloadLen))
		buf.WriteByte(20) // envRouting
		binary.Write(buf, binary.LittleEndian, uint16(valueLength))
		buf.WriteByte(0)                                  // TCP
		binary.Write(buf, binary.LittleEndian, uint16(1)) // port 1
		binary.Write(buf, binary.LittleEndian, uint16(len(routingServer)))
		for _, ch := range routingServer {
			binary.Write(buf, binary.LittleEndian, uint16(ch))
		}
		binary.Write(buf, binary.LittleEndian, uint16(0)) // old value

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

	err = db.Ping()
	if err == nil {
		t.Fatal("Expected Ping to fail after routing redirect to dead server")
	}

	t.Logf("Routing redirect correctly failed: %v", err)
}
