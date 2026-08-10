package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"runway/backend/internal/auth"
	"runway/backend/internal/domain/account"
	"runway/backend/internal/domain/financialdate"
	domainledger "runway/backend/internal/domain/ledger"
	"runway/backend/internal/domain/money"
	applicationledger "runway/backend/internal/ledger"
)

func TestLedgerRepositoryReconstructionAndLinkedTransfers(t *testing.T) {
	pool := newIsolatedTestDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	createTestOwner(t, pool, "owner", now)

	ids := sequentialIDs()
	service, err := applicationledger.NewService(NewLedgerRepository(pool), applicationledger.ServiceOptions{
		Clock: func() time.Time { return now }, IDGenerator: ids,
	})
	if err != nil {
		t.Fatal(err)
	}
	bank := createTestAccount(t, service, "owner", "Bank", account.Bank())
	cash := createTestAccount(t, service, "owner", "Cash", account.Cash())
	card := createTestAccount(t, service, "owner", "Card", account.CreditCard())
	_ = createTestAccount(t, service, "owner", "Loan", account.Loan())

	if _, err := service.GetAccount(ctx, "different-owner", bank.ID()); !errors.Is(err, applicationledger.ErrNotFound) {
		t.Fatalf("cross-owner GetAccount() error = %v", err)
	}
	date, _ := financialdate.Parse("2026-08-10")
	validationAmount, _ := money.New(1, money.MXN())
	if _, err := service.PostTransaction(ctx, "owner", bank.ID(), domainledger.LiabilityCharge(), validationAmount, date, "", testIdempotencyKey(t, "invalid-bank-effect")); !errors.Is(err, domainledger.ErrIncompatibleEffect) {
		t.Fatalf("liability effect on bank error = %v", err)
	}
	if _, err := service.PostTransaction(ctx, "owner", card.ID(), domainledger.AssetInflow(), validationAmount, date, "", testIdempotencyKey(t, "invalid-card-effect")); !errors.Is(err, domainledger.ErrIncompatibleEffect) {
		t.Fatalf("asset effect on card error = %v", err)
	}
	zeroAmount, _ := money.Zero(money.MXN())
	if _, err := service.PostTransaction(ctx, "owner", bank.ID(), domainledger.AssetInflow(), zeroAmount, date, "", testIdempotencyKey(t, "zero")); !errors.Is(err, domainledger.ErrZeroAmount) {
		t.Fatalf("zero transaction error = %v", err)
	}
	if _, err := service.PostTransaction(ctx, "owner", bank.ID(), domainledger.AssetInflow(), money.Money{}, date, "", testIdempotencyKey(t, "bad-currency")); !errors.Is(err, money.ErrCurrencyMismatch) {
		t.Fatalf("invalid transaction currency error = %v", err)
	}
	if _, err := service.CreateSnapshot(ctx, "owner", bank.ID(), money.Balance{}, now, 0, testIdempotencyKey(t, "bad-snapshot")); !errors.Is(err, money.ErrCurrencyMismatch) {
		t.Fatalf("invalid snapshot currency error = %v", err)
	}
	first := postTestTransaction(t, service, bank.ID(), domainledger.AssetInflow(), 100_000, date)
	second := postTestTransaction(t, service, bank.ID(), domainledger.AssetOutflow(), 10_000, date)
	if second.LedgerSequence() <= first.LedgerSequence() {
		t.Fatalf("sequences = %d, %d", first.LedgerSequence(), second.LedgerSequence())
	}

	snapshotBalance, _ := money.NewBalance(90_000, money.MXN())
	snapshot, err := service.CreateSnapshot(ctx, "owner", bank.ID(), snapshotBalance, now, second.LedgerSequence(), testIdempotencyKey(t, "snapshot"))
	if err != nil {
		t.Fatalf("CreateSnapshot() error = %v", err)
	}
	correctedBalance, _ := money.NewBalance(91_000, money.MXN())
	correctedSnapshot, err := service.CreateSnapshot(ctx, "owner", bank.ID(), correctedBalance, now, second.LedgerSequence(), testIdempotencyKey(t, "corrected-snapshot"))
	if err != nil {
		t.Fatalf("corrected CreateSnapshot() error = %v", err)
	}
	third := postTestTransaction(t, service, bank.ID(), domainledger.AssetInflow(), 5_000, date)
	result, err := service.CurrentBalance(ctx, "owner", bank.ID())
	if err != nil {
		t.Fatalf("CurrentBalance() error = %v", err)
	}
	if result.Balance.MinorUnits() != 96_000 || result.SnapshotID != correctedSnapshot.ID() || result.SnapshotID == snapshot.ID() || result.AppliedTransactions != 1 || result.LastAppliedSequence != third.LedgerSequence() {
		t.Fatalf("balance result = %+v", result)
	}

	unknownCutoffBalance, _ := money.NewBalance(0, money.MXN())
	if _, err := service.CreateSnapshot(ctx, "owner", cash.ID(), unknownCutoffBalance, now, first.LedgerSequence(), testIdempotencyKey(t, "cross-cutoff")); !errors.Is(err, applicationledger.ErrSnapshotCutoffUnknown) {
		t.Fatalf("cross-account cutoff error = %v", err)
	}
	if _, err := service.CreateSnapshot(ctx, "owner", bank.ID(), snapshotBalance, now, first.LedgerSequence(), testIdempotencyKey(t, "regressed-cutoff")); !errors.Is(err, applicationledger.ErrSnapshotCutoffRegression) {
		t.Fatalf("regressed cutoff error = %v", err)
	}

	cardCharge := postTestTransaction(t, service, card.ID(), domainledger.LiabilityCharge(), 10_000, date)
	transferAmount, _ := money.New(20_000, money.MXN())
	assetTransfer, err := service.CreateTransfer(ctx, "owner", bank.ID(), cash.ID(), transferAmount, date, "ATM transfer", testIdempotencyKey(t, "atm-transfer"))
	if err != nil {
		t.Fatalf("bank to cash transfer error = %v", err)
	}
	if assetTransfer.SourceTransaction.TransferID() != assetTransfer.Transfer.ID() || assetTransfer.DestinationTransaction.TransferID() != assetTransfer.Transfer.ID() {
		t.Fatal("asset transfer entries do not share transfer identity")
	}
	if assetTransfer.SourceTransaction.Effect() != domainledger.AssetOutflow() || assetTransfer.DestinationTransaction.Effect() != domainledger.AssetInflow() {
		t.Fatal("asset transfer effects are incorrect")
	}
	secondBank := createTestAccount(t, service, "owner", "Savings", account.Bank())
	bankTransferAmount, _ := money.New(100, money.MXN())
	if _, err := service.CreateTransfer(ctx, "owner", bank.ID(), secondBank.ID(), bankTransferAmount, date, "Savings transfer", testIdempotencyKey(t, "savings-transfer")); err != nil {
		t.Fatalf("bank to bank transfer error = %v", err)
	}
	if _, err := service.CreateTransfer(ctx, "owner", bank.ID(), bank.ID(), bankTransferAmount, date, "", testIdempotencyKey(t, "same-transfer")); !errors.Is(err, domainledger.ErrSameTransferAccount) {
		t.Fatalf("same-account transfer error = %v", err)
	}
	if _, err := service.CreateTransfer(ctx, "different-owner", bank.ID(), cash.ID(), bankTransferAmount, date, "", testIdempotencyKey(t, "foreign-transfer")); !errors.Is(err, applicationledger.ErrNotFound) {
		t.Fatalf("cross-owner transfer error = %v", err)
	}

	paymentAmount, _ := money.New(5_000, money.MXN())
	cardPayment, err := service.CreateTransfer(ctx, "owner", bank.ID(), card.ID(), paymentAmount, date, "Card payment", testIdempotencyKey(t, "card-payment"))
	if err != nil {
		t.Fatalf("bank to card transfer error = %v", err)
	}
	if cardPayment.DestinationTransaction.Effect() != domainledger.LiabilityPayment() || cardPayment.DestinationTransaction.LedgerSequence() <= cardCharge.LedgerSequence() {
		t.Fatal("card payment transaction is incorrect")
	}
	bankBalance, _ := service.CurrentBalance(ctx, "owner", bank.ID())
	cashBalance, _ := service.CurrentBalance(ctx, "owner", cash.ID())
	cardBalance, _ := service.CurrentBalance(ctx, "owner", card.ID())
	secondBankBalance, _ := service.CurrentBalance(ctx, "owner", secondBank.ID())
	if bankBalance.Balance.MinorUnits() != 70_900 || cashBalance.Balance.MinorUnits() != 20_000 || cardBalance.Balance.MinorUnits() != 5_000 || secondBankBalance.Balance.MinorUnits() != 100 {
		t.Fatalf("post-transfer balances = bank %d cash %d card %d", bankBalance.Balance.MinorUnits(), cashBalance.Balance.MinorUnits(), cardBalance.Balance.MinorUnits())
	}

	transactions, err := service.ListTransactions(ctx, "owner", bank.ID())
	if err != nil {
		t.Fatal(err)
	}
	for index := 1; index < len(transactions); index++ {
		if transactions[index].LedgerSequence() <= transactions[index-1].LedgerSequence() {
			t.Fatal("transactions are not in stable ledger sequence order")
		}
	}
}

func TestLinkedTransferRollsBackBothEntriesWhenOneInsertFails(t *testing.T) {
	pool := newIsolatedTestDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	createTestOwner(t, pool, "owner", now)
	repository := NewLedgerRepository(pool)
	bank, _ := account.New("bank", "owner", "Bank", account.Bank(), money.MXN(), now)
	cash, _ := account.New("cash", "owner", "Cash", account.Cash(), money.MXN(), now)
	if err := repository.CreateAccount(ctx, bank); err != nil {
		t.Fatal(err)
	}
	if err := repository.CreateAccount(ctx, cash); err != nil {
		t.Fatal(err)
	}
	date, _ := financialdate.Parse("2026-08-10")
	amount, _ := money.New(100, money.MXN())
	existing, err := repository.PostManualTransaction(ctx, "owner", applicationledger.ManualTransactionDraft{
		ID: "existing", AccountID: bank.ID(), Amount: amount,
		Effect: domainledger.AssetInflow(), FinancialDate: date, CreatedAt: now, Mutation: testMutation(t, "existing"),
	})
	if err != nil {
		t.Fatal(err)
	}
	transfer, _ := domainledger.NewTransfer("failed-transfer", "owner", bank.ID(), cash.ID(), amount, date, "", now)
	_, err = repository.CreateTransfer(ctx, "owner", applicationledger.TransferDraft{
		Transfer: transfer, SourceTransactionID: "rolled-back-source", DestinationTransactionID: existing.ID(), Mutation: testMutation(t, "failed-transfer"),
	})
	if !errors.Is(err, applicationledger.ErrConflict) {
		t.Fatalf("CreateTransfer() error = %v", err)
	}
	for query, value := range map[string]string{
		"SELECT count(*) FROM linked_transfers WHERE id = $1":       transfer.ID(),
		"SELECT count(*) FROM financial_transactions WHERE id = $1": "rolled-back-source",
	} {
		var count int
		if err := pool.QueryRow(ctx, query, value).Scan(&count); err != nil || count != 0 {
			t.Fatalf("rollback check count = %d, error = %v", count, err)
		}
	}
}

func TestLedgerSequenceDoesNotCollideUnderConcurrentInserts(t *testing.T) {
	pool := newIsolatedTestDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	createTestOwner(t, pool, "owner", now)
	repository := NewLedgerRepository(pool)
	bank, _ := account.New("bank", "owner", "Bank", account.Bank(), money.MXN(), now)
	if err := repository.CreateAccount(ctx, bank); err != nil {
		t.Fatal(err)
	}
	date, _ := financialdate.Parse("2026-08-10")
	amount, _ := money.New(1, money.MXN())

	const count = 12
	sequences := make(chan int64, count)
	errorsFound := make(chan error, count)
	var wait sync.WaitGroup
	for index := 0; index < count; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			posted, err := repository.PostManualTransaction(ctx, "owner", applicationledger.ManualTransactionDraft{
				ID: fmt.Sprintf("concurrent-%02d", index), AccountID: bank.ID(), Amount: amount,
				Effect: domainledger.AssetInflow(), FinancialDate: date, CreatedAt: now, Mutation: testMutation(t, fmt.Sprintf("concurrent-%02d", index)),
			})
			if err != nil {
				errorsFound <- err
				return
			}
			sequences <- posted.LedgerSequence()
		}(index)
	}
	wait.Wait()
	close(sequences)
	close(errorsFound)
	for err := range errorsFound {
		t.Errorf("concurrent PostTransaction() error = %v", err)
	}
	seen := make(map[int64]bool, count)
	for sequence := range sequences {
		if seen[sequence] {
			t.Fatalf("duplicate ledger sequence %d", sequence)
		}
		seen[sequence] = true
	}
	if len(seen) != count {
		t.Fatalf("sequence count = %d, want %d", len(seen), count)
	}
}

func TestArchivedAccountRejectsNewLedgerWritesButRemainsReadable(t *testing.T) {
	pool := newIsolatedTestDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	createTestOwner(t, pool, "owner", now)
	service, _ := applicationledger.NewService(NewLedgerRepository(pool), applicationledger.ServiceOptions{
		Clock: func() time.Time { return now }, IDGenerator: sequentialIDs(),
	})
	bank := createTestAccount(t, service, "owner", "Bank", account.Bank())
	cash := createTestAccount(t, service, "owner", "Cash", account.Cash())
	if _, err := service.ArchiveAccount(ctx, "owner", bank.ID()); err != nil {
		t.Fatal(err)
	}
	if loaded, err := service.GetAccount(ctx, "owner", bank.ID()); err != nil || !loaded.Status().IsArchived() {
		t.Fatalf("archived GetAccount() = %+v, %v", loaded, err)
	}
	date, _ := financialdate.Parse("2026-08-10")
	amount, _ := money.New(1, money.MXN())
	if _, err := service.PostTransaction(ctx, "owner", bank.ID(), domainledger.AssetInflow(), amount, date, "", testIdempotencyKey(t, "archived-post")); !errors.Is(err, account.ErrAccountArchived) {
		t.Fatalf("archived PostTransaction() error = %v", err)
	}
	balance, _ := money.ZeroBalance(money.MXN())
	if _, err := service.CreateSnapshot(ctx, "owner", bank.ID(), balance, now, 0, testIdempotencyKey(t, "archived-snapshot")); !errors.Is(err, account.ErrAccountArchived) {
		t.Fatalf("archived CreateSnapshot() error = %v", err)
	}
	if _, err := service.CreateTransfer(ctx, "owner", bank.ID(), cash.ID(), amount, date, "", testIdempotencyKey(t, "archived-transfer")); !errors.Is(err, account.ErrAccountArchived) {
		t.Fatalf("archived CreateTransfer() error = %v", err)
	}
	activeBank := createTestAccount(t, service, "owner", "Active bank", account.Bank())
	if _, err := service.ArchiveAccount(ctx, "owner", cash.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateTransfer(ctx, "owner", activeBank.ID(), cash.ID(), amount, date, "", testIdempotencyKey(t, "archived-destination-transfer")); !errors.Is(err, account.ErrAccountArchived) {
		t.Fatalf("archived destination CreateTransfer() error = %v", err)
	}
}

func TestLedgerForeignKeysRejectOrphansAndProtectHistory(t *testing.T) {
	pool := newIsolatedTestDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	createTestOwner(t, pool, "owner", now)
	repository := NewLedgerRepository(pool)
	bank, _ := account.New("bank", "owner", "Bank", account.Bank(), money.MXN(), now)
	if err := repository.CreateAccount(ctx, bank); err != nil {
		t.Fatal(err)
	}
	date, _ := financialdate.Parse("2026-08-10")
	amount, _ := money.New(1, money.MXN())
	if _, err := repository.PostManualTransaction(ctx, "owner", applicationledger.ManualTransactionDraft{
		ID: "transaction", AccountID: bank.ID(), Amount: amount,
		Effect: domainledger.AssetInflow(), FinancialDate: date, CreatedAt: now, Mutation: testMutation(t, "transaction"),
	}); err != nil {
		t.Fatal(err)
	}

	_, err := pool.Exec(ctx, `
		INSERT INTO accounts (id, owner_id, display_name, account_type, currency, status, created_at, updated_at)
		VALUES ('orphan', 'missing-owner', 'Orphan', 'bank', 'MXN', 'active', $1, $1)`, now)
	if !hasPostgresCode(err, "23503") {
		t.Fatalf("orphan account error = %v, want foreign-key violation", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM accounts WHERE owner_id = 'owner' AND id = 'bank'`); !hasPostgresCode(err, "23503") {
		t.Fatalf("delete account with history error = %v, want foreign-key violation", err)
	}
	var transactionCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM financial_transactions WHERE id = 'transaction'`).Scan(&transactionCount); err != nil || transactionCount != 1 {
		t.Fatalf("protected transaction count = %d, error = %v", transactionCount, err)
	}
}

func TestLiabilityPaymentsCannotCreateNegativeAmountOwed(t *testing.T) {
	pool := newIsolatedTestDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	createTestOwner(t, pool, "owner", now)
	service, _ := applicationledger.NewService(NewLedgerRepository(pool), applicationledger.ServiceOptions{Clock: func() time.Time { return now }, IDGenerator: sequentialIDs()})
	bank := createTestAccount(t, service, "owner", "Bank", account.Bank())
	card := createTestAccount(t, service, "owner", "Card", account.CreditCard())
	date, _ := financialdate.Parse("2026-08-10")
	oneHundred, _ := money.New(10_000, money.MXN())
	oneHundredAndOne, _ := money.New(10_001, money.MXN())

	if _, err := service.PostTransaction(ctx, "owner", card.ID(), domainledger.LiabilityPayment(), oneHundred, date, "", testIdempotencyKey(t, "standalone-overpayment")); !errors.Is(err, applicationledger.ErrLiabilityOverpayment) {
		t.Fatalf("standalone overpayment error = %v", err)
	}
	if _, err := service.PostTransaction(ctx, "owner", card.ID(), domainledger.LiabilityCharge(), oneHundred, date, "", testIdempotencyKey(t, "charge-for-payoff")); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PostTransaction(ctx, "owner", card.ID(), domainledger.LiabilityPayment(), oneHundred, date, "", testIdempotencyKey(t, "exact-payoff")); err != nil {
		t.Fatalf("exact payoff error = %v", err)
	}
	paidOff, err := service.CurrentBalance(ctx, "owner", card.ID())
	if err != nil || paidOff.Balance.MinorUnits() != 0 {
		t.Fatalf("exact payoff balance = %d, error = %v", paidOff.Balance.MinorUnits(), err)
	}

	if _, err := service.PostTransaction(ctx, "owner", card.ID(), domainledger.LiabilityCharge(), oneHundred, date, "", testIdempotencyKey(t, "charge-for-transfer")); err != nil {
		t.Fatal(err)
	}
	bankBefore, _ := service.CurrentBalance(ctx, "owner", bank.ID())
	if _, err := service.CreateTransfer(ctx, "owner", bank.ID(), card.ID(), oneHundredAndOne, date, "", testIdempotencyKey(t, "transfer-overpayment")); !errors.Is(err, applicationledger.ErrLiabilityOverpayment) {
		t.Fatalf("transfer overpayment error = %v", err)
	}
	bankAfter, _ := service.CurrentBalance(ctx, "owner", bank.ID())
	cardAfter, _ := service.CurrentBalance(ctx, "owner", card.ID())
	if bankAfter.Balance.MinorUnits() != bankBefore.Balance.MinorUnits() || cardAfter.Balance.MinorUnits() != 10_000 {
		t.Fatalf("overpayment mutated balances: bank %d -> %d, card = %d", bankBefore.Balance.MinorUnits(), bankAfter.Balance.MinorUnits(), cardAfter.Balance.MinorUnits())
	}

	negative, _ := money.NewBalance(-1, money.MXN())
	if _, err := service.CreateSnapshot(ctx, "owner", card.ID(), negative, now, 0, testIdempotencyKey(t, "negative-card-snapshot")); !errors.Is(err, applicationledger.ErrNegativeLiabilitySnapshot) {
		t.Fatalf("negative liability snapshot error = %v", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO balance_snapshots
			(id, owner_id, account_id, balance_minor, currency, effective_at, cutoff_sequence, created_at)
		VALUES ('raw-negative-card-snapshot', 'owner', $1, -1, 'MXN', $2, 0, $2)`, card.ID(), now)
	if !hasPostgresCode(err, "23514") {
		t.Fatalf("raw negative liability snapshot error = %v", err)
	}
}

func TestFinancialMutationIdempotency(t *testing.T) {
	pool := newIsolatedTestDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	createTestOwner(t, pool, "owner", now)
	service, _ := applicationledger.NewService(NewLedgerRepository(pool), applicationledger.ServiceOptions{Clock: func() time.Time { return now }, IDGenerator: sequentialIDs()})
	bank := createTestAccount(t, service, "owner", "Bank", account.Bank())
	cash := createTestAccount(t, service, "owner", "Cash", account.Cash())
	date, _ := financialdate.Parse("2026-08-10")
	amount, _ := money.New(100, money.MXN())
	differentAmount, _ := money.New(101, money.MXN())

	transactionKey := testIdempotencyKey(t, "same-transaction")
	first, err := service.PostTransaction(ctx, "owner", bank.ID(), domainledger.AssetInflow(), amount, date, " Deposit ", transactionKey)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := service.PostTransaction(ctx, "owner", bank.ID(), domainledger.AssetInflow(), amount, date, "Deposit", transactionKey)
	if err != nil || replayed.ID() != first.ID() || replayed.LedgerSequence() != first.LedgerSequence() {
		t.Fatalf("transaction replay = %+v, error = %v", replayed, err)
	}
	if _, err := service.PostTransaction(ctx, "owner", bank.ID(), domainledger.AssetInflow(), differentAmount, date, "Deposit", transactionKey); !errors.Is(err, applicationledger.ErrIdempotencyConflict) {
		t.Fatalf("changed transaction replay error = %v", err)
	}

	zero, _ := money.ZeroBalance(money.MXN())
	snapshotKey := testIdempotencyKey(t, "same-snapshot")
	firstSnapshot, err := service.CreateSnapshot(ctx, "owner", cash.ID(), zero, now, 0, snapshotKey)
	if err != nil {
		t.Fatal(err)
	}
	replayedSnapshot, err := service.CreateSnapshot(ctx, "owner", cash.ID(), zero, now, 0, snapshotKey)
	if err != nil || replayedSnapshot.ID() != firstSnapshot.ID() {
		t.Fatalf("snapshot replay = %+v, error = %v", replayedSnapshot, err)
	}
	one, _ := money.NewBalance(1, money.MXN())
	if _, err := service.CreateSnapshot(ctx, "owner", cash.ID(), one, now, 0, snapshotKey); !errors.Is(err, applicationledger.ErrIdempotencyConflict) {
		t.Fatalf("changed snapshot replay error = %v", err)
	}

	transferKey := testIdempotencyKey(t, "same-transfer")
	firstTransfer, err := service.CreateTransfer(ctx, "owner", bank.ID(), cash.ID(), amount, date, "Cash", transferKey)
	if err != nil {
		t.Fatal(err)
	}
	replayedTransfer, err := service.CreateTransfer(ctx, "owner", bank.ID(), cash.ID(), amount, date, "Cash", transferKey)
	if err != nil || replayedTransfer.Transfer.ID() != firstTransfer.Transfer.ID() {
		t.Fatalf("transfer replay = %+v, error = %v", replayedTransfer, err)
	}
	if _, err := service.CreateTransfer(ctx, "owner", bank.ID(), cash.ID(), differentAmount, date, "Cash", transferKey); !errors.Is(err, applicationledger.ErrIdempotencyConflict) {
		t.Fatalf("changed transfer replay error = %v", err)
	}
	if _, err := service.ArchiveAccount(ctx, "owner", bank.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ArchiveAccount(ctx, "owner", cash.ID()); err != nil {
		t.Fatal(err)
	}
	if afterArchive, err := service.PostTransaction(ctx, "owner", bank.ID(), domainledger.AssetInflow(), amount, date, "Deposit", transactionKey); err != nil || afterArchive.ID() != first.ID() {
		t.Fatalf("transaction replay after archive = %+v, error = %v", afterArchive, err)
	}
	if afterArchive, err := service.CreateSnapshot(ctx, "owner", cash.ID(), zero, now, 0, snapshotKey); err != nil || afterArchive.ID() != firstSnapshot.ID() {
		t.Fatalf("snapshot replay after archive = %+v, error = %v", afterArchive, err)
	}
	if afterArchive, err := service.CreateTransfer(ctx, "owner", bank.ID(), cash.ID(), amount, date, "Cash", transferKey); err != nil || afterArchive.Transfer.ID() != firstTransfer.Transfer.ID() {
		t.Fatalf("transfer replay after archive = %+v, error = %v", afterArchive, err)
	}

	var transactionCount, transferCount, transferEntryCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM financial_transactions WHERE id = $1`, first.ID()).Scan(&transactionCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM linked_transfers WHERE id = $1`, firstTransfer.Transfer.ID()).Scan(&transferCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM financial_transactions WHERE transfer_id = $1`, firstTransfer.Transfer.ID()).Scan(&transferEntryCount); err != nil {
		t.Fatal(err)
	}
	if transactionCount != 1 || transferCount != 1 || transferEntryCount != 2 {
		t.Fatalf("idempotent row counts = transaction %d, transfer %d, entries %d", transactionCount, transferCount, transferEntryCount)
	}
}

func TestConcurrentDuplicateMutationsPersistOnce(t *testing.T) {
	pool := newIsolatedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	createTestOwner(t, pool, "owner", now)
	service, _ := applicationledger.NewService(NewLedgerRepository(pool), applicationledger.ServiceOptions{Clock: func() time.Time { return now }, IDGenerator: sequentialIDs()})
	bank := createTestAccount(t, service, "owner", "Bank", account.Bank())
	cash := createTestAccount(t, service, "owner", "Cash", account.Cash())
	date, _ := financialdate.Parse("2026-08-10")
	amount, _ := money.New(100, money.MXN())

	runDuplicate := func(operation func() (string, error)) []string {
		results := make(chan string, 2)
		errorsFound := make(chan error, 2)
		var wait sync.WaitGroup
		for i := 0; i < 2; i++ {
			wait.Add(1)
			go func() {
				defer wait.Done()
				id, err := operation()
				if err != nil {
					errorsFound <- err
					return
				}
				results <- id
			}()
		}
		wait.Wait()
		close(results)
		close(errorsFound)
		for err := range errorsFound {
			t.Errorf("concurrent duplicate error = %v", err)
		}
		ids := make([]string, 0, 2)
		for id := range results {
			ids = append(ids, id)
		}
		return ids
	}

	transactionKey := testIdempotencyKey(t, "concurrent-transaction")
	transactionIDs := runDuplicate(func() (string, error) {
		posted, err := service.PostTransaction(ctx, "owner", bank.ID(), domainledger.AssetInflow(), amount, date, "", transactionKey)
		return posted.ID(), err
	})
	if len(transactionIDs) != 2 || transactionIDs[0] != transactionIDs[1] {
		t.Fatalf("concurrent transaction IDs = %v", transactionIDs)
	}

	transferKey := testIdempotencyKey(t, "concurrent-transfer")
	transferIDs := runDuplicate(func() (string, error) {
		result, err := service.CreateTransfer(ctx, "owner", bank.ID(), cash.ID(), amount, date, "", transferKey)
		return result.Transfer.ID(), err
	})
	if len(transferIDs) != 2 || transferIDs[0] != transferIDs[1] {
		t.Fatalf("concurrent transfer IDs = %v", transferIDs)
	}

	var manualCount, transferCount, transferEntries int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM financial_transactions WHERE transaction_kind = 'manual'`).Scan(&manualCount)
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM linked_transfers`).Scan(&transferCount)
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM financial_transactions WHERE transaction_kind = 'transfer'`).Scan(&transferEntries)
	if manualCount != 1 || transferCount != 1 || transferEntries != 2 {
		t.Fatalf("concurrent duplicate counts = manual %d transfer %d entries %d", manualCount, transferCount, transferEntries)
	}
}

func TestDatabaseProtectsLedgerHistoryAndOwnership(t *testing.T) {
	pool := newIsolatedTestDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	createTestOwner(t, pool, "owner", now)
	service, _ := applicationledger.NewService(NewLedgerRepository(pool), applicationledger.ServiceOptions{Clock: func() time.Time { return now }, IDGenerator: sequentialIDs()})
	bank := createTestAccount(t, service, "owner", "Bank", account.Bank())
	cash := createTestAccount(t, service, "owner", "Cash", account.Cash())
	date, _ := financialdate.Parse("2026-08-10")
	posted := postTestTransaction(t, service, bank.ID(), domainledger.AssetInflow(), 100, date)

	if _, err := pool.Exec(ctx, `UPDATE financial_transactions SET memo = 'changed' WHERE id = $1`, posted.ID()); !hasPostgresCode(err, "55000") {
		t.Fatalf("transaction UPDATE error = %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM financial_transactions WHERE id = $1`, posted.ID()); !hasPostgresCode(err, "55000") {
		t.Fatalf("transaction DELETE error = %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE accounts SET account_type = 'cash' WHERE id = $1`, bank.ID()); !hasPostgresCode(err, "55000") {
		t.Fatalf("account type UPDATE error = %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE accounts SET currency = 'USD' WHERE id = $1`, bank.ID()); !hasPostgresCode(err, "55000") {
		t.Fatalf("account currency UPDATE error = %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE accounts SET display_name = 'Renamed', status = 'archived', updated_at = $2 WHERE id = $1`, bank.ID(), now.Add(time.Minute)); err != nil {
		t.Fatalf("permitted account metadata UPDATE error = %v", err)
	}

	_, err := pool.Exec(ctx, `
		INSERT INTO financial_transactions
			(id, owner_id, account_id, transaction_kind, amount_minor, currency, effect, financial_date, memo, created_at)
		VALUES ('wrong-owner-tx', 'missing-owner', $1, 'manual', 1, 'MXN', 'asset_inflow', '2026-08-10', '', $2)`, cash.ID(), now)
	if !hasPostgresCode(err, "23503") {
		t.Fatalf("cross-owner transaction error = %v", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO linked_transfers
			(id, owner_id, source_account_id, destination_account_id, amount_minor, currency, financial_date, memo, created_at)
		VALUES ('wrong-owner-transfer', 'missing-owner', $1, $2, 1, 'MXN', '2026-08-10', '', $3)`, bank.ID(), cash.ID(), now)
	if !hasPostgresCode(err, "23503") {
		t.Fatalf("cross-owner transfer error = %v", err)
	}

	activeBank := createTestAccount(t, service, "owner", "Transfer bank", account.Bank())
	amount, _ := money.New(1, money.MXN())
	transfer, err := service.CreateTransfer(ctx, "owner", activeBank.ID(), cash.ID(), amount, date, "", testIdempotencyKey(t, "owner-linked-transfer"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO financial_transactions
			(id, owner_id, account_id, transaction_kind, amount_minor, currency, effect, financial_date, memo, transfer_id, created_at)
		VALUES ('wrong-owner-link', 'missing-owner', $1, 'transfer', 1, 'MXN', 'asset_outflow', '2026-08-10', '', $2, $3)`, activeBank.ID(), transfer.Transfer.ID(), now)
	if !hasPostgresCode(err, "23503") {
		t.Fatalf("inconsistent transfer ownership error = %v", err)
	}
}

func TestTransferLinkageCannotBeContradictedOrExtended(t *testing.T) {
	pool := newIsolatedTestDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	createTestOwner(t, pool, "owner", now)
	service, _ := applicationledger.NewService(NewLedgerRepository(pool), applicationledger.ServiceOptions{Clock: func() time.Time { return now }, IDGenerator: sequentialIDs()})
	bank := createTestAccount(t, service, "owner", "Bank", account.Bank())
	cash := createTestAccount(t, service, "owner", "Cash", account.Cash())
	third := createTestAccount(t, service, "owner", "Third", account.Bank())
	date, _ := financialdate.Parse("2026-08-10")
	amount, _ := money.New(100, money.MXN())
	result, err := service.CreateTransfer(ctx, "owner", bank.ID(), cash.ID(), amount, date, "", testIdempotencyKey(t, "protected-transfer"))
	if err != nil {
		t.Fatal(err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO financial_transactions
			(id, owner_id, account_id, transaction_kind, amount_minor, currency, effect, financial_date, memo, transfer_id, created_at)
		VALUES ('third-transfer-entry', 'owner', $1, 'transfer', 100, 'MXN', 'asset_inflow', '2026-08-10', '', $2, $3)`, third.ID(), result.Transfer.ID(), now)
	if !hasPostgresCode(err, "23514") {
		t.Fatalf("third transfer entry error = %v", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO financial_transactions
			(id, owner_id, account_id, transaction_kind, amount_minor, currency, effect, financial_date, memo, transfer_id, created_at)
		VALUES ('contradictory-transfer-entry', 'owner', $1, 'transfer', 101, 'MXN', 'asset_outflow', '2026-08-10', '', $2, $3)`, bank.ID(), result.Transfer.ID(), now)
	if !hasPostgresCode(err, "23514") && !hasPostgresCode(err, "23505") {
		t.Fatalf("contradictory transfer entry error = %v", err)
	}

	manual, err := service.PostTransaction(ctx, "owner", cash.ID(), domainledger.AssetInflow(), amount, date, "", testIdempotencyKey(t, "manual-after-transfer"))
	if err != nil || manual.Kind() != domainledger.ManualKind() || manual.TransferID() != "" {
		t.Fatalf("manual posting boundary = %+v, error = %v", manual, err)
	}
	var linkedCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM financial_transactions WHERE transfer_id = $1`, result.Transfer.ID()).Scan(&linkedCount); err != nil || linkedCount != 2 {
		t.Fatalf("linked entry count = %d, error = %v", linkedCount, err)
	}
}

func TestTransactionAndSnapshotSerializationCountsEveryEntryOnce(t *testing.T) {
	pool := newIsolatedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	createTestOwner(t, pool, "owner", now)
	service, _ := applicationledger.NewService(NewLedgerRepository(pool), applicationledger.ServiceOptions{Clock: func() time.Time { return now }, IDGenerator: sequentialIDs()})
	bank := createTestAccount(t, service, "owner", "Bank", account.Bank())
	date, _ := financialdate.Parse("2026-08-10")
	first := postTestTransaction(t, service, bank.ID(), domainledger.AssetInflow(), 100, date)

	blockingTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blockingTx.Rollback(context.Background())
	if _, err := blockingTx.Exec(ctx, `SELECT id FROM accounts WHERE owner_id = 'owner' AND id = $1 FOR UPDATE`, bank.ID()); err != nil {
		t.Fatal(err)
	}

	type snapshotResult struct {
		snapshot domainledger.BalanceSnapshot
		err      error
	}
	started := make(chan struct{})
	finished := make(chan snapshotResult, 1)
	go func() {
		close(started)
		balance, _ := money.NewBalance(100, money.MXN())
		snapshot, err := service.CreateSnapshot(ctx, "owner", bank.ID(), balance, now, first.LedgerSequence(), testIdempotencyKey(t, "racing-snapshot"))
		finished <- snapshotResult{snapshot: snapshot, err: err}
	}()
	<-started
	select {
	case result := <-finished:
		t.Fatalf("snapshot did not wait for the account lock: %+v", result)
	case <-time.After(100 * time.Millisecond):
	}

	var secondSequence int64
	if err := blockingTx.QueryRow(ctx, `
		INSERT INTO financial_transactions
			(id, owner_id, account_id, transaction_kind, amount_minor, currency, effect, financial_date, memo, created_at)
		VALUES ('racing-transaction', 'owner', $1, 'manual', 25, 'MXN', 'asset_inflow', '2026-08-10', '', $2)
		RETURNING ledger_sequence`, bank.ID(), now).Scan(&secondSequence); err != nil {
		t.Fatal(err)
	}
	if err := blockingTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	result := <-finished
	if result.err != nil {
		t.Fatal(result.err)
	}
	current, err := service.CurrentBalance(ctx, "owner", bank.ID())
	if err != nil || current.Balance.MinorUnits() != 125 || current.AppliedTransactions != 1 || current.LastAppliedSequence != secondSequence {
		t.Fatalf("serialized reconstruction = %+v, error = %v", current, err)
	}
}

func TestReverseConcurrentTransfersUseStableLockOrder(t *testing.T) {
	pool := newIsolatedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	createTestOwner(t, pool, "owner", now)
	service, _ := applicationledger.NewService(NewLedgerRepository(pool), applicationledger.ServiceOptions{Clock: func() time.Time { return now }, IDGenerator: sequentialIDs()})
	a := createTestAccount(t, service, "owner", "A", account.Bank())
	b := createTestAccount(t, service, "owner", "B", account.Bank())
	date, _ := financialdate.Parse("2026-08-10")
	amount, _ := money.New(100, money.MXN())

	errorsFound := make(chan error, 2)
	var wait sync.WaitGroup
	for index, direction := range [][2]string{{a.ID(), b.ID()}, {b.ID(), a.ID()}} {
		wait.Add(1)
		go func(index int, sourceID, destinationID string) {
			defer wait.Done()
			_, err := service.CreateTransfer(ctx, "owner", sourceID, destinationID, amount, date, "", testIdempotencyKey(t, fmt.Sprintf("reverse-%d", index)))
			errorsFound <- err
		}(index, direction[0], direction[1])
	}
	done := make(chan struct{})
	go func() { wait.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("reverse transfers timed out, possible deadlock")
	}
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Fatalf("reverse transfer error = %v", err)
		}
	}
	for _, financialAccount := range []account.Account{a, b} {
		balance, err := service.CurrentBalance(context.Background(), "owner", financialAccount.ID())
		if err != nil || balance.Balance.MinorUnits() != 0 {
			t.Fatalf("account %s balance = %d, error = %v", financialAccount.ID(), balance.Balance.MinorUnits(), err)
		}
	}
}

func createTestOwner(t *testing.T, pool *pgxpool.Pool, id string, now time.Time) {
	t.Helper()
	owner, err := auth.NewOwner(id, id+"@example.com", "encoded-password-hash", now)
	if err != nil {
		t.Fatal(err)
	}
	// The auth adapter is intentionally used so tests follow the production owner boundary.
	if err := NewAuthRepository(pool).CreateOwner(context.Background(), owner); err != nil {
		t.Fatal(err)
	}
}

func createTestAccount(t *testing.T, service *applicationledger.Service, ownerID, name string, accountType account.AccountType) account.Account {
	t.Helper()
	created, err := service.CreateAccount(context.Background(), ownerID, name, accountType, money.MXN())
	if err != nil {
		t.Fatal(err)
	}
	return created
}

func postTestTransaction(t *testing.T, service *applicationledger.Service, accountID string, effect domainledger.Effect, amountMinor int64, date financialdate.Date) domainledger.Transaction {
	t.Helper()
	amount, _ := money.New(amountMinor, money.MXN())
	posted, err := service.PostTransaction(context.Background(), "owner", accountID, effect, amount, date, "", testIdempotencyKey(t, "post-"+accountID+"-"+fmt.Sprint(amountMinor)+"-"+effect.String()))
	if err != nil {
		t.Fatal(err)
	}
	return posted
}

func sequentialIDs() func() (string, error) {
	var mutex sync.Mutex
	next := 0
	return func() (string, error) {
		mutex.Lock()
		defer mutex.Unlock()
		next++
		return fmt.Sprintf("generated-%03d", next), nil
	}
}

func hasPostgresCode(err error, code string) bool {
	var databaseError *pgconn.PgError
	return errors.As(err, &databaseError) && databaseError.Code == code
}

func testIdempotencyKey(t *testing.T, value string) applicationledger.IdempotencyKey {
	t.Helper()
	key, err := applicationledger.ParseIdempotencyKey(value)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func testMutation(t *testing.T, value string) applicationledger.MutationIdentity {
	t.Helper()
	return applicationledger.MutationIdentity{
		Key:         testIdempotencyKey(t, value),
		Fingerprint: sha256.Sum256([]byte(value)),
	}
}
