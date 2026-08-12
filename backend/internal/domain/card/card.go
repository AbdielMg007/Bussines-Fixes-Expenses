package card

import (
	"errors"
	"strings"
	"time"

	"runway/backend/internal/domain/financialdate"
	"runway/backend/internal/domain/money"
)

var (
	ErrInvalidCycle                     = errors.New("invalid credit card cycle")
	ErrInvalidStatement                 = errors.New("invalid credit card statement")
	ErrIssuedCannotBeReplacedByEstimate = errors.New("issued statement cannot be replaced by estimate")
	ErrInvalidPaymentIntent             = errors.New("invalid payment intent")
	ErrPaymentIntentCancelled           = errors.New("cancelled payment intent cannot be replaced")
	ErrPaymentIntentSettled             = errors.New("settled payment intent cannot be replaced")
	ErrInvalidPaymentIntentSettlement   = errors.New("invalid payment intent settlement")
	ErrNoAuthoritativeStatement         = errors.New("no authoritative statement")
	ErrInvalidInstallmentPlan           = errors.New("invalid installment plan")
	ErrInvalidInstallmentAllocation     = errors.New("invalid installment allocation")
	ErrInstallmentOverpayment           = errors.New("installment principal payment exceeds unpaid allocation")
	ErrInstallmentPlanCompleted         = errors.New("installment plan is completed")
)

type Authority string

const (
	Estimated Authority = "estimated"
	Issued    Authority = "issued"
)

func (a Authority) Valid() bool { return a == Estimated || a == Issued }

type IntentStatus string

const (
	IntentActive      IntentStatus = "active"
	IntentNeedsReview IntentStatus = "needs_review"
	IntentCancelled   IntentStatus = "cancelled"
	IntentSettled     IntentStatus = "settled"
)

type Cycle struct {
	ID, OwnerID, AccountID string
	Start, End             financialdate.Date
	CreatedAt              time.Time
}

func NewCycle(id, owner, account string, start, end financialdate.Date, now time.Time) (Cycle, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(owner) == "" || strings.TrimSpace(account) == "" || now.IsZero() {
		return Cycle{}, ErrInvalidCycle
	}
	c, err := start.Compare(end)
	if err != nil || c > 0 {
		return Cycle{}, ErrInvalidCycle
	}
	return Cycle{id, owner, account, start, end, now.UTC()}, nil
}

type Statement struct {
	ID, OwnerID, AccountID, CycleID string
	Revision                        int64
	Authority                       Authority
	Balance                         money.Money
	Minimum, AvoidInterest          *money.Money
	Due                             financialdate.Date
	CreatedAt                       time.Time
	SupersededAt                    *time.Time
	SupersededBy                    string
}

func NewStatement(id, owner, account, cycle string, rev int64, authority Authority, balance money.Money, min, avoid *money.Money, due financialdate.Date, now time.Time) (Statement, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(owner) == "" || strings.TrimSpace(account) == "" || strings.TrimSpace(cycle) == "" || rev < 1 || !authority.Valid() || now.IsZero() || due.String() == "" {
		return Statement{}, ErrInvalidStatement
	}
	if _, err := money.Zero(balance.Currency()); err != nil {
		return Statement{}, ErrInvalidStatement
	}
	for _, v := range []*money.Money{min, avoid} {
		if v != nil {
			if v.Currency() != balance.Currency() || v.MinorUnits() > balance.MinorUnits() {
				return Statement{}, ErrInvalidStatement
			}
		}
	}
	return Statement{id, owner, account, cycle, rev, authority, balance, min, avoid, due, now.UTC(), nil, ""}, nil
}

type PaymentIntent struct {
	ID, OwnerID, AccountID, CycleID string
	Amount                          money.Money
	Planned                         financialdate.Date
	Status                          IntentStatus
	Version                         int64
	CreatedAt, UpdatedAt            time.Time
}

func NewPaymentIntent(id, owner, account, cycle string, amount money.Money, planned financialdate.Date, status IntentStatus, version int64, created, updated time.Time) (PaymentIntent, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(owner) == "" || strings.TrimSpace(account) == "" || strings.TrimSpace(cycle) == "" || amount.MinorUnits() <= 0 || planned.String() == "" || version < 1 || created.IsZero() || updated.Before(created) || (status != IntentActive && status != IntentNeedsReview && status != IntentCancelled && status != IntentSettled) {
		return PaymentIntent{}, ErrInvalidPaymentIntent
	}
	return PaymentIntent{id, owner, account, cycle, amount, planned, status, version, created.UTC(), updated.UTC()}, nil
}
