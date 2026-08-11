package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"runway/backend/internal/auth"
	"runway/backend/internal/domain/financialdate"
	"runway/backend/internal/domain/money"
	domainschedule "runway/backend/internal/domain/schedule"
	applicationledger "runway/backend/internal/ledger"
)

func TestScheduleEndpointsRequireAuthenticationOriginAndIdempotency(t *testing.T) {
	handler := NewHandler(&fakeAuthentication{}, nil, &fakeSchedule{}, AuthConfig{AllowedOrigin: testOrigin})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/obligations", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", response.Code)
	}

	for _, test := range []struct{ path, body string }{
		{path: "/api/v1/obligations", body: `{"name":"Car","amount_minor":1,"currency":"MXN","recurrence":"one_time","start_date":"2026-08-15"}`},
		{path: "/api/v1/obligations/id/archive", body: `{"inactive_from":"2026-08-20"}`},
		{path: "/api/v1/scheduled-flows", body: `{"amount_minor":1,"currency":"MXN","direction":"inflow","financial_date":"2026-08-15","source_kind":"manual_expected_income","amount_provenance":"exact","date_provenance":"exact","inclusion_eligibility":"eligible"}`},
		{path: "/api/v1/receivables", body: `{"name":"Debt","original_amount_minor":1,"currency":"MXN","certainty":"uncertain","amount_provenance":"exact"}`},
		{path: "/api/v1/receivables/id/collections", body: `{"amount_minor":1,"currency":"MXN","ledger_transaction_id":""}`},
	} {
		request := scheduleJSONRequest(http.MethodPost, test.path, test.body)
		request.Header.Del("Idempotency-Key")
		response = httptest.NewRecorder()
		newAuthenticatedScheduleHandler(&fakeSchedule{}).ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("missing key POST %s = %d", test.path, response.Code)
		}
	}

	request := scheduleJSONRequest(http.MethodPost, "/api/v1/obligations", `{}`)
	request.Header.Set("Origin", "https://attacker.example")
	response = httptest.NewRecorder()
	newAuthenticatedScheduleHandler(&fakeSchedule{}).ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("wrong origin status = %d", response.Code)
	}
}

func TestObligationHTTPFlowUsesSessionOwnerAndExpandsOccurrences(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	var receivedOwner string
	future := &fakeSchedule{
		createObligation: func(ownerID, name string, amount money.Money, recurrence domainschedule.Recurrence, start financialdate.Date, end *financialdate.Date, key applicationledger.IdempotencyKey) (domainschedule.Obligation, error) {
			receivedOwner = ownerID
			return domainschedule.NewObligation("obligation-id", ownerID, name, amount, recurrence, start, end, now)
		},
		expandObligation: func(ownerID, obligationID string, from, to financialdate.Date) ([]domainschedule.ScheduledCashFlow, error) {
			amount, _ := money.New(2_264_00, money.MXN())
			obligation, _ := domainschedule.NewObligation(obligationID, ownerID, "Car", amount, domainschedule.Biweekly(), mustHTTPDate(t, "2026-08-16"), nil, now)
			return domainschedule.ExpandObligation(obligation, from, to)
		},
		archiveObligation: func(ownerID, obligationID string, inactiveFrom financialdate.Date, _ applicationledger.IdempotencyKey) (domainschedule.Obligation, error) {
			amount, _ := money.New(2_264_00, money.MXN())
			obligation, _ := domainschedule.NewObligation(obligationID, ownerID, "Car", amount, domainschedule.Biweekly(), mustHTTPDate(t, "2026-08-01"), nil, now)
			return obligation.Archive(inactiveFrom, now.Add(time.Hour))
		},
	}
	handler := newAuthenticatedScheduleHandler(future)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, scheduleJSONRequest(http.MethodPost, "/api/v1/obligations", `{"name":"Car","amount_minor":226400,"currency":"MXN","recurrence":"biweekly","start_date":"2026-08-16","end_date":null}`))
	if response.Code != http.StatusCreated || receivedOwner != "owner-id" || !strings.Contains(response.Body.String(), `"direction":"outflow"`) {
		t.Fatalf("create obligation = %d %s owner=%s", response.Code, response.Body.String(), receivedOwner)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/obligations/obligation-id/occurrences?from=2026-08-01&to=2026-09-30", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "valid-token"})
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("occurrences status = %d body = %s", response.Code, response.Body.String())
	}
	var occurrences []scheduledFlowResponse
	if err := json.NewDecoder(response.Body).Decode(&occurrences); err != nil || len(occurrences) != 4 || occurrences[0].SourceKind != "obligation" {
		t.Fatalf("occurrences = %+v, error = %v", occurrences, err)
	}

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, scheduleJSONRequest(http.MethodPost, "/api/v1/obligations/obligation-id/archive", `{"inactive_from":"2026-08-20"}`))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"inactive_from":"2026-08-20"`) {
		t.Fatalf("archive obligation = %d %s", response.Code, response.Body.String())
	}
}

func TestScheduledFlowAndReceivableHTTPFlows(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	future := &fakeSchedule{
		createScheduledFlow: func(ownerID string, amount money.Money, direction domainschedule.Direction, date financialdate.Date, source domainschedule.SourceKind, amountProvenance domainschedule.AmountProvenance, dateProvenance domainschedule.DateProvenance, inclusion domainschedule.InclusionEligibility, key applicationledger.IdempotencyKey) (domainschedule.ScheduledCashFlow, error) {
			return domainschedule.NewManualScheduledCashFlow("flow-id", ownerID, amount, direction, date, source, amountProvenance, dateProvenance, inclusion, now)
		},
		createReceivable: func(ownerID, name string, amount money.Money, expectedDate *financialdate.Date, certainty domainschedule.Certainty, amountProvenance domainschedule.AmountProvenance, dateProvenance *domainschedule.DateProvenance, key applicationledger.IdempotencyKey) (domainschedule.Receivable, error) {
			return domainschedule.NewReceivable("receivable-id", ownerID, name, amount, expectedDate, certainty, amountProvenance, dateProvenance, now)
		},
		recordCollection: func(ownerID, receivableID string, amount money.Money, ledgerID string, key applicationledger.IdempotencyKey) (domainschedule.ReceivableCollection, error) {
			original, _ := money.New(35_000_00, money.MXN())
			receivable, _ := domainschedule.NewReceivable(receivableID, ownerID, "Debt", original, nil, domainschedule.Uncertain(), domainschedule.ExactAmount(), nil, now)
			_, collection, err := receivable.RecordCollection("collection-id", amount, ledgerID, now.Add(time.Hour))
			return collection, err
		},
	}
	handler := newAuthenticatedScheduleHandler(future)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, scheduleJSONRequest(http.MethodPost, "/api/v1/scheduled-flows", `{"amount_minor":788700,"currency":"MXN","direction":"inflow","financial_date":"2026-08-15","source_kind":"manual_expected_income","amount_provenance":"exact","date_provenance":"estimated","inclusion_eligibility":"eligible"}`))
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"amount_provenance":"exact"`) || !strings.Contains(response.Body.String(), `"date_provenance":"estimated"`) {
		t.Fatalf("scheduled flow = %d %s", response.Code, response.Body.String())
	}
	for _, forbiddenSource := range []string{"obligation", "receivable"} {
		response = httptest.NewRecorder()
		body := strings.Replace(`{"amount_minor":1,"currency":"MXN","direction":"outflow","financial_date":"2026-08-15","source_kind":"SOURCE","amount_provenance":"exact","date_provenance":"exact","inclusion_eligibility":"eligible"}`, "SOURCE", forbiddenSource, 1)
		handler.ServeHTTP(response, scheduleJSONRequest(http.MethodPost, "/api/v1/scheduled-flows", body))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("forbidden source %s status = %d body = %s", forbiddenSource, response.Code, response.Body.String())
		}
	}

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, scheduleJSONRequest(http.MethodPost, "/api/v1/receivables", `{"name":"Family business","original_amount_minor":3500000,"currency":"MXN","expected_date":null,"certainty":"uncertain","amount_provenance":"exact","date_provenance":null}`))
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"expected_date":null`) || !strings.Contains(response.Body.String(), `"certainty":"uncertain"`) || !strings.Contains(response.Body.String(), `"amount_provenance":"exact"`) {
		t.Fatalf("receivable = %d %s", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, scheduleJSONRequest(http.MethodPost, "/api/v1/receivables", `{"name":"Confirmed reimbursement","original_amount_minor":10000,"currency":"MXN","expected_date":"2026-08-20","certainty":"confirmed","amount_provenance":"exact","date_provenance":"estimated"}`))
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"certainty":"confirmed"`) || !strings.Contains(response.Body.String(), `"date_provenance":"estimated"`) {
		t.Fatalf("dated receivable = %d %s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, scheduleJSONRequest(http.MethodPost, "/api/v1/receivables/receivable-id/collections", `{"amount_minor":500000,"currency":"MXN","ledger_transaction_id":""}`))
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"collected_after_minor":500000`) || !strings.Contains(response.Body.String(), `"outstanding_after_minor":3000000`) {
		t.Fatalf("collection = %d %s", response.Code, response.Body.String())
	}
}

func TestScheduleHTTPStrictValidationAndConflictMapping(t *testing.T) {
	handler := newAuthenticatedScheduleHandler(&fakeSchedule{})
	for _, test := range []struct {
		name, path, body string
		want             int
	}{
		{name: "malformed", path: "/api/v1/obligations", body: `{`, want: 400},
		{name: "unknown field", path: "/api/v1/obligations", body: `{"name":"x","amount_minor":1,"currency":"MXN","recurrence":"one_time","start_date":"2026-08-15","owner_id":"attacker"}`, want: 400},
		{name: "trailing JSON", path: "/api/v1/receivables", body: `{"name":"x","original_amount_minor":1,"currency":"MXN","certainty":"uncertain","amount_provenance":"exact"}{}`, want: 400},
		{name: "zero amount", path: "/api/v1/scheduled-flows", body: `{"amount_minor":0,"currency":"MXN","direction":"inflow","financial_date":"2026-08-15","source_kind":"manual_expected_income","amount_provenance":"exact","date_provenance":"exact","inclusion_eligibility":"eligible"}`, want: 400},
		{name: "scenario provenance", path: "/api/v1/scheduled-flows", body: `{"amount_minor":1,"currency":"MXN","direction":"inflow","financial_date":"2026-08-15","source_kind":"manual_expected_income","amount_provenance":"exact","date_provenance":"assumed_by_scenario","inclusion_eligibility":"eligible"}`, want: 400},
		{name: "missing receivable amount provenance", path: "/api/v1/receivables", body: `{"name":"x","original_amount_minor":1,"currency":"MXN","certainty":"uncertain"}`, want: 400},
		{name: "dated receivable missing date provenance", path: "/api/v1/receivables", body: `{"name":"x","original_amount_minor":1,"currency":"MXN","expected_date":"2026-08-15","certainty":"confirmed","amount_provenance":"exact"}`, want: 400},
		{name: "undated receivable with date provenance", path: "/api/v1/receivables", body: `{"name":"x","original_amount_minor":1,"currency":"MXN","expected_date":null,"certainty":"uncertain","amount_provenance":"exact","date_provenance":"estimated"}`, want: 400},
		{name: "missing archive date", path: "/api/v1/obligations/id/archive", body: `{}`, want: 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, scheduleJSONRequest(http.MethodPost, test.path, test.body))
			if response.Code != test.want {
				t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
			}
		})
	}

	futureLimit := &fakeSchedule{expandObligation: func(string, string, financialdate.Date, financialdate.Date) ([]domainschedule.ScheduledCashFlow, error) {
		return nil, domainschedule.ErrOccurrenceLimitExceeded
	}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/obligations/id/occurrences?from=0001-01-01&to=9999-12-31", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "valid-token"})
	response := httptest.NewRecorder()
	newAuthenticatedScheduleHandler(futureLimit).ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("occurrence limit status = %d body = %s", response.Code, response.Body.String())
	}

	future := &fakeSchedule{recordCollection: func(string, string, money.Money, string, applicationledger.IdempotencyKey) (domainschedule.ReceivableCollection, error) {
		return domainschedule.ReceivableCollection{}, domainschedule.ErrCollectionExceedsAmount
	}}
	response = httptest.NewRecorder()
	newAuthenticatedScheduleHandler(future).ServeHTTP(response, scheduleJSONRequest(http.MethodPost, "/api/v1/receivables/id/collections", `{"amount_minor":1,"currency":"MXN","ledger_transaction_id":""}`))
	if response.Code != http.StatusConflict {
		t.Fatalf("over-collection status = %d", response.Code)
	}
}

type fakeSchedule struct {
	createObligation    func(string, string, money.Money, domainschedule.Recurrence, financialdate.Date, *financialdate.Date, applicationledger.IdempotencyKey) (domainschedule.Obligation, error)
	getObligation       func(string, string) (domainschedule.Obligation, error)
	listObligations     func(string) ([]domainschedule.Obligation, error)
	archiveObligation   func(string, string, financialdate.Date, applicationledger.IdempotencyKey) (domainschedule.Obligation, error)
	expandObligation    func(string, string, financialdate.Date, financialdate.Date) ([]domainschedule.ScheduledCashFlow, error)
	createScheduledFlow func(string, money.Money, domainschedule.Direction, financialdate.Date, domainschedule.SourceKind, domainschedule.AmountProvenance, domainschedule.DateProvenance, domainschedule.InclusionEligibility, applicationledger.IdempotencyKey) (domainschedule.ScheduledCashFlow, error)
	getScheduledFlow    func(string, string) (domainschedule.ScheduledCashFlow, error)
	listScheduledFlows  func(string) ([]domainschedule.ScheduledCashFlow, error)
	cancelScheduledFlow func(string, string, applicationledger.IdempotencyKey) (domainschedule.ScheduledCashFlow, error)
	createReceivable    func(string, string, money.Money, *financialdate.Date, domainschedule.Certainty, domainschedule.AmountProvenance, *domainschedule.DateProvenance, applicationledger.IdempotencyKey) (domainschedule.Receivable, error)
	getReceivable       func(string, string) (domainschedule.Receivable, error)
	listReceivables     func(string) ([]domainschedule.Receivable, error)
	recordCollection    func(string, string, money.Money, string, applicationledger.IdempotencyKey) (domainschedule.ReceivableCollection, error)
	cancelReceivable    func(string, string, applicationledger.IdempotencyKey) (domainschedule.Receivable, error)
}

func (f *fakeSchedule) CreateObligation(_ context.Context, ownerID, name string, amount money.Money, recurrence domainschedule.Recurrence, start financialdate.Date, end *financialdate.Date, key applicationledger.IdempotencyKey) (domainschedule.Obligation, error) {
	if f.createObligation == nil {
		return domainschedule.Obligation{}, errors.New("unexpected CreateObligation")
	}
	return f.createObligation(ownerID, name, amount, recurrence, start, end, key)
}
func (f *fakeSchedule) GetObligation(_ context.Context, ownerID, id string) (domainschedule.Obligation, error) {
	if f.getObligation == nil {
		return domainschedule.Obligation{}, errors.New("unexpected GetObligation")
	}
	return f.getObligation(ownerID, id)
}
func (f *fakeSchedule) ListObligations(_ context.Context, ownerID string) ([]domainschedule.Obligation, error) {
	if f.listObligations == nil {
		return nil, errors.New("unexpected ListObligations")
	}
	return f.listObligations(ownerID)
}
func (f *fakeSchedule) ArchiveObligation(_ context.Context, ownerID, id string, inactiveFrom financialdate.Date, key applicationledger.IdempotencyKey) (domainschedule.Obligation, error) {
	if f.archiveObligation == nil {
		return domainschedule.Obligation{}, errors.New("unexpected ArchiveObligation")
	}
	return f.archiveObligation(ownerID, id, inactiveFrom, key)
}
func (f *fakeSchedule) ExpandObligation(_ context.Context, ownerID, id string, from, to financialdate.Date) ([]domainschedule.ScheduledCashFlow, error) {
	if f.expandObligation == nil {
		return nil, errors.New("unexpected ExpandObligation")
	}
	return f.expandObligation(ownerID, id, from, to)
}
func (f *fakeSchedule) CreateManualScheduledFlow(_ context.Context, ownerID string, amount money.Money, direction domainschedule.Direction, date financialdate.Date, source domainschedule.SourceKind, amountProvenance domainschedule.AmountProvenance, dateProvenance domainschedule.DateProvenance, inclusion domainschedule.InclusionEligibility, key applicationledger.IdempotencyKey) (domainschedule.ScheduledCashFlow, error) {
	if f.createScheduledFlow == nil {
		return domainschedule.ScheduledCashFlow{}, errors.New("unexpected CreateManualScheduledFlow")
	}
	return f.createScheduledFlow(ownerID, amount, direction, date, source, amountProvenance, dateProvenance, inclusion, key)
}
func (f *fakeSchedule) GetScheduledFlow(_ context.Context, ownerID, id string) (domainschedule.ScheduledCashFlow, error) {
	if f.getScheduledFlow == nil {
		return domainschedule.ScheduledCashFlow{}, errors.New("unexpected GetScheduledFlow")
	}
	return f.getScheduledFlow(ownerID, id)
}
func (f *fakeSchedule) ListScheduledFlows(_ context.Context, ownerID string) ([]domainschedule.ScheduledCashFlow, error) {
	if f.listScheduledFlows == nil {
		return nil, errors.New("unexpected ListScheduledFlows")
	}
	return f.listScheduledFlows(ownerID)
}
func (f *fakeSchedule) CancelScheduledFlow(_ context.Context, ownerID, id string, key applicationledger.IdempotencyKey) (domainschedule.ScheduledCashFlow, error) {
	if f.cancelScheduledFlow == nil {
		return domainschedule.ScheduledCashFlow{}, errors.New("unexpected CancelScheduledFlow")
	}
	return f.cancelScheduledFlow(ownerID, id, key)
}
func (f *fakeSchedule) CreateReceivable(_ context.Context, ownerID, name string, amount money.Money, expectedDate *financialdate.Date, certainty domainschedule.Certainty, amountProvenance domainschedule.AmountProvenance, dateProvenance *domainschedule.DateProvenance, key applicationledger.IdempotencyKey) (domainschedule.Receivable, error) {
	if f.createReceivable == nil {
		return domainschedule.Receivable{}, errors.New("unexpected CreateReceivable")
	}
	return f.createReceivable(ownerID, name, amount, expectedDate, certainty, amountProvenance, dateProvenance, key)
}
func (f *fakeSchedule) GetReceivable(_ context.Context, ownerID, id string) (domainschedule.Receivable, error) {
	if f.getReceivable == nil {
		return domainschedule.Receivable{}, errors.New("unexpected GetReceivable")
	}
	return f.getReceivable(ownerID, id)
}
func (f *fakeSchedule) ListReceivables(_ context.Context, ownerID string) ([]domainschedule.Receivable, error) {
	if f.listReceivables == nil {
		return nil, errors.New("unexpected ListReceivables")
	}
	return f.listReceivables(ownerID)
}
func (f *fakeSchedule) RecordReceivableCollection(_ context.Context, ownerID, id string, amount money.Money, ledgerID string, key applicationledger.IdempotencyKey) (domainschedule.ReceivableCollection, error) {
	if f.recordCollection == nil {
		return domainschedule.ReceivableCollection{}, errors.New("unexpected RecordReceivableCollection")
	}
	return f.recordCollection(ownerID, id, amount, ledgerID, key)
}
func (f *fakeSchedule) CancelReceivable(_ context.Context, ownerID, id string, key applicationledger.IdempotencyKey) (domainschedule.Receivable, error) {
	if f.cancelReceivable == nil {
		return domainschedule.Receivable{}, errors.New("unexpected CancelReceivable")
	}
	return f.cancelReceivable(ownerID, id, key)
}

func newAuthenticatedScheduleHandler(future ScheduleService) http.Handler {
	owner, _ := auth.NewOwner("owner-id", "owner@example.com", "encoded-password-hash", time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC))
	authentication := &fakeAuthentication{authenticate: func(token string) (auth.Owner, error) {
		if token != "valid-token" {
			return auth.Owner{}, auth.ErrUnauthenticated
		}
		return owner, nil
	}}
	return NewHandler(authentication, nil, future, AuthConfig{AllowedOrigin: testOrigin})
}

func scheduleJSONRequest(method, path, body string) *http.Request {
	request := jsonRequest(method, path, body)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "valid-token"})
	request.Header.Set("Idempotency-Key", "schedule-http-test")
	return request
}

func mustHTTPDate(t *testing.T, value string) financialdate.Date {
	t.Helper()
	date, err := financialdate.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	return date
}
