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

type fakeCardService struct {
	register      func(string, app.StatementInput) (domain.Statement, error)
	replaceIntent func(string, string, money.Money, financialdate.Date) (domain.PaymentIntent, error)
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
func (f *fakeCardService) ReplaceIntent(_ context.Context, owner, cycle string, amount money.Money, date financialdate.Date) (domain.PaymentIntent, error) {
	if f.replaceIntent == nil {
		return domain.PaymentIntent{}, errors.New("unexpected ReplaceIntent")
	}
	return f.replaceIntent(owner, cycle, amount, date)
}
func (f *fakeCardService) CancelIntent(context.Context, string, string) (domain.PaymentIntent, error) {
	return domain.PaymentIntent{}, nil
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
