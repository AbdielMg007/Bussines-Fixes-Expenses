package ledger

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"testing"
	"time"

	"runway/backend/internal/domain/account"
	"runway/backend/internal/domain/financialdate"
	domainledger "runway/backend/internal/domain/ledger"
	"runway/backend/internal/domain/money"
)

func TestServiceAccountLifecycleAndOwnerIsolation(t *testing.T) {
	repository := newMemoryRepository()
	service := newTestService(t, repository)
	ctx := context.Background()
	for _, accountType := range []account.AccountType{account.Cash(), account.Bank(), account.CreditCard(), account.Loan()} {
		if _, err := service.CreateAccount(ctx, "owner", accountType.String(), accountType, money.MXN()); err != nil {
			t.Fatalf("CreateAccount(%s) error = %v", accountType, err)
		}
	}
	accounts, err := service.ListAccounts(ctx, "owner")
	if err != nil || len(accounts) != 4 {
		t.Fatalf("ListAccounts() count = %d, error = %v", len(accounts), err)
	}
	if _, err := service.GetAccount(ctx, "other-owner", accounts[0].ID()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner GetAccount() error = %v", err)
	}
	archived, err := service.ArchiveAccount(ctx, "owner", accounts[0].ID())
	if err != nil || !archived.Status().IsArchived() {
		t.Fatalf("ArchiveAccount() = %+v, %v", archived, err)
	}
	date, _ := financialdate.Parse("2026-08-10")
	amount, _ := money.New(1, money.MXN())
	if _, err := service.PostTransaction(ctx, "owner", archived.ID(), domainledger.AssetInflow(), amount, date, "", testKey(t, "archived-post")); !errors.Is(err, account.ErrAccountArchived) {
		t.Fatalf("post to archived account error = %v", err)
	}
}

func TestServicePostsAndReconstructsWithoutDoubleCountingSnapshotHistory(t *testing.T) {
	repository := newMemoryRepository()
	service := newTestService(t, repository)
	ctx := context.Background()
	bank, _ := service.CreateAccount(ctx, "owner", "Bank", account.Bank(), money.MXN())
	date, _ := financialdate.Parse("2026-08-10")
	inflow, _ := money.New(1_000, money.MXN())
	outflow, _ := money.New(1_250, money.MXN())
	first, err := service.PostTransaction(ctx, "owner", bank.ID(), domainledger.AssetInflow(), inflow, date, "  Opening  ", testKey(t, "opening"))
	if err != nil {
		t.Fatal(err)
	}
	if first.Memo() != "Opening" {
		t.Fatalf("normalized memo = %q, want Opening", first.Memo())
	}
	if _, err := service.PostTransaction(ctx, "owner", bank.ID(), domainledger.AssetOutflow(), outflow, date, "Payment", testKey(t, "payment")); err != nil {
		t.Fatal(err)
	}
	result, err := service.CurrentBalance(ctx, "owner", bank.ID())
	if err != nil || result.Balance.MinorUnits() != -250 {
		t.Fatalf("balance without snapshot = %d, %v", result.Balance.MinorUnits(), err)
	}

	reconciled, _ := money.NewBalance(1_100, money.MXN())
	snapshot, err := service.CreateSnapshot(ctx, "owner", bank.ID(), reconciled, time.Date(2026, 8, 10, 15, 0, 0, 0, time.UTC), first.LedgerSequence(), testKey(t, "snapshot"))
	if err != nil {
		t.Fatal(err)
	}
	result, err = service.CurrentBalance(ctx, "owner", bank.ID())
	if err != nil || result.Balance.MinorUnits() != -150 || result.SnapshotID != snapshot.ID() || result.AppliedTransactions != 1 {
		t.Fatalf("balance with snapshot = %+v, %v", result, err)
	}
}

func TestServiceValidatesEffectsAmountsAndTransfers(t *testing.T) {
	repository := newMemoryRepository()
	service := newTestService(t, repository)
	ctx := context.Background()
	bank, _ := service.CreateAccount(ctx, "owner", "Bank", account.Bank(), money.MXN())
	cash, _ := service.CreateAccount(ctx, "owner", "Cash", account.Cash(), money.MXN())
	card, _ := service.CreateAccount(ctx, "owner", "Card", account.CreditCard(), money.MXN())
	date, _ := financialdate.Parse("2026-08-10")
	amount, _ := money.New(500, money.MXN())

	if _, err := service.PostTransaction(ctx, "owner", bank.ID(), domainledger.LiabilityCharge(), amount, date, "", testKey(t, "bad-effect")); !errors.Is(err, domainledger.ErrIncompatibleEffect) {
		t.Fatalf("invalid bank effect error = %v", err)
	}
	zero, _ := money.Zero(money.MXN())
	if _, err := service.PostTransaction(ctx, "owner", bank.ID(), domainledger.AssetInflow(), zero, date, "", testKey(t, "zero")); !errors.Is(err, domainledger.ErrZeroAmount) {
		t.Fatalf("zero amount error = %v", err)
	}
	assetTransfer, err := service.CreateTransfer(ctx, "owner", bank.ID(), cash.ID(), amount, date, "Cash", testKey(t, "cash-transfer"))
	if err != nil || assetTransfer.SourceTransaction.Effect() != domainledger.AssetOutflow() || assetTransfer.DestinationTransaction.Effect() != domainledger.AssetInflow() {
		t.Fatalf("asset transfer = %+v, %v", assetTransfer, err)
	}
	if _, err := service.PostTransaction(ctx, "owner", card.ID(), domainledger.LiabilityCharge(), amount, date, "Charge", testKey(t, "card-charge")); err != nil {
		t.Fatal(err)
	}
	cardTransfer, err := service.CreateTransfer(ctx, "owner", bank.ID(), card.ID(), amount, date, "Card", testKey(t, "card-transfer"))
	if err != nil || cardTransfer.DestinationTransaction.Effect() != domainledger.LiabilityPayment() {
		t.Fatalf("card transfer = %+v, %v", cardTransfer, err)
	}
	if _, err := service.CreateTransfer(ctx, "owner", bank.ID(), bank.ID(), amount, date, "", testKey(t, "same")); !errors.Is(err, domainledger.ErrSameTransferAccount) {
		t.Fatalf("same-account transfer error = %v", err)
	}
	if _, err := service.CreateTransfer(ctx, "other-owner", bank.ID(), cash.ID(), amount, date, "", testKey(t, "other")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner transfer error = %v", err)
	}
}

func TestCreateSnapshotRejectsNegativeLiabilityBalance(t *testing.T) {
	repository := newMemoryRepository()
	service := newTestService(t, repository)
	card, err := service.CreateAccount(context.Background(), "owner", "Card", account.CreditCard(), money.MXN())
	if err != nil {
		t.Fatal(err)
	}
	negative, _ := money.NewBalance(-1, money.MXN())
	if _, err := service.CreateSnapshot(context.Background(), "owner", card.ID(), negative, time.Now(), 0, testKey(t, "negative-liability-snapshot")); !errors.Is(err, ErrNegativeLiabilitySnapshot) {
		t.Fatalf("CreateSnapshot() error = %v, want %v", err, ErrNegativeLiabilitySnapshot)
	}
}

func TestReconstructBalanceRejectsArithmeticCorruption(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	date, _ := financialdate.Parse("2026-08-10")
	one, _ := money.New(1, money.MXN())

	tests := []struct {
		name          string
		accountType   account.AccountType
		startingMinor int64
		effect        domainledger.Effect
		want          error
	}{
		{name: "asset overflow", accountType: account.Bank(), startingMinor: math.MaxInt64, effect: domainledger.AssetInflow(), want: money.ErrMonetaryAmountOverflow},
		{name: "asset underflow", accountType: account.Bank(), startingMinor: math.MinInt64, effect: domainledger.AssetOutflow(), want: money.ErrMonetaryAmountOverflow},
		{name: "liability overflow", accountType: account.CreditCard(), startingMinor: math.MaxInt64, effect: domainledger.LiabilityCharge(), want: money.ErrMonetaryAmountOverflow},
		{name: "liability underflow", accountType: account.CreditCard(), startingMinor: 0, effect: domainledger.LiabilityPayment(), want: domainledger.ErrNegativeLiabilityBalance},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			financialAccount, _ := account.New("account", "owner", "Account", test.accountType, money.MXN(), now)
			starting, _ := money.NewBalance(test.startingMinor, money.MXN())
			snapshot, _ := domainledger.NewBalanceSnapshot("snapshot", "owner", "account", starting, now, 0, now)
			transaction, _ := domainledger.NewTransaction(
				"transaction", "owner", "account", domainledger.ManualKind(), one, test.effect, date, 1, "", "", now,
			)
			_, err := ReconstructBalance(BalanceState{Account: financialAccount, Snapshot: &snapshot, Transactions: []domainledger.Transaction{transaction}})
			if !errors.Is(err, test.want) {
				t.Fatalf("ReconstructBalance() error = %v, want %v", err, test.want)
			}
		})
	}

	t.Run("negative persisted liability snapshot", func(t *testing.T) {
		card, _ := account.New("card", "owner", "Card", account.CreditCard(), money.MXN(), now)
		negative, _ := money.NewBalance(-1, money.MXN())
		snapshot, _ := domainledger.NewBalanceSnapshot("snapshot", "owner", "card", negative, now, 0, now)
		_, err := ReconstructBalance(BalanceState{Account: card, Snapshot: &snapshot})
		if !errors.Is(err, domainledger.ErrNegativeLiabilityBalance) {
			t.Fatalf("ReconstructBalance() error = %v, want %v", err, domainledger.ErrNegativeLiabilityBalance)
		}
	})
}

type memoryRepository struct {
	accounts     map[string]account.Account
	transactions []domainledger.Transaction
	snapshots    map[string]domainledger.BalanceSnapshot
	nextSequence int64
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{accounts: make(map[string]account.Account), snapshots: make(map[string]domainledger.BalanceSnapshot)}
}

func (r *memoryRepository) CreateAccount(_ context.Context, financialAccount account.Account) error {
	if _, exists := r.accounts[financialAccount.ID()]; exists {
		return ErrConflict
	}
	r.accounts[financialAccount.ID()] = financialAccount
	return nil
}

func (r *memoryRepository) GetAccount(_ context.Context, ownerID, accountID string) (account.Account, error) {
	financialAccount, exists := r.accounts[accountID]
	if !exists || financialAccount.OwnerID() != ownerID {
		return account.Account{}, ErrNotFound
	}
	return financialAccount, nil
}

func (r *memoryRepository) ListAccounts(_ context.Context, ownerID string) ([]account.Account, error) {
	result := make([]account.Account, 0)
	for _, financialAccount := range r.accounts {
		if financialAccount.OwnerID() == ownerID {
			result = append(result, financialAccount)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID() < result[j].ID() })
	return result, nil
}

func (r *memoryRepository) ArchiveAccount(_ context.Context, ownerID, accountID string, now time.Time) (account.Account, error) {
	financialAccount, err := r.GetAccount(context.Background(), ownerID, accountID)
	if err != nil {
		return account.Account{}, err
	}
	archived, err := financialAccount.Archive(now)
	if err != nil {
		return account.Account{}, err
	}
	r.accounts[accountID] = archived
	return archived, nil
}

func (r *memoryRepository) PostManualTransaction(_ context.Context, ownerID string, draft ManualTransactionDraft) (domainledger.Transaction, error) {
	financialAccount, err := r.GetAccount(context.Background(), ownerID, draft.AccountID)
	if err != nil {
		return domainledger.Transaction{}, err
	}
	if financialAccount.Status().IsArchived() {
		return domainledger.Transaction{}, account.ErrAccountArchived
	}
	r.nextSequence++
	posted, err := domainledger.NewTransaction(
		draft.ID, ownerID, draft.AccountID, domainledger.ManualKind(), draft.Amount, draft.Effect, draft.FinancialDate,
		r.nextSequence, draft.Memo, "", draft.CreatedAt,
	)
	if err != nil {
		return domainledger.Transaction{}, err
	}
	r.transactions = append(r.transactions, posted)
	return posted, nil
}

func (r *memoryRepository) ListTransactions(_ context.Context, ownerID, accountID string) ([]domainledger.Transaction, error) {
	if _, err := r.GetAccount(context.Background(), ownerID, accountID); err != nil {
		return nil, err
	}
	result := make([]domainledger.Transaction, 0)
	for _, transaction := range r.transactions {
		if transaction.OwnerID() == ownerID && transaction.AccountID() == accountID {
			result = append(result, transaction)
		}
	}
	return result, nil
}

func (r *memoryRepository) CreateSnapshot(_ context.Context, snapshot domainledger.BalanceSnapshot, _ MutationIdentity) (domainledger.BalanceSnapshot, error) {
	financialAccount, err := r.GetAccount(context.Background(), snapshot.OwnerID(), snapshot.AccountID())
	if err != nil {
		return domainledger.BalanceSnapshot{}, err
	}
	if financialAccount.Status().IsArchived() {
		return domainledger.BalanceSnapshot{}, account.ErrAccountArchived
	}
	r.snapshots[snapshot.AccountID()] = snapshot
	return snapshot, nil
}

func (r *memoryRepository) LoadBalanceState(_ context.Context, ownerID, accountID string) (BalanceState, error) {
	financialAccount, err := r.GetAccount(context.Background(), ownerID, accountID)
	if err != nil {
		return BalanceState{}, err
	}
	state := BalanceState{Account: financialAccount}
	cutoff := int64(0)
	if snapshot, exists := r.snapshots[accountID]; exists {
		state.Snapshot = &snapshot
		cutoff = snapshot.CutoffSequence()
	}
	for _, transaction := range r.transactions {
		if transaction.OwnerID() == ownerID && transaction.AccountID() == accountID && transaction.LedgerSequence() > cutoff {
			state.Transactions = append(state.Transactions, transaction)
		}
	}
	return state, nil
}

func (r *memoryRepository) CreateTransfer(_ context.Context, ownerID string, draft TransferDraft) (TransferResult, error) {
	source, err := r.GetAccount(context.Background(), ownerID, draft.Transfer.SourceAccountID())
	if err != nil {
		return TransferResult{}, err
	}
	destination, err := r.GetAccount(context.Background(), ownerID, draft.Transfer.DestinationAccountID())
	if err != nil {
		return TransferResult{}, err
	}
	if source.Status().IsArchived() || destination.Status().IsArchived() {
		return TransferResult{}, account.ErrAccountArchived
	}
	sourceEffect, destinationEffect, err := domainledger.TransferEffects(source, destination)
	if err != nil {
		return TransferResult{}, err
	}
	r.nextSequence++
	sourceTransaction, err := domainledger.NewTransaction(
		draft.SourceTransactionID, ownerID, source.ID(), domainledger.TransferKind(), draft.Transfer.Amount(), sourceEffect,
		draft.Transfer.FinancialDate(), r.nextSequence, draft.Transfer.Memo(), draft.Transfer.ID(), draft.Transfer.CreatedAt(),
	)
	if err != nil {
		return TransferResult{}, err
	}
	r.nextSequence++
	destinationTransaction, err := domainledger.NewTransaction(
		draft.DestinationTransactionID, ownerID, destination.ID(), domainledger.TransferKind(), draft.Transfer.Amount(), destinationEffect,
		draft.Transfer.FinancialDate(), r.nextSequence, draft.Transfer.Memo(), draft.Transfer.ID(), draft.Transfer.CreatedAt(),
	)
	if err != nil {
		return TransferResult{}, err
	}
	r.transactions = append(r.transactions, sourceTransaction, destinationTransaction)
	return TransferResult{Transfer: draft.Transfer, SourceTransaction: sourceTransaction, DestinationTransaction: destinationTransaction}, nil
}

func newTestService(t *testing.T, repository Repository) *Service {
	t.Helper()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	nextID := 0
	service, err := NewService(repository, ServiceOptions{
		Clock: func() time.Time { return now },
		IDGenerator: func() (string, error) {
			nextID++
			return fmt.Sprintf("id-%03d", nextID), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func testKey(t *testing.T, value string) IdempotencyKey {
	t.Helper()
	key, err := ParseIdempotencyKey(value)
	if err != nil {
		t.Fatal(err)
	}
	return key
}
