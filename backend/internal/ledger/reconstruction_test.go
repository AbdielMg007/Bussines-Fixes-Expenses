package ledger

import (
	"errors"
	"testing"
	"time"

	"runway/backend/internal/domain/account"
	"runway/backend/internal/domain/financialdate"
	domainledger "runway/backend/internal/domain/ledger"
	"runway/backend/internal/domain/money"
)

func TestReconstructBalance(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	financialAccount, _ := account.New("account", "owner", "Bank", account.Bank(), money.MXN(), now)
	date, _ := financialdate.Parse("2026-08-10")

	tests := []struct {
		name         string
		snapshot     *domainledger.BalanceSnapshot
		transactions []domainledger.Transaction
		want         int64
		wantApplied  int
	}{
		{name: "no snapshot and no transactions", want: 0},
		{name: "no snapshot with negative asset balance", transactions: []domainledger.Transaction{
			mustTransaction(t, "one", domainledger.AssetInflow(), 100, 1, date, now),
			mustTransaction(t, "two", domainledger.AssetOutflow(), 150, 2, date, now),
		}, want: -50, wantApplied: 2},
		{name: "snapshot only", snapshot: mustSnapshot(t, 400, 2, now), want: 400},
		{name: "snapshot plus later entries", snapshot: mustSnapshot(t, 400, 2, now), transactions: []domainledger.Transaction{
			mustTransaction(t, "three", domainledger.AssetOutflow(), 125, 3, date, now),
			mustTransaction(t, "four", domainledger.AssetInflow(), 25, 4, date, now),
		}, want: 300, wantApplied: 2},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := ReconstructBalance(BalanceState{Account: financialAccount, Snapshot: test.snapshot, Transactions: test.transactions})
			if err != nil {
				t.Fatalf("ReconstructBalance() error = %v", err)
			}
			if result.Balance.MinorUnits() != test.want || result.AppliedTransactions != test.wantApplied {
				t.Fatalf("balance = %d applied = %d; want %d, %d", result.Balance.MinorUnits(), result.AppliedTransactions, test.want, test.wantApplied)
			}
		})
	}
}

func TestReconstructBalanceRejectsTransactionsAtOrBeforeCutoffAndDuplicateSequences(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	financialAccount, _ := account.New("account", "owner", "Bank", account.Bank(), money.MXN(), now)
	date, _ := financialdate.Parse("2026-08-10")
	snapshot := mustSnapshot(t, 400, 2, now)
	for _, transactions := range [][]domainledger.Transaction{
		{mustTransaction(t, "represented", domainledger.AssetInflow(), 50, 2, date, now)},
		{
			mustTransaction(t, "first", domainledger.AssetInflow(), 50, 3, date, now),
			mustTransaction(t, "duplicate", domainledger.AssetInflow(), 50, 3, date, now),
		},
	} {
		if _, err := ReconstructBalance(BalanceState{Account: financialAccount, Snapshot: snapshot, Transactions: transactions}); !errors.Is(err, domainledger.ErrInvalidTransaction) {
			t.Fatalf("invalid sequence error = %v", err)
		}
	}
}

func TestReconstructLiabilityBalance(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	card, _ := account.New("account", "owner", "Card", account.CreditCard(), money.MXN(), now)
	date, _ := financialdate.Parse("2026-08-10")
	result, err := ReconstructBalance(BalanceState{Account: card, Transactions: []domainledger.Transaction{
		mustTransaction(t, "charge", domainledger.LiabilityCharge(), 6000, 1, date, now),
		mustTransaction(t, "payment", domainledger.LiabilityPayment(), 1000, 2, date, now),
	}})
	if err != nil || result.Balance.MinorUnits() != 5000 {
		t.Fatalf("liability balance = %d, error = %v; want 5000", result.Balance.MinorUnits(), err)
	}
}

func mustTransaction(t *testing.T, id string, effect domainledger.Effect, amountMinor, sequence int64, date financialdate.Date, now time.Time) domainledger.Transaction {
	t.Helper()
	amount, _ := money.New(amountMinor, money.MXN())
	transaction, err := domainledger.NewTransaction(id, "owner", "account", domainledger.ManualKind(), amount, effect, date, sequence, "", "", now)
	if err != nil {
		t.Fatal(err)
	}
	return transaction
}

func mustSnapshot(t *testing.T, balanceMinor, cutoff int64, now time.Time) *domainledger.BalanceSnapshot {
	t.Helper()
	balance, _ := money.NewBalance(balanceMinor, money.MXN())
	snapshot, err := domainledger.NewBalanceSnapshot("snapshot", "owner", "account", balance, now, cutoff, now)
	if err != nil {
		t.Fatal(err)
	}
	return &snapshot
}
