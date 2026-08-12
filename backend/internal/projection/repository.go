package projection

import (
	"context"
	"errors"
	"time"

	"runway/backend/internal/domain/account"
	"runway/backend/internal/domain/financialdate"
	domainprojection "runway/backend/internal/domain/projection"
	"runway/backend/internal/domain/schedule"
	applicationledger "runway/backend/internal/ledger"
)

var (
	ErrPolicyNotFound            = errors.New("projection policy not found")
	ErrConfigurationRequired     = errors.New("projection policy is required")
	ErrConfigurationInvalid      = errors.New("projection configuration is invalid")
	ErrFundingAccountInvalid     = errors.New("funding account is invalid")
	ErrFundingAccountNotSelected = errors.New("funding account is not selected for projection liquidity")
	ErrInvalidOwner              = errors.New("invalid projection owner")
)

type AccountBalance struct {
	Account account.Account
	State   applicationledger.BalanceState
}

type BaselineState struct {
	Policy      domainprojection.Policy
	AsOf        financialdate.Date
	HorizonEnd  financialdate.Date
	Accounts    []AccountBalance
	Obligations []schedule.Obligation
	ManualFlows []schedule.ScheduledCashFlow
	CardFlows   []schedule.ScheduledCashFlow
	CardIssues  []CardPaymentIssue
	Receivables []schedule.Receivable
}

// CardPaymentIssue is loaded with the same database snapshot as the baseline.
// It deliberately describes missing/invalid authority rather than guessing a card payment.
type CardPaymentIssue struct{ Code, CycleID, PaymentIntentID string }

type SafeToSpendState struct {
	Baseline       BaselineState
	FundingAccount AccountBalance
}

type Repository interface {
	GetPolicy(context.Context, string) (domainprojection.Policy, error)
	ReplacePolicy(context.Context, domainprojection.Policy) (domainprojection.Policy, error)
	LoadBaselineState(context.Context, string, time.Time) (BaselineState, error)
	LoadSafeToSpendState(context.Context, string, time.Time, string) (SafeToSpendState, error)
}
