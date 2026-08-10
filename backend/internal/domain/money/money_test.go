package money

import (
	"errors"
	"math"
	"testing"
)

func TestNewMoney(t *testing.T) {
	tests := []struct {
		name       string
		minorUnits int64
		currency   Currency
		wantErr    error
	}{
		{name: "zero", minorUnits: 0, currency: MXN()},
		{name: "positive", minorUnits: 12345, currency: MXN()},
		{name: "negative", minorUnits: -1, currency: MXN(), wantErr: ErrNegativeMagnitude},
		{name: "unsupported currency", minorUnits: 1, currency: Currency{}, wantErr: ErrUnsupportedCurrency},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value, err := New(test.minorUnits, test.currency)
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("error = %v, want %v", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if value.MinorUnits() != test.minorUnits {
				t.Fatalf("MinorUnits() = %d, want %d", value.MinorUnits(), test.minorUnits)
			}
			if value.Currency() != test.currency {
				t.Fatalf("Currency() = %v, want %v", value.Currency(), test.currency)
			}
		})
	}
}

func TestMoneyZero(t *testing.T) {
	zero, err := Zero(MXN())
	if err != nil {
		t.Fatalf("Zero() error = %v", err)
	}
	if zero.MinorUnits() != 0 || zero.Currency() != MXN() {
		t.Fatalf("Zero() = %+v, want zero MXN", zero)
	}
}

func TestMoneyGoZeroValueIsInvalid(t *testing.T) {
	var invalid Money
	validZero, err := Zero(MXN())
	if err != nil {
		t.Fatalf("Zero() error = %v", err)
	}
	if invalid == validZero {
		t.Fatal("Go zero-value Money must not equal valid zero MXN")
	}

	operations := []struct {
		name string
		run  func() error
	}{
		{name: "add", run: func() error { _, err := invalid.Add(invalid); return err }},
		{name: "subtract", run: func() error { _, err := invalid.Subtract(invalid); return err }},
		{name: "equal", run: func() error { _, err := invalid.Equal(invalid); return err }},
		{name: "compare", run: func() error { _, err := invalid.Compare(invalid); return err }},
	}

	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			if err := operation.run(); !errors.Is(err, ErrUnsupportedCurrency) {
				t.Fatalf("error = %v, want %v", err, ErrUnsupportedCurrency)
			}
		})
	}

	if _, err := invalid.Equal(validZero); !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatalf("Equal(valid zero) error = %v, want %v", err, ErrCurrencyMismatch)
	}
}

func TestMoneyAdd(t *testing.T) {
	usd := Currency{code: "USD"} // Deliberately bypasses construction to exercise the future-currency guard.
	tests := []struct {
		name    string
		left    Money
		right   Money
		want    int64
		wantErr error
	}{
		{name: "positive values", left: mustMoney(t, 120), right: mustMoney(t, 30), want: 150},
		{name: "add zero", left: mustMoney(t, 120), right: mustMoney(t, 0), want: 120},
		{name: "overflow", left: mustMoney(t, math.MaxInt64), right: mustMoney(t, 1), wantErr: ErrMonetaryAmountOverflow},
		{name: "currency mismatch", left: mustMoney(t, 120), right: Money{minorUnits: 30, currency: usd}, wantErr: ErrCurrencyMismatch},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.left.Add(test.right)
			assertMoneyResult(t, got, err, test.want, test.wantErr)
		})
	}
}

func TestMoneySubtract(t *testing.T) {
	usd := Currency{code: "USD"}
	tests := []struct {
		name    string
		left    Money
		right   Money
		want    int64
		wantErr error
	}{
		{name: "positive difference", left: mustMoney(t, 120), right: mustMoney(t, 30), want: 90},
		{name: "exactly zero", left: mustMoney(t, 120), right: mustMoney(t, 120), want: 0},
		{name: "insufficient magnitude", left: mustMoney(t, 30), right: mustMoney(t, 120), wantErr: ErrInsufficientMagnitude},
		{name: "currency mismatch", left: mustMoney(t, 120), right: Money{minorUnits: 30, currency: usd}, wantErr: ErrCurrencyMismatch},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.left.Subtract(test.right)
			assertMoneyResult(t, got, err, test.want, test.wantErr)
		})
	}
}

func TestMoneyEqual(t *testing.T) {
	usd := Currency{code: "USD"}
	tests := []struct {
		name    string
		left    Money
		right   Money
		want    bool
		wantErr error
	}{
		{name: "equal", left: mustMoney(t, 100), right: mustMoney(t, 100), want: true},
		{name: "different magnitude", left: mustMoney(t, 100), right: mustMoney(t, 101), want: false},
		{name: "currency mismatch", left: mustMoney(t, 100), right: Money{minorUnits: 100, currency: usd}, wantErr: ErrCurrencyMismatch},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.left.Equal(test.right)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
			if got != test.want {
				t.Fatalf("Equal() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestMoneyCompare(t *testing.T) {
	usd := Currency{code: "USD"}
	tests := []struct {
		name    string
		left    Money
		right   Money
		want    int
		wantErr error
	}{
		{name: "less", left: mustMoney(t, 99), right: mustMoney(t, 100), want: -1},
		{name: "equal", left: mustMoney(t, 100), right: mustMoney(t, 100), want: 0},
		{name: "greater", left: mustMoney(t, 101), right: mustMoney(t, 100), want: 1},
		{name: "currency mismatch", left: mustMoney(t, 100), right: Money{minorUnits: 100, currency: usd}, wantErr: ErrCurrencyMismatch},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.left.Compare(test.right)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
			if got != test.want {
				t.Fatalf("Compare() = %d, want %d", got, test.want)
			}
		})
	}
}

func mustMoney(t *testing.T, minorUnits int64) Money {
	t.Helper()
	value, err := New(minorUnits, MXN())
	if err != nil {
		t.Fatalf("New(%d) error = %v", minorUnits, err)
	}
	return value
}

func assertMoneyResult(t *testing.T, got Money, err error, want int64, wantErr error) {
	t.Helper()
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
	if wantErr == nil && got.MinorUnits() != want {
		t.Fatalf("MinorUnits() = %d, want %d", got.MinorUnits(), want)
	}
}
