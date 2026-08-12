package projection

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"runway/backend/internal/domain/account"
	"runway/backend/internal/domain/financialdate"
	domainledger "runway/backend/internal/domain/ledger"
	"runway/backend/internal/domain/money"
	domainprojection "runway/backend/internal/domain/projection"
	"runway/backend/internal/domain/schedule"
	applicationledger "runway/backend/internal/ledger"
)

func TestAssembleBaselineUsesLedgerTruthAndInclusionRules(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	policy := applicationTestPolicy(t, domainprojection.ConfirmedInflowsOnly(), domainprojection.AllActiveLiquidSelection(), nil)
	asOf := applicationDate(t, "2026-08-10")
	end, _ := asOf.AddDays(policy.HorizonDays())
	bank := applicationAccount(t, "bank", account.Bank(), account.ActiveStatus(), now)
	cash := applicationAccount(t, "cash", account.Cash(), account.ActiveStatus(), now)
	bankBalance, _ := money.NewBalance(300_000, money.MXN())
	cashBalance, _ := money.NewBalance(-40_000, money.MXN())

	obligationAmount, _ := money.New(226_400, money.MXN())
	obligation, _ := schedule.NewObligation("car", "owner", "Car", obligationAmount, schedule.OneTime(), applicationDate(t, "2026-08-15"), nil, now)
	payroll := applicationFlow(t, "payroll", 788_700, schedule.Inflow(), "2026-08-15", schedule.ExactAmount(), schedule.ExactDate(), schedule.EligibleForPolicy(), schedule.ScheduledFlow(), now)
	estimated := applicationFlow(t, "estimated", 10_000, schedule.Inflow(), "2026-08-16", schedule.EstimatedAmount(), schedule.ExactDate(), schedule.EligibleForPolicy(), schedule.ScheduledFlow(), now)
	conservativeOutflow := applicationFlow(t, "outflow", 20_000, schedule.Outflow(), "2026-08-17", schedule.EstimatedAmount(), schedule.EstimatedDate(), schedule.ExcludedFromPolicy(), schedule.ScheduledFlow(), now)
	cancelled := applicationFlow(t, "cancelled", 1_000, schedule.Outflow(), "2026-08-18", schedule.ExactAmount(), schedule.ExactDate(), schedule.EligibleForPolicy(), schedule.CancelledFlow(), now)
	settled := applicationFlow(t, "settled", 1_000, schedule.Outflow(), "2026-08-19", schedule.ExactAmount(), schedule.ExactDate(), schedule.EligibleForPolicy(), schedule.SettledFlow(), now)

	confirmed := applicationReceivable(t, "confirmed", 350_000, 50_000, "2026-08-20", schedule.Confirmed(), schedule.PartialReceivable(), now)
	expected := applicationReceivable(t, "expected-receivable", 100_000, 0, "2026-08-21", schedule.Expected(), schedule.OpenReceivable(), now)
	uncertain := applicationReceivable(t, "uncertain", 100_000, 0, "2026-08-22", schedule.Uncertain(), schedule.OpenReceivable(), now)
	undated := applicationReceivable(t, "undated", 3_500_000, 0, "", schedule.Uncertain(), schedule.OpenReceivable(), now)

	result, err := AssembleAndCalculate(BaselineState{
		Policy: policy, AsOf: asOf, HorizonEnd: end,
		Accounts:    []AccountBalance{{Account: bank, State: stateWithSnapshot(t, bank, bankBalance, now)}, {Account: cash, State: stateWithSnapshot(t, cash, cashBalance, now)}},
		Obligations: []schedule.Obligation{obligation},
		ManualFlows: []schedule.ScheduledCashFlow{estimated, settled, payroll, cancelled, conservativeOutflow},
		Receivables: []schedule.Receivable{uncertain, confirmed, undated, expected},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.OpeningBalance.MinorUnits() != 260_000 {
		t.Fatalf("opening = %d", result.OpeningBalance.MinorUnits())
	}
	wantIDs := []string{"obligation_occurrence:car@2026-08-15", "manual_scheduled_flow:payroll", "manual_scheduled_flow:outflow", "receivable:confirmed"}
	if len(result.Events) != len(wantIDs) {
		t.Fatalf("events = %+v", result.Events)
	}
	for index, want := range wantIDs {
		if result.Events[index].Event.ID() != want {
			t.Fatalf("event %d = %s, want %s", index, result.Events[index].Event.ID(), want)
		}
	}
	if result.Events[0].BalanceAfter.MinorUnits() != 33_600 || result.Events[1].BalanceAfter.MinorUnits() != 822_300 {
		t.Fatalf("same-day trace = %+v", result.Events[:2])
	}
	if result.Events[3].Event.Amount().MinorUnits() != 300_000 {
		t.Fatalf("partial receivable projected %d", result.Events[3].Event.Amount().MinorUnits())
	}
	if result.Events[0].Event.InclusionBasis() != domainprojection.MandatoryObligationOutflow() ||
		result.Events[1].Event.InclusionBasis() != domainprojection.EligibleManualInflow() ||
		result.Events[3].Event.InclusionBasis() != domainprojection.ConfirmedReceivable() {
		t.Fatalf("inclusion bases = %s, %s, %s", result.Events[0].Event.InclusionBasis().String(), result.Events[1].Event.InclusionBasis().String(), result.Events[3].Event.InclusionBasis().String())
	}
	if certainty, ok := result.Events[3].Event.SourceCertainty(); !ok || certainty != schedule.Confirmed() {
		t.Fatalf("confirmed receivable certainty = %s, %t", certainty.String(), ok)
	}
	if len(result.Exclusions) != 6 {
		t.Fatalf("exclusions = %+v", result.Exclusions)
	}
}

func TestAssembleIgnoresHistoricalCardFlowsAndPreservesCardIndeterminacy(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	policy := applicationTestPolicy(t, domainprojection.ConfirmedInflowsOnly(), domainprojection.AllActiveLiquidSelection(), nil)
	asOf := applicationDate(t, "2026-08-10")
	end, _ := asOf.AddDays(policy.HorizonDays())
	bank := applicationAccount(t, "bank", account.Bank(), account.ActiveStatus(), now)
	opening, _ := money.NewBalance(1_000_000, money.MXN())
	amount, _ := money.New(280_000, money.MXN())
	date := applicationDate(t, "2026-09-09")
	cancelled, err := schedule.RestoreScheduledCashFlow("cancelled-card-flow", "owner", schedule.CreditCardPaymentIntentSource(), "intent", amount, schedule.Outflow(), date, schedule.CancelledFlow(), schedule.ExactAmount(), schedule.ExactDate(), schedule.EligibleForPolicy(), "", now, now)
	if err != nil {
		t.Fatal(err)
	}
	result, err := AssembleAndCalculate(BaselineState{Policy: policy, AsOf: asOf, HorizonEnd: end, Accounts: []AccountBalance{{Account: bank, State: stateWithSnapshot(t, bank, opening, now)}}, CardFlows: nil, CardIssues: []CardPaymentIssue{{Code: "card_payment_intent_needs_review", CycleID: "cycle", PaymentIntentID: "intent"}}})
	if err != nil || result.Completeness != domainprojection.ProjectionIndeterminate || len(result.Events) != 0 {
		t.Fatalf("needs-review projection=%+v err=%v", result, err)
	}
	// The repository filter is what keeps this persisted historical flow out of CardFlows.
	if !cancelled.Status().IsCancelled() {
		t.Fatal("test fixture is not cancelled")
	}
	settled, err := schedule.RestoreScheduledCashFlow("settled-card-flow", "owner", schedule.CreditCardPaymentIntentSource(), "intent", amount, schedule.Outflow(), date, schedule.SettledFlow(), schedule.ExactAmount(), schedule.ExactDate(), schedule.EligibleForPolicy(), "posted-source", now, now)
	if err != nil {
		t.Fatal(err)
	}
	result, err = AssembleAndCalculate(BaselineState{Policy: policy, AsOf: asOf, HorizonEnd: end, Accounts: []AccountBalance{{Account: bank, State: stateWithSnapshot(t, bank, opening, now)}}})
	if err != nil || result.Completeness != domainprojection.ProjectionComplete || len(result.Events) != 0 || !settled.Status().IsSettled() {
		t.Fatalf("settled projection=%+v err=%v", result, err)
	}
}

func TestReceivableCertaintyProvenanceAndInclusionBasisRemainIndependent(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	policy := applicationTestPolicy(t, domainprojection.IncludeExpectedInflows(), domainprojection.ExplicitSelection(), nil)
	asOf := applicationDate(t, "2026-08-10")
	end, _ := asOf.AddDays(policy.HorizonDays())
	confirmed := applicationReceivableWithProvenance(t, "confirmed-estimated-date", 100, "2026-08-12", schedule.Confirmed(), schedule.ExactAmount(), schedule.EstimatedDate(), now)
	expected := applicationReceivableWithProvenance(t, "expected-exact-date", 200, "2026-08-13", schedule.Expected(), schedule.EstimatedAmount(), schedule.ExactDate(), now)
	uncertain := applicationReceivableWithProvenance(t, "uncertain-exact", 300, "2026-08-14", schedule.Uncertain(), schedule.ExactAmount(), schedule.ExactDate(), now)

	result, err := AssembleAndCalculate(BaselineState{Policy: policy, AsOf: asOf, HorizonEnd: end, Receivables: []schedule.Receivable{uncertain, expected, confirmed}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 2 || len(result.Exclusions) != 1 || result.Exclusions[0].SourceID != "uncertain-exact" {
		t.Fatalf("events/exclusions = %+v / %+v", result.Events, result.Exclusions)
	}
	confirmedEvent := result.Events[0].Event
	if confirmedEvent.DateProvenance() != schedule.EstimatedDate() || confirmedEvent.AmountProvenance() != schedule.ExactAmount() || confirmedEvent.InclusionBasis() != domainprojection.ConfirmedReceivable() {
		t.Fatalf("confirmed metadata = %+v", confirmedEvent)
	}
	if certainty, ok := confirmedEvent.SourceCertainty(); !ok || certainty != schedule.Confirmed() {
		t.Fatalf("confirmed certainty = %s, %t", certainty.String(), ok)
	}
	expectedEvent := result.Events[1].Event
	if expectedEvent.DateProvenance() != schedule.ExactDate() || expectedEvent.AmountProvenance() != schedule.EstimatedAmount() || expectedEvent.InclusionBasis() != domainprojection.ExpectedReceivableAllowedByPolicy() {
		t.Fatalf("expected metadata = %+v", expectedEvent)
	}
	if certainty, ok := expectedEvent.SourceCertainty(); !ok || certainty != schedule.Expected() {
		t.Fatalf("expected certainty = %s, %t", certainty.String(), ok)
	}
	if result.ClosingBalance.MinorUnits() != 300 {
		t.Fatalf("metadata changed arithmetic: %d", result.ClosingBalance.MinorUnits())
	}
}

func TestAllProjectionSourcesUseStrictFutureInclusiveHorizon(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	selection, _ := domainprojection.NewAccountSelection(domainprojection.ExplicitSelection(), nil)
	reserve, _ := money.Zero(money.MXN())
	policy, _ := domainprojection.NewPolicy("owner", "owner", money.MXN(), 1, reserve, "America/Mexico_City", selection, domainprojection.IncludeExpectedInflows(), domainprojection.OutflowsBeforeInflows(), now)
	asOf := applicationDate(t, "2026-08-10")
	end := applicationDate(t, "2026-08-11")
	amount, _ := money.New(1, money.MXN())
	obligationToday, _ := schedule.NewObligation("obligation-today", "owner", "Today", amount, schedule.OneTime(), asOf, nil, now)
	obligationTomorrow, _ := schedule.NewObligation("obligation-tomorrow", "owner", "Tomorrow", amount, schedule.OneTime(), end, nil, now)
	obligationAfter, _ := schedule.NewObligation("obligation-after", "owner", "After", amount, schedule.OneTime(), applicationDate(t, "2026-08-12"), nil, now)
	flows := []schedule.ScheduledCashFlow{
		applicationFlow(t, "flow-today", 1, schedule.Outflow(), "2026-08-10", schedule.ExactAmount(), schedule.ExactDate(), schedule.EligibleForPolicy(), schedule.ScheduledFlow(), now),
		applicationFlow(t, "flow-tomorrow", 1, schedule.Outflow(), "2026-08-11", schedule.ExactAmount(), schedule.ExactDate(), schedule.EligibleForPolicy(), schedule.ScheduledFlow(), now),
		applicationFlow(t, "flow-after", 1, schedule.Outflow(), "2026-08-12", schedule.ExactAmount(), schedule.ExactDate(), schedule.EligibleForPolicy(), schedule.ScheduledFlow(), now),
	}
	receivables := []schedule.Receivable{
		applicationReceivable(t, "receivable-today", 1, 0, "2026-08-10", schedule.Confirmed(), schedule.OpenReceivable(), now),
		applicationReceivable(t, "receivable-tomorrow", 1, 0, "2026-08-11", schedule.Confirmed(), schedule.OpenReceivable(), now),
		applicationReceivable(t, "receivable-after", 1, 0, "2026-08-12", schedule.Confirmed(), schedule.OpenReceivable(), now),
	}
	result, err := AssembleAndCalculate(BaselineState{
		Policy: policy, AsOf: asOf, HorizonEnd: end,
		Obligations: []schedule.Obligation{obligationAfter, obligationToday, obligationTomorrow},
		ManualFlows: flows, Receivables: receivables,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"obligation_occurrence:obligation-tomorrow@2026-08-11", "manual_scheduled_flow:flow-tomorrow", "receivable:receivable-tomorrow"}
	if len(result.Events) != len(want) {
		t.Fatalf("boundary events = %+v", result.Events)
	}
	for index := range want {
		if result.Events[index].Event.ID() != want[index] {
			t.Fatalf("event %d = %s, want %s", index, result.Events[index].Event.ID(), want[index])
		}
	}
}

func TestExpectedInflowsRequireExplicitPolicyAndUncertainNeverEnter(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	policy := applicationTestPolicy(t, domainprojection.IncludeExpectedInflows(), domainprojection.ExplicitSelection(), nil)
	asOf := applicationDate(t, "2026-08-10")
	end, _ := asOf.AddDays(policy.HorizonDays())
	expectedFlow := applicationFlow(t, "estimated", 100, schedule.Inflow(), "2026-08-11", schedule.EstimatedAmount(), schedule.EstimatedDate(), schedule.EligibleForPolicy(), schedule.ScheduledFlow(), now)
	expectedReceivable := applicationReceivable(t, "expected", 200, 0, "2026-08-12", schedule.Expected(), schedule.OpenReceivable(), now)
	uncertain := applicationReceivable(t, "uncertain", 300, 0, "2026-08-13", schedule.Uncertain(), schedule.OpenReceivable(), now)
	result, err := AssembleAndCalculate(BaselineState{Policy: policy, AsOf: asOf, HorizonEnd: end, ManualFlows: []schedule.ScheduledCashFlow{expectedFlow}, Receivables: []schedule.Receivable{uncertain, expectedReceivable}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 2 || result.ClosingBalance.MinorUnits() != 300 {
		t.Fatalf("expected result = %+v", result)
	}
	if result.Events[0].Event.InclusionBasis() != domainprojection.ExpectedManualInflowAllowedByPolicy() || result.Events[1].Event.InclusionBasis() != domainprojection.ExpectedReceivableAllowedByPolicy() {
		t.Fatalf("expected inclusion bases = %s, %s", result.Events[0].Event.InclusionBasis().String(), result.Events[1].Event.InclusionBasis().String())
	}
	if len(result.Exclusions) != 1 || result.Exclusions[0].SourceID != "uncertain" {
		t.Fatalf("uncertain exclusions = %+v", result.Exclusions)
	}
}

func TestObligationProjectionRespectsFutureBoundaryAndInactiveDate(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	policy := applicationTestPolicy(t, domainprojection.ConfirmedInflowsOnly(), domainprojection.ExplicitSelection(), nil)
	asOf := applicationDate(t, "2026-08-10")
	end, _ := asOf.AddDays(policy.HorizonDays())
	amount, _ := money.New(100, money.MXN())
	historical, _ := schedule.NewObligation("historical", "owner", "Historical", amount, schedule.OneTime(), applicationDate(t, "2026-08-01"), nil, now)
	future, _ := schedule.NewObligation("future", "owner", "Future", amount, schedule.Biweekly(), applicationDate(t, "2026-08-01"), nil, now)
	future, _ = future.Archive(applicationDate(t, "2026-08-20"), now.Add(time.Hour))
	result, err := AssembleAndCalculate(BaselineState{Policy: policy, AsOf: asOf, HorizonEnd: end, Obligations: []schedule.Obligation{historical, future}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 1 || result.Events[0].Event.ID() != "obligation_occurrence:future@2026-08-15" {
		t.Fatalf("obligation events = %+v", result.Events)
	}
}

func TestReceivableFinalStatesAreExcluded(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	policy := applicationTestPolicy(t, domainprojection.IncludeExpectedInflows(), domainprojection.ExplicitSelection(), nil)
	asOf := applicationDate(t, "2026-08-10")
	end, _ := asOf.AddDays(policy.HorizonDays())
	collected := applicationReceivable(t, "collected", 100, 100, "2026-08-12", schedule.Confirmed(), schedule.CollectedReceivable(), now)
	cancelled := applicationReceivable(t, "cancelled", 100, 25, "2026-08-13", schedule.Confirmed(), schedule.CancelledReceivable(), now)
	result, err := AssembleAndCalculate(BaselineState{Policy: policy, AsOf: asOf, HorizonEnd: end, Receivables: []schedule.Receivable{collected, cancelled}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 0 || len(result.Exclusions) != 2 || result.Exclusions[0].Reasons[0] != domainprojection.ExclusionCancelled || result.Exclusions[1].Reasons[0] != domainprojection.ExclusionCollected {
		t.Fatalf("final receivables = %+v", result)
	}
}

func TestOpeningLiquidityValidationAndOverflow(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	asOf := applicationDate(t, "2026-08-10")
	for _, test := range []struct {
		name        string
		accountType account.AccountType
		status      account.Status
	}{
		{name: "credit card", accountType: account.CreditCard(), status: account.ActiveStatus()},
		{name: "loan", accountType: account.Loan(), status: account.ActiveStatus()},
		{name: "archived bank", accountType: account.Bank(), status: account.ArchivedStatus()},
	} {
		t.Run(test.name, func(t *testing.T) {
			financialAccount := applicationAccount(t, "invalid", test.accountType, test.status, now)
			policy := applicationTestPolicy(t, domainprojection.ConfirmedInflowsOnly(), domainprojection.ExplicitSelection(), []string{"invalid"})
			end, _ := asOf.AddDays(policy.HorizonDays())
			zero, _ := money.ZeroBalance(money.MXN())
			_, err := AssembleAndCalculate(BaselineState{Policy: policy, AsOf: asOf, HorizonEnd: end, Accounts: []AccountBalance{{Account: financialAccount, State: stateWithSnapshot(t, financialAccount, zero, now)}}})
			if !errors.Is(err, domainprojection.ErrInvalidAccountSelection) {
				t.Fatalf("error = %v", err)
			}
		})
	}

	policy := applicationTestPolicy(t, domainprojection.ConfirmedInflowsOnly(), domainprojection.AllActiveLiquidSelection(), nil)
	end, _ := asOf.AddDays(policy.HorizonDays())
	first := applicationAccount(t, "first", account.Bank(), account.ActiveStatus(), now)
	second := applicationAccount(t, "second", account.Cash(), account.ActiveStatus(), now)
	max, _ := money.NewBalance(math.MaxInt64, money.MXN())
	one, _ := money.NewBalance(1, money.MXN())
	_, err := AssembleAndCalculate(BaselineState{Policy: policy, AsOf: asOf, HorizonEnd: end, Accounts: []AccountBalance{{Account: first, State: stateWithSnapshot(t, first, max, now)}, {Account: second, State: stateWithSnapshot(t, second, one, now)}}})
	if !errors.Is(err, money.ErrMonetaryAmountOverflow) {
		t.Fatalf("opening overflow error = %v", err)
	}
}

func TestServiceUsesInjectedClockAndMapsMissingPolicy(t *testing.T) {
	now := time.Date(2026, 8, 11, 4, 30, 0, 0, time.UTC)
	repository := &fakeProjectionRepository{loadError: ErrPolicyNotFound}
	service, _ := NewService(repository, ServiceOptions{Clock: func() time.Time { return now }})
	if _, err := service.CalculateBaseline(context.Background(), "owner"); !errors.Is(err, ErrConfigurationRequired) {
		t.Fatalf("configuration error = %v", err)
	}
}

func TestServiceCalculatesFundingSpecificSafeToSpendFromOneLoadedState(t *testing.T) {
	now := time.Date(2026, 8, 10, 18, 0, 0, 0, time.UTC)
	bank := applicationAccount(t, "bank", account.Bank(), account.ActiveStatus(), now)
	bankBalance, _ := money.NewBalance(260_000, money.MXN())
	selection, _ := domainprojection.NewAccountSelection(domainprojection.ExplicitSelection(), []string{"bank"})
	reserve, _ := money.New(30_000, money.MXN())
	policy, _ := domainprojection.NewPolicy("owner", "owner", money.MXN(), 30, reserve, "America/Mexico_City", selection, domainprojection.ConfirmedInflowsOnly(), domainprojection.OutflowsBeforeInflows(), now)
	asOf := applicationDate(t, "2026-08-10")
	horizonEnd, _ := asOf.AddDays(30)
	outflow := applicationFlow(t, "car", 226_400, schedule.Outflow(), "2026-08-15", schedule.ExactAmount(), schedule.ExactDate(), schedule.EligibleForPolicy(), schedule.ScheduledFlow(), now)
	inflow := applicationFlow(t, "payroll", 788_700, schedule.Inflow(), "2026-08-15", schedule.ExactAmount(), schedule.ExactDate(), schedule.EligibleForPolicy(), schedule.ScheduledFlow(), now)
	accountState := AccountBalance{Account: bank, State: stateWithSnapshot(t, bank, bankBalance, now)}
	repository := &fakeProjectionRepository{safeToSpendState: SafeToSpendState{
		Baseline:       BaselineState{Policy: policy, AsOf: asOf, HorizonEnd: horizonEnd, Accounts: []AccountBalance{accountState}, ManualFlows: []schedule.ScheduledCashFlow{inflow, outflow}},
		FundingAccount: accountState,
	}}
	service, _ := NewService(repository, ServiceOptions{Clock: func() time.Time { return now }})

	result, err := service.CalculateSafeToSpend(context.Background(), "owner", "bank")
	if err != nil {
		t.Fatal(err)
	}
	if result.SafeToSpend.MinorUnits() != 3_600 || result.Status != domainprojection.ConstrainedByFutureCashFlowStatus() || result.FundingAccountBalance.MinorUnits() != 260_000 {
		t.Fatalf("safe-to-spend = %+v", result)
	}
}

func TestServiceSafeToSpendFundingValidation(t *testing.T) {
	now := time.Date(2026, 8, 10, 18, 0, 0, 0, time.UTC)
	asOf := applicationDate(t, "2026-08-10")
	reserve, _ := money.Zero(money.MXN())
	selection, _ := domainprojection.NewAccountSelection(domainprojection.ExplicitSelection(), nil)
	policy, _ := domainprojection.NewPolicy("owner", "owner", money.MXN(), 30, reserve, "America/Mexico_City", selection, domainprojection.ConfirmedInflowsOnly(), domainprojection.OutflowsBeforeInflows(), now)
	horizonEnd, _ := asOf.AddDays(30)
	zero, _ := money.ZeroBalance(money.MXN())
	otherOwnerBank, _ := account.Restore("other-bank", "other-owner", "Other bank", account.Bank(), money.MXN(), account.ActiveStatus(), now, now)

	for _, test := range []struct {
		name      string
		financial account.Account
		wantError error
		wantState domainprojection.SafeToSpendStatus
	}{
		{name: "non-selected bank", financial: applicationAccount(t, "bank", account.Bank(), account.ActiveStatus(), now), wantError: ErrFundingAccountNotSelected},
		{name: "archived cash", financial: applicationAccount(t, "cash", account.Cash(), account.ArchivedStatus(), now), wantError: ErrFundingAccountInvalid},
		{name: "other owner", financial: otherOwnerBank, wantError: applicationledger.ErrNotFound},
		{name: "credit card unsupported", financial: applicationAccount(t, "card", account.CreditCard(), account.ActiveStatus(), now), wantState: domainprojection.UnsupportedFundingTypeStatus()},
	} {
		t.Run(test.name, func(t *testing.T) {
			funding := AccountBalance{Account: test.financial, State: stateWithSnapshot(t, test.financial, zero, now)}
			repository := &fakeProjectionRepository{safeToSpendState: SafeToSpendState{Baseline: BaselineState{Policy: policy, AsOf: asOf, HorizonEnd: horizonEnd}, FundingAccount: funding}}
			service, _ := NewService(repository, ServiceOptions{Clock: func() time.Time { return now }})
			result, err := service.CalculateSafeToSpend(context.Background(), "owner", test.financial.ID())
			if test.wantError != nil {
				if !errors.Is(err, test.wantError) {
					t.Fatalf("error = %v", err)
				}
				return
			}
			if err != nil || result.Status != test.wantState || result.SafeToSpend.MinorUnits() != 0 {
				t.Fatalf("result = %+v, error = %v", result, err)
			}
		})
	}
}

type fakeProjectionRepository struct {
	policy           domainprojection.Policy
	state            BaselineState
	safeToSpendState SafeToSpendState
	loadError        error
}

func (r *fakeProjectionRepository) GetPolicy(context.Context, string) (domainprojection.Policy, error) {
	return r.policy, nil
}
func (r *fakeProjectionRepository) ReplacePolicy(_ context.Context, value domainprojection.Policy) (domainprojection.Policy, error) {
	r.policy = value
	return value, nil
}
func (r *fakeProjectionRepository) LoadBaselineState(context.Context, string, time.Time) (BaselineState, error) {
	return r.state, r.loadError
}
func (r *fakeProjectionRepository) LoadSafeToSpendState(context.Context, string, time.Time, string) (SafeToSpendState, error) {
	return r.safeToSpendState, r.loadError
}

func applicationTestPolicy(t *testing.T, inflow domainprojection.InflowPolicy, mode domainprojection.AccountSelectionMode, ids []string) domainprojection.Policy {
	t.Helper()
	selection, err := domainprojection.NewAccountSelection(mode, ids)
	if err != nil {
		t.Fatal(err)
	}
	reserve, _ := money.Zero(money.MXN())
	policy, err := domainprojection.NewPolicy("owner", "owner", money.MXN(), 60, reserve, "America/Mexico_City", selection, inflow, domainprojection.OutflowsBeforeInflows(), time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func applicationAccount(t *testing.T, id string, accountType account.AccountType, status account.Status, now time.Time) account.Account {
	t.Helper()
	value, err := account.Restore(id, "owner", id, accountType, money.MXN(), status, now, now)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func stateWithSnapshot(t *testing.T, financialAccount account.Account, balance money.Balance, now time.Time) applicationledger.BalanceState {
	t.Helper()
	snapshot, err := domainledger.NewBalanceSnapshot("snapshot-"+financialAccount.ID(), financialAccount.OwnerID(), financialAccount.ID(), balance, now, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	return applicationledger.BalanceState{Account: financialAccount, Snapshot: &snapshot}
}

func applicationFlow(t *testing.T, id string, minor int64, direction schedule.Direction, dateValue string, amountProvenance schedule.AmountProvenance, dateProvenance schedule.DateProvenance, inclusion schedule.InclusionEligibility, status schedule.FlowStatus, now time.Time) schedule.ScheduledCashFlow {
	t.Helper()
	amount, _ := money.New(minor, money.MXN())
	date := applicationDate(t, dateValue)
	settlementID := ""
	if status.IsSettled() {
		settlementID = "ledger-" + id
	}
	flow, err := schedule.RestoreScheduledCashFlow(id, "owner", schedule.ManualOtherSource(), id, amount, direction, date, status, amountProvenance, dateProvenance, inclusion, settlementID, now, now)
	if err != nil {
		t.Fatal(err)
	}
	return flow
}

func applicationReceivable(t *testing.T, id string, originalMinor, collectedMinor int64, dateValue string, certainty schedule.Certainty, status schedule.ReceivableStatus, now time.Time) schedule.Receivable {
	t.Helper()
	original, _ := money.New(originalMinor, money.MXN())
	collected, _ := money.New(collectedMinor, money.MXN())
	var expected *financialdate.Date
	var dateProvenance *schedule.DateProvenance
	if dateValue != "" {
		date := applicationDate(t, dateValue)
		expected = &date
		provenance := schedule.ExactDate()
		dateProvenance = &provenance
	}
	value, err := schedule.RestoreReceivable(
		id, "owner", id, original, collected, status, expected, certainty,
		schedule.ExactAmount(), dateProvenance, now, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func applicationReceivableWithProvenance(t *testing.T, id string, originalMinor int64, dateValue string, certainty schedule.Certainty, amountProvenance schedule.AmountProvenance, dateProvenance schedule.DateProvenance, now time.Time) schedule.Receivable {
	t.Helper()
	original, _ := money.New(originalMinor, money.MXN())
	zero, _ := money.Zero(money.MXN())
	date := applicationDate(t, dateValue)
	value, err := schedule.RestoreReceivable(
		id, "owner", id, original, zero, schedule.OpenReceivable(), &date, certainty,
		amountProvenance, &dateProvenance, now, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func applicationDate(t *testing.T, value string) financialdate.Date {
	t.Helper()
	date, err := financialdate.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	return date
}
