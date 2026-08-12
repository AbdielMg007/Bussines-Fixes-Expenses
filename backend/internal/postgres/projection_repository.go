package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"runway/backend/internal/domain/account"
	"runway/backend/internal/domain/financialdate"
	"runway/backend/internal/domain/money"
	domainprojection "runway/backend/internal/domain/projection"
	domainschedule "runway/backend/internal/domain/schedule"
	applicationledger "runway/backend/internal/ledger"
	applicationprojection "runway/backend/internal/projection"
)

type ProjectionRepository struct {
	pool            *pgxpool.Pool
	afterPolicyRead func()
}

func NewProjectionRepository(pool *pgxpool.Pool) *ProjectionRepository {
	return &ProjectionRepository{pool: pool}
}

func (r *ProjectionRepository) GetPolicy(ctx context.Context, ownerID string) (domainprojection.Policy, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return domainprojection.Policy{}, errors.New("get projection policy")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	policy, err := loadPolicy(ctx, tx, ownerID)
	if err != nil {
		return domainprojection.Policy{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domainprojection.Policy{}, errors.New("get projection policy")
	}
	return policy, nil
}

func (r *ProjectionRepository) ReplacePolicy(ctx context.Context, requested domainprojection.Policy) (domainprojection.Policy, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domainprojection.Policy{}, errors.New("replace projection policy")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var lockedOwnerID string
	if err := tx.QueryRow(ctx, `SELECT id FROM owners WHERE id = $1 FOR UPDATE`, requested.OwnerID()).Scan(&lockedOwnerID); errors.Is(err, pgx.ErrNoRows) {
		return domainprojection.Policy{}, applicationledger.ErrNotFound
	} else if err != nil {
		return domainprojection.Policy{}, errors.New("replace projection policy")
	}

	selection := requested.AccountSelection()
	if selection.Mode() == domainprojection.ExplicitSelection() {
		ids := selection.AccountIDs()
		if len(ids) > 0 {
			rows, err := tx.Query(ctx, accountSelect+`
				WHERE owner_id = $1 AND id = ANY($2::text[])
				ORDER BY id FOR SHARE`, requested.OwnerID(), ids)
			if err != nil {
				return domainprojection.Policy{}, errors.New("validate projection accounts")
			}
			count := 0
			for rows.Next() {
				financialAccount, err := scanAccount(rows)
				if err != nil {
					rows.Close()
					return domainprojection.Policy{}, err
				}
				if err := applicationprojection.ValidateSelectedAccount(financialAccount, requested.OwnerID(), requested.Currency()); err != nil {
					rows.Close()
					return domainprojection.Policy{}, err
				}
				count++
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				return domainprojection.Policy{}, errors.New("validate projection accounts")
			}
			rows.Close()
			if count != len(ids) {
				return domainprojection.Policy{}, applicationledger.ErrNotFound
			}
		}
	}
	existing, err := loadPolicy(ctx, tx, requested.OwnerID())
	if err == nil && samePolicyConfiguration(existing, requested) {
		if err := tx.Commit(ctx); err != nil {
			return domainprojection.Policy{}, errors.New("replace projection policy")
		}
		return existing, nil
	}
	if err != nil && !errors.Is(err, applicationprojection.ErrPolicyNotFound) {
		return domainprojection.Policy{}, err
	}

	if _, err := tx.Exec(ctx, `DELETE FROM projection_policy_accounts WHERE owner_id = $1`, requested.OwnerID()); err != nil {
		return domainprojection.Policy{}, errors.New("replace projection policy")
	}
	var id, ownerID, currencyCode, timezone, selectionMode, inflowPolicy, sameDayOrder string
	var horizonDays int
	var reserveMinor, version int64
	var createdAt, updatedAt time.Time
	err = tx.QueryRow(ctx, `
		INSERT INTO projection_policies
			(id, owner_id, currency, horizon_days, reserve_minor, financial_timezone,
			 account_selection_mode, inflow_policy, same_day_order, version, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 1, $10, $10)
		ON CONFLICT (owner_id) DO UPDATE SET
			currency = EXCLUDED.currency,
			horizon_days = EXCLUDED.horizon_days,
			reserve_minor = EXCLUDED.reserve_minor,
			financial_timezone = EXCLUDED.financial_timezone,
			account_selection_mode = EXCLUDED.account_selection_mode,
			inflow_policy = EXCLUDED.inflow_policy,
			same_day_order = EXCLUDED.same_day_order,
			version = projection_policies.version + 1,
			updated_at = EXCLUDED.updated_at
		RETURNING id, owner_id, currency, horizon_days, reserve_minor, financial_timezone,
		          account_selection_mode, inflow_policy, same_day_order, version, created_at, updated_at`,
		requested.ID(), requested.OwnerID(), requested.Currency().Code(), requested.HorizonDays(), requested.Reserve().MinorUnits(),
		requested.FinancialTimezone(), selection.Mode().String(), requested.InflowPolicy().String(),
		requested.SameDayOrder().String(), requested.UpdatedAt(),
	).Scan(&id, &ownerID, &currencyCode, &horizonDays, &reserveMinor, &timezone, &selectionMode, &inflowPolicy, &sameDayOrder, &version, &createdAt, &updatedAt)
	if err != nil {
		return domainprojection.Policy{}, projectionWriteError(err)
	}
	for _, accountID := range selection.AccountIDs() {
		if _, err := tx.Exec(ctx, `
			INSERT INTO projection_policy_accounts (owner_id, policy_id, account_id)
			VALUES ($1, $2, $3)`, ownerID, id, accountID); err != nil {
			return domainprojection.Policy{}, projectionWriteError(err)
		}
	}
	restored, err := restorePolicyValues(
		id, ownerID, currencyCode, horizonDays, reserveMinor, timezone, selectionMode,
		inflowPolicy, sameDayOrder, version, createdAt, updatedAt, selection.AccountIDs(),
	)
	if err != nil {
		return domainprojection.Policy{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domainprojection.Policy{}, errors.New("replace projection policy")
	}
	return restored, nil
}

func samePolicyConfiguration(left, right domainprojection.Policy) bool {
	if left.Currency() != right.Currency() || left.HorizonDays() != right.HorizonDays() ||
		left.Reserve().MinorUnits() != right.Reserve().MinorUnits() || left.FinancialTimezone() != right.FinancialTimezone() ||
		left.InflowPolicy() != right.InflowPolicy() || left.SameDayOrder() != right.SameDayOrder() ||
		left.AccountSelection().Mode() != right.AccountSelection().Mode() {
		return false
	}
	leftIDs := left.AccountSelection().AccountIDs()
	rightIDs := right.AccountSelection().AccountIDs()
	if len(leftIDs) != len(rightIDs) {
		return false
	}
	for index := range leftIDs {
		if leftIDs[index] != rightIDs[index] {
			return false
		}
	}
	return true
}

func (r *ProjectionRepository) LoadBaselineState(ctx context.Context, ownerID string, now time.Time) (applicationprojection.BaselineState, error) {
	state, _, err := r.loadProjectionState(ctx, ownerID, now, "")
	return state, err
}

func (r *ProjectionRepository) LoadSafeToSpendState(ctx context.Context, ownerID string, now time.Time, fundingAccountID string) (applicationprojection.SafeToSpendState, error) {
	state, funding, err := r.loadProjectionState(ctx, ownerID, now, fundingAccountID)
	if err != nil {
		return applicationprojection.SafeToSpendState{}, err
	}
	if funding == nil {
		return applicationprojection.SafeToSpendState{}, applicationprojection.ErrFundingAccountInvalid
	}
	return applicationprojection.SafeToSpendState{Baseline: state, FundingAccount: *funding}, nil
}

func (r *ProjectionRepository) loadProjectionState(ctx context.Context, ownerID string, now time.Time, fundingAccountID string) (applicationprojection.BaselineState, *applicationprojection.AccountBalance, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return applicationprojection.BaselineState{}, nil, errors.New("load projection inputs")
	}
	defer func() { _ = tx.Rollback(ctx) }()

	policy, err := loadPolicy(ctx, tx, ownerID)
	if err != nil {
		return applicationprojection.BaselineState{}, nil, err
	}
	if r.afterPolicyRead != nil {
		r.afterPolicyRead()
	}
	asOf, err := domainprojection.FinancialDateAt(now, policy.FinancialTimezone())
	if err != nil {
		return applicationprojection.BaselineState{}, nil, err
	}
	horizonEnd, err := asOf.AddDays(policy.HorizonDays())
	if err != nil {
		return applicationprojection.BaselineState{}, nil, err
	}

	accounts, err := loadProjectionAccounts(ctx, tx, policy)
	if err != nil {
		return applicationprojection.BaselineState{}, nil, err
	}
	accountBalances := make([]applicationprojection.AccountBalance, 0, len(accounts))
	for _, financialAccount := range accounts {
		if err := applicationprojection.ValidateSelectedAccount(financialAccount, ownerID, policy.Currency()); err != nil {
			if policy.AccountSelection().Mode() == domainprojection.ExplicitSelection() &&
				errors.Is(err, domainprojection.ErrInvalidAccountSelection) {
				return applicationprojection.BaselineState{}, nil, fmt.Errorf("%w: selected account %s", applicationprojection.ErrConfigurationInvalid, financialAccount.ID())
			}
			return applicationprojection.BaselineState{}, nil, err
		}
		state, err := loadBalanceStateInTransaction(ctx, tx, financialAccount)
		if err != nil {
			return applicationprojection.BaselineState{}, nil, err
		}
		accountBalances = append(accountBalances, applicationprojection.AccountBalance{Account: financialAccount, State: state})
	}
	var fundingAccount *applicationprojection.AccountBalance
	if strings.TrimSpace(fundingAccountID) != "" {
		for index := range accountBalances {
			if accountBalances[index].Account.ID() == fundingAccountID {
				value := accountBalances[index]
				fundingAccount = &value
				break
			}
		}
		if fundingAccount == nil {
			financialAccount, err := scanAccount(tx.QueryRow(ctx, accountSelect+` WHERE owner_id = $1 AND id = $2`, ownerID, fundingAccountID))
			if err != nil {
				return applicationprojection.BaselineState{}, nil, err
			}
			state, err := loadBalanceStateInTransaction(ctx, tx, financialAccount)
			if err != nil {
				return applicationprojection.BaselineState{}, nil, err
			}
			value := applicationprojection.AccountBalance{Account: financialAccount, State: state}
			fundingAccount = &value
		}
	}
	obligations, err := loadProjectionObligations(ctx, tx, ownerID)
	if err != nil {
		return applicationprojection.BaselineState{}, nil, err
	}
	flows, err := loadProjectionFlows(ctx, tx, ownerID)
	if err != nil {
		return applicationprojection.BaselineState{}, nil, err
	}
	receivables, err := loadProjectionReceivables(ctx, tx, ownerID)
	if err != nil {
		return applicationprojection.BaselineState{}, nil, err
	}
	cardIssues, err := loadCardPaymentIssues(ctx, tx, ownerID, asOf, horizonEnd)
	if err != nil {
		return applicationprojection.BaselineState{}, nil, err
	}
	manualFlows := make([]domainschedule.ScheduledCashFlow, 0, len(flows))
	cardFlows := make([]domainschedule.ScheduledCashFlow, 0)
	for _, flow := range flows {
		if flow.SourceKind().IsManual() {
			manualFlows = append(manualFlows, flow)
		}
		// Card flows are immutable historical lineage once cancelled or settled.
		// Only the current scheduled remainder is a future projection event.
		if flow.SourceKind().IsCreditCardPaymentIntent() && flow.Status().IsScheduled() {
			cardFlows = append(cardFlows, flow)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return applicationprojection.BaselineState{}, nil, errors.New("load projection inputs")
	}
	return applicationprojection.BaselineState{
		Policy: policy, AsOf: asOf, HorizonEnd: horizonEnd, Accounts: accountBalances,
		Obligations: obligations, ManualFlows: manualFlows, CardFlows: cardFlows, CardIssues: cardIssues, Receivables: receivables,
	}, fundingAccount, nil
}

const projectionPolicySelect = `
	SELECT id, owner_id, currency, horizon_days, reserve_minor, financial_timezone,
	       account_selection_mode, inflow_policy, same_day_order, version, created_at, updated_at
	FROM projection_policies`

func loadPolicy(ctx context.Context, tx pgx.Tx, ownerID string) (domainprojection.Policy, error) {
	var id, storedOwnerID, currencyCode, timezone, selectionMode, inflowPolicy, sameDayOrder string
	var horizonDays int
	var reserveMinor, version int64
	var createdAt, updatedAt time.Time
	err := tx.QueryRow(ctx, projectionPolicySelect+` WHERE owner_id = $1`, ownerID).Scan(
		&id, &storedOwnerID, &currencyCode, &horizonDays, &reserveMinor, &timezone,
		&selectionMode, &inflowPolicy, &sameDayOrder, &version, &createdAt, &updatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domainprojection.Policy{}, applicationprojection.ErrPolicyNotFound
	}
	if err != nil {
		return domainprojection.Policy{}, errors.New("read projection policy")
	}
	rows, err := tx.Query(ctx, `
		SELECT account_id FROM projection_policy_accounts
		WHERE owner_id = $1 AND policy_id = $2 ORDER BY account_id`, storedOwnerID, id)
	if err != nil {
		return domainprojection.Policy{}, errors.New("read projection policy accounts")
	}
	accountIDs := make([]string, 0)
	for rows.Next() {
		var accountID string
		if err := rows.Scan(&accountID); err != nil {
			rows.Close()
			return domainprojection.Policy{}, errors.New("read projection policy accounts")
		}
		accountIDs = append(accountIDs, accountID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return domainprojection.Policy{}, errors.New("read projection policy accounts")
	}
	rows.Close()
	return restorePolicyValues(
		id, storedOwnerID, currencyCode, horizonDays, reserveMinor, timezone, selectionMode,
		inflowPolicy, sameDayOrder, version, createdAt, updatedAt, accountIDs,
	)
}

func restorePolicyValues(
	id, ownerID, currencyCode string,
	horizonDays int,
	reserveMinor int64,
	timezone, selectionModeValue, inflowPolicyValue, sameDayOrderValue string,
	version int64,
	createdAt, updatedAt time.Time,
	accountIDs []string,
) (domainprojection.Policy, error) {
	currency, err := money.ParseCurrency(currencyCode)
	if err != nil {
		return domainprojection.Policy{}, err
	}
	reserve, err := money.New(reserveMinor, currency)
	if err != nil {
		return domainprojection.Policy{}, err
	}
	selectionMode, err := domainprojection.ParseAccountSelectionMode(selectionModeValue)
	if err != nil {
		return domainprojection.Policy{}, err
	}
	selection, err := domainprojection.NewAccountSelection(selectionMode, accountIDs)
	if err != nil {
		return domainprojection.Policy{}, err
	}
	inflowPolicy, err := domainprojection.ParseInflowPolicy(inflowPolicyValue)
	if err != nil {
		return domainprojection.Policy{}, err
	}
	sameDayOrder, err := domainprojection.ParseSameDayOrder(sameDayOrderValue)
	if err != nil {
		return domainprojection.Policy{}, err
	}
	return domainprojection.RestorePolicy(
		id, ownerID, currency, horizonDays, reserve, timezone, selection, inflowPolicy,
		sameDayOrder, version, createdAt, updatedAt,
	)
}

func loadProjectionAccounts(ctx context.Context, tx pgx.Tx, policy domainprojection.Policy) ([]account.Account, error) {
	var rows pgx.Rows
	var err error
	if policy.AccountSelection().Mode() == domainprojection.AllActiveLiquidSelection() {
		rows, err = tx.Query(ctx, accountSelect+`
			WHERE owner_id = $1 AND status = 'active' AND account_type IN ('cash', 'bank') AND currency = $2
			ORDER BY id`, policy.OwnerID(), policy.Currency().Code())
	} else {
		rows, err = tx.Query(ctx, accountSelect+`
			WHERE owner_id = $1 AND id = ANY($2::text[]) ORDER BY id`,
			policy.OwnerID(), policy.AccountSelection().AccountIDs())
	}
	if err != nil {
		return nil, errors.New("load projection accounts")
	}
	defer rows.Close()
	result := make([]account.Account, 0)
	for rows.Next() {
		financialAccount, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, financialAccount)
	}
	if rows.Err() != nil {
		return nil, errors.New("load projection accounts")
	}
	if policy.AccountSelection().Mode() == domainprojection.ExplicitSelection() && len(result) != len(policy.AccountSelection().AccountIDs()) {
		return nil, applicationledger.ErrNotFound
	}
	return result, nil
}

func loadProjectionObligations(ctx context.Context, tx pgx.Tx, ownerID string) ([]domainschedule.Obligation, error) {
	rows, err := tx.Query(ctx, obligationSelect+` WHERE owner_id = $1 ORDER BY created_at, id`, ownerID)
	if err != nil {
		return nil, errors.New("load projection obligations")
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
		return nil, errors.New("load projection obligations")
	}
	return result, nil
}

func loadProjectionFlows(ctx context.Context, tx pgx.Tx, ownerID string) ([]domainschedule.ScheduledCashFlow, error) {
	rows, err := tx.Query(ctx, scheduledFlowSelect+` WHERE owner_id = $1 ORDER BY financial_date, created_at, id`, ownerID)
	if err != nil {
		return nil, errors.New("load projection scheduled flows")
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
		return nil, errors.New("load projection scheduled flows")
	}
	return result, nil
}

func loadCardPaymentIssues(ctx context.Context, tx pgx.Tx, ownerID string, asOf, horizonEnd financialdate.Date) ([]applicationprojection.CardPaymentIssue, error) {
	rows, err := tx.Query(ctx, `
		SELECT c.id, COALESCE(i.id,''), COALESCE(i.status,''), s.due_date::text, COALESCE(i.planned_date::text,''),
		       count(f.id), COALESCE(max(f.amount_minor),0), COALESCE(max(f.financial_date)::text,'')
		FROM credit_card_cycles c
		JOIN credit_card_statements s ON s.cycle_id=c.id AND s.superseded_at IS NULL
		LEFT JOIN credit_card_payment_intents i ON i.cycle_id=c.id
		LEFT JOIN scheduled_cash_flows f ON f.owner_id=c.owner_id AND f.source_kind='credit_card_payment_intent' AND f.source_id=i.id AND f.status='scheduled'
		WHERE c.owner_id=$1 AND s.statement_balance_minor>0
		  AND (s.due_date BETWEEN $2::date AND $3::date OR i.planned_date=$2::date)
		GROUP BY c.id,i.id,i.status,i.planned_date,s.due_date,s.statement_balance_minor
		ORDER BY c.id`, ownerID, asOf.String(), horizonEnd.String())
	if err != nil {
		return nil, errors.New("load card payment conditions")
	}
	defer rows.Close()
	issues := make([]applicationprojection.CardPaymentIssue, 0)
	for rows.Next() {
		var cycleID, intentID, status, due, planned, flowDate string
		var count, amount int64
		if err := rows.Scan(&cycleID, &intentID, &status, &due, &planned, &count, &amount, &flowDate); err != nil {
			return nil, err
		}
		if (due == asOf.String() || planned == asOf.String()) && status != "settled" {
			if status != "settled" {
				issues = append(issues, applicationprojection.CardPaymentIssue{Code: "card_payment_due_today_unsettled", CycleID: cycleID, PaymentIntentID: intentID})
			}
			continue
		}
		switch status {
		case "":
			issues = append(issues, applicationprojection.CardPaymentIssue{Code: "card_payment_intent_missing", CycleID: cycleID})
		case "needs_review":
			issues = append(issues, applicationprojection.CardPaymentIssue{Code: "card_payment_intent_needs_review", CycleID: cycleID, PaymentIntentID: intentID})
		case "cancelled":
			issues = append(issues, applicationprojection.CardPaymentIssue{Code: "card_payment_intent_missing", CycleID: cycleID, PaymentIntentID: intentID})
		case "active":
			if count != 1 || flowDate == "" {
				issues = append(issues, applicationprojection.CardPaymentIssue{Code: "invalid_card_payment_flow", CycleID: cycleID, PaymentIntentID: intentID})
			} else if flowDate == asOf.String() {
				issues = append(issues, applicationprojection.CardPaymentIssue{Code: "card_payment_due_today_unsettled", CycleID: cycleID, PaymentIntentID: intentID})
			} else if flowDate < asOf.String() {
				issues = append(issues, applicationprojection.CardPaymentIssue{Code: "card_payment_past_due_unsettled", CycleID: cycleID, PaymentIntentID: intentID})
			} else if flowDate > horizonEnd.String() {
				issues = append(issues, applicationprojection.CardPaymentIssue{Code: "invalid_card_payment_flow", CycleID: cycleID, PaymentIntentID: intentID})
			}
		case "settled":
		default:
			issues = append(issues, applicationprojection.CardPaymentIssue{Code: "invalid_card_payment_flow", CycleID: cycleID, PaymentIntentID: intentID})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New("load card payment conditions")
	}
	return issues, nil
}

func loadProjectionReceivables(ctx context.Context, tx pgx.Tx, ownerID string) ([]domainschedule.Receivable, error) {
	rows, err := tx.Query(ctx, receivableSelect+` WHERE owner_id = $1 ORDER BY created_at, id`, ownerID)
	if err != nil {
		return nil, errors.New("load projection receivables")
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
		return nil, errors.New("load projection receivables")
	}
	return result, nil
}

func projectionWriteError(err error) error {
	if isUniqueViolation(err) {
		return applicationledger.ErrConflict
	}
	var pgError interface{ SQLState() string }
	if errors.As(err, &pgError) && (pgError.SQLState() == "23503" || pgError.SQLState() == "23514") {
		return fmt.Errorf("%w: database rejected projection policy", domainprojection.ErrInvalidAccountSelection)
	}
	return errors.New("replace projection policy")
}
