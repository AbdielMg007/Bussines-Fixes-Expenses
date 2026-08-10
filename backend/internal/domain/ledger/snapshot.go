package ledger

import (
	"strings"
	"time"

	"runway/backend/internal/domain/money"
)

type BalanceSnapshot struct {
	id             string
	ownerID        string
	accountID      string
	balance        money.Balance
	effectiveAt    time.Time
	cutoffSequence int64
	createdAt      time.Time
}

func NewBalanceSnapshot(
	id, ownerID, accountID string,
	balance money.Balance,
	effectiveAt time.Time,
	cutoffSequence int64,
	createdAt time.Time,
) (BalanceSnapshot, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(ownerID) == "" || strings.TrimSpace(accountID) == "" || effectiveAt.IsZero() || createdAt.IsZero() || cutoffSequence < 0 {
		return BalanceSnapshot{}, ErrInvalidSnapshot
	}
	if _, err := money.NewBalance(balance.MinorUnits(), balance.Currency()); err != nil {
		return BalanceSnapshot{}, err
	}

	return BalanceSnapshot{
		id:             id,
		ownerID:        ownerID,
		accountID:      accountID,
		balance:        balance,
		effectiveAt:    effectiveAt.UTC(),
		cutoffSequence: cutoffSequence,
		createdAt:      createdAt.UTC(),
	}, nil
}

func (s BalanceSnapshot) ID() string             { return s.id }
func (s BalanceSnapshot) OwnerID() string        { return s.ownerID }
func (s BalanceSnapshot) AccountID() string      { return s.accountID }
func (s BalanceSnapshot) Balance() money.Balance { return s.balance }
func (s BalanceSnapshot) EffectiveAt() time.Time { return s.effectiveAt }
func (s BalanceSnapshot) CutoffSequence() int64  { return s.cutoffSequence }
func (s BalanceSnapshot) CreatedAt() time.Time   { return s.createdAt }
