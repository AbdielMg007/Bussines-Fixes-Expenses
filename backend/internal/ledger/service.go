package ledger

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"strconv"
	"strings"
	"time"

	"runway/backend/internal/domain/account"
	"runway/backend/internal/domain/financialdate"
	domainledger "runway/backend/internal/domain/ledger"
	"runway/backend/internal/domain/money"
)

type ServiceOptions struct {
	Clock       func() time.Time
	IDGenerator func() (string, error)
}

type Service struct {
	repository  Repository
	clock       func() time.Time
	idGenerator func() (string, error)
}

type BalanceResult struct {
	AccountID              string
	Balance                money.Balance
	SnapshotID             string
	SnapshotCutoffSequence int64
	LastAppliedSequence    int64
	AppliedTransactions    int
}

func NewService(repository Repository, options ServiceOptions) (*Service, error) {
	if repository == nil {
		return nil, errors.New("invalid ledger service configuration")
	}
	if options.Clock == nil {
		options.Clock = time.Now
	}
	if options.IDGenerator == nil {
		options.IDGenerator = generateID
	}
	return &Service{repository: repository, clock: options.Clock, idGenerator: options.IDGenerator}, nil
}

func (s *Service) CreateAccount(ctx context.Context, ownerID, name string, accountType account.AccountType, currency money.Currency) (account.Account, error) {
	if err := validateOwnerID(ownerID); err != nil {
		return account.Account{}, err
	}
	id, err := s.idGenerator()
	if err != nil {
		return account.Account{}, err
	}
	created, err := account.New(id, ownerID, name, accountType, currency, s.clock().UTC())
	if err != nil {
		return account.Account{}, err
	}
	if err := s.repository.CreateAccount(ctx, created); err != nil {
		return account.Account{}, err
	}
	return created, nil
}

func (s *Service) GetAccount(ctx context.Context, ownerID, accountID string) (account.Account, error) {
	if err := validateOwnerID(ownerID); err != nil {
		return account.Account{}, err
	}
	return s.repository.GetAccount(ctx, ownerID, accountID)
}

func (s *Service) ListAccounts(ctx context.Context, ownerID string) ([]account.Account, error) {
	if err := validateOwnerID(ownerID); err != nil {
		return nil, err
	}
	return s.repository.ListAccounts(ctx, ownerID)
}

func (s *Service) ArchiveAccount(ctx context.Context, ownerID, accountID string) (account.Account, error) {
	if err := validateOwnerID(ownerID); err != nil {
		return account.Account{}, err
	}
	return s.repository.ArchiveAccount(ctx, ownerID, accountID, s.clock().UTC())
}

func (s *Service) PostTransaction(
	ctx context.Context,
	ownerID, accountID string,
	effect domainledger.Effect,
	amount money.Money,
	date financialdate.Date,
	memo string,
	idempotencyKey IdempotencyKey,
) (domainledger.Transaction, error) {
	financialAccount, err := s.GetAccount(ctx, ownerID, accountID)
	if err != nil {
		return domainledger.Transaction{}, err
	}
	if err := effect.ValidateFor(financialAccount.Type()); err != nil {
		return domainledger.Transaction{}, err
	}
	if amount.Currency() != financialAccount.Currency() {
		return domainledger.Transaction{}, money.ErrCurrencyMismatch
	}
	if amount.MinorUnits() == 0 {
		return domainledger.Transaction{}, domainledger.ErrZeroAmount
	}
	if _, err := financialdate.Parse(date.String()); err != nil {
		return domainledger.Transaction{}, err
	}
	memo = strings.TrimSpace(memo)
	if len([]rune(memo)) > domainledger.MaxMemoLength {
		return domainledger.Transaction{}, domainledger.ErrInvalidTransaction
	}
	id, err := s.idGenerator()
	if err != nil {
		return domainledger.Transaction{}, err
	}
	return s.repository.PostManualTransaction(ctx, ownerID, ManualTransactionDraft{
		ID: id, AccountID: accountID, Amount: amount, Effect: effect, FinancialDate: date, Memo: memo, CreatedAt: s.clock().UTC(),
		Mutation: MutationIdentity{Key: idempotencyKey, Fingerprint: canonicalFingerprint(
			"v1", "post_transaction", ownerID, accountID, effect.String(), strconv.FormatInt(amount.MinorUnits(), 10),
			amount.Currency().Code(), date.String(), memo,
		)},
	})
}

func (s *Service) ListTransactions(ctx context.Context, ownerID, accountID string) ([]domainledger.Transaction, error) {
	if _, err := s.GetAccount(ctx, ownerID, accountID); err != nil {
		return nil, err
	}
	return s.repository.ListTransactions(ctx, ownerID, accountID)
}

func (s *Service) CreateSnapshot(
	ctx context.Context,
	ownerID, accountID string,
	balance money.Balance,
	effectiveAt time.Time,
	cutoffSequence int64,
	idempotencyKey IdempotencyKey,
) (domainledger.BalanceSnapshot, error) {
	financialAccount, err := s.GetAccount(ctx, ownerID, accountID)
	if err != nil {
		return domainledger.BalanceSnapshot{}, err
	}
	if balance.Currency() != financialAccount.Currency() {
		return domainledger.BalanceSnapshot{}, money.ErrCurrencyMismatch
	}
	if financialAccount.Type().CanRepresentLiability() && balance.MinorUnits() < 0 {
		return domainledger.BalanceSnapshot{}, ErrNegativeLiabilitySnapshot
	}
	id, err := s.idGenerator()
	if err != nil {
		return domainledger.BalanceSnapshot{}, err
	}
	snapshot, err := domainledger.NewBalanceSnapshot(id, ownerID, accountID, balance, effectiveAt, cutoffSequence, s.clock().UTC())
	if err != nil {
		return domainledger.BalanceSnapshot{}, err
	}
	return s.repository.CreateSnapshot(ctx, snapshot, MutationIdentity{
		Key: idempotencyKey,
		Fingerprint: canonicalFingerprint(
			"v1", "create_snapshot", ownerID, accountID, strconv.FormatInt(balance.MinorUnits(), 10), balance.Currency().Code(),
			effectiveAt.UTC().Format(time.RFC3339Nano), strconv.FormatInt(cutoffSequence, 10),
		),
	})
}

func (s *Service) CurrentBalance(ctx context.Context, ownerID, accountID string) (BalanceResult, error) {
	if err := validateOwnerID(ownerID); err != nil {
		return BalanceResult{}, err
	}
	state, err := s.repository.LoadBalanceState(ctx, ownerID, accountID)
	if err != nil {
		return BalanceResult{}, err
	}
	return ReconstructBalance(state)
}

func (s *Service) CreateTransfer(
	ctx context.Context,
	ownerID, sourceAccountID, destinationAccountID string,
	amount money.Money,
	date financialdate.Date,
	memo string,
	idempotencyKey IdempotencyKey,
) (TransferResult, error) {
	if err := validateOwnerID(ownerID); err != nil {
		return TransferResult{}, err
	}
	source, err := s.repository.GetAccount(ctx, ownerID, sourceAccountID)
	if err != nil {
		return TransferResult{}, err
	}
	destination, err := s.repository.GetAccount(ctx, ownerID, destinationAccountID)
	if err != nil {
		return TransferResult{}, err
	}
	if _, _, err := domainledger.TransferEffects(source, destination); err != nil {
		return TransferResult{}, err
	}
	if amount.Currency() != source.Currency() {
		return TransferResult{}, money.ErrCurrencyMismatch
	}

	transferID, err := s.idGenerator()
	if err != nil {
		return TransferResult{}, err
	}
	sourceTransactionID, err := s.idGenerator()
	if err != nil {
		return TransferResult{}, err
	}
	destinationTransactionID, err := s.idGenerator()
	if err != nil {
		return TransferResult{}, err
	}
	createdAt := s.clock().UTC()
	transfer, err := domainledger.NewTransfer(transferID, ownerID, sourceAccountID, destinationAccountID, amount, date, memo, createdAt)
	if err != nil {
		return TransferResult{}, err
	}
	return s.repository.CreateTransfer(ctx, ownerID, TransferDraft{
		Transfer: transfer, SourceTransactionID: sourceTransactionID, DestinationTransactionID: destinationTransactionID,
		Mutation: MutationIdentity{Key: idempotencyKey, Fingerprint: canonicalFingerprint(
			"v1", "create_transfer", ownerID, sourceAccountID, destinationAccountID, strconv.FormatInt(amount.MinorUnits(), 10),
			amount.Currency().Code(), date.String(), transfer.Memo(),
		)},
	})
}

func ReconstructBalance(state BalanceState) (BalanceResult, error) {
	balance, err := money.ZeroBalance(state.Account.Currency())
	if err != nil {
		return BalanceResult{}, err
	}
	result := BalanceResult{AccountID: state.Account.ID(), Balance: balance}
	if state.Snapshot != nil {
		if state.Snapshot.OwnerID() != state.Account.OwnerID() || state.Snapshot.AccountID() != state.Account.ID() || state.Snapshot.Balance().Currency() != state.Account.Currency() {
			return BalanceResult{}, domainledger.ErrInvalidSnapshot
		}
		result.Balance = state.Snapshot.Balance()
		if state.Account.Type().CanRepresentLiability() && result.Balance.MinorUnits() < 0 {
			return BalanceResult{}, domainledger.ErrNegativeLiabilityBalance
		}
		result.SnapshotID = state.Snapshot.ID()
		result.SnapshotCutoffSequence = state.Snapshot.CutoffSequence()
		result.LastAppliedSequence = state.Snapshot.CutoffSequence()
	}

	previousSequence := result.SnapshotCutoffSequence
	for _, transaction := range state.Transactions {
		if transaction.OwnerID() != state.Account.OwnerID() || transaction.AccountID() != state.Account.ID() || transaction.LedgerSequence() <= previousSequence {
			return BalanceResult{}, domainledger.ErrInvalidTransaction
		}
		if err := transaction.Effect().ValidateFor(state.Account.Type()); err != nil {
			return BalanceResult{}, err
		}
		result.Balance, err = transaction.Effect().Apply(result.Balance, transaction.Amount())
		if err != nil {
			return BalanceResult{}, err
		}
		if state.Account.Type().CanRepresentLiability() && result.Balance.MinorUnits() < 0 {
			return BalanceResult{}, domainledger.ErrNegativeLiabilityBalance
		}
		previousSequence = transaction.LedgerSequence()
		result.LastAppliedSequence = previousSequence
		result.AppliedTransactions++
	}
	return result, nil
}

func canonicalFingerprint(parts ...string) [32]byte {
	hash := sha256.New()
	var length [8]byte
	for _, part := range parts {
		binary.BigEndian.PutUint64(length[:], uint64(len(part)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write([]byte(part))
	}
	var result [32]byte
	copy(result[:], hash.Sum(nil))
	return result
}

func validateOwnerID(ownerID string) error {
	if strings.TrimSpace(ownerID) == "" {
		return ErrInvalidOwner
	}
	return nil
}

func generateID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}
