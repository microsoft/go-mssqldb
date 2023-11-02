package mssql

import (
	"database/sql/driver"
)

type NullableUniqueIdentifier UniqueIdentifier

func (n *NullableUniqueIdentifier) Scan(v interface{}) error {
	u := UniqueIdentifier(*n)
	if v == nil {
		*n = [16]byte{0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0}
		return nil
	}
	err := u.Scan(v)
	*n = NullableUniqueIdentifier(u)
	return err
}

func (n NullableUniqueIdentifier) Value() (driver.Value, error) {
	return UniqueIdentifier(n).Value()
}

func (n NullableUniqueIdentifier) String() string {
	return UniqueIdentifier(n).String()
}

func (n NullableUniqueIdentifier) MarshalText() (text []byte, err error) {
	return UniqueIdentifier(n).MarshalText()
}

func (n *NullableUniqueIdentifier) UnmarshalJSON(b []byte) error {
	u := UniqueIdentifier(*n)
	err := u.UnmarshalJSON(b)
	*n = NullableUniqueIdentifier(u)
	return err
}
