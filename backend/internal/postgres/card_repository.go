package postgres

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"runway/backend/internal/card"
	"runway/backend/internal/domain/account"
	domain "runway/backend/internal/domain/card"
	"runway/backend/internal/domain/financialdate"
	"runway/backend/internal/domain/money"
	"strconv"
	"time"
)

type CardRepository struct{ pool *pgxpool.Pool }

func NewCardRepository(p *pgxpool.Pool) *CardRepository { return &CardRepository{p} }
func (r *CardRepository) RegisterStatement(ctx context.Context, owner string, in card.StatementInput, id string, now time.Time) (domain.Statement, error) {
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return domain.Statement{}, errors.New("register statement")
	}
	defer tx.Rollback(ctx)
	a, e := scanAccount(tx.QueryRow(ctx, accountSelect+` WHERE owner_id=$1 AND id=$2 FOR UPDATE`, owner, in.AccountID))
	if e != nil {
		return domain.Statement{}, e
	}
	if a.Type() != account.CreditCard() || a.Status().IsArchived() {
		return domain.Statement{}, domain.ErrInvalidCycle
	}
	if a.Currency() != in.Balance.Currency() {
		return domain.Statement{}, money.ErrCurrencyMismatch
	}
	m := in.Mutation
	resource, replay, e := claimMutation(ctx, tx, owner, "register_credit_card_statement", m, id, now)
	if e != nil {
		return domain.Statement{}, e
	}
	if replay {
		s, e := scanCardStatement(tx.QueryRow(ctx, statementSQL+` WHERE owner_id=$1 AND id=$2`, owner, resource))
		if e != nil {
			return domain.Statement{}, e
		}
		e = tx.Commit(ctx)
		return s, e
	}
	var cycleID string
	e = tx.QueryRow(ctx, `SELECT id FROM credit_card_cycles WHERE owner_id=$1 AND account_id=$2 AND cycle_start=$3::date AND cycle_end=$4::date FOR UPDATE`, owner, in.AccountID, in.Start.String(), in.End.String()).Scan(&cycleID)
	if e != nil {
		if !errors.Is(e, pgx.ErrNoRows) {
			return domain.Statement{}, e
		}
		cycleID = id + "-cycle"
		_, e = tx.Exec(ctx, `INSERT INTO credit_card_cycles(id,owner_id,account_id,cycle_start,cycle_end,created_at) VALUES($1,$2,$3,$4::date,$5::date,$6)`, cycleID, owner, in.AccountID, in.Start.String(), in.End.String(), now)
		if e != nil {
			return domain.Statement{}, e
		}
	}
	var prior domain.Statement
	hasPrior := false
	e = func() error {
		s, x := scanCardStatement(tx.QueryRow(ctx, statementSQL+` WHERE cycle_id=$1 AND superseded_at IS NULL FOR UPDATE`, cycleID))
		if errors.Is(x, card.ErrNotFound) {
			return nil
		}
		if x != nil {
			return x
		}
		prior = s
		hasPrior = true
		return nil
	}()
	if e != nil {
		return domain.Statement{}, e
	}
	if hasPrior && prior.Authority == domain.Issued && in.Authority == domain.Estimated {
		return domain.Statement{}, domain.ErrIssuedCannotBeReplacedByEstimate
	}
	rev := int64(1)
	if hasPrior {
		rev = prior.Revision + 1
	}
	s, e := domain.NewStatement(id, owner, in.AccountID, cycleID, rev, in.Authority, in.Balance, in.Minimum, in.AvoidInterest, in.Due, now)
	if e != nil {
		return domain.Statement{}, e
	}
	if hasPrior {
		_, e = tx.Exec(ctx, `UPDATE credit_card_statements SET superseded_at=$1,superseded_by_id=$2 WHERE id=$3`, now, id, prior.ID)
		if e != nil {
			return domain.Statement{}, e
		}
	}
	_, e = tx.Exec(ctx, `INSERT INTO credit_card_statements(id,owner_id,account_id,cycle_id,revision,authority,statement_balance_minor,currency,minimum_payment_minor,payment_to_avoid_interest_minor,due_date,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::date,$12)`, s.ID, s.OwnerID, s.AccountID, s.CycleID, s.Revision, s.Authority, s.Balance.MinorUnits(), s.Balance.Currency().Code(), minorPtr(s.Minimum), minorPtr(s.AvoidInterest), s.Due.String(), s.CreatedAt)
	if e != nil {
		return domain.Statement{}, e
	}
	_, e = tx.Exec(ctx, `UPDATE credit_card_installment_allocations SET status='statement_allocated' WHERE owner_id=$1 AND account_id=$2 AND cycle_id=$3 AND status='pending'`, owner, in.AccountID, cycleID)
	if e != nil {
		return domain.Statement{}, e
	}
	var invalidatedID string
	e = tx.QueryRow(ctx, `UPDATE credit_card_payment_intents SET status='needs_review',updated_at=$1 WHERE cycle_id=$2 AND status='active' AND (amount_minor>$3 OR planned_date>$4::date) RETURNING id`, now, cycleID, s.Balance.MinorUnits(), s.Due.String()).Scan(&invalidatedID)
	if errors.Is(e, pgx.ErrNoRows) {
		e = nil
	}
	if e != nil {
		return domain.Statement{}, e
	}
	if invalidatedID != "" {
		if e = cancelActiveCardFlow(ctx, tx, owner, invalidatedID, now); e != nil {
			return domain.Statement{}, e
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return domain.Statement{}, e
	}
	return s, nil
}
func (r *CardRepository) ListStatements(ctx context.Context, owner, aid string) ([]domain.Statement, error) {
	rows, e := r.pool.Query(ctx, statementSQL+` WHERE owner_id=$1 AND account_id=$2 ORDER BY created_at,id`, owner, aid)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []domain.Statement{}
	for rows.Next() {
		s, e := scanCardStatement(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
func (r *CardRepository) GetStatement(ctx context.Context, owner, id string) (domain.Statement, error) {
	return scanCardStatement(r.pool.QueryRow(ctx, statementSQL+` WHERE owner_id=$1 AND id=$2`, owner, id))
}
func (r *CardRepository) GetIntent(ctx context.Context, owner, cycle string) (domain.PaymentIntent, error) {
	return scanIntent(r.pool.QueryRow(ctx, intentSQL+` WHERE owner_id=$1 AND cycle_id=$2`, owner, cycle))
}
func (r *CardRepository) GetIntentSummary(ctx context.Context, owner, cycle string) (card.PaymentIntentSummary, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return card.PaymentIntentSummary{}, err
	}
	defer tx.Rollback(ctx)
	intent, err := scanIntent(tx.QueryRow(ctx, intentSQL+` WHERE owner_id=$1 AND cycle_id=$2`, owner, cycle))
	if err != nil {
		return card.PaymentIntentSummary{}, err
	}
	summary, err := intentSummary(ctx, tx, owner, intent)
	if err != nil {
		return card.PaymentIntentSummary{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return card.PaymentIntentSummary{}, err
	}
	return summary, nil
}
func (r *CardRepository) ReplaceIntent(ctx context.Context, owner, cycle string, amount money.Money, date financialdate.Date, id string, now time.Time) (domain.PaymentIntent, error) {
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return domain.PaymentIntent{}, e
	}
	defer tx.Rollback(ctx)
	var aid string
	var bal int64
	var due string
	var curr string
	e = tx.QueryRow(ctx, `SELECT c.account_id,s.statement_balance_minor,s.due_date::text,s.currency FROM credit_card_cycles c JOIN credit_card_statements s ON s.cycle_id=c.id AND s.superseded_at IS NULL WHERE c.owner_id=$1 AND c.id=$2 FOR UPDATE`, owner, cycle).Scan(&aid, &bal, &due, &curr)
	if e != nil {
		return domain.PaymentIntent{}, domain.ErrNoAuthoritativeStatement
	}
	if amount.Currency().Code() != curr || amount.MinorUnits() <= 0 || amount.MinorUnits() > bal {
		return domain.PaymentIntent{}, domain.ErrInvalidPaymentIntent
	}
	d, _ := financialdate.Parse(due)
	cmp, _ := date.Compare(d)
	if cmp > 0 {
		return domain.PaymentIntent{}, domain.ErrInvalidPaymentIntent
	}
	old, e := scanIntent(tx.QueryRow(ctx, intentSQL+` WHERE owner_id=$1 AND cycle_id=$2 FOR UPDATE`, owner, cycle))
	if errors.Is(e, card.ErrNotFound) {
		x, e := domain.NewPaymentIntent(id, owner, aid, cycle, amount, date, domain.IntentActive, 1, now, now)
		if e != nil {
			return x, e
		}
		_, e = tx.Exec(ctx, `INSERT INTO credit_card_payment_intents(id,owner_id,account_id,cycle_id,amount_minor,currency,planned_date,status,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7::date,$8,$9,$10,$11)`, x.ID, x.OwnerID, x.AccountID, x.CycleID, x.Amount.MinorUnits(), x.Amount.Currency().Code(), x.Planned.String(), x.Status, x.Version, x.CreatedAt, x.UpdatedAt)
		if e != nil {
			return x, e
		}
		if e = createCardFlow(ctx, tx, x, x.Amount.MinorUnits(), now); e != nil {
			return x, e
		}
		e = tx.Commit(ctx)
		return x, e
	}
	if e != nil {
		return domain.PaymentIntent{}, e
	}
	if old.Status == domain.IntentCancelled {
		return domain.PaymentIntent{}, domain.ErrPaymentIntentCancelled
	}
	if old.Status == domain.IntentSettled {
		return domain.PaymentIntent{}, domain.ErrPaymentIntentSettled
	}
	same := old.Amount.MinorUnits() == amount.MinorUnits() && old.Planned.String() == date.String() && old.Status == domain.IntentActive
	if same {
		e = tx.Commit(ctx)
		return old, e
	}
	v := old.Version + 1
	_, e = tx.Exec(ctx, `UPDATE credit_card_payment_intents SET amount_minor=$1,currency=$2,planned_date=$3::date,status='active',version=$4,updated_at=$5 WHERE id=$6`, amount.MinorUnits(), curr, date.String(), v, now, old.ID)
	if e != nil {
		return domain.PaymentIntent{}, e
	}
	x, e := domain.NewPaymentIntent(old.ID, owner, aid, cycle, amount, date, domain.IntentActive, v, old.CreatedAt, now)
	if e != nil {
		return x, e
	}
	if e = cancelActiveCardFlow(ctx, tx, owner, old.ID, now); e != nil {
		return x, e
	}
	if e = createCardFlow(ctx, tx, x, x.Amount.MinorUnits(), now); e != nil {
		return x, e
	}
	e = tx.Commit(ctx)
	return x, e
}
func (r *CardRepository) CancelIntent(ctx context.Context, owner, cycle string, now time.Time) (domain.PaymentIntent, error) {
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return domain.PaymentIntent{}, e
	}
	defer tx.Rollback(ctx)
	old, e := scanIntent(tx.QueryRow(ctx, intentSQL+` WHERE owner_id=$1 AND cycle_id=$2 FOR UPDATE`, owner, cycle))
	if e != nil {
		return domain.PaymentIntent{}, e
	}
	if old.Status == domain.IntentCancelled {
		e = tx.Commit(ctx)
		return old, e
	}
	if old.Status == domain.IntentSettled {
		return domain.PaymentIntent{}, domain.ErrPaymentIntentSettled
	}
	v := old.Version + 1
	_, e = tx.Exec(ctx, `UPDATE credit_card_payment_intents SET status='cancelled',version=$1,updated_at=$2 WHERE id=$3`, v, now, old.ID)
	if e != nil {
		return domain.PaymentIntent{}, e
	}
	if e = cancelActiveCardFlow(ctx, tx, owner, old.ID, now); e != nil {
		return domain.PaymentIntent{}, e
	}
	x, e := domain.NewPaymentIntent(old.ID, old.OwnerID, old.AccountID, old.CycleID, old.Amount, old.Planned, domain.IntentCancelled, v, old.CreatedAt, now)
	if e != nil {
		return x, e
	}
	e = tx.Commit(ctx)
	return x, e
}

func (r *CardRepository) SettleIntent(ctx context.Context, owner string, in card.PaymentIntentSettlementInput, id string, now time.Time) (card.PaymentIntentSettlementResult, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return card.PaymentIntentSettlementResult{}, err
	}
	defer tx.Rollback(ctx)
	resource, replay, err := claimMutation(ctx, tx, owner, "settle_credit_card_payment_intent", in.Mutation, id, now)
	if err != nil {
		return card.PaymentIntentSettlementResult{}, err
	}
	if replay {
		settlement, err := scanIntentSettlement(tx.QueryRow(ctx, settlementSQL+` WHERE owner_id=$1 AND id=$2`, owner, resource))
		if err != nil {
			return card.PaymentIntentSettlementResult{}, err
		}
		intent, err := scanIntent(tx.QueryRow(ctx, intentSQL+` WHERE owner_id=$1 AND id=$2`, owner, settlement.IntentID))
		if err != nil {
			return card.PaymentIntentSettlementResult{}, err
		}
		if err = tx.Commit(ctx); err != nil {
			return card.PaymentIntentSettlementResult{}, err
		}
		return card.PaymentIntentSettlementResult{Settlement: settlement, Intent: intent}, nil
	}
	intent, err := scanIntent(tx.QueryRow(ctx, intentSQL+` WHERE owner_id=$1 AND id=$2 FOR UPDATE`, owner, in.IntentID))
	if err != nil {
		return card.PaymentIntentSettlementResult{}, err
	}
	if intent.Status != domain.IntentActive {
		return card.PaymentIntentSettlementResult{}, domain.ErrInvalidPaymentIntentSettlement
	}
	var amount int64
	var currency, sourceTransactionID string
	err = tx.QueryRow(ctx, `SELECT lt.amount_minor,lt.currency,src.id
		FROM linked_transfers lt
		JOIN financial_transactions src ON src.owner_id=lt.owner_id AND src.transfer_id=lt.id AND src.account_id=lt.source_account_id
		JOIN accounts source_account ON source_account.owner_id=lt.owner_id AND source_account.id=lt.source_account_id
		WHERE lt.owner_id=$1 AND lt.id=$2 AND lt.destination_account_id=$3
		  AND src.effect='asset_outflow' AND source_account.account_type IN ('cash','bank')`, owner, in.TransferID, intent.AccountID).Scan(&amount, &currency, &sourceTransactionID)
	if err != nil || currency != intent.Amount.Currency().Code() || amount <= 0 {
		return card.PaymentIntentSettlementResult{}, domain.ErrInvalidPaymentIntentSettlement
	}
	var settledBefore int64
	if err = tx.QueryRow(ctx, `SELECT COALESCE(SUM(amount_minor),0) FROM credit_card_payment_intent_settlements WHERE intent_id=$1`, intent.ID).Scan(&settledBefore); err != nil {
		return card.PaymentIntentSettlementResult{}, err
	}
	if settledBefore > intent.Amount.MinorUnits() || amount > intent.Amount.MinorUnits()-settledBefore {
		return card.PaymentIntentSettlementResult{}, domain.ErrInvalidPaymentIntentSettlement
	}
	settlement := card.PaymentIntentSettlement{ID: id, OwnerID: owner, AccountID: intent.AccountID, CycleID: intent.CycleID, IntentID: intent.ID, TransferID: in.TransferID, CreatedAt: now}
	settlement.Amount, _ = money.New(amount, intent.Amount.Currency())
	_, err = tx.Exec(ctx, `INSERT INTO credit_card_payment_intent_settlements(id,owner_id,account_id,cycle_id,intent_id,transfer_id,amount_minor,currency,source_transaction_id,created_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, settlement.ID, settlement.OwnerID, settlement.AccountID, settlement.CycleID, settlement.IntentID, settlement.TransferID, amount, currency, sourceTransactionID, now)
	if err != nil {
		return card.PaymentIntentSettlementResult{}, err
	}
	remaining := intent.Amount.MinorUnits() - settledBefore - amount
	if remaining == 0 {
		_, err = tx.Exec(ctx, `UPDATE scheduled_cash_flows SET status='settled',settlement_transaction_id=$1,updated_at=$2 WHERE owner_id=$3 AND source_kind='credit_card_payment_intent' AND source_id=$4 AND status='scheduled'`, sourceTransactionID, now, owner, intent.ID)
		if err != nil {
			return card.PaymentIntentSettlementResult{}, err
		}
		_, err = tx.Exec(ctx, `UPDATE credit_card_payment_intents SET status='settled',version=version+1,updated_at=$1 WHERE id=$2`, now, intent.ID)
		if err != nil {
			return card.PaymentIntentSettlementResult{}, err
		}
	} else {
		if err = cancelActiveCardFlow(ctx, tx, owner, intent.ID, now); err != nil {
			return card.PaymentIntentSettlementResult{}, err
		}
		if err = createCardFlow(ctx, tx, intent, remaining, now); err != nil {
			return card.PaymentIntentSettlementResult{}, err
		}
	}
	updated, err := scanIntent(tx.QueryRow(ctx, intentSQL+` WHERE owner_id=$1 AND id=$2`, owner, intent.ID))
	if err != nil {
		return card.PaymentIntentSettlementResult{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return card.PaymentIntentSettlementResult{}, err
	}
	return card.PaymentIntentSettlementResult{Settlement: settlement, Intent: updated}, nil
}

// createCardFlow is deliberately private to the card repository. Public scheduled-flow
// creation remains manual-only; card cash effects are derived solely from an active intent.
func createCardFlow(ctx context.Context, tx pgx.Tx, intent domain.PaymentIntent, remaining int64, now time.Time) error {
	if remaining <= 0 || intent.Status != domain.IntentActive {
		return domain.ErrInvalidPaymentIntent
	}
	flowID := intent.ID + "-flow-" + strconv.FormatInt(intent.Version, 10) + "-" + strconv.FormatInt(remaining, 10)
	_, err := tx.Exec(ctx, `INSERT INTO scheduled_cash_flows(id,owner_id,source_kind,source_id,amount_minor,currency,direction,financial_date,status,amount_provenance,date_provenance,inclusion_eligibility,created_at,updated_at)
		VALUES($1,$2,'credit_card_payment_intent',$3,$4,$5,'outflow',$6::date,'scheduled','exact','exact','eligible',$7,$7)`,
		flowID, intent.OwnerID, intent.ID, remaining, intent.Amount.Currency().Code(), intent.Planned.String(), now)
	return err
}

const settlementSQL = `SELECT id,owner_id,account_id,cycle_id,intent_id,transfer_id,amount_minor,currency,created_at FROM credit_card_payment_intent_settlements`

type intentSummaryQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func intentSummary(ctx context.Context, q intentSummaryQuerier, owner string, intent domain.PaymentIntent) (card.PaymentIntentSummary, error) {
	var settledMinor int64
	if err := q.QueryRow(ctx, `SELECT COALESCE(SUM(amount_minor),0) FROM credit_card_payment_intent_settlements WHERE owner_id=$1 AND intent_id=$2`, owner, intent.ID).Scan(&settledMinor); err != nil {
		return card.PaymentIntentSummary{}, err
	}
	settled, err := money.New(settledMinor, intent.Amount.Currency())
	if err != nil {
		return card.PaymentIntentSummary{}, domain.ErrInvalidPaymentIntentSettlement
	}
	remaining, err := intent.Amount.Subtract(settled)
	if err != nil {
		return card.PaymentIntentSummary{}, domain.ErrInvalidPaymentIntentSettlement
	}
	return card.PaymentIntentSummary{Intent: intent, SettledAmount: settled, RemainingAmount: remaining}, nil
}

func scanIntentSettlement(r row) (card.PaymentIntentSettlement, error) {
	var x card.PaymentIntentSettlement
	var amount int64
	var currency string
	err := r.Scan(&x.ID, &x.OwnerID, &x.AccountID, &x.CycleID, &x.IntentID, &x.TransferID, &amount, &currency, &x.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return x, card.ErrNotFound
	}
	if err != nil {
		return x, err
	}
	c, err := money.ParseCurrency(currency)
	if err != nil {
		return x, err
	}
	x.Amount, err = money.New(amount, c)
	return x, err
}

func cancelActiveCardFlow(ctx context.Context, tx pgx.Tx, owner, intentID string, now time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE scheduled_cash_flows SET status='cancelled',updated_at=$1
		WHERE owner_id=$2 AND source_kind='credit_card_payment_intent' AND source_id=$3 AND status='scheduled'`, now, owner, intentID)
	return err
}

const statementSQL = `SELECT id,owner_id,account_id,cycle_id,revision,authority,statement_balance_minor,currency,minimum_payment_minor,payment_to_avoid_interest_minor,due_date::text,created_at,superseded_at,superseded_by_id FROM credit_card_statements`
const intentSQL = `SELECT id,owner_id,account_id,cycle_id,amount_minor,currency,planned_date::text,status,version,created_at,updated_at FROM credit_card_payment_intents`

type row interface{ Scan(...any) error }

func scanCardStatement(r row) (domain.Statement, error) {
	var id, o, a, c, au, cu, due string
	var rev, bal int64
	var min, av *int64
	var created time.Time
	var sup *time.Time
	var supid *string
	e := r.Scan(&id, &o, &a, &c, &rev, &au, &bal, &cu, &min, &av, &due, &created, &sup, &supid)
	if errors.Is(e, pgx.ErrNoRows) {
		return domain.Statement{}, card.ErrNotFound
	}
	if e != nil {
		return domain.Statement{}, e
	}
	m, _ := money.New(bal, money.MXN())
	var mi, ai *money.Money
	if min != nil {
		x, _ := money.New(*min, money.MXN())
		mi = &x
	}
	if av != nil {
		x, _ := money.New(*av, money.MXN())
		ai = &x
	}
	d, _ := financialdate.Parse(due)
	s, e := domain.NewStatement(id, o, a, c, rev, domain.Authority(au), m, mi, ai, d, created)
	if e != nil {
		return s, e
	}
	s.SupersededAt = sup
	if supid != nil {
		s.SupersededBy = *supid
	}
	return s, nil
}
func scanIntent(r row) (domain.PaymentIntent, error) {
	var id, o, a, c, cu, d, st string
	var amt, v int64
	var created, updated time.Time
	e := r.Scan(&id, &o, &a, &c, &amt, &cu, &d, &st, &v, &created, &updated)
	if errors.Is(e, pgx.ErrNoRows) {
		return domain.PaymentIntent{}, card.ErrNotFound
	}
	if e != nil {
		return domain.PaymentIntent{}, e
	}
	m, _ := money.New(amt, money.MXN())
	date, _ := financialdate.Parse(d)
	return domain.NewPaymentIntent(id, o, a, c, m, date, domain.IntentStatus(st), v, created, updated)
}
func minorPtr(m *money.Money) any {
	if m == nil {
		return nil
	}
	return m.MinorUnits()
}
