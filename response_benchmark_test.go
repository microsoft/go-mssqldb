package mssql

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/binary"
	"fmt"
	"testing"
)

type responseBenchmarkTransport struct {
	bytes.Reader
	responses [3][]byte
	writes    int
}

func (c *responseBenchmarkTransport) Write(p []byte) (int, error) {
	phase := c.writes % len(c.responses)
	want := packTransMgrReq
	if phase == 1 {
		want = packRPCRequest
	}
	if len(p) == 0 || packetType(p[0]) != want {
		return 0, fmt.Errorf("benchmark request %d: expected packet type %d", phase, want)
	}
	c.Reader.Reset(c.responses[phase])
	c.writes++
	return len(p), nil
}

func (*responseBenchmarkTransport) Close() error { return nil }

type responseBenchmarkConnector struct{ conn *Conn }

func (c responseBenchmarkConnector) Connect(context.Context) (driver.Conn, error) {
	return c.conn, nil
}

func (responseBenchmarkConnector) Driver() driver.Driver { return &Driver{} }

func responseBenchmarkReply(change []byte) []byte {
	payload := make([]byte, 0, 3+len(change)+13)
	if len(change) != 0 {
		payload = append(payload, byte(tokenEnvChange))
		payload = binary.LittleEndian.AppendUint16(payload, uint16(len(change)))
		payload = append(payload, change...)
	}
	payload = append(payload, byte(tokenDone))
	payload = append(payload, make([]byte, 12)...)
	reply := make([]byte, 8, 8+len(payload))
	reply[0], reply[1], reply[6] = byte(packReply), 1, 1
	binary.BigEndian.PutUint16(reply[2:4], uint16(8+len(payload)))
	return append(reply, payload...)
}

// Exercise the same database/sql BEGIN/parameterized EXEC/COMMIT sequence as
// BenchmarkRoundTrip_Transaction without network or server allocation noise.
func BenchmarkResponseTransaction(b *testing.B) {
	begin := binary.LittleEndian.AppendUint64([]byte{envTypBeginTran, 8}, 1)
	begin = append(begin, 0)
	commit := binary.LittleEndian.AppendUint64([]byte{envTypCommitTran, 0, 8}, 1)
	transport := &responseBenchmarkTransport{responses: [3][]byte{
		responseBenchmarkReply(begin),
		responseBenchmarkReply(nil),
		responseBenchmarkReply(commit),
	}}
	c := &Conn{
		sess:           &tdsSession{buf: newTdsBuffer(defaultPacketSize, transport)},
		connector:      &Connector{},
		connectionGood: true,
		transactionCtx: context.Background(),
	}
	db := sql.OpenDB(responseBenchmarkConnector{c})
	defer db.Close()
	db.SetMaxOpenConns(1)
	conn, err := db.Conn(context.Background())
	if err != nil {
		b.Fatal(err)
	}
	defer conn.Close()
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO #bench_tx VALUES (@p1, @p2)", i, "txn-test"); err != nil {
			tx.Rollback()
			b.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	if transport.writes != 3*b.N || c.sess.tranid != 0 || c.inTransaction {
		b.Fatalf("benchmark did not finish %d transactions: %d requests, transaction %d, active %v",
			b.N, transport.writes, c.sess.tranid, c.inTransaction)
	}
}
