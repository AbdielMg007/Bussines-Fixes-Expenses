package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"runway/backend/internal/domain/account"
	domaincard "runway/backend/internal/domain/card"
	"runway/backend/internal/domain/financialdate"
	domainledger "runway/backend/internal/domain/ledger"
	"runway/backend/internal/domain/money"
	domainprojection "runway/backend/internal/domain/projection"
	domainschedule "runway/backend/internal/domain/schedule"
	applicationledger "runway/backend/internal/ledger"
	applicationprojection "runway/backend/internal/projection"
	applicationschedule "runway/backend/internal/schedule"
)

func TestCardPaymentIssuesTreatPastAndTodayActiveFlowsAsIndeterminate(t *testing.T) {
	pool := newIsolatedTestDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	createTestOwner(t, pool, "owner", now)
	ledgerService, _ := applicationledger.NewService(NewLedgerRepository(pool), applicationledger.ServiceOptions{Clock: func() time.Time { return now }, IDGenerator: sequentialIDs()})
	cardAccount := createTestAccount(t, ledgerService, "owner", "Card", account.CreditCard())
	cards := NewCardRepository(pool)
	statement, err := cards.RegisterStatement(ctx, "owner", cardStatementInput(t, "owner", cardAccount.ID(), domaincard.Issued, 280_000, "2026-09-09", "statement"), "statement", now)
	if err != nil {
		t.Fatal(err)
	}
	amount, _ := money.New(280_000, money.MXN())
	past := cardTestDate(t, "2026-08-01")
	if _, err = cards.ReplaceIntent(ctx, "owner", statement.CycleID, amount, past, "intent", now); err != nil {
		t.Fatal(err)
	}
	asOf := cardTestDate(t, "2026-08-10")
	// The statement due date lies beyond this intentionally short horizon. A
	// past active plan is still unresolved and must not disappear optimistically.
	end := cardTestDate(t, "2026-08-11")
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	issues, err := loadCardPaymentIssues(ctx, tx, "owner", asOf, end)
	_ = tx.Rollback(ctx)
	if err != nil || len(issues) != 1 || issues[0].Code != "card_payment_past_due_unsettled" {
		t.Fatalf("past issues=%+v err=%v", issues, err)
	}
	// A valid replacement planned today is also unresolved because Issue 7 excludes same-day events.
	today := cardTestDate(t, "2026-08-10")
	if _, err = cards.ReplaceIntent(ctx, "owner", statement.CycleID, amount, today, "replace", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	tx, err = pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	issues, err = loadCardPaymentIssues(ctx, tx, "owner", asOf, end)
	_ = tx.Rollback(ctx)
	if err != nil || len(issues) != 1 || issues[0].Code != "card_payment_due_today_unsettled" {
		t.Fatalf("today issues=%+v err=%v", issues, err)
	}
}

func TestPastDueStatementWithoutIntentMakesProjectionAndSafeToSpendIndeterminate(t *testing.T) {
	pool := newIsolatedTestDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	createTestOwner(t, pool, "owner", now)
	ledgerService, _ := applicationledger.NewService(NewLedgerRepository(pool), applicationledger.ServiceOptions{Clock: func() time.Time { return now }, IDGenerator: sequentialIDs()})
	projectionService, _ := applicationprojection.NewService(NewProjectionRepository(pool), applicationprojection.ServiceOptions{Clock: func() time.Time { return now }})
	bank := createTestAccount(t, ledgerService, "owner", "Bank", account.Bank())
	cardAccount := createTestAccount(t, ledgerService, "owner", "Card", account.CreditCard())
	openingDate := projectionTestDate(t, "2026-08-10")
	postProjectionTransaction(t, ledgerService, bank.ID(), domainledger.AssetInflow(), 1_000_000, openingDate, "opening-bank")
	cards := NewCardRepository(pool)
	if _, err := cards.RegisterStatement(ctx, "owner", cardStatementInput(t, "owner", cardAccount.ID(), domaincard.Issued, 280_000, "2026-08-09", "past-due-missing-intent"), "statement", now); err != nil {
		t.Fatal(err)
	}
	reserve, _ := money.Zero(money.MXN())
	all, _ := domainprojection.NewAccountSelection(domainprojection.AllActiveLiquidSelection(), nil)
	if _, err := projectionService.ReplacePolicy(ctx, "owner", money.MXN(), 30, reserve, "America/Mexico_City", all, domainprojection.ConfirmedInflowsOnly(), domainprojection.OutflowsBeforeInflows()); err != nil {
		t.Fatal(err)
	}

	projection, err := projectionService.CalculateBaseline(ctx, "owner")
	if err != nil || projection.Completeness != domainprojection.ProjectionIndeterminate || len(projection.Issues) != 1 || projection.Issues[0].Code != "card_payment_intent_missing" {
		t.Fatalf("past-due missing intent projection=%+v err=%v", projection, err)
	}
	safe, err := projectionService.CalculateSafeToSpend(ctx, "owner", bank.ID())
	if err != nil || safe.Status != domainprojection.IndeterminateStatus() || safe.SafeToSpend.MinorUnits() != 0 || safe.IndeterminateReason != "card_payment_intent_missing" {
		t.Fatalf("past-due missing intent safe-to-spend=%+v err=%v", safe, err)
	}
}

func TestProjectionRepositoryPolicySelectionPersistenceAndOwnerIsolation(t *testing.T) {
	pool := newIsolatedTestDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 18, 0, 0, 0, time.UTC)
	createTestOwner(t, pool, "owner", now)
	ledgerService, _ := applicationledger.NewService(NewLedgerRepository(pool), applicationledger.ServiceOptions{Clock: func() time.Time { return now }, IDGenerator: sequentialIDs()})
	bank := createTestAccount(t, ledgerService, "owner", "Bank", account.Bank())
	cash := createTestAccount(t, ledgerService, "owner", "Cash", account.Cash())
	card := createTestAccount(t, ledgerService, "owner", "Card", account.CreditCard())
	loan := createTestAccount(t, ledgerService, "owner", "Loan", account.Loan())

	repository := NewProjectionRepository(pool)
	service, _ := applicationprojection.NewService(repository, applicationprojection.ServiceOptions{Clock: func() time.Time { return now }})
	reserve, _ := money.Zero(money.MXN())
	all, _ := domainprojection.NewAccountSelection(domainprojection.AllActiveLiquidSelection(), nil)
	created, err := service.ReplacePolicy(ctx, "owner", money.MXN(), 60, reserve, "America/Mexico_City", all, domainprojection.ConfirmedInflowsOnly(), domainprojection.OutflowsBeforeInflows())
	if err != nil || created.Version() != 1 {
		t.Fatalf("create policy = %+v, %v", created, err)
	}
	loaded, err := service.GetPolicy(ctx, "owner")
	if err != nil || loaded.ID() != "owner" || loaded.AccountSelection().Mode() != domainprojection.AllActiveLiquidSelection() {
		t.Fatalf("loaded policy = %+v, %v", loaded, err)
	}
	replayed, err := service.ReplacePolicy(ctx, "owner", money.MXN(), 60, reserve, "America/Mexico_City", all, domainprojection.ConfirmedInflowsOnly(), domainprojection.OutflowsBeforeInflows())
	if err != nil || replayed.Version() != 1 || !replayed.UpdatedAt().Equal(created.UpdatedAt()) {
		t.Fatalf("idempotent policy replacement = %+v, %v", replayed, err)
	}
	state, err := repository.LoadBaselineState(ctx, "owner", now)
	if err != nil || len(state.Accounts) != 2 || state.Accounts[0].Account.ID() != bank.ID() && state.Accounts[0].Account.ID() != cash.ID() {
		t.Fatalf("all-active accounts = %+v, %v", state.Accounts, err)
	}
	for _, selected := range state.Accounts {
		if selected.Account.ID() == card.ID() || selected.Account.ID() == loan.ID() {
			t.Fatal("liability entered all-active liquidity")
		}
	}

	explicit, _ := domainprojection.NewAccountSelection(domainprojection.ExplicitSelection(), []string{bank.ID()})
	replaced, err := service.ReplacePolicy(ctx, "owner", money.MXN(), 30, reserve, "America/Mexico_City", explicit, domainprojection.IncludeExpectedInflows(), domainprojection.OutflowsBeforeInflows())
	if err != nil || replaced.Version() != 2 || len(replaced.AccountSelection().AccountIDs()) != 1 {
		t.Fatalf("replace policy = %+v, %v", replaced, err)
	}
	explicitState, err := repository.LoadBaselineState(ctx, "owner", now)
	if err != nil || len(explicitState.Accounts) != 1 || explicitState.Accounts[0].Account.ID() != bank.ID() {
		t.Fatalf("explicit accounts = %+v, %v", explicitState.Accounts, err)
	}

	explicitCash, _ := domainprojection.NewAccountSelection(domainprojection.ExplicitSelection(), []string{cash.ID()})
	if value, err := service.ReplacePolicy(ctx, "owner", money.MXN(), 30, reserve, "America/Mexico_City", explicitCash, domainprojection.IncludeExpectedInflows(), domainprojection.OutflowsBeforeInflows()); err != nil || value.Version() != 3 {
		t.Fatalf("explicit A to B = %+v, %v", value, err)
	}
	assertProjectionPolicyAccountIDs(t, pool, "owner", []string{cash.ID()})
	if value, err := service.ReplacePolicy(ctx, "owner", money.MXN(), 30, reserve, "America/Mexico_City", all, domainprojection.IncludeExpectedInflows(), domainprojection.OutflowsBeforeInflows()); err != nil || value.Version() != 4 {
		t.Fatalf("explicit to all-active = %+v, %v", value, err)
	}
	assertProjectionPolicyAccountIDs(t, pool, "owner", nil)
	if value, err := service.ReplacePolicy(ctx, "owner", money.MXN(), 30, reserve, "America/Mexico_City", explicit, domainprojection.IncludeExpectedInflows(), domainprojection.OutflowsBeforeInflows()); err != nil || value.Version() != 5 {
		t.Fatalf("all-active to explicit = %+v, %v", value, err)
	}
	assertProjectionPolicyAccountIDs(t, pool, "owner", []string{bank.ID()})

	for _, invalidID := range []string{card.ID(), loan.ID()} {
		invalid, _ := domainprojection.NewAccountSelection(domainprojection.ExplicitSelection(), []string{invalidID})
		if _, err := service.ReplacePolicy(ctx, "owner", money.MXN(), 30, reserve, "America/Mexico_City", invalid, domainprojection.ConfirmedInflowsOnly(), domainprojection.OutflowsBeforeInflows()); !errors.Is(err, domainprojection.ErrInvalidAccountSelection) {
			t.Fatalf("liability selection %s error = %v", invalidID, err)
		}
	}
	if _, err := ledgerService.ArchiveAccount(ctx, "owner", cash.ID()); err != nil {
		t.Fatal(err)
	}
	archived, _ := domainprojection.NewAccountSelection(domainprojection.ExplicitSelection(), []string{cash.ID()})
	if _, err := service.ReplacePolicy(ctx, "owner", money.MXN(), 30, reserve, "America/Mexico_City", archived, domainprojection.ConfirmedInflowsOnly(), domainprojection.OutflowsBeforeInflows()); !errors.Is(err, domainprojection.ErrInvalidAccountSelection) {
		t.Fatalf("archived selection error = %v", err)
	}
	unchanged, err := service.GetPolicy(ctx, "owner")
	if err != nil || unchanged.Version() != 5 || len(unchanged.AccountSelection().AccountIDs()) != 1 || unchanged.AccountSelection().AccountIDs()[0] != bank.ID() {
		t.Fatalf("failed replacement changed policy = %+v, %v", unchanged, err)
	}

	if _, err := ledgerService.ArchiveAccount(ctx, "owner", bank.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.LoadBaselineState(ctx, "owner", now); !errors.Is(err, applicationprojection.ErrConfigurationInvalid) {
		t.Fatalf("stale explicit selection error = %v", err)
	}
	stalePolicy, err := service.GetPolicy(ctx, "owner")
	if err != nil || stalePolicy.Version() != 5 || stalePolicy.AccountSelection().AccountIDs()[0] != bank.ID() {
		t.Fatalf("stale policy mutated = %+v, %v", stalePolicy, err)
	}

	if _, err := pool.Exec(ctx, `ALTER TABLE owners DROP CONSTRAINT owners_singleton_key_key`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO owners (id, singleton_key, email, password_hash, created_at, updated_at)
		VALUES ('other', TRUE, 'other@example.com', 'encoded-password-hash', $1, $1)`, now); err != nil {
		t.Fatal(err)
	}
	otherLedger, _ := applicationledger.NewService(NewLedgerRepository(pool), applicationledger.ServiceOptions{Clock: func() time.Time { return now }, IDGenerator: func() (string, error) { return "other-bank-id", nil }})
	otherBank := createTestAccount(t, otherLedger, "other", "Other bank", account.Bank())
	crossOwner, _ := domainprojection.NewAccountSelection(domainprojection.ExplicitSelection(), []string{otherBank.ID()})
	if _, err := service.ReplacePolicy(ctx, "owner", money.MXN(), 30, reserve, "America/Mexico_City", crossOwner, domainprojection.ConfirmedInflowsOnly(), domainprojection.OutflowsBeforeInflows()); !errors.Is(err, applicationledger.ErrNotFound) {
		t.Fatalf("cross-owner selection error = %v", err)
	}

	// The composite foreign keys and validation trigger also protect the adapter boundary.
	_, err = pool.Exec(ctx, `INSERT INTO projection_policy_accounts (owner_id, policy_id, account_id) VALUES ('owner', 'owner', $1)`, card.ID())
	if !hasPostgresCode(err, "23514") {
		t.Fatalf("raw liability selection error = %v", err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO projection_policy_accounts (owner_id, policy_id, account_id) VALUES ('owner', 'owner', $1)`, otherBank.ID())
	if !hasPostgresCode(err, "23503") {
		t.Fatalf("raw cross-owner selection error = %v", err)
	}
}

func TestProjectionPolicyConcurrentReplacementVersionsAreMonotonicAndAtomic(t *testing.T) {
	pool := newIsolatedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	now := time.Date(2026, 8, 10, 18, 0, 0, 0, time.UTC)
	createTestOwner(t, pool, "owner", now)
	ledgerService, _ := applicationledger.NewService(NewLedgerRepository(pool), applicationledger.ServiceOptions{Clock: func() time.Time { return now }, IDGenerator: sequentialIDs()})
	bank := createTestAccount(t, ledgerService, "owner", "Bank", account.Bank())
	cash := createTestAccount(t, ledgerService, "owner", "Cash", account.Cash())
	card := createTestAccount(t, ledgerService, "owner", "Card", account.CreditCard())
	service, _ := applicationprojection.NewService(NewProjectionRepository(pool), applicationprojection.ServiceOptions{Clock: func() time.Time { return now }})
	reserve, _ := money.Zero(money.MXN())
	all, _ := domainprojection.NewAccountSelection(domainprojection.AllActiveLiquidSelection(), nil)
	if initial, err := service.ReplacePolicy(ctx, "owner", money.MXN(), 60, reserve, "America/Mexico_City", all, domainprojection.ConfirmedInflowsOnly(), domainprojection.OutflowsBeforeInflows()); err != nil || initial.Version() != 1 {
		t.Fatalf("initial policy = %+v, %v", initial, err)
	}

	bankSelection, _ := domainprojection.NewAccountSelection(domainprojection.ExplicitSelection(), []string{bank.ID()})
	cashSelection, _ := domainprojection.NewAccountSelection(domainprojection.ExplicitSelection(), []string{cash.ID()})
	type replacement struct {
		policy domainprojection.Policy
		err    error
	}
	results := make(chan replacement, 2)
	start := make(chan struct{})
	for _, request := range []struct {
		horizon   int
		selection domainprojection.AccountSelection
	}{
		{horizon: 30, selection: bankSelection},
		{horizon: 45, selection: cashSelection},
	} {
		request := request
		go func() {
			<-start
			policy, err := service.ReplacePolicy(ctx, "owner", money.MXN(), request.horizon, reserve, "America/Mexico_City", request.selection, domainprojection.IncludeExpectedInflows(), domainprojection.OutflowsBeforeInflows())
			results <- replacement{policy: policy, err: err}
		}()
	}
	close(start)
	versions := map[int64]bool{}
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		versions[result.policy.Version()] = true
	}
	if !versions[2] || !versions[3] || len(versions) != 2 {
		t.Fatalf("concurrent versions = %v", versions)
	}
	current, err := service.GetPolicy(ctx, "owner")
	if err != nil || current.Version() != 3 || len(current.AccountSelection().AccountIDs()) != 1 {
		t.Fatalf("current policy = %+v, %v", current, err)
	}
	assertProjectionPolicyAccountIDs(t, pool, "owner", current.AccountSelection().AccountIDs())
	stable, err := service.ReplacePolicy(ctx, "owner", money.MXN(), current.HorizonDays(), reserve, current.FinancialTimezone(), current.AccountSelection(), current.InflowPolicy(), current.SameDayOrder())
	if err != nil || stable.Version() != 3 {
		t.Fatalf("identical replacement = %+v, %v", stable, err)
	}
	invalid, _ := domainprojection.NewAccountSelection(domainprojection.ExplicitSelection(), []string{card.ID()})
	if _, err := service.ReplacePolicy(ctx, "owner", money.MXN(), 20, reserve, "America/Mexico_City", invalid, domainprojection.ConfirmedInflowsOnly(), domainprojection.OutflowsBeforeInflows()); !errors.Is(err, domainprojection.ErrInvalidAccountSelection) {
		t.Fatalf("invalid replacement error = %v", err)
	}
	unchanged, err := service.GetPolicy(ctx, "owner")
	if err != nil || unchanged.Version() != 3 || unchanged.HorizonDays() != current.HorizonDays() || unchanged.AccountSelection().AccountIDs()[0] != current.AccountSelection().AccountIDs()[0] {
		t.Fatalf("failed replacement changed policy = %+v, %v", unchanged, err)
	}
}

func TestBaselineProjectionIntegrationTraceAndExclusions(t *testing.T) {
	pool := newIsolatedTestDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 18, 0, 0, 0, time.UTC)
	createTestOwner(t, pool, "owner", now)
	ids := sequentialIDs()
	ledgerService, _ := applicationledger.NewService(NewLedgerRepository(pool), applicationledger.ServiceOptions{Clock: func() time.Time { return now }, IDGenerator: ids})
	scheduleService, _ := applicationschedule.NewService(NewScheduleRepository(pool), applicationschedule.ServiceOptions{Clock: func() time.Time { return now }, IDGenerator: ids})
	projectionService, _ := applicationprojection.NewService(NewProjectionRepository(pool), applicationprojection.ServiceOptions{Clock: func() time.Time { return now }})
	bank := createTestAccount(t, ledgerService, "owner", "Bank", account.Bank())
	card := createTestAccount(t, ledgerService, "owner", "Card", account.CreditCard())
	date := projectionTestDate(t, "2026-08-10")
	postProjectionTransaction(t, ledgerService, bank.ID(), domainledger.AssetInflow(), 260_000, date, "opening-bank")
	postProjectionTransaction(t, ledgerService, card.ID(), domainledger.LiabilityCharge(), 900_000, date, "opening-card")

	carAmount, _ := money.New(226_400, money.MXN())
	if _, err := scheduleService.CreateObligation(ctx, "owner", "Car", carAmount, domainschedule.OneTime(), projectionTestDate(t, "2026-08-15"), nil, testIdempotencyKey(t, "projection-car")); err != nil {
		t.Fatal(err)
	}
	payroll, _ := money.New(788_700, money.MXN())
	if _, err := scheduleService.CreateManualScheduledFlow(ctx, "owner", payroll, domainschedule.Inflow(), projectionTestDate(t, "2026-08-15"), domainschedule.ManualExpectedIncomeSource(), domainschedule.ExactAmount(), domainschedule.ExactDate(), domainschedule.EligibleForPolicy(), testIdempotencyKey(t, "projection-payroll")); err != nil {
		t.Fatal(err)
	}
	insurance, _ := money.New(222_300, money.MXN())
	if _, err := scheduleService.CreateManualScheduledFlow(ctx, "owner", insurance, domainschedule.Outflow(), projectionTestDate(t, "2026-09-01"), domainschedule.ManualOtherSource(), domainschedule.ExactAmount(), domainschedule.ExactDate(), domainschedule.EligibleForPolicy(), testIdempotencyKey(t, "projection-insurance")); err != nil {
		t.Fatal(err)
	}
	uncertainAmount, _ := money.New(3_500_000, money.MXN())
	if _, err := scheduleService.CreateReceivable(ctx, "owner", "Family business", uncertainAmount, nil, domainschedule.Uncertain(), domainschedule.ExactAmount(), nil, testIdempotencyKey(t, "projection-receivable")); err != nil {
		t.Fatal(err)
	}
	reserve, _ := money.New(100_000, money.MXN())
	all, _ := domainprojection.NewAccountSelection(domainprojection.AllActiveLiquidSelection(), nil)
	if _, err := projectionService.ReplacePolicy(ctx, "owner", money.MXN(), 30, reserve, "America/Mexico_City", all, domainprojection.ConfirmedInflowsOnly(), domainprojection.OutflowsBeforeInflows()); err != nil {
		t.Fatal(err)
	}

	result, err := projectionService.CalculateBaseline(ctx, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if result.OpeningBalance.MinorUnits() != 260_000 || len(result.SelectedAccountIDs) != 1 || result.SelectedAccountIDs[0] != bank.ID() {
		t.Fatalf("opening liquidity = %+v", result)
	}
	wantAfter := []int64{33_600, 822_300, 600_000}
	if len(result.Events) != len(wantAfter) {
		t.Fatalf("events = %+v", result.Events)
	}
	for index, want := range wantAfter {
		if result.Events[index].BalanceAfter.MinorUnits() != want {
			t.Fatalf("event %d after = %d, want %d", index, result.Events[index].BalanceAfter.MinorUnits(), want)
		}
	}
	if result.ClosingBalance.MinorUnits() != 600_000 || result.Reserve.MinorUnits() != 100_000 {
		t.Fatalf("closing/reserve = %d/%d", result.ClosingBalance.MinorUnits(), result.Reserve.MinorUnits())
	}
	if len(result.Exclusions) != 1 || result.Exclusions[0].SourceKind != domainprojection.ReceivableSource() || len(result.Exclusions[0].Reasons) != 2 {
		t.Fatalf("uncertain undated exclusion = %+v", result.Exclusions)
	}
	var materializedObligationFlows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM scheduled_cash_flows WHERE owner_id = 'owner' AND source_kind = 'obligation'`).Scan(&materializedObligationFlows); err != nil || materializedObligationFlows != 0 {
		t.Fatalf("derived obligation materialization count = %d, error = %v", materializedObligationFlows, err)
	}
}

func TestSafeToSpendIntegrationTraceFundingRulesAndNoMutation(t *testing.T) {
	pool := newIsolatedTestDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 18, 0, 0, 0, time.UTC)
	createTestOwner(t, pool, "owner", now)
	ids := sequentialIDs()
	ledgerService, _ := applicationledger.NewService(NewLedgerRepository(pool), applicationledger.ServiceOptions{Clock: func() time.Time { return now }, IDGenerator: ids})
	scheduleService, _ := applicationschedule.NewService(NewScheduleRepository(pool), applicationschedule.ServiceOptions{Clock: func() time.Time { return now }, IDGenerator: ids})
	projectionService, _ := applicationprojection.NewService(NewProjectionRepository(pool), applicationprojection.ServiceOptions{Clock: func() time.Time { return now }})
	bank := createTestAccount(t, ledgerService, "owner", "Bank", account.Bank())
	card := createTestAccount(t, ledgerService, "owner", "Card", account.CreditCard())
	asOf := projectionTestDate(t, "2026-08-10")
	postProjectionTransaction(t, ledgerService, bank.ID(), domainledger.AssetInflow(), 260_000, asOf, "safe-opening-bank")
	postProjectionTransaction(t, ledgerService, card.ID(), domainledger.LiabilityCharge(), 900_000, asOf, "safe-opening-card")

	car, _ := money.New(226_400, money.MXN())
	if _, err := scheduleService.CreateObligation(ctx, "owner", "Car", car, domainschedule.OneTime(), projectionTestDate(t, "2026-08-15"), nil, testIdempotencyKey(t, "safe-car")); err != nil {
		t.Fatal(err)
	}
	payroll, _ := money.New(788_700, money.MXN())
	if _, err := scheduleService.CreateManualScheduledFlow(ctx, "owner", payroll, domainschedule.Inflow(), projectionTestDate(t, "2026-08-15"), domainschedule.ManualExpectedIncomeSource(), domainschedule.ExactAmount(), domainschedule.ExactDate(), domainschedule.EligibleForPolicy(), testIdempotencyKey(t, "safe-payroll")); err != nil {
		t.Fatal(err)
	}
	insurance, _ := money.New(222_300, money.MXN())
	if _, err := scheduleService.CreateManualScheduledFlow(ctx, "owner", insurance, domainschedule.Outflow(), projectionTestDate(t, "2026-09-01"), domainschedule.ManualOtherSource(), domainschedule.ExactAmount(), domainschedule.ExactDate(), domainschedule.EligibleForPolicy(), testIdempotencyKey(t, "safe-insurance")); err != nil {
		t.Fatal(err)
	}
	uncertain, _ := money.New(3_500_000, money.MXN())
	if _, err := scheduleService.CreateReceivable(ctx, "owner", "Family business", uncertain, nil, domainschedule.Uncertain(), domainschedule.ExactAmount(), nil, testIdempotencyKey(t, "safe-uncertain")); err != nil {
		t.Fatal(err)
	}
	all, _ := domainprojection.NewAccountSelection(domainprojection.AllActiveLiquidSelection(), nil)
	reserve300, _ := money.New(30_000, money.MXN())
	if _, err := projectionService.ReplacePolicy(ctx, "owner", money.MXN(), 30, reserve300, "America/Mexico_City", all, domainprojection.ConfirmedInflowsOnly(), domainprojection.OutflowsBeforeInflows()); err != nil {
		t.Fatal(err)
	}

	before := projectionFinancialRowCounts(t, pool)
	result, err := projectionService.CalculateSafeToSpend(ctx, "owner", bank.ID())
	if err != nil {
		t.Fatal(err)
	}
	if result.SafeToSpend.MinorUnits() != 3_600 || result.Status != domainprojection.ConstrainedByFutureCashFlowStatus() ||
		result.BaselineMinimumBalance.MinorUnits() != 33_600 || result.LimitingEventID == "" || result.LimitingDate == nil || result.LimitingDate.String() != "2026-08-15" {
		t.Fatalf("reserve 300 result = %+v", result)
	}
	if result.BaselineProjection.OpeningBalance.MinorUnits() != 260_000 || len(result.BaselineProjection.Events) != 3 || len(result.BaselineProjection.Exclusions) != 1 {
		t.Fatalf("baseline audit trace = %+v", result.BaselineProjection)
	}
	if after := projectionFinancialRowCounts(t, pool); after != before {
		t.Fatalf("safe-to-spend mutated financial rows: before=%v after=%v", before, after)
	}

	reserve500, _ := money.New(50_000, money.MXN())
	if _, err := projectionService.ReplacePolicy(ctx, "owner", money.MXN(), 30, reserve500, "America/Mexico_City", all, domainprojection.ConfirmedInflowsOnly(), domainprojection.OutflowsBeforeInflows()); err != nil {
		t.Fatal(err)
	}
	below, err := projectionService.CalculateSafeToSpend(ctx, "owner", bank.ID())
	if err != nil || below.SafeToSpend.MinorUnits() != 0 || below.Status != domainprojection.AlreadyBelowReserveStatus() || below.Deficit.MinorUnits() != 16_400 || below.EarliestBreachDate == nil || below.EarliestBreachDate.String() != "2026-08-15" {
		t.Fatalf("reserve 500 result = %+v, %v", below, err)
	}
	unsupported, err := projectionService.CalculateSafeToSpend(ctx, "owner", card.ID())
	if err != nil || unsupported.Status != domainprojection.UnsupportedFundingTypeStatus() || unsupported.SafeToSpend.MinorUnits() != 0 {
		t.Fatalf("credit-card funding = %+v, %v", unsupported, err)
	}
}

func TestSafeToSpendUsesOneRepeatableReadProjectionState(t *testing.T) {
	pool := newIsolatedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	now := time.Date(2026, 8, 10, 18, 0, 0, 0, time.UTC)
	createTestOwner(t, pool, "owner", now)
	ids := sequentialIDs()
	ledgerService, _ := applicationledger.NewService(NewLedgerRepository(pool), applicationledger.ServiceOptions{Clock: func() time.Time { return now }, IDGenerator: ids})
	bank := createTestAccount(t, ledgerService, "owner", "Bank", account.Bank())
	asOf := projectionTestDate(t, "2026-08-10")
	postProjectionTransaction(t, ledgerService, bank.ID(), domainledger.AssetInflow(), 100_000, asOf, "safe-snapshot-opening")
	reserve, _ := money.Zero(money.MXN())
	all, _ := domainprojection.NewAccountSelection(domainprojection.AllActiveLiquidSelection(), nil)
	baseRepository := NewProjectionRepository(pool)
	baseService, _ := applicationprojection.NewService(baseRepository, applicationprojection.ServiceOptions{Clock: func() time.Time { return now }})
	if _, err := baseService.ReplacePolicy(ctx, "owner", money.MXN(), 30, reserve, "America/Mexico_City", all, domainprojection.ConfirmedInflowsOnly(), domainprojection.OutflowsBeforeInflows()); err != nil {
		t.Fatal(err)
	}

	policyRead := make(chan struct{})
	continueRead := make(chan struct{})
	var once sync.Once
	repository := NewProjectionRepository(pool)
	repository.afterPolicyRead = func() {
		once.Do(func() { close(policyRead) })
		<-continueRead
	}
	service, _ := applicationprojection.NewService(repository, applicationprojection.ServiceOptions{Clock: func() time.Time { return now }})
	resultChannel := make(chan domainprojection.SafeToSpendResult, 1)
	errorChannel := make(chan error, 1)
	go func() {
		result, err := service.CalculateSafeToSpend(ctx, "owner", bank.ID())
		resultChannel <- result
		errorChannel <- err
	}()
	<-policyRead
	postProjectionTransaction(t, ledgerService, bank.ID(), domainledger.AssetOutflow(), 40_000, asOf, "safe-snapshot-outflow")
	close(continueRead)
	old := <-resultChannel
	if err := <-errorChannel; err != nil {
		t.Fatal(err)
	}
	if old.OpeningLiquidBalance.MinorUnits() != 100_000 || old.FundingAccountBalance.MinorUnits() != 100_000 || old.SafeToSpend.MinorUnits() != 100_000 {
		t.Fatalf("old coherent state = %+v", old)
	}
	fresh, err := baseService.CalculateSafeToSpend(ctx, "owner", bank.ID())
	if err != nil || fresh.OpeningLiquidBalance.MinorUnits() != 60_000 || fresh.FundingAccountBalance.MinorUnits() != 60_000 || fresh.SafeToSpend.MinorUnits() != 60_000 {
		t.Fatalf("fresh coherent state = %+v, %v", fresh, err)
	}
}

func TestProjectionReceivableProvenanceIntegrationTrace(t *testing.T) {
	pool := newIsolatedTestDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 18, 0, 0, 0, time.UTC)
	createTestOwner(t, pool, "owner", now)
	ids := sequentialIDs()
	ledgerService, _ := applicationledger.NewService(NewLedgerRepository(pool), applicationledger.ServiceOptions{Clock: func() time.Time { return now }, IDGenerator: ids})
	createTestAccount(t, ledgerService, "owner", "Bank", account.Bank())
	scheduleService, _ := applicationschedule.NewService(NewScheduleRepository(pool), applicationschedule.ServiceOptions{Clock: func() time.Time { return now }, IDGenerator: ids})
	projectionService, _ := applicationprojection.NewService(NewProjectionRepository(pool), applicationprojection.ServiceOptions{Clock: func() time.Time { return now }})

	confirmedDate := projectionTestDate(t, "2026-08-20")
	estimatedDateProvenance := domainschedule.EstimatedDate()
	exactAmount, _ := money.New(100, money.MXN())
	if _, err := scheduleService.CreateReceivable(ctx, "owner", "Confirmed estimated date", exactAmount, &confirmedDate, domainschedule.Confirmed(), domainschedule.ExactAmount(), &estimatedDateProvenance, testIdempotencyKey(t, "confirmed-estimated-date")); err != nil {
		t.Fatal(err)
	}
	expectedDate := projectionTestDate(t, "2026-08-21")
	exactDateProvenance := domainschedule.ExactDate()
	estimatedAmount, _ := money.New(200, money.MXN())
	if _, err := scheduleService.CreateReceivable(ctx, "owner", "Expected exact date", estimatedAmount, &expectedDate, domainschedule.Expected(), domainschedule.EstimatedAmount(), &exactDateProvenance, testIdempotencyKey(t, "expected-exact-date")); err != nil {
		t.Fatal(err)
	}
	uncertainDate := projectionTestDate(t, "2026-08-22")
	uncertainAmount, _ := money.New(300, money.MXN())
	if _, err := scheduleService.CreateReceivable(ctx, "owner", "Uncertain exact", uncertainAmount, &uncertainDate, domainschedule.Uncertain(), domainschedule.ExactAmount(), &exactDateProvenance, testIdempotencyKey(t, "uncertain-exact")); err != nil {
		t.Fatal(err)
	}
	reserve, _ := money.Zero(money.MXN())
	all, _ := domainprojection.NewAccountSelection(domainprojection.AllActiveLiquidSelection(), nil)
	if _, err := projectionService.ReplacePolicy(ctx, "owner", money.MXN(), 30, reserve, "America/Mexico_City", all, domainprojection.IncludeExpectedInflows(), domainprojection.OutflowsBeforeInflows()); err != nil {
		t.Fatal(err)
	}
	result, err := projectionService.CalculateBaseline(ctx, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 2 || result.ClosingBalance.MinorUnits() != 300 || len(result.Exclusions) != 1 || result.Exclusions[0].SourceID == "" {
		t.Fatalf("projection = events %+v exclusions %+v", result.Events, result.Exclusions)
	}
	first, second := result.Events[0].Event, result.Events[1].Event
	firstCertainty, firstHasCertainty := first.SourceCertainty()
	secondCertainty, secondHasCertainty := second.SourceCertainty()
	if !firstHasCertainty || firstCertainty != domainschedule.Confirmed() || first.DateProvenance() != domainschedule.EstimatedDate() || first.InclusionBasis() != domainprojection.ConfirmedReceivable() {
		t.Fatalf("confirmed trace = %+v", first)
	}
	if !secondHasCertainty || secondCertainty != domainschedule.Expected() || second.AmountProvenance() != domainschedule.EstimatedAmount() || second.DateProvenance() != domainschedule.ExactDate() || second.InclusionBasis() != domainprojection.ExpectedReceivableAllowedByPolicy() {
		t.Fatalf("expected trace = %+v", second)
	}
	if result.Exclusions[0].Reasons[0] != domainprojection.ExclusionUncertainReceivable {
		t.Fatalf("uncertain exact exclusion = %+v", result.Exclusions[0])
	}
}

func TestProjectionLoadUsesOneRepeatableReadSnapshot(t *testing.T) {
	pool := newIsolatedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	now := time.Date(2026, 8, 10, 18, 0, 0, 0, time.UTC)
	createTestOwner(t, pool, "owner", now)
	ids := sequentialIDs()
	ledgerService, _ := applicationledger.NewService(NewLedgerRepository(pool), applicationledger.ServiceOptions{Clock: func() time.Time { return now }, IDGenerator: ids})
	createTestAccount(t, ledgerService, "owner", "Bank", account.Bank())
	scheduleService, _ := applicationschedule.NewService(NewScheduleRepository(pool), applicationschedule.ServiceOptions{Clock: func() time.Time { return now }, IDGenerator: ids})
	amount, _ := money.New(100, money.MXN())
	flow, err := scheduleService.CreateManualScheduledFlow(ctx, "owner", amount, domainschedule.Outflow(), projectionTestDate(t, "2026-08-15"), domainschedule.ManualOtherSource(), domainschedule.ExactAmount(), domainschedule.ExactDate(), domainschedule.EligibleForPolicy(), testIdempotencyKey(t, "snapshot-flow"))
	if err != nil {
		t.Fatal(err)
	}
	reserve, _ := money.Zero(money.MXN())
	all, _ := domainprojection.NewAccountSelection(domainprojection.AllActiveLiquidSelection(), nil)
	baseRepository := NewProjectionRepository(pool)
	projectionService, _ := applicationprojection.NewService(baseRepository, applicationprojection.ServiceOptions{Clock: func() time.Time { return now }})
	if _, err := projectionService.ReplacePolicy(ctx, "owner", money.MXN(), 30, reserve, "America/Mexico_City", all, domainprojection.ConfirmedInflowsOnly(), domainprojection.OutflowsBeforeInflows()); err != nil {
		t.Fatal(err)
	}

	policyRead := make(chan struct{})
	continueRead := make(chan struct{})
	var once sync.Once
	repository := NewProjectionRepository(pool)
	repository.afterPolicyRead = func() {
		once.Do(func() { close(policyRead) })
		<-continueRead
	}
	resultChannel := make(chan applicationprojection.BaselineState, 1)
	errorChannel := make(chan error, 1)
	go func() {
		state, err := repository.LoadBaselineState(ctx, "owner", now)
		resultChannel <- state
		errorChannel <- err
	}()
	<-policyRead
	if _, err := scheduleService.CancelScheduledFlow(ctx, "owner", flow.ID(), testIdempotencyKey(t, "snapshot-cancel")); err != nil {
		t.Fatal(err)
	}
	close(continueRead)
	state := <-resultChannel
	if err := <-errorChannel; err != nil {
		t.Fatal(err)
	}
	if len(state.ManualFlows) != 1 || !state.ManualFlows[0].Status().IsScheduled() {
		t.Fatalf("repeatable-read state = %+v", state.ManualFlows)
	}
	fresh, err := baseRepository.LoadBaselineState(ctx, "owner", now)
	if err != nil || len(fresh.ManualFlows) != 1 || !fresh.ManualFlows[0].Status().IsCancelled() {
		t.Fatalf("fresh state = %+v, %v", fresh.ManualFlows, err)
	}
}

func TestProjectionSnapshotDoesNotMixConcurrentReceivableCollection(t *testing.T) {
	pool := newIsolatedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	now := time.Date(2026, 8, 10, 18, 0, 0, 0, time.UTC)
	createTestOwner(t, pool, "owner", now)
	ids := sequentialIDs()
	ledgerService, _ := applicationledger.NewService(NewLedgerRepository(pool), applicationledger.ServiceOptions{Clock: func() time.Time { return now }, IDGenerator: ids})
	createTestAccount(t, ledgerService, "owner", "Bank", account.Bank())
	scheduleService, _ := applicationschedule.NewService(NewScheduleRepository(pool), applicationschedule.ServiceOptions{Clock: func() time.Time { return now }, IDGenerator: ids})
	original, _ := money.New(10_000, money.MXN())
	expectedDate := projectionTestDate(t, "2026-08-20")
	dateProvenance := domainschedule.EstimatedDate()
	receivable, err := scheduleService.CreateReceivable(
		ctx, "owner", "Reimbursement", original, &expectedDate, domainschedule.Confirmed(),
		domainschedule.ExactAmount(), &dateProvenance, testIdempotencyKey(t, "snapshot-receivable"),
	)
	if err != nil {
		t.Fatal(err)
	}
	reserve, _ := money.Zero(money.MXN())
	all, _ := domainprojection.NewAccountSelection(domainprojection.AllActiveLiquidSelection(), nil)
	baseRepository := NewProjectionRepository(pool)
	projectionService, _ := applicationprojection.NewService(baseRepository, applicationprojection.ServiceOptions{Clock: func() time.Time { return now }})
	if _, err := projectionService.ReplacePolicy(ctx, "owner", money.MXN(), 30, reserve, "America/Mexico_City", all, domainprojection.ConfirmedInflowsOnly(), domainprojection.OutflowsBeforeInflows()); err != nil {
		t.Fatal(err)
	}

	policyRead := make(chan struct{})
	continueRead := make(chan struct{})
	var once sync.Once
	repository := NewProjectionRepository(pool)
	repository.afterPolicyRead = func() {
		once.Do(func() { close(policyRead) })
		<-continueRead
	}
	stateChannel := make(chan applicationprojection.BaselineState, 1)
	errorChannel := make(chan error, 1)
	go func() {
		state, err := repository.LoadBaselineState(ctx, "owner", now)
		stateChannel <- state
		errorChannel <- err
	}()
	<-policyRead
	collection, _ := money.New(4_000, money.MXN())
	if _, err := scheduleService.RecordReceivableCollection(ctx, "owner", receivable.ID(), collection, "", testIdempotencyKey(t, "snapshot-collection")); err != nil {
		t.Fatal(err)
	}
	close(continueRead)
	oldState := <-stateChannel
	if err := <-errorChannel; err != nil {
		t.Fatal(err)
	}
	if len(oldState.Receivables) != 1 || oldState.Receivables[0].CollectedAmount().MinorUnits() != 0 {
		t.Fatalf("old snapshot receivable = %+v", oldState.Receivables)
	}
	loadedDateProvenance, hasDateProvenance := oldState.Receivables[0].DateProvenance()
	if oldState.Receivables[0].Certainty() != domainschedule.Confirmed() || oldState.Receivables[0].AmountProvenance() != domainschedule.ExactAmount() || !hasDateProvenance || loadedDateProvenance != domainschedule.EstimatedDate() {
		t.Fatalf("persisted independent metadata = certainty %s amount %s date %s", oldState.Receivables[0].Certainty().String(), oldState.Receivables[0].AmountProvenance().String(), loadedDateProvenance.String())
	}
	oldOutstanding, _ := oldState.Receivables[0].OutstandingAmount()
	if oldOutstanding.MinorUnits() != 10_000 || oldState.Receivables[0].Status() != domainschedule.OpenReceivable() {
		t.Fatalf("old coherent state = outstanding %d status %s", oldOutstanding.MinorUnits(), oldState.Receivables[0].Status().String())
	}

	fresh, err := baseRepository.LoadBaselineState(ctx, "owner", now)
	if err != nil || len(fresh.Receivables) != 1 {
		t.Fatalf("fresh state = %+v, %v", fresh.Receivables, err)
	}
	freshOutstanding, _ := fresh.Receivables[0].OutstandingAmount()
	if fresh.Receivables[0].CollectedAmount().MinorUnits() != 4_000 || freshOutstanding.MinorUnits() != 6_000 || fresh.Receivables[0].Status() != domainschedule.PartialReceivable() {
		t.Fatalf("fresh coherent state = collected %d outstanding %d status %s", fresh.Receivables[0].CollectedAmount().MinorUnits(), freshOutstanding.MinorUnits(), fresh.Receivables[0].Status().String())
	}
}

func postProjectionTransaction(t *testing.T, service *applicationledger.Service, accountID string, effect domainledger.Effect, minor int64, date financialdate.Date, key string) {
	t.Helper()
	amount, _ := money.New(minor, money.MXN())
	if _, err := service.PostTransaction(context.Background(), "owner", accountID, effect, amount, date, "", testIdempotencyKey(t, key)); err != nil {
		t.Fatal(err)
	}
}

func projectionTestDate(t *testing.T, value string) financialdate.Date {
	t.Helper()
	date, err := financialdate.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	return date
}

type projectionRowCounts struct {
	transactions int
	obligations  int
	flows        int
	receivables  int
}

func projectionFinancialRowCounts(t *testing.T, pool interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) projectionRowCounts {
	t.Helper()
	var counts projectionRowCounts
	if err := pool.QueryRow(context.Background(), `
		SELECT
			(SELECT count(*) FROM financial_transactions),
			(SELECT count(*) FROM obligations),
			(SELECT count(*) FROM scheduled_cash_flows),
			(SELECT count(*) FROM receivables)`).Scan(&counts.transactions, &counts.obligations, &counts.flows, &counts.receivables); err != nil {
		t.Fatal(err)
	}
	return counts
}

func assertProjectionPolicyAccountIDs(t *testing.T, pool interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, ownerID string, want []string) {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT account_id FROM projection_policy_accounts WHERE owner_id = $1 ORDER BY account_id`, ownerID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		got = append(got, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("policy accounts = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("policy accounts = %v, want %v", got, want)
		}
	}
}
