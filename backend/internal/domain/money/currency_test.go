package money

import (
	"errors"
	"testing"
)

func TestParseCurrency(t *testing.T) {
	tests := []struct {
		name    string
		code    string
		want    string
		wantErr error
	}{
		{name: "MXN accepted", code: "MXN", want: "MXN"},
		{name: "USD unsupported", code: "USD", wantErr: ErrUnsupportedCurrency},
		{name: "lowercase unsupported", code: "mxn", wantErr: ErrUnsupportedCurrency},
		{name: "empty unsupported", code: "", wantErr: ErrUnsupportedCurrency},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			currency, err := ParseCurrency(test.code)
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("error = %v, want %v", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseCurrency() error = %v", err)
			}
			if currency.Code() != test.want || currency.String() != test.want {
				t.Fatalf("currency code = %q, string = %q, want %q", currency.Code(), currency.String(), test.want)
			}
		})
	}
}

func TestMXN(t *testing.T) {
	if got := MXN().Code(); got != "MXN" {
		t.Fatalf("MXN().Code() = %q, want MXN", got)
	}
}

func TestZeroCurrencyIsInvalid(t *testing.T) {
	var currency Currency
	if currency == MXN() {
		t.Fatal("zero Currency must not equal MXN")
	}
	if currency.Code() != "" || currency.String() != "" {
		t.Fatalf("zero Currency code = %q, string = %q, want empty values", currency.Code(), currency.String())
	}
	if _, err := New(0, currency); !errors.Is(err, ErrUnsupportedCurrency) {
		t.Fatalf("New() error = %v, want %v", err, ErrUnsupportedCurrency)
	}
}
