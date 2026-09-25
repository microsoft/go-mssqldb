package mssql

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// fakeWriteDeadliner is a minimal writeDeadliner implementation for testing
// watchContextForWrite without a real net.Conn.
type fakeWriteDeadliner struct {
	lastDeadline time.Time
	setCount     int
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
