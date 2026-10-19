package mssql

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

// A cancelled transaction context used to make Rollback return before its
// response reader finished, and database/sql then reused the connection.
func TestRollbackCanceledContextDrainsResponseBeforeReturning(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseRead := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseRead)

	transport := &gatedTransport{
		packet:  rollbackTranReplyPacket(),
		release: release,
	}
	sess := &tdsSession{
		buf:    newTdsBuffer(defaultPacketSize, transport),
		tranid: 7,
	}
	conn := &Conn{
		sess:           sess,
		connectionGood: true,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	go func() {
		done <- conn.simpleProcessResp(ctx, true)
	}()

	select {
	case err := <-done:
		t.Fatalf("rollback returned while its response was still held: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	releaseRead()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("rollback error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("rollback did not return after its response was released")
	}
	if sess.tranid != 0 {
		t.Fatalf("tranid = %d after rollback response, want 0", sess.tranid)
	}
}

// A deadline is not the same as cancellation: the connection is discarded, and
// rollback must not wait on a response that the deadline already gave up on.
func TestRollbackDeadlineReturnsWithoutTheResponse(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })

	conn := &Conn{
		sess: &tdsSession{
			buf: newTdsBuffer(defaultPacketSize, &gatedTransport{
				packet:  rollbackTranReplyPacket(),
				release: release,
			}),
			tranid: 7,
		},
		connectionGood: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)

	done := make(chan error, 1)
	go func() {
		done <- conn.simpleProcessResp(ctx, true)
	}()

	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("rollback error = %v, want context.DeadlineExceeded", err)
		}
	case <-time.After(time.Second):
		t.Fatal("deadline rollback waited for the response")
	}
	if conn.connectionGood {
		t.Fatal("deadline rollback left the connection usable")
	}
}

// A cancelled rollback whose response read fails must return that error.
// Keeping context.Canceled would leave the broken connection in the pool.
func TestRollbackCanceledContextReadErrorDiscardsConnection(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseRead := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseRead)

	conn := &Conn{
		sess: &tdsSession{
			buf: newTdsBuffer(defaultPacketSize, &gatedTransport{
				release: release,
				readErr: io.EOF,
			}),
			tranid: 7,
		},
		connectionGood: true,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	go func() {
		done <- conn.simpleProcessResp(ctx, true)
	}()

	select {
	case err := <-done:
		t.Fatalf("rollback returned while its response was still held: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	releaseRead()

	select {
	case err := <-done:
		if !errors.Is(err, io.EOF) {
			t.Fatalf("rollback error = %v, want the read error", err)
		}
		if errors.Is(err, context.Canceled) {
			t.Fatal("rollback kept context.Canceled after the response read failed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("rollback did not return after its response read failed")
	}
	if conn.connectionGood {
		t.Fatal("a failed rollback read left the connection usable")
	}
}

func TestCancelledTxRollbackDoesNotRaceWithReuse(t *testing.T) {
	db := requireTestDB(t)
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatal(err)
	}

	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(2)

	var wg sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 300; i++ {
				ctx, cancel := context.WithCancel(context.Background())
				if tx, err := db.BeginTx(ctx, nil); err == nil {
					_, _ = tx.ExecContext(ctx, "SELECT 1")
				}
				cancel()
				_ = db.QueryRowContext(context.Background(), "SELECT 1").Scan(new(int))
			}
		}()
	}
	wg.Wait()
}

// gatedTransport serves one TDS packet after release is closed.
// A non-nil readErr is returned instead of the packet.
type gatedTransport struct {
	packet  []byte
	off     int
	release <-chan struct{}
	readErr error

	once sync.Once
}

func (g *gatedTransport) Read(p []byte) (int, error) {
	g.once.Do(func() { <-g.release })
	if g.readErr != nil {
		return 0, g.readErr
	}
	if g.off >= len(g.packet) {
		return 0, io.EOF
	}
	n := copy(p, g.packet[g.off:])
	g.off += n
	return n, nil
}

func (g *gatedTransport) Write(p []byte) (int, error) {
	return len(p), nil
}

func (g *gatedTransport) Close() error {
	return nil
}

func rollbackTranReplyPacket() []byte {
	// ENVCHANGE rollback with empty new and old transaction descriptors, then DONE.
	env := []byte{
		byte(tokenEnvChange),
		3, 0,
		byte(envTypRollbackTran),
		0,
		0,
	}
	done := make([]byte, 13)
	done[0] = byte(tokenDone)
	payload := append(env, done...)

	total := 8 + len(payload)
	packet := make([]byte, total)
	packet[0] = byte(packReply)
	packet[1] = 0x01
	binary.BigEndian.PutUint16(packet[2:4], uint16(total))
	packet[6] = 1
	copy(packet[8:], payload)
	return packet
}
