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
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/golang-sql/sqlexp"
	"github.com/stretchr/testify/require"
)

type deferredResetConnector struct {
	*Conn
	entered chan struct{}
	once    sync.Once
}

func (c *deferredResetConnector) Connect(context.Context) (driver.Conn, error) { return c, nil }
func (*deferredResetConnector) Driver() driver.Driver                          { return &Driver{} }
func (c *deferredResetConnector) ResetSession(ctx context.Context) error {
	c.once.Do(func() { close(c.entered) })
	return c.Conn.ResetSession(ctx)
}

func requireResetSQL(t *testing.T, packet []byte, want string) {
	t.Helper()
	require.GreaterOrEqual(t, len(packet), 12)
	require.Equal(t, byte(packSQLBatch), packet[0])
	require.NotZero(t, packet[1]&0x08, "the first request must reset the pooled session")
	offset := 8 + int(binary.LittleEndian.Uint32(packet[8:12]))
	require.LessOrEqual(t, offset, len(packet))
	text, err := ucs22str(packet[offset:])
	require.NoError(t, err)
	require.Equal(t, want, text)
}

func TestDeferredReset_AcquisitionKeepsEarlierBatch(t *testing.T) {
	for _, initSQL := range []string{"", "SET NOCOUNT ON"} {
		initName := "default"
		if initSQL != "" {
			initName = "initializer"
		}
		for _, tc := range []struct {
			name     string
			query    bool
			deadline bool
			healthy  bool
		}{
			{name: "Conn cancellation"},
			{name: "Conn deadline", deadline: true},
			{name: "Query cancellation", query: true},
			{name: "Query deadline", query: true, deadline: true},
			{name: "healthy delayed Conn", healthy: true},
		} {
			t.Run(initName+"/"+tc.name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					prefix := compatReply(compatOutput(t, "bad", []byte{typeIntN, 8, 0}), false)
					tailTokens := compatOutput(t, "later", []byte{typeInt4, 73, 0, 0, 0})
					tail := &statusOwnershipGate{
						Reader:  bytes.NewReader(compatReply(append(tailTokens, compatDone(doneFinal)...), true)),
						entered: make(chan struct{}), resume: make(chan struct{}),
					}
					release := sync.OnceFunc(func() { close(tail.resume) })
					var packets [][]byte
					transport := &compatTransport{
						Reader: io.MultiReader(bytes.NewReader(prefix), tail,
							bytes.NewReader(bytes.Repeat(compatReply(compatDone(doneFinal), true), 3))),
						onWrite: func(p []byte) (int, error) {
							packets = append(packets, bytes.Clone(p))
							return len(p), nil
						},
					}
					c := compatConn(transport)
					c.connector.SessionInitSQL = initSQL
					connector := &deferredResetConnector{Conn: c, entered: make(chan struct{})}
					db := sql.OpenDB(connector)
					db.SetMaxOpenConns(1)
					var conn *sql.Conn
					type acquired struct {
						conn *sql.Conn
						err  error
					}
					var outstanding chan acquired
					defer func() {
						release()
						synctest.Wait()
						select {
						case got := <-outstanding:
							if got.conn != nil {
								got.conn.Close()
							}
						default:
						}
						if conn != nil {
							conn.Close()
						}
						db.Close()
						synctest.Wait()
					}()
					var bad, later int64
					rows, err := db.QueryContext(context.Background(), "batch",
						sql.Named("bad", sql.Out{Dest: &bad}), sql.Named("later", sql.Out{Dest: &later}))
					require.Nil(t, rows)
					require.ErrorContains(t, err, "converting driver.Value type <nil>")
					synctest.Wait()
					select {
					case <-tail.entered:
					default:
						t.Fatal("the earlier response did not reach its controlled tail")
					}
					pending := c.sess.cleanup
					require.NotNil(t, pending)
					select {
					case <-pending.done:
						t.Fatal("fixture completed the earlier response before acquisition")
					default:
					}
					require.Equal(t, 1, db.Stats().Idle)
					attempts := 2
					if tc.healthy {
						attempts = 1
					}
					for attempt := 0; attempt < attempts; attempt++ {
						if conn != nil {
							require.NoError(t, conn.Close())
							conn = nil
						}
						ctx, cancel := context.WithCancel(context.Background())
						if tc.deadline {
							cancel()
							ctx, cancel = context.WithTimeout(context.Background(), time.Second)
						}
						defer cancel()
						result := make(chan acquired, 1)
						outstanding = result
						go func() {
							if tc.query {
								rows, err := db.QueryContext(ctx, "SELECT 2")
								if rows != nil {
									rows.Close()
								}
								result <- acquired{err: err}
							} else {
								reserved, err := db.Conn(ctx)
								result <- acquired{conn: reserved, err: err}
							}
						}()
						synctest.Wait()
						select {
						case <-connector.entered:
						default:
							t.Fatal("database/sql did not enter ResetSession")
						}
						require.Empty(t, result, "acquisition must be inside the pending-cleanup wait")
						require.Len(t, packets, 1)
						require.NoError(t, ctx.Err())
						if tc.healthy {
							time.Sleep(10 * time.Second)
							require.Empty(t, result)
							release()
						} else if tc.deadline {
							time.Sleep(time.Second)
						} else {
							cancel()
						}
						synctest.Wait()
						require.Len(t, result, 1)
						got := <-result
						require.Zero(t, transport.closes.Load(), "acquisition must not close the independently healthy earlier batch")
						if tc.query {
							require.True(t, got.err == ctx.Err())
						} else {
							// database/sql ignores the reset's context error, even for DB.Conn.
							require.NoError(t, got.err)
							require.NotNil(t, got.conn)
							conn = got.conn
						}
						cancel()
						require.True(t, c.IsValid())
						if !tc.healthy {
							require.True(t, c.resetPending)
							require.False(t, c.resetSession)
							require.Same(t, pending, c.sess.cleanup)
							select {
							case <-pending.done:
								t.Fatal("later acquisition cancellation stopped the earlier batch")
							default:
							}
							require.Len(t, packets, 1, "no init, application SQL, or ATTENTION may overtake cleanup")
						}
					}
					if !tc.healthy {
						time.Sleep(10 * time.Second)
						require.True(t, c.IsValid())
						require.Zero(t, transport.closes.Load())
						require.Len(t, packets, 1)
					}
					release()
					<-pending.done
					synctest.Wait()
					require.EqualValues(t, 73, later, "the earlier batch must finish naturally")
					if conn == nil {
						conn, err = db.Conn(context.Background())
						require.NoError(t, err)
					}
					for range 2 {
						_, err = conn.ExecContext(context.Background(), "SELECT 42")
						require.NoError(t, err)
					}
					wantPackets := 3
					firstSQL := "SELECT 42"
					if initSQL != "" {
						wantPackets++
						firstSQL = initSQL
					}
					require.Len(t, packets, wantPackets, "initialization must run once when configured, before application SQL")
					requireResetSQL(t, packets[1], firstSQL)
					for _, packet := range packets[2:] {
						require.Zero(t, packet[1]&0x08)
					}
					require.False(t, c.resetPending)
					require.False(t, c.resetSession)
				})
			})
		}
	}
}

func TestDeferredReset_PreservesApplicationOutputs(t *testing.T) {
	for _, tc := range []struct {
		name       string
		query      bool
		prepared   bool
		initFailed bool
	}{
		{name: "Exec"},
		{name: "Query", query: true},
		{name: "prepared Exec", prepared: true},
		{name: "prepared Query", prepared: true, query: true},
		{name: "failed initialization", initFailed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				response := func(value byte, flags uint16, rowset bool) []byte {
					tokens := infoLikeToken(tokenInfo)
					if rowset {
						tokens = append(tokens, colMetadataInt4()...)
						tokens = append(tokens, rowInt4(int32(value))...)
						tokens = append(tokens, compatDone(doneMore)...)
					}
					tokens = append(tokens, compatOutput(t, "answer", []byte{typeInt4, value, 0, 0, 0})...)
					tokens = append(tokens, byte(tokenReturnStatus), value, 0, 0, 0)
					return compatReply(append(tokens, compatDone(flags)...), true)
				}
				flags := uint16(doneFinal)
				if tc.initFailed {
					flags = doneError
				}
				answer := int64(7)
				status := ReturnStatus(99)
				messages := &sqlexp.ReturnMessage{}
				var beforeApplication [2]int64
				var packets [][]byte
				transport := &compatTransport{
					Reader: bytes.NewReader(append(response(91, flags, false), response(42, doneFinal, tc.query)...)),
					onWrite: func(p []byte) (int, error) {
						packets = append(packets, bytes.Clone(p))
						if packetType(p[0]) == packRPCRequest {
							beforeApplication = [2]int64{answer, int64(status)}
						}
						return len(p), nil
					},
				}
				c := compatConn(transport)
				c.connector.SessionInitSQL = "SET NOCOUNT ON"
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				require.ErrorIs(t, c.ResetSession(ctx), context.Canceled)
				db := sql.OpenDB(compatConnector{conn: c})
				defer db.Close()
				conn, err := db.Conn(context.Background())
				require.NoError(t, err)
				defer conn.Close()
				var stmt *sql.Stmt
				if tc.prepared {
					stmt, err = conn.PrepareContext(context.Background(), "application_procedure")
					require.NoError(t, err)
					defer stmt.Close()
				}
				require.Empty(t, packets, "preparing a statement must not execute deferred initialization")
				args := []interface{}{sql.Named("answer", sql.Out{Dest: &answer}), &status, messages}
				if tc.query {
					var rows *sql.Rows
					if tc.prepared {
						rows, err = stmt.QueryContext(context.Background(), args...)
					} else {
						rows, err = conn.QueryContext(context.Background(), "application_procedure", args...)
					}
					require.NoError(t, err)
					for {
						for rows.Next() {
							var value int64
							require.NoError(t, rows.Scan(&value))
							require.EqualValues(t, 42, value)
						}
						if rows.Err() != nil || !rows.NextResultSet() {
							break
						}
					}
					err = rows.Err()
					require.NoError(t, rows.Close())
				} else if tc.prepared {
					_, err = stmt.ExecContext(context.Background(), args...)
				} else {
					_, err = conn.ExecContext(context.Background(), "application_procedure", args...)
				}
				synctest.Wait()
				requireResetSQL(t, packets[0], c.connector.SessionInitSQL)
				if tc.initFailed {
					require.ErrorIs(t, err, driver.ErrBadConn)
					require.Len(t, packets, 1, "initialization failure must not submit the application command")
					require.EqualValues(t, 7, answer)
					require.Zero(t, status)
					require.False(t, c.IsValid())
					_, err = conn.ExecContext(context.Background(), "application_procedure")
					require.ErrorIs(t, err, sql.ErrConnDone)
					require.EqualValues(t, 1, transport.closes.Load())
				} else {
					require.NoError(t, err)
					require.Len(t, packets, 2)
					require.Equal(t, [2]int64{7, 0}, beforeApplication, "initialization must not assign application storage")
					require.EqualValues(t, 42, answer)
					require.EqualValues(t, 42, status)
					require.Zero(t, packets[1][1]&0x08)
					require.True(t, c.IsValid())
				}
				msgCtx, cancelMessages := context.WithCancel(context.Background())
				notices := make(chan int, 1)
				go func() {
					count := 0
					for {
						message := messages.Message(msgCtx)
						if msgCtx.Err() != nil {
							break
						}
						if _, ok := message.(sqlexp.MsgNotice); ok {
							count++
						}
					}
					notices <- count
				}()
				synctest.Wait()
				cancelMessages()
				wantNotices := 1
				if tc.initFailed {
					wantNotices = 0
				}
				require.Equal(t, wantNotices, <-notices, "only the application response may reach its message queue")
				require.Nil(t, c.outs.params)
				require.Nil(t, c.outs.returnStatus)
				require.Nil(t, c.outs.msgq)
			})
		})
	}
}

func TestDeferredReset_PreInitializationCancellation(t *testing.T) {
	transport := &compatTransport{Reader: bytes.NewReader(bytes.Repeat(compatReply(compatDone(doneFinal), true), 2))}
	c := compatConn(transport)
	defer c.Close()
	c.connector.SessionInitSQL = "SET NOCOUNT ON"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for range 2 {
		require.ErrorIs(t, c.ResetSession(ctx), context.Canceled)
		require.True(t, c.resetPending)
		require.True(t, c.IsValid())
		require.False(t, c.resetSession)
		require.Zero(t, transport.writes.Load())
	}
	_, err := (&Bulk{cn: c, ctx: ctx}).Done()
	require.NoError(t, err)
	require.True(t, c.resetPending, "empty bulk completion must remain a local no-op")
	require.Zero(t, transport.writes.Load())
	for range 2 {
		require.NoError(t, c.awaitResponse(context.Background()))
		require.EqualValues(t, 1, transport.writes.Load())
	}
	require.False(t, c.resetPending)
}

func TestDeferredReset_FailureRejectsEveryRequest(t *testing.T) {
	for _, cleanupFailed := range []bool{false, true} {
		failure := "initialization"
		if cleanupFailed {
			failure = "cleanup"
		}
		for _, action := range responseCleanupActions {
			t.Run(failure+"/"+action.name, func(t *testing.T) {
				transport := &compatTransport{Reader: bytes.NewReader(compatReply(compatDone(doneError), true))}
				c := compatConn(transport)
				defer c.Close()
				c.connector.SessionInitSQL = "SET NOCOUNT ON"
				c.resetPending = true
				if cleanupFailed {
					c.sess.cleanup = &responseCleanup{done: make(chan struct{}), err: errors.New("cleanup failed")}
					close(c.sess.cleanup.done)
				}
				wantWrites := int32(1)
				if cleanupFailed {
					wantWrites = 0
				}
				for range 2 {
					require.ErrorIs(t, action.run(c), driver.ErrBadConn)
					require.False(t, c.IsValid())
					require.Equal(t, wantWrites, transport.writes.Load(), "neither application SQL nor a second initializer may be sent")
				}
			})
		}
	}

}

func TestDeferredReset_BulkStartsAfterInitialization(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		metadata := colMetadataInt4()
		require.Zero(t, metadata[len(metadata)-1], "the shared metadata fixture must have an empty column name")
		metadata[len(metadata)-1] = 2
		metadata = append(metadata, str2ucs2("id")...)
		done := compatDone(doneCount)
		binary.LittleEndian.PutUint64(done[5:], 2)
		responses := [][]byte{
			compatReply(compatDone(doneFinal), true), // init
			compatReply(compatDone(doneFinal), true), // FMTONLY ON
			compatReply(append(metadata, compatDone(doneFinal)...), true),
			compatReply(compatDone(doneFinal), true), // INSERT BULK
			compatReply(done, true),
		}
		client, server := net.Pipe()
		defer server.Close()
		var packets [][]byte
		peer := make(chan error, 1)
		go func() {
			for i := 0; i < len(responses); {
				var header [8]byte
				if _, err := io.ReadFull(server, header[:]); err != nil {
					peer <- err
					return
				}
				length := int(binary.BigEndian.Uint16(header[2:4]))
				if length < len(header) {
					peer <- errors.New("invalid request packet length")
					return
				}
				packet := make([]byte, length)
				copy(packet, header[:])
				if _, err := io.ReadFull(server, packet[8:]); err != nil {
					peer <- err
					return
				}
				reply := responses[i]
				if packetType(packet[0]) == packAttention {
					// Metadata Rows.Close may cancel before the final DONE
					// is delivered. Acknowledge it without consuming the next reply.
					if i != 3 {
						peer <- errors.New("unexpected ATTENTION outside metadata close")
						return
					}
					reply = compatReply(compatDone(doneAttn), true)
				} else {
					packets = append(packets, packet)
					i++
				}
				if _, err := server.Write(reply); err != nil {
					peer <- err
					return
				}
			}
			peer <- nil
		}()
		c := compatConn(client)
		defer c.Close()
		c.sess.loginAck.TDSVersion = verTDS74
		c.connector.SessionInitSQL = "SET NOCOUNT ON"
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		require.ErrorIs(t, c.ResetSession(ctx), context.Canceled)
		bulk := c.CreateBulk("target", []string{"id"})
		for _, value := range []int64{1, 2} {
			require.NoError(t, bulk.AddRow([]interface{}{value}))
			synctest.Wait()
			require.Len(t, packets, 4, "later bulk rows must not run initialization again")
		}
		count, err := bulk.Done()
		require.NoError(t, err)
		require.NoError(t, <-peer)
		require.EqualValues(t, 2, count)
		require.Len(t, packets, 5)
		requireResetSQL(t, packets[0], c.connector.SessionInitSQL)
		for _, packet := range packets[1:] {
			require.Zero(t, packet[1]&0x08)
		}
		require.Equal(t, byte(packBulkLoadBCP), packets[4][0])
		require.True(t, bytes.Contains(packets[4][8:], append(rowInt4(1), rowInt4(2)...)), "both encoded rows must survive initialization")
		require.False(t, c.resetPending)
	})
}
