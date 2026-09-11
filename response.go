package mssql

import (
	"context"
	"database/sql/driver"

	"github.com/microsoft/go-mssqldb/msdsn"
)

type responseErrorKind uint8

const (
	responseErrorNone responseErrorKind = iota
	responseErrorFatal
	responseErrorOutput
)

// Output assignment failed after the value was decoded successfully.
// This internal token keeps that distinction out of the public error.
type outputParameterError struct{ error }

type responseCleanup struct {
	done   chan struct{}
	cancel context.CancelFunc
	err    error // Read only after done closes.
}

// discard transfers the remaining response to one background consumer. It must
// finish before another request can read session state or write to the transport.
func (t *tokenProcessor) discard() {
	if t.cleanup != nil {
		return
	}
	if t.finished {
		t.release()
		return
	}
	pending := &responseCleanup{done: make(chan struct{}), cancel: t.cancel}
	t.cleanup = pending
	t.sess.cleanup = pending
	if t.cancelMessages != nil {
		t.cancelMessages()
	}
	reader := *t
	reader.cleanup = nil
	go func() {
		defer close(pending.done)
		defer reader.release()
		pending.err = reader.drain()
		if pending.err != nil {
			reader.sess.LogF(reader.ctx, msdsn.LogErrors, "Response cleanup failed: %v", pending.err)
			return
		}
		if reader.sess.readDone != nil {
			<-reader.sess.readDone
		}
	}()
}

func (t *tokenProcessor) release() {
	if t.cleanup == nil && t.cancel != nil {
		t.cancel()
	}
}

func (c *Conn) awaitResponse(ctx context.Context) error {
	if !c.connectionGood {
		return driver.ErrBadConn
	}
	if c.sess == nil || c.sess.cleanup == nil {
		return nil
	}
	pending := c.sess.cleanup
	select {
	case <-pending.done:
	default:
		select {
		case <-pending.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	c.sess.cleanup = nil
	if pending.err != nil {
		c.connectionGood = false
		return driver.ErrBadConn
	}
	return nil
}

func (c *Conn) responseError(reader *tokenProcessor, err error) error {
	if err == driver.ErrBadConn {
		// Keep checkBadConn's existing rejection of this sentinel after
		// execution; returning it would let database/sql retry executed SQL.
		return c.checkBadConn(reader.ctx, err, false)
	}
	switch reader.errorKind {
	case responseErrorOutput:
		reader.discard()
		return err
	case responseErrorFatal:
		c.connectionGood = false
		reader.release()
		return err
	default:
		return c.checkBadConn(reader.ctx, err, false)
	}
}
