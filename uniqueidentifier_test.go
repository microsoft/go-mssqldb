package mssql

import (
	"bytes"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUniqueIdentifierScanNull(t *testing.T) {
	t.Parallel()

	sut := UniqueIdentifier{0x01}
	scanErr := sut.Scan(nil) // NULL in the DB
	if scanErr == nil {
		t.Fatal("expected an error for Scan(nil)")
	}
}

func TestUniqueIdentifierScanBytes(t *testing.T) {
	t.Parallel()
	dbUUID := UniqueIdentifier{0x67, 0x45, 0x23, 0x01,
		0xAB, 0x89,
		0xEF, 0xCD,
		0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF,
	}
	uuid := UniqueIdentifier{0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF, 0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF}

	var sut UniqueIdentifier
	scanErr := sut.Scan(dbUUID[:])
	if scanErr != nil {
		t.Fatal(scanErr)
	}
	assert.Equal(t, uuid, sut, "bytes not swapped correctly")
}

func TestUniqueIdentifierScanString(t *testing.T) {
	t.Parallel()
	uuid := UniqueIdentifier{0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF, 0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF}

	var sut UniqueIdentifier
	scanErr := sut.Scan(uuid.String())
	if scanErr != nil {
		t.Fatal(scanErr)
	}
	assert.Equal(t, uuid, sut, "string not scanned correctly")
}

func TestUniqueIdentifierScanUnexpectedType(t *testing.T) {
	t.Parallel()
	var sut UniqueIdentifier
	scanErr := sut.Scan(int(1))
	if scanErr == nil {
		t.Fatal(scanErr)
	}
}

func TestUniqueIdentifierValue(t *testing.T) {
	t.Parallel()
	dbUUID := UniqueIdentifier{0x67, 0x45, 0x23, 0x01,
		0xAB, 0x89,
		0xEF, 0xCD,
		0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF,
	}

	uuid := UniqueIdentifier{0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF, 0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF}

	sut := uuid
	v, valueErr := sut.Value()
	if valueErr != nil {
		t.Fatal(valueErr)
	}

	b, ok := v.([]byte)
	if !ok {
		t.Fatalf("(%T) is not []byte", v)
	}

	assert.True(t, bytes.Equal(b, dbUUID[:]), "got %q; want %q", b, dbUUID)
}

func TestUniqueIdentifierString(t *testing.T) {
	t.Parallel()
	sut := UniqueIdentifier{0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF, 0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF}
	expected := "01234567-89AB-CDEF-0123-456789ABCDEF"
	assert.Equal(t, expected, sut.String(), "sut.String()")
}

func TestUniqueIdentifierMarshalText(t *testing.T) {
	t.Parallel()
	sut := UniqueIdentifier{0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF, 0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF}
	expected := []byte{48, 49, 50, 51, 52, 53, 54, 55, 45, 56, 57, 65, 66, 45, 67, 68, 69, 70, 45, 48, 49, 50, 51, 45, 52, 53, 54, 55, 56, 57, 65, 66, 67, 68, 69, 70}
	text, _ := sut.MarshalText()
	assert.True(t, reflect.DeepEqual(text, expected), "sut.MarshalText() = %v; want %v", text, expected)
}

func TestUniqueIdentifierUnmarshalJSON(t *testing.T) {
	t.Parallel()
	input := []byte("01234567-89AB-CDEF-0123-456789ABCDEF")
	var u UniqueIdentifier

	err := u.UnmarshalJSON(input)
	if err != nil {
		t.Fatal(err)
	}
	expected := UniqueIdentifier{0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF, 0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF}
	assert.Equal(t, expected, u, "u.UnmarshalJSON()")
}

func TestUniqueIdentifierUnmarshalJSONInvalid(t *testing.T) {
	t.Parallel()
	var u UniqueIdentifier
	// Invalid hex characters should fail
	err := u.UnmarshalJSON([]byte("ZZZZZZZZ-ZZZZ-ZZZZ-ZZZZ-ZZZZZZZZZZZZ"))
	assert.Error(t, err, "expected error for invalid hex characters")
}

func TestUniqueIdentifierJSONUnmarshalLength(t *testing.T) {
	t.Parallel()

	uuid := UniqueIdentifier{0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF, 0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF}
	tests := []struct {
		name    string
		input   string
		want    UniqueIdentifier
		wantErr bool
	}{
		{name: "canonical", input: `"01234567-89AB-CDEF-0123-456789ABCDEF"`, want: uuid},
		{name: "compact", input: `"0123456789ABCDEF0123456789ABCDEF"`, want: uuid},
		{name: "lowercase", input: `"01234567-89ab-cdef-0123-456789abcdef"`, want: uuid},
		{name: "zero UUID", input: `"00000000-0000-0000-0000-000000000000"`},
		{name: "empty", input: `""`, wantErr: true},
		{name: "one byte", input: `"01"`, wantErr: true},
		{name: "fifteen bytes", input: `"01234567-89AB-CDEF-0123-456789ABCD"`, wantErr: true},
		{name: "seventeen bytes", input: `"01234567-89AB-CDEF-0123-456789ABCDEF00"`, wantErr: true},
		{name: "thirty-two bytes", input: `"0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF"`, wantErr: true},
		{name: "odd hex length", input: `"0123456789ABCDEF0123456789ABCDE"`, wantErr: true},
		{name: "invalid hex", input: `"01234567-89AB-CDEF-0123-456789ABCDEZ"`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			initial := UniqueIdentifier{0xFF, 0xEE, 0xDD, 0xCC}
			u := initial
			err := json.Unmarshal([]byte(tt.input), &u)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Equal(t, initial, u, "invalid input must not change the UUID")
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, u)
		})
	}
}

func TestUniqueIdentifierScanInvalidByteLength(t *testing.T) {
	t.Parallel()
	var u UniqueIdentifier
	// Wrong byte length should fail
	err := u.Scan([]byte{0x01, 0x02, 0x03}) // Only 3 bytes, need 16
	assert.Error(t, err, "expected error for invalid byte length")
}

func TestUniqueIdentifierScanInvalidStringLength(t *testing.T) {
	t.Parallel()
	var u UniqueIdentifier
	// Wrong string length should fail
	err := u.Scan("too-short")
	assert.Error(t, err, "expected error for invalid string length")
}

var _ fmt.Stringer = UniqueIdentifier{}
var _ sql.Scanner = &UniqueIdentifier{}
var _ driver.Valuer = UniqueIdentifier{}
