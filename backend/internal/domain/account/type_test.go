package account_test

import (
	"errors"
	"testing"

	"runway/backend/internal/domain/account"
)

func TestParseTypeAndSemantics(t *testing.T) {
	tests := []struct {
		name              string
		value             string
		constructor       account.AccountType
		asset             bool
		liability         bool
		liquidityEligible bool
	}{
		{name: "cash", value: "cash", constructor: account.Cash(), asset: true, liquidityEligible: true},
		{name: "bank", value: "bank", constructor: account.Bank(), asset: true, liquidityEligible: true},
		{name: "credit card", value: "credit_card", constructor: account.CreditCard(), liability: true},
		{name: "loan", value: "loan", constructor: account.Loan(), liability: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			accountType, err := account.ParseType(test.value)
			if err != nil {
				t.Fatalf("ParseType() error = %v", err)
			}
			if accountType != test.constructor {
				t.Fatalf("ParseType() = %v, want %v", accountType, test.constructor)
			}
			if accountType.String() != test.value {
				t.Fatalf("String() = %q, want %q", accountType.String(), test.value)
			}
			if accountType.CanRepresentAsset() != test.asset {
				t.Fatalf("CanRepresentAsset() = %t, want %t", accountType.CanRepresentAsset(), test.asset)
			}
			if accountType.CanRepresentLiability() != test.liability {
				t.Fatalf("CanRepresentLiability() = %t, want %t", accountType.CanRepresentLiability(), test.liability)
			}
			if accountType.IsPotentiallyLiquidityEligible() != test.liquidityEligible {
				t.Fatalf(
					"IsPotentiallyLiquidityEligible() = %t, want %t",
					accountType.IsPotentiallyLiquidityEligible(),
					test.liquidityEligible,
				)
			}
		})
	}
}

func TestParseTypeRejectsInvalidValues(t *testing.T) {
	values := []string{"", "card", "credit-card", "BANK", " bank "}
	for _, value := range values {
		t.Run(value, func(t *testing.T) {
			_, err := account.ParseType(value)
			if !errors.Is(err, account.ErrInvalidAccountType) {
				t.Fatalf("error = %v, want %v", err, account.ErrInvalidAccountType)
			}
		})
	}
}

func TestZeroAccountTypeHasNoDomainSemantics(t *testing.T) {
	var accountType account.AccountType
	if accountType.CanRepresentAsset() || accountType.CanRepresentLiability() || accountType.IsPotentiallyLiquidityEligible() {
		t.Fatal("zero AccountType must not imply asset, liability, or liquidity semantics")
	}
}
