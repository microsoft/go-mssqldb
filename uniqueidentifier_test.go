package mssql

import (
	"bytes"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"reflect"
	"testing"
)

func TestUniqueIdentifierScanBytes(t *testing.T) {
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
	if sut != uuid {
		t.Errorf("bytes not swapped correctly: got %q; want %q", sut, uuid)
	}
}

func TestUniqueIdentifierScanString(t *testing.T) {
	uuid := UniqueIdentifier{0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF, 0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF}

	var sut UniqueIdentifier
	scanErr := sut.Scan(uuid.String())
	if scanErr != nil {
		t.Fatal(scanErr)
	}
	if sut != uuid {
		t.Errorf("string not scanned correctly: got %q; want %q", sut, uuid)
	}
}

func TestUniqueIdentifierScanUnexpectedType(t *testing.T) {
	var sut UniqueIdentifier
	scanErr := sut.Scan(int(1))
	if scanErr == nil {
		t.Fatal(scanErr)
	}
}

func TestUniqueIdentifierValue(t *testing.T) {
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

	if !bytes.Equal(b, dbUUID[:]) {
		t.Errorf("got %q; want %q", b, dbUUID)
	}
}

func TestUniqueIdentifierString(t *testing.T) {
	sut := UniqueIdentifier{0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF, 0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF}
	expected := "01234567-89AB-CDEF-0123-456789ABCDEF"
	if actual := sut.String(); actual != expected {
		t.Errorf("sut.String() = %s; want %s", sut, expected)
	}
}

func TestUniqueIdentifierMarshalText(t *testing.T) {
	sut := UniqueIdentifier{0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF, 0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF}
	expected := []byte{48, 49, 50, 51, 52, 53, 54, 55, 45, 56, 57, 65, 66, 45, 67, 68, 69, 70, 45, 48, 49, 50, 51, 45, 52, 53, 54, 55, 56, 57, 65, 66, 67, 68, 69, 70}
	text, _ := sut.MarshalText()
	if actual := text; !reflect.DeepEqual(actual, expected) {
		t.Errorf("sut.MarshalText() = %v; want %v", actual, expected)
	}
}

func TestUniqueIdentifierUnmarshalJSON(t *testing.T) {
	input := []byte("01234567-89AB-CDEF-0123-456789ABCDEF")
	var u UniqueIdentifier

	err := u.UnmarshalJSON(input)
	if err != nil {
		t.Fatal(err)
	}
	expected := UniqueIdentifier{0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF, 0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF}
	if u != expected {
		t.Errorf("u.UnmarshalJSON() = %v; want %v", u, expected)
	}
}

var _ fmt.Stringer = UniqueIdentifier{}
var _ sql.Scanner = &UniqueIdentifier{}
var _ driver.Valuer = UniqueIdentifier{}
