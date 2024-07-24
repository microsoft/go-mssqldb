//go:build !go1.10
// +build !go1.10

package mssql

import (
	"database/sql"
	"testing"
)

func openSettingGuidConversion(t *testing.T, guidConversion bool) (*sql.DB, *testLogger) {
	tl := testLogger{t: t}
	SetLogger(&tl)
	checkConnStr(t)
	conn, err := sql.Open("sqlserver", makeConnStrSettingGuidConversion(t, guidConversion).String())
	if err != nil {
		t.Error("Open connection failed:", err.Error())
		return nil, &tl
	}
	return conn, &tl
}

func open(t *testing.T) (*sql.DB, *testLogger) {
	return openSettingGuidConversion(t, false /*guidConversion*/)
}
