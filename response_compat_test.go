package mssql

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/golang-sql/sqlexp"
	"github.com/microsoft/go-mssqldb/msdsn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These compatibility tests use only interfaces present before PR #410 so the
// same tests can be run against the base commit as well as the repaired driver.
type compatTransport struct {
	io.Reader
	onWrite func([]byte) (int, error)
	writes  atomic.Int32
	closes  atomic.Int32
}

func (c *compatTransport) Write(p []byte) (int, error) {
	c.writes.Add(1)
	if c.onWrite != nil {
		return c.onWrite(p)
	}
	return len(p), nil
}

func (c *compatTransport) Close() error {
	c.closes.Add(1)
	if r, ok := c.Reader.(io.Closer); ok {
		return r.Close()
	}
	return nil
}

func (*compatTransport) LocalAddr() net.Addr              { return nil }
func (*compatTransport) RemoteAddr() net.Addr             { return nil }
func (*compatTransport) SetDeadline(time.Time) error      { return nil }
func (*compatTransport) SetReadDeadline(time.Time) error  { return nil }
func (*compatTransport) SetWriteDeadline(time.Time) error { return nil }

func compatReply(payload []byte, final bool) []byte {
	p := make([]byte, 8+len(payload))
	p[0], p[6] = byte(packReply), 1
	if final {
		p[1] = 1
	}
	binary.BigEndian.PutUint16(p[2:4], uint16(len(p)))
	copy(p[8:], payload)
	return p
}

func compatDone(status uint16) []byte {
	p := make([]byte, 13)
	p[0] = byte(tokenDone)
	binary.LittleEndian.PutUint16(p[1:3], status)
	return p
}

func compatOutput(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	b.WriteByte(byte(tokenReturnValue))
	require.NoError(t, binary.Write(&b, binary.LittleEndian, uint16(0)))
	require.NoError(t, writeBVarChar(&b, "@"+name))
	b.WriteByte(0)
	require.NoError(t, binary.Write(&b, binary.LittleEndian, uint32(0)))
	require.NoError(t, binary.Write(&b, binary.LittleEndian, uint16(0)))
	b.Write(data)
	return b.Bytes()
}

func compatConn(transport io.ReadWriteCloser) *Conn {
	return &Conn{
		sess:           &tdsSession{buf: newTdsBuffer(defaultPacketSize, transport)},
		connectionGood: true,
		connector:      &Connector{params: msdsn.Config{}},
		transactionCtx: context.Background(),
	}
}

func TestCompatibility_ErrorIdentity(t *testing.T) {
	for _, original := range []error{
		errors.New("provider failed"),
		context.Canceled,
		context.DeadlineExceeded,
		&net.OpError{Op: "read", Err: errors.New("transport failed")},
		StreamError{InnerError: errors.New("malformed response")},
		ServerError{sqlError: Error{Number: 50000, Message: "server failed"}},
	} {
		for _, buffered := range []bool{false, true} {
			t.Run(original.Error(), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					ch := make(chan tokenStruct, 1)
					if buffered {
						ch <- original
						close(ch)
					} else {
						go func() {
							time.Sleep(time.Second)
							ch <- original
							close(ch)
						}()
					}
					r := &tokenProcessor{ctx: context.Background(), tokChan: ch, sess: &tdsSession{}}
					_, got := r.nextToken()
					require.IsType(t, original, got)
					if reflect.TypeOf(original).Comparable() {
						require.True(t, got == original, "the public error must retain its identity")
					} else {
						require.Equal(t, original, got)
					}
					assert.Equal(t, original.Error(), got.Error())
				})
			})
		}
	}
}

func TestCompatibility_StatementErrorReturnsBeforeBatchCompletes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		input, server := io.Pipe()
		defer input.Close()
		defer server.Close()
		attention := make(chan struct{})
		attentionOnce := sync.OnceFunc(func() { close(attention) })
		transport := &compatTransport{Reader: input, onWrite: func(p []byte) (int, error) {
			if p[0] == byte(packAttention) {
				attentionOnce()
			}
			return len(p), nil
		}}
		c := compatConn(transport)
		defer c.sess.buf.bufClose()
		completed := make(chan time.Time, 1)
		go func() {
			if _, err := server.Write(compatReply(compatDone(doneError|doneMore), false)); err != nil {
				return
			}
			select {
			case <-time.After(10 * time.Second):
				completed <- time.Now()
				_, _ = server.Write(compatReply(compatDone(doneFinal), true))
			case <-attention:
				_, _ = server.Write(compatReply(compatDone(doneAttn), true))
			}
		}()

		start := time.Now()
		rows, err := (&Stmt{c: c}).processQueryResponse(context.Background())
		require.Nil(t, rows)
		require.IsType(t, Error{}, err)
		assert.Less(t, time.Since(start), time.Second, "logical errors must return without waiting for the rest of the batch")

		time.Sleep(10 * time.Second)
		synctest.Wait()
		require.Len(t, completed, 1, "the later statement must execute, not be cancelled by cleanup")
		assert.GreaterOrEqual(t, (<-completed).Sub(start), 10*time.Second,
			"the fixture must actually exceed the removed five-second cleanup deadline")
		assert.Zero(t, transport.writes.Load(), "a logical error must not send ATTENTION")
		assert.True(t, c.IsValid())
	})
}

type compatScanner struct{ err error }

func (s *compatScanner) Scan(interface{}) error    { return s.err }
func (compatScanner) Value() (driver.Value, error) { return int64(0), nil }

func TestCompatibility_OutputErrorPreservesTransaction(t *testing.T) {
	for _, messages := range []bool{false, true} {
		t.Run(map[bool]string{false: "Exec", true: "ReturnMessage"}[messages], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				original := errors.New("application scanner rejected the value")
				var later int64
				stream := compatOutput(t, "first", []byte{typeIntN, 8, 0})
				stream = append(stream, compatOutput(t, "later", []byte{typeInt4, 42, 0, 0, 0})...)
				stream = append(stream, compatDone(doneFinal)...)
				data := append(compatReply(stream, true), compatReply(compatDone(doneFinal), true)...)
				transport := &compatTransport{Reader: bytes.NewReader(data)}
				c := compatConn(transport)
				defer c.sess.buf.bufClose()
				c.inTransaction, c.sess.tranid = true, 1
				first := &driver.NamedValue{Name: "first", Value: sql.Out{Dest: &compatScanner{err: original}}}
				require.NoError(t, c.CheckNamedValue(first))
				require.NoError(t, c.CheckNamedValue(&driver.NamedValue{Name: "later", Value: sql.Out{Dest: &later}}))

				var got error
				if messages {
					retmsg := &sqlexp.ReturnMessage{}
					require.ErrorIs(t, c.CheckNamedValue(&driver.NamedValue{Value: retmsg}), driver.ErrRemoveArgument)
					rows, err := (&Stmt{c: c}).processQueryResponse(context.Background())
					require.NoError(t, err)
					defer rows.Close()
					require.IsType(t, sqlexp.MsgNextResultSet{}, retmsg.Message(context.Background()))
					got = rows.(driver.RowsNextResultSet).NextResultSet()
				} else {
					_, got = (&Stmt{c: c}).processExec(context.Background())
				}
				require.True(t, got == original, "the original application error must be returned unchanged")
				synctest.Wait()
				select {
				case <-c.sess.readDone:
				default:
					t.Fatal("output conversion error left the response reader running")
				}
				assert.Equal(t, int64(42), later, "later output parameters must still be assigned")
				assert.True(t, c.IsValid(), "an assignment error is not a broken connection")
				require.NoError(t, c.Commit(), "the valid transaction must remain committable")
				assert.EqualValues(t, 1, transport.writes.Load(), "only COMMIT should be sent")
			})
		})
	}
}

type compatConnector struct{ conn *Conn }

func (c compatConnector) Connect(context.Context) (driver.Conn, error) { return c.conn, nil }
func (compatConnector) Driver() driver.Driver                          { return &Driver{} }

func TestCompatibility_SQLTransactionAfterOutputError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		original := errors.New("application scanner rejected the value")
		var later int64
		outputs := compatOutput(t, "first", []byte{typeIntN, 8, 0})
		outputs = append(outputs, compatOutput(t, "later", []byte{typeInt4, 42, 0, 0, 0})...)
		outputs = append(outputs, compatDone(doneFinal)...)
		data := compatReply(compatDone(doneFinal), true) // BEGIN
		data = append(data, compatReply(outputs, true)...)
		data = append(data, compatReply(compatDone(doneFinal), true)...) // subsequent command
		data = append(data, compatReply(compatDone(doneFinal), true)...) // COMMIT
		transport := &compatTransport{Reader: bytes.NewReader(data)}
		c := compatConn(transport)
		c.sess.tranid = 1
		db := sql.OpenDB(compatConnector{conn: c})
		defer db.Close()
		db.SetMaxOpenConns(1)
		tx, err := db.Begin()
		require.NoError(t, err)
		defer tx.Rollback()
		_, err = tx.Exec("output_procedure",
			sql.Named("first", sql.Out{Dest: &compatScanner{err: original}}),
			sql.Named("later", sql.Out{Dest: &later}),
		)
		require.Same(t, original, err)
		_, err = tx.Exec("SELECT 1")
		require.NoError(t, err)
		assert.Equal(t, int64(42), later)
		require.NoError(t, tx.Commit())
		assert.True(t, c.IsValid())
		assert.EqualValues(t, 4, transport.writes.Load())
	})
}

type compatDeadlineConn struct {
	compatTransport
	deadline time.Time
}

func (c *compatDeadlineConn) SetDeadline(deadline time.Time) error {
	c.deadline = deadline
	return nil
}

func TestCompatibility_AttentionUsesTransportTimeout(t *testing.T) {
	for _, timeout := range []time.Duration{0, 20 * time.Second, 2 * time.Second} {
		t.Run(timeout.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ch := make(chan tokenStruct, 1)
				finish := sync.OnceFunc(func() { close(ch) })
				defer finish()
				original := &net.OpError{Op: "write", Err: context.DeadlineExceeded}
				raw := &compatDeadlineConn{}
				writeDone := make(chan struct{})
				raw.onWrite = func(p []byte) (int, error) {
					defer close(writeDone)
					delay := 10 * time.Second
					if !raw.deadline.IsZero() && time.Until(raw.deadline) < delay {
						time.Sleep(time.Until(raw.deadline))
						return 0, original
					}
					time.Sleep(delay)
					ch <- doneStruct{Status: doneAttn}
					finish()
					return len(p), nil
				}
				c := compatConn(newTimeoutConn(raw, timeout))
				defer c.sess.buf.bufClose()
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				reader := &tokenProcessor{sess: c.sess, ctx: ctx, tokChan: ch}

				start := time.Now()
				_, got := reader.nextToken()
				if timeout == 2*time.Second {
					assert.True(t, got == original, "the transport error must not be replaced or wrapped")
					assert.Equal(t, timeout, time.Since(start))
				} else {
					assert.True(t, got == context.Canceled, "successful cancellation must keep its original context error")
					assert.Equal(t, 10*time.Second, time.Since(start), "do not impose a new five-second ATTENTION deadline")
				}
				<-writeDone
				finish()
				synctest.Wait()
				for range 2 {
					tok, err := reader.nextToken()
					assert.Nil(t, tok)
					assert.NoError(t, err)
				}
				assert.Zero(t, raw.closes.Load(), "the driver must not add an automatic transport close")
				assert.EqualValues(t, 1, raw.writes.Load())
			})
		})
	}
}

func TestRegression_AbandonedResponseProducer(t *testing.T) {
	probe := compatConn(&compatTransport{Reader: bytes.NewReader(compatReply(compatDone(doneFinal), true))})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader := startReading(probe.sess, ctx, outputs{})
	capacity := cap(reader.tokChan)
	for range reader.tokChan {
	}
	<-probe.sess.readDone
	probe.sess.buf.bufClose()
	const trailingTokens = 40
	require.Less(t, capacity, trailingTokens, "the fixture must exceed the production token-channel capacity")

	stream := compatDone(doneError | doneMore)
	for range trailingTokens {
		stream = append(stream, compatDone(doneMore)...)
	}
	stream = append(stream, compatDone(doneFinal)...)
	transport := &compatTransport{Reader: bytes.NewReader(compatReply(stream, true))}
	c := compatConn(transport)
	_, err := (&Stmt{c: c}).processQueryResponse(context.Background())
	require.IsType(t, Error{}, err)
	select {
	case <-c.sess.readDone:
	case <-time.After(2 * time.Second):
		t.Fatal("the abandoned response producer is still blocked on the full token channel")
	}
	// A subsequent request must complete on the same connection.
	transport.Reader = bytes.NewReader(compatReply(compatDone(doneFinal), true))
	_, err = (&Stmt{c: c, query: "SELECT 1"}).exec(context.Background(), nil)
	require.NoError(t, err)
	<-c.sess.readDone
	c.sess.buf.bufClose()
	assert.True(t, c.IsValid())
}
