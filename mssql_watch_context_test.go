package mssql

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/microsoft/go-mssqldb/msdsn"
	"github.com/stretchr/testify/assert"
)

// fakeWriteDeadliner is a minimal writeDeadliner implementation for testing
// watchContextForWrite without a real net.Conn.
type fakeWriteDeadliner struct {
	lastDeadline   time.Time
	setCount       int
	deadlineErr    error
	deadlineErrs   []error
	closed         bool
	deadlineCalled chan struct{}
	deadlineOnce   sync.Once
}

type attentionTransport struct {
	bytes.Buffer
	deadlines    []time.Time
	deadlineErr  error
	deadlineErrs []error
	closed       bool
}

func (*attentionTransport) Read([]byte) (int, error) {
	return 0, io.EOF
}

func (t *attentionTransport) Close() error {
	t.closed = true
	return nil
}

func (t *attentionTransport) SetWriteDeadline(deadline time.Time) error {
	t.deadlines = append(t.deadlines, deadline)
	if len(t.deadlineErrs) >= len(t.deadlines) {
		return t.deadlineErrs[len(t.deadlines)-1]
	}
	return t.deadlineErr
}

func (f *fakeWriteDeadliner) SetWriteDeadline(t time.Time) error {
	f.lastDeadline = t
	f.setCount++
	if f.deadlineCalled != nil {
		f.deadlineOnce.Do(func() { close(f.deadlineCalled) })
	}
	if len(f.deadlineErrs) >= f.setCount {
		return f.deadlineErrs[f.setCount-1]
	}
	return f.deadlineErr
}

func (f *fakeWriteDeadliner) Close() error {
	f.closed = true
	return nil
}

func TestWatchContextForWrite_NoopWhenNeverCancelled(t *testing.T) {
	wd := &fakeWriteDeadliner{}
	stop := watchContextForWrite(context.Background(), wd)
	assert.NoError(t, stop())
	// context.Background() can never be cancelled, so watchContextForWrite
	// should be a complete no-op.
	assert.Zero(t, wd.setCount, "SetWriteDeadline should not be called when ctx can never be cancelled")
}

func TestWatchContextForWrite_StopBeforeCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wd := &fakeWriteDeadliner{}
	stop := watchContextForWrite(ctx, wd)
	// Simulate the write completing normally, well before ctx is ever
	// cancelled.
	assert.NoError(t, stop())

	assert.Zero(t, wd.setCount, "no deadline should be cleared when none was armed")
	assert.True(t, wd.lastDeadline.IsZero(), "no deadline should be left armed if the write completed before ctx was cancelled")
}

func TestWatchContextForWrite_CancelForcesImmediateDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	wd := &fakeWriteDeadliner{}
	stop := watchContextForWrite(ctx, wd)

	cancel()
	// Give the monitoring goroutine a chance to observe ctx.Done() and
	// force an immediate write deadline before we ask it to stop.
	time.Sleep(50 * time.Millisecond)
	assert.NoError(t, stop())

	// stop() always clears the deadline before returning (see
	// watchContextForWrite doc comment), so the final recorded deadline is
	// zero, but SetWriteDeadline must have been called at least twice:
	// once to force the immediate deadline, once to clear it.
	assert.GreaterOrEqual(t, wd.setCount, 2, "SetWriteDeadline should be called both to force and then clear the deadline")
	assert.True(t, wd.lastDeadline.IsZero(), "stop() must always clear the deadline before returning")
}

func TestWatchContextForWrite_DeadlineFailureClosesTransport(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	deadlineErr := errors.New("deadlines unsupported")
	transport := &fakeWriteDeadliner{
		deadlineErr:    deadlineErr,
		deadlineCalled: make(chan struct{}),
	}
	stop := watchContextForWrite(ctx, transport)

	cancel()
	<-transport.deadlineCalled
	err := stop()

	assert.ErrorIs(t, err, deadlineErr)
	assert.True(t, transport.closed, "closing the transport must unblock the in-flight write")
}

func TestWatchContextForWrite_ClearFailureClosesTransport(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	clearErr := errors.New("cannot clear deadline")
	transport := &fakeWriteDeadliner{
		deadlineErrs:   []error{nil, clearErr},
		deadlineCalled: make(chan struct{}),
	}
	stop := watchContextForWrite(ctx, transport)

	cancel()
	<-transport.deadlineCalled
	err := stop()

	assert.ErrorIs(t, err, clearErr)
	assert.True(t, transport.closed, "a connection with a stale deadline must not be reused")
}

func TestWithWriteGuardPreservesLegacyPath(t *testing.T) {
	transport := &attentionTransport{deadlineErr: errors.New("must not be called")}
	sess := newSession(newTdsBuffer(defaultPacketSize, transport), nil, msdsn.Config{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false

	err := withWriteGuard(ctx, sess, func() error {
		called = true
		return nil
	})

	assert.NoError(t, err)
	assert.True(t, called)
	assert.Empty(t, transport.deadlines, "legacy writes must remain governed only by ConnTimeout")
	assert.False(t, transport.closed)
}

func TestWithWriteGuard_DeadlineFailureClosesAndReturnsNetworkError(t *testing.T) {
	deadlineErr := errors.New("deadlines unsupported")
	transport := &blockingDeadlineTransport{
		deadlineErr: deadlineErr,
		closed:      make(chan struct{}),
	}
	sess := newSession(newTdsBuffer(defaultPacketSize, transport), nil, msdsn.Config{
		DisableConnTimeoutAsQueryTimeout: true,
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := withWriteGuard(ctx, sess, func() error {
		<-transport.closed
		return nil
	})

	var netErr net.Error
	assert.ErrorAs(t, err, &netErr)
	assert.ErrorIs(t, err, deadlineErr)
}

type blockingDeadlineTransport struct {
	deadlineErr error
	closed      chan struct{}
	closeOnce   sync.Once
}

func (*blockingDeadlineTransport) Read([]byte) (int, error) {
	return 0, io.EOF
}

func (*blockingDeadlineTransport) Write(p []byte) (int, error) {
	return len(p), nil
}

func (t *blockingDeadlineTransport) Close() error {
	t.closeOnce.Do(func() { close(t.closed) })
	return nil
}

func (t *blockingDeadlineTransport) SetWriteDeadline(time.Time) error {
	return t.deadlineErr
}

func TestSendAttentionWithGuardPreservesLegacyNoTimeout(t *testing.T) {
	transport := &attentionTransport{}
	sess := newSession(newTdsBuffer(defaultPacketSize, transport), nil, msdsn.Config{})

	assert.NoError(t, sendAttentionWithGuard(sess))
	assert.Empty(t, transport.deadlines, "legacy mode must not introduce an attention write deadline")
}

func TestSendAttentionWithGuardBoundsOptInNoTimeout(t *testing.T) {
	transport := &attentionTransport{}
	sess := newSession(newTdsBuffer(defaultPacketSize, transport), nil, msdsn.Config{
		DisableConnTimeoutAsQueryTimeout: true,
	})

	assert.NoError(t, sendAttentionWithGuard(sess))
	if assert.Len(t, transport.deadlines, 2) {
		assert.False(t, transport.deadlines[0].IsZero(), "opt-in mode must bound the attention write")
		assert.True(t, transport.deadlines[1].IsZero(), "attention write deadline must be cleared")
	}
}

func TestSendAttentionWithGuardDoesNotWriteWithoutDeadline(t *testing.T) {
	deadlineErr := errors.New("deadlines unsupported")
	transport := &attentionTransport{deadlineErr: deadlineErr}
	sess := newSession(newTdsBuffer(defaultPacketSize, transport), nil, msdsn.Config{
		DisableConnTimeoutAsQueryTimeout: true,
	})

	err := sendAttentionWithGuard(sess)

	assert.ErrorIs(t, err, deadlineErr)
	var netErr net.Error
	assert.ErrorAs(t, err, &netErr)
	assert.True(t, transport.closed, "the unread server response must be abandoned")
	assert.Empty(t, transport.Bytes(), "attention must not risk an unbounded write")
	conn := &Conn{connectionGood: true}
	assert.Equal(t, err, conn.checkBadConn(context.Background(), err, false))
	assert.False(t, conn.connectionGood, "the response path must reject the connection")
}

func TestSendAttentionWithGuardClearFailureIsNetworkError(t *testing.T) {
	clearErr := errors.New("cannot clear deadline")
	transport := &attentionTransport{deadlineErrs: []error{nil, clearErr}}
	sess := newSession(newTdsBuffer(defaultPacketSize, transport), nil, msdsn.Config{
		DisableConnTimeoutAsQueryTimeout: true,
	})

	err := sendAttentionWithGuard(sess)

	assert.ErrorIs(t, err, clearErr)
	var netErr net.Error
	assert.ErrorAs(t, err, &netErr)
	assert.True(t, transport.closed, "a connection with a stale attention deadline must not be reused")
}
