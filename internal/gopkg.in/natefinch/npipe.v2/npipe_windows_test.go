package npipe

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

var pipeTestSequence atomic.Uint64

func newTestPipeName() string {
	return fmt.Sprintf(`\\.\pipe\go-mssqldb-close-%d-%d`, os.Getpid(), pipeTestSequence.Add(1))
}

func testPipePair(t *testing.T, listener *PipeListener) (client, server *PipeConn, closeClient func() error) {
	t.Helper()
	if listener == nil {
		var err error
		listener, err = Listen(newTestPipeName())
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		if err := listener.Close(); err != nil {
			t.Error(err)
		}
	})
	type accepted struct {
		conn *PipeConn
		err  error
	}
	result := make(chan accepted, 1)
	go func() {
		conn, err := listener.AcceptPipe()
		result <- accepted{conn, err}
	}()
	var err error
	client, err = DialTimeoutExisting(listener.addr.String(), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	closeClient = sync.OnceValue(client.Close)
	t.Cleanup(func() {
		if err := closeClient(); err != nil {
			t.Error(err)
		}
	})
	select {
	case got := <-result:
		if got.err != nil {
			t.Fatal(got.err)
		}
		server = got.conn
		t.Cleanup(func() {
			if err := server.Close(); err != nil {
				t.Error(err)
			}
		})
	case <-time.After(5 * time.Second):
		t.Fatal("pipe accept did not complete")
	}
	return client, server, closeClient
}

func testReusedPipeHandle(t *testing.T, retired syscall.Handle) *PipeListener {
	t.Helper()
	// Reserve other slots until Windows recycles the retired handle. The
	// caller checks the actual reuse before attempting stale I/O.
	for range 1024 {
		address := newTestPipeName()
		handle, err := createPipe(address, true)
		if err != nil {
			t.Fatal(err)
		}
		if handle == retired {
			return &PipeListener{handle: handle, addr: PipeAddr(address)}
		}
		t.Cleanup(func() {
			if err := syscall.CloseHandle(handle); err != nil {
				t.Error(err)
			}
		})
	}
	t.Fatal("fixture did not reuse the retired pipe handle")
	return nil
}

func TestPipeConnClosedOperationsDoNotReachReusedHandle(t *testing.T) {
	for _, operation := range []string{"read", "write"} {
		t.Run(operation, func(t *testing.T) {
			retired, _, closeRetired := testPipePair(t, nil)
			handle := retired.handle
			if err := closeRetired(); err != nil {
				t.Fatal(err)
			}
			replacement, peer, _ := testPipePair(t, testReusedPipeHandle(t, handle))
			if peer.handle != handle {
				t.Fatal("replacement pipe did not retain the reused handle")
			}
			const healthy = "healthy"
			for range 2 {
				if operation == "read" {
					if _, err := replacement.Write([]byte(healthy)); err != nil {
						t.Fatal(err)
					}
					data := make([]byte, len(healthy))
					if n, err := retired.Read(data); n != 0 || err == nil {
						t.Fatalf("closed reader consumed the new pipe's data: %q, %v", data[:n], err)
					}
					if _, err := io.ReadFull(peer, data); err != nil {
						t.Fatal(err)
					}
					if string(data) != healthy {
						t.Fatalf("new pipe read %q, want %q", data, healthy)
					}
				} else {
					n, staleErr := retired.Write([]byte("attention"))
					if _, err := peer.Write([]byte(healthy)); err != nil {
						t.Fatal(err)
					}
					data := make([]byte, len(healthy))
					if _, err := io.ReadFull(replacement, data); err != nil {
						t.Fatal(err)
					}
					if n != 0 || staleErr == nil || !bytes.Equal(data, []byte(healthy)) {
						t.Fatalf("closed writer reached the new pipe: wrote %d, error %v, new pipe read %q",
							n, staleErr, data)
					}
				}

			}
		})
	}
}

func waitForPipeState(t *testing.T, c *PipeConn, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		c.mu.Lock()
		ok := ready()
		c.mu.Unlock()
		if ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("pipe did not reach the required I/O state")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestPipeConnCloseInterruptsPendingIO(t *testing.T) {
	for _, operation := range []string{"read", "write"} {
		t.Run(operation, func(t *testing.T) {
			client, _, closeClient := testPipePair(t, nil)
			done := make(chan error, 1)
			go func() {
				var err error
				if operation == "read" {
					_, err = client.Read(make([]byte, 1))
				} else {
					// Exceed the fixture's 512-byte pipe buffer, while
					// the peer stays open without reading anything.
					_, err = client.Write(make([]byte, 1<<20))
				}
				done <- err
			}()
			waitForPipeState(t, client, func() bool { return client.active == 1 })
			select {
			case err := <-done:
				t.Fatalf("I/O completed before close instead of blocking: %v", err)
			default:
			}
			closed := make(chan error, 1)
			go func() { closed <- closeClient() }()
			select {
			case err := <-closed:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("close did not interrupt the pending I/O")
			}
			select {
			case err := <-done:
				if !errors.Is(err, syscall.ERROR_OPERATION_ABORTED) {
					t.Fatalf("pending I/O returned %v, want operation aborted", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("close left the I/O caller blocked")
			}
			client.mu.Lock()
			active := client.active
			client.mu.Unlock()
			if active != 0 {
				t.Fatalf("close released the handle with %d active requests", active)
			}
			if err := client.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPipeConnCloseWaitsForRequestCompletion(t *testing.T) {
	client, _, closeClient := testPipePair(t, nil)
	// Model a completed Windows request whose Go waiter still needs the
	// handle for GetOverlappedResult. CancelIoEx reports ERROR_NOT_FOUND.
	client.active = 1
	finish := sync.OnceFunc(client.finishIO)
	t.Cleanup(finish)
	closed := make(chan error, 1)
	go func() { closed <- closeClient() }()
	waitForPipeState(t, client, func() bool { return client.closed })
	select {
	case err := <-closed:
		t.Fatalf("close returned before the I/O waiter finished: %v", err)
	default:
	}
	if _, err := windows.GetFileType(windows.Handle(client.handle)); err != nil {
		t.Fatalf("pipe handle was released before the I/O waiter finished: %v", err)
	}
	finish()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("close did not finish after the request completed")
	}
}

func TestPipeConnDeadlineWaitsForCompletion(t *testing.T) {
	client, _, _ := testPipePair(t, nil)
	attempt := func() bool {
		event, err := createEvent(nil, true, false, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer syscall.CloseHandle(event)
		complete := sync.OnceFunc(func() {
			if err := windows.SetEvent(windows.Handle(event)); err != nil {
				t.Error(err)
			}
		})
		defer complete()
		overlapped := &syscall.Overlapped{HEvent: event}
		done := make(chan error, 1)
		started := make(chan struct{})
		go func() {
			deadline := time.Now().Add(100 * time.Millisecond)
			close(started)
			_, err := client.completeRequest(iodata{err: syscall.ERROR_IO_PENDING}, &deadline, overlapped)
			done <- err
		}()
		<-started
		select {
		case err := <-done:
			t.Fatalf("deadline returned before the cancellation completion event: %v", err)
		case <-time.After(250 * time.Millisecond):
		}
		complete()
		select {
		case err := <-done:
			if err == nil {
				// A descheduled worker may miss the deadline setup or
				// select completion before servicing the timer.
				t.Log("completion fixture missed the timeout phase; retrying setup")
				return false
			}
			var pipeErr PipeError
			if !errors.As(err, &pipeErr) || !pipeErr.Timeout() {
				t.Fatalf("completed deadline returned %v, want the original timeout error", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("completion event did not release the timed-out request")
		}
		return true
	}
	for range 3 {
		if attempt() {
			return
		}
	}
	t.Fatal("fixture did not exercise the timeout path")
}

func TestPipeConnClosePreservesErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		active int
		event  bool
	}{
		{"close failure", 0, false},
		{"cancel and close failure", 1, false},
		{"cancel failure only", 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &PipeConn{active: tc.active}
			if tc.event {
				handle, err := createEvent(nil, true, false, nil)
				if err != nil {
					t.Fatal(err)
				}
				client.handle = handle
			}
			t.Cleanup(func() { client.Close() })
			closed := make(chan error, 1)
			go func() { closed <- client.Close() }()
			if tc.active != 0 {
				finish := sync.OnceFunc(client.finishIO)
				t.Cleanup(finish)
				waitForPipeState(t, client, func() bool { return client.closed })
				finish()
			}
			if err := <-closed; !errors.Is(err, windows.ERROR_INVALID_HANDLE) {
				t.Fatalf("close returned %v, want the native invalid-handle error", err)
			}
			for range 2 {
				if err := client.Close(); !errors.Is(err, windows.ERROR_INVALID_HANDLE) {
					t.Fatalf("close returned %v, want the native invalid-handle error", err)
				}
			}
			if tc.event {
				// A zero mask changes no flags, but checks handle validity.
				err := windows.SetHandleInformation(windows.Handle(client.handle), 0, 0)
				if !errors.Is(err, windows.ERROR_INVALID_HANDLE) {
					t.Fatalf("the event handle was not released after cancellation failed: %v", err)
				}
			}
		})
	}
}

func TestPipeConnHealthyIOAndDeadlineCompatibility(t *testing.T) {
	client, peer, closeClient := testPipePair(t, nil)
	for range 2 {
		deadline := time.Now().Add(5 * time.Second)
		for _, set := range []func(time.Time) error{client.SetDeadline, client.SetReadDeadline, client.SetWriteDeadline} {
			if err := set(deadline); err != nil {
				t.Fatal(err)
			}
		}
		for _, pair := range [][2]*PipeConn{{client, peer}, {peer, client}} {
			if _, err := pair[0].Write([]byte("healthy")); err != nil {
				t.Fatal(err)
			}
			data := make([]byte, len("healthy"))
			if _, err := io.ReadFull(pair[1], data); err != nil {
				t.Fatal(err)
			}
			if string(data) != "healthy" {
				t.Fatalf("healthy pipe read %q", data)
			}
		}
	}
	if err := closeClient(); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		for _, set := range []func(time.Time) error{client.SetDeadline, client.SetReadDeadline, client.SetWriteDeadline} {
			if err := set(time.Now()); err != nil {
				t.Fatalf("local deadline update after close returned %v", err)
			}
		}
		if n, err := client.Write([]byte("closed")); n != 0 || !errors.Is(err, ErrClosed) {
			t.Fatalf("deadline update reopened the closed pipe: wrote %d, error %v", n, err)
		}
	}
}

func TestPipeConnCloseRacesSubmission(t *testing.T) {
	for attempt := range 100 {
		t.Run(fmt.Sprint(attempt), func(t *testing.T) {
			client, _, closeClient := testPipePair(t, nil)
			start := make(chan struct{})
			type result struct {
				operation string
				err       error
			}
			results := make(chan result, 3)
			go func() {
				<-start
				_, err := client.Read(make([]byte, 1))
				results <- result{"read", err}
			}()
			go func() {
				<-start
				_, err := client.Write(make([]byte, 1<<20))
				results <- result{"write", err}
			}()
			go func() {
				<-start
				results <- result{"close", closeClient()}
			}()
			close(start)
			for range 3 {
				select {
				case got := <-results:
					if got.operation == "close" {
						if got.err != nil {
							t.Fatal(got.err)
						}
					} else if !errors.Is(got.err, ErrClosed) && !errors.Is(got.err, syscall.ERROR_OPERATION_ABORTED) {
						t.Fatalf("%s returned %v, want closed or canceled I/O", got.operation, got.err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("I/O submission raced past close's cancellation")
				}
			}
		})
	}
}

func TestPipeConnReadDeadlinePreservesReuse(t *testing.T) {
	client, peer, _ := testPipePair(t, nil)
	for range 2 {
		if err := client.SetReadDeadline(time.Now().Add(250 * time.Millisecond)); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() {
			_, err := client.Read(make([]byte, 1))
			done <- err
		}()
		select {
		case err := <-done:
			var pipeErr PipeError
			if !errors.As(err, &pipeErr) || !pipeErr.Timeout() {
				t.Fatalf("read returned %v, want the original timeout error", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("read deadline did not cancel the pending native I/O")
		}
		if err := client.SetReadDeadline(time.Time{}); err != nil {
			t.Fatal(err)
		}
		if _, err := peer.Write([]byte("healthy")); err != nil {
			t.Fatal(err)
		}
		data := make([]byte, len("healthy"))
		if _, err := io.ReadFull(client, data); err != nil {
			t.Fatal(err)
		}
		if string(data) != "healthy" {
			t.Fatalf("read after timeout returned %q", data)
		}
	}
}

func TestPipeConnPeerCloseReturnsEOF(t *testing.T) {
	client, peer, _ := testPipePair(t, nil)
	if err := peer.Close(); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if n, err := client.Read(make([]byte, 1)); n != 0 || err != io.EOF {
			t.Fatalf("peer close returned %d, %v; want 0, io.EOF", n, err)
		}
	}
}
