package mssql

import (
	"database/sql/driver"
)

type NullUniqueIdentifier UniqueIdentifier

func (n *NullUniqueIdentifier) Scan(v interface{}) error {
	u := UniqueIdentifier(*n)
	if v == nil {
		*n = [16]byte{0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0}
		return nil
	}
	err := u.Scan(v)
	*n = NullUniqueIdentifier(u)
	return err
}

func (n NullUniqueIdentifier) Value() (driver.Value, error) {
	return UniqueIdentifier(n).Value()
}

func (n NullUniqueIdentifier) String() string {
	return UniqueIdentifier(n).String()
}

func (n NullUniqueIdentifier) MarshalText() (text []byte, err error) {
	return UniqueIdentifier(n).MarshalText()
}

func (n *NullUniqueIdentifier) UnmarshalJSON(b []byte) error {
	u := UniqueIdentifier(*n)
	err := u.UnmarshalJSON(b)
	*n = NullUniqueIdentifier(u)
	return err
}
