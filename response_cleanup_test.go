package mssql

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net"
	"testing"
	"testing/synctest"
	"time"

	"github.com/golang-sql/sqlexp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type blockingOutput struct {
	entered chan struct{}
	resume  <-chan struct{}
}

func (s *blockingOutput) Scan(interface{}) error {
	close(s.entered)
	<-s.resume
	return nil
}

func (blockingOutput) Value() (driver.Value, error) { return int64(0), nil }

func TestResponseCleanup_SerializesRequests(t *testing.T) {
	for _, action := range []struct {
		name string
		run  func(*Conn) error
	}{
		{"query", func(c *Conn) error {
			rows, err := (&Stmt{c: c, query: "SELECT 1"}).queryContext(context.Background(), nil)
			if err == nil {
				err = rows.Close()
			}
			return err
		}},
		{"exec", func(c *Conn) error {
			_, err := (&Stmt{c: c, query: "SELECT 1"}).exec(context.Background(), nil)
			return err
		}},
		{"begin", func(c *Conn) error { _, err := c.Begin(); return err }},
		{"commit", func(c *Conn) error { return c.Commit() }},
		{"rollback", func(c *Conn) error { return c.Rollback() }},
		{"reset", func(c *Conn) error {
			c.connector.SessionInitSQL = "SELECT 1"
			return c.ResetSession(context.Background())
		}},
		{"bulk row", func(c *Conn) error {
			b := &Bulk{cn: c, ctx: context.Background(), headerSent: true}
			if err := b.AddRow(nil); err != nil {
				return err
			}
			return c.sess.buf.FinishPacket()
		}},
		{"bulk done", func(c *Conn) error {
			b := &Bulk{cn: c, ctx: context.Background(), headerSent: true}
			_, err := b.Done()
			return err
		}},
	} {
		t.Run(action.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				tail := compatDone(doneError | doneMore)
				for range 40 {
					tail = append(tail, compatDone(doneMore)...)
				}
				tail = append(tail, compatOutput(t, "pause", []byte{typeInt4, 42, 0, 0, 0})...)
				tail = append(tail, compatDone(doneFinal)...)
				data := append(compatReply(tail, true), compatReply(compatDone(doneFinal), true)...)
				transport := &compatTransport{Reader: bytes.NewReader(data)}
				c := compatConn(transport)
				defer c.sess.buf.bufClose()
				c.inTransaction, c.sess.tranid = true, 1
				c.sess.buf.BeginPacket(packBulkLoadBCP, false)
				resume := make(chan struct{})
				scanner := &blockingOutput{entered: make(chan struct{}), resume: resume}
				c.outs.params = map[string]interface{}{"pause": scanner}

				_, err := (&Stmt{c: c}).processQueryResponse(context.Background())
				require.IsType(t, Error{}, err)
				synctest.Wait()
				select {
				case <-scanner.entered:
				default:
					close(resume)
					t.Fatal("the background drain did not consume more than the token-channel capacity")
				}

				result := make(chan error, 1)
				go func() { result <- action.run(c) }()
				synctest.Wait()
				assert.Empty(t, result, "the next request must wait for response cleanup")
				assert.Zero(t, transport.writes.Load(), "no request may be sent before cleanup")
				assert.Equal(t, 8, c.sess.buf.wpos, "buffered bulk writes must wait too")

				close(resume)
				synctest.Wait()
				require.Len(t, result, 1)
				require.NoError(t, <-result)
				assert.EqualValues(t, 1, transport.writes.Load())
				require.NoError(t, c.awaitResponse(context.Background()), "a completed cleanup must not block later calls")
				require.NoError(t, c.awaitResponse(context.Background()))
			})
		})
	}
}

func TestResponseCleanup_SeesServerTransactionState(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := compatConn(&compatTransport{})
		defer c.sess.buf.bufClose()
		c.inTransaction, c.sess.tranid = true, 1
		pending := &responseCleanup{done: make(chan struct{})}
		c.sess.cleanup = pending
		result := make(chan error, 1)
		go func() { result <- c.Commit() }()
		synctest.Wait()
		assert.Empty(t, result)
		// The prior response can report a server-side rollback.
		c.sess.tranid = 0
		close(pending.done)
		synctest.Wait()
		require.Len(t, result, 1)
		assert.ErrorContains(t, <-result, "server does not have an active transaction")
		assert.Zero(t, c.sess.buf.transport.(*compatTransport).writes.Load())
	})
}

func TestResponseCleanup_WaitCancellationDoesNotCancelPriorBatch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		transport := &compatTransport{}
		c := compatConn(transport)
		defer c.sess.buf.bufClose()
		priorCancelled := false
		pending := &responseCleanup{
			done:   make(chan struct{}),
			cancel: func() { priorCancelled = true },
		}
		c.sess.cleanup = pending
		assert.True(t, c.IsValid(), "a busy connection is not a failed connection")
		for range 2 {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			_, err := (&Stmt{c: c, query: "SELECT 1"}).exec(ctx, nil)
			cancel()
			assert.ErrorIs(t, err, context.DeadlineExceeded)
			assert.Same(t, pending, c.sess.cleanup)
			assert.True(t, c.IsValid())
			assert.False(t, priorCancelled)
			assert.Zero(t, transport.writes.Load())
		}
		close(pending.done)
		require.NoError(t, c.awaitResponse(context.Background()))
		assert.Nil(t, c.sess.cleanup)
	})
}

func TestResponseCleanup_FailurePreventsReuse(t *testing.T) {
	original := errors.New("response truncated")
	c := compatConn(&compatTransport{})
	defer c.sess.buf.bufClose()
	c.sess.cleanup = &responseCleanup{done: make(chan struct{}), err: original}
	close(c.sess.cleanup.done)
	assert.False(t, c.IsValid())
	for range 2 {
		_, err := (&Stmt{c: c, query: "SELECT 1"}).exec(context.Background(), nil)
		assert.ErrorIs(t, err, driver.ErrBadConn)
		assert.False(t, c.IsValid())
		assert.Zero(t, c.sess.buf.transport.(*compatTransport).writes.Load())
	}
}

func TestResponseCleanup_AbandonedMessageQueue(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		original := errors.New("application rejected the output")
		var later int64
		stream := compatOutput(t, "first", []byte{typeIntN, 8, 0})
		for range 40 {
			stream = append(stream, compatDone(doneMore)...)
		}
		stream = append(stream, compatOutput(t, "later", []byte{typeInt4, 42, 0, 0, 0})...)
		stream = append(stream, compatDone(doneFinal)...)
		transport := &compatTransport{Reader: bytes.NewReader(compatReply(stream, true))}
		c := compatConn(transport)
		defer c.sess.buf.bufClose()
		msg := &sqlexp.ReturnMessage{}
		sqlexp.ReturnMessageInit(msg)
		c.outs = outputs{
			params: map[string]interface{}{"first": &compatScanner{err: original}, "later": &later},
			msgq:   msg,
		}
		rows, err := (&Stmt{c: c}).processQueryResponse(context.Background())
		require.NoError(t, err)
		defer rows.Close()
		require.IsType(t, sqlexp.MsgNextResultSet{}, msg.Message(context.Background()))
		require.Same(t, original, rows.(driver.RowsNextResultSet).NextResultSet())
		require.NoError(t, rows.Close(), "automatic Rows.Close must not cancel the remaining batch")
		// Do not consume more messages: 40 DONEs exceed both queue capacities.
		synctest.Wait()
		select {
		case <-c.sess.cleanup.done:
		default:
			t.Fatal("background cleanup blocked on an abandoned message queue")
		}
		require.NoError(t, c.awaitResponse(context.Background()))
		assert.Equal(t, int64(42), later)
		assert.True(t, c.IsValid())
		assert.Zero(t, transport.writes.Load(), "dropping messages must not send ATTENTION")
	})
}

func TestResponseError_ApplicationErrorTypesRemainRecoverable(t *testing.T) {
	for _, original := range []error{
		context.Canceled,
		context.DeadlineExceeded,
		&net.OpError{Op: "application scanner", Err: errors.New("conversion failed")},
		StreamError{InnerError: errors.New("application scanner error")},
	} {
		synctest.Test(t, func(t *testing.T) {
			c := compatConn(&compatTransport{})
			defer c.sess.buf.bufClose()
			ch := make(chan tokenStruct, 2)
			ch <- outputParameterError{original}
			ch <- doneStruct{}
			close(ch)
			reader := &tokenProcessor{sess: c.sess, ctx: context.Background(), tokChan: ch}
			err := reader.iterateResponse()
			assert.True(t, c.responseError(reader, err) == original)
			synctest.Wait()
			require.NoError(t, c.awaitResponse(context.Background()))
			assert.True(t, c.IsValid(), "error origin, not its concrete type, determines response safety")
		})
	}
}

func TestResponseError_RejectsRetrySentinelAfterExecution(t *testing.T) {
	for _, kind := range []responseErrorKind{responseErrorNone, responseErrorOutput, responseErrorFatal} {
		c := &Conn{connectionGood: true, sess: &tdsSession{}}
		ch := make(chan tokenStruct)
		close(ch)
		reader := &tokenProcessor{ctx: context.Background(), sess: c.sess, tokChan: ch, errorKind: kind}
		assert.PanicsWithValue(t, "driver.ErrBadConn in checkBadConn. This should not happen.",
			func() { c.responseError(reader, driver.ErrBadConn) },
			"do not turn an invalid post-execution ErrBadConn into an automatic SQL retry")
	}
}

func TestReadCancelConfirmation_IgnoresOutputAssignmentErrors(t *testing.T) {
	for _, expired := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		if expired {
			cancel()
		}
		ch := make(chan tokenStruct, 2)
		ch <- outputParameterError{errors.New("application conversion failed")}
		ch <- doneStruct{Status: doneAttn}
		close(ch)
		result, err := readCancelConfirmation(ctx, ch)
		cancel()
		assert.NoError(t, err)
		assert.Equal(t, cancelConfirmationReceived, result)
	}
}

func TestResponseCleanup_KeepsOriginalMessageQueue(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := compatConn(&compatTransport{Reader: bytes.NewReader(compatReply(compatDone(doneFinal), true))})
		defer c.sess.buf.bufClose()
		msg := &sqlexp.ReturnMessage{}
		sqlexp.ReturnMessageInit(msg)
		reader := startReading(c.sess, context.Background(), outputs{msgq: msg})
		defer reader.release()
		// Prepared-statement arguments can be checked before waiting for cleanup.
		sqlexp.ReturnMessageInit(msg)
		synctest.Wait()
		require.NoError(t, sqlexp.ReturnMessageEnqueue(context.Background(), msg, msgSentinel{}))
		require.IsType(t, msgSentinel{}, msg.Message(context.Background()))
		require.IsType(t, sqlexp.MsgNextResultSet{}, reader.outs.msgq.Message(context.Background()))
		require.IsType(t, sqlexp.MsgNextResultSet{}, reader.outs.msgq.Message(context.Background()))
		for range reader.tokChan {
		}
		<-c.sess.readDone
	})
}

func TestResponseCleanup_CloseCancelsAndWaitsForBufferUsers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client, server := net.Pipe()
		defer server.Close()
		c := compatConn(client)
		readDone := make(chan struct{})
		queryCtx, cancel := context.WithCancel(context.Background())
		reader := &tokenProcessor{
			ctx: queryCtx, cancel: cancel, sess: c.sess, tokChan: make(chan tokenStruct),
		}
		c.sess.readDone = readDone
		returnBuffer := c.sess.buf.bufClose
		bufferReturned := false
		c.sess.buf.bufClose = func() { bufferReturned = true; returnBuffer() }
		go func() {
			// The response stays silent until the transport closes.
			_, err := client.Read(make([]byte, 1))
			reader.tokChan <- err
			close(reader.tokChan)
			close(readDone)
		}()
		reader.discard()
		pending := reader.cleanup
		reader.discard()
		assert.Same(t, pending, reader.cleanup, "repeated handoff must not create another consumer")
		synctest.Wait()
		assert.False(t, bufferReturned)
		require.NoError(t, c.Close())
		synctest.Wait()
		assert.ErrorIs(t, queryCtx.Err(), context.Canceled)
		assert.True(t, bufferReturned, "close must wait for cleanup and the reader before pooling the buffer")
		select {
		case <-pending.done:
		default:
			t.Fatal("close left cleanup running")
		}
		select {
		case <-readDone:
		default:
			t.Fatal("close left the response reader running")
		}
	})
}

func TestResponseCleanup_FinishedReaderDoesNotRestart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader := &tokenProcessor{finished: true, ctx: ctx, cancel: cancel}
	for range 2 {
		reader.discard()
		assert.Nil(t, reader.cleanup)
		assert.ErrorIs(t, ctx.Err(), context.Canceled)
	}
}

func TestResponseCleanup_AssignsReturnStatus(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var status ReturnStatus
		ch := make(chan tokenStruct, 2)
		ch <- ReturnStatus(17)
		ch <- doneStruct{}
		close(ch)
		reader := &tokenProcessor{
			ctx: context.Background(), sess: &tdsSession{}, tokChan: ch,
			outs: outputs{returnStatus: &status},
		}
		reader.discard()
		<-reader.cleanup.done
		assert.NoError(t, reader.cleanup.err)
		assert.Equal(t, ReturnStatus(17), status)
	})
}

func TestRowsqColumns_PreservesOriginalError(t *testing.T) {
	for _, recoverable := range []bool{false, true} {
		synctest.Test(t, func(t *testing.T) {
			original := errors.New("cannot read this value")
			c := compatConn(&compatTransport{})
			defer c.sess.buf.bufClose()
			ch := make(chan tokenStruct, 2)
			if recoverable {
				ch <- outputParameterError{original}
				ch <- doneStruct{}
			} else {
				ch <- original
			}
			close(ch)
			r := &Rowsq{
				stmt:   &Stmt{c: c},
				reader: &tokenProcessor{ctx: context.Background(), sess: c.sess, tokChan: ch},
			}
			for range 2 {
				assert.Empty(t, r.Columns())
				assert.Same(t, original, r.Next(nil))
				assert.Same(t, original, r.NextResultSet())
			}
			synctest.Wait()
			assert.Equal(t, recoverable, c.IsValid())
		})
	}
}

func TestResponseCleanup_SQLPoolReuseWaits(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		stream := compatDone(doneError | doneMore)
		for range 40 {
			stream = append(stream, compatDone(doneMore)...)
		}
		stream = append(stream, compatOutput(t, "pause", []byte{typeInt4, 42, 0, 0, 0})...)
		stream = append(stream, compatDone(doneFinal)...)
		data := append(compatReply(stream, true), compatReply(compatDone(doneFinal), true)...)
		transport := &compatTransport{Reader: bytes.NewReader(data)}
		c := compatConn(transport)
		db := sql.OpenDB(compatConnector{conn: c})
		defer db.Close()
		db.SetMaxOpenConns(1)
		resume := make(chan struct{})
		output := &blockingOutput{entered: make(chan struct{}), resume: resume}
		rows, err := db.QueryContext(context.Background(), "batch", sql.Named("pause", sql.Out{Dest: output}))
		require.Nil(t, rows)
		require.IsType(t, Error{}, err)
		synctest.Wait()
		select {
		case <-output.entered:
		default:
			close(resume)
			t.Fatal("the response was not drained past its token-channel capacity")
		}
		result := make(chan error, 1)
		go func() {
			_, err := db.ExecContext(context.Background(), "SELECT 1")
			result <- err
		}()
		synctest.Wait()
		assert.Empty(t, result)
		assert.EqualValues(t, 1, transport.writes.Load(), "pool reuse must wait before sending the second query")
		close(resume)
		synctest.Wait()
		require.Len(t, result, 1)
		require.NoError(t, <-result)
		assert.EqualValues(t, 2, transport.writes.Load())
		assert.True(t, c.IsValid())
	})
}
