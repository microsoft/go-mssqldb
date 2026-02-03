//go:build go1.10
// +build go1.10

package mssql

import (
	"strings"
	"testing"
)

func TestResult_LastInsertId(t *testing.T) {
	r := &Result{}
	id, err := r.LastInsertId()
	
	if err == nil {
		t.Error("LastInsertId() should return an error")
	}
	
	if id != -1 {
		t.Errorf("LastInsertId() = %d, want -1", id)
	}
	
	expectedMsg := "LastInsertId is not supported"
	if err != nil && !strings.Contains(err.Error(), expectedMsg) {
		t.Errorf("LastInsertId() error = %v, should contain %q", err, expectedMsg)
	}
}

func TestConnector_Driver(t *testing.T) {
	drv := &Driver{}
	c := &Connector{
		driver: drv,
	}
	
	result := c.Driver()
	if result != drv {
		t.Error("Driver() should return the same driver instance")
	}
}
