package financialdate_test

import (
	"errors"
	"math"
	"testing"
	"time"

	"runway/backend/internal/domain/financialdate"
)

func TestNew(t *testing.T) {
	valid := []struct {
		name  string
		year  int
		month time.Month
		day   int
		want  string
	}{
		{name: "ordinary date", year: 2026, month: time.August, day: 10, want: "2026-08-10"},
		{name: "leap day", year: 2024, month: time.February, day: 29, want: "2024-02-29"},
		{name: "leap century", year: 2000, month: time.February, day: 29, want: "2000-02-29"},
		{name: "minimum year", year: 1, month: time.January, day: 1, want: "0001-01-01"},
		{name: "maximum year", year: 9999, month: time.December, day: 31, want: "9999-12-31"},
	}

	for _, test := range valid {
		t.Run(test.name, func(t *testing.T) {
			date, err := financialdate.New(test.year, test.month, test.day)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if date.String() != test.want {
				t.Fatalf("String() = %q, want %q", date.String(), test.want)
			}
			if date.Year() != test.year || date.Month() != test.month || date.Day() != test.day {
				t.Fatalf("date components = %d-%d-%d", date.Year(), date.Month(), date.Day())
			}
		})
	}
}

func TestNewRejectsInvalidDates(t *testing.T) {
	tests := []struct {
		name  string
		year  int
		month time.Month
		day   int
	}{
		{name: "year zero", year: 0, month: time.January, day: 1},
		{name: "year too large", year: 10000, month: time.January, day: 1},
		{name: "month zero", year: 2026, month: 0, day: 1},
		{name: "month thirteen", year: 2026, month: 13, day: 1},
		{name: "day zero", year: 2026, month: time.January, day: 0},
		{name: "invalid leap day", year: 2023, month: time.February, day: 29},
		{name: "non-leap century 1900", year: 1900, month: time.February, day: 29},
		{name: "non-leap century 2100", year: 2100, month: time.February, day: 29},
		{name: "april thirty one", year: 2026, month: time.April, day: 31},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := financialdate.New(test.year, test.month, test.day)
			if !errors.Is(err, financialdate.ErrInvalidDate) {
				t.Fatalf("error = %v, want %v", err, financialdate.ErrInvalidDate)
			}
		})
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    string
		wantErr bool
	}{
		{name: "valid", value: "2026-08-10", want: "2026-08-10"},
		{name: "valid leap day", value: "2024-02-29", want: "2024-02-29"},
		{name: "not padded", value: "2026-8-1", wantErr: true},
		{name: "time included", value: "2026-08-10T00:00:00Z", wantErr: true},
		{name: "invalid date", value: "2026-02-29", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			date, err := financialdate.Parse(test.value)
			if test.wantErr {
				if !errors.Is(err, financialdate.ErrInvalidDate) {
					t.Fatalf("error = %v, want %v", err, financialdate.ErrInvalidDate)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if date.String() != test.want {
				t.Fatalf("String() = %q, want %q", date.String(), test.want)
			}
		})
	}
}

func TestAddDays(t *testing.T) {
	tests := []struct {
		name  string
		start string
		days  int
		want  string
	}{
		{name: "month boundary", start: "2026-01-31", days: 1, want: "2026-02-01"},
		{name: "leap year", start: "2024-02-28", days: 1, want: "2024-02-29"},
		{name: "after leap day", start: "2024-02-29", days: 1, want: "2024-03-01"},
		{name: "year boundary", start: "2026-12-31", days: 1, want: "2027-01-01"},
		{name: "negative days", start: "2026-01-01", days: -1, want: "2025-12-31"},
		{name: "zero days", start: "2026-08-10", days: 0, want: "2026-08-10"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			start := mustParseDate(t, test.start)
			got, err := start.AddDays(test.days)
			if err != nil {
				t.Fatalf("AddDays() error = %v", err)
			}
			if got.String() != test.want {
				t.Fatalf("AddDays() = %q, want %q", got.String(), test.want)
			}
		})
	}
}

func TestAddDaysRejectsOutOfRangeResult(t *testing.T) {
	minimum := mustParseDate(t, "0001-01-01")
	if _, err := minimum.AddDays(-1); !errors.Is(err, financialdate.ErrInvalidDate) {
		t.Fatalf("lower-bound error = %v, want %v", err, financialdate.ErrInvalidDate)
	}

	maximum := mustParseDate(t, "9999-12-31")
	if _, err := maximum.AddDays(1); !errors.Is(err, financialdate.ErrInvalidDate) {
		t.Fatalf("upper-bound error = %v, want %v", err, financialdate.ErrInvalidDate)
	}

	ordinary := mustParseDate(t, "2026-08-10")
	if _, err := ordinary.AddDays(math.MaxInt); !errors.Is(err, financialdate.ErrInvalidDate) {
		t.Fatalf("extreme offset error = %v, want %v", err, financialdate.ErrInvalidDate)
	}
}

func TestEqualAndCompare(t *testing.T) {
	tests := []struct {
		name        string
		left        string
		right       string
		wantEqual   bool
		wantCompare int
	}{
		{name: "equal", left: "2026-08-10", right: "2026-08-10", wantEqual: true},
		{name: "earlier year", left: "2025-08-10", right: "2026-08-10", wantCompare: -1},
		{name: "earlier month", left: "2026-07-10", right: "2026-08-10", wantCompare: -1},
		{name: "earlier day", left: "2026-08-09", right: "2026-08-10", wantCompare: -1},
		{name: "later", left: "2026-08-11", right: "2026-08-10", wantCompare: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			left := mustParseDate(t, test.left)
			right := mustParseDate(t, test.right)
			equal, err := left.Equal(right)
			if err != nil {
				t.Fatalf("Equal() error = %v", err)
			}
			if equal != test.wantEqual {
				t.Fatalf("Equal() = %t, want %t", equal, test.wantEqual)
			}
			comparison, err := left.Compare(right)
			if err != nil {
				t.Fatalf("Compare() error = %v", err)
			}
			if comparison != test.wantCompare {
				t.Fatalf("Compare() = %d, want %d", comparison, test.wantCompare)
			}
		})
	}
}

func TestZeroDateIsInvalid(t *testing.T) {
	var zero financialdate.Date
	if zero.String() != "" {
		t.Fatalf("zero String() = %q, want empty", zero.String())
	}
	if _, err := zero.AddDays(1); !errors.Is(err, financialdate.ErrInvalidDate) {
		t.Fatalf("AddDays() error = %v, want %v", err, financialdate.ErrInvalidDate)
	}
	if _, err := zero.Compare(mustParseDate(t, "2026-08-10")); !errors.Is(err, financialdate.ErrInvalidDate) {
		t.Fatalf("Compare() error = %v, want %v", err, financialdate.ErrInvalidDate)
	}
}

func mustParseDate(t *testing.T, value string) financialdate.Date {
	t.Helper()
	date, err := financialdate.Parse(value)
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", value, err)
	}
	return date
}
