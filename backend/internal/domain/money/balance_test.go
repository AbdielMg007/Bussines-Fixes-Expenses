package money

import (
	"errors"
	"math"
	"testing"
)

func TestNewBalance(t *testing.T) {
	tests := []struct {
		name       string
		minorUnits int64
		currency   Currency
		wantErr    error
	}{
		{name: "positive", minorUnits: 125, currency: MXN()},
		{name: "zero", minorUnits: 0, currency: MXN()},
		{name: "negative", minorUnits: -125, currency: MXN()},
		{name: "minimum int64 deficit", minorUnits: math.MinInt64, currency: MXN()},
		{name: "unsupported currency", minorUnits: 0, currency: Currency{}, wantErr: ErrUnsupportedCurrency},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			balance, err := NewBalance(test.minorUnits, test.currency)
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("error = %v, want %v", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewBalance() error = %v", err)
			}
			if balance.MinorUnits() != test.minorUnits || balance.Currency() != test.currency {
				t.Fatalf("balance = %+v, want %d %s", balance, test.minorUnits, test.currency)
			}
		})
	}
}

func TestBalanceZero(t *testing.T) {
	zero, err := ZeroBalance(MXN())
	if err != nil {
		t.Fatalf("ZeroBalance() error = %v", err)
	}
	if zero.MinorUnits() != 0 || zero.Currency() != MXN() {
		t.Fatalf("ZeroBalance() = %+v, want zero MXN", zero)
	}
}

func TestBalanceGoZeroValueIsInvalid(t *testing.T) {
	var invalid Balance
	validZero, err := ZeroBalance(MXN())
	if err != nil {
		t.Fatalf("ZeroBalance() error = %v", err)
	}
	if invalid == validZero {
		t.Fatal("Go zero-value Balance must not equal valid zero MXN balance")
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

func TestBalanceArithmetic(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		left      int64
		right     int64
		want      int64
		wantErr   error
	}{
		{name: "add positives", operation: "add", left: 100, right: 25, want: 125},
		{name: "add negative", operation: "add", left: 100, right: -125, want: -25},
		{name: "subtract to deficit", operation: "subtract", left: 100, right: 125, want: -25},
		{name: "subtract negative", operation: "subtract", left: 100, right: -25, want: 125},
		{name: "addition positive overflow", operation: "add", left: math.MaxInt64, right: 1, wantErr: ErrMonetaryAmountOverflow},
		{name: "addition negative overflow", operation: "add", left: math.MinInt64, right: -1, wantErr: ErrMonetaryAmountOverflow},
		{name: "subtraction negative overflow", operation: "subtract", left: math.MinInt64, right: 1, wantErr: ErrMonetaryAmountOverflow},
		{name: "subtraction positive overflow", operation: "subtract", left: math.MaxInt64, right: -1, wantErr: ErrMonetaryAmountOverflow},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			left := mustBalance(t, test.left)
			right := mustBalance(t, test.right)
			var (
				got Balance
				err error
			)
			if test.operation == "add" {
				got, err = left.Add(right)
			} else {
				got, err = left.Subtract(right)
			}
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
			if test.wantErr == nil && got.MinorUnits() != test.want {
				t.Fatalf("MinorUnits() = %d, want %d", got.MinorUnits(), test.want)
			}
		})
	}
}

func TestBalanceEqualAndCompare(t *testing.T) {
	tests := []struct {
		name        string
		left        int64
		right       int64
		wantEqual   bool
		wantCompare int
	}{
		{name: "less", left: -1, right: 0, wantCompare: -1},
		{name: "equal zero", left: 0, right: 0, wantEqual: true, wantCompare: 0},
		{name: "equal negative", left: -10, right: -10, wantEqual: true, wantCompare: 0},
		{name: "greater", left: 1, right: 0, wantCompare: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			left := mustBalance(t, test.left)
			right := mustBalance(t, test.right)
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

func TestBalanceCurrencyMismatch(t *testing.T) {
	mxn := mustBalance(t, 100)
	usd := Balance{minorUnits: 100, currency: Currency{code: "USD"}}

	operations := []struct {
		name string
		run  func() error
	}{
		{name: "add", run: func() error { _, err := mxn.Add(usd); return err }},
		{name: "subtract", run: func() error { _, err := mxn.Subtract(usd); return err }},
		{name: "equal", run: func() error { _, err := mxn.Equal(usd); return err }},
		{name: "compare", run: func() error { _, err := mxn.Compare(usd); return err }},
	}

	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			if err := operation.run(); !errors.Is(err, ErrCurrencyMismatch) {
				t.Fatalf("error = %v, want %v", err, ErrCurrencyMismatch)
			}
		})
	}
}

func mustBalance(t *testing.T, minorUnits int64) Balance {
	t.Helper()
	balance, err := NewBalance(minorUnits, MXN())
	if err != nil {
		t.Fatalf("NewBalance(%d) error = %v", minorUnits, err)
	}
	return balance
}
