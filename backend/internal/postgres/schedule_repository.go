package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"runway/backend/internal/domain/financialdate"
	"runway/backend/internal/domain/money"
	domainschedule "runway/backend/internal/domain/schedule"
	applicationledger "runway/backend/internal/ledger"
)

type ScheduleRepository struct{ pool *pgxpool.Pool }

func NewScheduleRepository(pool *pgxpool.Pool) *ScheduleRepository {
	return &ScheduleRepository{pool: pool}
}

func (r *ScheduleRepository) CreateObligation(ctx context.Context, obligation domainschedule.Obligation, mutation applicationledger.MutationIdentity) (domainschedule.Obligation, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domainschedule.Obligation{}, errors.New("create obligation")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	resourceID, replay, err := claimMutation(ctx, tx, obligation.OwnerID(), "create_obligation", mutation, obligation.ID(), obligation.CreatedAt())
	if err != nil {
		return domainschedule.Obligation{}, err
	}
	if replay {
		result, err := scanObligation(tx.QueryRow(ctx, obligationSelect+` WHERE owner_id = $1 AND id = $2`, obligation.OwnerID(), resourceID))
		if err != nil {
			return domainschedule.Obligation{}, err
		}
		result, err = originalObligationResult(result)
		if err != nil {
			return domainschedule.Obligation{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return domainschedule.Obligation{}, errors.New("replay obligation")
		}
		return result, nil
	}
	endDate, hasEnd := obligation.EndDate()
	var end any
	if hasEnd {
		end = endDate.String()
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO obligations
			(id, owner_id, display_name, amount_minor, currency, direction, recurrence, start_date, end_date, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'outflow', $6, $7::date, $8::date, $9, $10, $11)`,
		obligation.ID(), obligation.OwnerID(), obligation.DisplayName(), obligation.Amount().MinorUnits(), obligation.Amount().Currency().Code(),
		obligation.Recurrence().String(), obligation.StartDate().String(), end, obligation.Status().String(), obligation.CreatedAt(), obligation.UpdatedAt(),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return domainschedule.Obligation{}, applicationledger.ErrConflict
		}
		return domainschedule.Obligation{}, errors.New("create obligation")
	}
	if err := tx.Commit(ctx); err != nil {
		return domainschedule.Obligation{}, errors.New("create obligation")
	}
	return obligation, nil
}

func (r *ScheduleRepository) GetObligation(ctx context.Context, ownerID, obligationID string) (domainschedule.Obligation, error) {
	return scanObligation(r.pool.QueryRow(ctx, obligationSelect+` WHERE owner_id = $1 AND id = $2`, ownerID, obligationID))
}

func (r *ScheduleRepository) ListObligations(ctx context.Context, ownerID string) ([]domainschedule.Obligation, error) {
	rows, err := r.pool.Query(ctx, obligationSelect+` WHERE owner_id = $1 ORDER BY created_at, id`, ownerID)
	if err != nil {
		return nil, errors.New("list obligations")
	}
	defer rows.Close()
	result := make([]domainschedule.Obligation, 0)
	for rows.Next() {
		value, err := scanObligation(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	if rows.Err() != nil {
		return nil, errors.New("list obligations")
	}
	return result, nil
}

func (r *ScheduleRepository) ArchiveObligation(
	ctx context.Context, ownerID, obligationID string, inactiveFrom financialdate.Date, now time.Time,
	mutation applicationledger.MutationIdentity,
) (domainschedule.Obligation, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domainschedule.Obligation{}, errors.New("archive obligation")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := scanObligation(tx.QueryRow(ctx, obligationSelect+` WHERE owner_id = $1 AND id = $2 FOR UPDATE`, ownerID, obligationID))
	if err != nil {
		return domainschedule.Obligation{}, err
	}
	_, replay, err := claimMutation(ctx, tx, ownerID, "archive_obligation", mutation, obligationID, now)
	if err != nil {
		return domainschedule.Obligation{}, err
	}
	if replay {
		if err := tx.Commit(ctx); err != nil {
			return domainschedule.Obligation{}, errors.New("replay obligation archive")
		}
		return current, nil
	}
	archived, err := current.Archive(inactiveFrom, now)
	if err != nil {
		return domainschedule.Obligation{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE obligations SET status = 'archived', inactive_from = $1::date, updated_at = $2 WHERE owner_id = $3 AND id = $4`, inactiveFrom.String(), archived.UpdatedAt(), ownerID, obligationID); err != nil {
		return domainschedule.Obligation{}, errors.New("archive obligation")
	}
	if err := tx.Commit(ctx); err != nil {
		return domainschedule.Obligation{}, errors.New("archive obligation")
	}
	return archived, nil
}

func (r *ScheduleRepository) CreateScheduledFlow(ctx context.Context, flow domainschedule.ScheduledCashFlow, mutation applicationledger.MutationIdentity) (domainschedule.ScheduledCashFlow, error) {
	if !flow.SourceKind().IsManual() {
		return domainschedule.ScheduledCashFlow{}, domainschedule.ErrInvalidScheduledFlow
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domainschedule.ScheduledCashFlow{}, errors.New("create scheduled flow")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	resourceID, replay, err := claimMutation(ctx, tx, flow.OwnerID(), "create_scheduled_flow", mutation, flow.ID(), flow.CreatedAt())
	if err != nil {
		return domainschedule.ScheduledCashFlow{}, err
	}
	if replay {
		result, err := scanScheduledFlow(tx.QueryRow(ctx, scheduledFlowSelect+` WHERE owner_id = $1 AND id = $2`, flow.OwnerID(), resourceID))
		if err != nil {
			return domainschedule.ScheduledCashFlow{}, err
		}
		result, err = originalScheduledFlowResult(result)
		if err != nil {
			return domainschedule.ScheduledCashFlow{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return domainschedule.ScheduledCashFlow{}, errors.New("replay scheduled flow")
		}
		return result, nil
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO scheduled_cash_flows
			(id, owner_id, source_kind, source_id, amount_minor, currency, direction, financial_date, status,
			 amount_provenance, date_provenance, inclusion_eligibility, settlement_transaction_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::date, $9, $10, $11, $12, NULL, $13, $14)`,
		flow.ID(), flow.OwnerID(), flow.SourceKind().String(), flow.SourceID(), flow.Amount().MinorUnits(), flow.Amount().Currency().Code(),
		flow.Direction().String(), flow.FinancialDate().String(), flow.Status().String(), flow.AmountProvenance().String(),
		flow.DateProvenance().String(), flow.InclusionEligibility().String(), flow.CreatedAt(), flow.UpdatedAt(),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return domainschedule.ScheduledCashFlow{}, applicationledger.ErrConflict
		}
		return domainschedule.ScheduledCashFlow{}, errors.New("create scheduled flow")
	}
	if err := tx.Commit(ctx); err != nil {
		return domainschedule.ScheduledCashFlow{}, errors.New("create scheduled flow")
	}
	return flow, nil
}

func (r *ScheduleRepository) GetScheduledFlow(ctx context.Context, ownerID, flowID string) (domainschedule.ScheduledCashFlow, error) {
	return scanScheduledFlow(r.pool.QueryRow(ctx, scheduledFlowSelect+` WHERE owner_id = $1 AND id = $2`, ownerID, flowID))
}

func (r *ScheduleRepository) ListScheduledFlows(ctx context.Context, ownerID string) ([]domainschedule.ScheduledCashFlow, error) {
	rows, err := r.pool.Query(ctx, scheduledFlowSelect+` WHERE owner_id = $1 ORDER BY financial_date, created_at, id`, ownerID)
	if err != nil {
		return nil, errors.New("list scheduled flows")
	}
	defer rows.Close()
	result := make([]domainschedule.ScheduledCashFlow, 0)
	for rows.Next() {
		value, err := scanScheduledFlow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	if rows.Err() != nil {
		return nil, errors.New("list scheduled flows")
	}
	return result, nil
}

func (r *ScheduleRepository) CancelScheduledFlow(ctx context.Context, ownerID, flowID string, now time.Time, mutation applicationledger.MutationIdentity) (domainschedule.ScheduledCashFlow, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domainschedule.ScheduledCashFlow{}, errors.New("cancel scheduled flow")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := scanScheduledFlow(tx.QueryRow(ctx, scheduledFlowSelect+` WHERE owner_id = $1 AND id = $2 FOR UPDATE`, ownerID, flowID))
	if err != nil {
		return domainschedule.ScheduledCashFlow{}, err
	}
	_, replay, err := claimMutation(ctx, tx, ownerID, "cancel_scheduled_flow", mutation, flowID, now)
	if err != nil {
		return domainschedule.ScheduledCashFlow{}, err
	}
	if replay {
		if err := tx.Commit(ctx); err != nil {
			return domainschedule.ScheduledCashFlow{}, errors.New("replay scheduled flow cancellation")
		}
		return current, nil
	}
	cancelled, err := current.Cancel(now)
	if err != nil {
		return domainschedule.ScheduledCashFlow{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE scheduled_cash_flows SET status = 'cancelled', updated_at = $1 WHERE owner_id = $2 AND id = $3`, cancelled.UpdatedAt(), ownerID, flowID); err != nil {
		return domainschedule.ScheduledCashFlow{}, errors.New("cancel scheduled flow")
	}
	if err := tx.Commit(ctx); err != nil {
		return domainschedule.ScheduledCashFlow{}, errors.New("cancel scheduled flow")
	}
	return cancelled, nil
}

func (r *ScheduleRepository) CreateReceivable(ctx context.Context, receivable domainschedule.Receivable, mutation applicationledger.MutationIdentity) (domainschedule.Receivable, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domainschedule.Receivable{}, errors.New("create receivable")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	resourceID, replay, err := claimMutation(ctx, tx, receivable.OwnerID(), "create_receivable", mutation, receivable.ID(), receivable.CreatedAt())
	if err != nil {
		return domainschedule.Receivable{}, err
	}
	if replay {
		result, err := scanReceivable(tx.QueryRow(ctx, receivableSelect+` WHERE owner_id = $1 AND id = $2`, receivable.OwnerID(), resourceID))
		if err != nil {
			return domainschedule.Receivable{}, err
		}
		result, err = originalReceivableResult(result)
		if err != nil {
			return domainschedule.Receivable{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return domainschedule.Receivable{}, errors.New("replay receivable")
		}
		return result, nil
	}
	expectedDate, hasDate := receivable.ExpectedDate()
	var expected any
	if hasDate {
		expected = expectedDate.String()
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO receivables
			(id, owner_id, display_name, original_amount_minor, collected_amount_minor, currency, status, expected_date, certainty, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::date, $9, $10, $11)`,
		receivable.ID(), receivable.OwnerID(), receivable.DisplayName(), receivable.OriginalAmount().MinorUnits(),
		receivable.CollectedAmount().MinorUnits(), receivable.OriginalAmount().Currency().Code(), receivable.Status().String(),
		expected, receivable.Certainty().String(), receivable.CreatedAt(), receivable.UpdatedAt(),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return domainschedule.Receivable{}, applicationledger.ErrConflict
		}
		return domainschedule.Receivable{}, errors.New("create receivable")
	}
	if err := tx.Commit(ctx); err != nil {
		return domainschedule.Receivable{}, errors.New("create receivable")
	}
	return receivable, nil
}

func (r *ScheduleRepository) GetReceivable(ctx context.Context, ownerID, receivableID string) (domainschedule.Receivable, error) {
	return scanReceivable(r.pool.QueryRow(ctx, receivableSelect+` WHERE owner_id = $1 AND id = $2`, ownerID, receivableID))
}

func (r *ScheduleRepository) ListReceivables(ctx context.Context, ownerID string) ([]domainschedule.Receivable, error) {
	rows, err := r.pool.Query(ctx, receivableSelect+` WHERE owner_id = $1 ORDER BY created_at, id`, ownerID)
	if err != nil {
		return nil, errors.New("list receivables")
	}
	defer rows.Close()
	result := make([]domainschedule.Receivable, 0)
	for rows.Next() {
		value, err := scanReceivable(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	if rows.Err() != nil {
		return nil, errors.New("list receivables")
	}
	return result, nil
}

func (r *ScheduleRepository) RecordReceivableCollection(
	ctx context.Context, ownerID, receivableID, collectionID string, amount money.Money,
	ledgerTransactionID string, now time.Time, mutation applicationledger.MutationIdentity,
) (domainschedule.ReceivableCollection, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domainschedule.ReceivableCollection{}, errors.New("record receivable collection")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := scanReceivable(tx.QueryRow(ctx, receivableSelect+` WHERE owner_id = $1 AND id = $2 FOR UPDATE`, ownerID, receivableID))
	if err != nil {
		return domainschedule.ReceivableCollection{}, err
	}
	resourceID, replay, err := claimMutation(ctx, tx, ownerID, "collect_receivable", mutation, collectionID, now)
	if err != nil {
		return domainschedule.ReceivableCollection{}, err
	}
	if replay {
		result, err := scanCollection(tx.QueryRow(ctx, collectionSelect+` WHERE owner_id = $1 AND id = $2`, ownerID, resourceID))
		if err != nil {
			return domainschedule.ReceivableCollection{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return domainschedule.ReceivableCollection{}, errors.New("replay receivable collection")
		}
		return result, nil
	}
	updated, collection, err := current.RecordCollection(collectionID, amount, ledgerTransactionID, now)
	if err != nil {
		return domainschedule.ReceivableCollection{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE receivables
		SET collected_amount_minor = $1, status = $2, updated_at = $3
		WHERE owner_id = $4 AND id = $5`,
		updated.CollectedAmount().MinorUnits(), updated.Status().String(), updated.UpdatedAt(), ownerID, receivableID,
	); err != nil {
		return domainschedule.ReceivableCollection{}, errors.New("update receivable collection")
	}
	var ledgerID any
	if ledgerTransactionID != "" {
		ledgerID = ledgerTransactionID
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO receivable_collections
			(id, owner_id, receivable_id, amount_minor, currency, collected_before_minor, collected_after_minor,
			 outstanding_after_minor, resulting_status, ledger_transaction_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		collection.ID(), collection.OwnerID(), collection.ReceivableID(), collection.Amount().MinorUnits(), collection.Amount().Currency().Code(),
		collection.CollectedBefore().MinorUnits(), collection.CollectedAfter().MinorUnits(), collection.OutstandingAfter().MinorUnits(),
		collection.ResultingStatus().String(), ledgerID, collection.CreatedAt(),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return domainschedule.ReceivableCollection{}, applicationledger.ErrConflict
		}
		return domainschedule.ReceivableCollection{}, errors.New("record receivable collection")
	}
	if err := tx.Commit(ctx); err != nil {
		return domainschedule.ReceivableCollection{}, errors.New("record receivable collection")
	}
	return collection, nil
}

func (r *ScheduleRepository) CancelReceivable(ctx context.Context, ownerID, receivableID string, now time.Time, mutation applicationledger.MutationIdentity) (domainschedule.Receivable, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domainschedule.Receivable{}, errors.New("cancel receivable")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := scanReceivable(tx.QueryRow(ctx, receivableSelect+` WHERE owner_id = $1 AND id = $2 FOR UPDATE`, ownerID, receivableID))
	if err != nil {
		return domainschedule.Receivable{}, err
	}
	_, replay, err := claimMutation(ctx, tx, ownerID, "cancel_receivable", mutation, receivableID, now)
	if err != nil {
		return domainschedule.Receivable{}, err
	}
	if replay {
		if err := tx.Commit(ctx); err != nil {
			return domainschedule.Receivable{}, errors.New("replay receivable cancellation")
		}
		return current, nil
	}
	cancelled, err := current.Cancel(now)
	if err != nil {
		return domainschedule.Receivable{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE receivables SET status = 'cancelled', updated_at = $1 WHERE owner_id = $2 AND id = $3`, cancelled.UpdatedAt(), ownerID, receivableID); err != nil {
		return domainschedule.Receivable{}, errors.New("cancel receivable")
	}
	if err := tx.Commit(ctx); err != nil {
		return domainschedule.Receivable{}, errors.New("cancel receivable")
	}
	return cancelled, nil
}

const obligationSelect = `
	SELECT id, owner_id, display_name, amount_minor, currency, recurrence,
	       to_char(start_date, 'YYYY-MM-DD'), COALESCE(to_char(end_date, 'YYYY-MM-DD'), ''),
	       COALESCE(to_char(inactive_from, 'YYYY-MM-DD'), ''), status, created_at, updated_at
	FROM obligations`

const scheduledFlowSelect = `
	SELECT id, owner_id, source_kind, source_id, amount_minor, currency, direction,
	       to_char(financial_date, 'YYYY-MM-DD'), status, amount_provenance, date_provenance,
	       inclusion_eligibility, COALESCE(settlement_transaction_id, ''), created_at, updated_at
	FROM scheduled_cash_flows`

const receivableSelect = `
	SELECT id, owner_id, display_name, original_amount_minor, collected_amount_minor, currency,
	       status, COALESCE(to_char(expected_date, 'YYYY-MM-DD'), ''), certainty, created_at, updated_at
	FROM receivables`

const collectionSelect = `
	SELECT id, owner_id, receivable_id, amount_minor, currency, collected_before_minor,
	       collected_after_minor, outstanding_after_minor, resulting_status,
	       COALESCE(ledger_transaction_id, ''), created_at
	FROM receivable_collections`

func scanObligation(row rowScanner) (domainschedule.Obligation, error) {
	var id, ownerID, name, currencyCode, recurrenceValue, startValue, endValue, inactiveFromValue, statusValue string
	var amountMinor int64
	var createdAt, updatedAt time.Time
	if err := row.Scan(&id, &ownerID, &name, &amountMinor, &currencyCode, &recurrenceValue, &startValue, &endValue, &inactiveFromValue, &statusValue, &createdAt, &updatedAt); err != nil {
		return domainschedule.Obligation{}, scheduleReadError(err, "obligation")
	}
	amount, recurrence, start, status, err := restoreObligationValues(amountMinor, currencyCode, recurrenceValue, startValue, statusValue)
	if err != nil {
		return domainschedule.Obligation{}, err
	}
	var end *financialdate.Date
	if endValue != "" {
		value, err := financialdate.Parse(endValue)
		if err != nil {
			return domainschedule.Obligation{}, err
		}
		end = &value
	}
	var inactiveFrom *financialdate.Date
	if inactiveFromValue != "" {
		value, err := financialdate.Parse(inactiveFromValue)
		if err != nil {
			return domainschedule.Obligation{}, err
		}
		inactiveFrom = &value
	}
	return domainschedule.RestoreObligation(id, ownerID, name, amount, recurrence, start, end, inactiveFrom, status, createdAt, updatedAt)
}

func restoreObligationValues(amountMinor int64, currencyCode, recurrenceValue, startValue, statusValue string) (money.Money, domainschedule.Recurrence, financialdate.Date, domainschedule.ObligationStatus, error) {
	currency, err := money.ParseCurrency(currencyCode)
	if err != nil {
		return money.Money{}, domainschedule.Recurrence{}, financialdate.Date{}, domainschedule.ObligationStatus{}, err
	}
	amount, err := money.New(amountMinor, currency)
	if err != nil {
		return money.Money{}, domainschedule.Recurrence{}, financialdate.Date{}, domainschedule.ObligationStatus{}, err
	}
	recurrence, err := domainschedule.ParseRecurrence(recurrenceValue)
	if err != nil {
		return money.Money{}, domainschedule.Recurrence{}, financialdate.Date{}, domainschedule.ObligationStatus{}, err
	}
	start, err := financialdate.Parse(startValue)
	if err != nil {
		return money.Money{}, domainschedule.Recurrence{}, financialdate.Date{}, domainschedule.ObligationStatus{}, err
	}
	status, err := domainschedule.ParseObligationStatus(statusValue)
	return amount, recurrence, start, status, err
}

func scanScheduledFlow(row rowScanner) (domainschedule.ScheduledCashFlow, error) {
	var id, ownerID, sourceKindValue, sourceID, currencyCode, directionValue, dateValue, statusValue string
	var amountProvenanceValue, dateProvenanceValue, inclusionValue, settlementID string
	var amountMinor int64
	var createdAt, updatedAt time.Time
	if err := row.Scan(&id, &ownerID, &sourceKindValue, &sourceID, &amountMinor, &currencyCode, &directionValue, &dateValue, &statusValue, &amountProvenanceValue, &dateProvenanceValue, &inclusionValue, &settlementID, &createdAt, &updatedAt); err != nil {
		return domainschedule.ScheduledCashFlow{}, scheduleReadError(err, "scheduled flow")
	}
	currency, err := money.ParseCurrency(currencyCode)
	if err != nil {
		return domainschedule.ScheduledCashFlow{}, err
	}
	amount, err := money.New(amountMinor, currency)
	if err != nil {
		return domainschedule.ScheduledCashFlow{}, err
	}
	sourceKind, err := domainschedule.ParseSourceKind(sourceKindValue)
	if err != nil {
		return domainschedule.ScheduledCashFlow{}, err
	}
	direction, err := domainschedule.ParseDirection(directionValue)
	if err != nil {
		return domainschedule.ScheduledCashFlow{}, err
	}
	date, err := financialdate.Parse(dateValue)
	if err != nil {
		return domainschedule.ScheduledCashFlow{}, err
	}
	status, err := domainschedule.ParseFlowStatus(statusValue)
	if err != nil {
		return domainschedule.ScheduledCashFlow{}, err
	}
	amountProvenance, err := domainschedule.ParseAmountProvenance(amountProvenanceValue)
	if err != nil {
		return domainschedule.ScheduledCashFlow{}, err
	}
	dateProvenance, err := domainschedule.ParseDateProvenance(dateProvenanceValue)
	if err != nil {
		return domainschedule.ScheduledCashFlow{}, err
	}
	inclusion, err := domainschedule.ParseInclusionEligibility(inclusionValue)
	if err != nil {
		return domainschedule.ScheduledCashFlow{}, err
	}
	return domainschedule.RestoreScheduledCashFlow(id, ownerID, sourceKind, sourceID, amount, direction, date, status, amountProvenance, dateProvenance, inclusion, settlementID, createdAt, updatedAt)
}

func scanReceivable(row rowScanner) (domainschedule.Receivable, error) {
	var id, ownerID, name, currencyCode, statusValue, expectedValue, certaintyValue string
	var originalMinor, collectedMinor int64
	var createdAt, updatedAt time.Time
	if err := row.Scan(&id, &ownerID, &name, &originalMinor, &collectedMinor, &currencyCode, &statusValue, &expectedValue, &certaintyValue, &createdAt, &updatedAt); err != nil {
		return domainschedule.Receivable{}, scheduleReadError(err, "receivable")
	}
	currency, err := money.ParseCurrency(currencyCode)
	if err != nil {
		return domainschedule.Receivable{}, err
	}
	original, err := money.New(originalMinor, currency)
	if err != nil {
		return domainschedule.Receivable{}, err
	}
	collected, err := money.New(collectedMinor, currency)
	if err != nil {
		return domainschedule.Receivable{}, err
	}
	status, err := domainschedule.ParseReceivableStatus(statusValue)
	if err != nil {
		return domainschedule.Receivable{}, err
	}
	certainty, err := domainschedule.ParseCertainty(certaintyValue)
	if err != nil {
		return domainschedule.Receivable{}, err
	}
	var expected *financialdate.Date
	if expectedValue != "" {
		value, err := financialdate.Parse(expectedValue)
		if err != nil {
			return domainschedule.Receivable{}, err
		}
		expected = &value
	}
	return domainschedule.RestoreReceivable(id, ownerID, name, original, collected, status, expected, certainty, createdAt, updatedAt)
}

func scanCollection(row rowScanner) (domainschedule.ReceivableCollection, error) {
	var id, ownerID, receivableID, currencyCode, statusValue, ledgerTransactionID string
	var amountMinor, beforeMinor, afterMinor, outstandingMinor int64
	var createdAt time.Time
	if err := row.Scan(&id, &ownerID, &receivableID, &amountMinor, &currencyCode, &beforeMinor, &afterMinor, &outstandingMinor, &statusValue, &ledgerTransactionID, &createdAt); err != nil {
		return domainschedule.ReceivableCollection{}, scheduleReadError(err, "receivable collection")
	}
	currency, err := money.ParseCurrency(currencyCode)
	if err != nil {
		return domainschedule.ReceivableCollection{}, err
	}
	amount, err := money.New(amountMinor, currency)
	if err != nil {
		return domainschedule.ReceivableCollection{}, err
	}
	before, err := money.New(beforeMinor, currency)
	if err != nil {
		return domainschedule.ReceivableCollection{}, err
	}
	after, err := money.New(afterMinor, currency)
	if err != nil {
		return domainschedule.ReceivableCollection{}, err
	}
	outstanding, err := money.New(outstandingMinor, currency)
	if err != nil {
		return domainschedule.ReceivableCollection{}, err
	}
	status, err := domainschedule.ParseReceivableStatus(statusValue)
	if err != nil {
		return domainschedule.ReceivableCollection{}, err
	}
	return domainschedule.RestoreReceivableCollection(id, ownerID, receivableID, amount, before, after, outstanding, status, ledgerTransactionID, createdAt)
}

func scheduleReadError(err error, resource string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return applicationledger.ErrNotFound
	}
	return fmt.Errorf("read %s", resource)
}

func originalObligationResult(current domainschedule.Obligation) (domainschedule.Obligation, error) {
	end, hasEnd := current.EndDate()
	var endPointer *financialdate.Date
	if hasEnd {
		endPointer = &end
	}
	return domainschedule.RestoreObligation(
		current.ID(), current.OwnerID(), current.DisplayName(), current.Amount(), current.Recurrence(),
		current.StartDate(), endPointer, nil, domainschedule.ActiveObligation(), current.CreatedAt(), current.CreatedAt(),
	)
}

func originalScheduledFlowResult(current domainschedule.ScheduledCashFlow) (domainschedule.ScheduledCashFlow, error) {
	return domainschedule.RestoreScheduledCashFlow(
		current.ID(), current.OwnerID(), current.SourceKind(), current.SourceID(), current.Amount(), current.Direction(),
		current.FinancialDate(), domainschedule.ScheduledFlow(), current.AmountProvenance(), current.DateProvenance(),
		current.InclusionEligibility(), "", current.CreatedAt(), current.CreatedAt(),
	)
}

func originalReceivableResult(current domainschedule.Receivable) (domainschedule.Receivable, error) {
	zero, err := money.Zero(current.OriginalAmount().Currency())
	if err != nil {
		return domainschedule.Receivable{}, err
	}
	expected, hasExpected := current.ExpectedDate()
	var expectedPointer *financialdate.Date
	if hasExpected {
		expectedPointer = &expected
	}
	return domainschedule.RestoreReceivable(
		current.ID(), current.OwnerID(), current.DisplayName(), current.OriginalAmount(), zero,
		domainschedule.OpenReceivable(), expectedPointer, current.Certainty(), current.CreatedAt(), current.CreatedAt(),
	)
}
