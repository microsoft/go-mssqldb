package mssql

import (
	"context"
	"database/sql"
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

	start := time.Now()
	conn, err := db.Conn(ctx)
	elapsed := time.Since(start)

	if err == nil {
		conn.Close()
		t.Fatal("Expected connection to fail, but it succeeded")
	}

	// The connection should fail well before the full ConnTimeout (30s).
	// We use a generous 5s bound to avoid flakes on slow CI; the real
	// expectation is ~500ms from the context deadline.
	if elapsed > 5*time.Second {
		t.Errorf("Connection took %v, expected it to respect the 500ms context deadline", elapsed)
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

	// Cancel after 500ms to simulate a caller-driven cancellation.
	time.AfterFunc(500*time.Millisecond, cancel)

	start := time.Now()
	conn, err := db.Conn(ctx)
	elapsed := time.Since(start)

	if err == nil {
		conn.Close()
		t.Fatal("Expected connection to fail, but it succeeded")
	}

	if elapsed > 5*time.Second {
		t.Errorf("Connection took %v, expected it to respect context cancellation within ~500ms", elapsed)
	}
}
