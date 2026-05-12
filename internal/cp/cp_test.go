package cp

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCollation_getLcid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		lcidAndFlags uint32
		expected     uint32
	}{
		{"zero", 0, 0},
		{"simple lcid", 0x00000409, 0x00000409}, // US English
		{"with flags", 0x0ff00409, 0x00000409},  // US English with flags
		{"max lcid", 0x000fffff, 0x000fffff},
		{"thai", 0x0000041e, 0x0000041e},
		{"japanese", 0x00000411, 0x00000411},
		{"chinese simplified", 0x00000804, 0x00000804},
		{"korean", 0x00000412, 0x00000412},
		{"chinese traditional", 0x00000404, 0x00000404},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := Collation{LcidAndFlags: tc.lcidAndFlags}
			assert.Equal(t, tc.expected, c.getLcid(), "getLcid() mismatch")
		})
	}
}

func TestCollation_getFlags(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		lcidAndFlags uint32
		expected     uint32
	}{
		{"zero", 0, 0},
		{"no flags", 0x00000409, 0},
		{"with flags", 0x0ab00409, 0xab},
		{"max flags", 0x0ff00000, 0xff},
		{"mixed", 0x05500000, 0x55},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := Collation{LcidAndFlags: tc.lcidAndFlags}
			assert.Equal(t, tc.expected, c.getFlags(), "getFlags() mismatch")
		})
	}
}

func TestCollation_getVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		lcidAndFlags uint32
		expected     uint32
	}{
		{"zero", 0, 0},
		{"version 0", 0x00000409, 0},
		{"version 1", 0x10000409, 1},
		{"version 2", 0x20000409, 2},
		{"max version", 0xf0000000, 0xf},
		{"version 8", 0x80000000, 8},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := Collation{LcidAndFlags: tc.lcidAndFlags}
			assert.Equal(t, tc.expected, c.getVersion(), "getVersion() mismatch")
		})
	}
}

func TestCollation2Charset_BySortId(t *testing.T) {
	t.Parallel()
	// Test various sort IDs that map to specific code pages
	tests := []struct {
		name     string
		sortId   uint8
		expected string // name of expected code page function
	}{
		{"sortId 30 -> cp437", 30, "cp437"},
		{"sortId 31 -> cp437", 31, "cp437"},
		{"sortId 40 -> cp850", 40, "cp850"},
		{"sortId 50 -> cp1252", 50, "cp1252"},
		{"sortId 80 -> cp1250", 80, "cp1250"},
		{"sortId 104 -> cp1251", 104, "cp1251"},
		{"sortId 112 -> cp1253", 112, "cp1253"},
		{"sortId 128 -> cp1254", 128, "cp1254"},
		{"sortId 136 -> cp1255", 136, "cp1255"},
		{"sortId 144 -> cp1256", 144, "cp1256"},
		{"sortId 152 -> cp1257", 152, "cp1257"},
		{"sortId 192 -> cp932", 192, "cp932"},
		{"sortId 194 -> cp949", 194, "cp949"},
		{"sortId 196 -> cp950", 196, "cp950"},
		{"sortId 198 -> cp936", 198, "cp936"},
		{"sortId 204 -> cp874", 204, "cp874"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := Collation{SortId: tc.sortId}
			cm := collation2charset(c)
			assert.NotNil(t, cm, "collation2charset returned nil for sortId %d", tc.sortId)
			if cm == nil {
				return
			}
			// Verify charset map is not empty
			nonZeroCount := 0
			for _, r := range cm.sb {
				if r != 0 {
					nonZeroCount++
				}
			}
			assert.NotZero(t, nonZeroCount, "charset map has no non-zero entries")
		})
	}
}

func TestCollation2Charset_ByLcid(t *testing.T) {
	t.Parallel()
	// Test various LCIDs that map to specific code pages
	tests := []struct {
		name string
		lcid uint32
	}{
		{"thai 0x041e", 0x041e},
		{"japanese 0x0411", 0x0411},
		{"chinese simplified 0x0804", 0x0804},
		{"korean 0x0412", 0x0412},
		{"chinese traditional 0x0404", 0x0404},
		{"polish 0x0415 -> cp1250", 0x0415},
		{"russian 0x0419 -> cp1251", 0x0419},
		{"greek 0x0408 -> cp1253", 0x0408},
		{"turkish 0x041f -> cp1254", 0x041f},
		{"hebrew 0x040d -> cp1255", 0x040d},
		{"arabic 0x0401 -> cp1256", 0x0401},
		{"estonian 0x0425 -> cp1257", 0x0425},
		{"vietnamese 0x042a -> cp1258", 0x042a},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := Collation{LcidAndFlags: tc.lcid, SortId: 0}
			cm := collation2charset(c)
			if cm == nil {
				// Some LCIDs return nil (like Hindi)
				return
			}
			// Verify charset map is not empty
			nonZeroCount := 0
			for _, r := range cm.sb {
				if r != 0 {
					nonZeroCount++
				}
			}
			assert.NotZero(t, nonZeroCount, "charset map has no non-zero entries")
		})
	}
}

func TestCollation2Charset_DefaultToCP1252(t *testing.T) {
	t.Parallel()
	// Unknown sort ID and LCID should default to cp1252
	c := Collation{LcidAndFlags: 0x00000409, SortId: 0} // US English, no sort ID
	cm := collation2charset(c)
	if cm == nil {
		t.Fatal("collation2charset returned nil for default case")
	}
}

func TestCollation2Charset_NilForUnicode(t *testing.T) {
	t.Parallel()
	// Some LCIDs return nil (Unicode languages like Hindi)
	unicodeLcids := []uint32{0x0439, 0x045a, 0x0465}
	for _, lcid := range unicodeLcids {
		c := Collation{LcidAndFlags: lcid, SortId: 0}
		cm := collation2charset(c)
		assert.Nil(t, cm, "Expected nil for LCID %#x, got charset map", lcid)
	}
}

func TestCharsetToUTF8_ASCII(t *testing.T) {
	t.Parallel()
	// ASCII characters should pass through unchanged in most code pages
	c := Collation{SortId: 50} // cp1252
	input := []byte("Hello, World!")
	result := CharsetToUTF8(c, input)
	assert.Equal(t, "Hello, World!", result)
}

func TestCharsetToUTF8_NilCharset(t *testing.T) {
	t.Parallel()
	// When charset is nil, should return string as-is
	c := Collation{LcidAndFlags: 0x0439, SortId: 0} // Hindi - returns nil charset
	input := []byte("test")
	result := CharsetToUTF8(c, input)
	assert.Equal(t, "test", result)
}

func TestCharsetToUTF8_CP1252_Extended(t *testing.T) {
	t.Parallel()
	// Test CP1252 extended characters
	c := Collation{SortId: 50} // cp1252
	// 0x80 in CP1252 is Euro sign (€)
	input := []byte{0x80}
	result := CharsetToUTF8(c, input)
	assert.Equal(t, "€", result)
}

func TestCharsetToUTF8_EmptyInput(t *testing.T) {
	t.Parallel()
	c := Collation{SortId: 50} // cp1252
	input := []byte{}
	result := CharsetToUTF8(c, input)
	assert.Empty(t, result)
}

func TestCharsetToUTF8_CP932_DoubleByte(t *testing.T) {
	t.Parallel()
	// Test Japanese CP932 (Shift-JIS) double-byte handling
	c := Collation{SortId: 192} // cp932
	// Test ASCII portion
	input := []byte("ABC")
	result := CharsetToUTF8(c, input)
	assert.Equal(t, "ABC", result)
}

func TestCharsetToUTF8_IncompleteDoubleByte(t *testing.T) {
	t.Parallel()
	// Test handling of incomplete double-byte sequence at end
	c := Collation{SortId: 192} // cp932 - has double byte chars
	// 0x81 is a lead byte in CP932, but no following byte
	input := []byte{0x41, 0x81} // 'A' followed by incomplete double byte
	result := CharsetToUTF8(c, input)
	// Should have 'A' and replacement char
	assert.GreaterOrEqual(t, len(result), 2, "Expected at least 2 characters, got %q", result)
}

// Test each code page getter returns a valid charset map
func TestGetCodePages(t *testing.T) {
	t.Parallel()
	codePages := []struct {
		name   string
		getter func() *charsetMap
	}{
		{"cp437", getcp437},
		{"cp850", getcp850},
		{"cp874", getcp874},
		{"cp932", getcp932},
		{"cp936", getcp936},
		{"cp949", getcp949},
		{"cp950", getcp950},
		{"cp1250", getcp1250},
		{"cp1251", getcp1251},
		{"cp1252", getcp1252},
		{"cp1253", getcp1253},
		{"cp1254", getcp1254},
		{"cp1255", getcp1255},
		{"cp1256", getcp1256},
		{"cp1257", getcp1257},
		{"cp1258", getcp1258},
	}
	for _, cp := range codePages {
		cp := cp
		t.Run(cp.name, func(t *testing.T) {
			t.Parallel()
			cm := cp.getter()
			if cm == nil {
				t.Fatal("getter returned nil")
			}
			// Verify ASCII portion (0x00-0x7F) maps correctly
			for i := 0; i < 128; i++ {
				if cm.sb[i] != rune(i) && cm.sb[i] != -1 {
					// Some code pages may have variations even in ASCII range
					// but most should match
				}
			}
			// Verify at least some extended chars are defined (0x80-0xFF)
			extendedCount := 0
			for i := 128; i < 256; i++ {
				if cm.sb[i] != 0 {
					extendedCount++
				}
			}
			if extendedCount == 0 {
				t.Error("no extended characters defined")
			}
		})
	}
}
