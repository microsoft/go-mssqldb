package mssql

import (
	"context"
	"testing"
	"time"
)

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
