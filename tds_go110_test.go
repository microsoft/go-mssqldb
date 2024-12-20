//go:build go1.10
// +build go1.10

package mssql

import (
	"database/sql"
	"testing"
)

func openSettingGuidConversion(t testing.TB, guidConversion bool) (*sql.DB, *testLogger) {
	connector, logger := getTestConnector(t, guidConversion)
	conn := sql.OpenDB(connector)
	return conn, logger
}

func open(t testing.TB) (*sql.DB, *testLogger) {
	return openSettingGuidConversion(t, false /*guidConversion*/)
}

func getTestConnector(t testing.TB, guidConversion bool) (*Connector, *testLogger) {
	tl := testLogger{t: t}
	SetLogger(&tl)

	connectionString := makeConnStrSettingGuidConversion(t, guidConversion).String()
	connector, err := NewConnector(connectionString)
	if err != nil {
		t.Error("Open connection failed:", err.Error())
		return nil, &tl
	}
	return connector, &tl
}
