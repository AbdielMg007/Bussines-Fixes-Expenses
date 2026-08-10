package account

import (
	"errors"
	"strings"
	"time"

	"runway/backend/internal/domain/money"
)

const MaxDisplayNameLength = 120

var (
	ErrInvalidAccount     = errors.New("invalid account")
	ErrInvalidDisplayName = errors.New("invalid account display name")
	ErrAccountArchived    = errors.New("account is archived")
)

type Account struct {
	id        string
	ownerID   string
	name      string
	typeValue AccountType
	currency  money.Currency
	status    Status
	createdAt time.Time
	updatedAt time.Time
}

func New(id, ownerID, displayName string, accountType AccountType, currency money.Currency, now time.Time) (Account, error) {
	return Restore(id, ownerID, displayName, accountType, currency, ActiveStatus(), now, now)
}

func Restore(
	id, ownerID, displayName string,
	accountType AccountType,
	currency money.Currency,
	status Status,
	createdAt, updatedAt time.Time,
) (Account, error) {
	name := strings.TrimSpace(displayName)
	if strings.TrimSpace(id) == "" || strings.TrimSpace(ownerID) == "" || createdAt.IsZero() || updatedAt.IsZero() || updatedAt.Before(createdAt) {
		return Account{}, ErrInvalidAccount
	}
	if name == "" || len([]rune(name)) > MaxDisplayNameLength {
		return Account{}, ErrInvalidDisplayName
	}
	if _, err := ParseType(accountType.String()); err != nil {
		return Account{}, err
	}
	if _, err := money.Zero(currency); err != nil {
		return Account{}, err
	}
	if _, err := ParseStatus(status.String()); err != nil {
		return Account{}, err
	}

	return Account{
		id:        id,
		ownerID:   ownerID,
		name:      name,
		typeValue: accountType,
		currency:  currency,
		status:    status,
		createdAt: createdAt.UTC(),
		updatedAt: updatedAt.UTC(),
	}, nil
}

func (a Account) Archive(now time.Time) (Account, error) {
	if a.status.IsArchived() {
		return Account{}, ErrAccountArchived
	}
	if now.IsZero() || now.Before(a.updatedAt) {
		return Account{}, ErrInvalidAccount
	}
	a.status = ArchivedStatus()
	a.updatedAt = now.UTC()
	return a, nil
}

func (a Account) ID() string               { return a.id }
func (a Account) OwnerID() string          { return a.ownerID }
func (a Account) DisplayName() string      { return a.name }
func (a Account) Type() AccountType        { return a.typeValue }
func (a Account) Currency() money.Currency { return a.currency }
func (a Account) Status() Status           { return a.status }
func (a Account) CreatedAt() time.Time     { return a.createdAt }
func (a Account) UpdatedAt() time.Time     { return a.updatedAt }
