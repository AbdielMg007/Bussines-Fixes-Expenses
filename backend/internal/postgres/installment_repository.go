package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"runway/backend/internal/card"
	"runway/backend/internal/domain/account"
	domain "runway/backend/internal/domain/card"
	"runway/backend/internal/domain/financialdate"
	"runway/backend/internal/domain/money"
	applicationledger "runway/backend/internal/ledger"
)

const installmentPlanSQL = `SELECT id,owner_id,account_id,description,purchase_transaction_id,original_principal_minor,currency,installment_count,first_cycle_id,status,schedule_version,created_at,updated_at FROM credit_card_installment_plans`
const installmentAllocationSQL = `SELECT id,owner_id,account_id,plan_id,cycle_id,installment_number,schedule_version,principal_minor,currency,status,created_at FROM credit_card_installment_allocations`
const installmentPaymentSQL = `SELECT id,owner_id,account_id,plan_id,allocation_id,amount_minor,currency,created_at FROM credit_card_installment_principal_payments`

func (r *CardRepository) CreateInstallmentPlan(ctx context.Context, owner string, input card.InstallmentPlanInput, id string, now time.Time) (domain.InstallmentPlan, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.InstallmentPlan{}, errors.New("begin installment plan")
	}
	defer tx.Rollback(ctx)

	financialAccount, err := scanAccount(tx.QueryRow(ctx, accountSelect+` WHERE owner_id=$1 AND id=$2 FOR UPDATE`, owner, input.AccountID))
	if err != nil {
		return domain.InstallmentPlan{}, err
	}
	if financialAccount.Type() != account.CreditCard() || financialAccount.Status().IsArchived() {
		return domain.InstallmentPlan{}, domain.ErrInvalidInstallmentPlan
	}
	if financialAccount.Currency() != input.Principal.Currency() {
		return domain.InstallmentPlan{}, money.ErrCurrencyMismatch
	}
	if err := validateInstallmentPurchaseTransaction(ctx, tx, owner, financialAccount.ID(), input.PurchaseTransactionID, input.Principal); err != nil {
		return domain.InstallmentPlan{}, err
	}

	resourceID, replay, err := claimMutation(ctx, tx, owner, "create_installment_plan", input.Mutation, id, now)
	if err != nil {
		return domain.InstallmentPlan{}, err
	}
	if replay {
		plan, err := scanInstallmentPlan(tx.QueryRow(ctx, installmentPlanSQL+` WHERE owner_id=$1 AND id=$2`, owner, resourceID))
		if err != nil {
			return domain.InstallmentPlan{}, err
		}
		return plan, tx.Commit(ctx)
	}

	entries, err := domain.BuildInstallmentSchedule(input.Principal, input.InstallmentCount, input.FirstCycleStart, input.FirstCycleEnd)
	if err != nil {
		return domain.InstallmentPlan{}, err
	}
	cycleIDs := make([]string, len(entries))
	for index, entry := range entries {
		cycleIDs[index], err = getOrCreateInstallmentCycle(ctx, tx, owner, financialAccount.ID(), id, entry.InstallmentNumber, entry.CycleStart, entry.CycleEnd, now)
		if err != nil {
			return domain.InstallmentPlan{}, err
		}
	}
	plan, err := domain.NewInstallmentPlan(id, owner, financialAccount.ID(), input.Description, input.PurchaseTransactionID, cycleIDs[0], input.Principal, input.InstallmentCount, 1, domain.InstallmentPlanActive, now, now)
	if err != nil {
		return domain.InstallmentPlan{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO credit_card_installment_plans(id,owner_id,account_id,description,purchase_transaction_id,original_principal_minor,currency,installment_count,first_cycle_id,status,schedule_version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, plan.ID, plan.OwnerID, plan.AccountID, plan.Description, plan.PurchaseTransactionID, plan.OriginalPrincipal.MinorUnits(), plan.OriginalPrincipal.Currency().Code(), plan.InstallmentCount, plan.FirstCycleID, plan.Status, plan.ScheduleVersion, plan.CreatedAt, plan.UpdatedAt); err != nil {
		return domain.InstallmentPlan{}, err
	}
	for index, entry := range entries {
		allocationID := fmt.Sprintf("%s-allocation-%03d", id, entry.InstallmentNumber)
		status, err := initialInstallmentAllocationStatus(ctx, tx, cycleIDs[index])
		if err != nil {
			return domain.InstallmentPlan{}, err
		}
		allocation, err := domain.NewInstallmentAllocation(allocationID, owner, financialAccount.ID(), id, cycleIDs[index], entry.InstallmentNumber, 1, entry.Principal, status, now)
		if err != nil {
			return domain.InstallmentPlan{}, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO credit_card_installment_allocations(id,owner_id,account_id,plan_id,cycle_id,installment_number,schedule_version,principal_minor,currency,status,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, allocation.ID, allocation.OwnerID, allocation.AccountID, allocation.PlanID, allocation.CycleID, allocation.InstallmentNumber, allocation.ScheduleVersion, allocation.Principal.MinorUnits(), allocation.Principal.Currency().Code(), allocation.Status, allocation.CreatedAt); err != nil {
			return domain.InstallmentPlan{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.InstallmentPlan{}, err
	}
	return plan, nil
}

func (r *CardRepository) GetInstallmentPlan(ctx context.Context, owner, planID string) (domain.InstallmentPlan, error) {
	return scanInstallmentPlan(r.pool.QueryRow(ctx, installmentPlanSQL+` WHERE owner_id=$1 AND id=$2`, owner, planID))
}

func (r *CardRepository) ListInstallmentPlans(ctx context.Context, owner, accountID string) ([]domain.InstallmentPlan, error) {
	rows, err := r.pool.Query(ctx, installmentPlanSQL+` WHERE owner_id=$1 AND account_id=$2 ORDER BY created_at,id`, owner, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	plans := []domain.InstallmentPlan{}
	for rows.Next() {
		plan, err := scanInstallmentPlan(rows)
		if err != nil {
			return nil, err
		}
		plans = append(plans, plan)
	}
	return plans, rows.Err()
}

func (r *CardRepository) ListInstallmentAllocations(ctx context.Context, owner, planID string) ([]domain.InstallmentAllocation, error) {
	rows, err := r.pool.Query(ctx, installmentAllocationSQL+` WHERE owner_id=$1 AND plan_id=$2 ORDER BY schedule_version,installment_number,id`, owner, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	allocations := []domain.InstallmentAllocation{}
	for rows.Next() {
		allocation, err := scanInstallmentAllocation(rows)
		if err != nil {
			return nil, err
		}
		allocations = append(allocations, allocation)
	}
	return allocations, rows.Err()
}

func (r *CardRepository) GetInstallmentPlanSummary(ctx context.Context, owner, planID string) (card.InstallmentPlanSummary, error) {
	plan, err := r.GetInstallmentPlan(ctx, owner, planID)
	if err != nil {
		return card.InstallmentPlanSummary{}, err
	}
	return installmentSummary(ctx, r.pool, plan)
}

func (r *CardRepository) RecordInstallmentPrincipalPayment(ctx context.Context, owner, allocationID string, amount money.Money, mutation applicationledger.MutationIdentity, id string, now time.Time) (card.InstallmentPrincipalPaymentResult, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return card.InstallmentPrincipalPaymentResult{}, errors.New("begin installment principal payment")
	}
	defer tx.Rollback(ctx)

	allocation, plan, err := loadAllocationAndPlanForUpdate(ctx, tx, owner, allocationID)
	if err != nil {
		return card.InstallmentPrincipalPaymentResult{}, err
	}
	if amount.Currency() != allocation.Principal.Currency() {
		return card.InstallmentPrincipalPaymentResult{}, money.ErrCurrencyMismatch
	}

	resourceID, replay, err := claimMutation(ctx, tx, owner, "record_installment_principal_payment", mutation, id, now)
	if err != nil {
		return card.InstallmentPrincipalPaymentResult{}, err
	}
	if replay {
		payment, err := scanInstallmentPrincipalPayment(tx.QueryRow(ctx, installmentPaymentSQL+` WHERE owner_id=$1 AND id=$2`, owner, resourceID))
		if err != nil {
			return card.InstallmentPrincipalPaymentResult{}, err
		}
		summary, err := installmentSummary(ctx, tx, plan)
		if err != nil {
			return card.InstallmentPrincipalPaymentResult{}, err
		}
		return card.InstallmentPrincipalPaymentResult{Payment: payment, Summary: summary}, tx.Commit(ctx)
	}
	if plan.Status == domain.InstallmentPlanCompleted {
		return card.InstallmentPrincipalPaymentResult{}, domain.ErrInstallmentPlanCompleted
	}
	if allocation.Status == domain.InstallmentPaid || allocation.Status == domain.InstallmentSuperseded {
		return card.InstallmentPrincipalPaymentResult{}, domain.ErrInstallmentOverpayment
	}

	paid, err := allocationPaidPrincipal(ctx, tx, allocation.ID, allocation.Principal.Currency())
	if err != nil {
		return card.InstallmentPrincipalPaymentResult{}, err
	}
	unpaid, err := allocation.Principal.Subtract(paid)
	if err != nil {
		return card.InstallmentPrincipalPaymentResult{}, err
	}
	if comparison, err := amount.Compare(unpaid); err != nil || comparison > 0 || amount.MinorUnits() <= 0 {
		return card.InstallmentPrincipalPaymentResult{}, domain.ErrInstallmentOverpayment
	}
	payment, err := domain.NewInstallmentPrincipalPayment(id, owner, allocation.AccountID, allocation.PlanID, allocation.ID, amount, now)
	if err != nil {
		return card.InstallmentPrincipalPaymentResult{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO credit_card_installment_principal_payments(id,owner_id,account_id,plan_id,allocation_id,amount_minor,currency,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, payment.ID, payment.OwnerID, payment.AccountID, payment.PlanID, payment.AllocationID, payment.Amount.MinorUnits(), payment.Amount.Currency().Code(), payment.CreatedAt); err != nil {
		return card.InstallmentPrincipalPaymentResult{}, err
	}
	updatedPaid, err := paid.Add(amount)
	if err != nil {
		return card.InstallmentPrincipalPaymentResult{}, err
	}
	if equal, err := updatedPaid.Equal(allocation.Principal); err != nil {
		return card.InstallmentPrincipalPaymentResult{}, err
	} else if equal {
		if _, err = tx.Exec(ctx, `UPDATE credit_card_installment_allocations SET status='paid' WHERE id=$1`, allocation.ID); err != nil {
			return card.InstallmentPrincipalPaymentResult{}, err
		}
	}
	summary, err := installmentSummary(ctx, tx, plan)
	if err != nil {
		return card.InstallmentPrincipalPaymentResult{}, err
	}
	if summary.OutstandingPrincipal.MinorUnits() == 0 && plan.Status == domain.InstallmentPlanActive {
		if _, err = tx.Exec(ctx, `UPDATE credit_card_installment_plans SET status='completed',updated_at=$1 WHERE id=$2`, now, plan.ID); err != nil {
			return card.InstallmentPrincipalPaymentResult{}, err
		}
		plan.Status = domain.InstallmentPlanCompleted
		plan.UpdatedAt = now.UTC()
		summary.Plan = plan
	}
	if err = tx.Commit(ctx); err != nil {
		return card.InstallmentPrincipalPaymentResult{}, err
	}
	return card.InstallmentPrincipalPaymentResult{Payment: payment, Summary: summary}, nil
}

func initialInstallmentAllocationStatus(ctx context.Context, tx pgx.Tx, cycleID string) (domain.InstallmentAllocationStatus, error) {
	var hasStatement bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM credit_card_statements WHERE cycle_id=$1 AND superseded_at IS NULL)`, cycleID).Scan(&hasStatement); err != nil {
		return "", err
	}
	if hasStatement {
		return domain.InstallmentStatementAllocated, nil
	}
	return domain.InstallmentPending, nil
}

func getOrCreateInstallmentCycle(ctx context.Context, tx pgx.Tx, owner, accountID, planID string, installmentNumber int, start, end financialdate.Date, now time.Time) (string, error) {
	var cycleID string
	err := tx.QueryRow(ctx, `SELECT id FROM credit_card_cycles WHERE owner_id=$1 AND account_id=$2 AND cycle_start=$3::date AND cycle_end=$4::date FOR UPDATE`, owner, accountID, start.String(), end.String()).Scan(&cycleID)
	if err == nil {
		return cycleID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	cycleID = fmt.Sprintf("%s-cycle-%03d", planID, installmentNumber)
	_, err = tx.Exec(ctx, `INSERT INTO credit_card_cycles(id,owner_id,account_id,cycle_start,cycle_end,created_at) VALUES($1,$2,$3,$4::date,$5::date,$6)`, cycleID, owner, accountID, start.String(), end.String(), now)
	return cycleID, err
}

func loadAllocationAndPlanForUpdate(ctx context.Context, tx pgx.Tx, owner, allocationID string) (domain.InstallmentAllocation, domain.InstallmentPlan, error) {
	allocation, err := scanInstallmentAllocation(tx.QueryRow(ctx, installmentAllocationSQL+` WHERE owner_id=$1 AND id=$2 FOR UPDATE`, owner, allocationID))
	if err != nil {
		return domain.InstallmentAllocation{}, domain.InstallmentPlan{}, err
	}
	plan, err := scanInstallmentPlan(tx.QueryRow(ctx, installmentPlanSQL+` WHERE owner_id=$1 AND id=$2 FOR UPDATE`, owner, allocation.PlanID))
	if err != nil {
		return domain.InstallmentAllocation{}, domain.InstallmentPlan{}, err
	}
	return allocation, plan, nil
}

type installmentScanner interface{ Scan(...any) error }

func scanInstallmentPlan(row installmentScanner) (domain.InstallmentPlan, error) {
	var id, ownerID, accountID, description, purchaseTransactionID, currency, firstCycleID, status string
	var original int64
	var installmentCount, version int
	var createdAt, updatedAt time.Time
	if err := row.Scan(&id, &ownerID, &accountID, &description, &purchaseTransactionID, &original, &currency, &installmentCount, &firstCycleID, &status, &version, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.InstallmentPlan{}, card.ErrNotFound
		}
		return domain.InstallmentPlan{}, err
	}
	parsedCurrency, err := money.ParseCurrency(currency)
	if err != nil {
		return domain.InstallmentPlan{}, err
	}
	principal, err := money.New(original, parsedCurrency)
	if err != nil {
		return domain.InstallmentPlan{}, err
	}
	return domain.NewInstallmentPlan(id, ownerID, accountID, description, purchaseTransactionID, firstCycleID, principal, installmentCount, version, domain.InstallmentPlanStatus(status), createdAt, updatedAt)
}

func validateInstallmentPurchaseTransaction(ctx context.Context, tx pgx.Tx, owner, accountID, transactionID string, principal money.Money) error {
	var amount int64
	var currency, effect string
	err := tx.QueryRow(ctx, `SELECT amount_minor,currency,effect FROM financial_transactions WHERE owner_id=$1 AND account_id=$2 AND id=$3`, owner, accountID, transactionID).Scan(&amount, &currency, &effect)
	if errors.Is(err, pgx.ErrNoRows) {
		return card.ErrNotFound
	}
	if err != nil {
		return err
	}
	if effect != "liability_charge" || currency != principal.Currency().Code() || amount != principal.MinorUnits() {
		return domain.ErrInvalidInstallmentPlan
	}
	return nil
}

func scanInstallmentAllocation(row installmentScanner) (domain.InstallmentAllocation, error) {
	var id, ownerID, accountID, planID, cycleID, currency, status string
	var number, version int
	var principal int64
	var createdAt time.Time
	if err := row.Scan(&id, &ownerID, &accountID, &planID, &cycleID, &number, &version, &principal, &currency, &status, &createdAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.InstallmentAllocation{}, card.ErrNotFound
		}
		return domain.InstallmentAllocation{}, err
	}
	parsedCurrency, err := money.ParseCurrency(currency)
	if err != nil {
		return domain.InstallmentAllocation{}, err
	}
	amount, err := money.New(principal, parsedCurrency)
	if err != nil {
		return domain.InstallmentAllocation{}, err
	}
	return domain.NewInstallmentAllocation(id, ownerID, accountID, planID, cycleID, number, version, amount, domain.InstallmentAllocationStatus(status), createdAt)
}

func scanInstallmentPrincipalPayment(row installmentScanner) (domain.InstallmentPrincipalPayment, error) {
	var id, ownerID, accountID, planID, allocationID, currency string
	var amount int64
	var createdAt time.Time
	if err := row.Scan(&id, &ownerID, &accountID, &planID, &allocationID, &amount, &currency, &createdAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.InstallmentPrincipalPayment{}, card.ErrNotFound
		}
		return domain.InstallmentPrincipalPayment{}, err
	}
	parsedCurrency, err := money.ParseCurrency(currency)
	if err != nil {
		return domain.InstallmentPrincipalPayment{}, err
	}
	principal, err := money.New(amount, parsedCurrency)
	if err != nil {
		return domain.InstallmentPrincipalPayment{}, err
	}
	return domain.NewInstallmentPrincipalPayment(id, ownerID, accountID, planID, allocationID, principal, createdAt)
}

func allocationPaidPrincipal(ctx context.Context, query interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, allocationID string, currency money.Currency) (money.Money, error) {
	var paid int64
	if err := query.QueryRow(ctx, `SELECT COALESCE(SUM(amount_minor),0) FROM credit_card_installment_principal_payments WHERE allocation_id=$1`, allocationID).Scan(&paid); err != nil {
		return money.Money{}, err
	}
	return money.New(paid, currency)
}

func installmentSummary(ctx context.Context, query interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, plan domain.InstallmentPlan) (card.InstallmentPlanSummary, error) {
	var paid int64
	if err := query.QueryRow(ctx, `SELECT COALESCE(SUM(amount_minor),0) FROM credit_card_installment_principal_payments WHERE owner_id=$1 AND plan_id=$2`, plan.OwnerID, plan.ID).Scan(&paid); err != nil {
		return card.InstallmentPlanSummary{}, err
	}
	paidPrincipal, err := money.New(paid, plan.OriginalPrincipal.Currency())
	if err != nil {
		return card.InstallmentPlanSummary{}, err
	}
	outstanding, err := plan.OriginalPrincipal.Subtract(paidPrincipal)
	if err != nil {
		return card.InstallmentPlanSummary{}, err
	}
	return card.InstallmentPlanSummary{Plan: plan, PaidPrincipal: paidPrincipal, OutstandingPrincipal: outstanding}, nil
}
