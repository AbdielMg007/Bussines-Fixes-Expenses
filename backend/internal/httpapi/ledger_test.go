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
	"runway/backend/internal/domain/account"
	"runway/backend/internal/domain/financialdate"
	domainledger "runway/backend/internal/domain/ledger"
	"runway/backend/internal/domain/money"
	applicationledger "runway/backend/internal/ledger"
)

func TestLedgerEndpointsRequireAuthenticationAndExactOrigin(t *testing.T) {
	financial := &fakeLedger{}
	unauthenticated := NewHandler(&fakeAuthentication{}, financial, AuthConfig{AllowedOrigin: testOrigin})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/accounts", nil)
	response := httptest.NewRecorder()
	unauthenticated.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", response.Code)
	}

	called := false
	financial.createAccount = func(string, string, account.AccountType, money.Currency) (account.Account, error) {
		called = true
		return account.Account{}, nil
	}
	handler := newAuthenticatedLedgerHandler(financial)
	request = httptest.NewRequest(http.MethodPost, "/api/v1/accounts", strings.NewReader(`{"name":"Bank","type":"bank","currency":"MXN"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://attacker.example")
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "valid-token"})
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || called {
		t.Fatalf("wrong-origin status = %d, called = %t", response.Code, called)
	}
}

func TestAccountHTTPFlowUsesAuthenticatedOwner(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	var receivedOwner string
	financial := &fakeLedger{
		createAccount: func(ownerID, name string, accountType account.AccountType, currency money.Currency) (account.Account, error) {
			receivedOwner = ownerID
			return account.New("account-id", ownerID, name, accountType, currency, now)
		},
		getAccount: func(ownerID, accountID string) (account.Account, error) {
			if accountID == "someone-elses-account" {
				return account.Account{}, applicationledger.ErrNotFound
			}
			return account.New(accountID, ownerID, "Bank", account.Bank(), money.MXN(), now)
		},
	}
	handler := newAuthenticatedLedgerHandler(financial)
	request := ledgerJSONRequest(http.MethodPost, "/api/v1/accounts", `{"name":"Primary bank","type":"bank","currency":"MXN"}`)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || receivedOwner != "owner-id" {
		t.Fatalf("create status = %d owner = %q body = %s", response.Code, receivedOwner, response.Body.String())
	}
	var created accountResponse
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil || created.ID != "account-id" || created.Status != "active" {
		t.Fatalf("created account = %+v, error = %v", created, err)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/accounts/someone-elses-account", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "valid-token"})
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("cross-owner resource status = %d, want 404", response.Code)
	}
}

func TestTransactionAndTransferHTTPFlows(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	date, _ := financialdate.Parse("2026-08-10")
	financial := &fakeLedger{
		postTransaction: func(ownerID, accountID string, effect domainledger.Effect, amount money.Money, gotDate financialdate.Date, memo string, key applicationledger.IdempotencyKey) (domainledger.Transaction, error) {
			if ownerID != "owner-id" || accountID != "bank" || effect != domainledger.AssetOutflow() || amount.MinorUnits() != 1250 || gotDate != date {
				return domainledger.Transaction{}, errors.New("unexpected transaction command")
			}
			return domainledger.NewTransaction("tx-id", ownerID, accountID, domainledger.ManualKind(), amount, effect, gotDate, 7, memo, "", now)
		},
		createTransfer: func(ownerID, sourceID, destinationID string, amount money.Money, gotDate financialdate.Date, memo string, key applicationledger.IdempotencyKey) (applicationledger.TransferResult, error) {
			transfer, err := domainledger.NewTransfer("transfer-id", ownerID, sourceID, destinationID, amount, gotDate, memo, now)
			if err != nil {
				return applicationledger.TransferResult{}, err
			}
			source, _ := domainledger.NewTransaction("source-tx", ownerID, sourceID, domainledger.TransferKind(), amount, domainledger.AssetOutflow(), gotDate, 8, memo, transfer.ID(), now)
			destination, _ := domainledger.NewTransaction("destination-tx", ownerID, destinationID, domainledger.TransferKind(), amount, domainledger.LiabilityPayment(), gotDate, 9, memo, transfer.ID(), now)
			return applicationledger.TransferResult{Transfer: transfer, SourceTransaction: source, DestinationTransaction: destination}, nil
		},
	}
	handler := newAuthenticatedLedgerHandler(financial)

	request := ledgerJSONRequest(http.MethodPost, "/api/v1/accounts/bank/transactions", `{"effect":"asset_outflow","amount_minor":1250,"currency":"MXN","financial_date":"2026-08-10","memo":"Groceries"}`)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"ledger_sequence":7`) {
		t.Fatalf("transaction response = %d %s", response.Code, response.Body.String())
	}

	request = ledgerJSONRequest(http.MethodPost, "/api/v1/transfers", `{"source_account_id":"bank","destination_account_id":"card","amount_minor":1250,"currency":"MXN","financial_date":"2026-08-10","memo":"Card payment"}`)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"effect":"liability_payment"`) || !strings.Contains(response.Body.String(), `"id":"transfer-id"`) {
		t.Fatalf("transfer response = %d %s", response.Code, response.Body.String())
	}
}

func TestLedgerReadSnapshotAndArchiveHTTPFlows(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	financialAccount, _ := account.New("bank", "owner-id", "Bank", account.Bank(), money.MXN(), now)
	amount, _ := money.New(500, money.MXN())
	date, _ := financialdate.Parse("2026-08-10")
	transaction, _ := domainledger.NewTransaction("tx", "owner-id", "bank", domainledger.ManualKind(), amount, domainledger.AssetOutflow(), date, 4, "", "", now)
	financial := &fakeLedger{
		listAccounts: func(ownerID string) ([]account.Account, error) {
			return []account.Account{financialAccount}, nil
		},
		listTransactions: func(ownerID, accountID string) ([]domainledger.Transaction, error) {
			return []domainledger.Transaction{transaction}, nil
		},
		createSnapshot: func(ownerID, accountID string, balance money.Balance, effectiveAt time.Time, cutoff int64, key applicationledger.IdempotencyKey) (domainledger.BalanceSnapshot, error) {
			return domainledger.NewBalanceSnapshot("snapshot", ownerID, accountID, balance, effectiveAt, cutoff, now)
		},
		currentBalance: func(ownerID, accountID string) (applicationledger.BalanceResult, error) {
			balance, _ := money.NewBalance(-250, money.MXN())
			return applicationledger.BalanceResult{AccountID: accountID, Balance: balance, SnapshotID: "snapshot", SnapshotCutoffSequence: 4, LastAppliedSequence: 5, AppliedTransactions: 1}, nil
		},
		archiveAccount: func(ownerID, accountID string) (account.Account, error) {
			return financialAccount.Archive(now.Add(time.Hour))
		},
	}
	handler := newAuthenticatedLedgerHandler(financial)

	for _, path := range []string{"/api/v1/accounts", "/api/v1/accounts/bank/transactions"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "valid-token"})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d body = %s", path, response.Code, response.Body.String())
		}
	}

	request := ledgerJSONRequest(http.MethodPost, "/api/v1/accounts/bank/snapshots", `{"balance_minor":1000,"currency":"MXN","effective_at":"2026-08-10T12:00:00Z","cutoff_sequence":4}`)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"cutoff_sequence":4`) {
		t.Fatalf("snapshot response = %d %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/accounts/bank/balance", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "valid-token"})
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"balance_minor":-250`) {
		t.Fatalf("balance response = %d %s", response.Code, response.Body.String())
	}

	request = ledgerJSONRequest(http.MethodPost, "/api/v1/accounts/bank/archive", `{}`)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"archived"`) {
		t.Fatalf("archive response = %d %s", response.Code, response.Body.String())
	}
}

func TestLedgerHTTPRejectsMalformedAndInvalidRequests(t *testing.T) {
	handler := newAuthenticatedLedgerHandler(&fakeLedger{})
	tests := []struct {
		name string
		path string
		body string
		want int
	}{
		{name: "malformed", path: "/api/v1/accounts", body: `{`, want: http.StatusBadRequest},
		{name: "unknown account field", path: "/api/v1/accounts", body: `{"name":"Bank","type":"bank","currency":"MXN","owner_id":"attacker"}`, want: http.StatusBadRequest},
		{name: "invalid type", path: "/api/v1/accounts", body: `{"name":"Bank","type":"investment","currency":"MXN"}`, want: http.StatusBadRequest},
		{name: "negative amount", path: "/api/v1/accounts/bank/transactions", body: `{"effect":"asset_outflow","amount_minor":-1,"currency":"MXN","financial_date":"2026-08-10","memo":""}`, want: http.StatusBadRequest},
		{name: "zero amount", path: "/api/v1/accounts/bank/transactions", body: `{"effect":"asset_outflow","amount_minor":0,"currency":"MXN","financial_date":"2026-08-10","memo":""}`, want: http.StatusBadRequest},
		{name: "unsupported currency", path: "/api/v1/transfers", body: `{"source_account_id":"a","destination_account_id":"b","amount_minor":1,"currency":"USD","financial_date":"2026-08-10","memo":""}`, want: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, ledgerJSONRequest(http.MethodPost, test.path, test.body))
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, test.want, response.Body.String())
			}
		})
	}
}

func TestFinancialMutationHTTPRequiresIdempotencyKey(t *testing.T) {
	handler := newAuthenticatedLedgerHandler(&fakeLedger{})
	tests := []struct {
		path string
		body string
	}{
		{path: "/api/v1/accounts/bank/transactions", body: `{"effect":"asset_inflow","amount_minor":1,"currency":"MXN","financial_date":"2026-08-10","memo":""}`},
		{path: "/api/v1/accounts/bank/snapshots", body: `{"balance_minor":0,"currency":"MXN","effective_at":"2026-08-10T12:00:00Z","cutoff_sequence":0}`},
		{path: "/api/v1/transfers", body: `{"source_account_id":"bank","destination_account_id":"cash","amount_minor":1,"currency":"MXN","financial_date":"2026-08-10","memo":""}`},
	}
	for _, test := range tests {
		request := ledgerJSONRequest(http.MethodPost, test.path, test.body)
		request.Header.Del("Idempotency-Key")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("POST %s without key status = %d, want 400; body = %s", test.path, response.Code, response.Body.String())
		}
	}
}

func TestSnapshotHTTPRequiresFieldsButAcceptsExplicitZero(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	called := 0
	financial := &fakeLedger{createSnapshot: func(ownerID, accountID string, balance money.Balance, effectiveAt time.Time, cutoff int64, _ applicationledger.IdempotencyKey) (domainledger.BalanceSnapshot, error) {
		called++
		if balance.MinorUnits() != 0 || cutoff != 0 {
			return domainledger.BalanceSnapshot{}, errors.New("explicit zeros were not preserved")
		}
		return domainledger.NewBalanceSnapshot("snapshot-zero", ownerID, accountID, balance, effectiveAt, cutoff, now)
	}}
	handler := newAuthenticatedLedgerHandler(financial)

	for _, test := range []struct {
		name string
		body string
	}{
		{name: "omitted balance", body: `{"currency":"MXN","effective_at":"2026-08-10T12:00:00Z","cutoff_sequence":0}`},
		{name: "omitted cutoff", body: `{"balance_minor":0,"currency":"MXN","effective_at":"2026-08-10T12:00:00Z"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, ledgerJSONRequest(http.MethodPost, "/api/v1/accounts/bank/snapshots", test.body))
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body = %s", response.Code, response.Body.String())
			}
		})
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, ledgerJSONRequest(http.MethodPost, "/api/v1/accounts/bank/snapshots", `{"balance_minor":0,"currency":"MXN","effective_at":"2026-08-10T12:00:00Z","cutoff_sequence":0}`))
	if response.Code != http.StatusCreated || called != 1 || !strings.Contains(response.Body.String(), `"balance_minor":0`) || !strings.Contains(response.Body.String(), `"cutoff_sequence":0`) {
		t.Fatalf("explicit-zero snapshot = %d %s, calls = %d", response.Code, response.Body.String(), called)
	}
}

func TestLedgerHTTPMapsIdempotencyConflictAndLiabilityOverpaymentToConflict(t *testing.T) {
	for _, domainError := range []error{applicationledger.ErrIdempotencyConflict, applicationledger.ErrLiabilityOverpayment} {
		financial := &fakeLedger{postTransaction: func(string, string, domainledger.Effect, money.Money, financialdate.Date, string, applicationledger.IdempotencyKey) (domainledger.Transaction, error) {
			return domainledger.Transaction{}, domainError
		}}
		response := httptest.NewRecorder()
		newAuthenticatedLedgerHandler(financial).ServeHTTP(response, ledgerJSONRequest(http.MethodPost, "/api/v1/accounts/card/transactions", `{"effect":"liability_payment","amount_minor":1,"currency":"MXN","financial_date":"2026-08-10","memo":""}`))
		if response.Code != http.StatusConflict {
			t.Fatalf("error %v status = %d, want 409", domainError, response.Code)
		}
	}
}

type fakeLedger struct {
	createAccount    func(string, string, account.AccountType, money.Currency) (account.Account, error)
	getAccount       func(string, string) (account.Account, error)
	listAccounts     func(string) ([]account.Account, error)
	archiveAccount   func(string, string) (account.Account, error)
	postTransaction  func(string, string, domainledger.Effect, money.Money, financialdate.Date, string, applicationledger.IdempotencyKey) (domainledger.Transaction, error)
	listTransactions func(string, string) ([]domainledger.Transaction, error)
	createSnapshot   func(string, string, money.Balance, time.Time, int64, applicationledger.IdempotencyKey) (domainledger.BalanceSnapshot, error)
	currentBalance   func(string, string) (applicationledger.BalanceResult, error)
	createTransfer   func(string, string, string, money.Money, financialdate.Date, string, applicationledger.IdempotencyKey) (applicationledger.TransferResult, error)
}

func (f *fakeLedger) CreateAccount(_ context.Context, ownerID, name string, accountType account.AccountType, currency money.Currency) (account.Account, error) {
	if f.createAccount == nil {
		return account.Account{}, errors.New("unexpected CreateAccount")
	}
	return f.createAccount(ownerID, name, accountType, currency)
}

func (f *fakeLedger) GetAccount(_ context.Context, ownerID, accountID string) (account.Account, error) {
	if f.getAccount == nil {
		return account.Account{}, errors.New("unexpected GetAccount")
	}
	return f.getAccount(ownerID, accountID)
}

func (f *fakeLedger) ListAccounts(_ context.Context, ownerID string) ([]account.Account, error) {
	if f.listAccounts == nil {
		return nil, errors.New("unexpected ListAccounts")
	}
	return f.listAccounts(ownerID)
}

func (f *fakeLedger) ArchiveAccount(_ context.Context, ownerID, accountID string) (account.Account, error) {
	if f.archiveAccount == nil {
		return account.Account{}, errors.New("unexpected ArchiveAccount")
	}
	return f.archiveAccount(ownerID, accountID)
}

func (f *fakeLedger) PostTransaction(_ context.Context, ownerID, accountID string, effect domainledger.Effect, amount money.Money, date financialdate.Date, memo string, key applicationledger.IdempotencyKey) (domainledger.Transaction, error) {
	if f.postTransaction == nil {
		return domainledger.Transaction{}, errors.New("unexpected PostTransaction")
	}
	return f.postTransaction(ownerID, accountID, effect, amount, date, memo, key)
}

func (f *fakeLedger) ListTransactions(_ context.Context, ownerID, accountID string) ([]domainledger.Transaction, error) {
	if f.listTransactions == nil {
		return nil, errors.New("unexpected ListTransactions")
	}
	return f.listTransactions(ownerID, accountID)
}

func (f *fakeLedger) CreateSnapshot(_ context.Context, ownerID, accountID string, balance money.Balance, effectiveAt time.Time, cutoff int64, key applicationledger.IdempotencyKey) (domainledger.BalanceSnapshot, error) {
	if f.createSnapshot == nil {
		return domainledger.BalanceSnapshot{}, errors.New("unexpected CreateSnapshot")
	}
	return f.createSnapshot(ownerID, accountID, balance, effectiveAt, cutoff, key)
}

func (f *fakeLedger) CurrentBalance(_ context.Context, ownerID, accountID string) (applicationledger.BalanceResult, error) {
	if f.currentBalance == nil {
		return applicationledger.BalanceResult{}, errors.New("unexpected CurrentBalance")
	}
	return f.currentBalance(ownerID, accountID)
}

func (f *fakeLedger) CreateTransfer(_ context.Context, ownerID, sourceID, destinationID string, amount money.Money, date financialdate.Date, memo string, key applicationledger.IdempotencyKey) (applicationledger.TransferResult, error) {
	if f.createTransfer == nil {
		return applicationledger.TransferResult{}, errors.New("unexpected CreateTransfer")
	}
	return f.createTransfer(ownerID, sourceID, destinationID, amount, date, memo, key)
}

func newAuthenticatedLedgerHandler(financial LedgerService) http.Handler {
	owner, err := auth.NewOwner(
		"owner-id",
		"owner@example.com",
		"encoded-password-hash",
		time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC),
	)
	if err != nil {
		panic(err)
	}
	authentication := &fakeAuthentication{authenticate: func(token string) (auth.Owner, error) {
		if token != "valid-token" {
			return auth.Owner{}, auth.ErrUnauthenticated
		}
		return owner, nil
	}}
	return NewHandler(authentication, financial, AuthConfig{AllowedOrigin: testOrigin})
}

func ledgerJSONRequest(method, path, body string) *http.Request {
	request := jsonRequest(method, path, body)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "valid-token"})
	request.Header.Set("Idempotency-Key", "http-test-key")
	return request
}
