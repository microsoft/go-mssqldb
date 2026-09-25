package mssql

import (
	"context"
	"database/sql"
	"net"
	"testing"
	"time"

	"github.com/microsoft/go-mssqldb/msdsn"
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
	// URL() emits the canonical lowercase key, so use the constant to match exactly
	q.Del(msdsn.TrustServerCertificate)
	q.Set(msdsn.TrustServerCertificate, "true")
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

// TestConnTimeout_AppliesToQueryExecution_ByDefault_Integration is a regression
// test for the historical, backward-compatible behavior of "connection
// timeout": by default it keeps being re-applied as a socket read/write
// deadline for the whole lifetime of the connection, so a long-running
// command can be cut short by it even though the caller's context has a much
// longer deadline.
func TestConnTimeout_AppliesToQueryExecution_ByDefault_Integration(t *testing.T) {
	checkConnStr(t)

	connStr := makeConnStr(t)
	q := connStr.Query()
	q.Set("connection timeout", "2")
	connStr.RawQuery = q.Encode()

	connector, err := NewConnector(connStr.String())
	if err != nil {
		t.Fatalf("NewConnector failed: %v", err)
	}
	db := sql.OpenDB(connector)
	defer db.Close()

	// Establish the connection (and complete login) first, outside the
	// timer below. Otherwise a login/handshake failure hitting the same
	// "connection timeout" could satisfy the assertions below just as
	// well as a query-execution timeout would, without actually
	// exercising the behavior under test.
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("PingContext failed: %v", err)
	}

	// The context deadline (20s) is much longer than "connection timeout"
	// (2s), so if the command fails before the context deadline elapses,
	// it must have been the connection timeout that cut it short.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	start := time.Now()
	_, err = db.ExecContext(ctx, "waitfor delay '00:00:05'")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected the command to fail due to the connection timeout, but it succeeded")
	}
	if err == context.DeadlineExceeded {
		t.Fatalf("expected a connection-timeout failure, not context.DeadlineExceeded: %v", err)
	}
	// Assert the failure is specifically a socket timeout (i/o timeout),
	// not merely "any error before 5s" (which a failed login or an
	// unrelated network/server error would also satisfy).
	if neterr, ok := err.(net.Error); !ok || !neterr.Timeout() {
		t.Fatalf("expected a net.Error timeout, got %T: %v", err, err)
	}
	assert.Less(t, elapsed, 5*time.Second, "command should have failed well before the 5s WAITFOR DELAY completed")
}

// TestConnTimeout_ScopedToLoginOnly_WhenDisabled_Integration verifies the new
// opt-in behavior: with disableconntimeoutasquerytimeout=true, "connection
// timeout" only bounds the login/handshake phase, so a long-running command
// that exceeds it is no longer cut short and instead completes, governed
// solely by the context deadline.
func TestConnTimeout_ScopedToLoginOnly_WhenDisabled_Integration(t *testing.T) {
	checkConnStr(t)

	connStr := makeConnStr(t)
	q := connStr.Query()
	q.Set("connection timeout", "2")
	q.Set(msdsn.DisableConnTimeoutAsQueryTimeout, "true")
	connStr.RawQuery = q.Encode()

	connector, err := NewConnector(connStr.String())
	if err != nil {
		t.Fatalf("NewConnector failed: %v", err)
	}
	db := sql.OpenDB(connector)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	_, err = db.ExecContext(ctx, "waitfor delay '00:00:05'")
	assert.NoError(t, err, "command should complete despite exceeding the connection timeout, since it is disabled as a query timeout")
}
