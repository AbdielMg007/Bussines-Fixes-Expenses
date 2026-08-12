package card

import (
	"fmt"
	"strings"
	"time"

	"runway/backend/internal/domain/financialdate"
	"runway/backend/internal/domain/money"
)

const MaxInstallmentCount = 600

type InstallmentPlanStatus string

const (
	InstallmentPlanActive    InstallmentPlanStatus = "active"
	InstallmentPlanCompleted InstallmentPlanStatus = "completed"
)

type InstallmentAllocationStatus string

const (
	InstallmentPending            InstallmentAllocationStatus = "pending"
	InstallmentStatementAllocated InstallmentAllocationStatus = "statement_allocated"
	InstallmentPaid               InstallmentAllocationStatus = "paid"
	InstallmentSuperseded         InstallmentAllocationStatus = "superseded"
)

type InstallmentPlan struct {
	ID, OwnerID, AccountID, Description, PurchaseTransactionID, FirstCycleID string
	OriginalPrincipal                                                        money.Money
	InstallmentCount, ScheduleVersion                                        int
	Status                                                                   InstallmentPlanStatus
	CreatedAt, UpdatedAt                                                     time.Time
}

func NewInstallmentPlan(id, ownerID, accountID, description, purchaseTransactionID, firstCycleID string, principal money.Money, installmentCount, scheduleVersion int, status InstallmentPlanStatus, createdAt, updatedAt time.Time) (InstallmentPlan, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(ownerID) == "" || strings.TrimSpace(accountID) == "" || strings.TrimSpace(description) == "" || strings.TrimSpace(purchaseTransactionID) == "" || strings.TrimSpace(firstCycleID) == "" || principal.MinorUnits() <= 0 || installmentCount < 1 || installmentCount > MaxInstallmentCount || scheduleVersion < 1 || createdAt.IsZero() || updatedAt.IsZero() || updatedAt.Before(createdAt) || (status != InstallmentPlanActive && status != InstallmentPlanCompleted) {
		return InstallmentPlan{}, ErrInvalidInstallmentPlan
	}
	if _, err := money.Zero(principal.Currency()); err != nil {
		return InstallmentPlan{}, ErrInvalidInstallmentPlan
	}
	return InstallmentPlan{ID: id, OwnerID: ownerID, AccountID: accountID, Description: strings.TrimSpace(description), PurchaseTransactionID: purchaseTransactionID, FirstCycleID: firstCycleID, OriginalPrincipal: principal, InstallmentCount: installmentCount, ScheduleVersion: scheduleVersion, Status: status, CreatedAt: createdAt.UTC(), UpdatedAt: updatedAt.UTC()}, nil
}

type InstallmentAllocation struct {
	ID, OwnerID, AccountID, PlanID, CycleID string
	InstallmentNumber, ScheduleVersion      int
	Principal                               money.Money
	Status                                  InstallmentAllocationStatus
	CreatedAt                               time.Time
}

func NewInstallmentAllocation(id, ownerID, accountID, planID, cycleID string, installmentNumber, scheduleVersion int, principal money.Money, status InstallmentAllocationStatus, createdAt time.Time) (InstallmentAllocation, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(ownerID) == "" || strings.TrimSpace(accountID) == "" || strings.TrimSpace(planID) == "" || strings.TrimSpace(cycleID) == "" || installmentNumber < 1 || scheduleVersion < 1 || principal.MinorUnits() <= 0 || createdAt.IsZero() || (status != InstallmentPending && status != InstallmentStatementAllocated && status != InstallmentPaid && status != InstallmentSuperseded) {
		return InstallmentAllocation{}, ErrInvalidInstallmentAllocation
	}
	if _, err := money.Zero(principal.Currency()); err != nil {
		return InstallmentAllocation{}, ErrInvalidInstallmentAllocation
	}
	return InstallmentAllocation{ID: id, OwnerID: ownerID, AccountID: accountID, PlanID: planID, CycleID: cycleID, InstallmentNumber: installmentNumber, ScheduleVersion: scheduleVersion, Principal: principal, Status: status, CreatedAt: createdAt.UTC()}, nil
}

type InstallmentPrincipalPayment struct {
	ID, OwnerID, AccountID, PlanID, AllocationID string
	Amount                                       money.Money
	CreatedAt                                    time.Time
}

func NewInstallmentPrincipalPayment(id, ownerID, accountID, planID, allocationID string, amount money.Money, createdAt time.Time) (InstallmentPrincipalPayment, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(ownerID) == "" || strings.TrimSpace(accountID) == "" || strings.TrimSpace(planID) == "" || strings.TrimSpace(allocationID) == "" || amount.MinorUnits() <= 0 || createdAt.IsZero() {
		return InstallmentPrincipalPayment{}, ErrInvalidInstallmentAllocation
	}
	if _, err := money.Zero(amount.Currency()); err != nil {
		return InstallmentPrincipalPayment{}, ErrInvalidInstallmentAllocation
	}
	return InstallmentPrincipalPayment{ID: id, OwnerID: ownerID, AccountID: accountID, PlanID: planID, AllocationID: allocationID, Amount: amount, CreatedAt: createdAt.UTC()}, nil
}

type InstallmentScheduleEntry struct {
	InstallmentNumber    int
	Principal            money.Money
	CycleStart, CycleEnd financialdate.Date
}

// BuildInstallmentSchedule allocates quotient principal to every installment
// and assigns the one-minor-unit remainder to the earliest installments.
func BuildInstallmentSchedule(principal money.Money, installmentCount int, firstStart, firstEnd financialdate.Date) ([]InstallmentScheduleEntry, error) {
	if principal.MinorUnits() <= 0 || installmentCount < 1 || installmentCount > MaxInstallmentCount || principal.MinorUnits() < int64(installmentCount) {
		return nil, ErrInvalidInstallmentPlan
	}
	if comparison, err := firstStart.Compare(firstEnd); err != nil || comparison > 0 {
		return nil, ErrInvalidInstallmentPlan
	}
	if _, err := money.Zero(principal.Currency()); err != nil {
		return nil, ErrInvalidInstallmentPlan
	}
	base := principal.MinorUnits() / int64(installmentCount)
	remainder := principal.MinorUnits() % int64(installmentCount)
	entries := make([]InstallmentScheduleEntry, 0, installmentCount)
	for index := 0; index < installmentCount; index++ {
		start, err := addCalendarMonths(firstStart, index)
		if err != nil {
			return nil, err
		}
		end, err := addCalendarMonths(firstEnd, index)
		if err != nil {
			return nil, err
		}
		if comparison, err := start.Compare(end); err != nil || comparison > 0 {
			return nil, ErrInvalidInstallmentPlan
		}
		minor := base
		if int64(index) < remainder {
			minor++
		}
		allocation, err := money.New(minor, principal.Currency())
		if err != nil {
			return nil, err
		}
		entries = append(entries, InstallmentScheduleEntry{InstallmentNumber: index + 1, Principal: allocation, CycleStart: start, CycleEnd: end})
	}
	return entries, nil
}

func addCalendarMonths(date financialdate.Date, months int) (financialdate.Date, error) {
	if months < 0 {
		return financialdate.Date{}, ErrInvalidInstallmentPlan
	}
	year := date.Year() + (int(date.Month())-1+months)/12
	month := time.Month((int(date.Month())-1+months)%12 + 1)
	if year > 9999 {
		return financialdate.Date{}, fmt.Errorf("%w: installment schedule exceeds supported financial date range", ErrInvalidInstallmentPlan)
	}
	day := date.Day()
	for {
		candidate, err := financialdate.New(year, month, day)
		if err == nil {
			return candidate, nil
		}
		day--
		if day == 0 {
			return financialdate.Date{}, ErrInvalidInstallmentPlan
		}
	}
}
