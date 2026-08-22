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
	"database/sql/driver"
	"encoding/binary"
	"errors"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"

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

func TestProcessSingleResponse_OutputConversionErrorStopsReader(t *testing.T) {
	var stream bytes.Buffer
	stream.WriteByte(byte(tokenReturnValue))
	require.NoError(t, binary.Write(&stream, binary.LittleEndian, uint16(0)))
	require.NoError(t, writeBVarChar(&stream, "@p"))
	stream.WriteByte(0)
	require.NoError(t, binary.Write(&stream, binary.LittleEndian, uint32(0)))
	require.NoError(t, binary.Write(&stream, binary.LittleEndian, uint16(0)))
	stream.WriteByte(typeInt4)
	require.NoError(t, binary.Write(&stream, binary.LittleEndian, int32(42)))
	stream.Write(appendDoneToken(nil, tokenDone, doneFinal))

	transport := &countingTransport{reader: bytes.NewReader(wrapReplyPacket(stream.Bytes()))}
	sess := &tdsSession{
		buf:    newTdsBuffer(defaultPacketSize, transport),
		logger: optionalLogger{},
	}
	tokChan := make(chan tokenStruct, 1)
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
	require.Len(t, tokens, 1, "the reader must stop after sending the conversion error")
	assert.Error(t, tokens[0].(error))
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
// This test crafts such a response and verifies that after processQueryResponse
// returns the error, the reader goroutine exits (sess.readDone is closed),
// meaning the session is safe to reuse.
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
	stream = appendDoneToken(stream, tokenDone, doneAttn)
	packet := wrapReplyPacket(stream)

	// countingTransport keeps reads and writes on separate streams so that an
	// attention packet written during the drain does not corrupt reads.
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
}

func TestSendAttentionWithTimeout_BoundsWrite(t *testing.T) {
	t.Run("connection timeout", func(t *testing.T) {
		client, server := net.Pipe()
		defer client.Close()
		defer server.Close()

		transport := newTimeoutConn(client, time.Second)
		start := time.Now()
		err := sendAttentionWithTimeout(transport, 20*time.Millisecond)

		require.Error(t, err)
		assert.Less(t, time.Since(start), time.Second,
			"the attention timeout must override a longer connection timeout")
	})

	t.Run("TLS transport", func(t *testing.T) {
		client, server := newTLSPipe(t)
		defer client.Close()
		defer server.Close()

		start := time.Now()
		err := sendAttentionWithTimeout(client, 20*time.Millisecond)

		require.Error(t, err)
		assert.Less(t, time.Since(start), time.Second,
			"a stalled TLS attention write must return within its timeout")
	})
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
// for the follow-up to issue #407 in the drain/attention path: when drain
// cancels the reader and nextToken cannot send its attention (sendAttention
// returns an error), the background processSingleResponse goroutine may still be
// mid-response and about to stream more tokens than the channel can buffer.
// Marking the connection bad only closes the transport; it cannot unblock a
// pending channel send, so without a background consumer the producer would
// block forever on a full channel and sess.readDone would never close,
// re-introducing the original hang on the next query.
//
// The test drives that exact sequence: an error DONE arrives in a non-final
// first packet (so processQueryResponse enters the drain path), drain's
// attention write fails, and the server then streams far more DONE tokens than
// the 5-slot channel can hold. The assertion is that the reader goroutine still
// exits (readDone closes), proving nextToken started a fallback drain of the
// abandoned channel.
func TestNextToken_AttentionWriteFailureDoesNotLeakReader(t *testing.T) {
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
// through checkBadConn, which only evicts for recognized fatal error types.
// nextToken therefore wraps the failed-attention error in a StreamError so
// checkBadConn reliably marks the connection bad; an unwrapped "ordinary"
// transport error would leave connectionGood true and let database/sql reuse a
// connection whose transport just failed.
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
	var se StreamError
	require.ErrorAs(t, err, &se,
		"a failed attention write must surface a StreamError so callers evict the connection")

	// Every nextToken caller routes its error through checkBadConn. Verify that
	// path marks the connection bad for this signal.
	conn := &Conn{
		sess:           sess,
		connectionGood: true,
		connector:      &Connector{params: msdsn.Config{}},
	}
	returned := conn.checkBadConn(ctx, err, false)
	assert.False(t, conn.connectionGood,
		"checkBadConn must evict the connection after a failed attention write")
	assert.Equal(t, err, returned, "checkBadConn should return the same error unchanged")

	// Let the background drain goroutine started by nextToken exit.
	close(tokChan)
}

// TestRowsqColumns_AttentionWriteFailureMarksConnectionBad covers the
// Rowsq.Columns caller of nextToken, which has no error return and previously
// ignored every non-nil error. When the query context expires while Columns is
// waiting and the attention write fails, Columns must mark the connection bad
// and stop looping instead of spinning on nextToken (racing the fallback drain
// goroutine) and discarding the StreamError. See issue #407.
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

// TestNextToken_TokenChannelContextErrorIsFatal verifies the core normalization:
// a context.Canceled/DeadlineExceeded that arrives as a token-channel error
// (the shape processSingleResponse produces when a row parser or an Always
// Encrypted key provider forwards the reader context's error) is returned by
// nextToken wrapped in StreamError, not as the bare context error. This is what
// lets every caller distinguish it from the clean context error the confirmed-
// attention path returns and evict the connection. See issue #407.
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
		var se StreamError
		assert.ErrorAs(t, err, &se,
			"a token-channel context error must be wrapped in StreamError")
		assert.ErrorIs(t, err, ctxErr,
			"the wrapped error must still unwrap to the original context error")
	}
}

// TestNextToken_TokenChannelOrdinaryErrorIsFatal verifies that a token-channel
// error that is NOT context-shaped — for example an Always Encrypted decryption
// or key-provider failure returned by parseRow, or a badStreamPanicf stream
// corruption — is also promoted to StreamError so checkBadConn evicts the
// connection. processSingleResponse abandons the response and returns whenever it
// forwards such an error, leaving unread TDS bytes on the wire, so the connection
// must never be reused regardless of the error's concrete type. errors.Is still
// unwraps to the original error. See issue #407.
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
	var se StreamError
	assert.ErrorAs(t, err, &se,
		"an ordinary token-channel error must be wrapped in StreamError")
	assert.ErrorIs(t, err, sentinel,
		"the wrapped error must still unwrap to the original error")

	returned := conn.checkBadConn(context.Background(), err, false)
	assert.False(t, conn.connectionGood,
		"an ordinary parse error abandoned mid-stream must evict the connection")
	assert.ErrorIs(t, returned, sentinel)
}

// TestWrapTokenChannelError_PreservesTopLevelFatalErrors verifies errors already
// recognized by checkBadConn keep their concrete types, while an error that
// merely wraps a StreamError is promoted to a top-level fatal signal.
func TestWrapTokenChannelError_PreservesTopLevelFatalErrors(t *testing.T) {
	inner := errors.New("boom")
	original := StreamError{InnerError: inner}
	got := wrapTokenChannelError(original)
	se, ok := got.(StreamError)
	require.True(t, ok, "result must be a StreamError")
	assert.Equal(t, inner, se.InnerError,
		"an existing StreamError must not be re-wrapped")

	wrapped := errors.Join(errors.New("outer"), original)
	got = wrapTokenChannelError(wrapped)
	se, ok = got.(StreamError)
	require.True(t, ok, "a wrapped StreamError must be promoted to the top level")
	assert.Equal(t, wrapped, se.InnerError)

	serverErr := ServerError{sqlError: Error{Message: "fatal"}}
	assert.IsType(t, ServerError{}, wrapTokenChannelError(serverErr),
		"a direct ServerError must preserve its public concrete type")

	netErr := &net.OpError{Op: "read", Err: inner}
	assert.Same(t, netErr, wrapTokenChannelError(netErr),
		"a direct net.Error must preserve its public concrete type")

	assert.Nil(t, wrapTokenChannelError(nil), "nil must pass through unchanged")
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
	// error through checkBadConn; the wrapped StreamError makes that evict.
	returned := conn.checkBadConn(context.Background(), err, false)
	assert.False(t, conn.connectionGood,
		"a parse-produced context error must evict the connection in the message loop")
	assert.Error(t, returned)
}
