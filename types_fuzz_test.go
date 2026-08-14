package mssql

import (
	"encoding/binary"
	"runtime"
	"testing"

	"github.com/microsoft/go-mssqldb/msdsn"
)

// This file adds structured fuzz targets for the second layer of the TDS parser
// hardening effort tracked by issue #418. Raw random bytes almost never form a
// valid COLMETADATA/TYPE_INFO stream, so every target is seeded with wire-format
// bytes assembled from the same builders the real parser expects. The fuzz
// bodies then let the engine mutate those structured seeds.
//
// INVARIANTS enforced by every target here:
//   - The parser must never hang and never OOM.
//   - The only acceptable failure on malformed input is a recovered parser
//     rejection: either a StreamError (from badStreamPanic) or a deliberate
//     badStreamPanicf(error). Those are swallowed.
//   - A runtime panic (index out of range, nil deref, slice bounds) or a bare
//     string panic ("shouldn't get here") is treated as a REAL BUG: it is not
//     swallowed, so the fuzz engine records and minimizes the crasher.
//
// The metadata/row targets reuse drainSingleResponse (from token_fuzz_test.go),
// which already installs processSingleResponse's recover(), so those never need
// their own panic handling. The direct type/variant/PLP targets drive the
// readXxx functions themselves and therefore wrap the call in
// callExpectingParserPanic.

// isExpectedParserPanic reports whether a recovered panic value represents a
// deliberate parser rejection of a malformed stream (acceptable) rather than a
// real bug. StreamError and the plain fmt.Errorf values raised by
// badStreamPanicf both satisfy the error interface but are NOT runtime.Error;
// runtime panics (index out of range, slice bounds, nil deref) implement
// runtime.Error and are treated as bugs, as are non-error (e.g. string) panics.
func isExpectedParserPanic(r interface{}) bool {
	if _, isRuntime := r.(runtime.Error); isRuntime {
		return false
	}
	_, isErr := r.(error)
	return isErr
}

// callExpectingParserPanic runs fn, swallowing an expected parser rejection and
// re-panicking anything that looks like a real bug so the fuzz engine captures
// it as a crasher.
func callExpectingParserPanic(fn func()) {
	defer func() {
		if r := recover(); r != nil {
			if isExpectedParserPanic(r) {
				return
			}
			panic(r)
		}
	}()
	fn()
}

// fuzzTypeReadBuffer frames payload as one or more packReply packets and returns
// a *tdsBuffer positioned at the first payload byte (after BeginRead consumes
// the packet header). This lets the direct readXxx targets read a fuzzed byte
// stream through the real tdsBuffer, including across packet seams selected by
// frag. The returned closer must be called to return the pooled buffer.
func fuzzTypeReadBuffer(payload []byte, frag byte) (buf *tdsBuffer, closeBuf func(), ok bool) {
	framed, framedOK := frameReplyPackets(payload, frag)
	if !framedOK {
		return nil, nil, false
	}
	sess := newFuzzSession(framed)
	if _, err := sess.buf.BeginRead(); err != nil {
		sess.buf.bufClose()
		return nil, nil, false
	}
	return sess.buf, sess.buf.bufClose, true
}

// --- column / type-info wire builders ------------------------------------

// fuzzColumn is one COLMETADATA column plus the matching ROW value bytes.
type fuzzColumn struct {
	meta []byte // UserType(4) + Flags(2) + TypeId(1) + type-specific metadata
	val  []byte // the value bytes a ROW carries for this column
}

// fuzzTypeSpec captures just the type-info metadata (what readTypeInfo/readVarLen
// consume after the TypeId) and the matching value bytes, so the same spec can
// seed both the COLMETADATA/ROW targets and the direct readTypeInfo target.
type fuzzTypeSpec struct {
	name     string
	typeId   byte
	typeMeta []byte // metadata read by readVarLen (excludes UserType/Flags/TypeId)
	value    []byte // value bytes read by ti.Reader
}

// colBase prepends the UserType(4)+Flags(2)+TypeId(1) header to type metadata.
func colBase(typeId byte, typeMeta []byte) []byte {
	b := make([]byte, 0, 7+len(typeMeta))
	b = append(b, 0, 0, 0, 0) // UserType
	b = append(b, 0, 0)       // Flags
	b = append(b, typeId)
	b = append(b, typeMeta...)
	return b
}

func (s fuzzTypeSpec) column() fuzzColumn {
	return fuzzColumn{meta: colBase(s.typeId, s.typeMeta), val: s.value}
}

// buildColMetadata builds a COLMETADATA token for the given columns, each with
// an empty column name.
func buildColMetadata(cols []fuzzColumn) []byte {
	out := []byte{byte(tokenColMetadata)}
	var cnt [2]byte
	binary.LittleEndian.PutUint16(cnt[:], uint16(len(cols)))
	out = append(out, cnt[:]...)
	for _, c := range cols {
		out = append(out, c.meta...)
		out = append(out, 0x00) // ColName BVarChar, length 0
	}
	return out
}

// buildRow builds a ROW token carrying every column's value bytes.
func buildRow(cols []fuzzColumn) []byte {
	out := []byte{byte(tokenRow)}
	for _, c := range cols {
		out = append(out, c.val...)
	}
	return out
}

// buildResponse assembles COLMETADATA + ROW + final DONE for a column set.
func buildResponse(cols []fuzzColumn) []byte {
	return concat(buildColMetadata(cols), buildRow(cols), doneToken(tokenDone, doneFinal))
}

// ucs2 encodes an ASCII string as little-endian UCS-2, as SQL Server sends
// national character data.
func ucs2(s string) []byte {
	out := make([]byte, 0, len(s)*2)
	for i := 0; i < len(s); i++ {
		out = append(out, s[i], 0x00)
	}
	return out
}

// byteLenValue frames a BYTELEN value (1 length byte + data).
func byteLenValue(data []byte) []byte {
	return append([]byte{byte(len(data))}, data...)
}

// shortLenValue frames a USHORTLEN value (2 length bytes + data).
func shortLenValue(data []byte) []byte {
	var l [2]byte
	binary.LittleEndian.PutUint16(l[:], uint16(len(data)))
	return append(l[:], data...)
}

// collationBytes is a zero collation (5 bytes: uint32 LcidAndFlags + SortId).
func collationBytes() []byte { return []byte{0, 0, 0, 0, 0} }

// shortSizeBytes is the little-endian uint16 max-size field for short-len types.
func shortSizeBytes(size uint16) []byte {
	var b [2]byte
	binary.LittleEndian.PutUint16(b[:], size)
	return b[:]
}

// longLenValue frames a TEXT/NTEXT/IMAGE value: a 1-byte textptr length + textptr
// + 8-byte timestamp + int32 data size + data.
func longLenValue(data []byte) []byte {
	out := []byte{0x01, 0xAA}             // textptr length 1, textptr byte
	out = append(out, make([]byte, 8)...) // timestamp (ignored)
	var sz [4]byte
	binary.LittleEndian.PutUint32(sz[:], uint32(len(data)))
	out = append(out, sz[:]...)
	out = append(out, data...)
	return out
}

// variantValue builds a sql_variant value: int32 total size + vartype byte +
// propbytes byte + property bytes + data bytes. size is (2 + len(prop) + len(data))
// so readVariantTypeWithEncoding's datalen == len(data).
func variantValue(vartype byte, prop, data []byte) []byte {
	size := 2 + len(prop) + len(data)
	var sz [4]byte
	binary.LittleEndian.PutUint32(sz[:], uint32(size))
	out := append([]byte{}, sz[:]...)
	out = append(out, vartype, byte(len(prop)))
	out = append(out, prop...)
	out = append(out, data...)
	return out
}

// --- PLP wire builders ----------------------------------------------------

func plpNull() []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, _PLP_NULL)
	return b
}

// plpChunks builds a PLP value advertising size, followed by each chunk
// (uint32 length + bytes) and, when terminate is set, the zero-length
// terminator chunk.
func plpChunks(size uint64, chunks [][]byte, terminate bool) []byte {
	out := make([]byte, 8)
	binary.LittleEndian.PutUint64(out, size)
	for _, c := range chunks {
		var l [4]byte
		binary.LittleEndian.PutUint32(l[:], uint32(len(c)))
		out = append(out, l[:]...)
		out = append(out, c...)
	}
	if terminate {
		out = append(out, 0, 0, 0, 0)
	}
	return out
}

// --- type spec catalog ----------------------------------------------------

// fixedTypeSpecs returns one column of every fixed-length type.
func fixedTypeSpecs() []fuzzTypeSpec {
	return []fuzzTypeSpec{
		{"int1", typeInt1, nil, []byte{0x7f}},
		{"bit", typeBit, nil, []byte{0x01}},
		{"int2", typeInt2, nil, []byte{0x34, 0x12}},
		{"int4", typeInt4, nil, []byte{0x04, 0x03, 0x02, 0x01}},
		{"int8", typeInt8, nil, []byte{8, 7, 6, 5, 4, 3, 2, 1}},
		{"flt4", typeFlt4, nil, []byte{0xdb, 0x0f, 0x49, 0x40}},
		{"flt8", typeFlt8, nil, []byte{0x18, 0x2d, 0x44, 0x54, 0xfb, 0x21, 0x09, 0x40}},
		{"money", typeMoney, nil, []byte{0, 0, 0, 0, 0, 0, 0, 0}},
		{"money4", typeMoney4, nil, []byte{0, 0, 0, 0}},
		{"datetime", typeDateTime, nil, []byte{0, 0, 0, 0, 0, 0, 0, 0}},
		{"datetim4", typeDateTim4, nil, []byte{0, 0, 0, 0}},
	}
}

// byteLenTypeSpecs returns one column of every BYTELEN variable type.
func byteLenTypeSpecs() []fuzzTypeSpec {
	dec := []byte{0x01, 0x2a, 0x00, 0x00, 0x00} // sign + one uint32 group
	return []fuzzTypeSpec{
		{"intN", typeIntN, []byte{4}, byteLenValue([]byte{4, 3, 2, 1})},
		{"bitN", typeBitN, []byte{1}, byteLenValue([]byte{1})},
		{"fltN", typeFltN, []byte{8}, byteLenValue([]byte{0, 0, 0, 0, 0, 0, 0, 0})},
		{"moneyN", typeMoneyN, []byte{8}, byteLenValue([]byte{0, 0, 0, 0, 0, 0, 0, 0})},
		{"datetimeN", typeDateTimeN, []byte{8}, byteLenValue([]byte{0, 0, 0, 0, 0, 0, 0, 0})},
		{"guid", typeGuid, []byte{0x10}, byteLenValue(make([]byte, 16))},
		{"decimalN", typeDecimalN, []byte{17, 18, 0}, byteLenValue(dec)},
		{"numericN", typeNumericN, []byte{17, 18, 0}, byteLenValue(dec)},
		{"decimal", typeDecimal, []byte{17, 18, 0}, byteLenValue(dec)},
		{"numeric", typeNumeric, []byte{17, 18, 0}, byteLenValue(dec)},
		{"char", typeChar, []byte{4}, byteLenValue([]byte("abcd"))},
		{"varchar", typeVarChar, []byte{4}, byteLenValue([]byte("abcd"))},
		{"binary", typeBinary, []byte{4}, byteLenValue([]byte{1, 2, 3, 4})},
		{"varbinary", typeVarBinary, []byte{4}, byteLenValue([]byte{1, 2, 3, 4})},
	}
}

// dateTimeTypeSpecs returns date/time BYTELEN types across their valid scales.
func dateTimeTypeSpecs() []fuzzTypeSpec {
	specs := []fuzzTypeSpec{
		{"dateN", typeDateN, nil, byteLenValue([]byte{0, 0, 0})},
	}
	// scale -> time payload size (base size of the time component)
	timeSize := map[byte]int{0: 3, 1: 3, 2: 3, 3: 4, 4: 4, 5: 5, 6: 5, 7: 5}
	for _, scale := range []byte{0, 3, 7} {
		ts := timeSize[scale]
		specs = append(specs,
			fuzzTypeSpec{"timeN", typeTimeN, []byte{scale}, byteLenValue(make([]byte, ts))},
			fuzzTypeSpec{"datetime2N", typeDateTime2N, []byte{scale}, byteLenValue(make([]byte, ts+3))},
			fuzzTypeSpec{"datetimeoffsetN", typeDateTimeOffsetN, []byte{scale}, byteLenValue(make([]byte, ts+5))},
		)
	}
	return specs
}

// shortLenTypeSpecs returns short-length (USHORTLEN) types read via readShortLenType.
func shortLenTypeSpecs() []fuzzTypeSpec {
	const size = uint16(8000)
	charMeta := append(shortSizeBytes(size), collationBytes()...)
	binMeta := shortSizeBytes(size)
	return []fuzzTypeSpec{
		{"bigvarchar", typeBigVarChar, charMeta, shortLenValue([]byte("abcd"))},
		{"bigchar", typeBigChar, charMeta, shortLenValue([]byte("abcd"))},
		{"nvarchar", typeNVarChar, charMeta, shortLenValue(ucs2("ab"))},
		{"nchar", typeNChar, charMeta, shortLenValue(ucs2("ab"))},
		{"bigvarbin", typeBigVarBin, binMeta, shortLenValue([]byte{1, 2, 3, 4})},
		{"bigbinary", typeBigBinary, binMeta, shortLenValue([]byte{1, 2, 3, 4})},
	}
}

// plpTypeSpecs returns the (max)/PLP-flavored short-len types plus XML, all read
// via readPLPType.
func plpTypeSpecs() []fuzzTypeSpec {
	maxChar := append(shortSizeBytes(0xffff), collationBytes()...)
	maxBin := shortSizeBytes(0xffff)
	return []fuzzTypeSpec{
		{"bigvarchar_max", typeBigVarChar, maxChar, plpChunks(4, [][]byte{[]byte("abcd")}, true)},
		{"nvarchar_max", typeNVarChar, maxChar, plpChunks(4, [][]byte{ucs2("ab")}, true)},
		{"bigvarbin_max", typeBigVarBin, maxBin, plpChunks(4, [][]byte{{1, 2, 3, 4}}, true)},
		{"xml", typeXml, []byte{0x00}, plpChunks(4, [][]byte{ucs2("ab")}, true)},
	}
}

// longLenTypeSpecs returns the LONGLEN types text/ntext/image.
func longLenTypeSpecs() []fuzzTypeSpec {
	textMeta := concat([]byte{0, 0, 0, 0}, collationBytes(), []byte{0x00}) // size + collation + numparts
	imageMeta := []byte{0, 0, 0, 0, 0x00}                                  // size + numparts
	return []fuzzTypeSpec{
		{"text", typeText, textMeta, longLenValue([]byte("hello"))},
		{"ntext", typeNText, textMeta, longLenValue(ucs2("hello"))},
		{"image", typeImage, imageMeta, longLenValue([]byte{1, 2, 3, 4, 5})},
	}
}

// variantSubtypeValues returns representative sql_variant payloads keyed by the
// variant subtype they carry.
func variantSubtypeValues() [][]byte {
	return [][]byte{
		variantValue(typeInt4, nil, []byte{4, 3, 2, 1}),
		variantValue(typeBit, nil, []byte{1}),
		variantValue(typeInt8, nil, []byte{8, 7, 6, 5, 4, 3, 2, 1}),
		variantValue(typeFlt8, nil, make([]byte, 8)),
		variantValue(typeMoney, nil, make([]byte, 8)),
		variantValue(typeDateTime, nil, make([]byte, 8)),
		variantValue(typeGuid, nil, make([]byte, 16)),
		variantValue(typeDecimalN, []byte{18, 0}, []byte{0x01, 0x2a, 0, 0, 0}),
		variantValue(typeBigVarChar, append(collationBytes(), 0, 0), []byte("abcd")),
		variantValue(typeNVarChar, append(collationBytes(), 0, 0), ucs2("ab")),
		variantValue(typeTimeN, []byte{7}, make([]byte, 5)),
		variantValue(typeBigVarBin, []byte{0, 0}, []byte{1, 2, 3, 4}),
	}
}

// variantTypeSpecs wraps each variant subtype value as its own sql_variant column.
func variantTypeSpecs() []fuzzTypeSpec {
	var specs []fuzzTypeSpec
	for _, v := range variantSubtypeValues() {
		specs = append(specs, fuzzTypeSpec{"variant", typeVariant, []byte{0, 0, 0, 0}, v})
	}
	return specs
}

// allTypeSpecs concatenates every family for a broad seed.
func allTypeSpecs() []fuzzTypeSpec {
	var specs []fuzzTypeSpec
	specs = append(specs, fixedTypeSpecs()...)
	specs = append(specs, byteLenTypeSpecs()...)
	specs = append(specs, dateTimeTypeSpecs()...)
	specs = append(specs, shortLenTypeSpecs()...)
	specs = append(specs, plpTypeSpecs()...)
	specs = append(specs, longLenTypeSpecs()...)
	specs = append(specs, variantTypeSpecs()...)
	return specs
}

func columnsFor(specs []fuzzTypeSpec) []fuzzColumn {
	cols := make([]fuzzColumn, len(specs))
	for i, s := range specs {
		cols[i] = s.column()
	}
	return cols
}

// --- FuzzColMetadataAndRow -----------------------------------------------

// FuzzColMetadataAndRow feeds COLMETADATA + ROW + DONE streams, seeded with one
// column of every type family (fixed, byte-len, date/time, short-len, PLP,
// long-len, and several sql_variant subtypes), through the full response parser
// via drainSingleResponse. processSingleResponse installs its own recover, so
// the invariant is simply that the harness must always return: no hang, no OOM,
// no escaping panic regardless of how the fuzzer mutates the seed.
func FuzzColMetadataAndRow(f *testing.F) {
	seeds := [][]byte{
		buildResponse(columnsFor(fixedTypeSpecs())),
		buildResponse(columnsFor(byteLenTypeSpecs())),
		buildResponse(columnsFor(dateTimeTypeSpecs())),
		buildResponse(columnsFor(shortLenTypeSpecs())),
		buildResponse(columnsFor(plpTypeSpecs())),
		buildResponse(columnsFor(longLenTypeSpecs())),
		buildResponse(columnsFor(variantTypeSpecs())),
		buildResponse(columnsFor(allTypeSpecs())),
	}
	for _, seed := range seeds {
		f.Add(seed, byte(0))
		f.Add(seed, byte(3))
	}

	f.Fuzz(func(t *testing.T, stream []byte, frag byte) {
		if len(stream) > 64*1024 {
			t.Skip()
		}
		_, _, _ = drainSingleResponse(stream, frag)
	})
}

// --- FuzzNbcRow -----------------------------------------------------------

// nbcRowResponse builds COLMETADATA(n int4 columns) + NBCROW(nullMask) + DONE.
func nbcRowResponse(n int, nullMask []bool) []byte {
	cols := make([]fuzzColumn, n)
	for i := range cols {
		cols[i] = fixedTypeSpecs()[3].column() // int4
	}
	meta := buildColMetadata(cols)

	bitlen := (n + 7) / 8
	bm := make([]byte, bitlen)
	for i := 0; i < n; i++ {
		if nullMask[i] {
			bm[i/8] |= 1 << (uint(i) % 8)
		}
	}
	row := append([]byte{byte(tokenNbcRow)}, bm...)
	for i := 0; i < n; i++ {
		if !nullMask[i] {
			row = append(row, 0, 0, 0, 0) // int4 value
		}
	}
	return concat(meta, row, doneToken(tokenDone, doneFinal))
}

// FuzzNbcRow exercises the NBCROW null-bitmap decoder around the byte
// boundaries (column counts 7/8/9 and 15/16/17) with assorted null masks.
func FuzzNbcRow(f *testing.F) {
	for _, n := range []int{7, 8, 9, 15, 16, 17} {
		masks := [][]bool{
			make([]bool, n), // none null
		}
		allNull := make([]bool, n)
		alt := make([]bool, n)
		for i := 0; i < n; i++ {
			allNull[i] = true
			alt[i] = i%2 == 0
		}
		masks = append(masks, allNull, alt)
		for _, m := range masks {
			f.Add(nbcRowResponse(n, m), byte(0))
			f.Add(nbcRowResponse(n, m), byte(3))
		}
	}

	f.Fuzz(func(t *testing.T, stream []byte, frag byte) {
		if len(stream) > 64*1024 {
			t.Skip()
		}
		_, _, _ = drainSingleResponse(stream, frag)
	})
}

// --- FuzzTypeInfoAndValue -------------------------------------------------

// FuzzTypeInfoAndValue drives readTypeInfo directly with a fuzzed typeId and
// trailing metadata bytes, then, if a Reader was installed, runs it against the
// remaining fuzzed value bytes. Seeds cover every valid typeId with valid
// metadata + value; the engine mutates the typeId, metadata, and value
// independently.
func FuzzTypeInfoAndValue(f *testing.F) {
	for _, s := range allTypeSpecs() {
		f.Add(s.typeId, s.typeMeta, s.value, byte(0))
		f.Add(s.typeId, s.typeMeta, s.value, byte(3))
	}

	f.Fuzz(func(t *testing.T, typeId byte, meta, value []byte, frag byte) {
		if len(meta)+len(value) > 64*1024 {
			t.Skip()
		}
		payload := append(append([]byte{}, meta...), value...)
		buf, closeBuf, ok := fuzzTypeReadBuffer(payload, frag)
		if !ok {
			return
		}
		defer closeBuf()

		callExpectingParserPanic(func() {
			ti := readTypeInfo(buf, typeId, nil, msdsn.EncodeParameters{})
			if ti.Reader != nil {
				_ = ti.Reader(&ti, buf, nil, msdsn.EncodeParameters{})
			}
		})
	})
}

// --- FuzzVariantValue -----------------------------------------------------

// FuzzVariantValue drives readVariantTypeWithEncoding with fuzzed
// size/vartype/propbytes/payload, seeded with every supported variant subtype.
func FuzzVariantValue(f *testing.F) {
	for _, v := range variantSubtypeValues() {
		f.Add(v, byte(0))
		f.Add(v, byte(3))
	}
	// size == 0 is the NULL variant; keep it as a seed.
	f.Add([]byte{0, 0, 0, 0}, byte(0))

	f.Fuzz(func(t *testing.T, payload []byte, frag byte) {
		if len(payload) > 64*1024 {
			t.Skip()
		}
		buf, closeBuf, ok := fuzzTypeReadBuffer(payload, frag)
		if !ok {
			return
		}
		defer closeBuf()

		ti := typeInfo{TypeId: typeVariant}
		callExpectingParserPanic(func() {
			_ = readVariantTypeWithEncoding(&ti, buf, nil, msdsn.EncodeParameters{})
		})
	})
}

// --- FuzzPLPValue ---------------------------------------------------------

// plpFuzzTypeIds are the type ids whose values are read as partially
// length-prefixed streams.
var plpFuzzTypeIds = []byte{
	typeXml, typeBigVarChar, typeBigChar, typeNVarChar, typeNChar,
	typeBigVarBin, typeBigBinary, typeText, typeNText, typeImage,
}

// FuzzPLPValue drives readPLPType with a fuzzed advertised size, chunk sizes,
// and payload against each PLP-backed type id. Seeds cover NULL, unknown-length,
// single-chunk, multi-chunk, empty, and missing-terminator streams.
func FuzzPLPValue(f *testing.F) {
	seeds := [][]byte{
		plpNull(),
		plpChunks(_UNKNOWN_PLP_LEN, [][]byte{[]byte("hello")}, true),
		plpChunks(5, [][]byte{[]byte("hello")}, true),
		plpChunks(8, [][]byte{[]byte("ab"), []byte("cdef")}, true),
		plpChunks(0, nil, true),                        // empty value
		plpChunks(5, [][]byte{[]byte("hello")}, false), // missing terminator
		plpChunks(_UNKNOWN_PLP_LEN, nil, true),         // unknown length, empty
	}
	for sel := range plpFuzzTypeIds {
		for _, s := range seeds {
			f.Add(byte(sel), s, byte(0))
		}
	}

	f.Fuzz(func(t *testing.T, typeSel byte, payload []byte, frag byte) {
		if len(payload) > 64*1024 {
			t.Skip()
		}
		buf, closeBuf, ok := fuzzTypeReadBuffer(payload, frag)
		if !ok {
			return
		}
		defer closeBuf()

		ti := typeInfo{TypeId: plpFuzzTypeIds[int(typeSel)%len(plpFuzzTypeIds)]}
		callExpectingParserPanic(func() {
			_ = readPLPType(&ti, buf, nil, msdsn.EncodeParameters{})
		})
	})
}
