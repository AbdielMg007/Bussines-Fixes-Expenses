package ledger

import (
	"context"
	"time"

	"runway/backend/internal/domain/account"
	"runway/backend/internal/domain/financialdate"
	domainledger "runway/backend/internal/domain/ledger"
	"runway/backend/internal/domain/money"
)

type MutationIdentity struct {
	Key         IdempotencyKey
	Fingerprint [32]byte
}

type ManualTransactionDraft struct {
	ID            string
	AccountID     string
	Amount        money.Money
	Effect        domainledger.Effect
	FinancialDate financialdate.Date
	Memo          string
	CreatedAt     time.Time
	Mutation      MutationIdentity
}

type TransferDraft struct {
	Transfer                 domainledger.Transfer
	SourceTransactionID      string
	DestinationTransactionID string
	Mutation                 MutationIdentity
}

type TransferResult struct {
	Transfer               domainledger.Transfer
	SourceTransaction      domainledger.Transaction
	DestinationTransaction domainledger.Transaction
}

type BalanceState struct {
	Account      account.Account
	Snapshot     *domainledger.BalanceSnapshot
	Transactions []domainledger.Transaction
}

type Repository interface {
	CreateAccount(context.Context, account.Account) error
	GetAccount(context.Context, string, string) (account.Account, error)
	ListAccounts(context.Context, string) ([]account.Account, error)
	ArchiveAccount(context.Context, string, string, time.Time) (account.Account, error)

	PostManualTransaction(context.Context, string, ManualTransactionDraft) (domainledger.Transaction, error)
	ListTransactions(context.Context, string, string) ([]domainledger.Transaction, error)

	CreateSnapshot(context.Context, domainledger.BalanceSnapshot, MutationIdentity) (domainledger.BalanceSnapshot, error)
	LoadBalanceState(context.Context, string, string) (BalanceState, error)

	CreateTransfer(context.Context, string, TransferDraft) (TransferResult, error)
}
