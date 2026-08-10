package money

import "fmt"

const mxnCode = "MXN"

// Currency identifies the denomination of a monetary value. Its fields are
// private so supported currencies can only be created through this package.
type Currency struct {
	code string
}

// ParseCurrency constructs a supported currency from its exact ISO-style code.
func ParseCurrency(code string) (Currency, error) {
	switch code {
	case mxnCode:
		return Currency{code: mxnCode}, nil
	default:
		return Currency{}, fmt.Errorf("%w: %q", ErrUnsupportedCurrency, code)
	}
}

// MXN returns the supported Mexican peso currency.
func MXN() Currency {
	return Currency{code: mxnCode}
}

// Code returns the currency code.
func (c Currency) Code() string {
	return c.code
}

func (c Currency) String() string {
	return c.code
}

func (c Currency) validate() error {
	if c.code != mxnCode {
		return fmt.Errorf("%w: %q", ErrUnsupportedCurrency, c.code)
	}
	return nil
}

func requireSameCurrency(left, right Currency) error {
	if left.code != right.code {
		return fmt.Errorf("%w: %q and %q", ErrCurrencyMismatch, left.code, right.code)
	}
	if err := left.validate(); err != nil {
		return err
	}
	return right.validate()
}
