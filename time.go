package mssql

import (
	"database/sql"
//	"database/sql/driver"
//	"time"

	"github.com/golang-sql/civil"
)

type Date civil.Date

// Scan implements the [Scanner] interface.
func (d *Date) Scan(value any) error {
	t := &sql.NullTime{}

	err := t.Scan(value)
	if err != nil {
		return err
	}

	d.Year = t.Time.Year()
	d.Month = t.Time.Month()
	d.Day = t.Time.Day()

	return nil
}

// Value implements the [Valuer] interface
//func (d Date) Value() (driver.Value, error) {
//	return civil.Date(d).In(time.UTC), nil
//}

type DateTime civil.DateTime

// Scan implements the [Scanner] interface.
func (dt *DateTime) Scan(value any) error {
	t := &sql.NullTime{}

	err := t.Scan(value)
	if err != nil {
		return err
	}

	dt.Date.Year = t.Time.Year()
	dt.Date.Month = t.Time.Month()
	dt.Date.Day = t.Time.Day()

	dt.Time.Hour = t.Time.Hour()
	dt.Time.Minute = t.Time.Minute()
	dt.Time.Second = t.Time.Second()
	dt.Time.Nanosecond = t.Time.Nanosecond()

	return nil
}

type DateTime2 civil.DateTime

// Scan implements the [Scanner] interface.
func (dt2 *DateTime2) Scan(value any) error {
	t := &sql.NullTime{}

	err := t.Scan(value)
	if err != nil {
		return err
	}

	dt2.Date.Year = t.Time.Year()
	dt2.Date.Month = t.Time.Month()
	dt2.Date.Day = t.Time.Day()

	dt2.Time.Hour = t.Time.Hour()
	dt2.Time.Minute = t.Time.Minute()
	dt2.Time.Second = t.Time.Second()
	dt2.Time.Nanosecond = t.Time.Nanosecond()

	return nil
}

type Time civil.Time

// Scan implements the [Scanner] interface.
func (tt *Time) Scan(value any) error {
	t := &sql.NullTime{}

	err := t.Scan(value)
	if err != nil {
		return err
	}

	tt.Hour = t.Time.Hour()
	tt.Minute = t.Time.Minute()
	tt.Second = t.Time.Second()
	tt.Nanosecond = t.Time.Nanosecond()

	return nil
}

type NullDate struct {
	Date  Date
	Valid bool
}

// Scan implements the [Scanner] interface.
func (n *NullDate) Scan(value any) error {
	t := &sql.NullTime{}

	err := t.Scan(value)
	if err != nil {
		return err
	}

	if !t.Valid {
		n.Valid = false

		return nil
	}

	n.Valid = true

	n.Date.Year = t.Time.Year()
	n.Date.Month = t.Time.Month()
	n.Date.Day = t.Time.Day()

	return nil
}

// Value implements the [Valuer] interface
//func (n NullDate) Value() (driver.Value, error) {
//	if n.Valid {
//		return civil.Date(n.Date).In(time.UTC), nil
//	} else {
//		return nil, nil
//	}
//}

type NullDateTime struct {
	DateTime DateTime
	Valid    bool
}

// Scan implements the [Scanner] interface.
func (n *NullDateTime) Scan(value any) error {
	t := &sql.NullTime{}

	err := t.Scan(value)
	if err != nil {
		return err
	}

	if !t.Valid {
		n.Valid = false

		return nil
	}

	n.Valid = true

	n.DateTime.Date.Year = t.Time.Year()
	n.DateTime.Date.Month = t.Time.Month()
	n.DateTime.Date.Day = t.Time.Day()

	n.DateTime.Time.Hour = t.Time.Hour()
	n.DateTime.Time.Minute = t.Time.Minute()
	n.DateTime.Time.Second = t.Time.Second()
	n.DateTime.Time.Nanosecond = t.Time.Nanosecond()

	return nil
}

type NullDateTime2 struct {
	DateTime DateTime2
	Valid    bool
}

// Scan implements the [Scanner] interface.
func (n *NullDateTime2) Scan(value any) error {
	t := &sql.NullTime{}

	err := t.Scan(value)
	if err != nil {
		return err
	}

	if !t.Valid {
		n.Valid = false

		return nil
	}

	n.Valid = true

	n.DateTime.Date.Year = t.Time.Year()
	n.DateTime.Date.Month = t.Time.Month()
	n.DateTime.Date.Day = t.Time.Day()

	n.DateTime.Time.Hour = t.Time.Hour()
	n.DateTime.Time.Minute = t.Time.Minute()
	n.DateTime.Time.Second = t.Time.Second()
	n.DateTime.Time.Nanosecond = t.Time.Nanosecond()

	return nil
}

type NullTime struct {
	Time  Time
	Valid bool
}

// Scan implements the [Scanner] interface.
func (n *NullTime) Scan(value any) error {
	t := &sql.NullTime{}

	err := t.Scan(value)
	if err != nil {
		return err
	}

	if !t.Valid {
		n.Valid = false

		return nil
	}

	n.Valid = true

	n.Time.Hour = t.Time.Hour()
	n.Time.Minute = t.Time.Minute()
	n.Time.Second = t.Time.Second()
	n.Time.Nanosecond = t.Time.Nanosecond()

	return nil
}

type NullDateTimeOffset struct {
	DateTimeOffset DateTimeOffset
	Valid          bool
}

// Scan implements the [Scanner] interface.
func (n *NullDateTimeOffset) Scan(value any) error {
	t := &sql.NullTime{}

	err := t.Scan(value)
	if err != nil {
		return err
	}

	n.Valid = t.Valid
	n.DateTimeOffset = DateTimeOffset(t.Time)

	return nil
}
