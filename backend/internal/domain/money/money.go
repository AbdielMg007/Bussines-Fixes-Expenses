package money

import (
	"fmt"
	"math"
)

// Money is a non-negative monetary magnitude in integer minor units.
type Money struct {
	minorUnits int64
	currency   Currency
}

// New constructs a monetary magnitude.
func New(minorUnits int64, currency Currency) (Money, error) {
	value := Money{minorUnits: minorUnits, currency: currency}
	if err := value.validate(); err != nil {
		return Money{}, err
	}
	return value, nil
}

// Zero constructs a zero monetary magnitude in the given currency.
func Zero(currency Currency) (Money, error) {
	return New(0, currency)
}

// MinorUnits returns the non-negative magnitude in minor units.
func (m Money) MinorUnits() int64 {
	return m.minorUnits
}

// Currency returns the magnitude's currency.
func (m Money) Currency() Currency {
	return m.currency
}

// Equal reports whether two magnitudes are equal. Different currencies fail.
func (m Money) Equal(other Money) (bool, error) {
	if err := validateMoneyPair(m, other); err != nil {
		return false, err
	}
	return m.minorUnits == other.minorUnits, nil
}

// Add returns the sum of two magnitudes in the same currency.
func (m Money) Add(other Money) (Money, error) {
	if err := validateMoneyPair(m, other); err != nil {
		return Money{}, err
	}
	if m.minorUnits > math.MaxInt64-other.minorUnits {
		return Money{}, ErrMonetaryAmountOverflow
	}
	return New(m.minorUnits+other.minorUnits, m.currency)
}

// Subtract returns the non-negative difference between two magnitudes.
func (m Money) Subtract(other Money) (Money, error) {
	if err := validateMoneyPair(m, other); err != nil {
		return Money{}, err
	}
	if other.minorUnits > m.minorUnits {
		return Money{}, fmt.Errorf(
			"%w: cannot subtract %d from %d",
			ErrInsufficientMagnitude,
			other.minorUnits,
			m.minorUnits,
		)
	}
	return New(m.minorUnits-other.minorUnits, m.currency)
}

// Compare returns -1, 0, or 1 when m is less than, equal to, or greater than
// other. Different currencies fail.
func (m Money) Compare(other Money) (int, error) {
	if err := validateMoneyPair(m, other); err != nil {
		return 0, err
	}
	switch {
	case m.minorUnits < other.minorUnits:
		return -1, nil
	case m.minorUnits > other.minorUnits:
		return 1, nil
	default:
		return 0, nil
	}
}

func (m Money) validate() error {
	if m.minorUnits < 0 {
		return fmt.Errorf("%w: %d", ErrNegativeMagnitude, m.minorUnits)
	}
	return m.currency.validate()
}

func validateMoneyPair(left, right Money) error {
	if left.minorUnits < 0 {
		return fmt.Errorf("%w: %d", ErrNegativeMagnitude, left.minorUnits)
	}
	if right.minorUnits < 0 {
		return fmt.Errorf("%w: %d", ErrNegativeMagnitude, right.minorUnits)
	}
	return requireSameCurrency(left.currency, right.currency)
}
