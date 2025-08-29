package mssql

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/golang-sql/civil"
)

// NullDate represents a civil.Date that may be null.
// NullDate implements the Scanner interface so it can be used as a scan destination,
// similar to sql.NullString.
type NullDate struct {
	Date  civil.Date
	Valid bool // Valid is true if Date is not NULL
}

// Scan implements the Scanner interface.
func (n *NullDate) Scan(value interface{}) error {
	if value == nil {
		n.Date, n.Valid = civil.Date{}, false
		return nil
	}
	n.Valid = true
	switch v := value.(type) {
	case time.Time:
		n.Date = civil.DateOf(v)
		return nil
	default:
		n.Valid = false
		return fmt.Errorf("cannot scan %T into NullDate", value)
	}
}

// Value implements the driver Valuer interface.
func (n NullDate) Value() (driver.Value, error) {
	if !n.Valid {
		return nil, nil
	}
	return n.Date, nil
}

// String returns the string representation of the date or "NULL".
func (n NullDate) String() string {
	if !n.Valid {
		return "NULL"
	}
	return n.Date.String()
}

// MarshalText implements the encoding.TextMarshaler interface.
func (n NullDate) MarshalText() ([]byte, error) {
	if !n.Valid {
		return []byte("null"), nil
	}
	return n.Date.MarshalText()
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (n *NullDate) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		n.Date, n.Valid = civil.Date{}, false
		return nil
	}
	err := json.Unmarshal(b, &n.Date)
	n.Valid = err == nil
	return err
}

// MarshalJSON implements the json.Marshaler interface.
func (n NullDate) MarshalJSON() ([]byte, error) {
	if !n.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(n.Date)
}

// NullDateTime represents a civil.DateTime that may be null.
// NullDateTime implements the Scanner interface so it can be used as a scan destination,
// similar to sql.NullString.
type NullDateTime struct {
	DateTime civil.DateTime
	Valid    bool // Valid is true if DateTime is not NULL
}

// Scan implements the Scanner interface.
func (n *NullDateTime) Scan(value interface{}) error {
	if value == nil {
		n.DateTime, n.Valid = civil.DateTime{}, false
		return nil
	}
	n.Valid = true
	switch v := value.(type) {
	case time.Time:
		n.DateTime = civil.DateTimeOf(v)
		return nil
	default:
		n.Valid = false
		return fmt.Errorf("cannot scan %T into NullDateTime", value)
	}
}

// Value implements the driver Valuer interface.
func (n NullDateTime) Value() (driver.Value, error) {
	if !n.Valid {
		return nil, nil
	}
	return n.DateTime, nil
}

// String returns the string representation of the datetime or "NULL".
func (n NullDateTime) String() string {
	if !n.Valid {
		return "NULL"
	}
	return n.DateTime.String()
}

// MarshalText implements the encoding.TextMarshaler interface.
func (n NullDateTime) MarshalText() ([]byte, error) {
	if !n.Valid {
		return []byte("null"), nil
	}
	return n.DateTime.MarshalText()
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (n *NullDateTime) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		n.DateTime, n.Valid = civil.DateTime{}, false
		return nil
	}
	err := json.Unmarshal(b, &n.DateTime)
	n.Valid = err == nil
	return err
}

// MarshalJSON implements the json.Marshaler interface.
func (n NullDateTime) MarshalJSON() ([]byte, error) {
	if !n.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(n.DateTime)
}

// NullTime represents a civil.Time that may be null.
// NullTime implements the Scanner interface so it can be used as a scan destination,
// similar to sql.NullString.
type NullTime struct {
	Time  civil.Time
	Valid bool // Valid is true if Time is not NULL
}

// Scan implements the Scanner interface.
func (n *NullTime) Scan(value interface{}) error {
	if value == nil {
		n.Time, n.Valid = civil.Time{}, false
		return nil
	}
	n.Valid = true
	switch v := value.(type) {
	case time.Time:
		n.Time = civil.TimeOf(v)
		return nil
	default:
		n.Valid = false
		return fmt.Errorf("cannot scan %T into NullTime", value)
	}
}

// Value implements the driver Valuer interface.
func (n NullTime) Value() (driver.Value, error) {
	if !n.Valid {
		return nil, nil
	}
	return n.Time, nil
}

// String returns the string representation of the time or "NULL".
func (n NullTime) String() string {
	if !n.Valid {
		return "NULL"
	}
	return n.Time.String()
}

// MarshalText implements the encoding.TextMarshaler interface.
func (n NullTime) MarshalText() ([]byte, error) {
	if !n.Valid {
		return []byte("null"), nil
	}
	return n.Time.MarshalText()
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (n *NullTime) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		n.Time, n.Valid = civil.Time{}, false
		return nil
	}
	err := json.Unmarshal(b, &n.Time)
	n.Valid = err == nil
	return err
}

// MarshalJSON implements the json.Marshaler interface.
func (n NullTime) MarshalJSON() ([]byte, error) {
	if !n.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(n.Time)
}
