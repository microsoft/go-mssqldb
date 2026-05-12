package mssql

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestServerError(t *testing.T) {

	originalErr := Error{Message: "underlying error"}
	sererErr := ServerError{sqlError: originalErr}

	// Verify that error message is backwards compatible
	oldMessage := "SQL Server had internal error"
	if newMessage := sererErr.Error(); newMessage != oldMessage {
		t.Fatalf("ServerError returned incompatible error message. Got '%s', wanted '%s'", newMessage, oldMessage)
	}

	// Verify that the underlying error is preserved
	unwrappedErr := sererErr.Unwrap()
	if underlyingErr, ok := unwrappedErr.(Error); !ok || underlyingErr.Message != originalErr.Message {
		t.Fatalf("ServerError did not preserve wrapped error. Got '%+v', wanted '%+v'", unwrappedErr, originalErr)
	}
}

func TestRetryableError(t *testing.T) {

	originalErr := driver.ErrBadConn
	retryableErr := RetryableError{err: originalErr}

	// Verify that the error message matches the original error's
	origMessage := originalErr.Error()
	if wrappedMessage := retryableErr.Error(); wrappedMessage != origMessage {
		t.Fatalf("RetryableError returned incorrect error message. Got '%s', wanted '%s'", wrappedMessage, origMessage)
	}

	// Verify that the underlying error is preserved
	unwrappedErr := retryableErr.Unwrap()
	if unwrappedErr != originalErr {
		t.Fatalf("RetryableError did not preserve wrapped error. Got '%+v', wanted '%+v'", unwrappedErr, originalErr)
	}

	// Verify that underlying error is correctly recognized
	if !retryableErr.Is(driver.ErrBadConn) {
		t.Fatalf("RetryableError wrapping driver.ErrBadConn does not report it is a driver.ErrBadConn error")
	}

}

func TestBadStreamPanic(t *testing.T) {

	errMsg := "test error XYZ"
	err := errors.New(errMsg)

	defer func() {
		r := recover()
		if e, ok := r.(error); !ok || !strings.HasSuffix(e.Error(), errMsg) {
			t.Fatalf("unexpected error recovered from panic: "+
				"got error = '%+v', wanted error to end with '%s'", e, errMsg)
		}
	}()

	badStreamPanic(err)

	t.Fatalf("badStreamPanic did not panic as expected when passed %+v", err)
}

func TestBadStreamPanicf(t *testing.T) {

	errfmt := "the error is '%s'"
	errMsg := "test error XYZ"
	expectedMsg := fmt.Sprintf(errfmt, errMsg)

	defer func() {
		r := recover()
		if e, ok := r.(error); !ok || !strings.HasSuffix(e.Error(), expectedMsg) {
			t.Fatalf("unexpected error recovered from panic: "+
				"got error = '%+v', wanted error to end with '%s'", e, expectedMsg)
		}
	}()

	badStreamPanicf(errfmt, errMsg)

	t.Fatalf("badStreamPanicf did not panic as expected when passed %s", expectedMsg)
}

func TestError_Methods(t *testing.T) {
	err := Error{
		Number:     12345,
		State:      1,
		Class:      16,
		Message:    "Test error message",
		ServerName: "TestServer",
		ProcName:   "TestProc",
		LineNo:     42,
	}

	// Test Error() method
	errorStr := err.Error()
	assert.Contains(t, errorStr, "mssql:", "Error() should contain 'mssql:'")
	assert.Contains(t, errorStr, err.Message, "Error() should contain message")

	// Test String() method
	assert.Equal(t, err.Message, err.String(), "String()")

	// Test SQLErrorNumber()
	assert.Equal(t, err.Number, err.SQLErrorNumber(), "SQLErrorNumber()")

	// Test SQLErrorState()
	assert.Equal(t, err.State, err.SQLErrorState(), "SQLErrorState()")

	// Test SQLErrorClass()
	assert.Equal(t, err.Class, err.SQLErrorClass(), "SQLErrorClass()")

	// Test SQLErrorMessage()
	assert.Equal(t, err.Message, err.SQLErrorMessage(), "SQLErrorMessage()")

	// Test SQLErrorServerName()
	assert.Equal(t, err.ServerName, err.SQLErrorServerName(), "SQLErrorServerName()")

	// Test SQLErrorProcName()
	assert.Equal(t, err.ProcName, err.SQLErrorProcName(), "SQLErrorProcName()")

	// Test SQLErrorLineNo()
	assert.Equal(t, err.LineNo, err.SQLErrorLineNo(), "SQLErrorLineNo()")
}
