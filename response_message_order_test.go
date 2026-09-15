package mssql

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/golang-sql/sqlexp"
	"github.com/stretchr/testify/require"
)

type messageOrderFailure struct {
	err     error
	entered chan struct{}
	resume  <-chan struct{}
}

func (s *messageOrderFailure) result() error {
	close(s.entered)
	<-s.resume
	return s.err
}

func (s *messageOrderFailure) Scan(interface{}) error { return s.result() }
func (messageOrderFailure) Value() (driver.Value, error) {
	return int64(0), nil
}

func (s *messageOrderFailure) Read([]byte) (int, error) { panic(s.result()) }

func requireFullResponseMessageQueue(t *testing.T, ctx context.Context, messages *sqlexp.ReturnMessage) {
	t.Helper()
	// Cancel only this extra enqueue, not the SQL operation.
	probeCtx, cancelProbe := context.WithCancel(ctx)
	defer cancelProbe()
	probe := make(chan error, 1)
	go func() { probe <- sqlexp.ReturnMessageEnqueue(probeCtx, messages, msgSentinel{}) }()
	synctest.Wait()
	require.Empty(t, probe, "fixture did not fill the notification queue")
	cancelProbe()
	require.ErrorIs(t, <-probe, context.Canceled)
}

func TestResponseMessages_ErrorBeforeFullQueueNotification(t *testing.T) {
	for _, tc := range []struct {
		name    string
		exec    bool
		fatal   bool
		healthy bool
	}{
		{name: "query output error"},
		{name: "query parser error", fatal: true},
		{name: "exec output error", exec: true},
		{name: "exec parser error", exec: true, fatal: true},
		{name: "healthy query output", healthy: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var original error
				if !tc.healthy {
					original = errors.New("response failed")
				}
				resume := make(chan struct{})
				release := sync.OnceFunc(func() { close(resume) })
				failure := &messageOrderFailure{
					err: original, entered: make(chan struct{}), resume: resume,
				}
				var later int64
				const queueCapacity = 15
				notices := queueCapacity
				if tc.exec {
					// Exec consumes tokens without dequeuing the initial
					// MsgNext and MsgNextResultSet notifications.
					notices -= 2
				}
				stream := append(colMetadataInt4(), rowInt4(7)...)
				stream = append(stream, compatDone(doneMore)...)
				stream = append(stream, bytes.Repeat(infoLikeToken(tokenInfo), notices)...)
				var input io.Reader
				if tc.fatal {
					input = io.MultiReader(bytes.NewReader(compatReply(stream, false)), failure)
				} else {
					stream = append(stream, compatOutput(t, "first", []byte{typeIntN, 8, 0})...)
					stream = append(stream, compatOutput(t, "later", []byte{typeInt4, 42, 0, 0, 0})...)
					stream = append(stream, compatDone(doneFinal)...)
					data := append(compatReply(stream, true), compatReply(compatDone(doneFinal), true)...)
					input = bytes.NewReader(data)
				}
				transport := &compatTransport{Reader: input}
				c := compatConn(transport)
				db := sql.OpenDB(compatConnector{conn: c})
				db.SetMaxOpenConns(1)
				ctx, cancel := context.WithCancel(context.Background())
				messages := &sqlexp.ReturnMessage{}
				args := []interface{}{
					messages,
					sql.Named("first", sql.Out{Dest: failure}),
					sql.Named("later", sql.Out{Dest: &later}),
				}
				type outcome struct {
					more bool
					err  error
				}
				result := make(chan outcome, 1)
				started := false
				var rows *sql.Rows
				defer func() {
					release()
					cancel()
					if started {
						for range result {
						}
					}
					if rows != nil {
						rows.Close()
					}
					db.Close()
					synctest.Wait()
				}()
				if tc.exec {
					started = true
					go func() {
						defer close(result)
						_, err := db.ExecContext(ctx, "output_procedure", args...)
						result <- outcome{err: err}
					}()
				} else {
					var err error
					rows, err = db.QueryContext(ctx, "output_procedure", args...)
					require.NoError(t, err)
					require.IsType(t, sqlexp.MsgNext{}, messages.Message(ctx))
					require.True(t, rows.Next())
					var row int64
					require.NoError(t, rows.Scan(&row))
					require.EqualValues(t, 7, row)
					require.False(t, rows.Next())
					require.NoError(t, rows.Err())
					require.IsType(t, sqlexp.MsgNextResultSet{}, messages.Message(ctx))
					started = true
					go func() {
						defer close(result)
						more := rows.NextResultSet()
						result <- outcome{more: more, err: rows.Err()}
					}()
				}
				synctest.Wait()
				select {
				case <-failure.entered:
				default:
					t.Fatal("the producer did not reach the controlled failure")
				}
				require.Empty(t, result, "the response must still be waiting at the controlled failure")

				requireFullResponseMessageQueue(t, ctx, messages)

				release()
				synctest.Wait()
				require.Len(t, result, 1, "a full notification queue must not block error delivery")
				got := <-result
				if tc.healthy {
					require.True(t, got.more)
					require.NoError(t, got.err)
					for range notices {
						require.IsType(t, sqlexp.MsgNotice{}, messages.Message(ctx))
					}
					require.IsType(t, sqlexp.MsgNextResultSet{}, messages.Message(ctx))
					// This quiet case closes only after natural completion;
					// the fixture does not implement server-side ATTENTION.
					<-c.sess.readDone
					require.False(t, rows.NextResultSet())
					require.NoError(t, rows.Err())
				} else {
					require.False(t, got.more)
					require.Same(t, original, got.err)
				}
				require.NoError(t, ctx.Err(), "error delivery must not cancel the caller's operation context")
				if tc.fatal {
					<-c.sess.readDone
					require.False(t, c.IsValid())
					require.EqualValues(t, 1, transport.writes.Load())
				} else {
					_, err := db.ExecContext(ctx, "SELECT 1")
					require.NoError(t, err)
					require.EqualValues(t, 42, later)
					require.True(t, c.IsValid())
					require.EqualValues(t, 2, transport.writes.Load(), "cleanup must not add an ATTENTION write")
				}
			})
		})
	}
}

func TestResponseMessages_ErrorWakesMessageDrivenCaller(t *testing.T) {
	for _, tc := range []struct {
		name  string
		fatal bool
	}{
		{name: "output error"},
		{name: "parser error", fatal: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				original := errors.New("response failed")
				resume := make(chan struct{})
				release := sync.OnceFunc(func() { close(resume) })
				resumeTail := make(chan struct{})
				releaseTail := sync.OnceFunc(func() { close(resumeTail) })
				tail := &blockingOutput{entered: make(chan struct{}), resume: resumeTail}
				failure := &messageOrderFailure{
					err: original, entered: make(chan struct{}), resume: resume,
				}
				stream := bytes.Repeat(infoLikeToken(tokenInfo), 15)
				var input io.Reader
				if tc.fatal {
					input = io.MultiReader(bytes.NewReader(compatReply(stream, false)), failure)
				} else {
					stream = append(stream, compatOutput(t, "first", []byte{typeIntN, 8, 0})...)
					stream = append(stream, compatOutput(t, "pause", []byte{typeInt4, 0, 0, 0, 0})...)
					stream = append(stream, compatOutput(t, "later", []byte{typeInt4, 42, 0, 0, 0})...)
					stream = append(stream, compatDone(doneFinal)...)
					data := append(compatReply(stream, true), compatReply(compatDone(doneFinal), true)...)
					input = bytes.NewReader(data)
				}
				transport := &compatTransport{Reader: input}
				c := compatConn(transport)
				db := sql.OpenDB(compatConnector{conn: c})
				db.SetMaxOpenConns(1)
				ctx, cancel := context.WithCancel(context.Background())
				var rows *sql.Rows
				defer func() {
					release()
					releaseTail()
					cancel()
					if rows != nil {
						rows.Close()
					}
					db.Close()
					synctest.Wait()
				}()
				var later int64
				messages := &sqlexp.ReturnMessage{}
				var err error
				rows, err = db.QueryContext(ctx, "output_procedure", messages,
					sql.Named("first", sql.Out{Dest: failure}),
					sql.Named("pause", sql.Out{Dest: tail}),
					sql.Named("later", sql.Out{Dest: &later}),
				)
				require.NoError(t, err)
				synctest.Wait()
				select {
				case <-failure.entered:
				default:
					t.Fatal("the producer did not reach the controlled failure")
				}
				requireFullResponseMessageQueue(t, ctx, messages)
				release()
				readMessage := func() sqlexp.RawMessage {
					t.Helper()
					result := make(chan sqlexp.RawMessage, 1)
					go func() { result <- messages.Message(ctx) }()
					synctest.Wait()
					require.Len(t, result, 1, "the message-driven caller was not notified")
					return <-result
				}
				for range 15 {
					require.IsType(t, sqlexp.MsgNotice{}, readMessage())
				}
				if tc.fatal {
					message := readMessage()
					require.IsType(t, sqlexp.MsgError{}, message)
					require.Same(t, original, message.(sqlexp.MsgError).Error)
				}
				require.IsType(t, sqlexp.MsgNextResultSet{}, readMessage())
				require.False(t, rows.NextResultSet())
				require.Same(t, original, rows.Err())
				require.NoError(t, ctx.Err())
				if tc.fatal {
					<-c.sess.readDone
					require.False(t, c.IsValid())
					require.EqualValues(t, 1, transport.writes.Load())
				} else {
					synctest.Wait()
					select {
					case <-tail.entered:
					default:
						t.Fatal("the unfinished response did not reach its controlled trailing output")
					}
					releaseTail()
					_, err := db.ExecContext(ctx, "SELECT 1")
					require.NoError(t, err)
					require.EqualValues(t, 42, later)
					require.True(t, c.IsValid())
					require.EqualValues(t, 2, transport.writes.Load())
				}
			})
		})
	}
}
