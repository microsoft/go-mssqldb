package mssql

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang-sql/sqlexp"
)

// errNoStatementError is a sentinel used by the message-loop regression test to
// signal that the expected statement-scoped server error never surfaced.
var errNoStatementError = errors.New("no statement error surfaced")

// TestQueryErrorDoesNotHangConnection is an end-to-end regression test for
// issue #407. A batch whose first result-producing token is a statement-scoped
// error (here raised with RAISERROR, so no column metadata precedes the error
// DONE) followed by a large result set used to leak the response reader
// goroutine: processQueryResponse returned on the error DONE without draining
// the trailing rows, so sess.readDone was never closed and the NEXT query on
// the same pooled connection hung forever in startResponseReader.
//
// The test pins the pool to a single connection so the second query is
// guaranteed to reuse the connection left behind by the first, then asserts
// each query returns within a timeout.
func TestQueryErrorDoesNotHangConnection(t *testing.T) {
	checkConnStr(t)
	db, _ := open(t)
	defer db.Close()

	// Force reuse of the same underlying connection across queries.
	db.SetMaxOpenConns(1)

	// RAISERROR emits an error DONE with no preceding column metadata, then a
	// large result set follows in the same batch. This exercises the
	// processQueryResponse early-return-on-error path.
	const badBatch = "SET NOCOUNT ON; RAISERROR('issue407', 16, 1); SELECT TOP 500 name FROM sys.all_objects;"

	runQuery := func(attempt int) {
		done := make(chan error, 1)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			rows, err := db.QueryContext(ctx, badBatch)
			if rows != nil {
				rows.Close()
			}
			done <- err
		}()

		select {
		case err := <-done:
			// The statement-scoped error is expected to surface; the point of
			// the test is that the call returns rather than hanging.
			if err == nil {
				t.Fatalf("attempt %d: expected server error from RAISERROR, got nil", attempt)
			}
		case <-time.After(20 * time.Second):
			t.Fatalf("attempt %d: query hung, connection was not drained (issue #407)", attempt)
		}
	}

	// The first query leaks the reader before the fix; the second reuses the
	// connection and is where the hang manifested.
	for attempt := 1; attempt <= 3; attempt++ {
		runQuery(attempt)
	}
}

func TestQueryErrorCompletesRemainingBatch(t *testing.T) {
	checkConnStr(t)
	db, _ := open(t)
	defer db.Close()
	db.SetMaxOpenConns(1)

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec("CREATE TABLE #issue407_batch (value int NOT NULL)"); err != nil {
		t.Fatal(err)
	}

	const batch = `
SET NOCOUNT ON;
INSERT INTO #issue407_batch VALUES (1);
RAISERROR('issue407', 16, 1);
WAITFOR DELAY '00:00:00.200';
INSERT INTO #issue407_batch VALUES (2);`
	rows, err := tx.Query(batch)
	if rows != nil {
		rows.Close()
	}
	if err == nil {
		t.Fatal("expected server error from RAISERROR")
	}

	var count int
	if err := tx.QueryRow("SELECT COUNT(*) FROM #issue407_batch").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("statements after the error did not complete: got %d rows, want 2", count)
	}
}

// TestQueryErrorDoesNotHangMessageLoop is the issue #407 regression test for the
// message-loop (Rowsq) reader path that go-sqlcmd uses. That path is reached when
// the caller passes a *sqlexp.ReturnMessage: processQueryResponse returns a Rowsq
// immediately (before reading the error DONE) and the application pulls tokens via
// Next/NextResultSet/Close, so the inline drain added for the classic Rows path is
// never exercised here.
//
// Two abandonment patterns are covered, each with the pool pinned to a single
// connection so the following query is guaranteed to reuse the connection left
// behind by the batch that errored:
//   - "drain": the app runs the message loop to completion (normal go-sqlcmd
//     behaviour).
//   - "abandon": the app stops pulling messages as soon as the statement error
//     surfaces and relies on Rows.Close to unwind the reader, leaving the trailing
//     result set unread. This most closely mirrors the #407 leak.
//
// In both cases the batch itself and a subsequent query on the reused connection
// must return rather than hang.
func TestQueryErrorDoesNotHangMessageLoop(t *testing.T) {
	checkConnStr(t)

	// RAISERROR emits an error DONE with no preceding column metadata, then a
	// large result set follows in the same batch, so a reader that stops on the
	// error would leave the trailing rows buffered.
	const badBatch = "SET NOCOUNT ON; RAISERROR('issue407', 16, 1); SELECT TOP 500 name FROM sys.all_objects;"

	run := func(t *testing.T, stopOnError bool) {
		db, _ := open(t)
		defer db.Close()
		db.SetMaxOpenConns(1)

		once := func(attempt int) {
			done := make(chan error, 1)
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()

				retmsg := &sqlexp.ReturnMessage{}
				rows, qerr := db.QueryContext(ctx, badBatch, retmsg)
				if qerr != nil {
					done <- qerr
					return
				}
				defer rows.Close()

				sawError := false
				active := true
				for active {
					switch retmsg.Message(ctx).(type) {
					case sqlexp.MsgError:
						sawError = true
						if stopOnError {
							// Abandon the trailing result set; the deferred
							// Close must drain it.
							active = false
						}
					case sqlexp.MsgNextResultSet:
						active = rows.NextResultSet()
					case sqlexp.MsgNext:
						// One MsgNext is delivered per result set; the consumer
						// must drain every row with rows.Next() until it returns
						// false, per the sqlexp message-loop contract.
						for rows.Next() {
						}
					default:
						// MsgNotice, MsgRowsAffected, etc.
					}
				}

				if !sawError {
					done <- errNoStatementError
					return
				}
				done <- nil
			}()

			select {
			case err := <-done:
				if err == errNoStatementError {
					t.Fatalf("attempt %d: expected statement error from RAISERROR, got none", attempt)
				}
				if err != nil {
					t.Fatalf("attempt %d: unexpected error: %v", attempt, err)
				}
			case <-time.After(20 * time.Second):
				t.Fatalf("attempt %d: message-loop query hung, connection was not drained (issue #407)", attempt)
			}
		}

		// Attempt 1 abandons/errors; attempts 2 and 3 reuse the same connection,
		// which is where a leaked reader would hang.
		for attempt := 1; attempt <= 3; attempt++ {
			once(attempt)
		}
	}

	t.Run("drain", func(t *testing.T) { run(t, false) })
	t.Run("abandon", func(t *testing.T) { run(t, true) })
}
