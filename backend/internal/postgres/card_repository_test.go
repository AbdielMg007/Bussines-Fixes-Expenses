package postgres

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	appcard "runway/backend/internal/card"
	"runway/backend/internal/domain/account"
	domaincard "runway/backend/internal/domain/card"
	"runway/backend/internal/domain/financialdate"
	domainledger "runway/backend/internal/domain/ledger"
	"runway/backend/internal/domain/money"
	applicationledger "runway/backend/internal/ledger"
)

func TestCardRepositoryStatementAuthorityIdempotencyAndIntentLifecycle(t *testing.T) {
	pool := newIsolatedTestDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	createTestOwner(t, pool, "owner", now)
	ledgerService, err := applicationledger.NewService(NewLedgerRepository(pool), applicationledger.ServiceOptions{Clock: func() time.Time { return now }, IDGenerator: sequentialIDs()})
	if err != nil {
		t.Fatal(err)
	}
	creditCard := createTestAccount(t, ledgerService, "owner", "Card", account.CreditCard())
	repository := NewCardRepository(pool)

	estimateInput := cardStatementInput(t, "owner", creditCard.ID(), domaincard.Estimated, 500_000, "2026-09-09", "estimate-key")
	estimate, err := repository.RegisterStatement(ctx, "owner", estimateInput, "estimate", now)
	if err != nil {
		t.Fatalf("register estimate: %v", err)
	}
	if estimate.Minimum != nil || estimate.AvoidInterest != nil {
		t.Fatal("missing statement source facts became amounts")
	}
	replay, err := repository.RegisterStatement(ctx, "owner", estimateInput, "retry-id-must-not-matter", now)
	if err != nil || replay.ID != estimate.ID || replay.Revision != 1 {
		t.Fatalf("statement replay = %+v, error = %v", replay, err)
	}
	conflict := cardStatementInput(t, "owner", creditCard.ID(), domaincard.Estimated, 500_001, "2026-09-09", "estimate-key")
	if _, err := repository.RegisterStatement(ctx, "owner", conflict, "conflict", now); !errors.Is(err, applicationledger.ErrIdempotencyConflict) {
		t.Fatalf("same key different statement error = %v", err)
	}

	amount, _ := money.New(280_000, money.MXN())
	planned := cardTestDate(t, "2026-09-09")
	intent, err := repository.ReplaceIntent(ctx, "owner", estimate.CycleID, amount, planned, "intent", now)
	if err != nil || intent.Status != domaincard.IntentActive || intent.Version != 1 {
		t.Fatalf("create intent = %+v, error = %v", intent, err)
	}
	stable, err := repository.ReplaceIntent(ctx, "owner", estimate.CycleID, amount, planned, "ignored", now.Add(time.Minute))
	if err != nil || stable.ID != intent.ID || stable.Version != 1 {
		t.Fatalf("identical intent PUT = %+v, error = %v", stable, err)
	}

	issuedInput := cardStatementInput(t, "owner", creditCard.ID(), domaincard.Issued, 450_000, "2026-09-09", "issued-key")
	issued, err := repository.RegisterStatement(ctx, "owner", issuedInput, "issued", now.Add(time.Minute))
	if err != nil || issued.CycleID != estimate.CycleID || issued.Revision != 2 {
		t.Fatalf("estimate to issued = %+v, error = %v", issued, err)
	}
	activeIntent, err := repository.GetIntent(ctx, "owner", estimate.CycleID)
	if err != nil || activeIntent.Status != domaincard.IntentActive || activeIntent.Amount.MinorUnits() != 280_000 {
		t.Fatalf("intent after valid correction = %+v, error = %v", activeIntent, err)
	}

	correctedInput := cardStatementInput(t, "owner", creditCard.ID(), domaincard.Issued, 200_000, "2026-09-09", "corrected-key")
	corrected, err := repository.RegisterStatement(ctx, "owner", correctedInput, "corrected", now.Add(2*time.Minute))
	if err != nil || corrected.Revision != 3 || corrected.CycleID != estimate.CycleID {
		t.Fatalf("corrected issued = %+v, error = %v", corrected, err)
	}
	needsReview, err := repository.GetIntent(ctx, "owner", estimate.CycleID)
	if err != nil || needsReview.Status != domaincard.IntentNeedsReview || needsReview.Amount.MinorUnits() != 280_000 || needsReview.Planned.String() != "2026-09-09" {
		t.Fatalf("intent after invalidating correction = %+v, error = %v", needsReview, err)
	}
	if _, err := repository.RegisterStatement(ctx, "owner", cardStatementInput(t, "owner", creditCard.ID(), domaincard.Estimated, 100_000, "2026-09-09", "forbidden-estimate"), "forbidden-estimate", now.Add(3*time.Minute)); !errors.Is(err, domaincard.ErrIssuedCannotBeReplacedByEstimate) {
		t.Fatalf("issued to estimate error = %v", err)
	}

	replacementAmount, _ := money.New(200_000, money.MXN())
	replaced, err := repository.ReplaceIntent(ctx, "owner", estimate.CycleID, replacementAmount, planned, "replacement", now.Add(3*time.Minute))
	if err != nil || replaced.Status != domaincard.IntentActive || replaced.Version != 2 {
		t.Fatalf("replace needs-review intent = %+v, error = %v", replaced, err)
	}
	cancelled, err := repository.CancelIntent(ctx, "owner", estimate.CycleID, now.Add(4*time.Minute))
	if err != nil || cancelled.Status != domaincard.IntentCancelled || cancelled.Version != 3 {
		t.Fatalf("cancel intent = %+v, error = %v", cancelled, err)
	}
	if _, err := repository.ReplaceIntent(ctx, "owner", estimate.CycleID, replacementAmount, planned, "must-not-revive", now.Add(5*time.Minute)); !errors.Is(err, domaincard.ErrPaymentIntentCancelled) {
		t.Fatalf("cancelled intent replacement error = %v", err)
	}
	unchanged, err := repository.GetIntent(ctx, "owner", estimate.CycleID)
	if err != nil || unchanged.Status != domaincard.IntentCancelled || unchanged.Version != 3 || unchanged.Amount.MinorUnits() != 200_000 {
		t.Fatalf("cancelled intent changed = %+v, error = %v", unchanged, err)
	}

	statements, err := repository.ListStatements(ctx, "owner", creditCard.ID())
	if err != nil || len(statements) != 3 || statements[0].ID != estimate.ID || statements[1].ID != issued.ID || statements[2].ID != corrected.ID || statements[2].SupersededAt != nil {
		t.Fatalf("statement history = %+v, error = %v", statements, err)
	}
	if statements[0].SupersededBy != issued.ID || statements[1].SupersededBy != corrected.ID {
		t.Fatalf("supersession history = %+v", statements)
	}
	var activeCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM credit_card_statements WHERE cycle_id=$1 AND superseded_at IS NULL`, estimate.CycleID).Scan(&activeCount); err != nil || activeCount != 1 {
		t.Fatalf("active statement count = %d, error = %v", activeCount, err)
	}
}

func TestCardRepositorySerializesStatementAndIntentRaces(t *testing.T) {
	pool := newIsolatedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	createTestOwner(t, pool, "owner", now)
	ledgerService, _ := applicationledger.NewService(NewLedgerRepository(pool), applicationledger.ServiceOptions{Clock: func() time.Time { return now }, IDGenerator: sequentialIDs()})
	creditCard := createTestAccount(t, ledgerService, "owner", "Card", account.CreditCard())
	repository := NewCardRepository(pool)

	var first sync.WaitGroup
	first.Add(2)
	firstErrors := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func(index int) {
			defer first.Done()
			input := cardStatementInput(t, "owner", creditCard.ID(), domaincard.Issued, int64(500_000-index), "2026-09-09", fmt.Sprintf("first-%d", index))
			_, err := repository.RegisterStatement(ctx, "owner", input, fmt.Sprintf("first-statement-%d", index), now)
			firstErrors <- err
		}(i)
	}
	first.Wait()
	close(firstErrors)
	for err := range firstErrors {
		if err != nil {
			t.Fatalf("concurrent first statement: %v", err)
		}
	}
	var cycleID string
	if err := pool.QueryRow(ctx, `SELECT id FROM credit_card_cycles WHERE owner_id='owner' AND account_id=$1`, creditCard.ID()).Scan(&cycleID); err != nil {
		t.Fatal(err)
	}
	assertCardStatementState(t, ctx, pool, cycleID, 2, 1)

	var revisions sync.WaitGroup
	revisions.Add(2)
	revisionErrors := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func(index int) {
			defer revisions.Done()
			input := cardStatementInput(t, "owner", creditCard.ID(), domaincard.Issued, int64(450_000-index), "2026-09-09", fmt.Sprintf("revision-%d", index))
			_, err := repository.RegisterStatement(ctx, "owner", input, fmt.Sprintf("revision-statement-%d", index), now.Add(time.Minute))
			revisionErrors <- err
		}(i)
	}
	revisions.Wait()
	close(revisionErrors)
	for err := range revisionErrors {
		if err != nil {
			t.Fatalf("concurrent issued revision: %v", err)
		}
	}
	assertCardStatementState(t, ctx, pool, cycleID, 4, 1)

	amountA, _ := money.New(1_000, money.MXN())
	amountB, _ := money.New(2_000, money.MXN())
	date := cardTestDate(t, "2026-09-09")
	var intents sync.WaitGroup
	intents.Add(2)
	intentErrors := make(chan error, 2)
	for index, amount := range []money.Money{amountA, amountB} {
		go func(index int, amount money.Money) {
			defer intents.Done()
			_, err := repository.ReplaceIntent(ctx, "owner", cycleID, amount, date, fmt.Sprintf("intent-%d", index), now.Add(2*time.Minute))
			intentErrors <- err
		}(index, amount)
	}
	intents.Wait()
	close(intentErrors)
	for err := range intentErrors {
		if err != nil {
			t.Fatalf("concurrent intent replacement: %v", err)
		}
	}
	intent, err := repository.GetIntent(ctx, "owner", cycleID)
	if err != nil || intent.Version != 2 || intent.Status != domaincard.IntentActive {
		t.Fatalf("serialized intent = %+v, error = %v", intent, err)
	}

	var sameKey sync.WaitGroup
	sameKey.Add(2)
	sameKeyIDs := make(chan string, 2)
	sameKeyErrors := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func(index int) {
			defer sameKey.Done()
			input := cardStatementInput(t, "owner", creditCard.ID(), domaincard.Issued, 300_000, "2026-10-09", "same-key")
			statement, err := repository.RegisterStatement(ctx, "owner", input, fmt.Sprintf("same-key-%d", index), now.Add(3*time.Minute))
			if err != nil {
				sameKeyErrors <- err
				return
			}
			sameKeyIDs <- statement.ID
		}(i)
	}
	sameKey.Wait()
	close(sameKeyIDs)
	close(sameKeyErrors)
	for err := range sameKeyErrors {
		t.Fatal(err)
	}
	ids := []string{}
	for id := range sameKeyIDs {
		ids = append(ids, id)
	}
	if len(ids) != 2 || ids[0] != ids[1] {
		t.Fatalf("same-key statements = %v", ids)
	}

	// A correction and an invalid intent replacement serialize through the cycle and active statement locks.
	var race sync.WaitGroup
	race.Add(2)
	raceErrors := make(chan error, 2)
	go func() {
		defer race.Done()
		input := cardStatementInput(t, "owner", creditCard.ID(), domaincard.Issued, 500, "2026-09-09", "correction-race")
		_, err := repository.RegisterStatement(ctx, "owner", input, "correction-race", now.Add(4*time.Minute))
		raceErrors <- err
	}()
	go func() {
		defer race.Done()
		amount, _ := money.New(1_000, money.MXN())
		_, err := repository.ReplaceIntent(ctx, "owner", cycleID, amount, date, "intent-race", now.Add(4*time.Minute))
		raceErrors <- err
	}()
	race.Wait()
	close(raceErrors)
	for err := range raceErrors {
		if err != nil && !errors.Is(err, domaincard.ErrInvalidPaymentIntent) {
			t.Fatalf("statement/intent race error = %v", err)
		}
	}
	intent, err = repository.GetIntent(ctx, "owner", cycleID)
	if err != nil || intent.Status != domaincard.IntentNeedsReview {
		t.Fatalf("intent after statement/intent race = %+v, error = %v", intent, err)
	}
}

func TestCardDatabaseIntegrityProtectsCyclesReferencesAndHistory(t *testing.T) {
	pool := newIsolatedTestDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	createTestOwner(t, pool, "owner", now)
	ledgerService, _ := applicationledger.NewService(NewLedgerRepository(pool), applicationledger.ServiceOptions{Clock: func() time.Time { return now }, IDGenerator: sequentialIDs()})
	cardA := createTestAccount(t, ledgerService, "owner", "Card A", account.CreditCard())
	cardB := createTestAccount(t, ledgerService, "owner", "Card B", account.CreditCard())
	cash := createTestAccount(t, ledgerService, "owner", "Cash", account.Cash())
	bank := createTestAccount(t, ledgerService, "owner", "Bank", account.Bank())
	loan := createTestAccount(t, ledgerService, "owner", "Loan", account.Loan())
	archived := createTestAccount(t, ledgerService, "owner", "Archived card", account.CreditCard())
	if _, err := ledgerService.ArchiveAccount(ctx, "owner", archived.ID()); err != nil {
		t.Fatal(err)
	}

	for _, candidate := range []account.Account{cash, bank, loan, archived} {
		_, err := pool.Exec(ctx, `INSERT INTO credit_card_cycles(id,owner_id,account_id,cycle_start,cycle_end,created_at) VALUES($1,'owner',$2,'2026-08-01','2026-08-31',$3)`, "invalid-"+candidate.ID(), candidate.ID(), now)
		if !hasPostgresCode(err, "23514") {
			t.Fatalf("invalid cycle account %s error = %v", candidate.Type(), err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO credit_card_cycles(id,owner_id,account_id,cycle_start,cycle_end,created_at) VALUES('wrong-owner','unknown-owner',$1,'2026-08-01','2026-08-31',$2)`, cardA.ID(), now); !hasPostgresCode(err, "23503") {
		t.Fatalf("wrong-owner cycle error = %v", err)
	}
	for cycleID, accountID := range map[string]string{"cycle-a": cardA.ID(), "cycle-b": cardB.ID()} {
		if _, err := pool.Exec(ctx, `INSERT INTO credit_card_cycles(id,owner_id,account_id,cycle_start,cycle_end,created_at) VALUES($1,'owner',$2,'2026-08-01','2026-08-31',$3)`, cycleID, accountID, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE credit_card_cycles SET cycle_start='2026-08-02' WHERE id='cycle-a'`); !hasPostgresCode(err, "55000") {
		t.Fatalf("cycle identity update error = %v", err)
	}
	_, err := pool.Exec(ctx, `INSERT INTO credit_card_statements(id,owner_id,account_id,cycle_id,revision,authority,statement_balance_minor,currency,due_date,created_at) VALUES('cross-card','owner',$1,'cycle-b',1,'issued',100,'MXN','2026-09-09',$2)`, cardA.ID(), now)
	if !hasPostgresCode(err, "23503") {
		t.Fatalf("cross-card statement error = %v", err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO credit_card_payment_intents(id,owner_id,account_id,cycle_id,amount_minor,currency,planned_date,status,version,created_at,updated_at) VALUES('cross-card-intent','owner',$1,'cycle-b',100,'MXN','2026-09-09','active',1,$2,$2)`, cardA.ID(), now)
	if !hasPostgresCode(err, "23503") {
		t.Fatalf("cross-card intent error = %v", err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO credit_card_statements(id,owner_id,account_id,cycle_id,revision,authority,statement_balance_minor,currency,due_date,created_at) VALUES('statement-a','owner',$1,'cycle-a',1,'issued',100,'MXN','2026-09-09',$2)`, cardA.ID(), now); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO credit_card_statements(id,owner_id,account_id,cycle_id,revision,authority,statement_balance_minor,currency,due_date,created_at) VALUES('statement-b','owner',$1,'cycle-b',1,'issued',100,'MXN','2026-09-09',$2)`, cardB.ID(), now); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE credit_card_statements SET superseded_at=$1,superseded_by_id='missing' WHERE id='statement-a'`, now); !hasPostgresCode(err, "23503") {
		t.Fatalf("missing superseding statement error = %v", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE credit_card_statements SET superseded_at=$1,superseded_by_id='statement-b' WHERE id='statement-a'`, now); !hasPostgresCode(err, "23503") {
		t.Fatalf("cross-cycle superseding statement error = %v", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE credit_card_statements SET statement_balance_minor=99 WHERE id='statement-a'`); err == nil {
		t.Fatalf("statement update error = %v", err)
	}
	if _, err = pool.Exec(ctx, `DELETE FROM credit_card_statements WHERE id='statement-a'`); err == nil {
		t.Fatalf("statement delete error = %v", err)
	}
}

func TestInstallmentPlanConservesPrincipalAcrossStatementsAndExplicitPayments(t *testing.T) {
	pool := newIsolatedTestDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	createTestOwner(t, pool, "owner", now)
	ledgerService, _ := applicationledger.NewService(NewLedgerRepository(pool), applicationledger.ServiceOptions{Clock: func() time.Time { return now }, IDGenerator: sequentialIDs()})
	creditCard := createTestAccount(t, ledgerService, "owner", "Card", account.CreditCard())
	bank := createTestAccount(t, ledgerService, "owner", "Bank", account.Bank())
	repository := NewCardRepository(pool)
	principal, _ := money.New(1_200_000, money.MXN())
	purchaseDate := cardTestDate(t, "2026-08-10")
	purchase, err := ledgerService.PostTransaction(ctx, "owner", creditCard.ID(), domainledger.LiabilityCharge(), principal, purchaseDate, "Laptop MSI purchase", testIdempotencyKey(t, "msi-purchase"))
	if err != nil {
		t.Fatal(err)
	}
	input := appcard.InstallmentPlanInput{AccountID: creditCard.ID(), Description: "Laptop MSI", PurchaseTransactionID: purchase.ID(), Principal: principal, InstallmentCount: 12, FirstCycleStart: cardTestDate(t, "2026-08-01"), FirstCycleEnd: cardTestDate(t, "2026-08-31"), Mutation: testMutation(t, "create-msi")}
	plan, err := repository.CreateInstallmentPlan(ctx, "owner", input, "msi-plan", now)
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	replay, err := repository.CreateInstallmentPlan(ctx, "owner", input, "ignored-on-replay", now)
	if err != nil || replay.ID != plan.ID {
		t.Fatalf("plan replay = %+v, error = %v", replay, err)
	}
	allocations, err := repository.ListInstallmentAllocations(ctx, "owner", plan.ID)
	if err != nil || len(allocations) != 12 {
		t.Fatalf("allocations = %d, error = %v", len(allocations), err)
	}
	for index, allocation := range allocations {
		if allocation.InstallmentNumber != index+1 || allocation.Principal.MinorUnits() != 100_000 || allocation.Status != domaincard.InstallmentPending {
			t.Fatalf("allocation %d = %+v", index, allocation)
		}
	}
	summary, err := repository.GetInstallmentPlanSummary(ctx, "owner", plan.ID)
	if err != nil || summary.PaidPrincipal.MinorUnits() != 0 || summary.OutstandingPrincipal.MinorUnits() != 1_200_000 {
		t.Fatalf("initial summary = %+v, error = %v", summary, err)
	}

	statement, err := repository.RegisterStatement(ctx, "owner", cardStatementInput(t, "owner", creditCard.ID(), domaincard.Estimated, 500_000, "2026-09-09", "msi-statement-estimate"), "msi-statement-estimate", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repository.RegisterStatement(ctx, "owner", cardStatementInput(t, "owner", creditCard.ID(), domaincard.Issued, 450_000, "2026-09-09", "msi-statement-issued"), "msi-statement-issued", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err = repository.RegisterStatement(ctx, "owner", cardStatementInput(t, "owner", creditCard.ID(), domaincard.Issued, 400_000, "2026-09-09", "msi-statement-corrected"), "msi-statement-corrected", now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	allocationsAfterStatements, err := repository.ListInstallmentAllocations(ctx, "owner", plan.ID)
	if err != nil || len(allocationsAfterStatements) != 12 || allocationsAfterStatements[0].CycleID != statement.CycleID || allocationsAfterStatements[0].Status != domaincard.InstallmentStatementAllocated {
		t.Fatalf("statement lineage allocations = %+v, error = %v", allocationsAfterStatements, err)
	}

	first := allocations[0]
	paymentAmount, _ := money.New(100_000, money.MXN())
	payment, err := repository.RecordInstallmentPrincipalPayment(ctx, "owner", first.ID, paymentAmount, testMutation(t, "pay-first-msi"), "payment-first", now.Add(3*time.Minute))
	if err != nil || payment.Summary.PaidPrincipal.MinorUnits() != 100_000 || payment.Summary.OutstandingPrincipal.MinorUnits() != 1_100_000 {
		t.Fatalf("pay first allocation = %+v, error = %v", payment, err)
	}
	replayedPayment, err := repository.RecordInstallmentPrincipalPayment(ctx, "owner", first.ID, paymentAmount, testMutation(t, "pay-first-msi"), "ignored-payment", now.Add(4*time.Minute))
	if err != nil || replayedPayment.Payment.ID != payment.Payment.ID || replayedPayment.Summary.PaidPrincipal.MinorUnits() != 100_000 {
		t.Fatalf("payment replay = %+v, error = %v", replayedPayment, err)
	}
	if _, err := repository.RecordInstallmentPrincipalPayment(ctx, "owner", first.ID, paymentAmount, testMutation(t, "overpay-first-msi"), "overpay", now.Add(5*time.Minute)); !errors.Is(err, domaincard.ErrInstallmentOverpayment) {
		t.Fatalf("paid allocation overpayment error = %v", err)
	}

	date := purchaseDate
	charge, _ := money.New(200_000, money.MXN())
	if _, err := ledgerService.PostTransaction(ctx, "owner", creditCard.ID(), domainledger.LiabilityCharge(), charge, date, "generic card charge", testIdempotencyKey(t, "generic-card-charge")); err != nil {
		t.Fatal(err)
	}
	if _, err := ledgerService.PostTransaction(ctx, "owner", bank.ID(), domainledger.AssetInflow(), charge, date, "bank funds", testIdempotencyKey(t, "generic-bank-funds")); err != nil {
		t.Fatal(err)
	}
	if _, err := ledgerService.CreateTransfer(ctx, "owner", bank.ID(), creditCard.ID(), charge, date, "generic card payment", testIdempotencyKey(t, "generic-card-payment")); err != nil {
		t.Fatal(err)
	}
	afterGenericPayment, err := repository.GetInstallmentPlanSummary(ctx, "owner", plan.ID)
	if err != nil || afterGenericPayment.PaidPrincipal.MinorUnits() != 100_000 || afterGenericPayment.OutstandingPrincipal.MinorUnits() != 1_100_000 {
		t.Fatalf("generic card payment changed MSI principal = %+v, error = %v", afterGenericPayment, err)
	}
}

func TestInstallmentPrincipalPaymentsSerializeAndDatabaseProtectsLineage(t *testing.T) {
	pool := newIsolatedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	createTestOwner(t, pool, "owner", now)
	ledgerService, _ := applicationledger.NewService(NewLedgerRepository(pool), applicationledger.ServiceOptions{Clock: func() time.Time { return now }, IDGenerator: sequentialIDs()})
	creditCard := createTestAccount(t, ledgerService, "owner", "Card", account.CreditCard())
	cash := createTestAccount(t, ledgerService, "owner", "Cash", account.Cash())
	repository := NewCardRepository(pool)
	principal, _ := money.New(1_000, money.MXN())
	purchaseDate := cardTestDate(t, "2026-08-10")
	purchase, err := ledgerService.PostTransaction(ctx, "owner", creditCard.ID(), domainledger.LiabilityCharge(), principal, purchaseDate, "MSI purchase", testIdempotencyKey(t, "concurrent-msi-purchase"))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := repository.CreateInstallmentPlan(ctx, "owner", appcard.InstallmentPlanInput{AccountID: creditCard.ID(), Description: "Three MSI", PurchaseTransactionID: purchase.ID(), Principal: principal, InstallmentCount: 1, FirstCycleStart: cardTestDate(t, "2026-08-01"), FirstCycleEnd: cardTestDate(t, "2026-08-31"), Mutation: testMutation(t, "create-concurrent-msi")}, "concurrent-plan", now)
	if err != nil {
		t.Fatal(err)
	}
	allocations, err := repository.ListInstallmentAllocations(ctx, "owner", plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	amount, _ := money.New(700, money.MXN())
	var wait sync.WaitGroup
	wait.Add(2)
	errorsFound := make(chan error, 2)
	for index := 0; index < 2; index++ {
		go func(index int) {
			defer wait.Done()
			_, err := repository.RecordInstallmentPrincipalPayment(ctx, "owner", allocations[0].ID, amount, testMutation(t, fmt.Sprintf("concurrent-msi-%d", index)), fmt.Sprintf("concurrent-payment-%d", index), now.Add(time.Minute))
			errorsFound <- err
		}(index)
	}
	wait.Wait()
	close(errorsFound)
	successes := 0
	for err := range errorsFound {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent payment successes = %d", successes)
	}
	summary, err := repository.GetInstallmentPlanSummary(ctx, "owner", plan.ID)
	if err != nil || summary.PaidPrincipal.MinorUnits() != 700 || summary.OutstandingPrincipal.MinorUnits() != 300 {
		t.Fatalf("concurrent summary = %+v, error = %v", summary, err)
	}
	fullyPaidPurchase, err := ledgerService.PostTransaction(ctx, "owner", creditCard.ID(), domainledger.LiabilityCharge(), principal, purchaseDate, "second MSI purchase", testIdempotencyKey(t, "fully-paid-msi-purchase"))
	if err != nil {
		t.Fatal(err)
	}
	fullyPaidPlan, err := repository.CreateInstallmentPlan(ctx, "owner", appcard.InstallmentPlanInput{AccountID: creditCard.ID(), Description: "Fully paid MSI", PurchaseTransactionID: fullyPaidPurchase.ID(), Principal: principal, InstallmentCount: 1, FirstCycleStart: cardTestDate(t, "2026-08-01"), FirstCycleEnd: cardTestDate(t, "2026-08-31"), Mutation: testMutation(t, "create-fully-paid-msi")}, "fully-paid-plan", now)
	if err != nil {
		t.Fatal(err)
	}
	fullyPaidAllocations, err := repository.ListInstallmentAllocations(ctx, "owner", fullyPaidPlan.ID)
	if err != nil || len(fullyPaidAllocations) != 1 {
		t.Fatalf("fully paid allocations = %+v, error = %v", fullyPaidAllocations, err)
	}
	half, _ := money.New(500, money.MXN())
	var fullyPaidWait sync.WaitGroup
	fullyPaidWait.Add(2)
	fullyPaidErrors := make(chan error, 2)
	for index := 0; index < 2; index++ {
		go func(index int) {
			defer fullyPaidWait.Done()
			_, err := repository.RecordInstallmentPrincipalPayment(ctx, "owner", fullyPaidAllocations[0].ID, half, testMutation(t, fmt.Sprintf("concurrent-full-msi-%d", index)), fmt.Sprintf("concurrent-full-payment-%d", index), now.Add(2*time.Minute))
			fullyPaidErrors <- err
		}(index)
	}
	fullyPaidWait.Wait()
	close(fullyPaidErrors)
	fullyPaidSuccesses := 0
	for err := range fullyPaidErrors {
		if err == nil {
			fullyPaidSuccesses++
		}
	}
	if fullyPaidSuccesses != 2 {
		t.Fatalf("concurrent full-payment successes = %d", fullyPaidSuccesses)
	}
	fullyPaidSummary, err := repository.GetInstallmentPlanSummary(ctx, "owner", fullyPaidPlan.ID)
	if err != nil || fullyPaidSummary.PaidPrincipal.MinorUnits() != 1_000 || fullyPaidSummary.OutstandingPrincipal.MinorUnits() != 0 || fullyPaidSummary.Plan.Status != domaincard.InstallmentPlanCompleted {
		t.Fatalf("concurrent full-payment summary = %+v, error = %v", fullyPaidSummary, err)
	}
	fullyPaidAllocations, err = repository.ListInstallmentAllocations(ctx, "owner", fullyPaidPlan.ID)
	if err != nil || fullyPaidAllocations[0].Status != domaincard.InstallmentPaid {
		t.Fatalf("concurrent full-payment allocation = %+v, error = %v", fullyPaidAllocations, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE credit_card_installment_principal_payments SET amount_minor=1 WHERE plan_id=$1`, plan.ID); err == nil {
		t.Fatal("principal payment history was mutable")
	}
	if _, err := pool.Exec(ctx, `UPDATE credit_card_installment_allocations SET status='paid' WHERE id=$1`, allocations[0].ID); err == nil {
		t.Fatal("allocation could be marked paid without complete principal payment")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO credit_card_installment_allocations(id,owner_id,account_id,plan_id,cycle_id,installment_number,schedule_version,principal_minor,currency,status,created_at) VALUES('duplicate-installment','owner',$1,$2,$3,1,1,1,'MXN','pending',$4)`, creditCard.ID(), plan.ID, allocations[0].CycleID, now); err == nil {
		t.Fatal("duplicate active installment number was accepted")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO credit_card_installment_plans(id,owner_id,account_id,description,purchase_transaction_id,original_principal_minor,currency,installment_count,first_cycle_id,status,schedule_version,created_at,updated_at) VALUES('cash-plan','owner',$1,'invalid',$2,1,'MXN',1,$3,'active',1,$4,$4)`, cash.ID(), purchase.ID(), allocations[0].CycleID, now); err == nil {
		t.Fatalf("non-card plan error = %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO credit_card_installment_allocations(id,owner_id,account_id,plan_id,cycle_id,installment_number,schedule_version,principal_minor,currency,status,created_at) VALUES('cross-card-allocation','owner',$1,$2,$3,2,1,1,'MXN','pending',$4)`, cash.ID(), plan.ID, allocations[0].CycleID, now); !hasPostgresCode(err, "23503") {
		t.Fatalf("cross-card allocation error = %v", err)
	}
}

func TestInstallmentCycleAndPaymentLifecycleConstraints(t *testing.T) {
	pool := newIsolatedTestDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	createTestOwner(t, pool, "owner", now)
	ledgerService, err := applicationledger.NewService(NewLedgerRepository(pool), applicationledger.ServiceOptions{Clock: func() time.Time { return now }, IDGenerator: sequentialIDs()})
	if err != nil {
		t.Fatal(err)
	}
	creditCard := createTestAccount(t, ledgerService, "owner", "Card", account.CreditCard())
	repository := NewCardRepository(pool)
	principal, _ := money.New(1_000, money.MXN())
	purchaseDate := cardTestDate(t, "2026-08-10")
	createPlan := func(name string, installments int) (domaincard.InstallmentPlan, domainledger.Transaction) {
		t.Helper()
		purchase, err := ledgerService.PostTransaction(ctx, "owner", creditCard.ID(), domainledger.LiabilityCharge(), principal, purchaseDate, name+" purchase", testIdempotencyKey(t, name+"-purchase"))
		if err != nil {
			t.Fatal(err)
		}
		plan, err := repository.CreateInstallmentPlan(ctx, "owner", appcard.InstallmentPlanInput{AccountID: creditCard.ID(), Description: name, PurchaseTransactionID: purchase.ID(), Principal: principal, InstallmentCount: installments, FirstCycleStart: cardTestDate(t, "2026-08-01"), FirstCycleEnd: cardTestDate(t, "2026-08-31"), Mutation: testMutation(t, name+"-plan")}, name+"-plan", now)
		if err != nil {
			t.Fatal(err)
		}
		return plan, purchase
	}

	plan, planPurchase := createPlan("lifecycle", 1)
	if _, err := repository.CreateInstallmentPlan(ctx, "owner", appcard.InstallmentPlanInput{AccountID: creditCard.ID(), Description: "duplicate source", PurchaseTransactionID: planPurchase.ID(), Principal: principal, InstallmentCount: 1, FirstCycleStart: cardTestDate(t, "2026-08-01"), FirstCycleEnd: cardTestDate(t, "2026-08-31"), Mutation: testMutation(t, "duplicate-source-plan")}, "duplicate-source-plan", now); err == nil {
		t.Fatal("one liability charge backed more than one installment plan")
	}
	allocations, err := repository.ListInstallmentAllocations(ctx, "owner", plan.ID)
	if err != nil || len(allocations) != 1 {
		t.Fatalf("lifecycle allocations = %+v, error = %v", allocations, err)
	}
	allocation := allocations[0]

	// A second plan may use the same card cycle, but duplicate allocations in
	// one plan/version may not.
	secondPlan, _ := createPlan("same-cycle-other-plan", 1)
	secondAllocations, err := repository.ListInstallmentAllocations(ctx, "owner", secondPlan.ID)
	if err != nil || len(secondAllocations) != 1 || secondAllocations[0].CycleID != allocation.CycleID {
		t.Fatalf("different plan same cycle = %+v, error = %v", secondAllocations, err)
	}
	duplicatePurchase, err := ledgerService.PostTransaction(ctx, "owner", creditCard.ID(), domainledger.LiabilityCharge(), principal, purchaseDate, "duplicate cycle purchase", testIdempotencyKey(t, "duplicate-cycle-purchase"))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO credit_card_installment_plans(id,owner_id,account_id,description,purchase_transaction_id,original_principal_minor,currency,installment_count,first_cycle_id,status,schedule_version,created_at,updated_at) VALUES('duplicate-cycle-plan','owner',$1,'duplicate cycle',$2,1000,'MXN',2,$3,'active',1,$4,$4)`, creditCard.ID(), duplicatePurchase.ID(), allocation.CycleID, now)
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO credit_card_installment_allocations(id,owner_id,account_id,plan_id,cycle_id,installment_number,schedule_version,principal_minor,currency,status,created_at) VALUES('duplicate-cycle-allocation-1','owner',$1,'duplicate-cycle-plan',$2,1,1,500,'MXN','pending',$3)`, creditCard.ID(), allocation.CycleID, now)
	}
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO credit_card_installment_allocations(id,owner_id,account_id,plan_id,cycle_id,installment_number,schedule_version,principal_minor,currency,status,created_at) VALUES('duplicate-cycle-allocation-2','owner',$1,'duplicate-cycle-plan',$2,2,1,500,'MXN','pending',$3)`, creditCard.ID(), allocation.CycleID, now)
	}
	if !hasPostgresCode(err, "23505") {
		t.Fatalf("duplicate plan/version/cycle error = %v", err)
	}
	_ = tx.Rollback(ctx)

	// Deferred lifecycle checks reject payment truth that has not been reflected
	// in allocation and plan status.
	insertTwoPayments := func(txID string, updateAllocation, updatePlan bool) error {
		t.Helper()
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		for index := 1; index <= 2; index++ {
			if _, err := tx.Exec(ctx, `INSERT INTO credit_card_installment_principal_payments(id,owner_id,account_id,plan_id,allocation_id,amount_minor,currency,created_at) VALUES($1,'owner',$2,$3,$4,500,'MXN',$5)`, fmt.Sprintf("%s-%d", txID, index), creditCard.ID(), plan.ID, allocation.ID, now); err != nil {
				return err
			}
		}
		if updateAllocation {
			if _, err := tx.Exec(ctx, `UPDATE credit_card_installment_allocations SET status='paid' WHERE id=$1`, allocation.ID); err != nil {
				return err
			}
		}
		if updatePlan {
			if _, err := tx.Exec(ctx, `UPDATE credit_card_installment_plans SET status='completed',updated_at=$1 WHERE id=$2`, now.Add(time.Minute), plan.ID); err != nil {
				return err
			}
		}
		return tx.Commit(ctx)
	}
	if err := insertTwoPayments("nonpaid-allocation", false, false); !hasPostgresCode(err, "23514") {
		t.Fatalf("fully paid non-paid allocation commit error = %v", err)
	}
	if err := insertTwoPayments("active-plan", true, false); !hasPostgresCode(err, "23514") {
		t.Fatalf("fully paid active plan commit error = %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE credit_card_installment_allocations SET status='paid' WHERE id=$1`, allocation.ID); err == nil {
		t.Fatal("unpaid allocation could be marked paid")
	}
	if _, err := pool.Exec(ctx, `UPDATE credit_card_installment_plans SET status='completed',updated_at=$1 WHERE id=$2`, now.Add(time.Minute), plan.ID); err == nil {
		t.Fatal("incomplete plan could be marked completed")
	}

	half, _ := money.New(500, money.MXN())
	if _, err := repository.RecordInstallmentPrincipalPayment(ctx, "owner", allocation.ID, half, testMutation(t, "lifecycle-payment-one"), "lifecycle-payment-one", now); err != nil {
		t.Fatal(err)
	}
	completed, err := repository.RecordInstallmentPrincipalPayment(ctx, "owner", allocation.ID, half, testMutation(t, "lifecycle-payment-two"), "lifecycle-payment-two", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if completed.Summary.Plan.Status != domaincard.InstallmentPlanCompleted || completed.Summary.PaidPrincipal.MinorUnits() != 1_000 || completed.Summary.OutstandingPrincipal.MinorUnits() != 0 {
		t.Fatalf("completed summary = %+v", completed.Summary)
	}
	updated, err := repository.ListInstallmentAllocations(ctx, "owner", plan.ID)
	if err != nil || updated[0].Status != domaincard.InstallmentPaid {
		t.Fatalf("completed allocation = %+v, error = %v", updated, err)
	}
}

func cardStatementInput(t *testing.T, owner, accountID string, authority domaincard.Authority, balanceMinor int64, due, key string) appcard.StatementInput {
	t.Helper()
	balance, err := money.New(balanceMinor, money.MXN())
	if err != nil {
		t.Fatal(err)
	}
	return appcard.StatementInput{
		AccountID: accountID,
		Start:     cardTestDate(t, "2026-08-01"),
		End:       cardTestDate(t, "2026-08-31"),
		Due:       cardTestDate(t, due),
		Authority: authority,
		Balance:   balance,
		Mutation: applicationledger.MutationIdentity{
			Key:         testIdempotencyKey(t, key),
			Fingerprint: applicationledger.CanonicalFingerprint("v1", "register_credit_card_statement", owner, accountID, "2026-08-01", "2026-08-31", string(authority), due, "MXN", fmt.Sprint(balanceMinor), "", ""),
		},
	}
}

func cardTestDate(t *testing.T, value string) financialdate.Date {
	t.Helper()
	date, err := financialdate.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	return date
}

func assertCardStatementState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, cycleID string, wantTotal, wantActive int) {
	t.Helper()
	var total, active int
	if err := pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE superseded_at IS NULL) FROM credit_card_statements WHERE cycle_id=$1`, cycleID).Scan(&total, &active); err != nil {
		t.Fatal(err)
	}
	if total != wantTotal || active != wantActive {
		t.Fatalf("statement state total=%d active=%d, want %d/%d", total, active, wantTotal, wantActive)
	}
}
