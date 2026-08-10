package schedule

import (
	"strings"
	"time"

	"runway/backend/internal/domain/financialdate"
	"runway/backend/internal/domain/money"
)

const MaxDisplayNameLength = 120

type Obligation struct {
	id           string
	ownerID      string
	name         string
	amount       money.Money
	recurrence   Recurrence
	startDate    financialdate.Date
	endDate      *financialdate.Date
	inactiveFrom *financialdate.Date
	status       ObligationStatus
	createdAt    time.Time
	updatedAt    time.Time
}

func NewObligation(
	id, ownerID, displayName string,
	amount money.Money,
	recurrence Recurrence,
	startDate financialdate.Date,
	endDate *financialdate.Date,
	now time.Time,
) (Obligation, error) {
	return RestoreObligation(id, ownerID, displayName, amount, recurrence, startDate, endDate, nil, ActiveObligation(), now, now)
}

func RestoreObligation(
	id, ownerID, displayName string,
	amount money.Money,
	recurrence Recurrence,
	startDate financialdate.Date,
	endDate *financialdate.Date,
	inactiveFrom *financialdate.Date,
	status ObligationStatus,
	createdAt, updatedAt time.Time,
) (Obligation, error) {
	name := strings.TrimSpace(displayName)
	if strings.TrimSpace(id) == "" || strings.TrimSpace(ownerID) == "" || createdAt.IsZero() || updatedAt.IsZero() || updatedAt.Before(createdAt) {
		return Obligation{}, ErrInvalidObligation
	}
	if name == "" || len([]rune(name)) > MaxDisplayNameLength {
		return Obligation{}, ErrInvalidName
	}
	if _, err := money.New(amount.MinorUnits(), amount.Currency()); err != nil || amount.MinorUnits() == 0 {
		return Obligation{}, ErrInvalidObligation
	}
	if _, err := ParseRecurrence(recurrence.String()); err != nil {
		return Obligation{}, err
	}
	if _, err := financialdate.Parse(startDate.String()); err != nil {
		return Obligation{}, err
	}
	if endDate != nil {
		if _, err := financialdate.Parse(endDate.String()); err != nil {
			return Obligation{}, err
		}
		comparison, _ := endDate.Compare(startDate)
		if comparison < 0 {
			return Obligation{}, ErrInvalidDateRange
		}
	}
	if _, err := ParseObligationStatus(status.String()); err != nil {
		return Obligation{}, err
	}
	if status.IsActive() && inactiveFrom != nil || status.IsArchived() && inactiveFrom == nil {
		return Obligation{}, ErrInvalidObligation
	}
	if inactiveFrom != nil {
		if _, err := financialdate.Parse(inactiveFrom.String()); err != nil {
			return Obligation{}, err
		}
	}
	if recurrence == oneTimeRecurrence && endDate != nil {
		comparison, _ := endDate.Compare(startDate)
		if comparison < 0 {
			return Obligation{}, ErrInvalidDateRange
		}
	}

	var copiedEnd *financialdate.Date
	if endDate != nil {
		value := *endDate
		copiedEnd = &value
	}
	var copiedInactiveFrom *financialdate.Date
	if inactiveFrom != nil {
		value := *inactiveFrom
		copiedInactiveFrom = &value
	}
	return Obligation{
		id: id, ownerID: ownerID, name: name, amount: amount, recurrence: recurrence,
		startDate: startDate, endDate: copiedEnd, inactiveFrom: copiedInactiveFrom, status: status,
		createdAt: createdAt.UTC(), updatedAt: updatedAt.UTC(),
	}, nil
}

func (o Obligation) Archive(inactiveFrom financialdate.Date, now time.Time) (Obligation, error) {
	if o.status.IsArchived() {
		return Obligation{}, ErrObligationArchived
	}
	if now.IsZero() || now.Before(o.updatedAt) {
		return Obligation{}, ErrInvalidObligation
	}
	if _, err := financialdate.Parse(inactiveFrom.String()); err != nil {
		return Obligation{}, err
	}
	o.inactiveFrom = &inactiveFrom
	o.status = ArchivedObligation()
	o.updatedAt = now.UTC()
	return o, nil
}

func (o Obligation) ID() string                    { return o.id }
func (o Obligation) OwnerID() string               { return o.ownerID }
func (o Obligation) DisplayName() string           { return o.name }
func (o Obligation) Amount() money.Money           { return o.amount }
func (o Obligation) Direction() Direction          { return Outflow() }
func (o Obligation) Recurrence() Recurrence        { return o.recurrence }
func (o Obligation) StartDate() financialdate.Date { return o.startDate }
func (o Obligation) Status() ObligationStatus      { return o.status }
func (o Obligation) CreatedAt() time.Time          { return o.createdAt }
func (o Obligation) UpdatedAt() time.Time          { return o.updatedAt }
func (o Obligation) EndDate() (financialdate.Date, bool) {
	if o.endDate == nil {
		return financialdate.Date{}, false
	}
	return *o.endDate, true
}

func (o Obligation) InactiveFrom() (financialdate.Date, bool) {
	if o.inactiveFrom == nil {
		return financialdate.Date{}, false
	}
	return *o.inactiveFrom, true
}
