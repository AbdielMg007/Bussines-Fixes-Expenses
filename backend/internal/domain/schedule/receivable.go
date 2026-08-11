package schedule

import (
	"strings"
	"time"

	"runway/backend/internal/domain/financialdate"
	"runway/backend/internal/domain/money"
)

type Receivable struct {
	id               string
	ownerID          string
	name             string
	originalAmount   money.Money
	collectedAmount  money.Money
	status           ReceivableStatus
	expectedDate     *financialdate.Date
	certainty        Certainty
	amountProvenance AmountProvenance
	dateProvenance   *DateProvenance
	createdAt        time.Time
	updatedAt        time.Time
}

func NewReceivable(
	id, ownerID, displayName string,
	originalAmount money.Money,
	expectedDate *financialdate.Date,
	certainty Certainty,
	amountProvenance AmountProvenance,
	dateProvenance *DateProvenance,
	now time.Time,
) (Receivable, error) {
	zero, err := money.Zero(originalAmount.Currency())
	if err != nil {
		return Receivable{}, err
	}
	return RestoreReceivable(
		id, ownerID, displayName, originalAmount, zero, OpenReceivable(), expectedDate,
		certainty, amountProvenance, dateProvenance, now, now,
	)
}

func RestoreReceivable(
	id, ownerID, displayName string,
	originalAmount, collectedAmount money.Money,
	status ReceivableStatus,
	expectedDate *financialdate.Date,
	certainty Certainty,
	amountProvenance AmountProvenance,
	dateProvenance *DateProvenance,
	createdAt, updatedAt time.Time,
) (Receivable, error) {
	name := strings.TrimSpace(displayName)
	if strings.TrimSpace(id) == "" || strings.TrimSpace(ownerID) == "" || createdAt.IsZero() || updatedAt.IsZero() || updatedAt.Before(createdAt) {
		return Receivable{}, ErrInvalidReceivable
	}
	if name == "" || len([]rune(name)) > MaxDisplayNameLength || originalAmount.MinorUnits() == 0 {
		return Receivable{}, ErrInvalidReceivable
	}
	comparison, err := collectedAmount.Compare(originalAmount)
	if err != nil || comparison > 0 {
		return Receivable{}, ErrInvalidReceivable
	}
	if _, err := ParseReceivableStatus(status.String()); err != nil {
		return Receivable{}, err
	}
	if _, err := ParseCertainty(certainty.String()); err != nil {
		return Receivable{}, err
	}
	if _, err := ParseAmountProvenance(amountProvenance.String()); err != nil {
		return Receivable{}, err
	}
	if (expectedDate == nil) != (dateProvenance == nil) {
		return Receivable{}, ErrInvalidProvenance
	}
	if expectedDate != nil {
		if _, err := financialdate.Parse(expectedDate.String()); err != nil {
			return Receivable{}, err
		}
		if _, err := ParseDateProvenance(dateProvenance.String()); err != nil || dateProvenance.IsScenarioAssumed() {
			return Receivable{}, ErrInvalidProvenance
		}
	}
	if status != CancelledReceivable() {
		switch {
		case collectedAmount.MinorUnits() == 0 && status != OpenReceivable():
			return Receivable{}, ErrInvalidReceivableStatus
		case collectedAmount.MinorUnits() > 0 && comparison < 0 && status != PartialReceivable():
			return Receivable{}, ErrInvalidReceivableStatus
		case comparison == 0 && status != CollectedReceivable():
			return Receivable{}, ErrInvalidReceivableStatus
		}
	} else if comparison == 0 {
		return Receivable{}, ErrInvalidReceivableStatus
	}

	var copiedDate *financialdate.Date
	var copiedDateProvenance *DateProvenance
	if expectedDate != nil {
		value := *expectedDate
		copiedDate = &value
		provenance := *dateProvenance
		copiedDateProvenance = &provenance
	}
	return Receivable{
		id: id, ownerID: ownerID, name: name, originalAmount: originalAmount,
		collectedAmount: collectedAmount, status: status, expectedDate: copiedDate,
		certainty: certainty, amountProvenance: amountProvenance, dateProvenance: copiedDateProvenance,
		createdAt: createdAt.UTC(), updatedAt: updatedAt.UTC(),
	}, nil
}

func (r Receivable) RecordCollection(id string, amount money.Money, ledgerTransactionID string, now time.Time) (Receivable, ReceivableCollection, error) {
	if r.status.IsFinal() {
		return Receivable{}, ReceivableCollection{}, ErrReceivableFinal
	}
	if amount.MinorUnits() == 0 {
		return Receivable{}, ReceivableCollection{}, ErrInvalidCollection
	}
	outstanding, err := r.OutstandingAmount()
	if err != nil {
		return Receivable{}, ReceivableCollection{}, err
	}
	comparison, err := amount.Compare(outstanding)
	if err != nil {
		return Receivable{}, ReceivableCollection{}, err
	}
	if comparison > 0 {
		return Receivable{}, ReceivableCollection{}, ErrCollectionExceedsAmount
	}
	if now.IsZero() || now.Before(r.updatedAt) {
		return Receivable{}, ReceivableCollection{}, ErrInvalidCollection
	}
	before := r.collectedAmount
	after, err := before.Add(amount)
	if err != nil {
		return Receivable{}, ReceivableCollection{}, err
	}
	outstandingAfter, err := r.originalAmount.Subtract(after)
	if err != nil {
		return Receivable{}, ReceivableCollection{}, err
	}
	resultingStatus := PartialReceivable()
	if outstandingAfter.MinorUnits() == 0 {
		resultingStatus = CollectedReceivable()
	}
	collection, err := RestoreReceivableCollection(
		id, r.ownerID, r.id, amount, before, after, outstandingAfter, resultingStatus,
		ledgerTransactionID, now,
	)
	if err != nil {
		return Receivable{}, ReceivableCollection{}, err
	}
	r.collectedAmount = after
	r.status = resultingStatus
	r.updatedAt = now.UTC()
	return r, collection, nil
}

func (r Receivable) Cancel(now time.Time) (Receivable, error) {
	if r.status.IsFinal() {
		return Receivable{}, ErrReceivableFinal
	}
	if now.IsZero() || now.Before(r.updatedAt) {
		return Receivable{}, ErrInvalidReceivable
	}
	r.status = CancelledReceivable()
	r.updatedAt = now.UTC()
	return r, nil
}

func (r Receivable) OutstandingAmount() (money.Money, error) {
	return r.originalAmount.Subtract(r.collectedAmount)
}

func (r Receivable) ID() string                         { return r.id }
func (r Receivable) OwnerID() string                    { return r.ownerID }
func (r Receivable) DisplayName() string                { return r.name }
func (r Receivable) OriginalAmount() money.Money        { return r.originalAmount }
func (r Receivable) CollectedAmount() money.Money       { return r.collectedAmount }
func (r Receivable) Status() ReceivableStatus           { return r.status }
func (r Receivable) Certainty() Certainty               { return r.certainty }
func (r Receivable) AmountProvenance() AmountProvenance { return r.amountProvenance }
func (r Receivable) CreatedAt() time.Time               { return r.createdAt }
func (r Receivable) UpdatedAt() time.Time               { return r.updatedAt }
func (r Receivable) ExpectedDate() (financialdate.Date, bool) {
	if r.expectedDate == nil {
		return financialdate.Date{}, false
	}
	return *r.expectedDate, true
}

func (r Receivable) DateProvenance() (DateProvenance, bool) {
	if r.dateProvenance == nil {
		return DateProvenance{}, false
	}
	return *r.dateProvenance, true
}

type ReceivableCollection struct {
	id                  string
	ownerID             string
	receivableID        string
	amount              money.Money
	collectedBefore     money.Money
	collectedAfter      money.Money
	outstandingAfter    money.Money
	resultingStatus     ReceivableStatus
	ledgerTransactionID string
	createdAt           time.Time
}

func RestoreReceivableCollection(
	id, ownerID, receivableID string,
	amount, collectedBefore, collectedAfter, outstandingAfter money.Money,
	resultingStatus ReceivableStatus,
	ledgerTransactionID string,
	createdAt time.Time,
) (ReceivableCollection, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(ownerID) == "" || strings.TrimSpace(receivableID) == "" || createdAt.IsZero() || amount.MinorUnits() == 0 {
		return ReceivableCollection{}, ErrInvalidCollection
	}
	wantAfter, err := collectedBefore.Add(amount)
	if err != nil {
		return ReceivableCollection{}, err
	}
	equal, err := wantAfter.Equal(collectedAfter)
	if err != nil || !equal {
		return ReceivableCollection{}, ErrInvalidCollection
	}
	if _, err := collectedAfter.Add(outstandingAfter); err != nil {
		return ReceivableCollection{}, err
	}
	if outstandingAfter.MinorUnits() == 0 {
		if resultingStatus != CollectedReceivable() {
			return ReceivableCollection{}, ErrInvalidCollection
		}
	} else if resultingStatus != PartialReceivable() {
		return ReceivableCollection{}, ErrInvalidCollection
	}
	return ReceivableCollection{
		id: id, ownerID: ownerID, receivableID: receivableID, amount: amount,
		collectedBefore: collectedBefore, collectedAfter: collectedAfter,
		outstandingAfter: outstandingAfter, resultingStatus: resultingStatus,
		ledgerTransactionID: strings.TrimSpace(ledgerTransactionID), createdAt: createdAt.UTC(),
	}, nil
}

func (c ReceivableCollection) ID() string                        { return c.id }
func (c ReceivableCollection) OwnerID() string                   { return c.ownerID }
func (c ReceivableCollection) ReceivableID() string              { return c.receivableID }
func (c ReceivableCollection) Amount() money.Money               { return c.amount }
func (c ReceivableCollection) CollectedBefore() money.Money      { return c.collectedBefore }
func (c ReceivableCollection) CollectedAfter() money.Money       { return c.collectedAfter }
func (c ReceivableCollection) OutstandingAfter() money.Money     { return c.outstandingAfter }
func (c ReceivableCollection) ResultingStatus() ReceivableStatus { return c.resultingStatus }
func (c ReceivableCollection) LedgerTransactionID() string       { return c.ledgerTransactionID }
func (c ReceivableCollection) CreatedAt() time.Time              { return c.createdAt }
