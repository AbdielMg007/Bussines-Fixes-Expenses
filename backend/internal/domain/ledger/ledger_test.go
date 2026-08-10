package ledger

import (
	"errors"
	"testing"
	"time"

	"runway/backend/internal/domain/account"
	"runway/backend/internal/domain/financialdate"
	"runway/backend/internal/domain/money"
)

func TestEffectsAreExplicitAndAccountCompatible(t *testing.T) {
	tests := []struct {
		name        string
		effect      Effect
		accountType account.AccountType
		start       int64
		want        int64
	}{
		{name: "asset inflow", effect: AssetInflow(), accountType: account.Bank(), start: 100, want: 150},
		{name: "asset outflow", effect: AssetOutflow(), accountType: account.Cash(), start: 100, want: 50},
		{name: "liability charge", effect: LiabilityCharge(), accountType: account.CreditCard(), start: 100, want: 150},
		{name: "liability payment", effect: LiabilityPayment(), accountType: account.Loan(), start: 100, want: 50},
	}
	amount, _ := money.New(50, money.MXN())
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.effect.ValidateFor(test.accountType); err != nil {
				t.Fatalf("ValidateFor() error = %v", err)
			}
			start, _ := money.NewBalance(test.start, money.MXN())
			got, err := test.effect.Apply(start, amount)
			if err != nil || got.MinorUnits() != test.want {
				t.Fatalf("Apply() = %d, %v; want %d", got.MinorUnits(), err, test.want)
			}
		})
	}
	if err := LiabilityCharge().ValidateFor(account.Bank()); !errors.Is(err, ErrIncompatibleEffect) {
		t.Fatalf("liability effect on asset error = %v", err)
	}
	if err := AssetInflow().ValidateFor(account.CreditCard()); !errors.Is(err, ErrIncompatibleEffect) {
		t.Fatalf("asset effect on liability error = %v", err)
	}
}

func TestTransactionRequiresPositiveMagnitudeAndStableSequence(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	date, _ := financialdate.Parse("2026-08-10")
	zero, _ := money.Zero(money.MXN())
	if _, err := NewTransaction("tx", "owner", "account", ManualKind(), zero, AssetInflow(), date, 1, "", "", now); !errors.Is(err, ErrZeroAmount) {
		t.Fatalf("zero transaction error = %v", err)
	}
	amount, _ := money.New(1, money.MXN())
	if _, err := NewTransaction("tx", "owner", "account", ManualKind(), amount, AssetInflow(), date, 0, "", "", now); !errors.Is(err, ErrInvalidTransaction) {
		t.Fatalf("zero sequence error = %v", err)
	}
	if _, err := NewTransaction("tx", "owner", "account", TransferKind(), amount, AssetInflow(), date, 1, "", "", now); !errors.Is(err, ErrInvalidTransaction) {
		t.Fatalf("unlinked transfer error = %v", err)
	}
}

func TestTransferEffectsPreserveAccountEconomics(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	source, _ := account.New("bank", "owner", "Bank", account.Bank(), money.MXN(), now)
	assetDestination, _ := account.New("cash", "owner", "Cash", account.Cash(), money.MXN(), now)
	liabilityDestination, _ := account.New("card", "owner", "Card", account.CreditCard(), money.MXN(), now)

	sourceEffect, destinationEffect, err := TransferEffects(source, assetDestination)
	if err != nil || sourceEffect != AssetOutflow() || destinationEffect != AssetInflow() {
		t.Fatalf("asset transfer effects = %q, %q, %v", sourceEffect, destinationEffect, err)
	}
	sourceEffect, destinationEffect, err = TransferEffects(source, liabilityDestination)
	if err != nil || sourceEffect != AssetOutflow() || destinationEffect != LiabilityPayment() {
		t.Fatalf("card payment effects = %q, %q, %v", sourceEffect, destinationEffect, err)
	}
	if _, _, err := TransferEffects(liabilityDestination, source); !errors.Is(err, ErrInvalidTransfer) {
		t.Fatalf("liability source error = %v", err)
	}
	if _, _, err := TransferEffects(source, source); !errors.Is(err, ErrSameTransferAccount) {
		t.Fatalf("same account error = %v", err)
	}
}
