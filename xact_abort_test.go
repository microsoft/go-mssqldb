package mssql

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/golang-sql/sqlexp"
)

// Regression test for #244: XACT_ABORT rollback must surface the error
// and block subsequent operations on the dead transaction.
func TestXactAbortSurfacesError(t *testing.T) {
	connector, err := NewConnector(makeConnStr(t).String())
	if err != nil {
		t.Fatal(err)
	}
	connector.SessionInitSQL = "SET XACT_ABORT ON"
	db := sql.OpenDB(connector)
	defer db.Close()

	_, err = db.Exec(`
		IF OBJECT_ID('dbo.xact_abort_pk', 'U') IS NOT NULL DROP TABLE dbo.xact_abort_pk;
		CREATE TABLE dbo.xact_abort_pk (
			id INT PRIMARY KEY,
			val VARCHAR(50)
		)`)
	if err != nil {
		t.Fatal("failed to create table:", err)
	}
	defer db.Exec("DROP TABLE IF EXISTS dbo.xact_abort_pk")

	// Seed a row so we can trigger a PK violation inside the transaction.
	_, err = db.Exec("INSERT INTO dbo.xact_abort_pk (id, val) VALUES (1, 'existing')")
	if err != nil {
		t.Fatal("failed to seed row:", err)
	}

	tx, err := db.Begin()
	if err != nil {
		t.Fatal("failed to begin transaction:", err)
	}
	defer tx.Rollback()

	// First INSERT succeeds (different PK).
	_, err = tx.Exec("INSERT INTO dbo.xact_abort_pk (id, val) VALUES (2, 'ok')")
	if err != nil {
		t.Fatal("first insert should succeed:", err)
	}

	// Duplicate PK: error 2627. With XACT_ABORT ON the server rolls
	// back the entire transaction. The driver MUST surface this error.
	_, err = tx.Exec("INSERT INTO dbo.xact_abort_pk (id, val) VALUES (1, 'dup')")
	if err == nil {
		t.Fatal("expected PK violation error, got nil")
	}
	var mssqlErr Error
	if errors.As(err, &mssqlErr) {
		if mssqlErr.Number != 2627 {
			t.Errorf("expected error 2627 (PK violation), got %d: %s", mssqlErr.Number, mssqlErr.Message)
		}
	} else {
		t.Errorf("expected mssql.Error, got %T: %v", err, err)
	}

	// Subsequent operations on the dead transaction must fail via
	// checkServerAbortedTransaction.
	_, execErr := tx.Exec("INSERT INTO dbo.xact_abort_pk (id, val) VALUES (3, 'after')")
	if execErr == nil {
		t.Fatal("expected error from Exec on aborted transaction, got nil")
	}
	if errors.As(execErr, &mssqlErr) {
		if mssqlErr.Number != 0 {
			t.Errorf("expected mssql error 0 (aborted transaction guard), got %d: %s", mssqlErr.Number, mssqlErr.Message)
		}
	} else {
		t.Errorf("expected mssql.Error from dead transaction guard, got %T: %v", execErr, execErr)
	}

	// Verify rollback: seed row survives but in-txn rows do not.
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM dbo.xact_abort_pk").Scan(&count)
	if err != nil {
		t.Fatal("failed to count rows:", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row (only seed), got %d", count)
	}
}

// Same as TestXactAbortSurfacesError but exercises the sqlexp.ReturnMessage
// (Rowsq) path. Verifies the sendQuery guard works for both Rows and Rowsq.
func TestXactAbortWithReturnMessage(t *testing.T) {
	connector, err := NewConnector(makeConnStr(t).String())
	if err != nil {
		t.Fatal(err)
	}
	connector.SessionInitSQL = "SET XACT_ABORT ON"
	db := sql.OpenDB(connector)
	defer db.Close()

	_, err = db.Exec(`
		IF OBJECT_ID('dbo.xact_msg_pk', 'U') IS NOT NULL DROP TABLE dbo.xact_msg_pk;
		CREATE TABLE dbo.xact_msg_pk (
			id INT PRIMARY KEY,
			val VARCHAR(50)
		)`)
	if err != nil {
		t.Fatal("failed to create table:", err)
	}
	defer db.Exec("DROP TABLE IF EXISTS dbo.xact_msg_pk")

	// Seed a row for the PK violation.
	_, err = db.Exec("INSERT INTO dbo.xact_msg_pk (id, val) VALUES (1, 'existing')")
	if err != nil {
		t.Fatal(err)
	}

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	// First insert succeeds.
	_, err = tx.Exec("INSERT INTO dbo.xact_msg_pk (id, val) VALUES (2, 'ok')")
	if err != nil {
		t.Fatal("first insert should succeed:", err)
	}

	// Use sqlexp.ReturnMessage to exercise the message-loop (Rowsq) path.
	// Duplicate PK triggers error 2627 + XACT_ABORT rollback.
	ctx := context.Background()
	retmsg := &sqlexp.ReturnMessage{}
	rows, err := tx.QueryContext(ctx,
		"INSERT INTO dbo.xact_msg_pk (id, val) VALUES (1, 'dup'); SELECT 1", retmsg)
	if err != nil {
		t.Logf("QueryContext returned error (expected): %v", err)
	} else {
		// Drain the message queue to observe the error via MsgError
		var gotError bool
		active := true
		for active {
			msg := retmsg.Message(ctx)
			switch m := msg.(type) {
			case sqlexp.MsgError:
				t.Logf("got MsgError from message queue: %v", m.Error)
				gotError = true
				var mssqlErr Error
				if errors.As(m.Error, &mssqlErr) {
					if mssqlErr.Number != 2627 {
						t.Errorf("expected error 2627 (PK violation) in MsgError, got %d: %s", mssqlErr.Number, mssqlErr.Message)
					}
				}
			case sqlexp.MsgNextResultSet:
				active = rows.NextResultSet()
			case sqlexp.MsgNext:
				rows.Next()
			default:
				// MsgNotice, MsgRowsAffected, etc.
			}
		}
		rows.Close()
		if !gotError {
			t.Error("expected MsgError from the message queue due to PK violation, got none")
		}
	}

	// Subsequent query on the dead transaction must fail via
	// checkServerAbortedTransaction in sendQuery.
	_, execErr := tx.ExecContext(ctx, "INSERT INTO dbo.xact_msg_pk (id, val) VALUES (3, 'after')")
	if execErr == nil {
		t.Fatal("expected error from ExecContext on aborted transaction, got nil")
	}
	var mssqlErr Error
	if errors.As(execErr, &mssqlErr) {
		if mssqlErr.Number != 0 {
			t.Errorf("expected mssql error 0 (aborted transaction guard), got %d: %s", mssqlErr.Number, mssqlErr.Message)
		}
	} else {
		t.Errorf("expected mssql.Error from dead transaction guard, got %T: %v", execErr, execErr)
	}

	// Verify rollback: only seed row survives.
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM dbo.xact_msg_pk").Scan(&count)
	if err != nil {
		t.Fatal("failed to count rows:", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row (only seed), got %d", count)
	}
}
