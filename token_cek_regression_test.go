package mssql

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func colMetadataWithCekTable(table []byte, ordinal uint16) []byte {
	stream := append([]byte{byte(tokenColMetadata), 1, 0}, table...)
	stream = binary.LittleEndian.AppendUint32(stream, 0)
	stream = binary.LittleEndian.AppendUint16(stream, colFlagEncrypted)
	stream = append(stream, typeBigVarBin)
	stream = binary.LittleEndian.AppendUint16(stream, 8000)
	stream = binary.LittleEndian.AppendUint16(stream, ordinal)
	stream = binary.LittleEndian.AppendUint32(stream, 0)
	stream = append(stream, typeInt4, 2, 1, 1, 0)
	return stream
}

func cekEntryStream(valueCount byte, keyStore, keyPath, algorithm string) []byte {
	stream := binary.LittleEndian.AppendUint32(nil, 7)
	stream = binary.LittleEndian.AppendUint32(stream, 11)
	stream = binary.LittleEndian.AppendUint32(stream, 2)
	stream = append(stream, 1, 2, 3, 4, 5, 6, 7, 8)
	stream = append(stream, valueCount)
	for i := 0; i < int(valueCount); i++ {
		stream = binary.LittleEndian.AppendUint16(stream, 2)
		stream = append(stream, byte(i), 0xa5)
		stream = append(stream, byte(len(keyStore)))
		stream = append(stream, ucs2(keyStore)...)
		stream = binary.LittleEndian.AppendUint16(stream, uint16(len(keyPath)))
		stream = append(stream, ucs2(keyPath)...)
		stream = append(stream, byte(len(algorithm)))
		stream = append(stream, ucs2(algorithm)...)
	}
	return stream
}

func TestReadCekTableEntry_ValueCounts(t *testing.T) {
	for _, count := range []byte{0, 1, 2, 3, 255} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			// Supply all advertised values so an EOF cannot stand in for the count check.
			r := bufFromBytes(cekEntryStream(count, "store", "path", "RSA_OAEP"))
			var entry cekTableEntry
			err := recoverErr(func() { entry = readCekTableEntry(r) })
			if count > 2 {
				assertStreamError(t, err)
				assert.Contains(t, err.Error(), "CEK value count")
				assert.Contains(t, err.Error(), "exceeds maximum 2")
				assert.Equal(t, 21, r.rpos, "must reject the count before reading any key values")
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			assert.Equal(t, 7, entry.databaseID)
			assert.Equal(t, 11, entry.keyId)
			assert.Equal(t, 2, entry.keyVersion)
			assert.Equal(t, []byte{1, 2, 3, 4, 5, 6, 7, 8}, entry.mdVersion)
			assert.Equal(t, int(count), entry.valueCount)
			assert.Len(t, entry.cekValues, int(count))
			for i, value := range entry.cekValues {
				assert.Equal(t, encryptionKeyInfo{
					encryptedKey:  []byte{byte(i), 0xa5},
					databaseID:    7,
					cekID:         11,
					cekVersion:    2,
					cekMdVersion:  entry.mdVersion,
					keyPath:       "path",
					keyStoreName:  "store",
					algorithmName: "RSA_OAEP",
				}, value)
			}
			assert.Equal(t, r.rsize, r.rpos)
		})
	}
}

func TestReadCekTable_BogusCountAllocations(t *testing.T) {
	r := bufFromBytes([]byte{0xff, 0xff})
	var errs [4]error
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := range errs {
		r.rpos = 0
		errs[i] = recoverErr(func() { readCekTable(r) })
	}
	runtime.ReadMemStats(&after)
	for _, err := range errs {
		assertStreamError(t, err)
	}
	// Four attempts amplify the count-driven allocation on both 32- and 64-bit
	// platforms; the fixed parser reads EOF without allocating the advertised table.
	const allocCeiling = 4 << 20
	if grew := after.TotalAlloc - before.TotalAlloc; grew > allocCeiling {
		t.Fatalf("bogus CEK table counts allocated %d bytes; entries must be allocated only after parsing", grew)
	}
}

func TestReadCekTable_Empty(t *testing.T) {
	r := bufFromBytes([]byte{0, 0, 0, 0, 0xa5})
	assert.Nil(t, readCekTable(r))
	assert.Nil(t, readCekTable(r))
	assert.Equal(t, byte(0xa5), r.byte())
}

func TestReadCekTable_PacketBoundaries(t *testing.T) {
	table := []byte{2, 0}
	table = append(table, cekEntryStream(1, "store", "path", "RSA_OAEP")...)
	table = append(table, cekEntryStream(2, "store", "path", "RSA_OAEP")...)
	stream := append(append([]byte{}, table...), table...)
	stream = append(stream, 0xa5)

	for _, chunk := range []int{0, 1, 3, 13} {
		t.Run(fmt.Sprint(chunk), func(t *testing.T) {
			packets, ok := frameReplyPackets(stream, chunk)
			if !ok {
				t.Fatal("failed to frame CEK tables")
			}
			sess := newFuzzSession(packets)
			defer sess.buf.bufClose()
			if _, err := sess.buf.BeginRead(); err != nil {
				t.Fatal(err)
			}
			for attempt := 0; attempt < 2; attempt++ {
				var got *cekTable
				if err := recoverErr(func() { got = readCekTable(sess.buf) }); err != nil {
					t.Fatal(err)
				}
				if got == nil || len(got.entries) != 2 {
					t.Fatalf("expected two CEK entries, got %#v", got)
				}
				for i, entry := range got.entries {
					assert.Equal(t, i+1, entry.valueCount)
					assert.Equal(t, []byte{1, 2, 3, 4, 5, 6, 7, 8}, entry.mdVersion)
					assert.Len(t, entry.cekValues, i+1)
					for j, value := range entry.cekValues {
						assert.Equal(t, []byte{byte(j), 0xa5}, value.encryptedKey)
						assert.Equal(t, "store", value.keyStoreName)
						assert.Equal(t, "path", value.keyPath)
						assert.Equal(t, "RSA_OAEP", value.algorithmName)
					}
				}
			}
			assert.Equal(t, byte(0xa5), sess.buf.byte())
			_, err := sess.buf.ReadByte()
			assert.ErrorIs(t, err, io.EOF)
		})
	}
}

func TestReadCekTableEntry_Lengths(t *testing.T) {
	cases := []struct {
		name      string
		keyStore  string
		keyPath   string
		algorithm string
	}{
		{"provider", strings.Repeat("s", 128), "path", "RSA_OAEP"},
		{"path", "store", strings.Repeat("p", 32768), "RSA_OAEP"},
		{"algorithm", "store", "path", strings.Repeat("a", 128)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stream := cekEntryStream(1, tc.keyStore, tc.keyPath, tc.algorithm)
			packets, ok := frameReplyPackets(stream, 0)
			if !ok {
				t.Fatal("failed to frame CEK entry")
			}
			sess := newFuzzSession(packets)
			defer sess.buf.bufClose()
			if _, err := sess.buf.BeginRead(); err != nil {
				t.Fatal(err)
			}
			var entry cekTableEntry
			if err := recoverErr(func() { entry = readCekTableEntry(sess.buf) }); err != nil {
				t.Fatal(err)
			}
			if len(entry.cekValues) != 1 {
				t.Fatalf("expected one CEK value, got %d", len(entry.cekValues))
			}
			value := entry.cekValues[0]
			assert.Equal(t, tc.keyStore, value.keyStoreName)
			assert.Equal(t, tc.keyPath, value.keyPath)
			assert.Equal(t, tc.algorithm, value.algorithmName)
			_, err := sess.buf.ReadByte()
			assert.ErrorIs(t, err, io.EOF)
		})
	}
}

func TestReadCekTableEntry_Truncated(t *testing.T) {
	stream := cekEntryStream(1, "store", "path", "RSA_OAEP")
	for end := 0; end < len(stream); end++ {
		t.Run(fmt.Sprint(end), func(t *testing.T) {
			err := recoverErr(func() { readCekTableEntry(bufFromBytes(stream[:end])) })
			assertStreamError(t, err)
		})
	}
}

func TestParseColMetadata72_CekTable(t *testing.T) {
	table := []byte{2, 0}
	table = append(table, cekEntryStream(1, "store", "path", "RSA_OAEP")...)
	second := cekEntryStream(2, "store", "path", "RSA_OAEP")
	binary.LittleEndian.PutUint32(second[4:8], 12)
	table = append(table, second...)
	stream := colMetadataWithCekTable(table, 1)
	columns := parseColMetadata72(bufFromBytes(stream[1:]), &tdsSession{alwaysEncrypted: true})
	if len(columns) != 1 || columns[0].cryptoMeta == nil || columns[0].cryptoMeta.entry == nil {
		t.Fatal("expected one encrypted column with CEK metadata")
	}
	cm := columns[0].cryptoMeta
	assert.Equal(t, uint16(1), cm.ordinal)
	assert.Equal(t, 12, cm.entry.keyId)
	assert.Equal(t, 2, cm.entry.valueCount)
	assert.Equal(t, byte(typeInt4), cm.typeInfo.TypeId)
	assert.True(t, columns[0].isEncrypted())
}

func TestProcessSingleResponse_CekAllocations(t *testing.T) {
	cases := map[string][]byte{
		"bogus table count": {byte(tokenColMetadata), 0, 0, 0xff, 0xff},
		"bogus value count": colMetadataWithCekTable(
			append([]byte{0xff, 0xff}, cekEntryStream(255, "store", "path", "RSA_OAEP")...), 0),
	}
	for name, stream := range cases {
		t.Run(name, func(t *testing.T) {
			for _, chunk := range []int{0, 1, 3} {
				tokens, _, sawError, framed := drainSingleResponseWithEncryption(stream, chunk, true, true)
				if !framed || !sawError {
					t.Fatalf("expected a malformed encrypted response, chunk=%d", chunk)
				}
				assert.Contains(t, tokens, "error:mssql.StreamError")
			}
		})
	}
}

func TestProcessSingleResponse_CekRotation(t *testing.T) {
	table := append([]byte{1, 0}, cekEntryStream(2, "store", "path", "RSA_OAEP")...)
	stream := append(colMetadataWithCekTable(table, 0), doneToken(tokenDone, 0)...)
	for _, chunk := range []int{0, 1, 3} {
		tokens, _, sawError, framed := drainSingleResponseWithEncryption(stream, chunk, true, true)
		if !framed || sawError {
			t.Fatalf("valid encrypted metadata failed with chunk=%d: %v", chunk, tokens)
		}
		assert.Contains(t, tokens, fmt.Sprintf("cols[{name=%q type=%d usertype=0 flags=%d}]", "", typeBigVarBin, colFlagEncrypted))
		assert.Contains(t, tokens, "done{status=0 curcmd=0 rows=0 errs=[]}")
	}
}

func TestCekStreamErrorMarksConnectionBad(t *testing.T) {
	r := bufFromBytes(cekEntryStream(255, "store", "path", "RSA_OAEP"))
	err := recoverErr(func() { readCekTableEntry(r) })
	assertStreamError(t, err)
	conn := &Conn{connectionGood: true}
	assert.Equal(t, err, conn.checkBadConn(context.Background(), err, false))
	assert.False(t, conn.connectionGood)
}
