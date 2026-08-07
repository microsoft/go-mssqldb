package mssql

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/binary"
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
	stream = appendDoneToken(stream, tokenDone, doneFinal)
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
