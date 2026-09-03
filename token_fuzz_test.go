package mssql

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/golang-sql/sqlexp"
)

// fuzzPacketSize is the TDS buffer size used by the response fuzz harness.
// It must be large enough to hold the biggest single packet the framing
// helper can produce. newTdsBuffer allocates a 64 KiB backing buffer and
// splits it in half, so the read buffer is 32 KiB; keep packets within that.
const fuzzPacketSize = 1 << 15 // 32768

// fuzzMaxPacketPayload is the largest token-stream payload that fits in a
// single framed packet given fuzzPacketSize and the 8 byte TDS header.
const fuzzMaxPacketPayload = fuzzPacketSize - 8

// fuzzTransport serves a fixed byte stream as the server side of a TDS
// connection. Writes are discarded because the response parser never replies
// on the happy path (attention/cancel writes are irrelevant to parsing here).
type fuzzTransport struct {
	r *bytes.Reader
}

func (t *fuzzTransport) Read(p []byte) (int, error)  { return t.r.Read(p) }
func (t *fuzzTransport) Write(p []byte) (int, error) { return len(p), nil }
func (t *fuzzTransport) Close() error                { return nil }

// frameReplyPackets wraps an arbitrary token stream into one or more packReply
// TDS packets. chunk is the maximum payload carried by each packet, so it
// directly selects where the packet seams fall: chunk==1 puts a boundary after
// every byte, and a chunk >= len(stream) yields a single packet. A chunk <= 0
// means "one packet" (subject to the size limit). chunk is clamped to
// fuzzMaxPacketPayload so a packet never exceeds the read buffer. Every
// non-final packet clears the final status bit; only the last packet sets it.
//
// Exposing the payload size (rather than a fixed fragment count) lets callers
// exercise arbitrary seam offsets: the fuzz body derives chunk from a fuzzed
// byte, and the determinism test enumerates every boundary for the valid seeds.
//
// It returns ok=false when the stream cannot be framed within the uint16 packet
// size limit (which, given the input bound in the fuzz body, never happens but
// is guarded defensively).
func frameReplyPackets(stream []byte, chunk int) (framed []byte, ok bool) {
	const headerLen = 8

	if chunk <= 0 || chunk > len(stream) {
		chunk = len(stream)
	}
	if chunk < 1 {
		chunk = 1
	}
	if chunk > fuzzMaxPacketPayload {
		chunk = fuzzMaxPacketPayload
	}

	var out []byte
	var seq byte = 1
	off := 0
	for {
		end := off + chunk
		if end >= len(stream) {
			end = len(stream)
		}
		final := end == len(stream)

		size := headerLen + (end - off)
		if size > int(^uint16(0)) {
			return nil, false
		}

		hdr := make([]byte, headerLen)
		hdr[0] = byte(packReply)
		if final {
			hdr[1] = 0x01 // final packet
		}
		binary.BigEndian.PutUint16(hdr[2:4], uint16(size))
		hdr[6] = seq
		out = append(out, hdr...)
		out = append(out, stream[off:end]...)

		// Per MS-TDS the PacketID (hdr[6]) wraps 255 -> 1; a real server never
		// emits 0. Match that so a long stream fragmented at a small chunk does
		// not produce a packet sequence the driver could never receive.
		seq++
		if seq == 0 {
			seq = 1
		}
		off = end
		if final {
			break
		}
	}
	return out, true
}

// newFuzzSession builds a *tdsSession whose buffer reads from the given framed
// TDS packet bytes. logFlags stays zero so the (nil) logger is never invoked.
func newFuzzSession(framed []byte) *tdsSession {
	transport := &fuzzTransport{r: bytes.NewReader(framed)}
	return &tdsSession{
		buf: newTdsBuffer(fuzzPacketSize, transport),
	}
}

// msgSentinel is a private RawMessage used only to mark the end of the return
// message queue when draining it after the parser goroutine has finished.
type msgSentinel struct{}

// normalizeMsg renders the deterministic contents of a return-message queue
// entry. INFO/ERROR values and rows-affected counts are exposed by the parser
// only through outs.msgq (never on the token channel), so folding them into the
// packet-boundary comparison is what makes the INFO/ERROR seeds actually verify
// their decoded values rather than merely the surrounding DONE.
func normalizeMsg(m sqlexp.RawMessage) string {
	switch v := m.(type) {
	case sqlexp.MsgNotice:
		if e, ok := v.Message.(Error); ok {
			return "notice" + normalizeErrors([]Error{e})
		}
		return fmt.Sprintf("notice{%v}", v.Message)
	case sqlexp.MsgError:
		if e, ok := v.Error.(Error); ok {
			return "error" + normalizeErrors([]Error{e})
		}
		return fmt.Sprintf("error{%v}", v.Error)
	case sqlexp.MsgRowsAffected:
		return fmt.Sprintf("rowsaffected=%d", v.Count)
	case sqlexp.MsgNext:
		return "next"
	case sqlexp.MsgNextResultSet:
		return "nextresultset"
	default:
		return fmt.Sprintf("%T", m)
	}
}

// normalizeErrors renders the deterministic contents of the ERROR tokens
// accumulated onto a DONE token. Recording only the count would let a
// packet-boundary regression that corrupts a decoded ERROR field (Number,
// State, Class, Message, ServerName, ProcName, LineNo) slip through while the
// token count stayed the same, so the full decoded values are compared here.
func normalizeErrors(errs []Error) string {
	parts := make([]string, len(errs))
	for i, e := range errs {
		parts[i] = fmt.Sprintf("{num=%d state=%d class=%d msg=%q server=%q proc=%q line=%d}",
			e.Number, e.State, e.Class, e.Message, e.ServerName, e.ProcName, e.LineNo)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// normalizeToken renders a token's parsed *contents* (not just its Go type) as
// a stable string, so the packet-boundary invariant can assert that
// fragmentation preserves the decoded values rather than merely the dispatch
// order. Recording only "%T" would let a regression in a cross-packet value
// path (for example the uint32 read behind rowInt4) change 42 to some other
// number while both runs still produced a []interface{}. The representation
// deliberately excludes non-deterministic fields (function pointers inside
// typeInfo, *cryptoMetadata) and keeps only the decoded, comparable values.
func normalizeToken(tok tokenStruct) string {
	switch v := tok.(type) {
	case doneStruct:
		return fmt.Sprintf("done{status=%d curcmd=%d rows=%d errs=%s}",
			v.Status, v.CurCmd, v.RowCount, normalizeErrors(v.errors))
	case []columnStruct:
		parts := make([]string, len(v))
		for i, c := range v {
			parts[i] = fmt.Sprintf("{name=%q type=%d usertype=%d flags=%d}",
				c.ColName, c.ti.TypeId, c.UserType, c.Flags)
		}
		return "cols[" + strings.Join(parts, ",") + "]"
	case []interface{}:
		return fmt.Sprintf("row%v", v)
	case ReturnStatus:
		return fmt.Sprintf("returnstatus=%d", int32(v))
	case orderStruct:
		return fmt.Sprintf("order%v", v.ColIds)
	case error:
		// Value irrelevant here: valid seeds never emit an error token, and
		// the boundary test already asserts that separately.
		return fmt.Sprintf("error:%T", v)
	default:
		return fmt.Sprintf("%T:%v", tok, tok)
	}
}

// drainSingleResponse runs processSingleResponse against a framed stream and
// fully drains the token channel so the reader goroutine never blocks on the
// size-5 buffered channel. chunk is the per-packet payload size passed to
// frameReplyPackets (chunk <= 0 means a single packet). When collect is true it
// returns the ordered list of normalized token contents that were produced
// (used for the packet-boundary determinism invariant) together with the
// database name recorded on the session, which is the observable side effect of
// an ENVCHANGE token and is not otherwise visible on the channel. Collection
// runs also wire a return-message queue into outs.msgq and drain it (see below)
// to append its contents (INFO/ERROR/rows-affected), because those decoded
// values are exposed only through the queue and never on the token channel;
// including them makes the INFO and ERROR seeds actually verify their parsed
// values across packet boundaries. The fuzz target passes collect=false to
// avoid the per-token formatting allocations and the queue plumbing in the hot
// path. It also reports whether any error/panic token was emitted.
// processSingleResponse installs its own recover(), so a malformed stream
// surfaces as an error token here rather than crashing the harness.
func drainSingleResponse(stream []byte, chunk int, collect bool) (tokens []string, dbState string, sawError bool, framed bool) {
	packets, ok := frameReplyPackets(stream, chunk)
	if !ok {
		return nil, "", false, false
	}
	sess := newFuzzSession(packets)
	defer sess.buf.bufClose()

	ch := make(chan tokenStruct, 5)

	var outs outputs
	var msgq *sqlexp.ReturnMessage
	var msgs []string
	var msgDone chan struct{}
	if collect {
		msgq = &sqlexp.ReturnMessage{}
		sqlexp.ReturnMessageInit(msgq)
		outs.msgq = msgq
		// Drain the queue concurrently with the parser: ReturnMessageEnqueue
		// blocks once the 15-slot buffer fills, so a response with enough
		// notices/result-sets/row-count messages would otherwise stall the
		// parser before it closes ch and hang this reusable harness. The
		// goroutine writes msgs and is joined (via msgDone) before msgs is
		// read below, so there is no data race.
		msgDone = make(chan struct{})
		go func() {
			defer close(msgDone)
			for {
				m := msgq.Message(context.Background())
				if _, stop := m.(msgSentinel); stop {
					return
				}
				msgs = append(msgs, "msg:"+normalizeMsg(m))
			}
		}()
	}

	go processSingleResponse(context.Background(), sess, ch, outs)

	for tok := range ch {
		if collect {
			tokens = append(tokens, normalizeToken(tok))
		}
		if _, isErr := tok.(error); isErr {
			sawError = true
		}
	}

	if collect {
		// ch is closed (its close is deferred in processSingleResponse and runs
		// on every exit path), so the parser has enqueued every message it will.
		// The sentinel makes the drain goroutine stop once it has consumed them.
		_ = sqlexp.ReturnMessageEnqueue(context.Background(), msgq, msgSentinel{})
		<-msgDone
		tokens = append(tokens, msgs...)
	}

	// sess.database is mutated by processEnvChg while the goroutine runs; the
	// range loop above has returned only after ch is closed, which the parser
	// does after it finishes, so this read is safe and final.
	return tokens, sess.database, sawError, true
}

// --- synthetic token-stream builders -------------------------------------

// doneBody returns a DONE/DONEPROC/DONEINPROC token body (status, curcmd,
// rowcount). The caller supplies the leading token byte.
func doneBody(status uint16) []byte {
	b := make([]byte, 1+2+2+8)
	// b[0] is the token byte, filled in by the caller via prepend.
	binary.LittleEndian.PutUint16(b[1:3], status)
	// curcmd and rowcount left as zero
	return b
}

func doneToken(tok token, status uint16) []byte {
	b := doneBody(status)
	b[0] = byte(tok)
	return b
}

// doneCountToken builds a DONE-family token that reports a row count. The
// doneCount status bit is set and CurCmd/RowCount carry distinct non-zero
// values, so the boundary test exercises those multi-byte cross-packet reads
// and (when collecting messages) the MsgRowsAffected normalization path, which
// a zero-valued DONE would leave untested.
func doneCountToken(tok token, status, curcmd uint16, rowcount uint64) []byte {
	b := doneBody(status | doneCount)
	b[0] = byte(tok)
	binary.LittleEndian.PutUint16(b[3:5], curcmd)
	binary.LittleEndian.PutUint64(b[5:13], rowcount)
	return b
}

// colMetadataInt4 returns a COLMETADATA token for a single, unnamed int4 column.
func colMetadataInt4() []byte {
	return []byte{
		byte(tokenColMetadata),
		0x01, 0x00, // count = 1
		0x00, 0x00, 0x00, 0x00, // UserType
		0x00, 0x00, // Flags
		typeInt4, // TypeId (fixed length, no extra type info)
		0x00,     // ColName BVarChar length = 0
	}
}

// rowInt4 returns a ROW token carrying one int4 value.
func rowInt4(v int32) []byte {
	b := []byte{byte(tokenRow), 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(b[1:5], uint32(v))
	return b
}

// nbcRowNull returns an NBCROW token whose single column is NULL (bit set),
// so no column data follows.
func nbcRowNull() []byte {
	return []byte{byte(tokenNbcRow), 0x01}
}

// ucs2 encodes an ASCII string as little-endian UCS-2, the on-the-wire form of
// TDS (B|Us)VarChar payloads used by the ERROR/INFO seeds.
func ucs2(s string) []byte {
	b := make([]byte, 0, len(s)*2)
	for i := 0; i < len(s); i++ {
		b = append(b, s[i], 0x00)
	}
	return b
}

// infoLikeToken builds an ERROR/INFO token body (they share a layout). Every
// field carries a distinct non-zero value so the packet-boundary test actually
// verifies each decoded field across seams: a fragmented-read regression that
// dropped any one of them to its zero value would change the normalized output.
// The Length field is set to the true byte length of the token data that
// follows it, so the seed is spec-faithful even though the current parser
// ignores it.
func infoLikeToken(tok token) []byte {
	var body []byte
	num := make([]byte, 4)
	binary.LittleEndian.PutUint32(num, 0x11223344)
	body = append(body, num...) // Number
	body = append(body, 0x07)   // State
	body = append(body, 0x0E)   // Class
	// Message: UsVarChar (uint16 char count + UCS2).
	msg := "hi"
	ml := make([]byte, 2)
	binary.LittleEndian.PutUint16(ml, uint16(len(msg)))
	body = append(body, ml...)
	body = append(body, ucs2(msg)...)
	// ServerName: BVarChar (byte char count + UCS2).
	body = append(body, 0x01)
	body = append(body, ucs2("s")...)
	// ProcName: BVarChar (byte char count + UCS2).
	body = append(body, 0x01)
	body = append(body, ucs2("p")...)
	// LineNo.
	ln := make([]byte, 4)
	binary.LittleEndian.PutUint32(ln, 0x0000007B)
	body = append(body, ln...)

	out := []byte{byte(tok), 0x00, 0x00} // token id + Length placeholder
	binary.LittleEndian.PutUint16(out[1:3], uint16(len(body)))
	return append(out, body...)
}

// tabNameToken builds a spec-faithful TDS 7.2 TABNAME token for a single-part
// table name. Per MS-TDS the token value is NumParts (BYTE) followed by that
// many US_VARCHAR name parts (USHORT char count + UCS-2 chars). The Length
// field is the true byte length of the value, so the seed stays valid even if
// parseTabName is later strengthened to actually decode the parts.
func tabNameToken(name string) []byte {
	part := make([]byte, 2)
	binary.LittleEndian.PutUint16(part, uint16(len(name)))
	part = append(part, ucs2(name)...)
	body := append([]byte{0x01}, part...) // NumParts = 1
	out := []byte{byte(tokenTabName), 0x00, 0x00}
	binary.LittleEndian.PutUint16(out[1:3], uint16(len(body)))
	return append(out, body...)
}

// envChangeDatabase builds an ENVCHANGE token announcing a database change.
func envChangeDatabase() []byte {
	// payload: type(1) + new BVarChar("x") + old BVarChar("")
	payload := []byte{
		envTypDatabase,
		0x01, 'x', 0x00, // new value: len 1, UCS2 'x'
		0x00, // old value: len 0
	}
	b := []byte{byte(tokenEnvChange), 0x00, 0x00}
	binary.LittleEndian.PutUint16(b[1:3], uint16(len(payload)))
	return append(b, payload...)
}

func returnStatusToken(v int32) []byte {
	b := []byte{byte(tokenReturnStatus), 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(b[1:5], uint32(v))
	return b
}

func concat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// validResponseSeeds returns complete, well-formed synthetic response streams.
// Each parses to a deterministic token sequence, so these double as the
// packet-boundary determinism corpus (see TestProcessSingleResponsePacketBoundary).
func validResponseSeeds() [][]byte {
	return [][]byte{
		// empty result set + DONE
		doneToken(tokenDone, doneFinal),
		// COLMETADATA -> ROW -> DONE. The terminating DONE sets doneCount and
		// carries non-zero CurCmd/RowCount so the content comparison exercises
		// those reads and the MsgRowsAffected path, not just status.
		concat(colMetadataInt4(), rowInt4(42), doneCountToken(tokenDone, doneFinal, 0x00C1, 7)),
		// COLMETADATA -> NBCROW(null) -> DONE
		concat(colMetadataInt4(), nbcRowNull(), doneToken(tokenDone, doneFinal)),
		// multiple result sets: DONE(doneMore) then final DONE
		concat(doneToken(tokenDone, doneMore), doneToken(tokenDone, doneFinal)),
		// ERROR -> DONE(doneError): a statement that completed with an error,
		// so the terminating DONE carries the doneError status bit per MS-TDS.
		concat(infoLikeToken(tokenError), doneToken(tokenDone, doneError)),
		// INFO -> DONE
		concat(infoLikeToken(tokenInfo), doneToken(tokenDone, doneFinal)),
		// RETURNSTATUS -> DONE. Non-zero multi-byte status so the seed validates
		// four-byte decoding across packet seams, not just token dispatch.
		concat(returnStatusToken(0x12345678), doneToken(tokenDone, doneFinal)),
		// DONEPROC (final)
		doneToken(tokenDoneProc, doneFinal),
		// DONEINPROC (final)
		doneToken(tokenDoneInProc, doneFinal),
		// ENVCHANGE(database) -> DONE
		concat(envChangeDatabase(), doneToken(tokenDone, doneFinal)),
		// COLMETADATA + TABNAME + COLINFO + ORDER -> DONE. The metadata defines
		// column 1 that COLINFO/ORDER reference, so this is an actual result-set
		// token sequence; the non-empty browse-token bodies also make the
		// boundary test exercise parseTabName/parseColInfo payload reads and
		// parseOrder's element loop across packet seams (an ORDER with one
		// column id), not just token dispatch.
		concat(
			colMetadataInt4(),
			tabNameToken("t"), // TDS 7.2 TABNAME: NumParts=1, one US_VARCHAR "t"
			[]byte{byte(tokenColInfo), 0x03, 0x00, 0x01, 0x01, 0x00}, // length=3, ColNum=1, TableNum=1, Status=0
			[]byte{byte(tokenOrder), 0x02, 0x00, 0x01, 0x00},         // length=2 bytes, one ColId=1
			doneToken(tokenDone, doneFinal),
		),
	}
}

// malformedResponseSeeds returns deliberately broken streams. Each is expected
// to be converted into an error token by processSingleResponse's recover().
// These are NOT part of the packet-boundary determinism corpus because where the
// parser trips can legitimately depend on how the bytes are split into packets.
func malformedResponseSeeds() [][]byte {
	return [][]byte{
		// unknown token id -> handled via recover into an error token
		{0x00},
		// truncated DONE token (missing bytes) -> recovered error token
		{byte(tokenDone), 0x00},
		// valid non-final DONE (doneMore) followed by trailing garbage: the
		// doneMore status keeps the parser looping past the DONE (a final DONE
		// would return first, leaving the garbage unread), so the trailing byte
		// is read as an unknown token id and recovered into an error token.
		// 0xAF is not a defined token id (unlike 0xFF, which is tokenDoneInProc),
		// so it genuinely exercises the unsupported-token path after a non-final
		// DONE rather than a second DONE-family token.
		concat(doneToken(tokenDone, doneMore), []byte{0xAF}),
	}
}

// fuzzResponseSeeds returns every synthetic seed (valid and malformed) for use
// as the fuzz corpus.
func fuzzResponseSeeds() [][]byte {
	return append(validResponseSeeds(), malformedResponseSeeds()...)
}

// TestProcessSingleResponseMalformedSeeds asserts that each deliberately broken
// seed is actually rejected: the parser must surface an error token (a
// StreamError produced by its internal recover()) rather than silently
// accepting the unknown or truncated token. Without this, a regression that
// stopped detecting bad streams would go unnoticed, since the fuzz target only
// checks for panics and the boundary test excludes these seeds.
func TestProcessSingleResponseMalformedSeeds(t *testing.T) {
	for i, seed := range malformedResponseSeeds() {
		seed := seed
		t.Run(fmt.Sprintf("seed_%d", i), func(t *testing.T) {
			_, _, sawErr, ok := drainSingleResponse(seed, 0, false)
			if !ok {
				t.Fatal("failed to frame malformed seed as a single packet")
			}
			if !sawErr {
				t.Fatalf("malformed seed %d did not produce an error token", i)
			}
		})
	}
}

// TestProcessSingleResponsePacketBoundary asserts that, for known-valid seeds,
// the parser output is independent of how the stream is fragmented across TDS
// packets. It compares the normalized token *contents* (so a cross-packet value
// regression is caught, not just a change in dispatch order) as well as the
// database name recorded on the session (the observable ENVCHANGE side effect).
// It also asserts that no parse fails, so a seed that errors identically for
// every fragmentation cannot pass the determinism comparison unnoticed.
func TestProcessSingleResponsePacketBoundary(t *testing.T) {
	for i, seed := range validResponseSeeds() {
		seed := seed
		t.Run(fmt.Sprintf("seed_%d", i), func(t *testing.T) {
			single, singleDB, singleErr, ok := drainSingleResponse(seed, 0, true) // one packet
			if !ok {
				t.Fatal("failed to frame seed as a single packet")
			}
			if singleErr {
				t.Fatalf("valid seed %d produced an error token as a single packet", i)
			}
			for chunk := 1; chunk < len(seed); chunk++ {
				fragmented, fragDB, fragErr, ok := drainSingleResponse(seed, chunk, true)
				if !ok {
					t.Fatalf("failed to frame seed with chunk=%d", chunk)
				}
				if fragErr {
					t.Fatalf("valid seed %d produced an error token with chunk=%d", i, chunk)
				}
				if !reflect.DeepEqual(single, fragmented) {
					t.Fatalf("token contents differ by packet boundary (chunk=%d):\n single=%v\n frag  =%v",
						chunk, single, fragmented)
				}
				if singleDB != fragDB {
					t.Fatalf("session database differs by packet boundary (chunk=%d): single=%q frag=%q",
						chunk, singleDB, fragDB)
				}
			}
		})
	}
}

// FuzzProcessSingleResponse feeds arbitrary token streams to the core TDS
// response parser (processSingleResponse) after framing them into one or more
// packReply packets. The parser is expected to either parse the stream or
// convert a malformed stream into an error token via its internal recover();
// the invariant this fuzz target enforces is that it must NEVER panic out of
// the harness regardless of input, and the reader goroutine must always
// terminate (the drain helper guarantees the channel is emptied).
//
// NOTE (issue #420 hardening): several TDS token parsers previously allocated
// buffers sized directly from attacker-controlled length prefixes before
// validating them against the available data (for example parseFedAuthInfo's
// make([]byte, size-offset) with a uint32 size, the variable-length column
// readers in types.go, and parseColMetadata72's column count). Under sustained
// fuzzing these unbounded allocations exhausted memory. Those sizes/counts are
// now bounded and underflows rejected via badStreamPanic, so a malformed stream
// fails cleanly as an error token instead of OOMing; the crafted repros are
// pinned as seeds above.
func FuzzProcessSingleResponse(f *testing.F) {
	for _, seed := range fuzzResponseSeeds() {
		f.Add(seed, uint16(0))
		f.Add(seed, uint16(3))
	}
	// A couple of raw single-token seeds for extra coverage.
	f.Add([]byte{byte(tokenColMetadata)}, uint16(0))
	f.Add([]byte{}, uint16(0))

	// Regression seeds for issue #420: token streams whose length prefixes drove
	// unbounded allocations before they were bounded. They must now parse into an
	// error token without OOMing.
	// FEDAUTHINFO underflow repro from the issue (EE 00*8): size=0,count=0.
	f.Add([]byte{byte(tokenFedAuthInfo), 0, 0, 0, 0, 0, 0, 0, 0}, uint16(0))
	// FEDAUTHINFO with a bogus-huge option count.
	f.Add([]byte{byte(tokenFedAuthInfo), 8, 0, 0, 0, 0xFF, 0xFF, 0xFF, 0xFF}, uint16(0))
	// COLMETADATA with a bogus-huge column count (0xFFFE) and no column data.
	f.Add([]byte{byte(tokenColMetadata), 0xFE, 0xFF}, uint16(0))
	// FEDAUTHINFO option whose dataOffset+dataLength overflows uint32: size=13,
	// count=1, then {ID=STSURL, dataLength=0xFFFFFFF7, dataOffset=13}. Pre-fix the
	// uint32 sum wrapped past the bounds check and drove a slice-bounds panic; it
	// must now surface as an error token.
	f.Add([]byte{byte(tokenFedAuthInfo),
		13, 0, 0, 0, // size
		1, 0, 0, 0, // count
		fedAuthInfoSTSURL,
		0xF7, 0xFF, 0xFF, 0xFF, // dataLength
		13, 0, 0, 0, // dataOffset
	}, uint16(0))

	f.Fuzz(func(t *testing.T, stream []byte, frag uint16) {
		// Bound input size to keep framing and allocations reasonable. A TDS
		// packet length is a uint16, and the read buffer is 32 KiB, so very
		// large inputs would either fail to frame or blow the buffer.
		if len(stream) > 64*1024 {
			t.Skip()
		}
		// Interpret the fuzzed value as a packet payload size (1..65536) so the
		// engine can drive the seam to any offset across the full payload range
		// that frameReplyPackets supports, not just the first 256 bytes.
		// frameReplyPackets clamps chunk to fuzzMaxPacketPayload.
		chunk := 1 + int(frag)
		// The invariant: this must return normally (no panic escaping the
		// parser's recover, no goroutine leak/deadlock). The returned values
		// are intentionally unused beyond confirming completion, so collect is
		// false to avoid per-token allocations in the hot fuzzing path.
		_, _, _, _ = drainSingleResponse(stream, chunk, false)
	})
}
