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

func TestNullableUniqueIdentifierScanNull(t *testing.T) {
	t.Parallel()
	nullUUID := NullUniqueIdentifier{
		UUID:  [16]byte{0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
		Valid: false,
	}

	sut := NullUniqueIdentifier{
		UUID:  [16]byte{0x1},
		Valid: true,
	}
	scanErr := sut.Scan(nil) // NULL in the DB
	if scanErr != nil {
		t.Fatal("NullUniqueIdentifier should not error out on Scan(nil)")
	}
	assert.Equal(t, nullUUID, sut, "bytes not swapped correctly")
}

func TestNullableUniqueIdentifierScanBytes(t *testing.T) {
	t.Parallel()
	dbUUID := [16]byte{0x67, 0x45, 0x23, 0x01, 0xAB, 0x89, 0xEF, 0xCD, 0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF}
	uuid := NullUniqueIdentifier{
		UUID:  [16]byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF, 0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF},
		Valid: true,
	}

	var sut NullUniqueIdentifier
	scanErr := sut.Scan(dbUUID[:])
	if scanErr != nil {
		t.Fatal(scanErr)
	}
	assert.Equal(t, uuid, sut, "bytes not swapped correctly")
}

func TestNullableUniqueIdentifierScanString(t *testing.T) {
	t.Parallel()
	uuid := NullUniqueIdentifier{
		UUID:  [16]byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF, 0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF},
		Valid: true,
	}

	var sut NullUniqueIdentifier
	scanErr := sut.Scan(uuid.String())
	if scanErr != nil {
		t.Fatal(scanErr)
	}
	assert.Equal(t, uuid, sut, "string not scanned correctly")
}

func TestNullableUniqueIdentifierScanUnexpectedType(t *testing.T) {
	t.Parallel()
	var sut NullUniqueIdentifier
	scanErr := sut.Scan(int(1))
	if scanErr == nil {
		t.Fatal(scanErr)
	}
}

func TestNullableUniqueIdentifierValue(t *testing.T) {
	t.Parallel()
	dbUUID := [16]byte{0x67, 0x45, 0x23, 0x01, 0xAB, 0x89, 0xEF, 0xCD, 0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF}

	uuid := NullUniqueIdentifier{
		UUID:  [16]byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF, 0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF},
		Valid: true,
	}

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

func TestNullableUniqueIdentifierValueNull(t *testing.T) {
	t.Parallel()
	uuid := NullUniqueIdentifier{
		UUID:  [16]byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF, 0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF},
		Valid: false,
	}

	sut := uuid
	v, valueErr := sut.Value()
	assert.NoError(t, valueErr, "unexpected error for invalid uuid")
	assert.Nil(t, v, "expected nil value for invalid uuid")
}

func TestNullableUniqueIdentifierString(t *testing.T) {
	t.Parallel()
	sut := NullUniqueIdentifier{
		UUID:  [16]byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF, 0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF},
		Valid: true,
	}
	expected := "01234567-89AB-CDEF-0123-456789ABCDEF"
	assert.Equal(t, expected, sut.String(), "sut.String()")
}

func TestNullableUniqueIdentifierStringNull(t *testing.T) {
	t.Parallel()
	sut := NullUniqueIdentifier{
		UUID:  [16]byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF, 0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF},
		Valid: false,
	}
	expected := "NULL"
	assert.Equal(t, expected, sut.String(), "sut.String()")
}

func TestNullableUniqueIdentifierMarshalText(t *testing.T) {
	t.Parallel()
	sut := NullUniqueIdentifier{
		UUID:  [16]byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF, 0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF},
		Valid: true,
	}
	expected := []byte{48, 49, 50, 51, 52, 53, 54, 55, 45, 56, 57, 65, 66, 45, 67, 68, 69, 70, 45, 48, 49, 50, 51, 45, 52, 53, 54, 55, 56, 57, 65, 66, 67, 68, 69, 70}
	text, marshalErr := sut.MarshalText()
	assert.NoError(t, marshalErr, "unexpected error while marshalling")
	assert.True(t, reflect.DeepEqual(text, expected), "sut.MarshalText() = %v; want %v", text, expected)
}

func TestNullableUniqueIdentifierMarshalTextNull(t *testing.T) {
	t.Parallel()
	sut := NullUniqueIdentifier{
		UUID:  [16]byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF, 0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF},
		Valid: false,
	}
	expected := []byte("null")
	text, marshalErr := sut.MarshalText()
	assert.NoError(t, marshalErr, "unexpected error while marshalling")
	assert.True(t, reflect.DeepEqual(text, expected), "sut.MarshalText() = %v; want %v", text, expected)
}

func TestNullableUniqueIdentifierUnmarshalJSON(t *testing.T) {
	t.Parallel()
	input := []byte("01234567-89AB-CDEF-0123-456789ABCDEF")
	var u NullUniqueIdentifier

	err := u.UnmarshalJSON(input)
	if err != nil {
		t.Fatal(err)
	}
	expected := NullUniqueIdentifier{
		UUID:  [16]byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF, 0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF},
		Valid: true,
	}
	assert.Equal(t, expected, u, "u.UnmarshalJSON()")
}

func TestNullableUniqueIdentifierUnmarshalJSONNull(t *testing.T) {
	t.Parallel()
	u := NullUniqueIdentifier{
		UUID:  [16]byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF, 0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF},
		Valid: true,
	}

	err := u.UnmarshalJSON([]byte("null"))
	if err != nil {
		t.Fatal(err)
	}
	expected := NullUniqueIdentifier{
		UUID:  [16]byte{0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
		Valid: false,
	}
	assert.Equal(t, expected, u, "u.UnmarshalJSON()")
}

func TestNullableUniqueIdentifierMarshalJSONNull(t *testing.T) {
	t.Parallel()
	nullUUID := NullUniqueIdentifier{
		UUID:  [16]byte{0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
		Valid: false,
	}

	got, err := nullUUID.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x6e, 0x75, 0x6c, 0x6c} // null = %x6e.75.6c.6c
	assert.True(t, reflect.DeepEqual(got, want), "got %v; want %v", got, want)
}

func TestNullableUniqueIdentifierMarshalJSONValid(t *testing.T) {
	t.Parallel()
	validUUID := NullUniqueIdentifier{
		UUID:  [16]byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF, 0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF},
		Valid: true,
	}

	got, err := validUUID.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	// Should be base64 encoded since UniqueIdentifier is a [16]byte array
	assert.NotEmpty(t, got, "expected non-empty JSON output for valid UUID")
}

func TestNullableUniqueIdentifierJSONMarshalNull(t *testing.T) {
	t.Parallel()
	nullUUID := NullUniqueIdentifier{
		UUID:  [16]byte{0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
		Valid: false,
	}

	got, err := json.Marshal(nullUUID)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x6e, 0x75, 0x6c, 0x6c}
	assert.True(t, reflect.DeepEqual(got, want), "got %v; want %v", got, want)
}

var (
	_ fmt.Stringer  = NullUniqueIdentifier{}
	_ sql.Scanner   = &NullUniqueIdentifier{}
	_ driver.Valuer = NullUniqueIdentifier{}
)
