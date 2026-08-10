package ledger

import (
	"strings"
	"time"

	"runway/backend/internal/domain/financialdate"
	"runway/backend/internal/domain/money"
)

const MaxMemoLength = 500

type Transaction struct {
	id             string
	ownerID        string
	accountID      string
	kind           TransactionKind
	amount         money.Money
	effect         Effect
	financialDate  financialdate.Date
	ledgerSequence int64
	memo           string
	transferID     string
	createdAt      time.Time
}

func NewTransaction(
	id, ownerID, accountID string,
	kind TransactionKind,
	amount money.Money,
	effect Effect,
	date financialdate.Date,
	ledgerSequence int64,
	memo, transferID string,
	createdAt time.Time,
) (Transaction, error) {
	memo = strings.TrimSpace(memo)
	if strings.TrimSpace(id) == "" || strings.TrimSpace(ownerID) == "" || strings.TrimSpace(accountID) == "" || ledgerSequence <= 0 || createdAt.IsZero() {
		return Transaction{}, ErrInvalidTransaction
	}
	if len([]rune(memo)) > MaxMemoLength {
		return Transaction{}, ErrInvalidTransaction
	}
	if _, err := ParseTransactionKind(kind.String()); err != nil {
		return Transaction{}, err
	}
	if _, err := ParseEffect(effect.String()); err != nil {
		return Transaction{}, err
	}
	if _, err := money.New(amount.MinorUnits(), amount.Currency()); err != nil {
		return Transaction{}, err
	}
	if amount.MinorUnits() == 0 {
		return Transaction{}, ErrZeroAmount
	}
	if _, err := financialdate.Parse(date.String()); err != nil {
		return Transaction{}, err
	}
	if (kind == manualKind && transferID != "") || (kind == transferKind && strings.TrimSpace(transferID) == "") {
		return Transaction{}, ErrInvalidTransaction
	}

	return Transaction{
		id:             id,
		ownerID:        ownerID,
		accountID:      accountID,
		kind:           kind,
		amount:         amount,
		effect:         effect,
		financialDate:  date,
		ledgerSequence: ledgerSequence,
		memo:           memo,
		transferID:     transferID,
		createdAt:      createdAt.UTC(),
	}, nil
}

func (t Transaction) ID() string                        { return t.id }
func (t Transaction) OwnerID() string                   { return t.ownerID }
func (t Transaction) AccountID() string                 { return t.accountID }
func (t Transaction) Kind() TransactionKind             { return t.kind }
func (t Transaction) Amount() money.Money               { return t.amount }
func (t Transaction) Effect() Effect                    { return t.effect }
func (t Transaction) FinancialDate() financialdate.Date { return t.financialDate }
func (t Transaction) LedgerSequence() int64             { return t.ledgerSequence }
func (t Transaction) Memo() string                      { return t.memo }
func (t Transaction) TransferID() string                { return t.transferID }
func (t Transaction) CreatedAt() time.Time              { return t.createdAt }
