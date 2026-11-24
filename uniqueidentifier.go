package mssql

import (
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

type UniqueIdentifier [16]byte

func (u *UniqueIdentifier) Scan(v interface{}) error {
	reverse := func(b []byte) {
		for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
			b[i], b[j] = b[j], b[i]
		}
	}

	switch vt := v.(type) {
	case []byte:
		if len(vt) != 16 {
			return errors.New("mssql: invalid UniqueIdentifier length")
		}

		var raw UniqueIdentifier

		copy(raw[:], vt)

		reverse(raw[0:4])
		reverse(raw[4:6])
		reverse(raw[6:8])
		*u = raw

		return nil
	case string:
		if len(vt) != 36 {
			return errors.New("mssql: invalid UniqueIdentifier string length")
		}

		return u.parseString(vt)
	default:
		return fmt.Errorf("mssql: cannot convert %T to UniqueIdentifier", v)
	}
}

func (u UniqueIdentifier) Value() (driver.Value, error) {
	reverse := func(b []byte) {
		for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
			b[i], b[j] = b[j], b[i]
		}
	}

	raw := make([]byte, len(u))
	copy(raw, u[:])

	reverse(raw[0:4])
	reverse(raw[4:6])
	reverse(raw[6:8])

	return raw, nil
}

func (u UniqueIdentifier) String() string {
	return fmt.Sprintf("%X-%X-%X-%X-%X", u[0:4], u[4:6], u[6:8], u[8:10], u[10:])
}

// MarshalText converts Uniqueidentifier to bytes corresponding to the stringified hexadecimal representation of the Uniqueidentifier
// e.g., "AAAAAAAA-AAAA-AAAA-AAAA-AAAAAAAAAAAA" -> [65 65 65 65 65 65 65 65 45 65 65 65 65 45 65 65 65 65 45 65 65 65 65 65 65 65 65 65 65 65 65]
func (u UniqueIdentifier) MarshalText() (text []byte, err error) {
	text = []byte(u.String())
	return
}

// Unmarshals a string representation of a UniqueIndentifier to bytes
// "01234567-89AB-CDEF-0123-456789ABCDEF" -> [48, 49, 50, 51, 52, 53, 54, 55, 45, 56, 57, 65, 66, 45, 67, 68, 69, 70, 45, 48, 49, 50, 51, 45, 52, 53, 54, 55, 56, 57, 65, 66, 67, 68, 69, 70]
func (u *UniqueIdentifier) UnmarshalJSON(b []byte) error {
	// remove quotes
	return u.parseString(strings.Trim(string(b), `"`))
}

// parseString decodes s, the hex digits of a UniqueIdentifier optionally
// separated by dashes, into u. u is left unchanged if s does not hold exactly
// 16 bytes of hex.
func (u *UniqueIdentifier) parseString(s string) error {
	h := strings.ReplaceAll(s, "-", "")
	if len(h) != hex.EncodedLen(len(u)) {
		return fmt.Errorf("mssql: invalid UniqueIdentifier %q", s)
	}
	var raw UniqueIdentifier
	if _, err := hex.Decode(raw[:], []byte(h)); err != nil {
		return err
	}
	*u = raw
	return nil
}
