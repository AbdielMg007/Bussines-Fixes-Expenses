package projection

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"runway/backend/internal/domain/account"
	"runway/backend/internal/domain/financialdate"
	"runway/backend/internal/domain/money"
	domainprojection "runway/backend/internal/domain/projection"
	"runway/backend/internal/domain/schedule"
	applicationledger "runway/backend/internal/ledger"
)

type ServiceOptions struct {
	Clock func() time.Time
}

type Service struct {
	repository Repository
	clock      func() time.Time
}

func NewService(repository Repository, options ServiceOptions) (*Service, error) {
	if repository == nil {
		return nil, errors.New("invalid projection service configuration")
	}
	if options.Clock == nil {
		options.Clock = time.Now
	}
	return &Service{repository: repository, clock: options.Clock}, nil
}

func (s *Service) GetPolicy(ctx context.Context, ownerID string) (domainprojection.Policy, error) {
	if strings.TrimSpace(ownerID) == "" {
		return domainprojection.Policy{}, ErrInvalidOwner
	}
	return s.repository.GetPolicy(ctx, ownerID)
}

func (s *Service) ReplacePolicy(
	ctx context.Context,
	ownerID string,
	currency money.Currency,
	horizonDays int,
	reserve money.Money,
	timezone string,
	selection domainprojection.AccountSelection,
	inflowPolicy domainprojection.InflowPolicy,
	sameDayOrder domainprojection.SameDayOrder,
) (domainprojection.Policy, error) {
	if strings.TrimSpace(ownerID) == "" {
		return domainprojection.Policy{}, ErrInvalidOwner
	}
	now := s.clock().UTC()
	policy, err := domainprojection.NewPolicy(
		ownerID, ownerID, currency, horizonDays, reserve, timezone, selection,
		inflowPolicy, sameDayOrder, now,
	)
	if err != nil {
		return domainprojection.Policy{}, err
	}
	return s.repository.ReplacePolicy(ctx, policy)
}

func (s *Service) CalculateBaseline(ctx context.Context, ownerID string) (domainprojection.Result, error) {
	if strings.TrimSpace(ownerID) == "" {
		return domainprojection.Result{}, ErrInvalidOwner
	}
	state, err := s.repository.LoadBaselineState(ctx, ownerID, s.clock().UTC())
	if err != nil {
		if errors.Is(err, ErrPolicyNotFound) {
			return domainprojection.Result{}, ErrConfigurationRequired
		}
		return domainprojection.Result{}, err
	}
	return AssembleAndCalculate(state)
}

func (s *Service) CalculateSafeToSpend(ctx context.Context, ownerID, fundingAccountID string) (domainprojection.SafeToSpendResult, error) {
	if strings.TrimSpace(ownerID) == "" {
		return domainprojection.SafeToSpendResult{}, ErrInvalidOwner
	}
	if strings.TrimSpace(fundingAccountID) == "" {
		return domainprojection.SafeToSpendResult{}, ErrFundingAccountInvalid
	}
	fundingAccountID = strings.TrimSpace(fundingAccountID)
	state, err := s.repository.LoadSafeToSpendState(ctx, ownerID, s.clock().UTC(), fundingAccountID)
	if err != nil {
		if errors.Is(err, ErrPolicyNotFound) {
			return domainprojection.SafeToSpendResult{}, ErrConfigurationRequired
		}
		return domainprojection.SafeToSpendResult{}, err
	}
	baseline, err := AssembleAndCalculate(state.Baseline)
	if err != nil {
		return domainprojection.SafeToSpendResult{}, err
	}
	funding := state.FundingAccount.Account
	if funding.OwnerID() != ownerID {
		return domainprojection.SafeToSpendResult{}, applicationledger.ErrNotFound
	}
	if funding.Status().IsArchived() || funding.Currency() != baseline.Currency {
		return domainprojection.SafeToSpendResult{}, ErrFundingAccountInvalid
	}
	fundingBalance, err := applicationledger.ReconstructBalance(state.FundingAccount.State)
	if err != nil {
		return domainprojection.SafeToSpendResult{}, err
	}
	if funding.Type() == account.Cash() || funding.Type() == account.Bank() {
		selected := false
		for _, accountID := range baseline.SelectedAccountIDs {
			if accountID == funding.ID() {
				selected = true
				break
			}
		}
		if !selected {
			return domainprojection.SafeToSpendResult{}, ErrFundingAccountNotSelected
		}
	}
	return domainprojection.CalculateSafeToSpend(baseline, funding, fundingBalance.Balance)
}

func AssembleAndCalculate(state BaselineState) (domainprojection.Result, error) {
	opening, err := money.ZeroBalance(state.Policy.Currency())
	if err != nil {
		return domainprojection.Result{}, err
	}
	selectedIDs := make([]string, 0, len(state.Accounts))
	seenAccounts := make(map[string]struct{}, len(state.Accounts))
	for _, selected := range state.Accounts {
		financialAccount := selected.Account
		if financialAccount.OwnerID() != state.Policy.OwnerID() || financialAccount.Status().IsArchived() ||
			!financialAccount.Type().IsPotentiallyLiquidityEligible() || financialAccount.Currency() != state.Policy.Currency() {
			return domainprojection.Result{}, domainprojection.ErrInvalidAccountSelection
		}
		if _, exists := seenAccounts[financialAccount.ID()]; exists {
			return domainprojection.Result{}, domainprojection.ErrInvalidAccountSelection
		}
		seenAccounts[financialAccount.ID()] = struct{}{}
		reconstructed, err := applicationledger.ReconstructBalance(selected.State)
		if err != nil {
			return domainprojection.Result{}, err
		}
		opening, err = opening.Add(reconstructed.Balance)
		if err != nil {
			return domainprojection.Result{}, err
		}
		selectedIDs = append(selectedIDs, financialAccount.ID())
	}
	sort.Strings(selectedIDs)
	if state.Policy.AccountSelection().Mode() == domainprojection.ExplicitSelection() {
		expected := state.Policy.AccountSelection().AccountIDs()
		if len(expected) != len(selectedIDs) {
			return domainprojection.Result{}, domainprojection.ErrInvalidAccountSelection
		}
		for index := range expected {
			if expected[index] != selectedIDs[index] {
				return domainprojection.Result{}, domainprojection.ErrInvalidAccountSelection
			}
		}
	}

	events := make([]domainprojection.Event, 0)
	exclusions := make([]domainprojection.Exclusion, 0)
	from, err := state.AsOf.AddDays(1)
	if err != nil {
		return domainprojection.Result{}, err
	}
	for _, obligation := range state.Obligations {
		if obligation.OwnerID() != state.Policy.OwnerID() || obligation.Amount().Currency() != state.Policy.Currency() {
			return domainprojection.Result{}, domainprojection.ErrInvalidProjectionEvent
		}
		occurrences, err := schedule.ExpandObligation(obligation, from, state.HorizonEnd)
		if err != nil {
			return domainprojection.Result{}, err
		}
		for _, occurrence := range occurrences {
			event, err := domainprojection.NewEvent(
				"obligation_occurrence:"+occurrence.ID(), domainprojection.ObligationOccurrenceSource(), obligation.ID(),
				occurrence.FinancialDate(), occurrence.Amount(), schedule.Outflow(), occurrence.AmountProvenance(),
				occurrence.DateProvenance(), domainprojection.MandatoryObligationOutflow(), nil, obligation.DisplayName(),
			)
			if err != nil {
				return domainprojection.Result{}, err
			}
			events = append(events, event)
		}
	}

	for _, flow := range state.ManualFlows {
		if flow.OwnerID() != state.Policy.OwnerID() || !flow.SourceKind().IsManual() || flow.Amount().Currency() != state.Policy.Currency() {
			return domainprojection.Result{}, domainprojection.ErrInvalidProjectionEvent
		}
		if !withinFutureRange(flow.FinancialDate(), state.AsOf, state.HorizonEnd) {
			continue
		}
		if flow.Status().IsCancelled() {
			exclusion, _ := domainprojection.NewExclusion(domainprojection.ManualScheduledFlowSource(), flow.ID(), []domainprojection.ExclusionReason{domainprojection.ExclusionCancelled}, "")
			exclusions = append(exclusions, exclusion)
			continue
		}
		if flow.Status().IsSettled() {
			exclusion, _ := domainprojection.NewExclusion(domainprojection.ManualScheduledFlowSource(), flow.ID(), []domainprojection.ExclusionReason{domainprojection.ExclusionSettled}, "")
			exclusions = append(exclusions, exclusion)
			continue
		}
		if flow.Direction() == schedule.Inflow() && !manualInflowIncluded(flow, state.Policy.InflowPolicy()) {
			exclusion, _ := domainprojection.NewExclusion(domainprojection.ManualScheduledFlowSource(), flow.ID(), []domainprojection.ExclusionReason{domainprojection.ExclusionPolicyExcludedInflow}, "")
			exclusions = append(exclusions, exclusion)
			continue
		}
		event, err := domainprojection.NewEvent(
			"manual_scheduled_flow:"+flow.ID(), domainprojection.ManualScheduledFlowSource(), flow.ID(),
			flow.FinancialDate(), flow.Amount(), flow.Direction(), flow.AmountProvenance(), flow.DateProvenance(),
			manualInclusionBasis(flow, state.Policy.InflowPolicy()), nil, "",
		)
		if err != nil {
			return domainprojection.Result{}, err
		}
		events = append(events, event)
	}
	for _, flow := range state.CardFlows {
		if flow.OwnerID() != state.Policy.OwnerID() || !flow.SourceKind().IsCreditCardPaymentIntent() ||
			flow.Amount().Currency() != state.Policy.Currency() || !flow.Status().IsScheduled() {
			return domainprojection.Result{}, domainprojection.ErrInvalidProjectionEvent
		}
		if !withinFutureRange(flow.FinancialDate(), state.AsOf, state.HorizonEnd) {
			continue
		}
		event, err := domainprojection.NewEvent(
			"credit_card_payment_intent:"+flow.ID(), domainprojection.CreditCardPaymentIntentSource(), flow.SourceID(),
			flow.FinancialDate(), flow.Amount(), schedule.Outflow(), schedule.ExactAmount(), schedule.ExactDate(),
			domainprojection.AuthoritativeCardPaymentIntent(), nil, "",
		)
		if err != nil {
			return domainprojection.Result{}, err
		}
		events = append(events, event)
	}

	for _, receivable := range state.Receivables {
		if receivable.OwnerID() != state.Policy.OwnerID() || receivable.OriginalAmount().Currency() != state.Policy.Currency() {
			return domainprojection.Result{}, domainprojection.ErrInvalidProjectionEvent
		}
		date, hasDate := receivable.ExpectedDate()
		reasons := receivableExclusionReasons(receivable, hasDate, state.Policy.InflowPolicy())
		if len(reasons) > 0 {
			exclusion, err := domainprojection.NewExclusion(domainprojection.ReceivableSource(), receivable.ID(), reasons, receivable.DisplayName())
			if err != nil {
				return domainprojection.Result{}, err
			}
			exclusions = append(exclusions, exclusion)
			continue
		}
		if !withinFutureRange(date, state.AsOf, state.HorizonEnd) {
			continue
		}
		outstanding, err := receivable.OutstandingAmount()
		if err != nil {
			return domainprojection.Result{}, err
		}
		if outstanding.MinorUnits() == 0 {
			continue
		}
		dateProvenance, hasDateProvenance := receivable.DateProvenance()
		if !hasDateProvenance {
			return domainprojection.Result{}, domainprojection.ErrInvalidProjectionEvent
		}
		certainty := receivable.Certainty()
		event, err := domainprojection.NewEvent(
			"receivable:"+receivable.ID(), domainprojection.ReceivableSource(), receivable.ID(), date,
			outstanding, schedule.Inflow(), receivable.AmountProvenance(), dateProvenance,
			receivableInclusionBasis(certainty), &certainty, receivable.DisplayName(),
		)
		if err != nil {
			return domainprojection.Result{}, err
		}
		events = append(events, event)
	}

	return domainprojection.Calculate(domainprojection.Input{
		Policy: state.Policy, AsOf: state.AsOf, HorizonEnd: state.HorizonEnd,
		SelectedAccountIDs: selectedIDs, OpeningBalance: opening, Events: events, Exclusions: exclusions, Issues: mapCardIssues(state.CardIssues),
	})
}

func mapCardIssues(issues []CardPaymentIssue) []domainprojection.Issue {
	result := make([]domainprojection.Issue, 0, len(issues))
	for _, issue := range issues {
		result = append(result, domainprojection.Issue{Code: issue.Code, CycleID: issue.CycleID, PaymentIntentID: issue.PaymentIntentID})
	}
	return result
}

func manualInclusionBasis(flow schedule.ScheduledCashFlow, policy domainprojection.InflowPolicy) domainprojection.InclusionBasis {
	if flow.Direction() == schedule.Outflow() {
		return domainprojection.MandatoryManualOutflow()
	}
	if policy.IncludesExpected() &&
		(flow.AmountProvenance() != schedule.ExactAmount() || flow.DateProvenance() != schedule.ExactDate()) {
		return domainprojection.ExpectedManualInflowAllowedByPolicy()
	}
	return domainprojection.EligibleManualInflow()
}

func receivableInclusionBasis(certainty schedule.Certainty) domainprojection.InclusionBasis {
	if certainty == schedule.Confirmed() {
		return domainprojection.ConfirmedReceivable()
	}
	return domainprojection.ExpectedReceivableAllowedByPolicy()
}

func manualInflowIncluded(flow schedule.ScheduledCashFlow, policy domainprojection.InflowPolicy) bool {
	if flow.InclusionEligibility() != schedule.EligibleForPolicy() {
		return false
	}
	if policy.IncludesExpected() {
		return true
	}
	return flow.AmountProvenance() == schedule.ExactAmount() && flow.DateProvenance() == schedule.ExactDate()
}

func receivableExclusionReasons(receivable schedule.Receivable, hasDate bool, policy domainprojection.InflowPolicy) []domainprojection.ExclusionReason {
	reasons := make([]domainprojection.ExclusionReason, 0, 2)
	if receivable.Status() == schedule.CancelledReceivable() {
		return []domainprojection.ExclusionReason{domainprojection.ExclusionCancelled}
	}
	if receivable.Status() == schedule.CollectedReceivable() {
		return []domainprojection.ExclusionReason{domainprojection.ExclusionCollected}
	}
	if !hasDate {
		reasons = append(reasons, domainprojection.ExclusionUndatedReceivable)
	}
	if receivable.Certainty() == schedule.Uncertain() {
		reasons = append(reasons, domainprojection.ExclusionUncertainReceivable)
	} else if receivable.Certainty() == schedule.Expected() && !policy.IncludesExpected() {
		reasons = append(reasons, domainprojection.ExclusionPolicyExcludedInflow)
	}
	return reasons
}

func withinFutureRange(date, asOf, horizonEnd financialdate.Date) bool {
	afterAsOf, err := date.Compare(asOf)
	if err != nil || afterAsOf <= 0 {
		return false
	}
	beforeEnd, err := date.Compare(horizonEnd)
	return err == nil && beforeEnd <= 0
}

func ValidateSelectedAccount(accountValue account.Account, ownerID string, currency money.Currency) error {
	if accountValue.OwnerID() != ownerID {
		return applicationledger.ErrNotFound
	}
	if accountValue.Status().IsArchived() || !accountValue.Type().IsPotentiallyLiquidityEligible() || accountValue.Currency() != currency {
		return fmt.Errorf("%w: %s", domainprojection.ErrInvalidAccountSelection, accountValue.ID())
	}
	return nil
}
