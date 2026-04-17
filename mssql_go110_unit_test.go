//go:build go1.10
// +build go1.10

package mssql

import (
	"context"
	"testing"

	"github.com/microsoft/go-mssqldb/msdsn"
	"github.com/stretchr/testify/assert"
)

func TestResult_LastInsertId(t *testing.T) {
	r := &Result{}
	id, err := r.LastInsertId()
	
	assert.Error(t, err, "LastInsertId() should return an error")
	assert.Equal(t, int64(-1), id, "LastInsertId() should return -1")
	
	expectedMsg := "LastInsertId is not supported"
	if err != nil {
		assert.Contains(t, err.Error(), expectedMsg, "error message should contain expected text")
	}
}

func TestConnector_Driver(t *testing.T) {
	drv := &Driver{}
	c := &Connector{
		driver: drv,
	}
	
	result := c.Driver()
	assert.Same(t, drv, result, "Driver() should return the same driver instance")
}

// TestRowsClose_SurfacesDoneStructErrors verifies that Rows.Close() returns
// errors from doneStruct tokens instead of silently discarding them.
// This is the unit test counterpart of the integration test
// TestIssue244_XactAbortErrorSurfaced.
func TestRowsClose_SurfacesDoneStructErrors(t *testing.T) {
	sess := &tdsSession{}
	conn := &Conn{
		sess:           sess,
		connectionGood: true,
		connector:      &Connector{params: msdsn.Config{}},
	}
	stmt := &Stmt{c: conn}

	ctx := context.Background()
	tokChan := make(chan tokenStruct, 5)
	reader := &tokenProcessor{
		tokChan: tokChan,
		ctx:     ctx,
		sess:    sess,
	}

	rows := &Rows{
		stmt:   stmt,
		reader: reader,
		cancel: func() {},
	}

	// Simulate the token stream: a row followed by an error-bearing doneStruct,
	// then close the channel to signal end of response.
	serverErr := Error{Number: 245, Message: "Conversion failed"}
	tokChan <- doneStruct{
		Status: doneError,
		errors: []Error{serverErr},
	}
	close(tokChan)

	err := rows.Close()
	assert.Error(t, err, "Close() should return an error from doneStruct")
	assert.Contains(t, err.Error(), "Conversion failed")
}

// TestRowsClose_NoErrorWhenClean verifies that Rows.Close() returns nil
// when the token stream has no errors.
func TestRowsClose_NoErrorWhenClean(t *testing.T) {
	sess := &tdsSession{}
	conn := &Conn{
		sess:           sess,
		connectionGood: true,
		connector:      &Connector{params: msdsn.Config{}},
	}
	stmt := &Stmt{c: conn}

	ctx := context.Background()
	tokChan := make(chan tokenStruct, 5)
	reader := &tokenProcessor{
		tokChan: tokChan,
		ctx:     ctx,
		sess:    sess,
	}

	rows := &Rows{
		stmt:   stmt,
		reader: reader,
		cancel: func() {},
	}

	// Simulate clean token stream: a done token with no error, then close.
	tokChan <- doneStruct{Status: doneCount, RowCount: 1}
	close(tokChan)

	err := rows.Close()
	assert.NoError(t, err, "Close() should return nil when no errors in token stream")
}
