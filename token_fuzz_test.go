package mssql

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"reflect"
	"strings"
	"testing"
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

		seq++
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
// an ENVCHANGE token and is not otherwise visible on the channel. The fuzz
// target passes collect=false to avoid the per-token formatting allocations in
// the hot path. It also reports whether any error/panic token was emitted.
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
	go processSingleResponse(context.Background(), sess, ch, outputs{})

	for tok := range ch {
		if collect {
			tokens = append(tokens, normalizeToken(tok))
		}
		if _, isErr := tok.(error); isErr {
			sawError = true
		}
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

// infoLikeToken builds an ERROR/INFO token body (they share a layout). The
// Length field is set to the true byte length of the token data that follows
// it, so the seed is spec-faithful even though the current parser ignores it.
func infoLikeToken(tok token) []byte {
	body := []byte{
		0x00, 0x00, 0x00, 0x00, // Number
		0x01,       // State
		0x01,       // Class
		0x00, 0x00, // Message UsVarChar length = 0
		0x00,                   // ServerName BVarChar length = 0
		0x00,                   // ProcName BVarChar length = 0
		0x00, 0x00, 0x00, 0x00, // LineNo
	}
	out := []byte{byte(tok), 0x00, 0x00} // token id + Length placeholder
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
		// COLMETADATA -> ROW -> DONE
		concat(colMetadataInt4(), rowInt4(42), doneToken(tokenDone, doneFinal)),
		// COLMETADATA -> NBCROW(null) -> DONE
		concat(colMetadataInt4(), nbcRowNull(), doneToken(tokenDone, doneFinal)),
		// multiple result sets: DONE(doneMore) then final DONE
		concat(doneToken(tokenDone, doneMore), doneToken(tokenDone, doneFinal)),
		// ERROR -> DONE(doneError): a statement that completed with an error,
		// so the terminating DONE carries the doneError status bit per MS-TDS.
		concat(infoLikeToken(tokenError), doneToken(tokenDone, doneError)),
		// INFO -> DONE
		concat(infoLikeToken(tokenInfo), doneToken(tokenDone, doneFinal)),
		// RETURNSTATUS -> DONE
		concat(returnStatusToken(0), doneToken(tokenDone, doneFinal)),
		// DONEPROC (final)
		doneToken(tokenDoneProc, doneFinal),
		// DONEINPROC (final)
		doneToken(tokenDoneInProc, doneFinal),
		// ENVCHANGE(database) -> DONE
		concat(envChangeDatabase(), doneToken(tokenDone, doneFinal)),
		// TABNAME + COLINFO + ORDER -> DONE
		concat(
			[]byte{byte(tokenTabName), 0x00, 0x00},
			[]byte{byte(tokenColInfo), 0x00, 0x00},
			[]byte{byte(tokenOrder), 0x00, 0x00},
			doneToken(tokenDone, doneFinal),
		),
		// valid final DONE followed by trailing garbage: the parser returns on
		// the final DONE before reading the garbage, so this still parses cleanly
		// and deterministically regardless of packet boundaries.
		concat(doneToken(tokenDone, doneFinal), []byte{0xDE, 0xAD, 0xBE, 0xEF}),
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
// NOTE (issue #418 hardening): the seed corpus below runs clean under
// `go test` and is what CI relies on. An extended `go test -fuzz` run of this
// target currently surfaces a pre-existing robustness gap: several TDS token
// parsers allocate buffers sized directly from attacker-controlled length
// prefixes before validating them against the available data (for example
// parseFedAuthInfo's make([]byte, size-offset) with a uint32 size, the
// variable-length column readers in types.go, and parseColMetadata72's column
// count). Under sustained fuzzing these unbounded allocations accumulate and
// exhaust memory. Bounding them is tracked as follow-up work in the
// fuzz-coverage stack and is intentionally out of scope for this
// test-infrastructure layer.
func FuzzProcessSingleResponse(f *testing.F) {
	for _, seed := range fuzzResponseSeeds() {
		f.Add(seed, byte(0))
		f.Add(seed, byte(3))
	}
	// A couple of raw single-token seeds for extra coverage.
	f.Add([]byte{byte(tokenColMetadata)}, byte(0))
	f.Add([]byte{}, byte(0))

	f.Fuzz(func(t *testing.T, stream []byte, frag byte) {
		// Bound input size to keep framing and allocations reasonable. A TDS
		// packet length is a uint16, and the read buffer is 32 KiB, so very
		// large inputs would either fail to frame or blow the buffer.
		if len(stream) > 64*1024 {
			t.Skip()
		}
		// Interpret the fuzzed byte as a packet payload size (1..256) so the
		// engine can drive the seam to arbitrary offsets rather than a fixed
		// set of even splits.
		chunk := 1 + int(frag)
		// The invariant: this must return normally (no panic escaping the
		// parser's recover, no goroutine leak/deadlock). The returned values
		// are intentionally unused beyond confirming completion, so collect is
		// false to avoid per-token allocations in the hot fuzzing path.
		_, _, _, _ = drainSingleResponse(stream, chunk, false)
	})
}
