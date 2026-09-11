package mssql

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"database/sql/driver"
	"encoding/binary"
	"errors"
	"io"
	"math/big"
	"net"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/golang-sql/sqlexp"
	"github.com/microsoft/go-mssqldb/msdsn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// appendDoneToken appends a single DONE-family token (tokenDone,
// tokenDoneProc or tokenDoneInProc) with the given status to a TDS token
// stream. The token layout is: type(1) status(2) curcmd(2) rowcount(8).
func appendDoneToken(stream []byte, tok token, status uint16) []byte {
	body := make([]byte, 1+2+2+8)
	body[0] = byte(tok)
	binary.LittleEndian.PutUint16(body[1:3], status)
	// curcmd and rowcount left as zero
	return append(stream, body...)
}

// wrapReplyPacket wraps a token stream in a single final TDS reply packet.
func wrapReplyPacket(tokenStream []byte) []byte {
	return wrapReplyPacketStatus(tokenStream, 0x01 /* final */)
}

// wrapReplyPacketStatus wraps a token stream in a TDS reply packet with the
// given status byte. Status 0x01 marks the packet as the final one; 0x00 tells
// the reader more packets follow, so it will attempt another read after
// consuming this packet.
func wrapReplyPacketStatus(tokenStream []byte, status byte) []byte {
	totalSize := 8 + len(tokenStream)
	packet := make([]byte, totalSize)
	packet[0] = byte(packReply)
	packet[1] = status
	binary.BigEndian.PutUint16(packet[2:4], uint16(totalSize))
	packet[6] = 0x01 // PacketNo
	copy(packet[8:], tokenStream)
	return packet
}

func outputParameterToken(t *testing.T, value []byte) []byte {
	t.Helper()
	var stream bytes.Buffer
	stream.WriteByte(byte(tokenReturnValue))
	require.NoError(t, binary.Write(&stream, binary.LittleEndian, uint16(0)))
	require.NoError(t, writeBVarChar(&stream, "@p"))
	stream.WriteByte(0)
	require.NoError(t, binary.Write(&stream, binary.LittleEndian, uint32(0)))
	require.NoError(t, binary.Write(&stream, binary.LittleEndian, uint16(0)))
	stream.Write(value)
	return stream.Bytes()
}

func TestProcessSingleResponse_OutputConversionErrorContinuesReader(t *testing.T) {
	stream := outputParameterToken(t, []byte{typeInt4, 42, 0, 0, 0})
	stream = appendDoneToken(stream, tokenDone, doneFinal)
	transport := &countingTransport{reader: bytes.NewReader(wrapReplyPacket(stream))}
	sess := &tdsSession{
		buf:    newTdsBuffer(defaultPacketSize, transport),
		logger: optionalLogger{},
	}
	tokChan := make(chan tokenStruct, 2)
	done := make(chan struct{})
	go func() {
		processSingleResponse(context.Background(), sess, tokChan, outputs{
			params: map[string]interface{}{"p": &struct{}{}},
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("output conversion error left the response reader blocked")
	}

	tokens := make([]tokenStruct, 0, 1)
	for tok := range tokChan {
		tokens = append(tokens, tok)
	}
	require.Len(t, tokens, 2, "the reader must continue after sending the conversion error")
	assert.Error(t, tokens[0].(error))
	assert.IsType(t, doneStruct{}, tokens[1])
}

func TestProcessQueryResponse_OutputParameterMessageLoop(t *testing.T) {
	for _, tc := range []struct {
		name     string
		value    []byte
		nullable bool
		wantErr  bool
	}{
		{name: "NULL into int64", value: []byte{typeIntN, 8, 0}, wantErr: true},
		{name: "value into int64", value: []byte{typeIntN, 8, 8, 42, 0, 0, 0, 0, 0, 0, 0}},
		{name: "NULL into nullable int64", value: []byte{typeIntN, 8, 0}, nullable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				stream := outputParameterToken(t, tc.value)
				stream = appendDoneToken(stream, tokenDoneProc, doneFinal)
				transport := &countingTransport{reader: bytes.NewReader(wrapReplyPacket(stream))}
				sess := &tdsSession{buf: newTdsBuffer(defaultPacketSize, transport)}
				retmsg := &sqlexp.ReturnMessage{}
				sqlexp.ReturnMessageInit(retmsg)
				value := int64(-1)
				nullable := sql.NullInt64{Int64: -1, Valid: true}
				var dest interface{} = &value
				if tc.nullable {
					dest = &nullable
				}
				conn := &Conn{
					sess:           sess,
					connectionGood: true,
					outs: outputs{
						params: map[string]interface{}{"p": dest},
						msgq:   retmsg,
					},
				}
				rows, err := (&Stmt{c: conn}).processQueryResponse(context.Background())
				require.NoError(t, err)
				require.IsType(t, &Rowsq{}, rows)
				defer func() {
					closeErr := rows.Close()
					if !t.Failed() {
						assert.NoError(t, closeErr)
					}
					synctest.Wait()
					sess.buf.bufClose()
				}()
				synctest.Wait()
				select {
				case <-sess.readDone:
				default:
					t.Fatal("output parameter response reader did not finish")
				}

				readMessage := func() sqlexp.RawMessage {
					result := make(chan sqlexp.RawMessage, 1)
					go func() { result <- retmsg.Message(context.Background()) }()
					synctest.Wait()
					if len(result) == 0 {
						// Release the waiter only after detecting the missing notification.
						require.NoError(t, sqlexp.ReturnMessageEnqueue(context.Background(), retmsg, msgSentinel{}))
						synctest.Wait()
						t.Fatal("response reader exited without notifying the message loop")
					}
					return <-result
				}

				require.IsType(t, sqlexp.MsgNextResultSet{}, readMessage())
				err = rows.(*Rowsq).NextResultSet()
				if tc.wantErr {
					require.Error(t, err)
					assert.ErrorContains(t, err, "converting driver.Value type <nil>")
					require.NoError(t, conn.awaitResponse(context.Background()))
					assert.True(t, conn.IsValid())
					_, err = conn.Prepare("SELECT 1")
					assert.NoError(t, err)
					assert.Equal(t, int64(-1), value)
					// The reader finished before the error was consumed, so
					// its normal final-DONE notifications are already queued.
					require.IsType(t, sqlexp.MsgNextResultSet{}, readMessage())
					require.IsType(t, sqlexp.MsgNextResultSet{}, readMessage())
				} else {
					require.NoError(t, err)
					require.IsType(t, sqlexp.MsgNextResultSet{}, readMessage())
					assert.ErrorIs(t, rows.(*Rowsq).NextResultSet(), io.EOF)
					assert.True(t, conn.IsValid())
					if tc.nullable {
						assert.Equal(t, sql.NullInt64{}, nullable)
					} else {
						assert.Equal(t, int64(42), value)
					}
				}
				require.NoError(t, sqlexp.ReturnMessageEnqueue(context.Background(), retmsg, msgSentinel{}))
				assert.IsType(t, msgSentinel{}, readMessage(), "no extra completion or error messages")
			})
		})
	}
}

func TestProcessSingleResponse_MessageCompletion(t *testing.T) {
	for _, tc := range []struct {
		name     string
		packet   []byte
		wantErr  bool
		messages []sqlexp.RawMessage
	}{
		{
			name:     "DONE",
			packet:   wrapReplyPacket(appendDoneToken(nil, tokenDone, doneFinal)),
			messages: []sqlexp.RawMessage{sqlexp.MsgNextResultSet{}, sqlexp.MsgNextResultSet{}},
		},
		{
			name:     "DONEPROC",
			packet:   wrapReplyPacket(appendDoneToken(nil, tokenDoneProc, doneFinal)),
			messages: []sqlexp.RawMessage{sqlexp.MsgNextResultSet{}, sqlexp.MsgNextResultSet{}},
		},
		{
			name:     "DONEINPROC",
			packet:   wrapReplyPacket(appendDoneToken(nil, tokenDoneInProc, doneFinal)),
			messages: []sqlexp.RawMessage{sqlexp.MsgNextResultSet{}, sqlexp.MsgNextResultSet{}},
		},
		{
			name:     "server error",
			packet:   wrapReplyPacket(appendDoneToken(nil, tokenDone, doneSrvError)),
			wantErr:  true,
			messages: []sqlexp.RawMessage{sqlexp.MsgNextResultSet{}},
		},
		{
			name:     "read error",
			wantErr:  true,
			messages: []sqlexp.RawMessage{sqlexp.MsgNextResultSet{}},
		},
		{
			name:     "parser panic",
			packet:   wrapReplyPacket([]byte{0xff}),
			wantErr:  true,
			messages: []sqlexp.RawMessage{sqlexp.MsgError{}, sqlexp.MsgNextResultSet{}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport := &countingTransport{reader: bytes.NewReader(tc.packet)}
			sess := &tdsSession{buf: newTdsBuffer(defaultPacketSize, transport)}
			defer sess.buf.bufClose()
			retmsg := &sqlexp.ReturnMessage{}
			sqlexp.ReturnMessageInit(retmsg)
			tokens := make(chan tokenStruct, 1)
			processSingleResponse(context.Background(), sess, tokens, outputs{msgq: retmsg})
			tok := <-tokens
			_, isError := tok.(error)
			assert.Equal(t, tc.wantErr, isError)
			_, more := <-tokens
			require.False(t, more, "the token channel must close after the terminal token")

			require.NoError(t, sqlexp.ReturnMessageEnqueue(context.Background(), retmsg, msgSentinel{}))
			var messages []sqlexp.RawMessage
			for {
				msg := retmsg.Message(context.Background())
				if _, ok := msg.(msgSentinel); ok {
					break
				}
				messages = append(messages, msg)
			}
			require.Len(t, messages, len(tc.messages))
			for i, want := range tc.messages {
				assert.IsType(t, want, messages[i])
			}
		})
	}
}

// TestProcessQueryResponse_ErrorTokenDoesNotLeakReader reproduces issue #407.
//
// When a query response contains an error DONE token that is followed by more
// tokens than the response token channel can buffer (as happens when a
// statement-scoped error such as a lock timeout does not abort the rest of the
// batch), processQueryResponse used to return the error without draining the
// channel. The background processSingleResponse goroutine then blocked forever
// on a full channel send, so sess.readDone was never closed and the next query
// on the same session hung in startResponseReader.
//
// The background drain must let the reader exit after the error is returned.
func TestProcessQueryResponse_ErrorTokenDoesNotLeakReader(t *testing.T) {
	// Build a token stream:
	//  1. an error DONE (doneError|doneMore) that processQueryResponse detects
	//     and returns on. doneMore keeps processSingleResponse reading.
	//  2. many DONE-IN-PROC tokens, far more than the 5-slot token channel can
	//     hold, so the reader goroutine would block on a channel send if the
	//     consumer stopped draining.
	//  3. a final DONE (doneFinal) that ends the response.
	var stream []byte
	stream = appendDoneToken(stream, tokenDone, doneError|doneMore)
	for i := 0; i < 40; i++ {
		stream = appendDoneToken(stream, tokenDoneInProc, doneMore)
	}
	stream = appendDoneToken(stream, tokenDone, doneFinal)
	packet := wrapReplyPacket(stream)

	// countingTransport records whether cleanup changed batch semantics by
	// sending an attention packet.
	transport := &countingTransport{reader: bytes.NewReader(packet)}
	sess := &tdsSession{
		buf:    newTdsBuffer(defaultPacketSize, transport),
		logger: optionalLogger{},
	}
	conn := &Conn{
		sess:           sess,
		connectionGood: true,
		connector:      &Connector{params: msdsn.Config{}},
	}
	stmt := &Stmt{c: conn}

	type result struct {
		rows driver.Rows
		err  error
	}
	resCh := make(chan result, 1)
	go func() {
		rows, err := stmt.processQueryResponse(context.Background())
		resCh <- result{rows, err}
	}()

	var res result
	select {
	case res = <-resCh:
	case <-time.After(10 * time.Second):
		t.Fatal("processQueryResponse hung (issue #407)")
	}

	assert.Error(t, res.err, "processQueryResponse should return the server error")
	assert.Nil(t, res.rows, "no rows should be returned on error")

	// The key assertion: the background reader goroutine must have exited so
	// that a subsequent query on this session would not block waiting on
	// readDone. Before the fix this channel was never closed.
	readDone := sess.readDone
	require.NotNil(t, readDone, "startResponseReader should have set readDone")
	select {
	case <-readDone:
		// reader goroutine exited cleanly
	case <-time.After(10 * time.Second):
		t.Fatal("reader goroutine leaked: readDone never closed (issue #407)")
	}
	assert.True(t, conn.connectionGood,
		"a clean drain must preserve the connection for the next transactional query")
	_, writeCalls := transport.counts()
	assert.Zero(t, writeCalls,
		"a naturally completed response must not send attention and abort the rest of the batch")
}

func TestSendAttention_HonorsTransportLifetime(t *testing.T) {
	t.Run("deadline ignoring transport", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			transport := &deadlineTrackingTransport{
				deadlineIgnoringTransport: &deadlineIgnoringTransport{closed: make(chan struct{})},
				deadlineSet:               make(chan struct{}),
			}
			buf := newTdsBuffer(defaultPacketSize, transport)
			defer buf.bufClose()
			result := make(chan error, 1)
			go func() { result <- sendAttention(buf) }()
			time.Sleep(10 * time.Second)
			synctest.Wait()
			assert.Empty(t, result, "do not override a transport without a timeout")
			select {
			case <-transport.closed:
				t.Fatal("attention write closed the transport without a caller request")
			default:
			}
			select {
			case <-transport.deadlineSet:
				t.Fatal("attention mutated the write deadline during an in-flight write")
			default:
			}
			require.NoError(t, transport.Close())
			assert.ErrorIs(t, <-result, net.ErrClosed)
		})
	})

	t.Run("connection timeout", func(t *testing.T) {
		client, server := net.Pipe()
		defer client.Close()
		defer server.Close()

		transport := newTimeoutConn(client, 20*time.Millisecond)
		buf := newTdsBuffer(defaultPacketSize, transport)
		defer buf.bufClose()
		start := time.Now()
		err := sendAttention(buf)

		require.Error(t, err)
		assert.Less(t, time.Since(start), time.Second,
			"the configured connection timeout must end the attention write")
	})

	t.Run("TLS transport", func(t *testing.T) {
		client, server := newTLSPipe(t)
		defer client.Close()
		defer server.Close()
		buf := newTdsBuffer(defaultPacketSize, client)
		defer buf.bufClose()
		require.NoError(t, client.SetWriteDeadline(time.Now().Add(20*time.Millisecond)))

		start := time.Now()
		err := sendAttention(buf)

		require.Error(t, err)
		assert.Less(t, time.Since(start), time.Second,
			"a TLS attention write must honor its configured transport deadline")
	})
}

type deadlineIgnoringTransport struct {
	closed chan struct{}
	once   sync.Once
}

func (*deadlineIgnoringTransport) Read([]byte) (int, error) {
	return 0, errors.New("unexpected read")
}

func (t *deadlineIgnoringTransport) Write([]byte) (int, error) {
	<-t.closed
	return 0, net.ErrClosed
}

func (t *deadlineIgnoringTransport) Close() error {
	t.once.Do(func() { close(t.closed) })
	return nil
}

func (*deadlineIgnoringTransport) SetWriteDeadline(time.Time) error {
	return nil
}

type deadlineTrackingTransport struct {
	*deadlineIgnoringTransport
	deadlineSet chan struct{}
	once        sync.Once
}

func (t *deadlineTrackingTransport) SetWriteDeadline(time.Time) error {
	t.once.Do(func() { close(t.deadlineSet) })
	return nil
}

func TestConnClose_DefersBufferPoolUntilReaderDone(t *testing.T) {
	transport := &deadlineIgnoringTransport{closed: make(chan struct{})}
	readDone := make(chan struct{})
	bufferReturned := make(chan struct{})
	conn := &Conn{sess: &tdsSession{
		buf: &tdsBuffer{
			transport: transport,
			bufClose:  func() { close(bufferReturned) },
		},
		readDone: readDone,
	}}

	require.NoError(t, conn.Close())
	select {
	case <-transport.closed:
	default:
		t.Fatal("Conn.Close must close the transport synchronously")
	}
	select {
	case <-bufferReturned:
		t.Fatal("Conn.Close returned the TDS buffer while the reader was active")
	default:
	}

	close(readDone)
	select {
	case <-bufferReturned:
	case <-time.After(time.Second):
		t.Fatal("TDS buffer was not returned after the reader exited")
	}
}

func newTLSPipe(t *testing.T) (*tls.Conn, *tls.Conn) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Minute),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	clientConn, serverConn := net.Pipe()
	client := tls.Client(clientConn, &tls.Config{InsecureSkipVerify: true}) // Test-only certificate.
	server := tls.Server(serverConn, &tls.Config{
		Certificates: []tls.Certificate{{
			Certificate: [][]byte{der},
			PrivateKey:  key,
		}},
	})

	serverResult := make(chan error, 1)
	go func() { serverResult <- server.Handshake() }()
	require.NoError(t, client.Handshake())
	require.NoError(t, <-serverResult)
	return client, server
}

// TestProcessQueryResponse_DrainFailureEvictsConnection covers the follow-up to
// issue #407: if draining the abandoned response itself fails (for example the
// server never confirms cancellation, or a read/attention error occurs while
// draining), the connection may still be unusable. In that case the drain error
// must be routed through checkBadConn so connectionGood is cleared and the pool
// evicts the connection, instead of the original plain server error leaving
// connectionGood == true and the pool reusing a broken connection.
//
// The crafted response starts with an error DONE (so processQueryResponse
// returns early) followed by a non-final packet whose continuation never
// arrives, so the drain hits a read error rather than a clean end-of-response.
func TestProcessQueryResponse_DrainFailureEvictsConnection(t *testing.T) {
	// Error DONE with doneMore, then a couple of DONE-IN-PROC tokens, all in a
	// packet marked "not final" (status 0x00). After the consumer returns on
	// the error DONE, the drain reads the remaining tokens and then the reader
	// tries to read the next packet, which never comes (EOF), producing an
	// error the drain reports.
	var stream []byte
	stream = appendDoneToken(stream, tokenDone, doneError|doneMore)
	stream = appendDoneToken(stream, tokenDoneInProc, doneMore)
	stream = appendDoneToken(stream, tokenDoneInProc, doneMore)
	packet := wrapReplyPacketStatus(stream, 0x00 /* not final: more packets expected */)

	transport := &countingTransport{reader: bytes.NewReader(packet)}
	sess := &tdsSession{
		buf:    newTdsBuffer(defaultPacketSize, transport),
		logger: optionalLogger{},
	}
	conn := &Conn{
		sess:           sess,
		connectionGood: true,
		connector:      &Connector{params: msdsn.Config{}},
	}
	stmt := &Stmt{c: conn}

	type result struct {
		rows driver.Rows
		err  error
	}
	resCh := make(chan result, 1)
	go func() {
		rows, err := stmt.processQueryResponse(context.Background())
		resCh <- result{rows, err}
	}()

	var res result
	select {
	case res = <-resCh:
	case <-time.After(10 * time.Second):
		t.Fatal("processQueryResponse hung (issue #407)")
	}

	assert.Error(t, res.err, "processQueryResponse should return the server error")
	assert.Nil(t, res.rows, "no rows should be returned on error")
	require.ErrorIs(t, conn.awaitResponse(context.Background()), driver.ErrBadConn)
	assert.False(t, conn.connectionGood,
		"a failed drain must mark the connection bad so the pool evicts it (issue #407)")
}

// TestDrain_ParseContextErrorEvictsConnection proves the provenance distinction
// added for issue #407: a context.Canceled/DeadlineExceeded value that reaches
// nextToken as an ordinary token-channel error (as happens when row parsing or
// an Always Encrypted key provider fails with a context error) must be reported
// by drain as a failure, not mistaken for a confirmed cancellation attention.
// Otherwise drain would return nil and the connection would be kept with unread
// TDS data still on the wire, corrupting the next query on the session.
func TestDrain_ParseContextErrorEvictsConnection(t *testing.T) {
	// Deliver a bare context.Canceled as a token-channel error (the shape
	// processSingleResponse produces when parseRow forwards a decrypt/key
	// provider error) on a context that is NOT cancelled, so the value cannot
	// have come from the confirmed-attention path.
	tokChan := make(chan tokenStruct, 1)
	tokChan <- context.Canceled
	close(tokChan)

	reader := &tokenProcessor{
		tokChan: tokChan,
		ctx:     context.Background(),
		sess:    &tdsSession{logger: optionalLogger{}},
	}

	err := reader.drain()
	require.Error(t, err,
		"a parse-produced context error must be reported as a drain failure")
	assert.ErrorIs(t, err, context.Canceled)
}

// gatedTransport serves a first response packet immediately, then blocks the
// next read until the first Write happens (which also fails, simulating a
// broken send), after which it serves a second packet. It lets a test place the
// background processSingleResponse goroutine in the exact state issue #407 cares
// about: blocked waiting for more server data at the moment drain tries and
// fails to send an attention.
type gatedTransport struct {
	mu       sync.Mutex
	p1       *bytes.Reader
	p2       *bytes.Reader
	gate     chan struct{}
	opened   bool
	writeErr error
}

func (t *gatedTransport) Read(p []byte) (int, error) {
	t.mu.Lock()
	if t.p1.Len() > 0 {
		t.mu.Unlock()
		return t.p1.Read(p)
	}
	gate := t.gate
	t.mu.Unlock()
	// First packet consumed: block until the attention Write opens the gate,
	// then stream the second packet.
	<-gate
	return t.p2.Read(p)
}

func (t *gatedTransport) Write(p []byte) (int, error) {
	t.mu.Lock()
	if !t.opened {
		t.opened = true
		close(t.gate)
	}
	we := t.writeErr
	t.mu.Unlock()
	// Report the write as failed so sendAttention returns an error, but still
	// release the reader so the producer goroutine can resume and stream the
	// remaining tokens.
	return len(p), we
}

func (*gatedTransport) Close() error { return nil }

// TestNextToken_AttentionWriteFailureDoesNotLeakReader is the regression test
// for the follow-up to issue #407 in the drain/attention path: when the caller
// cancels the reader and nextToken cannot send its attention (sendAttention
// returns an error), the background processSingleResponse goroutine may still be
// mid-response and about to stream more tokens than the channel can buffer.
// Marking the connection bad only closes the transport; it cannot unblock a
// pending channel send, so without a background consumer the producer would
// block forever on a full channel and sess.readDone would never close,
// re-introducing the original hang on the next query.
//
// The test drives that exact sequence: an error DONE arrives in a non-final
// first packet (so processQueryResponse enters the drain path), the caller
// cancels, the attention write fails, and the server streams more DONE tokens than
// the 5-slot channel can hold. The assertion is that the reader goroutine still
// exits (readDone closes), proving nextToken started a fallback drain of the
// abandoned channel.
func TestNextToken_AttentionWriteFailureDoesNotLeakReader(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Packet 1: a single error DONE (doneError|doneMore) in a non-final packet,
	// so the consumer returns on the error while the producer keeps reading.
	var s1 []byte
	s1 = appendDoneToken(s1, tokenDone, doneError|doneMore)
	packet1 := wrapReplyPacketStatus(s1, 0x00 /* not final */)

	// Packet 2: many DONE-IN-PROC tokens (more than the 5-slot channel buffer)
	// followed by a final DONE, so a producer with no consumer blocks on a full
	// channel send.
	var s2 []byte
	for i := 0; i < 20; i++ {
		s2 = appendDoneToken(s2, tokenDoneInProc, doneMore)
	}
	s2 = appendDoneToken(s2, tokenDone, doneFinal)
	packet2 := wrapReplyPacket(s2)

	transport := &gatedTransport{
		p1:       bytes.NewReader(packet1),
		p2:       bytes.NewReader(packet2),
		gate:     make(chan struct{}),
		writeErr: errors.New("simulated attention write failure"),
	}
	sess := &tdsSession{
		buf:    newTdsBuffer(defaultPacketSize, transport),
		logger: optionalLogger{},
	}
	conn := &Conn{
		sess:           sess,
		connectionGood: true,
		connector:      &Connector{params: msdsn.Config{}},
	}
	stmt := &Stmt{c: conn}

	type result struct {
		rows driver.Rows
		err  error
	}
	resCh := make(chan result, 1)
	go func() {
		rows, err := stmt.processQueryResponse(ctx)
		resCh <- result{rows, err}
	}()

	var res result
	select {
	case res = <-resCh:
	case <-time.After(10 * time.Second):
		t.Fatal("processQueryResponse hung (issue #407)")
	}

	assert.Error(t, res.err, "processQueryResponse should return the server error")
	assert.Nil(t, res.rows, "no rows should be returned on error")
	cancel()
	require.ErrorIs(t, conn.awaitResponse(context.Background()), driver.ErrBadConn)
	assert.False(t, conn.connectionGood,
		"a failed attention write must mark the connection bad so the pool evicts it")

	// The critical assertion: even though the attention write failed while the
	// producer still had a large response to stream, the reader goroutine must
	// have drained and exited so the next query would not hang on readDone.
	readDone := sess.readDone
	require.NotNil(t, readDone, "startResponseReader should have set readDone")
	select {
	case <-readDone:
		// reader goroutine exited: the fallback drain consumed the abandoned
		// channel and let the producer finish.
	case <-time.After(10 * time.Second):
		t.Fatal("reader goroutine leaked: readDone never closed after attention write failure (issue #407)")
	}
}

// TestNextToken_AttentionWriteFailureMarksConnectionBad verifies the follow-up
// to issue #407 for every nextToken caller other than processQueryResponse
// (Rows.Next/Close, Rowsq.Next/Close/NextResultSet). When a caller cancels the
// context and nextToken cannot send its attention, the transport is broken and
// the connection must not be reused. Those callers surface nextToken's error
// through responseError, which uses internal error-origin information without
// changing the original public error.
func TestNextToken_AttentionWriteFailureMarksConnectionBad(t *testing.T) {
	transport := &gatedTransport{
		p1:       bytes.NewReader(nil),
		p2:       bytes.NewReader(nil),
		gate:     make(chan struct{}),
		writeErr: errors.New("simulated attention write failure"),
	}
	sess := &tdsSession{
		buf:    newTdsBuffer(defaultPacketSize, transport),
		logger: optionalLogger{},
	}

	// A cancelled context with no tokens buffered drives nextToken straight to
	// the attention branch, where the write fails.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tokChan := make(chan tokenStruct)
	reader := &tokenProcessor{
		tokChan: tokChan,
		ctx:     ctx,
		sess:    sess,
	}

	_, err := reader.nextToken()
	require.Error(t, err, "a failed attention write must return an error")
	assert.Same(t, transport.writeErr, err)
	assert.Equal(t, responseErrorFatal, reader.errorKind)

	// Every nextToken caller routes its error through responseError. Verify that
	// path marks the connection bad for this signal.
	conn := &Conn{
		sess:           sess,
		connectionGood: true,
		connector:      &Connector{params: msdsn.Config{}},
	}
	returned := conn.responseError(reader, err)
	assert.False(t, conn.connectionGood,
		"responseError must evict the connection after a failed attention write")
	assert.Equal(t, err, returned, "responseError should return the same error unchanged")

	// Let the background drain goroutine started by nextToken exit.
	close(tokChan)
}

// TestRowsqColumns_AttentionWriteFailureMarksConnectionBad covers the
// Rowsq.Columns caller of nextToken, which has no error return and previously
// ignored every non-nil error. When the query context expires while Columns is
// waiting and the attention write fails, Columns must mark the connection bad
// and stop looping instead of spinning on nextToken (racing the fallback drain
// goroutine) and discarding the original error. See issue #407.
func TestRowsqColumns_AttentionWriteFailureMarksConnectionBad(t *testing.T) {
	transport := &gatedTransport{
		p1:       bytes.NewReader(nil),
		p2:       bytes.NewReader(nil),
		gate:     make(chan struct{}),
		writeErr: errors.New("simulated attention write failure"),
	}
	sess := &tdsSession{
		buf:    newTdsBuffer(defaultPacketSize, transport),
		logger: optionalLogger{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tokChan := make(chan tokenStruct)
	reader := &tokenProcessor{
		tokChan: tokChan,
		ctx:     ctx,
		sess:    sess,
	}
	conn := &Conn{
		sess:           sess,
		connectionGood: true,
		connector:      &Connector{params: msdsn.Config{}},
	}
	rc := &Rowsq{stmt: &Stmt{c: conn}, reader: reader}

	done := make(chan []string, 1)
	go func() { done <- rc.Columns() }()
	select {
	case cols := <-done:
		assert.Empty(t, cols,
			"Columns should return no columns after a failed attention write")
	case <-time.After(10 * time.Second):
		t.Fatal("Rowsq.Columns hung after a failed attention write (issue #407)")
	}
	assert.False(t, conn.connectionGood,
		"Rowsq.Columns must mark the connection bad after a failed attention write")

	// Let the background drain goroutine started by nextToken exit.
	close(tokChan)
}

// A context-shaped parser failure remains fatal without changing the error
// value the application receives.
func TestNextToken_TokenChannelContextErrorIsFatal(t *testing.T) {
	for _, ctxErr := range []error{context.Canceled, context.DeadlineExceeded} {
		tokChan := make(chan tokenStruct, 1)
		tokChan <- ctxErr
		reader := &tokenProcessor{
			tokChan: tokChan,
			// A non-cancelled ctx proves the value came from the channel, not
			// from the confirmed-attention path.
			ctx:  context.Background(),
			sess: &tdsSession{logger: optionalLogger{}},
		}
		tok, err := reader.nextToken()
		assert.Nil(t, tok)
		assert.Equal(t, responseErrorFatal, reader.errorKind)
		assert.True(t, err == ctxErr, "a token-channel context error must retain its identity")
	}
}

// Ordinary parser/provider failures also require eviction, independently of
// their public error type.
func TestNextToken_TokenChannelOrdinaryErrorIsFatal(t *testing.T) {
	sentinel := errors.New("always encrypted: failed to decrypt column encryption key")
	tokChan := make(chan tokenStruct, 1)
	tokChan <- sentinel
	conn := &Conn{
		connectionGood: true,
		connector:      &Connector{params: msdsn.Config{}},
	}
	reader := &tokenProcessor{
		tokChan: tokChan,
		ctx:     context.Background(),
		sess:    &tdsSession{logger: optionalLogger{}},
	}

	tok, err := reader.nextToken()
	assert.Nil(t, tok)
	assert.Equal(t, responseErrorFatal, reader.errorKind)
	assert.Same(t, sentinel, err)

	returned := conn.responseError(reader, err)
	assert.False(t, conn.connectionGood,
		"an ordinary parse error abandoned mid-stream must evict the connection")
	assert.ErrorIs(t, returned, sentinel)
}

func TestNextToken_PreservesTopLevelFatalErrors(t *testing.T) {
	inner := errors.New("boom")
	original := StreamError{InnerError: inner}
	wrapped := errors.Join(errors.New("outer"), original)
	serverErr := ServerError{sqlError: Error{Message: "fatal"}}
	netErr := &net.OpError{Op: "read", Err: inner}
	for _, original := range []error{original, wrapped, serverErr, netErr} {
		ch := make(chan tokenStruct, 1)
		ch <- original
		close(ch)
		reader := &tokenProcessor{ctx: context.Background(), tokChan: ch}
		_, got := reader.nextToken()
		assert.IsType(t, original, got)
		assert.Equal(t, original, got)
		conn := &Conn{connectionGood: true}
		assert.Equal(t, original, conn.responseError(reader, got))
		assert.False(t, conn.IsValid())
	}
}

// TestRowsClose_TokenChannelContextErrorEvictsConnection covers the caller the
// reviewer flagged: Rows.Close compares the nextToken error against
// reader.ctx.Err() and returns cleanly on a match. Before the fix, an Always
// Encrypted provider (or row parser) returning context.Canceled after Close
// cancels the context looked identical to a confirmed clean cancellation, so
// Close returned nil and left the connection reusable with unread TDS bytes on
// the wire. It must now evict the connection instead. See issue #407.
func TestRowsClose_TokenChannelContextErrorEvictsConnection(t *testing.T) {
	tokChan := make(chan tokenStruct, 1)
	tokChan <- context.Canceled
	ctx, cancel := context.WithCancel(context.Background())
	conn := &Conn{
		connectionGood: true,
		connector:      &Connector{params: msdsn.Config{}},
	}
	reader := &tokenProcessor{
		tokChan: tokChan,
		ctx:     ctx,
		sess:    &tdsSession{logger: optionalLogger{}},
	}
	rc := &Rows{stmt: &Stmt{c: conn}, reader: reader, cancel: cancel}

	err := rc.Close()
	assert.Error(t, err,
		"Close must not treat a parse-produced context error as a clean cancellation")
	assert.False(t, conn.connectionGood,
		"Close must evict the connection when a context error arrives mid-stream")
}

// TestRowsqClose_TokenChannelContextErrorEvictsConnection covers the same
// misclassification on the experimental Rowsq.Close path. See issue #407.
func TestRowsqClose_TokenChannelContextErrorEvictsConnection(t *testing.T) {
	tokChan := make(chan tokenStruct, 1)
	tokChan <- context.Canceled
	ctx, cancel := context.WithCancel(context.Background())
	conn := &Conn{
		connectionGood: true,
		connector:      &Connector{params: msdsn.Config{}},
	}
	reader := &tokenProcessor{
		tokChan: tokChan,
		ctx:     ctx,
		sess:    &tdsSession{logger: optionalLogger{}},
	}
	rc := &Rowsq{stmt: &Stmt{c: conn}, reader: reader, cancel: cancel}

	err := rc.Close()
	assert.Error(t, err,
		"Rowsq.Close must not treat a parse-produced context error as a clean cancellation")
	assert.False(t, conn.connectionGood,
		"Rowsq.Close must evict the connection when a context error arrives mid-stream")
}

// TestSimpleProcessResp_TokenChannelContextErrorEvictsConnection covers the
// message-loop caller of nextToken via iterateResponse: a context error
// forwarded as a token means the response was abandoned mid-stream, so the
// connection must be evicted rather than reused. See issue #407.
func TestSimpleProcessResp_TokenChannelContextErrorEvictsConnection(t *testing.T) {
	tokChan := make(chan tokenStruct, 1)
	tokChan <- context.Canceled
	conn := &Conn{
		sess:           &tdsSession{logger: optionalLogger{}},
		connectionGood: true,
		connector:      &Connector{params: msdsn.Config{}},
	}
	reader := &tokenProcessor{
		tokChan: tokChan,
		ctx:     context.Background(),
		sess:    conn.sess,
	}

	err := reader.iterateResponse()
	require.Error(t, err)
	// iterateResponse's callers (simpleProcessResp, processExec) route the
	// error through responseError, which uses its origin to decide on eviction.
	returned := conn.responseError(reader, err)
	assert.False(t, conn.connectionGood,
		"a parse-produced context error must evict the connection in the message loop")
	assert.Error(t, returned)
}
