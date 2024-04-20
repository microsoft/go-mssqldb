//go:build go1.10
// +build go1.10

package mssql

import (
	"database/sql"
	"testing"
)

func open(t testing.TB) (*sql.DB, *testLogger) {
	connector, logger := getTestConnector(t)
	conn := sql.OpenDB(connector)
	return conn, logger
}

func getTestConnector(t testing.TB) (*Connector, *testLogger) {
	tl := testLogger{t: t}
	SetLogger(&tl)
	connector, err := NewConnector(makeConnStr(t).String())
	if err != nil {
		t.Error("Open connection failed:", err.Error())
		return nil, &tl
	}
	return connector, &tl
}

func openWithVarcharDSN(t testing.TB) (*sql.DB, *testLogger) {
	connector, logger := getTestConnectorWithVarcharDSN(t)
	conn := sql.OpenDB(connector)
	return conn, logger
}

func getTestConnectorWithVarcharDSN(t testing.TB) (*Connector, *testLogger) {
	tl := testLogger{t: t}
	SetLogger(&tl)
	s := testConnParams(t)
	s.SendStringParametersAsUnicode = true
	connector, err := NewConnector(s.URL().String())
	if err != nil {
		t.Error("Open connection failed:", err.Error())
		return nil, &tl
	}
	return connector, &tl
}
