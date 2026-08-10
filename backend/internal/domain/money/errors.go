package money

import "errors"

var (
	ErrUnsupportedCurrency    = errors.New("unsupported currency")
	ErrCurrencyMismatch       = errors.New("currency mismatch")
	ErrNegativeMagnitude      = errors.New("monetary magnitude cannot be negative")
	ErrInsufficientMagnitude  = errors.New("insufficient monetary magnitude")
	ErrMonetaryAmountOverflow = errors.New("monetary amount overflow")
)
