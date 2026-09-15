package mssql

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/binary"
	"errors"
	"io"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/golang-sql/sqlexp"
	"github.com/stretchr/testify/require"
)

// Keep this fixture independent of helpers added after e722fbd so exactly the
// same tests can compare ReturnStatus ownership before and after PR #410.
type statusOwnershipGate struct {
	io.Reader
	entered chan struct{}
	resume  chan struct{}
	once    sync.Once
}

func (g *statusOwnershipGate) Read(p []byte) (int, error) {
	g.once.Do(func() { close(g.entered) })
	<-g.resume
	return g.Reader.Read(p)
}

type statusOwnershipTransport struct{ io.Reader }

func (*statusOwnershipTransport) Write(p []byte) (int, error) { return len(p), nil }
func (*statusOwnershipTransport) Close() error                { return nil }

type statusOwnershipConn struct {
	*Conn
	initialized chan struct{}
}

func (c *statusOwnershipConn) CheckNamedValue(value *driver.NamedValue) error {
	err := c.Conn.CheckNamedValue(value)
	if _, ok := value.Value.(*ReturnStatus); ok {
		c.initialized <- struct{}{}
	}
	return err
}

type statusOwnershipConnector struct{ conn *statusOwnershipConn }

func (c statusOwnershipConnector) Connect(context.Context) (driver.Conn, error) {
	return c.conn, nil
}

func (statusOwnershipConnector) Driver() driver.Driver { return &Driver{} }

type statusOwnershipScanner struct{ err error }

func (s *statusOwnershipScanner) Scan(interface{}) error { return s.err }
func (statusOwnershipScanner) Value() (driver.Value, error) {
	return int64(0), nil
}

func statusOwnershipReply(payload []byte, final bool) []byte {
	reply := make([]byte, 8, len(payload)+8)
	reply[0], reply[6] = byte(packReply), 1
	if final {
		reply[1] = 1
	}
	binary.BigEndian.PutUint16(reply[2:4], uint16(len(payload)+8))
	return append(reply, payload...)
}

func statusOwnershipDone(flags uint16) []byte {
	done := make([]byte, 13)
	done[0] = byte(tokenDone)
	binary.LittleEndian.PutUint16(done[1:3], flags)
	return done
}

func statusOwnershipValue(value uint32) []byte {
	return binary.LittleEndian.AppendUint32([]byte{byte(tokenReturnStatus)}, value)
}

func statusOwnershipOutput(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	b.WriteByte(byte(tokenReturnValue))
	require.NoError(t, binary.Write(&b, binary.LittleEndian, uint16(0)))
	require.NoError(t, writeBVarChar(&b, "@out"))
	b.WriteByte(0)
	require.NoError(t, binary.Write(&b, binary.LittleEndian, uint32(0)))
	require.NoError(t, binary.Write(&b, binary.LittleEndian, uint16(0)))
	b.Write([]byte{typeInt4, 0, 0, 0, 0})
	return b.Bytes()
}

func statusOwnershipRows(value uint32) []byte {
	metadata := []byte{byte(tokenColMetadata), 1, 0, 0, 0, 0, 0, 0, 0, typeInt4, 0}
	row := binary.LittleEndian.AppendUint32([]byte{byte(tokenRow)}, value)
	return append(metadata, row...)
}

func statusOwnershipDB(t *testing.T, input io.Reader) (*sql.DB, *statusOwnershipConn) {
	t.Helper()
	c := &statusOwnershipConn{
		Conn: &Conn{
			sess:           &tdsSession{buf: newTdsBuffer(defaultPacketSize, &statusOwnershipTransport{input})},
			connector:      &Connector{},
			connectionGood: true,
			transactionCtx: context.Background(),
		},
		initialized: make(chan struct{}, 4),
	}
	db := sql.OpenDB(statusOwnershipConnector{c})
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if c.sess.readDone != nil {
			<-c.sess.readDone
		}
		synctest.Wait()
		db.Close()
		synctest.Wait()
	})
	return db, c
}

func TestReturnStatusCompatibility_EarlyErrorReleasesStorage(t *testing.T) {
	for _, tc := range []struct {
		name   string
		exec   bool
		output bool
	}{
		{name: "Query statement error"},
		{name: "Query output error", output: true},
		{name: "Exec output error", exec: true, output: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				original := errors.New("application output failure")
				prefix := statusOwnershipValue(11)
				if tc.output {
					prefix = append(prefix, statusOwnershipOutput(t)...)
				} else {
					prefix = append(prefix, statusOwnershipDone(doneError|doneMore)...)
				}
				// Two trailing tokens cannot fill the five-slot producer:
				// this tests ownership independently of the original hang.
				tail := append(statusOwnershipValue(17), statusOwnershipDone(doneFinal)...)
				gate := &statusOwnershipGate{
					Reader:  bytes.NewReader(statusOwnershipReply(tail, true)),
					entered: make(chan struct{}), resume: make(chan struct{}),
				}
				db, c := statusOwnershipDB(t, io.MultiReader(
					bytes.NewReader(statusOwnershipReply(prefix, false)), gate))
				release := sync.OnceFunc(func() { close(gate.resume) })
				defer release()
				var status ReturnStatus
				args := []interface{}{&status}
				if tc.output {
					args = append(args, sql.Named("out", sql.Out{Dest: &statusOwnershipScanner{original}}))
				}
				result := make(chan error, 1)
				go func() {
					if tc.exec {
						_, err := db.Exec("theproc", args...)
						result <- err
					} else {
						rows, err := db.Query("theproc", args...)
						if rows != nil {
							rows.Close()
						}
						result <- err
					}
				}()
				synctest.Wait()
				require.Len(t, result, 1, "the error must return before the trailing status is available")
				err := <-result
				if tc.output {
					require.Same(t, original, err)
				} else {
					require.IsType(t, Error{}, err)
				}
				select {
				case <-gate.entered:
				default:
					t.Fatal("the trailing RETURNSTATUS was not held back")
				}
				select {
				case <-c.sess.readDone:
					t.Fatal("fixture completed the response before the application reused its status storage")
				default:
				}
				require.Equal(t, ReturnStatus(11), status, "status received before the error")
				status = 901 // Application-owned storage after the call returns.
				release()
				<-c.sess.readDone
				synctest.Wait()
				require.Equal(t, ReturnStatus(901), status, "background work wrote status after the error returned")
			})
		})
	}
}

func TestReturnStatusCompatibility_RowsErrorKeepsForegroundValue(t *testing.T) {
	for _, tc := range []struct {
		name     string
		output   bool
		messages bool
	}{
		{name: "statement error"},
		{name: "output error", output: true},
		{name: "message output error", output: true, messages: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				original := errors.New("application output failure")
				stream := append(statusOwnershipRows(7), statusOwnershipValue(11)...)
				if tc.output {
					stream = append(stream, statusOwnershipOutput(t)...)
				} else {
					stream = append(stream, statusOwnershipDone(doneError|doneMore)...)
				}
				stream = append(stream, statusOwnershipValue(17)...)
				stream = append(stream, statusOwnershipDone(doneFinal)...)
				db, c := statusOwnershipDB(t, bytes.NewReader(statusOwnershipReply(stream, true)))
				var status ReturnStatus
				args := []interface{}{&status}
				if tc.output {
					args = append(args, sql.Named("out", sql.Out{Dest: &statusOwnershipScanner{original}}))
				}
				var messages *sqlexp.ReturnMessage
				if tc.messages {
					messages = &sqlexp.ReturnMessage{}
					args = append(args, messages)
				}
				rows, err := db.Query("theproc", args...)
				require.NoError(t, err)
				defer rows.Close()
				if messages != nil {
					require.IsType(t, sqlexp.MsgNext{}, messages.Message(context.Background()))
				}
				require.True(t, rows.Next())
				var value int64
				require.NoError(t, rows.Scan(&value))
				require.EqualValues(t, 7, value)
				synctest.Wait()
				// All trailing tokens are buffered before automatic Close,
				// so this fixture does not depend on ATTENTION behavior.
				<-c.sess.readDone
				require.False(t, rows.Next())
				if tc.output {
					require.Same(t, original, rows.Err())
				} else {
					require.IsType(t, Error{}, rows.Err())
				}
				synctest.Wait()
				require.Equal(t, ReturnStatus(11), status, "status after the error must not overwrite the foreground value")
			})
		})
	}
}

func TestReturnStatusCompatibility_ReuseInitializesNewOperation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		prefix := statusOwnershipDone(doneError | doneMore)
		tail := append(statusOwnershipValue(17), statusOwnershipDone(doneFinal)...)
		first := &statusOwnershipGate{
			Reader:  bytes.NewReader(statusOwnershipReply(tail, true)),
			entered: make(chan struct{}), resume: make(chan struct{}),
		}
		second := &statusOwnershipGate{
			Reader:  bytes.NewReader(statusOwnershipReply(statusOwnershipDone(doneFinal), true)),
			entered: make(chan struct{}), resume: make(chan struct{}),
		}
		db, c := statusOwnershipDB(t, io.MultiReader(
			bytes.NewReader(statusOwnershipReply(prefix, false)), first, second))
		releaseFirst := sync.OnceFunc(func() { close(first.resume) })
		releaseSecond := sync.OnceFunc(func() { close(second.resume) })
		defer releaseFirst()
		defer releaseSecond()
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		defer func() {
			releaseFirst()
			releaseSecond()
			conn.Close()
		}()
		var status ReturnStatus
		rows, err := conn.QueryContext(context.Background(), "first", &status)
		require.Nil(t, rows)
		require.IsType(t, Error{}, err)
		require.Equal(t, ReturnStatus(0), status)
		<-c.initialized
		result := make(chan error, 1)
		go func() {
			_, err := conn.ExecContext(context.Background(), "second", &status)
			result <- err
		}()
		<-c.initialized
		synctest.Wait()
		require.Equal(t, ReturnStatus(0), status, "CheckNamedValue resets the next operation's status")
		require.Empty(t, result, "the second operation must still be unfinished")
		select {
		case <-first.entered:
		default:
			t.Fatal("the first response did not reach the controlled trailing status")
		}
		select {
		case <-second.entered:
			t.Fatal("the second response started before the first response was released")
		default:
		}
		releaseFirst()
		<-second.entered
		synctest.Wait()
		// The second procedure has no RETURNSTATUS, so its initialized value
		// must not be replaced by the first procedure's abandoned status.
		releaseSecond()
		require.NoError(t, <-result)
		require.Equal(t, ReturnStatus(0), status, "the prior response overwrote storage reused by the next operation")
	})
}

func TestReturnStatusCompatibility_CompleteResponseAssignsStatus(t *testing.T) {
	for _, tc := range []struct {
		name         string
		query        bool
		statementErr bool
	}{
		{name: "Exec success"},
		{name: "Exec statement error", statementErr: true},
		{name: "Query all result sets", query: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var prefix []byte
				if tc.query {
					prefix = append(statusOwnershipRows(7), statusOwnershipDone(doneMore)...)
					prefix = append(prefix, statusOwnershipRows(8)...)
					prefix = append(prefix, statusOwnershipDone(doneMore)...)
				} else if tc.statementErr {
					prefix = statusOwnershipDone(doneError | doneMore)
				} else {
					prefix = statusOwnershipDone(doneMore)
				}
				tail := append(statusOwnershipValue(17), statusOwnershipDone(doneFinal)...)
				gate := &statusOwnershipGate{
					Reader:  bytes.NewReader(statusOwnershipReply(tail, true)),
					entered: make(chan struct{}), resume: make(chan struct{}),
				}
				db, _ := statusOwnershipDB(t, io.MultiReader(
					bytes.NewReader(statusOwnershipReply(prefix, false)), gate))
				release := sync.OnceFunc(func() { close(gate.resume) })
				defer release()
				var status ReturnStatus
				var values []int64
				result := make(chan error, 1)
				go func() {
					if !tc.query {
						_, err := db.Exec("theproc", &status)
						result <- err
						return
					}
					rows, err := db.Query("theproc", &status)
					if err != nil {
						result <- err
						return
					}
					defer rows.Close()
					for {
						for rows.Next() {
							var value int64
							if err := rows.Scan(&value); err != nil {
								result <- err
								return
							}
							values = append(values, value)
						}
						if !rows.NextResultSet() {
							break
						}
					}
					result <- rows.Err()
				}()
				synctest.Wait()
				select {
				case <-gate.entered:
				default:
					t.Fatal("the consumer did not reach the trailing status")
				}
				require.Empty(t, result, "a complete-response operation must wait for RETURNSTATUS")
				release()
				err := <-result
				if tc.statementErr {
					require.IsType(t, Error{}, err)
				} else {
					require.NoError(t, err)
				}
				require.Equal(t, ReturnStatus(17), status)
				if tc.query {
					require.Equal(t, []int64{7, 8}, values)
				}
			})
		})
	}
}
