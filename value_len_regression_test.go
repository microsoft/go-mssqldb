package mssql

import (
	"testing"

	"github.com/microsoft/go-mssqldb/msdsn"
	"github.com/stretchr/testify/assert"
)

// TestReadByteLenType_OversizedValuePanics is a regression test for issue #422:
// readByteLenTypeWithEncoding sliced ti.Buffer[:size] using the untrusted
// per-value length byte without checking it against the column buffer, so a size
// larger than ti.Size produced a runtime "slice bounds out of range" panic
// instead of a recoverable StreamError.
func TestReadByteLenType_OversizedValuePanics(t *testing.T) {
	ti := typeInfo{
		TypeId: typeIntN,
		Size:   4,
		Buffer: make([]byte, 4),
	}
	// size byte = 48, larger than the 4-byte column buffer.
	stream := append([]byte{48}, make([]byte, 48)...)
	err := recoverErr(func() {
		readByteLenTypeWithEncoding(&ti, bufFromBytes(stream), nil, msdsn.EncodeParameters{})
	})
	if err == nil {
		t.Fatalf("expected a stream error, got none")
	}
	assertStreamError(t, err)
	assert.Contains(t, err.Error(), "exceeds column buffer")
}

// TestReadShortLenType_OversizedValuePanics is a regression test for issue #422:
// readShortLenType sliced ti.Buffer[:size] using the untrusted per-value uint16
// length without checking it against the column buffer.
func TestReadShortLenType_OversizedValuePanics(t *testing.T) {
	ti := typeInfo{
		TypeId: typeBigVarChar,
		Size:   4,
		Buffer: make([]byte, 4),
	}
	// size uint16 = 48 (little endian), larger than the 4-byte column buffer.
	stream := append([]byte{48, 0}, make([]byte, 48)...)
	err := recoverErr(func() {
		readShortLenType(&ti, bufFromBytes(stream), nil, msdsn.EncodeParameters{})
	})
	if err == nil {
		t.Fatalf("expected a stream error, got none")
	}
	assertStreamError(t, err)
	assert.Contains(t, err.Error(), "exceeds column buffer")
}

// TestTemporalDecoders_UndersizedBufferPanics is a regression test for issue
// #423: decodeTime, decodeDateTime2 and decodeDateTimeOffset computed a
// time-portion length from len(buf) without validating that the buffer held the
// minimum number of bytes for the scale, so an undersized buffer underflowed and
// produced a runtime "slice bounds out of range" panic.
func TestTemporalDecoders_UndersizedBufferPanics(t *testing.T) {
	loc := msdsn.EncodeParameters{}.GetTimezone()

	t.Run("decodeTime short buffer", func(t *testing.T) {
		err := recoverErr(func() {
			// scale 5 needs 5 bytes; give 2.
			decodeTime(5, make([]byte, 2), loc)
		})
		if err == nil {
			t.Fatalf("expected a stream error, got none")
		}
		assertStreamError(t, err)
		assert.Contains(t, err.Error(), "invalid")
	})

	t.Run("decodeTime long buffer", func(t *testing.T) {
		err := recoverErr(func() {
			// scale 0 needs exactly 3 bytes; give 5 (extra bytes must be rejected).
			decodeTime(0, make([]byte, 5), loc)
		})
		if err == nil {
			t.Fatalf("expected a stream error, got none")
		}
		assertStreamError(t, err)
		assert.Contains(t, err.Error(), "invalid")
	})

	t.Run("decodeDateTime2 short buffer", func(t *testing.T) {
		err := recoverErr(func() {
			// scale 0 needs 3+3 bytes; give 2.
			decodeDateTime2(0, make([]byte, 2), loc)
		})
		if err == nil {
			t.Fatalf("expected a stream error, got none")
		}
		assertStreamError(t, err)
		assert.Contains(t, err.Error(), "invalid")
	})

	t.Run("decodeDateTime2 long buffer", func(t *testing.T) {
		err := recoverErr(func() {
			// scale 0 needs exactly 6 bytes; give 8.
			decodeDateTime2(0, make([]byte, 8), loc)
		})
		if err == nil {
			t.Fatalf("expected a stream error, got none")
		}
		assertStreamError(t, err)
		assert.Contains(t, err.Error(), "invalid")
	})

	t.Run("decodeDateTimeOffset short buffer", func(t *testing.T) {
		err := recoverErr(func() {
			// scale 0 needs 3+3+2 bytes; give 4 (triggers negative timesize).
			decodeDateTimeOffset(0, make([]byte, 4))
		})
		if err == nil {
			t.Fatalf("expected a stream error, got none")
		}
		assertStreamError(t, err)
		assert.Contains(t, err.Error(), "invalid")
	})

	t.Run("decodeDateTimeOffset long buffer", func(t *testing.T) {
		err := recoverErr(func() {
			// scale 0 needs exactly 8 bytes; give 10.
			decodeDateTimeOffset(0, make([]byte, 10))
		})
		if err == nil {
			t.Fatalf("expected a stream error, got none")
		}
		assertStreamError(t, err)
		assert.Contains(t, err.Error(), "invalid")
	})

	t.Run("invalid scale", func(t *testing.T) {
		for name, fn := range map[string]func(){
			"decodeTime":           func() { decodeTime(8, make([]byte, 8), loc) },
			"decodeDateTime2":      func() { decodeDateTime2(8, make([]byte, 8), loc) },
			"decodeDateTimeOffset": func() { decodeDateTimeOffset(8, make([]byte, 8)) },
		} {
			t.Run(name, func(t *testing.T) {
				err := recoverErr(fn)
				if err == nil {
					t.Fatalf("expected a stream error, got none")
				}
				assertStreamError(t, err)
				assert.Contains(t, err.Error(), "invalid scale")
			})
		}
	})
}

// TestDecodeFixedLen_UndersizedBufferPanics is a regression test for issues #422
// and #423 reached via the sql_variant path: fixed-length value decoders indexed
// their buffer at a constant offset without validating length, and decodeDecimal
// looped past the four-word decimal integer array for an oversized buffer.
func TestDecodeFixedLen_UndersizedBufferPanics(t *testing.T) {
	loc := msdsn.EncodeParameters{}.GetTimezone()
	cases := map[string]struct {
		fn      func()
		wantSub string
	}{
		"decodeMoney short":        {func() { decodeMoney(make([]byte, 4)) }, "invalid"},
		"decodeMoney long":         {func() { decodeMoney(make([]byte, 9)) }, "invalid"},
		"decodeMoney4 short":       {func() { decodeMoney4(make([]byte, 2)) }, "invalid"},
		"decodeMoney4 long":        {func() { decodeMoney4(make([]byte, 5)) }, "invalid"},
		"decodeDateTim4 short":     {func() { decodeDateTim4(make([]byte, 2), loc) }, "invalid"},
		"decodeDateTim4 long":      {func() { decodeDateTim4(make([]byte, 5), loc) }, "invalid"},
		"decodeDateTime short":     {func() { decodeDateTime(make([]byte, 4), loc) }, "invalid"},
		"decodeDateTime long":      {func() { decodeDateTime(make([]byte, 9), loc) }, "invalid"},
		"decodeDate short":         {func() { decodeDate(make([]byte, 2), loc) }, "invalid"},
		"decodeDate long":          {func() { decodeDate(make([]byte, 4), loc) }, "invalid"},
		"decodeDecimal empty":      {func() { decodeDecimal(1, 0, make([]byte, 0)) }, "invalid"},
		"decodeDecimal oversz":     {func() { decodeDecimal(1, 0, make([]byte, 32)) }, "invalid"},
		"decodeDecimal misaligned": {func() { decodeDecimal(1, 0, make([]byte, 6)) }, "invalid"},
	}
	for name, tc := range cases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			err := recoverErr(tc.fn)
			if err == nil {
				t.Fatalf("expected a stream error, got none")
			}
			assertStreamError(t, err)
			assert.Contains(t, err.Error(), tc.wantSub)
		})
	}
}
