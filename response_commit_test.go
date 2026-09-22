package mssql

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/binary"
	"io"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type commitWaitContext struct {
	context.Context
	entered chan struct{}
	once    sync.Once
}

func (c *commitWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.entered) })
	return c.Context.Done()
}

func TestResponseCleanup_CommitWaitRetiresUnownedTransaction(t *testing.T) {
	for _, tc := range []struct {
		name         string
		deadline     bool
		releaseOnly  bool
		cleanupError bool
		healthy      bool
	}{
		{name: "canceled then reused"},
		{name: "deadline then reused", deadline: true},
		{name: "canceled then released", releaseOnly: true},
		{name: "deadline then released", deadline: true, releaseOnly: true},
		{name: "cleanup failed", cleanupError: true},
		{name: "healthy delayed cleanup", healthy: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				begin := binary.LittleEndian.AppendUint64([]byte{envTypBeginTran, 8}, 1)
				begin = append(begin, 0)
				prefix := append(responseBenchmarkReply(begin), compatReply(compatDone(doneError|doneMore), false)...)
				tailData := compatDone(doneFinal)
				if tc.cleanupError {
					tailData = []byte{0xff}
				}
				tail := &statusOwnershipGate{
					Reader:  bytes.NewReader(compatReply(tailData, true)),
					entered: make(chan struct{}), resume: make(chan struct{}),
				}
				release := sync.OnceFunc(func() { close(tail.resume) })
				after := compatReply(compatDone(doneFinal), true)
				if tc.healthy {
					commit := binary.LittleEndian.AppendUint64([]byte{envTypCommitTran, 0, 8}, 1)
					after = append(responseBenchmarkReply(commit), after...)
				}
				var packets [][]byte
				transport := &compatTransport{
					Reader: io.MultiReader(bytes.NewReader(prefix), tail, bytes.NewReader(after)),
					onWrite: func(packet []byte) (int, error) {
						packets = append(packets, bytes.Clone(packet))
						return len(packet), nil
					},
				}
				c := compatConn(transport)
				db := sql.OpenDB(compatConnector{conn: c})
				db.SetMaxOpenConns(1)
				conn, err := db.Conn(context.Background())
				require.NoError(t, err)
				txCtx, cancel := context.WithCancel(context.Background())
				if tc.deadline {
					cancel()
					txCtx, cancel = context.WithTimeout(context.Background(), time.Second)
				}
				defer func() {
					release()
					cancel()
					synctest.Wait()
					conn.Close()
					db.Close()
					synctest.Wait()
				}()
				tx, err := conn.BeginTx(txCtx, nil)
				require.NoError(t, err)
				rows, err := tx.Query("batch")
				require.Nil(t, rows)
				require.IsType(t, Error{}, err)
				synctest.Wait()
				select {
				case <-tail.entered:
				default:
					t.Fatal("the earlier query did not reach its controlled response tail")
				}
				pending := c.sess.cleanup
				require.NotNil(t, pending)
				require.Len(t, packets, 2)
				require.True(t, c.IsValid(), "a logical query error alone must not retire the connection")

				// Observe the driver, not database/sql's pre-Commit context check.
				require.True(t, c.transactionCtx == txCtx)
				waitCtx := &commitWaitContext{Context: txCtx, entered: make(chan struct{})}
				c.transactionCtx = waitCtx
				result := make(chan error, 1)
				go func() { result <- tx.Commit() }()
				synctest.Wait()
				select {
				case <-waitCtx.entered:
				default:
					t.Fatal("Commit did not enter the pending-response wait")
				}
				require.NoError(t, txCtx.Err(), "cancellation must happen after driver Commit enters its wait")
				require.Empty(t, result, "Commit must still be waiting for the controlled query tail")
				require.ErrorIs(t, tx.Rollback(), sql.ErrTxDone, "database/sql has already relinquished rollback ownership")
				require.Len(t, packets, 2, "COMMIT must not overtake the earlier response")
				select {
				case <-pending.done:
					t.Fatal("fixture finished cleanup before the Commit cancellation boundary")
				default:
				}

				switch {
				case tc.healthy:
					time.Sleep(10 * time.Second)
					require.Empty(t, result)
					require.Len(t, packets, 2)
					require.True(t, c.IsValid())
					release()
				case tc.cleanupError:
					release()
				case tc.deadline:
					time.Sleep(time.Second)
				default:
					cancel()
				}
				synctest.Wait()
				require.Len(t, result, 1)
				commitErr := <-result
				if tc.healthy {
					require.NoError(t, commitErr)
					require.Len(t, packets, 3)
					require.Equal(t, byte(packTransMgrReq), packets[2][0])
					offset := 8 + int(binary.LittleEndian.Uint32(packets[2][8:12]))
					require.Equal(t, uint16(tmCommitXact), binary.LittleEndian.Uint16(packets[2][offset:]))
					require.True(t, c.IsValid())
					require.False(t, c.inTransaction)
					require.Zero(t, c.sess.tranid)
				} else {
					want := txCtx.Err()
					if tc.cleanupError {
						want = driver.ErrBadConn
					}
					require.True(t, commitErr == want, "Commit must preserve the original wait error: got %v, want %v", commitErr, want)
					assert.False(t, c.IsValid(), "a completed sql.Tx cannot retain ownership of an open server transaction")
					require.Len(t, packets, 2, "failed waiting must not send COMMIT or claim a rollback")
					require.True(t, c.inTransaction)
					require.EqualValues(t, 1, c.sess.tranid, "do not hide the still-open server transaction")
					if !tc.cleanupError {
						require.Same(t, pending, c.sess.cleanup)
						select {
						case <-pending.done:
							t.Fatal("Commit cancellation must not cancel the earlier query's context")
						default:
						}
					}
				}

				// Let the independently scoped query finish before trying
				// reuse, so only transaction ownership determines the result.
				release()
				<-pending.done
				<-c.sess.readDone
				synctest.Wait()
				if !tc.releaseOnly {
					_, err := conn.ExecContext(context.Background(), "INSERT INTO test VALUES (@p1)", 42)
					if tc.healthy {
						require.NoError(t, err)
						require.Len(t, packets, 4)
						require.EqualValues(t, dataStmHdrTransDescr, binary.LittleEndian.Uint16(packets[3][16:18]))
						require.Zero(t, binary.LittleEndian.Uint64(packets[3][18:26]))
					} else {
						if err == nil {
							last := packets[len(packets)-1]
							t.Logf("unexpected reuse: transaction descriptor=%d, reset flag=%#x",
								binary.LittleEndian.Uint64(last[18:26]), last[1]&0x08)
						}
						want := driver.ErrBadConn
						if tc.cleanupError {
							want = sql.ErrConnDone
						}
						require.ErrorIs(t, err, want)
						_, err = conn.ExecContext(context.Background(), "INSERT INTO test VALUES (@p1)", 43)
						require.ErrorIs(t, err, sql.ErrConnDone)
						require.Len(t, packets, 2, "the reserved connection must not submit more SQL")
					}
				}
				if err := conn.Close(); err != nil {
					require.ErrorIs(t, err, sql.ErrConnDone)
				}
				synctest.Wait()
				require.Zero(t, db.Stats().InUse)
				if tc.healthy {
					require.Equal(t, 1, db.Stats().Idle)
					require.Zero(t, transport.closes.Load())
				} else {
					require.Zero(t, db.Stats().Idle, "the pool must not reuse an abandoned transaction")
					require.EqualValues(t, 1, transport.closes.Load())
				}
			})
		})
	}
}
