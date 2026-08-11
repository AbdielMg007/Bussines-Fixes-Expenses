package projection

import (
	"strings"

	"runway/backend/internal/domain/account"
	"runway/backend/internal/domain/financialdate"
	"runway/backend/internal/domain/money"
)

type SafeToSpendStatus struct{ value string }

var (
	safeStatus                        = SafeToSpendStatus{value: "safe"}
	constrainedByFutureCashFlowStatus = SafeToSpendStatus{value: "constrained_by_future_cash_flow"}
	constrainedByFundingBalanceStatus = SafeToSpendStatus{value: "constrained_by_funding_balance"}
	alreadyBelowReserveStatus         = SafeToSpendStatus{value: "already_below_reserve"}
	unsupportedFundingTypeStatus      = SafeToSpendStatus{value: "unsupported_funding_type"}
)

func SafeStatus() SafeToSpendStatus                        { return safeStatus }
func ConstrainedByFutureCashFlowStatus() SafeToSpendStatus { return constrainedByFutureCashFlowStatus }
func ConstrainedByFundingBalanceStatus() SafeToSpendStatus { return constrainedByFundingBalanceStatus }
func AlreadyBelowReserveStatus() SafeToSpendStatus         { return alreadyBelowReserveStatus }
func UnsupportedFundingTypeStatus() SafeToSpendStatus      { return unsupportedFundingTypeStatus }
func (s SafeToSpendStatus) String() string                 { return s.value }

type SafeToSpendResult struct {
	AsOf                   financialdate.Date
	Currency               money.Currency
	FundingAccountID       string
	FundingAccountBalance  money.Balance
	Reserve                money.Money
	OpeningLiquidBalance   money.Balance
	BaselineMinimumBalance money.Balance
	SafeToSpend            money.Money
	Status                 SafeToSpendStatus
	LimitingEventID        string
	LimitingDate           *financialdate.Date
	EarliestBreachEventID  string
	EarliestBreachDate     *financialdate.Date
	Deficit                money.Money
	PolicyID               string
	PolicyVersion          int64
	BaselineProjection     Result
}

func CalculateSafeToSpend(baseline Result, fundingAccount account.Account, fundingBalance money.Balance) (SafeToSpendResult, error) {
	if strings.TrimSpace(fundingAccount.ID()) == "" || baseline.PolicyVersion < 1 || strings.TrimSpace(baseline.PolicyID) == "" {
		return SafeToSpendResult{}, ErrInvalidSafeToSpendInput
	}
	if fundingAccount.Currency() != baseline.Currency || fundingBalance.Currency() != baseline.Currency || baseline.Reserve.Currency() != baseline.Currency {
		return SafeToSpendResult{}, money.ErrCurrencyMismatch
	}
	zero, err := money.Zero(baseline.Currency)
	if err != nil {
		return SafeToSpendResult{}, err
	}
	result := SafeToSpendResult{
		AsOf: baseline.AsOf, Currency: baseline.Currency, FundingAccountID: fundingAccount.ID(),
		FundingAccountBalance: fundingBalance, Reserve: baseline.Reserve,
		OpeningLiquidBalance: baseline.OpeningBalance, BaselineMinimumBalance: baseline.MinimumBalance,
		SafeToSpend: zero, Deficit: zero, PolicyID: baseline.PolicyID, PolicyVersion: baseline.PolicyVersion,
		BaselineProjection: baseline,
	}
	if fundingAccount.Type() != account.Cash() && fundingAccount.Type() != account.Bank() {
		result.Status = UnsupportedFundingTypeStatus()
		return result, nil
	}

	reserveBalance, err := money.NewBalance(baseline.Reserve.MinorUnits(), baseline.Currency)
	if err != nil {
		return SafeToSpendResult{}, err
	}
	minimumComparison, err := baseline.MinimumBalance.Compare(reserveBalance)
	if err != nil {
		return SafeToSpendResult{}, err
	}
	if minimumComparison < 0 {
		deficitBalance, err := reserveBalance.Subtract(baseline.MinimumBalance)
		if err != nil {
			return SafeToSpendResult{}, err
		}
		deficit, err := money.New(deficitBalance.MinorUnits(), baseline.Currency)
		if err != nil {
			return SafeToSpendResult{}, err
		}
		breachEventID, breachDate, err := earliestReserveBreach(baseline, reserveBalance)
		if err != nil {
			return SafeToSpendResult{}, err
		}
		result.Status = AlreadyBelowReserveStatus()
		result.Deficit = deficit
		result.LimitingEventID = baseline.MinimumEventID
		result.LimitingDate = minimumPointDate(baseline)
		result.EarliestBreachEventID = breachEventID
		result.EarliestBreachDate = breachDate
		return result, nil
	}

	headroomBalance, err := baseline.MinimumBalance.Subtract(reserveBalance)
	if err != nil {
		return SafeToSpendResult{}, err
	}
	headroom, err := money.New(headroomBalance.MinorUnits(), baseline.Currency)
	if err != nil {
		return SafeToSpendResult{}, err
	}
	if fundingBalance.MinorUnits() <= 0 {
		result.Status = ConstrainedByFundingBalanceStatus()
		return result, nil
	}
	fundingMagnitude, err := money.New(fundingBalance.MinorUnits(), baseline.Currency)
	if err != nil {
		return SafeToSpendResult{}, err
	}
	comparison, err := headroom.Compare(fundingMagnitude)
	if err != nil {
		return SafeToSpendResult{}, err
	}
	switch {
	case comparison < 0:
		result.SafeToSpend = headroom
		result.Status = ConstrainedByFutureCashFlowStatus()
		result.LimitingEventID = baseline.MinimumEventID
		result.LimitingDate = minimumPointDate(baseline)
	case comparison > 0:
		result.SafeToSpend = fundingMagnitude
		result.Status = ConstrainedByFundingBalanceStatus()
	default:
		result.SafeToSpend = headroom
		result.Status = SafeStatus()
	}
	return result, nil
}

func earliestReserveBreach(baseline Result, reserve money.Balance) (string, *financialdate.Date, error) {
	comparison, err := baseline.OpeningBalance.Compare(reserve)
	if err != nil {
		return "", nil, err
	}
	if comparison < 0 {
		date := baseline.AsOf
		return "", &date, nil
	}
	for _, applied := range baseline.Events {
		comparison, err := applied.BalanceAfter.Compare(reserve)
		if err != nil {
			return "", nil, err
		}
		if comparison < 0 {
			date := applied.Event.FinancialDate()
			return applied.Event.ID(), &date, nil
		}
	}
	return "", nil, ErrInvalidSafeToSpendInput
}

func minimumPointDate(baseline Result) *financialdate.Date {
	if baseline.MinimumDate != nil {
		date := *baseline.MinimumDate
		return &date
	}
	date := baseline.AsOf
	return &date
}
