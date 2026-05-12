//go:build go1.10
// +build go1.10

package mssql

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Integration tests for mssql_go110.go - require SQL Server connection

func TestConnector_Connect_Integration(t *testing.T) {
	// Skip if no database connection available
	checkConnStr(t)

	connector, err := NewConnector(makeConnStr(t).String())
	if err != nil {
		t.Fatalf("NewConnector failed: %v", err)
	}

	ctx := testContext(t)

	conn, err := connector.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer conn.Close()

	// Verify we got a valid connection
	if conn == nil {
		t.Fatal("Connect returned nil connection")
	}

	// Verify the connection implements driver.Conn
	mssqlConn, ok := conn.(*Conn)
	if !ok {
		t.Fatal("Connection is not *Conn type")
	}

	assert.True(t, mssqlConn.connectionGood, "Connection should be marked as good after successful connect")
}

func TestConnector_Driver_Integration(t *testing.T) {
	checkConnStr(t)

	connector, err := NewConnector(makeConnStr(t).String())
	if err != nil {
		t.Fatalf("NewConnector failed: %v", err)
	}

	drv := connector.Driver()
	if drv == nil {
		t.Fatal("Driver() returned nil")
	}

	// Verify it's the mssql driver
	_, ok := drv.(*Driver)
	assert.True(t, ok, "Driver() did not return *Driver type")
}

func TestConn_ResetSession_Integration(t *testing.T) {
	checkConnStr(t)

	connector, err := NewConnector(makeConnStr(t).String())
	if err != nil {
		t.Fatalf("NewConnector failed: %v", err)
	}

	ctx := testContext(t)

	conn, err := connector.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer conn.Close()

	mssqlConn := conn.(*Conn)

	// ResetSession should succeed on a good connection
	err = mssqlConn.ResetSession(ctx)
	assert.NoError(t, err, "ResetSession failed")

	assert.True(t, mssqlConn.resetSession, "resetSession flag should be true after ResetSession")
}

func TestConn_ResetSession_WithSessionInitSQL_Integration(t *testing.T) {
	checkConnStr(t)

	connStr := makeConnStr(t)
	connector, err := NewConnector(connStr.String())
	if err != nil {
		t.Fatalf("NewConnector failed: %v", err)
	}

	// Set a SessionInitSQL that should run on reset
	connector.SessionInitSQL = "SET NOCOUNT ON"

	ctx := testContext(t)

	conn, err := connector.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer conn.Close()

	mssqlConn := conn.(*Conn)

	// ResetSession should succeed and run the init SQL
	err = mssqlConn.ResetSession(ctx)
	assert.NoError(t, err, "ResetSession with SessionInitSQL failed")
}

func TestSqlOpen_WithConnector_Integration(t *testing.T) {
	checkConnStr(t)

	connector, err := NewConnector(makeConnStr(t).String())
	if err != nil {
		t.Fatalf("NewConnector failed: %v", err)
	}

	db := sql.OpenDB(connector)
	defer db.Close()

	ctx := testContext(t)

	// Verify the connection works by running a simple query
	var result int
	err = db.QueryRowContext(ctx, "SELECT 1").Scan(&result)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	assert.Equal(t, 1, result, "Expected 1")
}

func TestConnector_MultipleConnections_Integration(t *testing.T) {
	checkConnStr(t)

	connector, err := NewConnector(makeConnStr(t).String())
	if err != nil {
		t.Fatalf("NewConnector failed: %v", err)
	}

	ctx := testContext(t)

	// Create multiple connections from the same connector
	conn1, err := connector.Connect(ctx)
	if err != nil {
		t.Fatalf("First Connect failed: %v", err)
	}
	defer conn1.Close()

	conn2, err := connector.Connect(ctx)
	if err != nil {
		t.Fatalf("Second Connect failed: %v", err)
	}
	defer conn2.Close()

	// Verify they are different connections
	assert.NotEqual(t, conn1, conn2, "Expected different connection instances")

	// Verify both work
	mssqlConn1 := conn1.(*Conn)
	mssqlConn2 := conn2.(*Conn)

	assert.True(t, mssqlConn1.connectionGood && mssqlConn2.connectionGood, "Both connections should be marked as good")
}
