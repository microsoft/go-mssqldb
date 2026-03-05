//go:build go1.10
// +build go1.10

package mssql

import (
	"testing"

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
