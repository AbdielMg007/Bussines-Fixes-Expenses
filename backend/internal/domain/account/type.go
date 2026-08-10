package account

import (
	"errors"
	"fmt"
)

var ErrInvalidAccountType = errors.New("invalid account type")

const (
	cashValue       = "cash"
	bankValue       = "bank"
	creditCardValue = "credit_card"
	loanValue       = "loan"
)

// AccountType identifies the economic role available to an account. It does
// not decide whether an account is selected for consolidated liquidity.
type AccountType struct {
	value string
}

// ParseType constructs a supported account type from its exact stored value.
func ParseType(value string) (AccountType, error) {
	switch value {
	case cashValue, bankValue, creditCardValue, loanValue:
		return AccountType{value: value}, nil
	default:
		return AccountType{}, fmt.Errorf("%w: %q", ErrInvalidAccountType, value)
	}
}

func Cash() AccountType {
	return AccountType{value: cashValue}
}

func Bank() AccountType {
	return AccountType{value: bankValue}
}

func CreditCard() AccountType {
	return AccountType{value: creditCardValue}
}

func Loan() AccountType {
	return AccountType{value: loanValue}
}

func (t AccountType) String() string {
	return t.value
}

// CanRepresentAsset reports whether this type can hold an asset balance.
func (t AccountType) CanRepresentAsset() bool {
	return t.value == cashValue || t.value == bankValue
}

// CanRepresentLiability reports whether this type can hold a liability balance.
func (t AccountType) CanRepresentLiability() bool {
	return t.value == creditCardValue || t.value == loanValue
}

// IsPotentiallyLiquidityEligible reports type-level eligibility only. A future
// ProjectionPolicy and AccountSelection decide actual inclusion.
func (t AccountType) IsPotentiallyLiquidityEligible() bool {
	return t.value == cashValue || t.value == bankValue
}
