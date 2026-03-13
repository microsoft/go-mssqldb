//go:build go1.9
// +build go1.9

package mssql

import (
	"database/sql/driver"
	"fmt"
	"time"

	"github.com/golang-sql/civil"
)

// NullDate represents a civil.Date that may be null.
// NullDate implements the Scanner interface so it can be used as a scan destination.
type NullDate struct {
	Date  civil.Date
	Valid bool
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

// NullDateTime represents a civil.DateTime that may be null.
// NullDateTime implements the Scanner interface so it can be used as a scan destination.
type NullDateTime struct {
	DateTime civil.DateTime
	Valid    bool
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

// NullTime represents a civil.Time that may be null.
// NullTime implements the Scanner interface so it can be used as a scan destination.
type NullTime struct {
	Time  civil.Time
	Valid bool
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
