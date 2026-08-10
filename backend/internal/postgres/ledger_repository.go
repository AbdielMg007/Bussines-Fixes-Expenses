package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"runway/backend/internal/domain/account"
	"runway/backend/internal/domain/financialdate"
	domainledger "runway/backend/internal/domain/ledger"
	"runway/backend/internal/domain/money"
	applicationledger "runway/backend/internal/ledger"
)

type LedgerRepository struct {
	pool *pgxpool.Pool
}

func NewLedgerRepository(pool *pgxpool.Pool) *LedgerRepository {
	return &LedgerRepository{pool: pool}
}

func (r *LedgerRepository) CreateAccount(ctx context.Context, financialAccount account.Account) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO accounts (id, owner_id, display_name, account_type, currency, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		financialAccount.ID(), financialAccount.OwnerID(), financialAccount.DisplayName(), financialAccount.Type().String(),
		financialAccount.Currency().Code(), financialAccount.Status().String(), financialAccount.CreatedAt(), financialAccount.UpdatedAt(),
	)
	if err == nil {
		return nil
	}
	if isUniqueViolation(err) {
		return applicationledger.ErrConflict
	}
	return errors.New("create account")
}

func (r *LedgerRepository) GetAccount(ctx context.Context, ownerID, accountID string) (account.Account, error) {
	return scanAccount(r.pool.QueryRow(ctx, accountSelect+` WHERE owner_id = $1 AND id = $2`, ownerID, accountID))
}

func (r *LedgerRepository) ListAccounts(ctx context.Context, ownerID string) ([]account.Account, error) {
	rows, err := r.pool.Query(ctx, accountSelect+` WHERE owner_id = $1 ORDER BY created_at, id`, ownerID)
	if err != nil {
		return nil, errors.New("list accounts")
	}
	defer rows.Close()

	accounts := make([]account.Account, 0)
	for rows.Next() {
		financialAccount, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, financialAccount)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New("list accounts")
	}
	return accounts, nil
}

func (r *LedgerRepository) ArchiveAccount(ctx context.Context, ownerID, accountID string, now time.Time) (account.Account, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return account.Account{}, errors.New("archive account")
	}
	defer func() { _ = tx.Rollback(ctx) }()

	financialAccount, err := scanAccount(tx.QueryRow(ctx, accountSelect+` WHERE owner_id = $1 AND id = $2 FOR UPDATE`, ownerID, accountID))
	if err != nil {
		return account.Account{}, err
	}
	archived, err := financialAccount.Archive(now)
	if err != nil {
		return account.Account{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE accounts SET status = $1, updated_at = $2 WHERE owner_id = $3 AND id = $4`, archived.Status().String(), archived.UpdatedAt(), ownerID, accountID); err != nil {
		return account.Account{}, errors.New("archive account")
	}
	if err := tx.Commit(ctx); err != nil {
		return account.Account{}, errors.New("archive account")
	}
	return archived, nil
}

func (r *LedgerRepository) PostManualTransaction(ctx context.Context, ownerID string, draft applicationledger.ManualTransactionDraft) (domainledger.Transaction, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domainledger.Transaction{}, errors.New("post transaction")
	}
	defer func() { _ = tx.Rollback(ctx) }()

	financialAccount, err := lockedAccount(ctx, tx, ownerID, draft.AccountID)
	if err != nil {
		return domainledger.Transaction{}, err
	}
	if err := validateManualTransactionDraft(financialAccount, ownerID, draft); err != nil {
		return domainledger.Transaction{}, err
	}
	replayResourceID, replay, err := claimMutation(ctx, tx, ownerID, "post_transaction", draft.Mutation, draft.ID, draft.CreatedAt)
	if err != nil {
		return domainledger.Transaction{}, err
	}
	if replay {
		posted, err := scanTransaction(tx.QueryRow(ctx, transactionSelect+` WHERE owner_id = $1 AND id = $2`, ownerID, replayResourceID))
		if err != nil {
			return domainledger.Transaction{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return domainledger.Transaction{}, errors.New("replay transaction")
		}
		return posted, nil
	}
	if financialAccount.Status().IsArchived() {
		return domainledger.Transaction{}, account.ErrAccountArchived
	}
	if err := validateProspectiveEffect(ctx, tx, financialAccount, draft.Effect, draft.Amount); err != nil {
		return domainledger.Transaction{}, err
	}

	var sequence int64
	err = tx.QueryRow(ctx, `
		INSERT INTO financial_transactions
			(id, owner_id, account_id, transaction_kind, amount_minor, currency, effect, financial_date, memo, transfer_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::date, $9, $10, $11)
		RETURNING ledger_sequence`,
		draft.ID, ownerID, draft.AccountID, domainledger.ManualKind().String(), draft.Amount.MinorUnits(), draft.Amount.Currency().Code(),
		draft.Effect.String(), draft.FinancialDate.String(), draft.Memo, nil, draft.CreatedAt,
	).Scan(&sequence)
	if err != nil {
		if isUniqueViolation(err) {
			return domainledger.Transaction{}, applicationledger.ErrConflict
		}
		return domainledger.Transaction{}, errors.New("post transaction")
	}
	posted, err := domainledger.NewTransaction(
		draft.ID, ownerID, draft.AccountID, domainledger.ManualKind(), draft.Amount, draft.Effect, draft.FinancialDate,
		sequence, draft.Memo, "", draft.CreatedAt,
	)
	if err != nil {
		return domainledger.Transaction{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domainledger.Transaction{}, errors.New("post transaction")
	}
	return posted, nil
}

func (r *LedgerRepository) ListTransactions(ctx context.Context, ownerID, accountID string) ([]domainledger.Transaction, error) {
	if _, err := r.GetAccount(ctx, ownerID, accountID); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, transactionSelect+`
		WHERE owner_id = $1 AND account_id = $2 ORDER BY ledger_sequence`, ownerID, accountID)
	if err != nil {
		return nil, errors.New("list transactions")
	}
	defer rows.Close()
	return scanTransactions(rows)
}

func (r *LedgerRepository) CreateSnapshot(ctx context.Context, snapshot domainledger.BalanceSnapshot, mutation applicationledger.MutationIdentity) (domainledger.BalanceSnapshot, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domainledger.BalanceSnapshot{}, errors.New("create balance snapshot")
	}
	defer func() { _ = tx.Rollback(ctx) }()

	financialAccount, err := lockedAccount(ctx, tx, snapshot.OwnerID(), snapshot.AccountID())
	if err != nil {
		return domainledger.BalanceSnapshot{}, err
	}
	replayResourceID, replay, err := claimMutation(ctx, tx, snapshot.OwnerID(), "create_snapshot", mutation, snapshot.ID(), snapshot.CreatedAt())
	if err != nil {
		return domainledger.BalanceSnapshot{}, err
	}
	if replay {
		replayed, err := scanSnapshot(tx.QueryRow(ctx, snapshotSelect+` WHERE owner_id = $1 AND id = $2`, snapshot.OwnerID(), replayResourceID))
		if err != nil {
			return domainledger.BalanceSnapshot{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return domainledger.BalanceSnapshot{}, errors.New("replay balance snapshot")
		}
		return replayed, nil
	}
	if financialAccount.Status().IsArchived() {
		return domainledger.BalanceSnapshot{}, account.ErrAccountArchived
	}
	if snapshot.Balance().Currency() != financialAccount.Currency() {
		return domainledger.BalanceSnapshot{}, money.ErrCurrencyMismatch
	}
	if financialAccount.Type().CanRepresentLiability() && snapshot.Balance().MinorUnits() < 0 {
		return domainledger.BalanceSnapshot{}, applicationledger.ErrNegativeLiabilitySnapshot
	}

	if snapshot.CutoffSequence() > 0 {
		var exists bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM financial_transactions
				WHERE owner_id = $1 AND account_id = $2 AND ledger_sequence = $3
			)`, snapshot.OwnerID(), snapshot.AccountID(), snapshot.CutoffSequence()).Scan(&exists); err != nil {
			return domainledger.BalanceSnapshot{}, errors.New("validate snapshot cutoff")
		}
		if !exists {
			return domainledger.BalanceSnapshot{}, applicationledger.ErrSnapshotCutoffUnknown
		}
	}

	var latestCutoff int64
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(cutoff_sequence), 0)
		FROM balance_snapshots WHERE owner_id = $1 AND account_id = $2`, snapshot.OwnerID(), snapshot.AccountID()).Scan(&latestCutoff); err != nil {
		return domainledger.BalanceSnapshot{}, errors.New("validate snapshot order")
	}
	if snapshot.CutoffSequence() < latestCutoff {
		return domainledger.BalanceSnapshot{}, applicationledger.ErrSnapshotCutoffRegression
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO balance_snapshots
			(id, owner_id, account_id, balance_minor, currency, effective_at, cutoff_sequence, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		snapshot.ID(), snapshot.OwnerID(), snapshot.AccountID(), snapshot.Balance().MinorUnits(),
		snapshot.Balance().Currency().Code(), snapshot.EffectiveAt(), snapshot.CutoffSequence(), snapshot.CreatedAt(),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return domainledger.BalanceSnapshot{}, applicationledger.ErrConflict
		}
		return domainledger.BalanceSnapshot{}, errors.New("create balance snapshot")
	}
	if err := tx.Commit(ctx); err != nil {
		return domainledger.BalanceSnapshot{}, errors.New("create balance snapshot")
	}
	return snapshot, nil
}

func (r *LedgerRepository) LoadBalanceState(ctx context.Context, ownerID, accountID string) (applicationledger.BalanceState, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return applicationledger.BalanceState{}, errors.New("load balance")
	}
	defer func() { _ = tx.Rollback(ctx) }()

	financialAccount, err := scanAccount(tx.QueryRow(ctx, accountSelect+` WHERE owner_id = $1 AND id = $2`, ownerID, accountID))
	if err != nil {
		return applicationledger.BalanceState{}, err
	}
	state, err := loadBalanceStateInTransaction(ctx, tx, financialAccount)
	if err != nil {
		return applicationledger.BalanceState{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return applicationledger.BalanceState{}, errors.New("load balance")
	}
	return state, nil
}

func (r *LedgerRepository) CreateTransfer(ctx context.Context, ownerID string, draft applicationledger.TransferDraft) (applicationledger.TransferResult, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return applicationledger.TransferResult{}, errors.New("create transfer")
	}
	defer func() { _ = tx.Rollback(ctx) }()

	transfer := draft.Transfer
	if transfer.OwnerID() != ownerID {
		return applicationledger.TransferResult{}, domainledger.ErrInvalidTransfer
	}
	accounts, err := lockTransferAccounts(ctx, tx, ownerID, transfer.SourceAccountID(), transfer.DestinationAccountID())
	if err != nil {
		return applicationledger.TransferResult{}, err
	}
	source := accounts[transfer.SourceAccountID()]
	destination := accounts[transfer.DestinationAccountID()]
	replayResourceID, replay, err := claimMutation(ctx, tx, ownerID, "create_transfer", draft.Mutation, transfer.ID(), transfer.CreatedAt())
	if err != nil {
		return applicationledger.TransferResult{}, err
	}
	if replay {
		replayed, err := loadTransferResult(ctx, tx, ownerID, replayResourceID)
		if err != nil {
			return applicationledger.TransferResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return applicationledger.TransferResult{}, errors.New("replay transfer")
		}
		return replayed, nil
	}
	if source.Status().IsArchived() || destination.Status().IsArchived() {
		return applicationledger.TransferResult{}, account.ErrAccountArchived
	}
	sourceEffect, destinationEffect, err := domainledger.TransferEffects(source, destination)
	if err != nil {
		return applicationledger.TransferResult{}, err
	}
	if transfer.Amount().Currency() != source.Currency() {
		return applicationledger.TransferResult{}, money.ErrCurrencyMismatch
	}
	if err := validateProspectiveEffect(ctx, tx, source, sourceEffect, transfer.Amount()); err != nil {
		return applicationledger.TransferResult{}, err
	}
	if err := validateProspectiveEffect(ctx, tx, destination, destinationEffect, transfer.Amount()); err != nil {
		return applicationledger.TransferResult{}, err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO linked_transfers
			(id, owner_id, source_account_id, destination_account_id, amount_minor, currency, financial_date, memo, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7::date, $8, $9)`,
		transfer.ID(), ownerID, transfer.SourceAccountID(), transfer.DestinationAccountID(), transfer.Amount().MinorUnits(),
		transfer.Amount().Currency().Code(), transfer.FinancialDate().String(), transfer.Memo(), transfer.CreatedAt(),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return applicationledger.TransferResult{}, applicationledger.ErrConflict
		}
		return applicationledger.TransferResult{}, errors.New("create transfer")
	}

	sourceTransaction, err := insertTransferTransaction(ctx, tx, ownerID, draft.SourceTransactionID, source.ID(), sourceEffect, transfer)
	if err != nil {
		return applicationledger.TransferResult{}, err
	}
	destinationTransaction, err := insertTransferTransaction(ctx, tx, ownerID, draft.DestinationTransactionID, destination.ID(), destinationEffect, transfer)
	if err != nil {
		return applicationledger.TransferResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return applicationledger.TransferResult{}, errors.New("create transfer")
	}
	return applicationledger.TransferResult{Transfer: transfer, SourceTransaction: sourceTransaction, DestinationTransaction: destinationTransaction}, nil
}

const accountSelect = `
	SELECT id, owner_id, display_name, account_type, currency, status, created_at, updated_at
	FROM accounts`

const transactionSelect = `
	SELECT id, owner_id, account_id, transaction_kind, amount_minor, currency, effect,
	       to_char(financial_date, 'YYYY-MM-DD'), ledger_sequence, memo, COALESCE(transfer_id, ''), created_at
	FROM financial_transactions`

const snapshotSelect = `
	SELECT id, owner_id, account_id, balance_minor, currency, effective_at, cutoff_sequence, created_at
	FROM balance_snapshots`

const transferSelect = `
	SELECT id, owner_id, source_account_id, destination_account_id, amount_minor, currency,
	       to_char(financial_date, 'YYYY-MM-DD'), memo, created_at
	FROM linked_transfers`

func lockedAccount(ctx context.Context, tx pgx.Tx, ownerID, accountID string) (account.Account, error) {
	return scanAccount(tx.QueryRow(ctx, accountSelect+` WHERE owner_id = $1 AND id = $2 FOR UPDATE`, ownerID, accountID))
}

func scanAccount(row rowScanner) (account.Account, error) {
	var id, ownerID, name, typeValue, currencyCode, statusValue string
	var createdAt, updatedAt time.Time
	if err := row.Scan(&id, &ownerID, &name, &typeValue, &currencyCode, &statusValue, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return account.Account{}, applicationledger.ErrNotFound
		}
		return account.Account{}, errors.New("read account")
	}
	accountType, err := account.ParseType(typeValue)
	if err != nil {
		return account.Account{}, fmt.Errorf("restore account type: %w", err)
	}
	currency, err := money.ParseCurrency(currencyCode)
	if err != nil {
		return account.Account{}, fmt.Errorf("restore account currency: %w", err)
	}
	status, err := account.ParseStatus(statusValue)
	if err != nil {
		return account.Account{}, fmt.Errorf("restore account status: %w", err)
	}
	financialAccount, err := account.Restore(id, ownerID, name, accountType, currency, status, createdAt, updatedAt)
	if err != nil {
		return account.Account{}, fmt.Errorf("restore account: %w", err)
	}
	return financialAccount, nil
}

func scanTransaction(row rowScanner) (domainledger.Transaction, error) {
	var id, ownerID, accountID, kindValue, currencyCode, effectValue, dateValue, memo, transferID string
	var amountMinor, sequence int64
	var createdAt time.Time
	if err := row.Scan(&id, &ownerID, &accountID, &kindValue, &amountMinor, &currencyCode, &effectValue, &dateValue, &sequence, &memo, &transferID, &createdAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domainledger.Transaction{}, applicationledger.ErrNotFound
		}
		return domainledger.Transaction{}, errors.New("read transaction")
	}
	kind, err := domainledger.ParseTransactionKind(kindValue)
	if err != nil {
		return domainledger.Transaction{}, err
	}
	currency, err := money.ParseCurrency(currencyCode)
	if err != nil {
		return domainledger.Transaction{}, err
	}
	amount, err := money.New(amountMinor, currency)
	if err != nil {
		return domainledger.Transaction{}, err
	}
	effect, err := domainledger.ParseEffect(effectValue)
	if err != nil {
		return domainledger.Transaction{}, err
	}
	date, err := financialdate.Parse(dateValue)
	if err != nil {
		return domainledger.Transaction{}, err
	}
	return domainledger.NewTransaction(id, ownerID, accountID, kind, amount, effect, date, sequence, memo, transferID, createdAt)
}

func scanTransactions(rows pgx.Rows) ([]domainledger.Transaction, error) {
	transactions := make([]domainledger.Transaction, 0)
	for rows.Next() {
		transaction, err := scanTransaction(rows)
		if err != nil {
			return nil, err
		}
		transactions = append(transactions, transaction)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New("read transactions")
	}
	return transactions, nil
}

func scanSnapshot(row rowScanner) (domainledger.BalanceSnapshot, error) {
	var id, ownerID, accountID, currencyCode string
	var balanceMinor, cutoff int64
	var effectiveAt, createdAt time.Time
	if err := row.Scan(&id, &ownerID, &accountID, &balanceMinor, &currencyCode, &effectiveAt, &cutoff, &createdAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domainledger.BalanceSnapshot{}, applicationledger.ErrNotFound
		}
		return domainledger.BalanceSnapshot{}, errors.New("read balance snapshot")
	}
	currency, err := money.ParseCurrency(currencyCode)
	if err != nil {
		return domainledger.BalanceSnapshot{}, err
	}
	balance, err := money.NewBalance(balanceMinor, currency)
	if err != nil {
		return domainledger.BalanceSnapshot{}, err
	}
	return domainledger.NewBalanceSnapshot(id, ownerID, accountID, balance, effectiveAt, cutoff, createdAt)
}

func validateManualTransactionDraft(financialAccount account.Account, ownerID string, draft applicationledger.ManualTransactionDraft) error {
	if financialAccount.OwnerID() != ownerID || financialAccount.ID() != draft.AccountID {
		return applicationledger.ErrNotFound
	}
	if err := draft.Effect.ValidateFor(financialAccount.Type()); err != nil {
		return err
	}
	if draft.Amount.Currency() != financialAccount.Currency() {
		return money.ErrCurrencyMismatch
	}
	_, err := domainledger.NewTransaction(
		draft.ID, ownerID, draft.AccountID, domainledger.ManualKind(), draft.Amount, draft.Effect, draft.FinancialDate,
		1, draft.Memo, "", draft.CreatedAt,
	)
	return err
}

func claimMutation(
	ctx context.Context,
	tx pgx.Tx,
	ownerID, operation string,
	mutation applicationledger.MutationIdentity,
	resourceID string,
	createdAt time.Time,
) (string, bool, error) {
	if err := mutation.Validate(); err != nil {
		return "", false, err
	}
	var insertedResourceID string
	err := tx.QueryRow(ctx, `
		INSERT INTO financial_mutations
			(owner_id, operation, idempotency_key, request_fingerprint, resource_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (owner_id, operation, idempotency_key) DO NOTHING
		RETURNING resource_id`,
		ownerID, operation, mutation.Key.String(), mutation.Fingerprint[:], resourceID, createdAt,
	).Scan(&insertedResourceID)
	if err == nil {
		return insertedResourceID, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", false, errors.New("claim financial mutation")
	}

	var existingFingerprint []byte
	var existingResourceID string
	if err := tx.QueryRow(ctx, `
		SELECT request_fingerprint, resource_id
		FROM financial_mutations
		WHERE owner_id = $1 AND operation = $2 AND idempotency_key = $3`,
		ownerID, operation, mutation.Key.String(),
	).Scan(&existingFingerprint, &existingResourceID); err != nil {
		return "", false, errors.New("read financial mutation")
	}
	if !bytes.Equal(existingFingerprint, mutation.Fingerprint[:]) {
		return "", false, applicationledger.ErrIdempotencyConflict
	}
	return existingResourceID, true, nil
}

func loadBalanceStateInTransaction(ctx context.Context, tx pgx.Tx, financialAccount account.Account) (applicationledger.BalanceState, error) {
	state := applicationledger.BalanceState{Account: financialAccount}
	snapshot, err := scanSnapshot(tx.QueryRow(ctx, snapshotSelect+`
		WHERE owner_id = $1 AND account_id = $2
		ORDER BY cutoff_sequence DESC, snapshot_sequence DESC LIMIT 1`, financialAccount.OwnerID(), financialAccount.ID()))
	if err != nil && !errors.Is(err, applicationledger.ErrNotFound) {
		return applicationledger.BalanceState{}, err
	}
	if err == nil {
		state.Snapshot = &snapshot
	}
	cutoff := int64(0)
	if state.Snapshot != nil {
		cutoff = state.Snapshot.CutoffSequence()
	}
	rows, err := tx.Query(ctx, transactionSelect+`
		WHERE owner_id = $1 AND account_id = $2 AND ledger_sequence > $3
		ORDER BY ledger_sequence`, financialAccount.OwnerID(), financialAccount.ID(), cutoff)
	if err != nil {
		return applicationledger.BalanceState{}, errors.New("load balance transactions")
	}
	state.Transactions, err = scanTransactions(rows)
	rows.Close()
	if err != nil {
		return applicationledger.BalanceState{}, err
	}
	return state, nil
}

func validateProspectiveEffect(ctx context.Context, tx pgx.Tx, financialAccount account.Account, effect domainledger.Effect, amount money.Money) error {
	state, err := loadBalanceStateInTransaction(ctx, tx, financialAccount)
	if err != nil {
		return err
	}
	current, err := applicationledger.ReconstructBalance(state)
	if err != nil {
		return err
	}
	next, err := effect.Apply(current.Balance, amount)
	if err != nil {
		return err
	}
	if financialAccount.Type().CanRepresentLiability() && next.MinorUnits() < 0 {
		return applicationledger.ErrLiabilityOverpayment
	}
	return nil
}

func scanTransfer(row rowScanner) (domainledger.Transfer, error) {
	var id, ownerID, sourceID, destinationID, currencyCode, dateValue, memo string
	var amountMinor int64
	var createdAt time.Time
	if err := row.Scan(&id, &ownerID, &sourceID, &destinationID, &amountMinor, &currencyCode, &dateValue, &memo, &createdAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domainledger.Transfer{}, applicationledger.ErrNotFound
		}
		return domainledger.Transfer{}, errors.New("read transfer")
	}
	currency, err := money.ParseCurrency(currencyCode)
	if err != nil {
		return domainledger.Transfer{}, err
	}
	amount, err := money.New(amountMinor, currency)
	if err != nil {
		return domainledger.Transfer{}, err
	}
	date, err := financialdate.Parse(dateValue)
	if err != nil {
		return domainledger.Transfer{}, err
	}
	return domainledger.NewTransfer(id, ownerID, sourceID, destinationID, amount, date, memo, createdAt)
}

func loadTransferResult(ctx context.Context, tx pgx.Tx, ownerID, transferID string) (applicationledger.TransferResult, error) {
	transfer, err := scanTransfer(tx.QueryRow(ctx, transferSelect+` WHERE owner_id = $1 AND id = $2`, ownerID, transferID))
	if err != nil {
		return applicationledger.TransferResult{}, err
	}
	rows, err := tx.Query(ctx, transactionSelect+`
		WHERE owner_id = $1 AND transfer_id = $2 ORDER BY ledger_sequence`, ownerID, transferID)
	if err != nil {
		return applicationledger.TransferResult{}, errors.New("read transfer transactions")
	}
	transactions, err := scanTransactions(rows)
	rows.Close()
	if err != nil {
		return applicationledger.TransferResult{}, err
	}
	if len(transactions) != 2 {
		return applicationledger.TransferResult{}, errors.New("invalid persisted transfer")
	}
	result := applicationledger.TransferResult{Transfer: transfer}
	for _, transaction := range transactions {
		switch transaction.AccountID() {
		case transfer.SourceAccountID():
			result.SourceTransaction = transaction
		case transfer.DestinationAccountID():
			result.DestinationTransaction = transaction
		default:
			return applicationledger.TransferResult{}, errors.New("invalid persisted transfer")
		}
	}
	if result.SourceTransaction.ID() == "" || result.DestinationTransaction.ID() == "" {
		return applicationledger.TransferResult{}, errors.New("invalid persisted transfer")
	}
	return result, nil
}

func lockTransferAccounts(ctx context.Context, tx pgx.Tx, ownerID, sourceID, destinationID string) (map[string]account.Account, error) {
	firstID, secondID := sourceID, destinationID
	if secondID < firstID {
		firstID, secondID = secondID, firstID
	}
	first, err := lockedAccount(ctx, tx, ownerID, firstID)
	if err != nil {
		return nil, err
	}
	second, err := lockedAccount(ctx, tx, ownerID, secondID)
	if err != nil {
		return nil, err
	}
	return map[string]account.Account{first.ID(): first, second.ID(): second}, nil
}

func insertTransferTransaction(
	ctx context.Context,
	tx pgx.Tx,
	ownerID, transactionID, accountID string,
	effect domainledger.Effect,
	transfer domainledger.Transfer,
) (domainledger.Transaction, error) {
	var sequence int64
	err := tx.QueryRow(ctx, `
		INSERT INTO financial_transactions
			(id, owner_id, account_id, transaction_kind, amount_minor, currency, effect, financial_date, memo, transfer_id, created_at)
		VALUES ($1, $2, $3, 'transfer', $4, $5, $6, $7::date, $8, $9, $10)
		RETURNING ledger_sequence`,
		transactionID, ownerID, accountID, transfer.Amount().MinorUnits(), transfer.Amount().Currency().Code(), effect.String(),
		transfer.FinancialDate().String(), transfer.Memo(), transfer.ID(), transfer.CreatedAt(),
	).Scan(&sequence)
	if err != nil {
		if isUniqueViolation(err) {
			return domainledger.Transaction{}, applicationledger.ErrConflict
		}
		return domainledger.Transaction{}, errors.New("post transfer transaction")
	}
	return domainledger.NewTransaction(
		transactionID, ownerID, accountID, domainledger.TransferKind(), transfer.Amount(), effect,
		transfer.FinancialDate(), sequence, transfer.Memo(), transfer.ID(), transfer.CreatedAt(),
	)
}

func isUniqueViolation(err error) bool {
	var databaseError *pgconn.PgError
	return errors.As(err, &databaseError) && databaseError.Code == "23505"
}
