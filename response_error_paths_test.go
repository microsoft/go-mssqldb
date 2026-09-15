package mssql

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/binary"
	"errors"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/golang-sql/sqlexp"
	"github.com/stretchr/testify/require"
)

func TestResponseConsumers_RejectParserErrors(t *testing.T) {
	for _, action := range []struct {
		name string
		run  func(*Conn) error
	}{
		{"query", func(c *Conn) error {
			_, err := (&Stmt{c: c}).processQueryResponse(context.Background())
			return err
		}},
		{"exec", func(c *Conn) error {
			_, err := (&Stmt{c: c}).processExec(context.Background())
			return err
		}},
		{"transaction", func(c *Conn) error {
			return c.simpleProcessResp(context.Background(), false)
		}},
		{"bulk completion", func(c *Conn) error {
			_, err := (&Bulk{cn: c, ctx: context.Background(), headerSent: true}).Done()
			return err
		}},
		{"Rows.Next", func(c *Conn) error {
			reader := startReading(c.sess, context.Background(), outputs{})
			defer reader.release()
			return (&Rows{stmt: &Stmt{c: c}, reader: reader}).Next(nil)
		}},
		{"Rowsq.Next", func(c *Conn) error {
			reader := startReading(c.sess, context.Background(), outputs{})
			defer reader.release()
			return (&Rowsq{stmt: &Stmt{c: c}, reader: reader}).Next(nil)
		}},
	} {
		t.Run(action.name, func(t *testing.T) {
			original := errors.New("response could not be decoded")
			transport := &compatTransport{Reader: responseContextPanicReader{original}}
			c := compatConn(transport)
			defer c.Close()
			c.sess.buf.BeginPacket(packBulkLoadBCP, false)
			require.Same(t, original, action.run(c))
			<-c.sess.readDone
			require.False(t, c.IsValid(), "a parser failure must invalidate the connection")
			writes := transport.writes.Load()
			for range 2 {
				_, err := (&Stmt{c: c, query: "SELECT 1"}).exec(context.Background(), nil)
				require.ErrorIs(t, err, driver.ErrBadConn)
				require.Equal(t, writes, transport.writes.Load(), "failed responses must not be reused")
			}
		})
	}
}

type responseErrorScanner struct {
	err      error
	returned chan error
}

func (s *responseErrorScanner) Scan(interface{}) (err error) {
	defer func() { s.returned <- err }()
	return s.err
}

func TestRowsErrors_DrainLaterOutputs(t *testing.T) {
	for _, tc := range []struct {
		name      string
		statement bool
		messages  bool
	}{
		{"statement error", true, false},
		{"output assignment", false, false},
		{"message output assignment", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				original := errors.New("application output assignment failed")
				stream := append(colMetadataInt4(), rowInt4(7)...)
				if tc.statement {
					stream = append(stream, compatDone(doneError|doneMore)...)
				} else {
					stream = append(stream, compatOutput(t, "first", []byte{typeIntN, 8, 0})...)
				}
				for range 40 {
					stream = append(stream, compatDone(doneMore)...)
				}
				stream = append(stream, compatOutput(t, "again", []byte{typeIntN, 8, 0})...)
				stream = append(stream, compatOutput(t, "pause", []byte{typeInt4, 0, 0, 0, 0})...)
				stream = append(stream, compatOutput(t, "later", []byte{typeInt4, 42, 0, 0, 0})...)
				stream = append(stream, compatDone(doneFinal)...)
				transport := &compatTransport{Reader: bytes.NewReader(compatReply(stream, true))}
				c := compatConn(transport)
				resume := make(chan struct{})
				release := sync.OnceFunc(func() { close(resume) })
				pause := &blockingOutput{entered: make(chan struct{}), resume: resume}
				againError := errors.New("another assignment failed")
				again := &responseErrorScanner{err: againError, returned: make(chan error, 1)}
				var later int64
				c.outs.params = map[string]interface{}{
					"first": &compatScanner{err: original},
					"again": again,
					"pause": pause,
					"later": &later,
				}
				var messages *sqlexp.ReturnMessage
				if tc.messages {
					messages = &sqlexp.ReturnMessage{}
					sqlexp.ReturnMessageInit(messages)
					c.outs.msgq = messages
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				var rows driver.Rows
				defer func() {
					release()
					if rows != nil {
						rows.Close()
					}
					c.Close()
					synctest.Wait()
				}()
				var err error
				rows, err = (&Stmt{c: c}).processQueryResponse(ctx)
				require.NoError(t, err)
				if tc.messages {
					require.IsType(t, sqlexp.MsgNext{}, messages.Message(ctx))
				}
				require.Len(t, rows.Columns(), 1)
				values := make([]driver.Value, 1)
				require.NoError(t, rows.Next(values))
				require.Equal(t, int64(7), values[0])
				err = rows.Next(values)
				if tc.statement {
					require.IsType(t, Error{}, err)
				} else {
					require.Same(t, original, err)
				}
				synctest.Wait()
				select {
				case <-pause.entered:
				default:
					t.Fatal("the response producer did not reach the controlled output")
				}
				require.Len(t, again.returned, 1, "the later output error must actually occur during cleanup")
				require.Same(t, againError, <-again.returned)
				var reader *tokenProcessor
				if tc.messages {
					reader = rows.(*Rowsq).reader
				} else {
					reader = rows.(*Rows).reader
				}
				require.NotNil(t, reader.cleanup)
				require.NoError(t, reader.ctx.Err(), "recoverable output errors must leave cleanup running")
				for range 2 {
					require.NoError(t, rows.Close())
					require.NoError(t, reader.ctx.Err(), "closing abandoned rows must not cancel remaining SQL")
				}
				require.Zero(t, transport.writes.Load())
				release()
				require.NoError(t, c.awaitResponse(context.Background()))
				require.Equal(t, int64(42), later)
				require.True(t, c.IsValid())
				require.NoError(t, ctx.Err())
				require.Zero(t, transport.writes.Load(), "natural cleanup must not send ATTENTION")
			})
		})
	}
}

func TestCancellation_RejectsUnconfirmedSecondResponse(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload []byte
	}{
		{"missing confirmation", compatDone(doneFinal)},
		{"parser failure", []byte{0xff}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				first := make(chan tokenStruct)
				transport := &countingTransport{
					reader:  bytes.NewReader(compatReply(tc.payload, true)),
					onWrite: func() { close(first) },
				}
				c := compatConn(transport)
				defer c.Close()
				reader := &tokenProcessor{ctx: ctx, sess: c.sess, tokChan: first}
				_, err := reader.nextToken()
				require.IsType(t, StreamError{}, err)
				require.ErrorContains(t, err, "second response")
				require.True(t, c.responseError(reader, err) == err)
				require.False(t, c.IsValid())
				synctest.Wait()
				select {
				case <-c.sess.readDone:
				default:
					t.Fatal("failed second response left its producer running")
				}
				reads, writes := transport.counts()
				require.Positive(t, reads, "the cancellation response must actually have been read")
				require.Equal(t, 1, writes)
				for range 2 {
					tok, nextErr := reader.nextToken()
					require.Nil(t, tok)
					require.NoError(t, nextErr)
				}
				_, writes = transport.counts()
				require.Equal(t, 1, writes, "finished cancellation must not send more ATTENTION packets")
			})
		})
	}
}

func TestResponseMessages_InProcRowCount(t *testing.T) {
	done := compatDone(doneCount | doneMore)
	done[0] = byte(tokenDoneInProc)
	binary.LittleEndian.PutUint64(done[5:], 42)
	stream := append(done, compatDone(doneFinal)...)
	c := compatConn(&compatTransport{Reader: bytes.NewReader(compatReply(stream, true))})
	defer c.Close()
	messages := &sqlexp.ReturnMessage{}
	sqlexp.ReturnMessageInit(messages)
	reader := startReading(c.sess, context.Background(), outputs{msgq: messages})
	defer reader.release()
	require.NoError(t, reader.iterateResponse())
	<-c.sess.readDone
	require.EqualValues(t, 42, reader.rowCount)
	require.NoError(t, sqlexp.ReturnMessageEnqueue(context.Background(), messages, msgSentinel{}))
	require.Equal(t, sqlexp.MsgRowsAffected{Count: 42}, messages.Message(context.Background()))
	for range 3 {
		require.IsType(t, sqlexp.MsgNextResultSet{}, messages.Message(context.Background()))
	}
	require.IsType(t, msgSentinel{}, messages.Message(context.Background()))
}
