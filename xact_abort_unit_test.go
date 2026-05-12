package mssql

import (
	"context"
	"errors"
	"testing"

	"github.com/microsoft/go-mssqldb/msdsn"
	"github.com/stretchr/testify/assert"
)

// testRows creates a Rows with a valid stmt/Conn chain so that
// Rows.Close() can call checkBadConn without nil-pointer panics.
func testRows(ch chan tokenStruct, cancel func()) *Rows {
	sess := &tdsSession{logger: optionalLogger{}}
	return &Rows{
		stmt: &Stmt{c: &Conn{
			sess:           sess,
			connectionGood: true,
			connector:      &Connector{params: msdsn.Config{}},
		}},
		reader: &tokenProcessor{
			tokChan: ch,
			ctx:     context.Background(),
			sess:    sess,
		},
		cancel: cancel,
	}
}

func TestCheckServerAbortedTransaction_NotInTransaction(t *testing.T) {
	c := &Conn{
		sess:          &tdsSession{},
		inTransaction: false,
	}
	c.sess.tranid = 0
	assert.NoError(t, c.checkServerAbortedTransaction(),
		"should return nil when not in a transaction")
}

func TestCheckServerAbortedTransaction_ActiveTransaction(t *testing.T) {
	c := &Conn{
		sess:          &tdsSession{},
		inTransaction: true,
	}
	c.sess.tranid = 42
	assert.NoError(t, c.checkServerAbortedTransaction(),
		"should return nil when transaction is still active (tranid != 0)")
}

func TestCheckServerAbortedTransaction_AbortedTransaction(t *testing.T) {
	c := &Conn{
		sess:          &tdsSession{},
		inTransaction: true,
	}
	c.sess.tranid = 0
	err := c.checkServerAbortedTransaction()
	assert.Error(t, err, "should return error when inTransaction but tranid is 0")
	assert.Contains(t, err.Error(), "server does not have an active transaction")
}

func TestRowsClose_NoTokens(t *testing.T) {
	// Simulate a normal close where the channel is closed immediately (no tokens).
	ch := make(chan tokenStruct, 1)
	close(ch)

	rc := testRows(ch, func() {})

	err := rc.Close()
	assert.NoError(t, err, "Close with no tokens should return nil")
}

func TestRowsClose_NormalTokensThenClose(t *testing.T) {
	// Simulate tokens that are not errors followed by channel close.
	ch := make(chan tokenStruct, 3)
	ch <- doneStruct{Status: 0} // no error
	ch <- doneStruct{Status: 0} // no error
	close(ch)

	rc := testRows(ch, func() {})

	err := rc.Close()
	assert.NoError(t, err, "Close with non-error tokens should return nil")
}

func TestRowsClose_DoneStructWithError(t *testing.T) {
	// Simulate a doneStruct with doneError status and errors slice.
	ch := make(chan tokenStruct, 2)
	ch <- doneStruct{
		Status: doneError,
		errors: []Error{{Number: 245, Message: "Conversion failed"}},
	}
	close(ch)

	rc := testRows(ch, func() {})

	err := rc.Close()
	assert.Error(t, err, "Close should return error from doneStruct")
	assert.Contains(t, err.Error(), "Conversion failed")
}

func TestRowsClose_FirstErrorWins(t *testing.T) {
	// When multiple doneStructs have errors, the first one should be returned.
	ch := make(chan tokenStruct, 3)
	ch <- doneStruct{
		Status: doneError,
		errors: []Error{{Number: 245, Message: "First error"}},
	}
	ch <- doneStruct{
		Status: doneError,
		errors: []Error{{Number: 999, Message: "Second error"}},
	}
	close(ch)

	rc := testRows(ch, func() {})

	err := rc.Close()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "First error",
		"should return the first error, not the second")
}

func TestRowsClose_ErrorFromNextTokenWithCloseErr(t *testing.T) {
	// When a doneStruct error was captured and then nextToken returns
	// a real error (not ctx.Err()), the nextToken error is returned
	// because it represents a stream-level failure.
	ch := make(chan tokenStruct, 3)
	ch <- doneStruct{
		Status: doneError,
		errors: []Error{{Number: 245, Message: "Server error"}},
	}
	// ServerError implements error, so nextToken returns it as an error.
	ch <- ServerError{sqlError: Error{Number: 50000, Message: "nextToken error"}}
	close(ch)

	rc := testRows(ch, func() {})

	err := rc.Close()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "SQL Server had internal error",
		"nextToken stream error should be returned over closeErr")
}

func TestRowsClose_ErrorFromNextTokenWithoutCloseErr(t *testing.T) {
	// When nextToken returns an error and no doneStruct error was seen,
	// the nextToken error should be returned.
	ch := make(chan tokenStruct, 2)
	// ServerError implements error, so nextToken returns it as an error.
	ch <- ServerError{sqlError: Error{Number: 50000, Message: "stream error"}}
	close(ch)

	rc := testRows(ch, func() {})

	err := rc.Close()
	assert.Error(t, err)
	// ServerError.Error() returns "SQL Server had internal error"
	assert.Contains(t, err.Error(), "SQL Server had internal error")
}

func TestRowsClose_CancelIsCalled(t *testing.T) {
	// Verify that cancel() is called even when Close returns an error.
	ch := make(chan tokenStruct, 1)
	close(ch)

	cancelCalled := false
	rc := testRows(ch, func() { cancelCalled = true })

	rc.Close()
	assert.True(t, cancelCalled, "cancel() should be called via defer")
}

func TestRowsClose_CancelCalledOnError(t *testing.T) {
	ch := make(chan tokenStruct, 2)
	ch <- doneStruct{
		Status: doneError,
		errors: []Error{{Number: 1, Message: "error"}},
	}
	close(ch)

	cancelCalled := false
	rc := testRows(ch, func() { cancelCalled = true })

	rc.Close()
	assert.True(t, cancelCalled,
		"cancel() should be called via defer even when returning error")
}

func TestRowsClose_DoneInProcIgnored(t *testing.T) {
	// doneInProcStruct should NOT be treated as an error source in Close().
	// In processSingleResponse, errors from tokenError are accumulated in
	// the errs slice and attached to the next doneStruct/doneProcStruct.
	// A doneInProcStruct with its own errors field would be a duplicate;
	// the real error surfaces on the subsequent doneStruct. This test uses
	// a synthetic scenario to confirm Close() doesn't double-report.
	ch := make(chan tokenStruct, 2)
	ch <- doneInProcStruct{
		Status: doneError,
		errors: []Error{{Number: 245, Message: "in-proc error"}},
	}
	close(ch)

	rc := testRows(ch, func() {})

	err := rc.Close()
	assert.NoError(t, err,
		"doneInProcStruct errors should not be captured by Close")
}

func TestRowsClose_DoneStructNoErrorBit(t *testing.T) {
	// doneStruct with errors slice but no doneError status bit.
	// isError() returns true because len(d.errors) > 0.
	ch := make(chan tokenStruct, 2)
	ch <- doneStruct{
		Status: 0, // no doneError bit
		errors: []Error{{Number: 100, Message: "orphaned error"}},
	}
	close(ch)

	rc := testRows(ch, func() {})

	err := rc.Close()
	assert.Error(t, err,
		"should capture error when errors slice is non-empty even without doneError bit")
	assert.Contains(t, err.Error(), "orphaned error")
}

// Verify that the Error type returned by checkServerAbortedTransaction
// is recognized correctly.
func TestCheckServerAbortedTransaction_ErrorType(t *testing.T) {
	c := &Conn{
		sess:          &tdsSession{},
		inTransaction: true,
	}
	c.sess.tranid = 0

	err := c.checkServerAbortedTransaction()
	var mssqlErr Error
	assert.True(t, errors.As(err, &mssqlErr),
		"error should be of type mssql.Error")
	assert.Equal(t, int32(0), mssqlErr.Number)
}

func TestCheckServerAbortedTransaction_CommitOnDeadTransaction(t *testing.T) {
	c := &Conn{
		sess:           &tdsSession{},
		inTransaction:  true,
		connectionGood: true,
		connector:      &Connector{params: msdsn.Config{}},
	}
	c.sess.tranid = 0

	err := c.Commit()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "server does not have an active transaction")
	assert.False(t, c.inTransaction, "inTransaction should be cleared after Commit")
}

func TestCheckServerAbortedTransaction_RollbackOnDeadTransaction(t *testing.T) {
	c := &Conn{
		sess:           &tdsSession{},
		inTransaction:  true,
		connectionGood: true,
		connector:      &Connector{params: msdsn.Config{}},
	}
	c.sess.tranid = 0

	err := c.Rollback()
	assert.NoError(t, err, "Rollback on server-aborted transaction should succeed (nothing to send)")
	assert.False(t, c.inTransaction, "inTransaction should be cleared after Rollback")
}
