package projection

import (
	"fmt"
	"sort"
	"strings"

	"runway/backend/internal/domain/financialdate"
	"runway/backend/internal/domain/money"
	"runway/backend/internal/domain/schedule"
)

type EventSourceKind struct{ value string }

var (
	obligationOccurrenceSource = EventSourceKind{value: "obligation_occurrence"}
	manualScheduledFlowSource  = EventSourceKind{value: "manual_scheduled_flow"}
	receivableEventSource      = EventSourceKind{value: "receivable"}
)

func ObligationOccurrenceSource() EventSourceKind { return obligationOccurrenceSource }
func ManualScheduledFlowSource() EventSourceKind  { return manualScheduledFlowSource }
func ReceivableSource() EventSourceKind           { return receivableEventSource }
func (s EventSourceKind) String() string          { return s.value }

func eventSourceRank(source EventSourceKind) int {
	switch source {
	case obligationOccurrenceSource:
		return 0
	case manualScheduledFlowSource:
		return 1
	case receivableEventSource:
		return 2
	default:
		return 3
	}
}

func ParseEventSourceKind(value string) (EventSourceKind, error) {
	switch value {
	case obligationOccurrenceSource.value:
		return obligationOccurrenceSource, nil
	case manualScheduledFlowSource.value:
		return manualScheduledFlowSource, nil
	case receivableEventSource.value:
		return receivableEventSource, nil
	default:
		return EventSourceKind{}, ErrInvalidProjectionEvent
	}
}

type ExclusionReason string

const (
	ExclusionUndatedReceivable    ExclusionReason = "undated_receivable"
	ExclusionUncertainReceivable  ExclusionReason = "uncertain_receivable"
	ExclusionPolicyExcludedInflow ExclusionReason = "policy_excluded_inflow"
	ExclusionCancelled            ExclusionReason = "cancelled"
	ExclusionSettled              ExclusionReason = "settled"
	ExclusionCollected            ExclusionReason = "collected"
)

type InclusionBasis struct{ value string }

var (
	mandatoryObligationOutflow          = InclusionBasis{value: "mandatory_obligation_outflow"}
	mandatoryManualOutflow              = InclusionBasis{value: "mandatory_manual_outflow"}
	eligibleManualInflow                = InclusionBasis{value: "eligible_manual_inflow"}
	expectedManualInflowAllowedByPolicy = InclusionBasis{value: "expected_manual_inflow_allowed_by_policy"}
	confirmedReceivable                 = InclusionBasis{value: "confirmed_receivable"}
	expectedReceivableAllowedByPolicy   = InclusionBasis{value: "expected_receivable_allowed_by_policy"}
)

func MandatoryObligationOutflow() InclusionBasis { return mandatoryObligationOutflow }
func MandatoryManualOutflow() InclusionBasis     { return mandatoryManualOutflow }
func EligibleManualInflow() InclusionBasis       { return eligibleManualInflow }
func ExpectedManualInflowAllowedByPolicy() InclusionBasis {
	return expectedManualInflowAllowedByPolicy
}
func ConfirmedReceivable() InclusionBasis { return confirmedReceivable }
func ExpectedReceivableAllowedByPolicy() InclusionBasis {
	return expectedReceivableAllowedByPolicy
}
func (b InclusionBasis) String() string { return b.value }

func ParseInclusionBasis(value string) (InclusionBasis, error) {
	switch value {
	case mandatoryObligationOutflow.value:
		return mandatoryObligationOutflow, nil
	case mandatoryManualOutflow.value:
		return mandatoryManualOutflow, nil
	case eligibleManualInflow.value:
		return eligibleManualInflow, nil
	case expectedManualInflowAllowedByPolicy.value:
		return expectedManualInflowAllowedByPolicy, nil
	case confirmedReceivable.value:
		return confirmedReceivable, nil
	case expectedReceivableAllowedByPolicy.value:
		return expectedReceivableAllowedByPolicy, nil
	default:
		return InclusionBasis{}, ErrInvalidProjectionEvent
	}
}

type Event struct {
	id               string
	sourceKind       EventSourceKind
	sourceID         string
	date             financialdate.Date
	amount           money.Money
	direction        schedule.Direction
	amountProvenance schedule.AmountProvenance
	dateProvenance   schedule.DateProvenance
	inclusionBasis   InclusionBasis
	sourceCertainty  *schedule.Certainty
	label            string
}

func NewEvent(
	id string,
	sourceKind EventSourceKind,
	sourceID string,
	date financialdate.Date,
	amount money.Money,
	direction schedule.Direction,
	amountProvenance schedule.AmountProvenance,
	dateProvenance schedule.DateProvenance,
	inclusionBasis InclusionBasis,
	sourceCertainty *schedule.Certainty,
	label string,
) (Event, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(sourceID) == "" || amount.MinorUnits() == 0 {
		return Event{}, ErrInvalidProjectionEvent
	}
	if _, err := ParseEventSourceKind(sourceKind.String()); err != nil {
		return Event{}, err
	}
	if _, err := financialdate.Parse(date.String()); err != nil {
		return Event{}, err
	}
	if _, err := money.New(amount.MinorUnits(), amount.Currency()); err != nil {
		return Event{}, err
	}
	if _, err := schedule.ParseDirection(direction.String()); err != nil {
		return Event{}, err
	}
	if _, err := schedule.ParseAmountProvenance(amountProvenance.String()); err != nil {
		return Event{}, err
	}
	if _, err := schedule.ParseDateProvenance(dateProvenance.String()); err != nil || dateProvenance.IsScenarioAssumed() {
		return Event{}, ErrInvalidProjectionEvent
	}
	if _, err := ParseInclusionBasis(inclusionBasis.String()); err != nil {
		return Event{}, err
	}
	if err := validateEventInclusion(sourceKind, direction, amountProvenance, dateProvenance, inclusionBasis, sourceCertainty); err != nil {
		return Event{}, err
	}
	var copiedCertainty *schedule.Certainty
	if sourceCertainty != nil {
		value := *sourceCertainty
		copiedCertainty = &value
	}
	return Event{
		id: strings.TrimSpace(id), sourceKind: sourceKind, sourceID: strings.TrimSpace(sourceID),
		date: date, amount: amount, direction: direction, amountProvenance: amountProvenance,
		dateProvenance: dateProvenance, inclusionBasis: inclusionBasis,
		sourceCertainty: copiedCertainty, label: strings.TrimSpace(label),
	}, nil
}

func validateEventInclusion(
	source EventSourceKind,
	direction schedule.Direction,
	amountProvenance schedule.AmountProvenance,
	dateProvenance schedule.DateProvenance,
	basis InclusionBasis,
	certainty *schedule.Certainty,
) error {
	switch source {
	case obligationOccurrenceSource:
		if direction != schedule.Outflow() || basis != mandatoryObligationOutflow || certainty != nil {
			return ErrInvalidProjectionEvent
		}
	case manualScheduledFlowSource:
		if certainty != nil || (direction == schedule.Outflow() && basis != mandatoryManualOutflow) ||
			(direction == schedule.Inflow() && basis != eligibleManualInflow && basis != expectedManualInflowAllowedByPolicy) {
			return ErrInvalidProjectionEvent
		}
		if direction == schedule.Inflow() {
			exact := amountProvenance == schedule.ExactAmount() && dateProvenance == schedule.ExactDate()
			if (basis == eligibleManualInflow && !exact) || (basis == expectedManualInflowAllowedByPolicy && exact) {
				return ErrInvalidProjectionEvent
			}
		}
	case receivableEventSource:
		if direction != schedule.Inflow() || certainty == nil {
			return ErrInvalidProjectionEvent
		}
		if _, err := schedule.ParseCertainty(certainty.String()); err != nil || *certainty == schedule.Uncertain() {
			return ErrInvalidProjectionEvent
		}
		if (*certainty == schedule.Confirmed() && basis != confirmedReceivable) ||
			(*certainty == schedule.Expected() && basis != expectedReceivableAllowedByPolicy) {
			return ErrInvalidProjectionEvent
		}
	default:
		return ErrInvalidProjectionEvent
	}
	return nil
}

func (e Event) ID() string                                  { return e.id }
func (e Event) SourceKind() EventSourceKind                 { return e.sourceKind }
func (e Event) SourceID() string                            { return e.sourceID }
func (e Event) FinancialDate() financialdate.Date           { return e.date }
func (e Event) Amount() money.Money                         { return e.amount }
func (e Event) Direction() schedule.Direction               { return e.direction }
func (e Event) AmountProvenance() schedule.AmountProvenance { return e.amountProvenance }
func (e Event) DateProvenance() schedule.DateProvenance     { return e.dateProvenance }
func (e Event) InclusionBasis() InclusionBasis              { return e.inclusionBasis }
func (e Event) Label() string                               { return e.label }
func (e Event) SourceCertainty() (schedule.Certainty, bool) {
	if e.sourceCertainty == nil {
		return schedule.Certainty{}, false
	}
	return *e.sourceCertainty, true
}

type Exclusion struct {
	SourceKind EventSourceKind
	SourceID   string
	Reasons    []ExclusionReason
	Label      string
}

func NewExclusion(sourceKind EventSourceKind, sourceID string, reasons []ExclusionReason, label string) (Exclusion, error) {
	if _, err := ParseEventSourceKind(sourceKind.String()); err != nil || strings.TrimSpace(sourceID) == "" || len(reasons) == 0 {
		return Exclusion{}, ErrInvalidProjectionEvent
	}
	seen := make(map[ExclusionReason]struct{}, len(reasons))
	canonical := make([]ExclusionReason, 0, len(reasons))
	for _, reason := range reasons {
		switch reason {
		case ExclusionUndatedReceivable, ExclusionUncertainReceivable, ExclusionPolicyExcludedInflow,
			ExclusionCancelled, ExclusionSettled, ExclusionCollected:
		default:
			return Exclusion{}, ErrInvalidProjectionEvent
		}
		if _, exists := seen[reason]; !exists {
			seen[reason] = struct{}{}
			canonical = append(canonical, reason)
		}
	}
	sort.Slice(canonical, func(i, j int) bool { return canonical[i] < canonical[j] })
	return Exclusion{SourceKind: sourceKind, SourceID: strings.TrimSpace(sourceID), Reasons: canonical, Label: strings.TrimSpace(label)}, nil
}

type AppliedEvent struct {
	Event         Event
	BalanceBefore money.Balance
	BalanceAfter  money.Balance
}

type Input struct {
	Policy             Policy
	AsOf               financialdate.Date
	HorizonEnd         financialdate.Date
	SelectedAccountIDs []string
	OpeningBalance     money.Balance
	Events             []Event
	Exclusions         []Exclusion
}

type Result struct {
	AsOf               financialdate.Date
	HorizonEnd         financialdate.Date
	Currency           money.Currency
	PolicyID           string
	PolicyVersion      int64
	Reserve            money.Money
	SelectedAccountIDs []string
	OpeningBalance     money.Balance
	Events             []AppliedEvent
	ClosingBalance     money.Balance
	MinimumBalance     money.Balance
	MinimumEventID     string
	MinimumDate        *financialdate.Date
	Exclusions         []Exclusion
}

func Calculate(input Input) (Result, error) {
	if _, err := RestorePolicy(
		input.Policy.ID(), input.Policy.OwnerID(), input.Policy.Currency(), input.Policy.HorizonDays(),
		input.Policy.Reserve(), input.Policy.FinancialTimezone(), input.Policy.AccountSelection(),
		input.Policy.InflowPolicy(), input.Policy.SameDayOrder(), input.Policy.Version(),
		input.Policy.CreatedAt(), input.Policy.UpdatedAt(),
	); err != nil {
		return Result{}, err
	}
	wantEnd, err := input.AsOf.AddDays(input.Policy.HorizonDays())
	if err != nil {
		return Result{}, err
	}
	if equal, err := wantEnd.Equal(input.HorizonEnd); err != nil || !equal {
		return Result{}, ErrInvalidHorizon
	}
	if input.OpeningBalance.Currency() != input.Policy.Currency() {
		return Result{}, money.ErrCurrencyMismatch
	}

	events := append([]Event(nil), input.Events...)
	seenEventIDs := make(map[string]struct{}, len(events))
	for _, event := range events {
		if _, exists := seenEventIDs[event.ID()]; exists {
			return Result{}, fmt.Errorf("%w: duplicate event ID %s", ErrInvalidProjectionEvent, event.ID())
		}
		seenEventIDs[event.ID()] = struct{}{}
		if event.Amount().Currency() != input.Policy.Currency() {
			return Result{}, money.ErrCurrencyMismatch
		}
		afterAsOf, err := event.FinancialDate().Compare(input.AsOf)
		if err != nil {
			return Result{}, err
		}
		beforeEnd, err := event.FinancialDate().Compare(input.HorizonEnd)
		if err != nil {
			return Result{}, err
		}
		if afterAsOf <= 0 || beforeEnd > 0 {
			return Result{}, fmt.Errorf("%w: %s", ErrEventOutsideHorizon, event.ID())
		}
	}
	sort.Slice(events, func(i, j int) bool {
		dateComparison, _ := events[i].FinancialDate().Compare(events[j].FinancialDate())
		if dateComparison != 0 {
			return dateComparison < 0
		}
		leftRank, rightRank := directionRank(events[i].Direction()), directionRank(events[j].Direction())
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		leftSourceRank, rightSourceRank := eventSourceRank(events[i].SourceKind()), eventSourceRank(events[j].SourceKind())
		if leftSourceRank != rightSourceRank {
			return leftSourceRank < rightSourceRank
		}
		return events[i].ID() < events[j].ID()
	})

	selectedIDs := append([]string(nil), input.SelectedAccountIDs...)
	sort.Strings(selectedIDs)
	exclusions := append([]Exclusion(nil), input.Exclusions...)
	sort.Slice(exclusions, func(i, j int) bool {
		if exclusions[i].SourceKind.String() != exclusions[j].SourceKind.String() {
			return exclusions[i].SourceKind.String() < exclusions[j].SourceKind.String()
		}
		return exclusions[i].SourceID < exclusions[j].SourceID
	})

	current := input.OpeningBalance
	minimum := current
	applied := make([]AppliedEvent, 0, len(events))
	minimumEventID := ""
	var minimumDate *financialdate.Date
	for _, event := range events {
		delta, err := money.NewBalance(event.Amount().MinorUnits(), event.Amount().Currency())
		if err != nil {
			return Result{}, err
		}
		before := current
		if event.Direction() == schedule.Inflow() {
			current, err = current.Add(delta)
		} else {
			current, err = current.Subtract(delta)
		}
		if err != nil {
			return Result{}, err
		}
		applied = append(applied, AppliedEvent{Event: event, BalanceBefore: before, BalanceAfter: current})
		comparison, err := current.Compare(minimum)
		if err != nil {
			return Result{}, err
		}
		if comparison < 0 {
			minimum = current
			minimumEventID = event.ID()
			date := event.FinancialDate()
			minimumDate = &date
		}
	}

	return Result{
		AsOf: input.AsOf, HorizonEnd: input.HorizonEnd, Currency: input.Policy.Currency(),
		PolicyID: input.Policy.ID(), PolicyVersion: input.Policy.Version(), Reserve: input.Policy.Reserve(),
		SelectedAccountIDs: selectedIDs, OpeningBalance: input.OpeningBalance, Events: applied,
		ClosingBalance: current, MinimumBalance: minimum, MinimumEventID: minimumEventID,
		MinimumDate: minimumDate, Exclusions: exclusions,
	}, nil
}

func directionRank(direction schedule.Direction) int {
	if direction == schedule.Outflow() {
		return 0
	}
	return 1
}
