package mssql

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/golang-sql/sqlexp"
	"github.com/stretchr/testify/require"
)

type responseTrackingContext struct {
	context.Context
	children atomic.Int32
}

// Hide the underlying cancelCtx so context.WithCancel registers via AfterFunc.
func (*responseTrackingContext) Value(interface{}) interface{} { return nil }

func (c *responseTrackingContext) AfterFunc(f func()) func() bool {
	c.children.Add(1)
	return context.AfterFunc(c.Context, f)
}

func TestSynchronousResponsesBorrowOperationContext(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(*Conn, context.Context) error
	}{
		{"transaction response", func(c *Conn, ctx context.Context) error {
			return c.simpleProcessResp(ctx, false)
		}},
		{"exec response", func(c *Conn, ctx context.Context) error {
			_, err := (&Stmt{c: c}).processExec(ctx)
			return err
		}},
		{"bulk completion", func(c *Conn, ctx context.Context) error {
			c.sess.buf.BeginPacket(packBulkLoadBCP, false)
			_, err := (&Bulk{cn: c, ctx: ctx, headerSent: true}).Done()
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx := &responseTrackingContext{Context: parent}
			data := bytes.Repeat(compatReply(compatDone(doneFinal), true), 2)
			c := compatConn(&compatTransport{Reader: bytes.NewReader(data)})
			defer c.Close()
			c.sess.loginAck.TDSVersion = verTDS72
			for range 2 {
				require.NoError(t, tc.run(c, ctx))
				require.Zero(t, ctx.children.Load(), "synchronous response unexpectedly allocated a child cancellation")
				require.NoError(t, ctx.Err(), "response completion must not cancel the caller")
			}
		})
	}
}

func TestStartReadingSync_ContextOwnership(t *testing.T) {
	var output int64
	var status ReturnStatus
	for _, tc := range []struct {
		name string
		outs outputs
		owns bool
	}{
		{"no outputs", outputs{}, false},
		{"empty outputs", outputs{params: map[string]interface{}{}}, false},
		{"return status", outputs{returnStatus: &status}, false},
		{"output assignment", outputs{params: map[string]interface{}{"out": &output}}, true},
		{"message queue", outputs{msgq: &sqlexp.ReturnMessage{}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if tc.outs.msgq != nil {
					sqlexp.ReturnMessageInit(tc.outs.msgq)
				}
				transport := &compatTransport{Reader: bytes.NewReader(compatReply(compatDone(doneFinal), true))}
				sess := &tdsSession{buf: newTdsBuffer(defaultPacketSize, transport)}
				defer sess.buf.bufClose()
				reader := startReadingSync(sess, ctx, tc.outs)
				if tc.owns {
					require.NotNil(t, reader.cancel)
					require.NotSame(t, ctx, reader.ctx)
				} else {
					require.Nil(t, reader.cancel, "synchronous response must not allocate an owned cancellation")
					require.Same(t, ctx, reader.ctx, "retain the original operation context")
				}
				require.NoError(t, reader.iterateResponse())
				<-sess.readDone
				reader.release()
				require.NoError(t, ctx.Err(), "releasing a reader must not cancel the caller")
				if tc.owns {
					require.ErrorIs(t, reader.ctx.Err(), context.Canceled)
				}
				require.Zero(t, transport.writes.Load(), "successful completion must not send ATTENTION")
			})
		})
	}
}

func TestStartReadingSync_CallerCancellation(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(fmt.Sprintf("deadline_%v", deadline), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				client, server := net.Pipe()
				defer server.Close()
				defer client.Close()
				sess := &tdsSession{buf: newTdsBuffer(defaultPacketSize, client)}
				defer sess.buf.bufClose()
				ctx, cancel := context.WithCancel(context.Background())
				if deadline {
					cancel()
					ctx, cancel = context.WithTimeout(context.Background(), time.Second)
				}
				defer cancel()
				reader := startReadingSync(sess, ctx, outputs{})
				require.Nil(t, reader.cancel)
				reply := make(chan error, 1)
				go func() {
					var packet [8]byte
					_, err := io.ReadFull(server, packet[:])
					if err == nil && packetType(packet[0]) != packAttention {
						err = fmt.Errorf("got packet %d, want ATTENTION", packet[0])
					}
					if err == nil {
						_, err = server.Write(compatReply(compatDone(doneAttn), true))
					}
					reply <- err
				}()
				if !deadline {
					cancel()
				}
				_, err := reader.nextToken()
				require.True(t, err == ctx.Err(), "preserve the exact caller cancellation error")
				require.Error(t, err)
				require.NoError(t, <-reply)
				<-sess.readDone
				reader.release()
				c := &Conn{sess: sess, connectionGood: true}
				require.True(t, c.responseError(reader, err) == err)
				require.Equal(t, !deadline, c.IsValid(),
					"preserve checkBadConn's existing treatment of DeadlineExceeded as a net.Error")
			})
		})
	}
}

type responseContextPanicReader struct{ err error }

func (r responseContextPanicReader) Read([]byte) (int, error) { panic(r.err) }

func TestStartReadingSync_PreservesFatalErrorIdentity(t *testing.T) {
	for _, original := range []error{errors.New("provider error"), context.Canceled, context.DeadlineExceeded} {
		synctest.Test(t, func(t *testing.T) {
			transport := &compatTransport{Reader: responseContextPanicReader{original}}
			sess := &tdsSession{buf: newTdsBuffer(defaultPacketSize, transport)}
			defer sess.buf.bufClose()
			reader := startReadingSync(sess, context.Background(), outputs{})
			_, err := reader.nextToken()
			<-sess.readDone
			c := &Conn{sess: sess, connectionGood: true}
			require.True(t, c.responseError(reader, err) == original)
			require.False(t, c.IsValid(), "error origin must still make parser/provider failures fatal")
		})
	}
}

func TestExecOutputError_KeepsOwnedCleanup(t *testing.T) {
	for _, messages := range []bool{false, true} {
		t.Run(fmt.Sprintf("messages_%v", messages), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				original := errors.New("application rejected output")
				var later int64
				stream := compatOutput(t, "first", []byte{typeIntN, 8, 0})
				for range 40 {
					stream = append(stream, compatDone(doneMore)...)
				}
				stream = append(stream, compatOutput(t, "later", []byte{typeInt4, 42, 0, 0, 0})...)
				stream = append(stream, compatDone(doneFinal)...)
				data := append(compatReply(stream, true), compatReply(compatDone(doneFinal), true)...)
				transport := &compatTransport{Reader: bytes.NewReader(data)}
				c := compatConn(transport)
				db := sql.OpenDB(compatConnector{conn: c})
				defer db.Close()
				db.SetMaxOpenConns(1)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				args := []interface{}{
					sql.Named("first", sql.Out{Dest: &compatScanner{err: original}}),
					sql.Named("later", sql.Out{Dest: &later}),
				}
				if messages {
					args = append(args, &sqlexp.ReturnMessage{})
				}
				_, err := db.ExecContext(ctx, "output_procedure", args...)
				require.Same(t, original, err)
				require.NotNil(t, c.sess.cleanup)
				require.NotNil(t, c.sess.cleanup.cancel, "unfinished output responses still need owned cancellation")
				require.NoError(t, ctx.Err(), "output errors must not cancel the caller")
				_, err = db.ExecContext(context.Background(), "SELECT 1")
				require.NoError(t, err)
				require.Equal(t, int64(42), later)
				require.True(t, c.IsValid())
				require.NoError(t, ctx.Err())
				require.EqualValues(t, 2, transport.writes.Load(), "cleanup must not add an ATTENTION write")
			})
		})
	}
}
