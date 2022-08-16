//go:build !windows && go1.13
// +build !windows,go1.13

package mssql

import (
	"io/ioutil"
	"os"
	"testing"
)

func TestGetKrbParams(t *testing.T) {
	tempFile := createTempFile(t)
	defer os.Remove(tempFile)

	krbParams := map[string]interface{}{
		"Krb5ConfFile": tempFile,
		"KeytabFile":   tempFile,
		"KrbCache":     "path/to/cache",
	}

	_, err := getKrbParams(krbParams)
	if err == nil {
		t.Errorf("Error expected")
	}
}

func createTempFile(t *testing.T) string {
	file, err := ioutil.TempFile("", "test-*.txt")
	if err != nil {
		t.Fatalf("Failed to create a temp file:%v", err)
	}
	if _, err := file.Write([]byte("This is a test file\n")); err != nil {
		t.Fatalf("Failed to write file:%v", err)
	}
	return file.Name()
}
