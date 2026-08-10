package schedule

import (
	"strings"
	"time"

	"runway/backend/internal/domain/financialdate"
	"runway/backend/internal/domain/money"
)

type ScheduledCashFlow struct {
	id                      string
	ownerID                 string
	sourceKind              SourceKind
	sourceID                string
	amount                  money.Money
	direction               Direction
	date                    financialdate.Date
	status                  FlowStatus
	amountProvenance        AmountProvenance
	dateProvenance          DateProvenance
	inclusionEligibility    InclusionEligibility
	settlementTransactionID string
	createdAt               time.Time
	updatedAt               time.Time
}

func NewManualScheduledCashFlow(
	id, ownerID string,
	amount money.Money,
	direction Direction,
	date financialdate.Date,
	sourceKind SourceKind,
	amountProvenance AmountProvenance,
	dateProvenance DateProvenance,
	inclusion InclusionEligibility,
	now time.Time,
) (ScheduledCashFlow, error) {
	if !sourceKind.IsManual() || dateProvenance.IsScenarioAssumed() {
		return ScheduledCashFlow{}, ErrInvalidScheduledFlow
	}
	return RestoreScheduledCashFlow(
		id, ownerID, sourceKind, id, amount, direction, date, ScheduledFlow(), amountProvenance,
		dateProvenance, inclusion, "", now, now,
	)
}

func NewObligationOccurrence(id string, obligation Obligation, date financialdate.Date) (ScheduledCashFlow, error) {
	return RestoreScheduledCashFlow(
		id, obligation.OwnerID(), ObligationSource(), obligation.ID(), obligation.Amount(), Outflow(), date,
		ScheduledFlow(), ExactAmount(), ExactDate(), EligibleForPolicy(), "", obligation.CreatedAt(), obligation.CreatedAt(),
	)
}

func RestoreScheduledCashFlow(
	id, ownerID string,
	sourceKind SourceKind,
	sourceID string,
	amount money.Money,
	direction Direction,
	date financialdate.Date,
	status FlowStatus,
	amountProvenance AmountProvenance,
	dateProvenance DateProvenance,
	inclusion InclusionEligibility,
	settlementTransactionID string,
	createdAt, updatedAt time.Time,
) (ScheduledCashFlow, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(ownerID) == "" || strings.TrimSpace(sourceID) == "" || createdAt.IsZero() || updatedAt.IsZero() || updatedAt.Before(createdAt) {
		return ScheduledCashFlow{}, ErrInvalidScheduledFlow
	}
	if _, err := ParseSourceKind(sourceKind.String()); err != nil {
		return ScheduledCashFlow{}, err
	}
	if sourceKind.IsManual() && sourceID != id {
		return ScheduledCashFlow{}, ErrInvalidScheduledFlow
	}
	if _, err := money.New(amount.MinorUnits(), amount.Currency()); err != nil || amount.MinorUnits() == 0 {
		return ScheduledCashFlow{}, ErrInvalidScheduledFlow
	}
	if _, err := ParseDirection(direction.String()); err != nil {
		return ScheduledCashFlow{}, err
	}
	if _, err := financialdate.Parse(date.String()); err != nil {
		return ScheduledCashFlow{}, err
	}
	if _, err := ParseFlowStatus(status.String()); err != nil {
		return ScheduledCashFlow{}, err
	}
	if _, err := ParseAmountProvenance(amountProvenance.String()); err != nil {
		return ScheduledCashFlow{}, err
	}
	if _, err := ParseDateProvenance(dateProvenance.String()); err != nil {
		return ScheduledCashFlow{}, err
	}
	if _, err := ParseInclusionEligibility(inclusion.String()); err != nil {
		return ScheduledCashFlow{}, err
	}
	settlementTransactionID = strings.TrimSpace(settlementTransactionID)
	if status.IsSettled() != (settlementTransactionID != "") {
		return ScheduledCashFlow{}, ErrInvalidScheduledFlow
	}
	return ScheduledCashFlow{
		id: id, ownerID: ownerID, sourceKind: sourceKind, sourceID: sourceID,
		amount: amount, direction: direction, date: date, status: status,
		amountProvenance: amountProvenance, dateProvenance: dateProvenance,
		inclusionEligibility: inclusion, settlementTransactionID: settlementTransactionID,
		createdAt: createdAt.UTC(), updatedAt: updatedAt.UTC(),
	}, nil
}

func (f ScheduledCashFlow) Cancel(now time.Time) (ScheduledCashFlow, error) {
	if !f.status.IsScheduled() {
		return ScheduledCashFlow{}, ErrScheduledFlowFinal
	}
	if now.IsZero() || now.Before(f.updatedAt) {
		return ScheduledCashFlow{}, ErrInvalidScheduledFlow
	}
	f.status = CancelledFlow()
	f.updatedAt = now.UTC()
	return f, nil
}

func (f ScheduledCashFlow) ID() string                                 { return f.id }
func (f ScheduledCashFlow) OwnerID() string                            { return f.ownerID }
func (f ScheduledCashFlow) SourceKind() SourceKind                     { return f.sourceKind }
func (f ScheduledCashFlow) SourceID() string                           { return f.sourceID }
func (f ScheduledCashFlow) Amount() money.Money                        { return f.amount }
func (f ScheduledCashFlow) Direction() Direction                       { return f.direction }
func (f ScheduledCashFlow) FinancialDate() financialdate.Date          { return f.date }
func (f ScheduledCashFlow) Status() FlowStatus                         { return f.status }
func (f ScheduledCashFlow) AmountProvenance() AmountProvenance         { return f.amountProvenance }
func (f ScheduledCashFlow) DateProvenance() DateProvenance             { return f.dateProvenance }
func (f ScheduledCashFlow) InclusionEligibility() InclusionEligibility { return f.inclusionEligibility }
func (f ScheduledCashFlow) SettlementTransactionID() string            { return f.settlementTransactionID }
func (f ScheduledCashFlow) CreatedAt() time.Time                       { return f.createdAt }
func (f ScheduledCashFlow) UpdatedAt() time.Time                       { return f.updatedAt }
