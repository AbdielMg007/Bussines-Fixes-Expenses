package financialdate

import (
	"errors"
	"fmt"
	"time"
)

var ErrInvalidDate = errors.New("invalid financial date")

const (
	minimumYear     = 1
	maximumYear     = 9999
	maximumDayShift = maximumYear * 366
)

// Date is a Gregorian calendar date without a time-of-day or timezone.
type Date struct {
	year  int
	month time.Month
	day   int
}

// New constructs a valid financial date.
func New(year int, month time.Month, day int) (Date, error) {
	if year < minimumYear || year > maximumYear {
		return Date{}, fmt.Errorf("%w: year %d is outside %04d-%04d", ErrInvalidDate, year, minimumYear, maximumYear)
	}
	if month < time.January || month > time.December {
		return Date{}, fmt.Errorf("%w: month %d", ErrInvalidDate, month)
	}

	// UTC is used only for Gregorian validation; it is not stored in Date.
	candidate := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	if candidate.Year() != year || candidate.Month() != month || candidate.Day() != day {
		return Date{}, fmt.Errorf("%w: %04d-%02d-%02d", ErrInvalidDate, year, month, day)
	}
	return Date{year: year, month: month, day: day}, nil
}

// Parse constructs a date from the exact YYYY-MM-DD representation.
func Parse(value string) (Date, error) {
	if len(value) != len("YYYY-MM-DD") {
		return Date{}, fmt.Errorf("%w: %q", ErrInvalidDate, value)
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil || parsed.Format("2006-01-02") != value {
		return Date{}, fmt.Errorf("%w: %q", ErrInvalidDate, value)
	}
	return New(parsed.Year(), parsed.Month(), parsed.Day())
}

// Year returns the calendar year.
func (d Date) Year() int {
	return d.year
}

// Month returns the calendar month.
func (d Date) Month() time.Month {
	return d.month
}

// Day returns the day of the month.
func (d Date) Day() int {
	return d.day
}

// String returns the YYYY-MM-DD representation, or an empty string for an
// invalid zero value.
func (d Date) String() string {
	if d.validate() != nil {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02d", d.year, d.month, d.day)
}

// Equal reports whether two valid dates identify the same calendar date.
func (d Date) Equal(other Date) (bool, error) {
	if err := validatePair(d, other); err != nil {
		return false, err
	}
	return d == other, nil
}

// Compare returns -1, 0, or 1 when d is before, equal to, or after other.
func (d Date) Compare(other Date) (int, error) {
	if err := validatePair(d, other); err != nil {
		return 0, err
	}
	switch {
	case d.year != other.year:
		if d.year < other.year {
			return -1, nil
		}
		return 1, nil
	case d.month != other.month:
		if d.month < other.month {
			return -1, nil
		}
		return 1, nil
	case d.day < other.day:
		return -1, nil
	case d.day > other.day:
		return 1, nil
	default:
		return 0, nil
	}
}

// AddDays returns a date offset by Gregorian calendar days. It never consults
// the system local timezone.
func (d Date) AddDays(days int) (Date, error) {
	if err := d.validate(); err != nil {
		return Date{}, err
	}
	if days < -maximumDayShift || days > maximumDayShift {
		return Date{}, fmt.Errorf("%w: day offset %d is outside the supported range", ErrInvalidDate, days)
	}
	result := time.Date(d.year, d.month, d.day, 0, 0, 0, 0, time.UTC).AddDate(0, 0, days)
	return New(result.Year(), result.Month(), result.Day())
}

func (d Date) validate() error {
	_, err := New(d.year, d.month, d.day)
	return err
}

func validatePair(left, right Date) error {
	if err := left.validate(); err != nil {
		return err
	}
	return right.validate()
}
