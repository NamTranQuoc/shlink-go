package db

import (
	"database/sql/driver"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Time scans a datetime column. Stored values are wall-clock times in the configured timezone, so whatever the driver
// returns (time.Time in UTC, string or bytes) is re-read as wall-clock time in Loc.
type Time struct {
	Loc   *time.Location
	Time  time.Time
	Valid bool
}

var timeLayouts = []string{
	"2006-01-02 15:04:05.999999999",
	"2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02",
}

func (t *Time) Scan(src any) error {
	loc := t.Loc
	if loc == nil {
		loc = time.UTC
	}
	switch v := src.(type) {
	case nil:
		t.Valid = false
		return nil
	case time.Time:
		t.Time = time.Date(v.Year(), v.Month(), v.Day(), v.Hour(), v.Minute(), v.Second(), 0, loc)
		t.Valid = true
		return nil
	case []byte:
		return t.parse(string(v), loc)
	case string:
		return t.parse(v, loc)
	}
	return fmt.Errorf("cannot scan %T into db.Time", src)
}

func (t *Time) parse(s string, loc *time.Location) error {
	s = strings.TrimSpace(s)
	for _, layout := range timeLayouts {
		if parsed, err := time.ParseInLocation(layout, s, loc); err == nil {
			p := parsed
			t.Time = time.Date(p.Year(), p.Month(), p.Day(), p.Hour(), p.Minute(), p.Second(), 0, loc)
			t.Valid = true
			return nil
		}
	}
	return fmt.Errorf("cannot parse %q as a datetime", s)
}

// Ptr returns nil for NULL.
func (t Time) Ptr() *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}

// Bool scans boolean columns stored as bool, bit, tinyint or integer.
type Bool bool

func (b *Bool) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*b = false
	case bool:
		*b = Bool(v)
	case int64:
		*b = v != 0
	case []byte:
		*b = len(v) > 0 && string(v) != "0" && string(v) != "f" && string(v) != "false"
	case string:
		*b = v != "" && v != "0" && v != "f" && v != "false"
	default:
		return fmt.Errorf("cannot scan %T into db.Bool", src)
	}
	return nil
}

// Int scans integers that some drivers return as text or float (aggregates, DECIMAL sums).
type Int int64

func (i *Int) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*i = 0
	case int64:
		*i = Int(v)
	case float64:
		*i = Int(v)
	case []byte:
		return i.parse(string(v))
	case string:
		return i.parse(v)
	default:
		return fmt.Errorf("cannot scan %T into db.Int", src)
	}
	return nil
}

func (i *Int) parse(s string) error {
	s = strings.TrimSpace(s)
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		*i = Int(n)
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return err
	}
	*i = Int(f)
	return nil
}

// Value lets Int be used as a query argument.
func (i Int) Value() (driver.Value, error) { return int64(i), nil }
