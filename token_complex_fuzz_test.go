package mssql

import (
	"context"
	"encoding/binary"
	"testing"
)

// This file is Layer 3 of the TDS parser fuzz-hardening effort tracked by issue
// #418. It adds structured fuzz targets for the "complex" tokens that carry
// nested, length-prefixed sub-structures (ENVCHANGE, FEATUREEXTACK, LOGINACK,
// RETURNVALUE, ERROR/INFO) and for the Always Encrypted COLMETADATA path (CEK
// table + per-column crypto metadata).
//
// Random bytes almost never form a valid complex token, so every target is
// seeded with wire-format bytes assembled from the builders below and then the
// engine mutates those structured seeds. Unlike the end-to-end
// FuzzProcessSingleResponse (which routes through processSingleResponse's own
// recover and therefore only proves "no hang / no OOM / no escaping panic"),
// these targets drive the individual token parsers DIRECTLY and wrap the call in
// callExpectingParserPanic (from types_fuzz_test.go). That helper swallows only
// a deliberate parser rejection (StreamError from badStreamPanic, or the
// fmt.Errorf raised by badStreamPanicf) and re-panics anything that implements
// runtime.Error (index out of range, slice bounds, nil deref). Letting the
// runtime panic propagate is exactly what lets the fuzz engine record a real
// bug as a crasher.
//
// INVARIANTS (identical crash policy to Layer 2):
//   - The only acceptable failure on malformed input is a recovered parser
//     rejection (StreamError / badStreamPanicf error). Those are swallowed.
//   - A non-StreamError runtime panic, an infinite loop/hang, or a NEW-site OOM
//     (an eager make() from a wire length not already bounded by #421) is a REAL
//     BUG and is surfaced.
//   - Fuzz input is bounded below 64 KiB.

// --- small wire helpers ---------------------------------------------------

func u16le(v uint16) []byte {
	b := make([]byte, 2)
	binary.LittleEndian.PutUint16(b, v)
	return b
}

func u32le(v uint32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, v)
	return b
}

// bvarchar frames a B_VARCHAR: a 1-byte character count followed by that many
// UCS-2 (little-endian) code units.
func bvarchar(s string) []byte {
	return append([]byte{byte(len(s))}, ucs2(s)...)
}

// usvarchar frames a US_VARCHAR: a 2-byte character count followed by that many
// UCS-2 code units.
func usvarchar(s string) []byte {
	return append(u16le(uint16(len(s))), ucs2(s)...)
}

// bvarbyte frames a B_VARBYTE: a 1-byte length followed by that many raw bytes.
func bvarbyte(data []byte) []byte {
	return append([]byte{byte(len(data))}, data...)
}

// fuzzSessionReader frames payload as packReply packets, positions a
// *tdsSession's buffer at the first payload byte, and returns it. The session is
// otherwise zero-valued (alwaysEncrypted false, nil logger). Callers that need
// the Always Encrypted path flip sess.alwaysEncrypted afterwards.
func fuzzSessionReader(payload []byte, frag byte) (sess *tdsSession, closeBuf func(), ok bool) {
	framed, framedOK := frameReplyPackets(payload, frag)
	if !framedOK {
		return nil, nil, false
	}
	sess = newFuzzSession(framed)
	if _, err := sess.buf.BeginRead(); err != nil {
		sess.buf.bufClose()
		return nil, nil, false
	}
	return sess, sess.buf.bufClose, true
}

// ==========================================================================
// FuzzEnvChange
// ==========================================================================

// envChgRecords wraps the concatenated ENVCHANGE records with the leading uint16
// size field that processEnvChg reads first. The size equals the record length
// for a well-formed token; malformed seeds deliberately lie about it.
func envChgRecords(records []byte) []byte {
	return append(u16le(uint16(len(records))), records...)
}

func envChgSeeds() [][]byte {
	// One record per subtype, each with valid length fields.
	database := []byte{envTypDatabase}
	database = append(database, bvarchar("newdb")...)
	database = append(database, bvarchar("old")...)

	language := []byte{envTypLanguage}
	language = append(language, bvarchar("us_english")...)
	language = append(language, bvarchar("")...)

	charset := []byte{envTypCharset}
	charset = append(charset, bvarchar("iso_1")...)
	charset = append(charset, bvarchar("")...)

	packetsize := []byte{envTypPacketSize}
	packetsize = append(packetsize, bvarchar("4096")...)
	packetsize = append(packetsize, bvarchar("4096")...)

	sortID := []byte{envSortId}
	sortID = append(sortID, bvarchar("100")...)
	sortID = append(sortID, bvarchar("")...)

	sortFlags := []byte{envSortFlags}
	sortFlags = append(sortFlags, bvarchar("1")...)
	sortFlags = append(sortFlags, bvarchar("")...)

	// SQL collation: 1-byte size (must be 5) + 4-byte info + 1-byte sortID, then
	// an old-value B_VARCHAR.
	collation := []byte{envSqlCollation, 0x05, 0x00, 0x00, 0x00, 0x00, 0x00}
	collation = append(collation, bvarchar("")...)

	beginTran := []byte{envTypBeginTran}
	beginTran = append(beginTran, bvarbyte([]byte{1, 2, 3, 4, 5, 6, 7, 8})...) // 8-byte tranid
	beginTran = append(beginTran, bvarbyte(nil)...)

	commitTran := []byte{envTypCommitTran}
	commitTran = append(commitTran, bvarbyte(nil)...)
	commitTran = append(commitTran, bvarbyte(nil)...)

	mirror := []byte{envDatabaseMirrorPartner}
	mirror = append(mirror, bvarchar("partner")...)
	mirror = append(mirror, bvarchar("")...)

	// Routing: value length (ignored) + protocol(0) + new port + US_VARCHAR
	// alternate server + old value (0x0000).
	routing := []byte{envRouting}
	routing = append(routing, u16le(0)...) // value length, ignored
	routing = append(routing, 0x00)        // protocol TCP
	routing = append(routing, u16le(1433)...)
	routing = append(routing, usvarchar("altserver")...)
	routing = append(routing, u16le(0)...) // old value

	unknown := []byte{0xEE} // unknown envtype -> parser logs and returns

	var seeds [][]byte
	for _, rec := range [][]byte{
		database, language, charset, packetsize, sortID, sortFlags, collation,
		beginTran, commitTran, mirror, routing, unknown,
	} {
		seeds = append(seeds, envChgRecords(rec))
	}

	// Malformed edge cases.
	// Truncated database record (claims a value that isn't there).
	seeds = append(seeds, envChgRecords([]byte{envTypDatabase, 0x05, 'x', 0x00}))
	// Size larger than the actual records.
	seeds = append(seeds, append(u16le(64), envTypDatabase, 0x01, 'x', 0x00, 0x00))
	// Size zero (empty record set).
	seeds = append(seeds, u16le(0))
	// Collation with an invalid (non-5) size byte.
	seeds = append(seeds, envChgRecords([]byte{envSqlCollation, 0x03, 0, 0, 0}))
	// Packet size with a non-numeric value -> strconv.Atoi rejects it.
	seeds = append(seeds, envChgRecords(append(append([]byte{envTypPacketSize}, bvarchar("notanumber")...), bvarchar("")...)))

	return seeds
}

// FuzzEnvChange drives processEnvChg directly with a fuzzed ENVCHANGE body
// (size prefix + records). Seeds cover every subtype the parser understands plus
// truncated/oversized/invalid length fields. processEnvChg reads nested
// B_VARCHAR/B_VARBYTE/US_VARCHAR structures whose lengths are attacker
// controlled, so the invariant is that a malformed record fails as a StreamError
// rather than a runtime panic or a hang.
func FuzzEnvChange(f *testing.F) {
	for _, seed := range envChgSeeds() {
		f.Add(seed, byte(0))
		f.Add(seed, byte(3))
	}

	f.Fuzz(func(t *testing.T, payload []byte, frag byte) {
		if len(payload) > 64*1024 {
			t.Skip()
		}
		sess, closeBuf, ok := fuzzSessionReader(payload, frag)
		if !ok {
			return
		}
		defer closeBuf()

		callExpectingParserPanic(func() {
			processEnvChg(context.Background(), sess)
		})
	})
}

// ==========================================================================
// FuzzFeatureExtAckAndLoginAck
// ==========================================================================

// featureExtFeature frames one FEATUREEXTACK feature: id + uint32 length + data.
func featureExtFeature(id byte, data []byte) []byte {
	out := []byte{id}
	out = append(out, u32le(uint32(len(data)))...)
	out = append(out, data...)
	return out
}

// featureExtAckBody assembles the feature list plus the 0xFF terminator. The
// bytes exclude the leading token id because parseFeatureExtAck is driven
// starting at the first feature byte.
func featureExtAckBody(features ...[]byte) []byte {
	var out []byte
	for _, ft := range features {
		out = append(out, ft...)
	}
	return append(out, featExtTERMINATOR)
}

func featureExtAckSeeds() [][]byte {
	// FEDAUTH: 32-byte nonce + 32-byte signature.
	fedauth := featureExtFeature(featExtFEDAUTH, make([]byte, 64))
	// FEDAUTH advertising only a nonce.
	fedauthNonceOnly := featureExtFeature(featExtFEDAUTH, make([]byte, 32))
	// COLUMNENCRYPTION: version byte + enclave (length-prefixed UCS-2).
	colenc := featureExtFeature(featExtCOLUMNENCRYPTION, append([]byte{0x01, 0x03}, ucs2("abc")...))
	// COLUMNENCRYPTION with just a version byte, no enclave.
	colencNoEnclave := featureExtFeature(featExtCOLUMNENCRYPTION, []byte{0x02})
	// An unknown feature whose payload is skipped via CopyN.
	generic := featureExtFeature(0x08, []byte{1, 2, 3, 4})

	return [][]byte{
		featureExtAckBody(fedauth),
		featureExtAckBody(fedauthNonceOnly),
		featureExtAckBody(colenc),
		featureExtAckBody(colencNoEnclave),
		featureExtAckBody(generic),
		featureExtAckBody(fedauth, colenc, generic),
		featureExtAckBody(), // just the terminator
		// Malformed: feature claims an oversized length with no data behind it.
		append(featureExtFeature(featExtFEDAUTH, nil)[:5], featExtTERMINATOR),
		// Malformed: missing terminator (parser reads past EOF).
		featureExtFeature(0x08, []byte{9, 9}),
	}
}

// loginAckBody assembles a LOGINACK body (excluding the token id): a uint16 size
// followed by interface(1) + TDS version(4) + prog-name-len(1) + prog name
// (UCS-2) + prog version(4).
func loginAckBody(progName string, size int) []byte {
	body := []byte{0x01}            // interface
	body = append(body, 0, 0, 0, 0) // TDS version (big-endian)
	body = append(body, byte(len(progName)))
	body = append(body, ucs2(progName)...)
	body = append(body, 0, 0, 0, 0) // prog version (big-endian)
	if size < 0 {
		size = len(body)
	}
	return append(u16le(uint16(size)), body...)
}

func loginAckSeeds() [][]byte {
	return [][]byte{
		loginAckBody("go-mssqldb", -1),
		loginAckBody("x", -1),
		loginAckBody("", -1),
	}
}

// FuzzFeatureExtAckAndLoginAck drives parseFeatureExtAck (selector even) and
// parseLoginAck (selector odd) directly. FEATUREEXTACK loops over features by a
// uint32 length until a 0xFF terminator; LOGINACK sizes a buffer from a uint16
// and then indexes into it using an embedded prog-name length. Both consume
// server-controlled lengths, so a malformed stream must fail cleanly rather than
// panic out of the parser.
func FuzzFeatureExtAckAndLoginAck(f *testing.F) {
	for _, s := range featureExtAckSeeds() {
		f.Add(byte(0), s, byte(0))
		f.Add(byte(0), s, byte(3))
	}
	for _, s := range loginAckSeeds() {
		f.Add(byte(1), s, byte(0))
		f.Add(byte(1), s, byte(3))
	}

	f.Fuzz(func(t *testing.T, sel byte, payload []byte, frag byte) {
		if len(payload) > 64*1024 {
			t.Skip()
		}
		buf, closeBuf, ok := fuzzTypeReadBuffer(payload, frag)
		if !ok {
			return
		}
		defer closeBuf()

		callExpectingParserPanic(func() {
			if sel%2 == 0 {
				_ = parseFeatureExtAck(buf)
			} else {
				_ = parseLoginAck(buf)
			}
		})
	})
}

// ==========================================================================
// FuzzReturnValue
// ==========================================================================

// returnValueBody assembles a RETURNVALUE body (excluding the token id) carrying
// the given type-info spec's metadata and value: ParamOrdinal(2) + ParamName
// (B_VARCHAR) + Status(1) + UserType(4) + Flags(2) + TypeId(1) + type metadata +
// value bytes. Flags leave the encrypted bit clear so parseReturnValue takes the
// plaintext path (no key provider required).
func returnValueBody(s fuzzTypeSpec) []byte {
	out := u16le(0) // ParamOrdinal
	out = append(out, bvarchar("@p1")...)
	out = append(out, 0x00)       // Status
	out = append(out, 0, 0, 0, 0) // UserType
	out = append(out, 0, 0)       // Flags (not encrypted)
	out = append(out, s.typeId)
	out = append(out, s.typeMeta...)
	out = append(out, s.value...)
	return out
}

// FuzzReturnValue drives parseReturnValue directly, seeded with a RETURNVALUE
// carrying every type family (fixed, byte-len, date/time, short-len, long-len,
// variant, PLP). parseReturnValue reads a full TYPE_INFO and then runs the
// corresponding value Reader, so the mutated seeds exercise both the metadata
// decode and the value decode. The session is not alwaysEncrypted, so the crypto
// branch is skipped and no key provider is needed.
func FuzzReturnValue(f *testing.F) {
	for _, s := range allTypeSpecs() {
		body := returnValueBody(s)
		f.Add(body, byte(0))
		f.Add(body, byte(3))
	}

	f.Fuzz(func(t *testing.T, payload []byte, frag byte) {
		if len(payload) > 64*1024 {
			t.Skip()
		}
		sess, closeBuf, ok := fuzzSessionReader(payload, frag)
		if !ok {
			return
		}
		defer closeBuf()

		callExpectingParserPanic(func() {
			_ = parseReturnValue(sess.buf, sess)
		})
	})
}

// ==========================================================================
// FuzzErrorInfoTokens
// ==========================================================================

// errorInfoBody assembles an ERROR/INFO body (excluding the token id): a uint16
// length (ignored by the parser) + Number(4) + State(1) + Class(1) + Message
// (US_VARCHAR) + ServerName(B_VARCHAR) + ProcName(B_VARCHAR) + LineNo(4).
func errorInfoBody(message string) []byte {
	out := u16le(0)               // length, ignored
	out = append(out, 0, 0, 0, 0) // Number
	out = append(out, 0x01)       // State
	out = append(out, 0x10)       // Class
	out = append(out, usvarchar(message)...)
	out = append(out, bvarchar("server")...)
	out = append(out, bvarchar("proc")...)
	out = append(out, 0, 0, 0, 0) // LineNo
	return out
}

// errorInfoRawUcs2 builds a body whose Message US_VARCHAR advertises numchars
// code units but supplies an odd/short byte payload, exercising the UCS-2
// decoder's error path.
func errorInfoRawUcs2(numchars uint16, raw []byte) []byte {
	out := u16le(0)
	out = append(out, 0, 0, 0, 0)
	out = append(out, 0x01)
	out = append(out, 0x10)
	out = append(out, u16le(numchars)...)
	out = append(out, raw...)
	out = append(out, bvarchar("server")...)
	out = append(out, bvarchar("proc")...)
	out = append(out, 0, 0, 0, 0)
	return out
}

func errorInfoSeeds() [][]byte {
	return [][]byte{
		errorInfoBody("Something went wrong"),
		errorInfoBody(""),
		errorInfoBody("a longer diagnostic message with detail"),
		// Odd number of message bytes for a claimed char count.
		errorInfoRawUcs2(4, []byte{0x41, 0x00, 0x42}),
		// Oversized message char count with no payload behind it.
		errorInfoRawUcs2(0xFFFF, nil),
		// Message length OK but the record is truncated before ServerName.
		func() []byte {
			b := u16le(0)
			b = append(b, 0, 0, 0, 0, 0x01, 0x10)
			b = append(b, usvarchar("hi")...)
			return b
		}(),
	}
}

// FuzzErrorInfoTokens drives parseError72 and parseInfo (selector-chosen; they
// share a layout) with fuzzed ERROR/INFO bodies. These tokens carry US_VARCHAR
// and B_VARCHAR Unicode strings whose lengths the server controls, so malformed
// lengths must surface as a StreamError.
func FuzzErrorInfoTokens(f *testing.F) {
	for _, s := range errorInfoSeeds() {
		f.Add(byte(0), s, byte(0))
		f.Add(byte(1), s, byte(3))
	}

	f.Fuzz(func(t *testing.T, sel byte, payload []byte, frag byte) {
		if len(payload) > 64*1024 {
			t.Skip()
		}
		buf, closeBuf, ok := fuzzTypeReadBuffer(payload, frag)
		if !ok {
			return
		}
		defer closeBuf()

		callExpectingParserPanic(func() {
			if sel%2 == 0 {
				_ = parseError72(buf)
			} else {
				_ = parseInfo(buf)
			}
		})
	})
}

// ==========================================================================
// FuzzAlwaysEncryptedMetadata
// ==========================================================================

// cekTableEntryBytes builds one CEK table entry: databaseId(4) + cekID(4) +
// cekVersion(4) + cekMdVersion(8) + valueCount(1) + valueCount x [encrypted CEK
// (uint16 len + bytes), key store name (byte char count + UCS-2), key path
// (uint16 char count + UCS-2), algorithm name (byte char count + UCS-2)].
func cekTableEntryBytes(values int) []byte {
	out := u32le(1)                       // databaseId
	out = append(out, u32le(2)...)        // cekID
	out = append(out, u32le(3)...)        // cekVersion
	out = append(out, make([]byte, 8)...) // cekMdVersion
	out = append(out, byte(values))       // valueCount
	for i := 0; i < values; i++ {
		out = append(out, u16le(2)...) // encrypted CEK length
		out = append(out, 0xDE, 0xAD)  // encrypted CEK bytes
		out = append(out, 0x03)        // key store name char count
		out = append(out, ucs2("abc")...)
		out = append(out, u16le(3)...) // key path char count
		out = append(out, ucs2("xyz")...)
		out = append(out, 0x04) // algorithm name char count
		out = append(out, ucs2("RSA1")...)
	}
	return out
}

// cekTableBytes frames a CEK table: uint16 entry count followed by each entry.
func cekTableBytes(entries int, valuesPerEntry int) []byte {
	out := u16le(uint16(entries))
	for i := 0; i < entries; i++ {
		out = append(out, cekTableEntryBytes(valuesPerEntry)...)
	}
	return out
}

// encryptedColumnBytes builds one COLMETADATA column marked encrypted:
// UserType(4) + Flags(2, encrypted bit set) + TypeId, then, because the column
// is encrypted, the crypto metadata block: ordinal(2) + base type info
// (UserType(4)+TypeId) + algorithm id + [custom algorithm name] + encType +
// normRuleVer, and finally the ColName B_VARCHAR.
func encryptedColumnBytes(ordinal uint16, algID byte, customName string) []byte {
	out := u32le(0)                               // UserType
	out = append(out, u16le(colFlagEncrypted)...) // Flags: encrypted
	out = append(out, typeInt4)                   // plaintext TypeId (fixed, no metadata)

	// cryptoMetadata
	out = append(out, u16le(ordinal)...) // ordinal into the CEK table
	out = append(out, u32le(0)...)       // base type UserType
	out = append(out, typeInt4)          // base type TypeId (fixed)
	out = append(out, algID)             // algorithm id
	if algID == cipherAlgCustom {
		out = append(out, byte(len(customName)))
		out = append(out, ucs2(customName)...)
	}
	out = append(out, 0x01) // encType
	out = append(out, 0x01) // normRuleVer

	out = append(out, bvarchar("c1")...) // ColName
	return out
}

// plainColumnBytes builds one unencrypted int4 COLMETADATA column.
func plainColumnBytes() []byte {
	out := u32le(0)                // UserType
	out = append(out, u16le(0)...) // Flags
	out = append(out, typeInt4)    // TypeId
	out = append(out, bvarchar("c0")...)
	return out
}

// aeColMetadataBody assembles the COLMETADATA body that parseColMetadata72 reads
// (starting at the column count, excluding the token id) for an Always Encrypted
// session: count(2) + CEK table + columns.
func aeColMetadataBody(cols [][]byte, cekTable []byte) []byte {
	out := u16le(uint16(len(cols)))
	out = append(out, cekTable...)
	for _, c := range cols {
		out = append(out, c...)
	}
	return out
}

func aeMetadataSeeds() [][]byte {
	// Valid: one CEK entry, one encrypted column referencing ordinal 0.
	valid := aeColMetadataBody(
		[][]byte{encryptedColumnBytes(0, 0x01, "")},
		cekTableBytes(1, 1),
	)
	// Valid: custom algorithm name path (algorithm id 0x00 -> name follows).
	custom := aeColMetadataBody(
		[][]byte{encryptedColumnBytes(0, cipherAlgCustom, "MYALG")},
		cekTableBytes(1, 1),
	)
	// Valid: a CEK entry carrying two key values.
	multiValue := aeColMetadataBody(
		[][]byte{encryptedColumnBytes(0, 0x01, "")},
		cekTableBytes(1, 2),
	)
	// Valid: mix of a plaintext column and an encrypted one.
	mixed := aeColMetadataBody(
		[][]byte{plainColumnBytes(), encryptedColumnBytes(0, 0x01, "")},
		cekTableBytes(1, 1),
	)
	// Malformed: ordinal points past the end of the CEK table.
	badOrdinal := aeColMetadataBody(
		[][]byte{encryptedColumnBytes(5, 0x01, "")},
		cekTableBytes(1, 1),
	)
	// Malformed: CEK table advertises more entries than are present.
	shortCekTable := append(u16le(4), cekTableEntryBytes(1)...)
	shortCekTable = append(u16le(1), append(shortCekTable, encryptedColumnBytes(0, 0x01, "")...)...)
	// Malformed: huge CEK value count with no data behind it.
	hugeValueCount := func() []byte {
		entry := u32le(1)
		entry = append(entry, u32le(2)...)
		entry = append(entry, u32le(3)...)
		entry = append(entry, make([]byte, 8)...)
		entry = append(entry, 0xFF) // 255 values, none present
		table := append(u16le(1), entry...)
		return append(u16le(1), append(table, encryptedColumnBytes(0, 0x01, "")...)...)
	}()
	// count == 0xffff means "no metadata" and returns nil immediately.
	noMetadata := u16le(0xffff)

	return [][]byte{
		valid, custom, multiValue, mixed,
		badOrdinal, shortCekTable, hugeValueCount, noMetadata,
	}
}

// FuzzAlwaysEncryptedMetadata drives parseColMetadata72 on an Always Encrypted
// session, focusing on the metadata parse path a malicious server controls
// BEFORE any value decryption: the CEK table (readCekTable / readCekTableEntry)
// and the per-column crypto metadata (parseCryptoMetadata). The seeds never
// include ROW tokens, so decryptColumn is never reached and no real key provider
// is required. Malformed key/algorithm/ordinal/count fields must fail as a
// StreamError (or the deliberate badStreamPanicf for an out-of-range ordinal)
// rather than a runtime panic or a new-site OOM.
func FuzzAlwaysEncryptedMetadata(f *testing.F) {
	for _, s := range aeMetadataSeeds() {
		f.Add(s, byte(0))
		f.Add(s, byte(3))
	}

	f.Fuzz(func(t *testing.T, payload []byte, frag byte) {
		if len(payload) > 64*1024 {
			t.Skip()
		}
		sess, closeBuf, ok := fuzzSessionReader(payload, frag)
		if !ok {
			return
		}
		defer closeBuf()
		sess.alwaysEncrypted = true

		callExpectingParserPanic(func() {
			_ = parseColMetadata72(sess.buf, sess)
		})
	})
}
