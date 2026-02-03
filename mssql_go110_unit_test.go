//go:build go1.10
// +build go1.10

package mssql

import (
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
	if err != nil && !contains(err.Error(), expectedMsg) {
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

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
