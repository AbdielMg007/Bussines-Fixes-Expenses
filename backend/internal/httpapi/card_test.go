package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"runway/backend/internal/auth"
	app "runway/backend/internal/card"
	domain "runway/backend/internal/domain/card"
	"runway/backend/internal/domain/financialdate"
	"runway/backend/internal/domain/money"
	ledger "runway/backend/internal/ledger"
)

func TestStatementHTTPRequiresBalancePresenceAndAcceptsExplicitZero(t *testing.T) {
	called := false
	service := &fakeCardService{register: func(owner string, input app.StatementInput) (domain.Statement, error) {
		called = true
		if owner != "owner-id" || input.Balance.MinorUnits() != 0 {
			t.Fatalf("statement input = %+v owner=%q", input, owner)
		}
		return testHTTPStatement(t, input.Balance), nil
	}}
	handler := newAuthenticatedCardHandler(service)

	missing := cardJSONRequest(http.MethodPost, "/api/v1/credit-cards/card/statements", `{"cycle_start":"2026-08-01","cycle_end":"2026-08-31","authority":"issued","currency":"MXN","due_date":"2026-09-09"}`)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, missing)
	if response.Code != http.StatusBadRequest || called {
		t.Fatalf("omitted balance status=%d called=%t body=%s", response.Code, called, response.Body.String())
	}

	explicitZero := cardJSONRequest(http.MethodPost, "/api/v1/credit-cards/card/statements", `{"cycle_start":"2026-08-01","cycle_end":"2026-08-31","authority":"issued","statement_balance_minor":0,"currency":"MXN","due_date":"2026-09-09"}`)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, explicitZero)
	if response.Code != http.StatusCreated || !called {
		t.Fatalf("explicit zero status=%d called=%t body=%s", response.Code, called, response.Body.String())
	}
}

func TestCardHTTPMapsCancelledIntentReplacementToConflict(t *testing.T) {
	service := &fakeCardService{replaceIntent: func(string, string, money.Money, financialdate.Date) (domain.PaymentIntent, error) {
		return domain.PaymentIntent{}, domain.ErrPaymentIntentCancelled
	}}
	response := httptest.NewRecorder()
	newAuthenticatedCardHandler(service).ServeHTTP(response, cardJSONRequest(http.MethodPut, "/api/v1/credit-card-cycles/cycle/payment-intent", `{"amount_minor":1,"currency":"MXN","planned_date":"2026-09-09"}`))
	if response.Code != http.StatusConflict {
		t.Fatalf("cancelled intent replacement status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestGetPaymentIntentIncludesBackendSettlementSummary(t *testing.T) {
	amount, _ := money.New(280_000, money.MXN())
	settled, _ := money.New(100_000, money.MXN())
	remaining, _ := money.New(180_000, money.MXN())
	date, _ := financialdate.Parse("2026-09-09")
	intent, err := domain.NewPaymentIntent("intent", "owner-id", "card", "cycle", amount, date, domain.IntentActive, 1, time.Now().UTC(), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	service := &fakeCardService{getIntentSummary: func(owner, cycle string) (app.PaymentIntentSummary, error) {
		if owner != "owner-id" || cycle != "cycle" {
			t.Fatalf("summary owner=%q cycle=%q", owner, cycle)
		}
		return app.PaymentIntentSummary{Intent: intent, SettledAmount: settled, RemainingAmount: remaining}, nil
	}}
	response := httptest.NewRecorder()
	newAuthenticatedCardHandler(service).ServeHTTP(response, cardJSONRequest(http.MethodGet, "/api/v1/credit-card-cycles/cycle/payment-intent", ""))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"settled_amount_minor":100000`) || !strings.Contains(response.Body.String(), `"remaining_amount_minor":180000`) {
		t.Fatalf("summary fields missing: %s", response.Body.String())
	}
}

func TestInstallmentPlanAndPrincipalPaymentHTTPValidation(t *testing.T) {
	calledPlan := false
	calledPayment := false
	service := &fakeCardService{
		createPlan: func(owner string, input app.InstallmentPlanInput) (domain.InstallmentPlan, error) {
			calledPlan = true
			if owner != "owner-id" || input.Principal.MinorUnits() != 1 || input.InstallmentCount != 1 || input.PurchaseTransactionID != "purchase" {
				t.Fatalf("plan input = %+v owner=%q", input, owner)
			}
			return testHTTPInstallmentPlan(t, input), nil
		},
		recordPayment: func(owner, allocationID string, amount money.Money, _ ledger.MutationIdentity) (app.InstallmentPrincipalPaymentResult, error) {
			calledPayment = true
			if owner != "owner-id" || allocationID != "allocation" || amount.MinorUnits() != 1 {
				t.Fatalf("payment input owner=%q allocation=%q amount=%d", owner, allocationID, amount.MinorUnits())
			}
			return app.InstallmentPrincipalPaymentResult{}, nil
		},
	}
	handler := newAuthenticatedCardHandler(service)
	missingPlan := cardJSONRequest(http.MethodPost, "/api/v1/credit-cards/card/installment-plans", `{"description":"Laptop","currency":"MXN","installment_count":1,"first_cycle_start":"2026-08-01","first_cycle_end":"2026-08-31"}`)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, missingPlan)
	if response.Code != http.StatusBadRequest || calledPlan {
		t.Fatalf("missing plan principal status=%d called=%t", response.Code, calledPlan)
	}
	zeroPlan := cardJSONRequest(http.MethodPost, "/api/v1/credit-cards/card/installment-plans", `{"description":"Laptop","purchase_transaction_id":"purchase","original_principal_minor":0,"currency":"MXN","installment_count":1,"first_cycle_start":"2026-08-01","first_cycle_end":"2026-08-31"}`)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, zeroPlan)
	if response.Code != http.StatusBadRequest || calledPlan {
		t.Fatalf("zero plan status=%d called=%t body=%s", response.Code, calledPlan, response.Body.String())
	}
	validPlan := cardJSONRequest(http.MethodPost, "/api/v1/credit-cards/card/installment-plans", `{"description":"Laptop","purchase_transaction_id":"purchase","original_principal_minor":1,"currency":"MXN","installment_count":1,"first_cycle_start":"2026-08-01","first_cycle_end":"2026-08-31"}`)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, validPlan)
	if response.Code != http.StatusCreated || !calledPlan {
		t.Fatalf("valid plan status=%d called=%t body=%s", response.Code, calledPlan, response.Body.String())
	}
	missingPayment := cardJSONRequest(http.MethodPost, "/api/v1/installment-allocations/allocation/principal-payments", `{"currency":"MXN"}`)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, missingPayment)
	if response.Code != http.StatusBadRequest || calledPayment {
		t.Fatalf("missing payment amount status=%d called=%t", response.Code, calledPayment)
	}
	validPayment := cardJSONRequest(http.MethodPost, "/api/v1/installment-allocations/allocation/principal-payments", `{"amount_minor":1,"currency":"MXN"}`)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, validPayment)
	if response.Code != http.StatusCreated || !calledPayment {
		t.Fatalf("valid payment status=%d called=%t body=%s", response.Code, calledPayment, response.Body.String())
	}
}

type fakeCardService struct {
	register         func(string, app.StatementInput) (domain.Statement, error)
	replaceIntent    func(string, string, money.Money, financialdate.Date) (domain.PaymentIntent, error)
	getIntentSummary func(string, string) (app.PaymentIntentSummary, error)
	createPlan       func(string, app.InstallmentPlanInput) (domain.InstallmentPlan, error)
	recordPayment    func(string, string, money.Money, ledger.MutationIdentity) (app.InstallmentPrincipalPaymentResult, error)
}

func (f *fakeCardService) SettleIntent(_ context.Context, _ string, _ app.PaymentIntentSettlementInput) (app.PaymentIntentSettlementResult, error) {
	return app.PaymentIntentSettlementResult{}, nil
}

func (f *fakeCardService) RegisterStatement(_ context.Context, owner string, input app.StatementInput) (domain.Statement, error) {
	if f.register == nil {
		return domain.Statement{}, errors.New("unexpected RegisterStatement")
	}
	return f.register(owner, input)
}
func (f *fakeCardService) ListStatements(context.Context, string, string) ([]domain.Statement, error) {
	return nil, nil
}
func (f *fakeCardService) GetStatement(context.Context, string, string) (domain.Statement, error) {
	return domain.Statement{}, nil
}
func (f *fakeCardService) GetIntent(context.Context, string, string) (domain.PaymentIntent, error) {
	return domain.PaymentIntent{}, nil
}
func (f *fakeCardService) GetIntentSummary(_ context.Context, owner, cycle string) (app.PaymentIntentSummary, error) {
	if f.getIntentSummary == nil {
		return app.PaymentIntentSummary{}, errors.New("unexpected GetIntentSummary")
	}
	return f.getIntentSummary(owner, cycle)
}
func (f *fakeCardService) ReplaceIntent(_ context.Context, owner, cycle string, amount money.Money, date financialdate.Date) (domain.PaymentIntent, error) {
	if f.replaceIntent == nil {
		return domain.PaymentIntent{}, errors.New("unexpected ReplaceIntent")
	}
	return f.replaceIntent(owner, cycle, amount, date)
}
func (f *fakeCardService) CancelIntent(context.Context, string, string) (domain.PaymentIntent, error) {
	return domain.PaymentIntent{}, nil
}
func (f *fakeCardService) CreateInstallmentPlan(_ context.Context, owner string, input app.InstallmentPlanInput) (domain.InstallmentPlan, error) {
	if f.createPlan == nil {
		return domain.InstallmentPlan{}, errors.New("unexpected CreateInstallmentPlan")
	}
	return f.createPlan(owner, input)
}
func (f *fakeCardService) GetInstallmentPlan(context.Context, string, string) (domain.InstallmentPlan, error) {
	return domain.InstallmentPlan{}, nil
}
func (f *fakeCardService) ListInstallmentPlans(context.Context, string, string) ([]domain.InstallmentPlan, error) {
	return nil, nil
}
func (f *fakeCardService) ListInstallmentAllocations(context.Context, string, string) ([]domain.InstallmentAllocation, error) {
	return nil, nil
}
func (f *fakeCardService) GetInstallmentPlanSummary(context.Context, string, string) (app.InstallmentPlanSummary, error) {
	return app.InstallmentPlanSummary{}, nil
}
func (f *fakeCardService) RecordInstallmentPrincipalPayment(_ context.Context, owner, allocationID string, amount money.Money, mutation ledger.MutationIdentity) (app.InstallmentPrincipalPaymentResult, error) {
	if f.recordPayment == nil {
		return app.InstallmentPrincipalPaymentResult{}, errors.New("unexpected RecordInstallmentPrincipalPayment")
	}
	return f.recordPayment(owner, allocationID, amount, mutation)
}

func newAuthenticatedCardHandler(service CardService) http.Handler {
	owner, _ := auth.NewOwner("owner-id", "owner@example.com", "hash", time.Now().UTC())
	authentication := &fakeAuthentication{authenticate: func(token string) (auth.Owner, error) {
		if token != "valid-token" {
			return auth.Owner{}, auth.ErrUnauthenticated
		}
		return owner, nil
	}}
	return NewHandler(authentication, nil, nil, AuthConfig{AllowedOrigin: testOrigin}, service)
}

func cardJSONRequest(method, path, body string) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", testOrigin)
	request.Header.Set("Idempotency-Key", "card-http-key")
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "valid-token"})
	return request
}

func testHTTPStatement(t *testing.T, balance money.Money) domain.Statement {
	t.Helper()
	due, err := financialdate.Parse("2026-09-09")
	if err != nil {
		t.Fatal(err)
	}
	statement, err := domain.NewStatement("statement", "owner-id", "card", "cycle", 1, domain.Issued, balance, nil, nil, due, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	return statement
}
func testHTTPInstallmentPlan(t *testing.T, input app.InstallmentPlanInput) domain.InstallmentPlan {
	t.Helper()
	plan, err := domain.NewInstallmentPlan("plan", "owner-id", input.AccountID, input.Description, input.PurchaseTransactionID, "cycle", input.Principal, input.InstallmentCount, 1, domain.InstallmentPlanActive, time.Now().UTC(), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	return plan
}
