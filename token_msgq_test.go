package mssql

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"
	"time"

	"github.com/golang-sql/sqlexp"
)

// msgqResponse runs processSingleResponse in message queue mode on the given
// server bytes and returns a function that reads the next message. The read
// uses context.Background(), as an application without a query timeout does,
// so a missing message fails the test instead of being replaced by the
// MsgNextResultSet that Message returns when its context ends.
func msgqResponse(t *testing.T, server []byte) (next func() sqlexp.RawMessage, ch chan tokenStruct) {
	t.Helper()
	rm := &sqlexp.ReturnMessage{}
	sqlexp.ReturnMessageInit(rm)
	sess := &tdsSession{
		buf: newTdsBuffer(defaultPacketSize, closableBuffer{bytes.NewBuffer(server)}),
	}
	ch = make(chan tokenStruct, 5)
	go processSingleResponse(context.Background(), sess, ch, outputs{msgq: rm})
	received := 0
	next = func() sqlexp.RawMessage {
		t.Helper()
		got := make(chan sqlexp.RawMessage, 1)
		go func() { got <- rm.Message(context.Background()) }()
		select {
		case m := <-got:
			received++
			return m
		case <-time.After(5 * time.Second):
			t.Fatalf("no message after 5 s (%d received before); an application reading messages would wait forever", received)
			return nil
		}
	}
	return next, ch
}

func checkMsgqErrorThenNextResultSet(t *testing.T, next func() sqlexp.RawMessage, ch chan tokenStruct) {
	t.Helper()
	m := next()
	msgErr, ok := m.(sqlexp.MsgError)
	if !ok {
		t.Fatalf("first message: got %T, want sqlexp.MsgError", m)
	}
	if msgErr.Error == nil {
		t.Fatal("MsgError has a nil Error")
	}
	if m := next(); m != (sqlexp.MsgNextResultSet{}) {
		t.Fatalf("second message: got %T, want sqlexp.MsgNextResultSet", m)
	}
	// The error must still reach the token channel, where Rowsq.NextResultSet reads it.
	tok, more := <-ch
	if !more {
		t.Fatal("token channel closed without the error")
	}
	if _, isErr := tok.(error); !isErr {
		t.Fatalf("token: got %T, want an error", tok)
	}
}

// A connection that the server has already closed, for example after KILL or
// after a severity 20 error in the previous batch, fails in BeginRead.
func TestProcessSingleResponseMsgqConnectionClosed(t *testing.T) {
	next, ch := msgqResponse(t, nil)
	checkMsgqErrorThenNextResultSet(t, next, ch)
}

// A malformed stream panics inside processSingleResponse and is recovered there.
func TestProcessSingleResponseMsgqBadStream(t *testing.T) {
	tokenStream := []byte{0xFF} // not a valid token
	packet := make([]byte, 8+len(tokenStream))
	packet[0] = byte(packReply)
	packet[1] = 0x01 // final packet
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)))
	packet[6] = 0x01
	copy(packet[8:], tokenStream)
	next, ch := msgqResponse(t, packet)
	checkMsgqErrorThenNextResultSet(t, next, ch)
}
