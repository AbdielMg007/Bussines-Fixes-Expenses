package ledger

import (
	"strings"
	"time"

	"runway/backend/internal/domain/account"
	"runway/backend/internal/domain/financialdate"
	"runway/backend/internal/domain/money"
)

type Transfer struct {
	id                   string
	ownerID              string
	sourceAccountID      string
	destinationAccountID string
	amount               money.Money
	financialDate        financialdate.Date
	memo                 string
	createdAt            time.Time
}

func NewTransfer(
	id, ownerID, sourceAccountID, destinationAccountID string,
	amount money.Money,
	date financialdate.Date,
	memo string,
	createdAt time.Time,
) (Transfer, error) {
	memo = strings.TrimSpace(memo)
	if strings.TrimSpace(id) == "" || strings.TrimSpace(ownerID) == "" || strings.TrimSpace(sourceAccountID) == "" || strings.TrimSpace(destinationAccountID) == "" || createdAt.IsZero() {
		return Transfer{}, ErrInvalidTransfer
	}
	if sourceAccountID == destinationAccountID {
		return Transfer{}, ErrSameTransferAccount
	}
	if len([]rune(memo)) > MaxMemoLength {
		return Transfer{}, ErrInvalidTransfer
	}
	if _, err := money.New(amount.MinorUnits(), amount.Currency()); err != nil {
		return Transfer{}, err
	}
	if amount.MinorUnits() == 0 {
		return Transfer{}, ErrZeroAmount
	}
	if _, err := financialdate.Parse(date.String()); err != nil {
		return Transfer{}, err
	}

	return Transfer{
		id:                   id,
		ownerID:              ownerID,
		sourceAccountID:      sourceAccountID,
		destinationAccountID: destinationAccountID,
		amount:               amount,
		financialDate:        date,
		memo:                 memo,
		createdAt:            createdAt.UTC(),
	}, nil
}

func TransferEffects(source, destination account.Account) (Effect, Effect, error) {
	if source.ID() == destination.ID() {
		return Effect{}, Effect{}, ErrSameTransferAccount
	}
	if source.OwnerID() != destination.OwnerID() || !source.Type().CanRepresentAsset() {
		return Effect{}, Effect{}, ErrInvalidTransfer
	}
	if source.Currency() != destination.Currency() {
		return Effect{}, Effect{}, money.ErrCurrencyMismatch
	}
	if destination.Type().CanRepresentAsset() {
		return AssetOutflow(), AssetInflow(), nil
	}
	if destination.Type().CanRepresentLiability() {
		return AssetOutflow(), LiabilityPayment(), nil
	}
	return Effect{}, Effect{}, ErrInvalidTransfer
}

func (t Transfer) ID() string                        { return t.id }
func (t Transfer) OwnerID() string                   { return t.ownerID }
func (t Transfer) SourceAccountID() string           { return t.sourceAccountID }
func (t Transfer) DestinationAccountID() string      { return t.destinationAccountID }
func (t Transfer) Amount() money.Money               { return t.amount }
func (t Transfer) FinancialDate() financialdate.Date { return t.financialDate }
func (t Transfer) Memo() string                      { return t.memo }
func (t Transfer) CreatedAt() time.Time              { return t.createdAt }
