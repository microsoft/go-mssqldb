package mssql

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"
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
	if !strings.Contains(errorStr, "mssql:") || !strings.Contains(errorStr, err.Message) {
		t.Errorf("Error() = %q, should contain 'mssql:' and message", errorStr)
	}

	// Test String() method
	if err.String() != err.Message {
		t.Errorf("String() = %q, want %q", err.String(), err.Message)
	}

	// Test SQLErrorNumber()
	if err.SQLErrorNumber() != err.Number {
		t.Errorf("SQLErrorNumber() = %d, want %d", err.SQLErrorNumber(), err.Number)
	}

	// Test SQLErrorState()
	if err.SQLErrorState() != err.State {
		t.Errorf("SQLErrorState() = %d, want %d", err.SQLErrorState(), err.State)
	}

	// Test SQLErrorClass()
	if err.SQLErrorClass() != err.Class {
		t.Errorf("SQLErrorClass() = %d, want %d", err.SQLErrorClass(), err.Class)
	}

	// Test SQLErrorMessage()
	if err.SQLErrorMessage() != err.Message {
		t.Errorf("SQLErrorMessage() = %q, want %q", err.SQLErrorMessage(), err.Message)
	}

	// Test SQLErrorServerName()
	if err.SQLErrorServerName() != err.ServerName {
		t.Errorf("SQLErrorServerName() = %q, want %q", err.SQLErrorServerName(), err.ServerName)
	}

	// Test SQLErrorProcName()
	if err.SQLErrorProcName() != err.ProcName {
		t.Errorf("SQLErrorProcName() = %q, want %q", err.SQLErrorProcName(), err.ProcName)
	}

	// Test SQLErrorLineNo()
	if err.SQLErrorLineNo() != err.LineNo {
		t.Errorf("SQLErrorLineNo() = %d, want %d", err.SQLErrorLineNo(), err.LineNo)
	}
}
