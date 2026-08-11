package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"runway/backend/internal/domain/account"
	"runway/backend/internal/domain/financialdate"
	domainledger "runway/backend/internal/domain/ledger"
	"runway/backend/internal/domain/money"
	domainschedule "runway/backend/internal/domain/schedule"
	applicationledger "runway/backend/internal/ledger"
	applicationschedule "runway/backend/internal/schedule"
)

func exactDateProvenancePointer() *domainschedule.DateProvenance {
	value := domainschedule.ExactDate()
	return &value
}

func TestScheduleRepositoryLifecycleIdempotencyAndOwnerIsolation(t *testing.T) {
	pool := newIsolatedTestDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	createTestOwner(t, pool, "owner", now)
	service, err := applicationschedule.NewService(NewScheduleRepository(pool), applicationschedule.ServiceOptions{Clock: func() time.Time { return now }, IDGenerator: sequentialIDs()})
	if err != nil {
		t.Fatal(err)
	}
	amount, _ := money.New(2_264_00, money.MXN())
	start, _ := financialdate.Parse("2026-08-16")
	carKey := testIdempotencyKey(t, "car-obligation")
	car, err := service.CreateObligation(ctx, "owner", "Car", amount, domainschedule.Biweekly(), start, nil, carKey)
	if err != nil {
		t.Fatal(err)
	}
	replayedCar, err := service.CreateObligation(ctx, "owner", "Car", amount, domainschedule.Biweekly(), start, nil, carKey)
	if err != nil || replayedCar.ID() != car.ID() {
		t.Fatalf("obligation replay = %+v, %v", replayedCar, err)
	}
	if _, err := service.CreateObligation(ctx, "owner", "Car", amount, domainschedule.Monthly(), start, nil, carKey); !errors.Is(err, applicationledger.ErrIdempotencyConflict) {
		t.Fatalf("changed obligation replay error = %v", err)
	}
	occurrences, err := service.ExpandObligation(ctx, "owner", car.ID(), mustFinancialDate(t, "2026-08-01"), mustFinancialDate(t, "2026-09-30"))
	if err != nil || len(occurrences) != 4 {
		t.Fatalf("car occurrences = %d, %v", len(occurrences), err)
	}
	inactiveFrom := mustFinancialDate(t, "2026-09-01")
	archived, err := service.ArchiveObligation(ctx, "owner", car.ID(), inactiveFrom, testIdempotencyKey(t, "archive-car"))
	if err != nil || !archived.Status().IsArchived() {
		t.Fatalf("archive = %+v, %v", archived, err)
	}
	if replay, err := service.ArchiveObligation(ctx, "owner", car.ID(), inactiveFrom, testIdempotencyKey(t, "archive-car")); err != nil || !replay.Status().IsArchived() {
		t.Fatalf("archive replay = %+v, %v", replay, err)
	}
	if _, err := service.ArchiveObligation(ctx, "owner", car.ID(), mustFinancialDate(t, "2026-09-02"), testIdempotencyKey(t, "archive-car")); !errors.Is(err, applicationledger.ErrIdempotencyConflict) {
		t.Fatalf("changed archive date replay error = %v", err)
	}
	if createReplay, err := service.CreateObligation(ctx, "owner", "Car", amount, domainschedule.Biweekly(), start, nil, carKey); err != nil || createReplay.ID() != car.ID() || !createReplay.Status().IsActive() {
		t.Fatalf("create obligation replay after archive = %+v, %v", createReplay, err)
	}
	occurrences, _ = service.ExpandObligation(ctx, "owner", car.ID(), mustFinancialDate(t, "2026-08-01"), mustFinancialDate(t, "2026-09-30"))
	if len(occurrences) != 2 || occurrences[0].FinancialDate().String() != "2026-08-16" || occurrences[1].FinancialDate().String() != "2026-08-30" {
		t.Fatalf("archived occurrences = %+v", occurrences)
	}
	if _, err := service.GetObligation(ctx, "other", car.ID()); !errors.Is(err, applicationledger.ErrNotFound) {
		t.Fatalf("cross-owner obligation error = %v", err)
	}

	payrollAmount, _ := money.New(7_887_00, money.MXN())
	payrollKey := testIdempotencyKey(t, "payroll-flow")
	payroll, err := service.CreateManualScheduledFlow(ctx, "owner", payrollAmount, domainschedule.Inflow(), mustFinancialDate(t, "2026-08-15"), domainschedule.ManualExpectedIncomeSource(), domainschedule.ExactAmount(), domainschedule.ExactDate(), domainschedule.EligibleForPolicy(), payrollKey)
	if err != nil {
		t.Fatal(err)
	}
	replayedPayroll, err := service.CreateManualScheduledFlow(ctx, "owner", payrollAmount, domainschedule.Inflow(), mustFinancialDate(t, "2026-08-15"), domainschedule.ManualExpectedIncomeSource(), domainschedule.ExactAmount(), domainschedule.ExactDate(), domainschedule.EligibleForPolicy(), payrollKey)
	if err != nil || replayedPayroll.ID() != payroll.ID() {
		t.Fatalf("flow replay = %+v, %v", replayedPayroll, err)
	}
	if _, err := service.CreateManualScheduledFlow(ctx, "owner", payrollAmount, domainschedule.Inflow(), mustFinancialDate(t, "2026-08-16"), domainschedule.ManualExpectedIncomeSource(), domainschedule.ExactAmount(), domainschedule.ExactDate(), domainschedule.EligibleForPolicy(), payrollKey); !errors.Is(err, applicationledger.ErrIdempotencyConflict) {
		t.Fatalf("changed flow replay error = %v", err)
	}
	insuranceAmount, _ := money.New(1_000_00, money.MXN())
	insurance, err := service.CreateManualScheduledFlow(ctx, "owner", insuranceAmount, domainschedule.Outflow(), mustFinancialDate(t, "2026-09-01"), domainschedule.ManualOtherSource(), domainschedule.EstimatedAmount(), domainschedule.ExactDate(), domainschedule.EligibleForPolicy(), testIdempotencyKey(t, "insurance-flow"))
	if err != nil {
		t.Fatal(err)
	}
	flows, err := service.ListScheduledFlows(ctx, "owner")
	if err != nil || len(flows) != 2 || flows[0].ID() != payroll.ID() || flows[1].ID() != insurance.ID() {
		t.Fatalf("flow ordering = %+v, %v", flows, err)
	}
	cancelledFlow, err := service.CancelScheduledFlow(ctx, "owner", insurance.ID(), testIdempotencyKey(t, "cancel-insurance"))
	if err != nil || !cancelledFlow.Status().IsCancelled() {
		t.Fatalf("cancel flow = %+v, %v", cancelledFlow, err)
	}
	if replay, err := service.CancelScheduledFlow(ctx, "owner", insurance.ID(), testIdempotencyKey(t, "cancel-insurance")); err != nil || !replay.Status().IsCancelled() {
		t.Fatalf("cancel flow replay = %+v, %v", replay, err)
	}
	if createReplay, err := service.CreateManualScheduledFlow(ctx, "owner", insuranceAmount, domainschedule.Outflow(), mustFinancialDate(t, "2026-09-01"), domainschedule.ManualOtherSource(), domainschedule.EstimatedAmount(), domainschedule.ExactDate(), domainschedule.EligibleForPolicy(), testIdempotencyKey(t, "insurance-flow")); err != nil || createReplay.ID() != insurance.ID() || !createReplay.Status().IsScheduled() {
		t.Fatalf("create flow replay after cancellation = %+v, %v", createReplay, err)
	}

	original, _ := money.New(35_000_00, money.MXN())
	receivableKey := testIdempotencyKey(t, "uncertain-receivable")
	receivable, err := service.CreateReceivable(ctx, "owner", "Family business", original, nil, domainschedule.Uncertain(), domainschedule.ExactAmount(), nil, receivableKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, hasDate := receivable.ExpectedDate(); hasDate {
		t.Fatal("undated receivable acquired a date")
	}
	replayedReceivable, err := service.CreateReceivable(ctx, "owner", "Family business", original, nil, domainschedule.Uncertain(), domainschedule.ExactAmount(), nil, receivableKey)
	if err != nil || replayedReceivable.ID() != receivable.ID() {
		t.Fatalf("receivable replay = %+v, %v", replayedReceivable, err)
	}
	if _, err := service.CreateReceivable(ctx, "owner", "Family business", original, nil, domainschedule.Confirmed(), domainschedule.ExactAmount(), nil, receivableKey); !errors.Is(err, applicationledger.ErrIdempotencyConflict) {
		t.Fatalf("changed receivable replay error = %v", err)
	}
	if _, err := service.CreateReceivable(ctx, "owner", "Family business", original, nil, domainschedule.Uncertain(), domainschedule.EstimatedAmount(), nil, receivableKey); !errors.Is(err, applicationledger.ErrIdempotencyConflict) {
		t.Fatalf("changed receivable provenance replay error = %v", err)
	}
	partial, _ := money.New(5_000_00, money.MXN())
	collectionKey := testIdempotencyKey(t, "partial-collection")
	collection, err := service.RecordReceivableCollection(ctx, "owner", receivable.ID(), partial, "", collectionKey)
	if err != nil {
		t.Fatal(err)
	}
	replayedCollection, err := service.RecordReceivableCollection(ctx, "owner", receivable.ID(), partial, "", collectionKey)
	if err != nil || replayedCollection.ID() != collection.ID() || replayedCollection.CollectedAfter().MinorUnits() != 5_000_00 {
		t.Fatalf("collection replay = %+v, %v", replayedCollection, err)
	}
	one, _ := money.New(1, money.MXN())
	if _, err := service.RecordReceivableCollection(ctx, "owner", receivable.ID(), one, "", collectionKey); !errors.Is(err, applicationledger.ErrIdempotencyConflict) {
		t.Fatalf("changed collection replay error = %v", err)
	}
	current, err := service.GetReceivable(ctx, "owner", receivable.ID())
	if err != nil || current.CollectedAmount().MinorUnits() != 5_000_00 || current.Status() != domainschedule.PartialReceivable() {
		t.Fatalf("partial receivable = %+v, %v", current, err)
	}
	if createReplay, err := service.CreateReceivable(ctx, "owner", "Family business", original, nil, domainschedule.Uncertain(), domainschedule.ExactAmount(), nil, receivableKey); err != nil || createReplay.ID() != receivable.ID() || createReplay.CollectedAmount().MinorUnits() != 0 || createReplay.Status() != domainschedule.OpenReceivable() {
		t.Fatalf("create receivable replay after collection = %+v, %v", createReplay, err)
	}
	tooMuch, _ := money.New(31_000_00, money.MXN())
	if _, err := service.RecordReceivableCollection(ctx, "owner", receivable.ID(), tooMuch, "", testIdempotencyKey(t, "over-collection")); !errors.Is(err, domainschedule.ErrCollectionExceedsAmount) {
		t.Fatalf("over-collection error = %v", err)
	}
	secondOriginal, _ := money.New(100_00, money.MXN())
	second, _ := service.CreateReceivable(ctx, "owner", "Reimbursement", secondOriginal, nil, domainschedule.Confirmed(), domainschedule.ExactAmount(), nil, testIdempotencyKey(t, "second-receivable"))
	cancelledReceivable, err := service.CancelReceivable(ctx, "owner", second.ID(), testIdempotencyKey(t, "cancel-receivable"))
	if err != nil || cancelledReceivable.Status() != domainschedule.CancelledReceivable() {
		t.Fatalf("cancel receivable = %+v, %v", cancelledReceivable, err)
	}
	if replay, err := service.CancelReceivable(ctx, "owner", second.ID(), testIdempotencyKey(t, "cancel-receivable")); err != nil || replay.Status() != domainschedule.CancelledReceivable() || replay.ID() != second.ID() {
		t.Fatalf("cancel receivable replay = %+v, %v", replay, err)
	}
	if loaded, err := service.GetReceivable(ctx, "owner", second.ID()); err != nil || loaded.Status() != domainschedule.CancelledReceivable() {
		t.Fatalf("cancelled receivable read = %+v, %v", loaded, err)
	}
	third, _ := service.CreateReceivable(ctx, "owner", "Another reimbursement", secondOriginal, nil, domainschedule.Confirmed(), domainschedule.ExactAmount(), nil, testIdempotencyKey(t, "third-receivable"))
	if _, err := service.CancelReceivable(ctx, "owner", third.ID(), testIdempotencyKey(t, "cancel-receivable")); !errors.Is(err, applicationledger.ErrIdempotencyConflict) {
		t.Fatalf("cancel key reused for different receivable error = %v", err)
	}
	if loaded, err := service.GetReceivable(ctx, "owner", third.ID()); err != nil || loaded.Status() != domainschedule.OpenReceivable() {
		t.Fatalf("conflicting cancellation mutated receivable = %+v, %v", loaded, err)
	}
	if _, err := service.RecordReceivableCollection(ctx, "owner", second.ID(), secondOriginal, "", testIdempotencyKey(t, "collection-after-cancel")); !errors.Is(err, domainschedule.ErrReceivableFinal) {
		t.Fatalf("collection after cancellation error = %v", err)
	}
}

func TestScheduleListsUseStableIDTieBreakers(t *testing.T) {
	pool := newIsolatedTestDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	createTestOwner(t, pool, "owner", now)
	ids := []string{"z-obligation", "a-obligation", "z-flow", "a-flow", "z-receivable", "a-receivable"}
	next := 0
	service, err := applicationschedule.NewService(NewScheduleRepository(pool), applicationschedule.ServiceOptions{
		Clock: func() time.Time { return now },
		IDGenerator: func() (string, error) {
			id := ids[next]
			next++
			return id, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	amount, _ := money.New(100, money.MXN())
	date := mustFinancialDate(t, "2026-08-15")
	for _, value := range []struct{ name, key string }{{"Z obligation", "ordered-obligation-z"}, {"A obligation", "ordered-obligation-a"}} {
		if _, err := service.CreateObligation(ctx, "owner", value.name, amount, domainschedule.OneTime(), date, nil, testIdempotencyKey(t, value.key)); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"z", "a"} {
		if _, err := service.CreateManualScheduledFlow(ctx, "owner", amount, domainschedule.Outflow(), date, domainschedule.ManualOtherSource(), domainschedule.ExactAmount(), domainschedule.ExactDate(), domainschedule.EligibleForPolicy(), testIdempotencyKey(t, "ordered-flow-"+name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, value := range []struct{ name, key string }{{"Z receivable", "ordered-receivable-z"}, {"A receivable", "ordered-receivable-a"}} {
		if _, err := service.CreateReceivable(ctx, "owner", value.name, amount, nil, domainschedule.Uncertain(), domainschedule.ExactAmount(), nil, testIdempotencyKey(t, value.key)); err != nil {
			t.Fatal(err)
		}
	}

	obligations, _ := service.ListObligations(ctx, "owner")
	flows, _ := service.ListScheduledFlows(ctx, "owner")
	receivables, _ := service.ListReceivables(ctx, "owner")
	if len(obligations) != 2 || obligations[0].ID() != "a-obligation" || obligations[1].ID() != "z-obligation" {
		t.Fatalf("obligation ordering = %+v", obligations)
	}
	if len(flows) != 2 || flows[0].ID() != "a-flow" || flows[1].ID() != "z-flow" {
		t.Fatalf("flow ordering = %+v", flows)
	}
	if len(receivables) != 2 || receivables[0].ID() != "a-receivable" || receivables[1].ID() != "z-receivable" {
		t.Fatalf("receivable ordering = %+v", receivables)
	}
}

func TestReceivableCollectionCanLinkCompatibleLedgerInflow(t *testing.T) {
	pool := newIsolatedTestDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	createTestOwner(t, pool, "owner", now)
	ids := sequentialIDs()
	ledgerService, _ := applicationledger.NewService(NewLedgerRepository(pool), applicationledger.ServiceOptions{Clock: func() time.Time { return now }, IDGenerator: ids})
	scheduleService, _ := applicationschedule.NewService(NewScheduleRepository(pool), applicationschedule.ServiceOptions{Clock: func() time.Time { return now }, IDGenerator: ids})
	bank := createTestAccount(t, ledgerService, "owner", "Bank", account.Bank())
	amount, _ := money.New(500_00, money.MXN())
	date := mustFinancialDate(t, "2026-08-10")
	posted, err := ledgerService.PostTransaction(ctx, "owner", bank.ID(), domainledger.AssetInflow(), amount, date, "Receivable collection", testIdempotencyKey(t, "ledger-inflow"))
	if err != nil {
		t.Fatal(err)
	}
	receivable, _ := scheduleService.CreateReceivable(ctx, "owner", "Reimbursement", amount, &date, domainschedule.Confirmed(), domainschedule.ExactAmount(), exactDateProvenancePointer(), testIdempotencyKey(t, "linked-receivable"))
	collection, err := scheduleService.RecordReceivableCollection(ctx, "owner", receivable.ID(), amount, posted.ID(), testIdempotencyKey(t, "linked-collection"))
	if err != nil || collection.LedgerTransactionID() != posted.ID() || collection.ResultingStatus() != domainschedule.CollectedReceivable() {
		t.Fatalf("linked collection = %+v, error = %v", collection, err)
	}

	reuseReceivable, _ := scheduleService.CreateReceivable(ctx, "owner", "Second reimbursement", amount, &date, domainschedule.Confirmed(), domainschedule.ExactAmount(), exactDateProvenancePointer(), testIdempotencyKey(t, "reuse-receivable"))
	if _, err := scheduleService.RecordReceivableCollection(ctx, "owner", reuseReceivable.ID(), amount, posted.ID(), testIdempotencyKey(t, "reuse-ledger-link")); !errors.Is(err, applicationledger.ErrConflict) {
		t.Fatalf("reused ledger transaction error = %v", err)
	}
	if current, err := scheduleService.GetReceivable(ctx, "owner", reuseReceivable.ID()); err != nil || current.CollectedAmount().MinorUnits() != 0 || current.Status() != domainschedule.OpenReceivable() {
		t.Fatalf("reuse receivable after rollback = %+v, %v", current, err)
	}

	wrongAmount, _ := money.New(400_00, money.MXN())
	wrongAmountTransaction, err := ledgerService.PostTransaction(ctx, "owner", bank.ID(), domainledger.AssetInflow(), wrongAmount, date, "Wrong amount", testIdempotencyKey(t, "wrong-amount-inflow"))
	if err != nil {
		t.Fatal(err)
	}
	amountReceivable, _ := scheduleService.CreateReceivable(ctx, "owner", "Amount mismatch", amount, &date, domainschedule.Confirmed(), domainschedule.ExactAmount(), exactDateProvenancePointer(), testIdempotencyKey(t, "amount-mismatch-receivable"))
	if _, err := scheduleService.RecordReceivableCollection(ctx, "owner", amountReceivable.ID(), amount, wrongAmountTransaction.ID(), testIdempotencyKey(t, "amount-mismatch-link")); err == nil {
		t.Fatal("amount-mismatched ledger link succeeded")
	}

	wrongEffectTransaction, err := ledgerService.PostTransaction(ctx, "owner", bank.ID(), domainledger.AssetOutflow(), amount, date, "Wrong effect", testIdempotencyKey(t, "wrong-effect-outflow"))
	if err != nil {
		t.Fatal(err)
	}
	effectReceivable, _ := scheduleService.CreateReceivable(ctx, "owner", "Effect mismatch", amount, &date, domainschedule.Confirmed(), domainschedule.ExactAmount(), exactDateProvenancePointer(), testIdempotencyKey(t, "effect-mismatch-receivable"))
	if _, err := scheduleService.RecordReceivableCollection(ctx, "owner", effectReceivable.ID(), amount, wrongEffectTransaction.ID(), testIdempotencyKey(t, "effect-mismatch-link")); err == nil {
		t.Fatal("effect-mismatched ledger link succeeded")
	}

	if _, err := pool.Exec(ctx, `ALTER TABLE owners DROP CONSTRAINT owners_singleton_key_key`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO owners (id, singleton_key, email, password_hash, created_at, updated_at)
		VALUES ('other-owner', TRUE, 'other-owner@example.com', 'encoded-password-hash', $1, $1)`, now); err != nil {
		t.Fatal(err)
	}
	otherBank := createTestAccount(t, ledgerService, "other-owner", "Other bank", account.Bank())
	otherTransaction, err := ledgerService.PostTransaction(ctx, "other-owner", otherBank.ID(), domainledger.AssetInflow(), amount, date, "Other owner inflow", testIdempotencyKey(t, "other-owner-inflow"))
	if err != nil {
		t.Fatal(err)
	}
	crossOwnerReceivable, _ := scheduleService.CreateReceivable(ctx, "owner", "Cross-owner link", amount, &date, domainschedule.Confirmed(), domainschedule.ExactAmount(), exactDateProvenancePointer(), testIdempotencyKey(t, "cross-owner-receivable"))
	if _, err := scheduleService.RecordReceivableCollection(ctx, "owner", crossOwnerReceivable.ID(), amount, otherTransaction.ID(), testIdempotencyKey(t, "cross-owner-link")); err == nil {
		t.Fatal("cross-owner ledger link succeeded")
	}
}

func TestConcurrentCollectionRetryDoesNotDoubleCollect(t *testing.T) {
	pool := newIsolatedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	createTestOwner(t, pool, "owner", now)
	service, _ := applicationschedule.NewService(NewScheduleRepository(pool), applicationschedule.ServiceOptions{Clock: func() time.Time { return now }, IDGenerator: sequentialIDs()})
	original, _ := money.New(1_000_00, money.MXN())
	receivable, _ := service.CreateReceivable(ctx, "owner", "Debt", original, nil, domainschedule.Expected(), domainschedule.ExactAmount(), nil, testIdempotencyKey(t, "concurrent-receivable"))
	partial, _ := money.New(250_00, money.MXN())
	key := testIdempotencyKey(t, "concurrent-collection")
	results := make(chan domainschedule.ReceivableCollection, 2)
	errorsFound := make(chan error, 2)
	var wait sync.WaitGroup
	for index := 0; index < 2; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, err := service.RecordReceivableCollection(ctx, "owner", receivable.ID(), partial, "", key)
			if err != nil {
				errorsFound <- err
				return
			}
			results <- result
		}()
	}
	wait.Wait()
	close(results)
	close(errorsFound)
	for err := range errorsFound {
		t.Errorf("concurrent collection error = %v", err)
	}
	ids := make(map[string]bool)
	for result := range results {
		ids[result.ID()] = true
	}
	if len(ids) != 1 {
		t.Fatalf("collection IDs = %v", ids)
	}
	current, err := service.GetReceivable(ctx, "owner", receivable.ID())
	if err != nil || current.CollectedAmount().MinorUnits() != partial.MinorUnits() {
		t.Fatalf("collected amount = %d, %v", current.CollectedAmount().MinorUnits(), err)
	}
	var rows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM receivable_collections WHERE receivable_id = $1`, receivable.ID()).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("collection rows = %d, %v", rows, err)
	}
}

func TestConcurrentDifferentKeyCollectionsCannotOverCollect(t *testing.T) {
	pool := newIsolatedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	createTestOwner(t, pool, "owner", now)
	service, _ := applicationschedule.NewService(NewScheduleRepository(pool), applicationschedule.ServiceOptions{Clock: func() time.Time { return now }, IDGenerator: sequentialIDs()})
	original, _ := money.New(10_000_00, money.MXN())
	receivable, err := service.CreateReceivable(ctx, "owner", "Concurrent debt", original, nil, domainschedule.Expected(), domainschedule.ExactAmount(), nil, testIdempotencyKey(t, "different-key-receivable"))
	if err != nil {
		t.Fatal(err)
	}
	partial, _ := money.New(7_000_00, money.MXN())
	errorsFound := make(chan error, 2)
	var wait sync.WaitGroup
	keys := []applicationledger.IdempotencyKey{
		testIdempotencyKey(t, "different-collection-a"),
		testIdempotencyKey(t, "different-collection-b"),
	}
	for _, key := range keys {
		key := key
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := service.RecordReceivableCollection(ctx, "owner", receivable.ID(), partial, "", key)
			errorsFound <- err
		}()
	}
	wait.Wait()
	close(errorsFound)
	successes := 0
	rejections := 0
	for err := range errorsFound {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, domainschedule.ErrCollectionExceedsAmount):
			rejections++
		default:
			t.Fatalf("unexpected concurrent collection error = %v", err)
		}
	}
	if successes != 1 || rejections != 1 {
		t.Fatalf("concurrent results: successes=%d rejections=%d", successes, rejections)
	}
	current, err := service.GetReceivable(ctx, "owner", receivable.ID())
	if err != nil || current.CollectedAmount().MinorUnits() != partial.MinorUnits() || current.Status() != domainschedule.PartialReceivable() {
		t.Fatalf("current receivable = %+v, error = %v", current, err)
	}
	var collectionTotal int64
	if err := pool.QueryRow(ctx, `SELECT COALESCE(sum(amount_minor), 0) FROM receivable_collections WHERE owner_id = $1 AND receivable_id = $2`, "owner", receivable.ID()).Scan(&collectionTotal); err != nil {
		t.Fatal(err)
	}
	if collectionTotal != current.CollectedAmount().MinorUnits() || collectionTotal > original.MinorUnits() {
		t.Fatalf("collection total = %d, receivable collected = %d, original = %d", collectionTotal, current.CollectedAmount().MinorUnits(), original.MinorUnits())
	}
}

func TestScheduleDatabaseIntegrity(t *testing.T) {
	pool := newIsolatedTestDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	createTestOwner(t, pool, "owner", now)
	service, _ := applicationschedule.NewService(NewScheduleRepository(pool), applicationschedule.ServiceOptions{Clock: func() time.Time { return now }, IDGenerator: sequentialIDs()})
	amount, _ := money.New(100_00, money.MXN())
	date := mustFinancialDate(t, "2026-08-15")
	obligation, _ := service.CreateObligation(ctx, "owner", "Payment", amount, domainschedule.OneTime(), date, nil, testIdempotencyKey(t, "integrity-obligation"))
	flow, _ := service.CreateManualScheduledFlow(ctx, "owner", amount, domainschedule.Outflow(), date, domainschedule.ManualOtherSource(), domainschedule.ExactAmount(), domainschedule.ExactDate(), domainschedule.EligibleForPolicy(), testIdempotencyKey(t, "integrity-flow"))
	receivable, _ := service.CreateReceivable(ctx, "owner", "Receivable", amount, nil, domainschedule.Uncertain(), domainschedule.ExactAmount(), nil, testIdempotencyKey(t, "integrity-receivable"))
	partial, _ := money.New(10_00, money.MXN())
	collection, _ := service.RecordReceivableCollection(ctx, "owner", receivable.ID(), partial, "", testIdempotencyKey(t, "integrity-collection"))
	derivedOccurrence, _ := domainschedule.NewObligationOccurrence("derived-occurrence", obligation, date)
	if _, err := NewScheduleRepository(pool).CreateScheduledFlow(ctx, derivedOccurrence, applicationledger.MutationIdentity{}); !errors.Is(err, domainschedule.ErrInvalidScheduledFlow) {
		t.Fatalf("persist derived obligation occurrence error = %v", err)
	}
	receivableFlow, _ := domainschedule.RestoreScheduledCashFlow(
		"receivable-flow", "owner", domainschedule.ReceivableSource(), receivable.ID(), amount, domainschedule.Inflow(), date,
		domainschedule.ScheduledFlow(), domainschedule.ExactAmount(), domainschedule.ExactDate(), domainschedule.EligibleForPolicy(), "", now, now,
	)
	if _, err := NewScheduleRepository(pool).CreateScheduledFlow(ctx, receivableFlow, applicationledger.MutationIdentity{}); !errors.Is(err, domainschedule.ErrInvalidScheduledFlow) {
		t.Fatalf("persist receivable flow error = %v", err)
	}

	for _, statement := range []string{
		`UPDATE obligations SET amount_minor = amount_minor + 1 WHERE id = '` + obligation.ID() + `'`,
		`DELETE FROM obligations WHERE id = '` + obligation.ID() + `'`,
		`UPDATE scheduled_cash_flows SET amount_minor = amount_minor + 1 WHERE id = '` + flow.ID() + `'`,
		`DELETE FROM scheduled_cash_flows WHERE id = '` + flow.ID() + `'`,
		`UPDATE receivables SET original_amount_minor = original_amount_minor + 1 WHERE id = '` + receivable.ID() + `'`,
		`UPDATE receivables SET amount_provenance = 'estimated' WHERE id = '` + receivable.ID() + `'`,
		`DELETE FROM receivables WHERE id = '` + receivable.ID() + `'`,
		`UPDATE receivable_collections SET amount_minor = amount_minor + 1 WHERE id = '` + collection.ID() + `'`,
		`DELETE FROM receivable_collections WHERE id = '` + collection.ID() + `'`,
	} {
		if _, err := pool.Exec(ctx, statement); !hasPostgresCode(err, "55000") {
			t.Fatalf("immutable statement %q error = %v", statement, err)
		}
	}

	_, err := pool.Exec(ctx, `
		INSERT INTO obligations (id, owner_id, display_name, amount_minor, currency, direction, recurrence, start_date, status, created_at, updated_at)
		VALUES ('wrong-owner-obligation', 'missing-owner', 'Wrong', 1, 'MXN', 'outflow', 'one_time', '2026-08-15', 'active', $1, $1)`, now)
	if !hasPostgresCode(err, "23503") {
		t.Fatalf("cross-owner obligation error = %v", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO receivables
			(id, owner_id, display_name, original_amount_minor, collected_amount_minor, currency, status, certainty, created_at, updated_at)
		VALUES ('missing-provenance', 'owner', 'Missing provenance', 1, 0, 'MXN', 'open', 'confirmed', $1, $1)`, now)
	if !hasPostgresCode(err, "23502") {
		t.Fatalf("missing receivable provenance error = %v", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO receivables
			(id, owner_id, display_name, original_amount_minor, collected_amount_minor, currency, status,
			 expected_date, certainty, amount_provenance, date_provenance, created_at, updated_at)
		VALUES ('undated-with-date-provenance', 'owner', 'Invalid provenance', 1, 0, 'MXN', 'open',
		        NULL, 'uncertain', 'exact', 'exact', $1, $1)`, now)
	if !hasPostgresCode(err, "23514") {
		t.Fatalf("undated receivable date provenance error = %v", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO scheduled_cash_flows
			(id, owner_id, source_kind, source_id, amount_minor, currency, direction, financial_date, status, amount_provenance, date_provenance, inclusion_eligibility, created_at, updated_at)
		VALUES ('wrong-source-flow', 'owner', 'obligation', 'missing-obligation', 1, 'MXN', 'outflow', '2026-08-15', 'scheduled', 'exact', 'exact', 'eligible', $1, $1)`, now)
	if !hasPostgresCode(err, "23503") {
		t.Fatalf("cross-owner source error = %v", err)
	}
	for _, statement := range []string{
		`INSERT INTO scheduled_cash_flows
			(id, owner_id, source_kind, source_id, amount_minor, currency, direction, financial_date, status, amount_provenance, date_provenance, inclusion_eligibility, created_at, updated_at)
		 VALUES ('raw-obligation-flow', 'owner', 'obligation', '` + obligation.ID() + `', 1, 'MXN', 'outflow', '2026-08-15', 'scheduled', 'exact', 'exact', 'eligible', $1, $1)`,
		`INSERT INTO scheduled_cash_flows
			(id, owner_id, source_kind, source_id, amount_minor, currency, direction, financial_date, status, amount_provenance, date_provenance, inclusion_eligibility, created_at, updated_at)
		 VALUES ('raw-receivable-flow', 'owner', 'receivable', '` + receivable.ID() + `', 1, 'MXN', 'inflow', '2026-08-15', 'scheduled', 'exact', 'exact', 'eligible', $1, $1)`,
	} {
		if _, err := pool.Exec(ctx, statement, now); !hasPostgresCode(err, "23514") {
			t.Fatalf("forbidden persisted source statement error = %v", err)
		}
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO obligations (id, owner_id, display_name, amount_minor, currency, direction, recurrence, start_date, status, created_at, updated_at)
		VALUES ('initially-archived', 'owner', 'Wrong state', 1, 'MXN', 'outflow', 'one_time', '2026-08-15', 'archived', $1, $1)`, now)
	if !hasPostgresCode(err, "23514") {
		t.Fatalf("initial archived obligation error = %v", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO obligations (id, owner_id, display_name, amount_minor, currency, direction, recurrence, start_date, inactive_from, status, created_at, updated_at)
		VALUES ('active-with-inactive-date', 'owner', 'Wrong state', 1, 'MXN', 'outflow', 'one_time', '2026-08-15', '2026-08-20', 'active', $1, $1)`, now)
	if !hasPostgresCode(err, "23514") {
		t.Fatalf("active obligation with inactive_from error = %v", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO scheduled_cash_flows
			(id, owner_id, source_kind, source_id, amount_minor, currency, direction, financial_date, status, amount_provenance, date_provenance, inclusion_eligibility, created_at, updated_at)
		VALUES ('initially-cancelled', 'owner', 'manual_other', 'initially-cancelled', 1, 'MXN', 'outflow', '2026-08-15', 'cancelled', 'exact', 'exact', 'eligible', $1, $1)`, now)
	if !hasPostgresCode(err, "23514") {
		t.Fatalf("initial cancelled flow error = %v", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO receivables
			(id, owner_id, display_name, original_amount_minor, collected_amount_minor, currency, status, certainty, created_at, updated_at)
		VALUES ('initially-cancelled-receivable', 'owner', 'Wrong state', 10, 0, 'MXN', 'cancelled', 'uncertain', $1, $1)`, now)
	if !hasPostgresCode(err, "23514") {
		t.Fatalf("initial cancelled receivable error = %v", err)
	}
	_, err = pool.Exec(ctx, `UPDATE scheduled_cash_flows SET status = 'settled', settlement_transaction_id = 'missing-transaction', updated_at = $2 WHERE id = $1`, flow.ID(), now.Add(time.Minute))
	if !hasPostgresCode(err, "23503") {
		t.Fatalf("invalid settlement link error = %v", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO receivable_collections
			(id, owner_id, receivable_id, amount_minor, currency, collected_before_minor, collected_after_minor, outstanding_after_minor, resulting_status, created_at)
		VALUES ('wrong-currency-collection', 'owner', $1, 1, 'USD', 1000, 1001, 8999, 'partially_collected', $2)`, receivable.ID(), now)
	if !hasPostgresCode(err, "23514") {
		t.Fatalf("collection currency mismatch error = %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, `UPDATE receivables SET collected_amount_minor = collected_amount_minor + 1, status = 'partially_collected', updated_at = updated_at WHERE id = $1`, receivable.ID())
	if err == nil {
		err = tx.Commit(ctx)
	} else {
		_ = tx.Rollback(ctx)
	}
	if !hasPostgresCode(err, "23514") {
		t.Fatalf("unbacked collection update error = %v", err)
	}
}

func mustFinancialDate(t *testing.T, value string) financialdate.Date {
	t.Helper()
	date, err := financialdate.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	return date
}
