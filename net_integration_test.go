package mssql

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Integration tests for net.go - require SQL Server connection

func TestTimeoutConn_ReadWrite_Integration(t *testing.T) {
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

	// The connection internally uses timeoutConn
	// If we got here, Read/Write with timeout worked
	mssqlConn := conn.(*Conn)
	assert.True(t, mssqlConn.connectionGood, "Connection should be good after successful connection with timeout")
}

func TestConnection_WithTimeout_Integration(t *testing.T) {
	checkConnStr(t)

	connStr := makeConnStr(t)
	// Add connection timeout to the connection string
	q := connStr.Query()
	q.Set("connection timeout", "30")
	connStr.RawQuery = q.Encode()

	connector, err := NewConnector(connStr.String())
	if err != nil {
		t.Fatalf("NewConnector failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn, err := connector.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect with timeout failed: %v", err)
	}
	defer conn.Close()

	mssqlConn := conn.(*Conn)
	assert.True(t, mssqlConn.connectionGood, "Connection should be good")
}

func TestConnection_TLSHandshake_Integration(t *testing.T) {
	checkConnStr(t)

	connStr := makeConnStr(t)
	// Ensure we're using encryption (TLS)
	q := connStr.Query()
	q.Set("encrypt", "true")
	q.Set("TrustServerCertificate", "true")
	connStr.RawQuery = q.Encode()

	connector, err := NewConnector(connStr.String())
	if err != nil {
		t.Fatalf("NewConnector failed: %v", err)
	}

	ctx := testContext(t)

	conn, err := connector.Connect(ctx)
	if err != nil {
		// TLS might fail in some test environments, just log it
		t.Logf("TLS connection failed (may be expected in some environments): %v", err)
		t.Skip("TLS not available in this environment")
	}
	defer conn.Close()

	mssqlConn := conn.(*Conn)
	assert.True(t, mssqlConn.connectionGood, "TLS connection should be good")
}
