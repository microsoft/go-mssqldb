package mssql

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/microsoft/go-mssqldb/msdsn"
	"github.com/stretchr/testify/assert"
)

// fakeWriteDeadliner is a minimal writeDeadliner implementation for testing
// watchContextForWrite without a real net.Conn.
type fakeWriteDeadliner struct {
	lastDeadline time.Time
	setCount     int
}

type attentionTransport struct {
	bytes.Buffer
	deadlines   []time.Time
	deadlineErr error
}

func (*attentionTransport) Read([]byte) (int, error) {
	return 0, io.EOF
}

func (*attentionTransport) Close() error {
	return nil
}

func (t *attentionTransport) SetWriteDeadline(deadline time.Time) error {
	t.deadlines = append(t.deadlines, deadline)
	return t.deadlineErr
}

func (f *fakeWriteDeadliner) SetWriteDeadline(t time.Time) error {
	f.lastDeadline = t
	f.setCount++
	return nil
}

func TestWatchContextForWrite_NoopWhenNeverCancelled(t *testing.T) {
	wd := &fakeWriteDeadliner{}
	stop := watchContextForWrite(context.Background(), wd)
	stop()
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
	stop()

	// stop() always clears the deadline before returning (see
	// watchContextForWrite doc comment), even if it was never armed, so
	// exactly one call is expected here, leaving no deadline behind.
	assert.Equal(t, 1, wd.setCount, "stop() should clear the deadline exactly once even if ctx was never cancelled")
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
	stop()

	// stop() always clears the deadline before returning (see
	// watchContextForWrite doc comment), so the final recorded deadline is
	// zero, but SetWriteDeadline must have been called at least twice:
	// once to force the immediate deadline, once to clear it.
	assert.GreaterOrEqual(t, wd.setCount, 2, "SetWriteDeadline should be called both to force and then clear the deadline")
	assert.True(t, wd.lastDeadline.IsZero(), "stop() must always clear the deadline before returning")
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
	assert.Empty(t, transport.Bytes(), "attention must not risk an unbounded write")
}
