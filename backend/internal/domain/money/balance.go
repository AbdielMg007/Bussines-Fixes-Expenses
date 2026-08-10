package money

import "math"

// Balance is a signed monetary value in integer minor units. Unlike Money, it
// can represent account overdrafts and projected deficits.
type Balance struct {
	minorUnits int64
	currency   Currency
}

// NewBalance constructs a signed balance.
func NewBalance(minorUnits int64, currency Currency) (Balance, error) {
	if err := currency.validate(); err != nil {
		return Balance{}, err
	}
	return Balance{minorUnits: minorUnits, currency: currency}, nil
}

// ZeroBalance constructs a zero signed balance in the given currency.
func ZeroBalance(currency Currency) (Balance, error) {
	return NewBalance(0, currency)
}

// MinorUnits returns the signed balance in minor units.
func (b Balance) MinorUnits() int64 {
	return b.minorUnits
}

// Currency returns the balance's currency.
func (b Balance) Currency() Currency {
	return b.currency
}

// Equal reports whether two balances are equal. Different currencies fail.
func (b Balance) Equal(other Balance) (bool, error) {
	if err := validateBalancePair(b, other); err != nil {
		return false, err
	}
	return b.minorUnits == other.minorUnits, nil
}

// Add returns the sum of two signed balances in the same currency.
func (b Balance) Add(other Balance) (Balance, error) {
	if err := validateBalancePair(b, other); err != nil {
		return Balance{}, err
	}
	if additionOverflows(b.minorUnits, other.minorUnits) {
		return Balance{}, ErrMonetaryAmountOverflow
	}
	return NewBalance(b.minorUnits+other.minorUnits, b.currency)
}

// Subtract returns the signed difference between two balances.
func (b Balance) Subtract(other Balance) (Balance, error) {
	if err := validateBalancePair(b, other); err != nil {
		return Balance{}, err
	}
	if subtractionOverflows(b.minorUnits, other.minorUnits) {
		return Balance{}, ErrMonetaryAmountOverflow
	}
	return NewBalance(b.minorUnits-other.minorUnits, b.currency)
}

// Compare returns -1, 0, or 1 when b is less than, equal to, or greater than
// other. Different currencies fail.
func (b Balance) Compare(other Balance) (int, error) {
	if err := validateBalancePair(b, other); err != nil {
		return 0, err
	}
	switch {
	case b.minorUnits < other.minorUnits:
		return -1, nil
	case b.minorUnits > other.minorUnits:
		return 1, nil
	default:
		return 0, nil
	}
}

func validateBalancePair(left, right Balance) error {
	return requireSameCurrency(left.currency, right.currency)
}

func additionOverflows(left, right int64) bool {
	return right > 0 && left > math.MaxInt64-right ||
		right < 0 && left < math.MinInt64-right
}

func subtractionOverflows(left, right int64) bool {
	return right > 0 && left < math.MinInt64+right ||
		right < 0 && left > math.MaxInt64+right
}
