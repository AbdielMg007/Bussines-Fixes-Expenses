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
	"runway/backend/internal/domain/financialdate"
	"runway/backend/internal/domain/money"
	domainprojection "runway/backend/internal/domain/projection"
	"runway/backend/internal/domain/schedule"
	applicationprojection "runway/backend/internal/projection"
)

func TestProjectionEndpointsRequireAuthenticationAndMutationOrigin(t *testing.T) {
	handler := NewHandler(&fakeAuthentication{}, nil, nil, AuthConfig{AllowedOrigin: testOrigin}, &fakeProjectionService{})
	for _, path := range []string{"/api/v1/projection-policy", "/api/v1/projection"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("GET %s = %d", path, response.Code)
		}
	}

	request := projectionRequest(http.MethodPut, validPolicyJSON())
	request.Header.Set("Origin", "https://attacker.example")
	response := httptest.NewRecorder()
	newAuthenticatedProjectionHandler(&fakeProjectionService{}).ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("wrong origin = %d", response.Code)
	}
}

func TestProjectionPolicyHTTPUsesSessionOwnerAndDoesNotRequireIdempotencyKey(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	var receivedOwner string
	service := &fakeProjectionService{replace: func(ownerID string, currency money.Currency, horizon int, reserve money.Money, timezone string, selection domainprojection.AccountSelection, inflow domainprojection.InflowPolicy, order domainprojection.SameDayOrder) (domainprojection.Policy, error) {
		receivedOwner = ownerID
		return domainprojection.NewPolicy(ownerID, ownerID, currency, horizon, reserve, timezone, selection, inflow, order, now)
	}}
	request := projectionRequest(http.MethodPut, validPolicyJSON())
	request.Header.Del("Idempotency-Key")
	response := httptest.NewRecorder()
	newAuthenticatedProjectionHandler(service).ServeHTTP(response, request)
	if response.Code != http.StatusOK || receivedOwner != "owner-id" || !strings.Contains(response.Body.String(), `"reserve_minor":0`) || !strings.Contains(response.Body.String(), `"version":1`) {
		t.Fatalf("replace = %d %s owner=%s", response.Code, response.Body.String(), receivedOwner)
	}
}

func TestProjectionHTTPReturnsAuditableTraceAndExclusions(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	selection, _ := domainprojection.NewAccountSelection(domainprojection.AllActiveLiquidSelection(), nil)
	reserve, _ := money.New(100_00, money.MXN())
	policy, _ := domainprojection.NewPolicy("owner-id", "owner-id", money.MXN(), 30, reserve, "America/Mexico_City", selection, domainprojection.ConfirmedInflowsOnly(), domainprojection.OutflowsBeforeInflows(), now)
	asOf := mustProjectionHTTPDate(t, "2026-08-10")
	end, _ := asOf.AddDays(30)
	opening, _ := money.NewBalance(260_000, money.MXN())
	amount, _ := money.New(226_400, money.MXN())
	event, _ := domainprojection.NewEvent("event", domainprojection.ManualScheduledFlowSource(), "flow", mustProjectionHTTPDate(t, "2026-08-15"), amount, schedule.Outflow(), schedule.ExactAmount(), schedule.ExactDate(), domainprojection.MandatoryManualOutflow(), nil, "Car")
	receivableAmount, _ := money.New(100, money.MXN())
	confirmed := schedule.Confirmed()
	receivableEvent, _ := domainprojection.NewEvent("receivable-event", domainprojection.ReceivableSource(), "receivable", mustProjectionHTTPDate(t, "2026-08-16"), receivableAmount, schedule.Inflow(), schedule.ExactAmount(), schedule.EstimatedDate(), domainprojection.ConfirmedReceivable(), &confirmed, "Reimbursement")
	exclusion, _ := domainprojection.NewExclusion(domainprojection.ReceivableSource(), "debt", []domainprojection.ExclusionReason{domainprojection.ExclusionUndatedReceivable, domainprojection.ExclusionUncertainReceivable}, "Family business")
	result, _ := domainprojection.Calculate(domainprojection.Input{Policy: policy, AsOf: asOf, HorizonEnd: end, SelectedAccountIDs: []string{"bank"}, OpeningBalance: opening, Events: []domainprojection.Event{event, receivableEvent}, Exclusions: []domainprojection.Exclusion{exclusion}})
	service := &fakeProjectionService{calculate: func(ownerID string) (domainprojection.Result, error) {
		if ownerID != "owner-id" {
			t.Fatalf("owner = %s", ownerID)
		}
		return result, nil
	}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/projection", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "valid-token"})
	response := httptest.NewRecorder()
	newAuthenticatedProjectionHandler(service).ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"opening_liquid_balance_minor":260000`) ||
		!strings.Contains(response.Body.String(), `"balance_after_minor":33600`) ||
		!strings.Contains(response.Body.String(), `"inclusion_basis":"mandatory_manual_outflow"`) ||
		!strings.Contains(response.Body.String(), `"inclusion_basis":"confirmed_receivable"`) ||
		!strings.Contains(response.Body.String(), `"source_certainty":"confirmed"`) ||
		!strings.Contains(response.Body.String(), `"date_provenance":"estimated"`) ||
		!strings.Contains(response.Body.String(), `"uncertain_receivable"`) {
		t.Fatalf("projection = %d %s", response.Code, response.Body.String())
	}
}

func TestProjectionHTTPStrictValidationAndErrorMapping(t *testing.T) {
	handler := newAuthenticatedProjectionHandler(&fakeProjectionService{})
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "malformed", body: `{`},
		{name: "trailing", body: validPolicyJSON() + `{}`},
		{name: "unknown", body: strings.Replace(validPolicyJSON(), `"currency":"MXN"`, `"currency":"MXN","owner_id":"attacker"`, 1)},
		{name: "missing horizon", body: `{"currency":"MXN","reserve_minor":0,"financial_timezone":"America/Mexico_City","account_selection":{"mode":"explicit","account_ids":[]},"inflow_policy":"confirmed_only","same_day_order":"outflows_before_inflows"}`},
		{name: "missing reserve", body: `{"currency":"MXN","horizon_days":60,"financial_timezone":"America/Mexico_City","account_selection":{"mode":"explicit","account_ids":[]},"inflow_policy":"confirmed_only","same_day_order":"outflows_before_inflows"}`},
		{name: "negative reserve", body: strings.Replace(validPolicyJSON(), `"reserve_minor":0`, `"reserve_minor":-1`, 1)},
		{name: "duplicate account", body: strings.Replace(validPolicyJSON(), `"account_ids":[]`, `"account_ids":["bank","bank"]`, 1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, projectionRequest(http.MethodPut, test.body))
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
			}
		})
	}

	missingPolicy := &fakeProjectionService{calculate: func(string) (domainprojection.Result, error) {
		return domainprojection.Result{}, applicationprojection.ErrConfigurationRequired
	}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/projection", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "valid-token"})
	response := httptest.NewRecorder()
	newAuthenticatedProjectionHandler(missingPolicy).ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("configuration required = %d", response.Code)
	}

	policyMissing := &fakeProjectionService{get: func(string) (domainprojection.Policy, error) {
		return domainprojection.Policy{}, applicationprojection.ErrPolicyNotFound
	}}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/projection-policy", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "valid-token"})
	response = httptest.NewRecorder()
	newAuthenticatedProjectionHandler(policyMissing).ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing policy = %d", response.Code)
	}
}

func TestProjectionHTTPMapsStalePolicyConfigurationToConflict(t *testing.T) {
	service := &fakeProjectionService{calculate: func(string) (domainprojection.Result, error) {
		return domainprojection.Result{}, applicationprojection.ErrConfigurationInvalid
	}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/projection", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "valid-token"})
	response := httptest.NewRecorder()
	newAuthenticatedProjectionHandler(service).ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"error":"projection configuration is invalid; update projection policy"`) {
		t.Fatalf("stale configuration = %d %s", response.Code, response.Body.String())
	}
}

type fakeProjectionService struct {
	get       func(string) (domainprojection.Policy, error)
	replace   func(string, money.Currency, int, money.Money, string, domainprojection.AccountSelection, domainprojection.InflowPolicy, domainprojection.SameDayOrder) (domainprojection.Policy, error)
	calculate func(string) (domainprojection.Result, error)
}

func (f *fakeProjectionService) GetPolicy(_ context.Context, ownerID string) (domainprojection.Policy, error) {
	if f.get == nil {
		return domainprojection.Policy{}, errors.New("unexpected get")
	}
	return f.get(ownerID)
}

func (f *fakeProjectionService) ReplacePolicy(_ context.Context, ownerID string, currency money.Currency, horizon int, reserve money.Money, timezone string, selection domainprojection.AccountSelection, inflow domainprojection.InflowPolicy, order domainprojection.SameDayOrder) (domainprojection.Policy, error) {
	if f.replace == nil {
		return domainprojection.Policy{}, errors.New("unexpected replace")
	}
	return f.replace(ownerID, currency, horizon, reserve, timezone, selection, inflow, order)
}

func (f *fakeProjectionService) CalculateBaseline(_ context.Context, ownerID string) (domainprojection.Result, error) {
	if f.calculate == nil {
		return domainprojection.Result{}, errors.New("unexpected calculate")
	}
	return f.calculate(ownerID)
}

func newAuthenticatedProjectionHandler(service ProjectionService) http.Handler {
	owner, _ := auth.NewOwner("owner-id", "owner@example.com", "encoded-password-hash", time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC))
	authentication := &fakeAuthentication{authenticate: func(token string) (auth.Owner, error) {
		if token != "valid-token" {
			return auth.Owner{}, auth.ErrUnauthenticated
		}
		return owner, nil
	}}
	return NewHandler(authentication, nil, nil, AuthConfig{AllowedOrigin: testOrigin}, service)
}

func projectionRequest(method, body string) *http.Request {
	request := httptest.NewRequest(method, "/api/v1/projection-policy", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", testOrigin)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "valid-token"})
	return request
}

func validPolicyJSON() string {
	return `{"currency":"MXN","horizon_days":60,"reserve_minor":0,"financial_timezone":"America/Mexico_City","account_selection":{"mode":"explicit","account_ids":[]},"inflow_policy":"confirmed_only","same_day_order":"outflows_before_inflows"}`
}

func mustProjectionHTTPDate(t *testing.T, value string) financialdate.Date {
	t.Helper()
	date, err := financialdate.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	return date
}
