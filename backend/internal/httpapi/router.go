package httpapi

import (
	"context"
	"net/http"
	"time"

	"runway/backend/internal/auth"
	"runway/backend/internal/domain/account"
	"runway/backend/internal/domain/financialdate"
	domainledger "runway/backend/internal/domain/ledger"
	"runway/backend/internal/domain/money"
	domainprojection "runway/backend/internal/domain/projection"
	domainschedule "runway/backend/internal/domain/schedule"
	applicationledger "runway/backend/internal/ledger"
)

type AuthenticationService interface {
	Login(context.Context, string, string) (auth.LoginResult, error)
	Authenticate(context.Context, string) (auth.Owner, error)
	Logout(context.Context, string) error
}

type AuthConfig struct {
	AllowedOrigin        string
	CookieSecure         bool
	SessionMaxAgeSeconds int
}

type LedgerService interface {
	CreateAccount(context.Context, string, string, account.AccountType, money.Currency) (account.Account, error)
	GetAccount(context.Context, string, string) (account.Account, error)
	ListAccounts(context.Context, string) ([]account.Account, error)
	ArchiveAccount(context.Context, string, string) (account.Account, error)
	PostTransaction(context.Context, string, string, domainledger.Effect, money.Money, financialdate.Date, string, applicationledger.IdempotencyKey) (domainledger.Transaction, error)
	ListTransactions(context.Context, string, string) ([]domainledger.Transaction, error)
	CreateSnapshot(context.Context, string, string, money.Balance, time.Time, int64, applicationledger.IdempotencyKey) (domainledger.BalanceSnapshot, error)
	CurrentBalance(context.Context, string, string) (applicationledger.BalanceResult, error)
	CreateTransfer(context.Context, string, string, string, money.Money, financialdate.Date, string, applicationledger.IdempotencyKey) (applicationledger.TransferResult, error)
}

type ScheduleService interface {
	CreateObligation(context.Context, string, string, money.Money, domainschedule.Recurrence, financialdate.Date, *financialdate.Date, applicationledger.IdempotencyKey) (domainschedule.Obligation, error)
	GetObligation(context.Context, string, string) (domainschedule.Obligation, error)
	ListObligations(context.Context, string) ([]domainschedule.Obligation, error)
	ArchiveObligation(context.Context, string, string, financialdate.Date, applicationledger.IdempotencyKey) (domainschedule.Obligation, error)
	ExpandObligation(context.Context, string, string, financialdate.Date, financialdate.Date) ([]domainschedule.ScheduledCashFlow, error)
	CreateManualScheduledFlow(context.Context, string, money.Money, domainschedule.Direction, financialdate.Date, domainschedule.SourceKind, domainschedule.AmountProvenance, domainschedule.DateProvenance, domainschedule.InclusionEligibility, applicationledger.IdempotencyKey) (domainschedule.ScheduledCashFlow, error)
	GetScheduledFlow(context.Context, string, string) (domainschedule.ScheduledCashFlow, error)
	ListScheduledFlows(context.Context, string) ([]domainschedule.ScheduledCashFlow, error)
	CancelScheduledFlow(context.Context, string, string, applicationledger.IdempotencyKey) (domainschedule.ScheduledCashFlow, error)
	CreateReceivable(context.Context, string, string, money.Money, *financialdate.Date, domainschedule.Certainty, domainschedule.AmountProvenance, *domainschedule.DateProvenance, applicationledger.IdempotencyKey) (domainschedule.Receivable, error)
	GetReceivable(context.Context, string, string) (domainschedule.Receivable, error)
	ListReceivables(context.Context, string) ([]domainschedule.Receivable, error)
	RecordReceivableCollection(context.Context, string, string, money.Money, string, applicationledger.IdempotencyKey) (domainschedule.ReceivableCollection, error)
	CancelReceivable(context.Context, string, string, applicationledger.IdempotencyKey) (domainschedule.Receivable, error)
}

type ProjectionService interface {
	GetPolicy(context.Context, string) (domainprojection.Policy, error)
	ReplacePolicy(context.Context, string, money.Currency, int, money.Money, string, domainprojection.AccountSelection, domainprojection.InflowPolicy, domainprojection.SameDayOrder) (domainprojection.Policy, error)
	CalculateBaseline(context.Context, string) (domainprojection.Result, error)
}

func NewHandler(authentication AuthenticationService, financial LedgerService, future ScheduleService, authConfig AuthConfig, projectionServices ...ProjectionService) http.Handler {
	var projections ProjectionService
	if len(projectionServices) > 0 {
		projections = projectionServices[0]
	}
	authenticationHandler := authHandler{authentication: authentication, config: authConfig}
	financialHandler := ledgerHandler{authentication: authentication, ledger: financial, config: authConfig}
	futureHandler := scheduleHandler{authentication: authentication, schedule: future, config: authConfig}
	projectionHandler := projectionHandler{authentication: authentication, projection: projections, config: authConfig}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", health)
	mux.HandleFunc("POST /api/v1/auth/login", authenticationHandler.login)
	mux.HandleFunc("POST /api/v1/auth/logout", authenticationHandler.logout)
	mux.HandleFunc("GET /api/v1/auth/me", authenticationHandler.me)
	mux.HandleFunc("POST /api/v1/accounts", financialHandler.createAccount)
	mux.HandleFunc("GET /api/v1/accounts", financialHandler.listAccounts)
	mux.HandleFunc("GET /api/v1/accounts/{id}", financialHandler.getAccount)
	mux.HandleFunc("POST /api/v1/accounts/{id}/archive", financialHandler.archiveAccount)
	mux.HandleFunc("POST /api/v1/accounts/{id}/transactions", financialHandler.postTransaction)
	mux.HandleFunc("GET /api/v1/accounts/{id}/transactions", financialHandler.listTransactions)
	mux.HandleFunc("POST /api/v1/accounts/{id}/snapshots", financialHandler.createSnapshot)
	mux.HandleFunc("GET /api/v1/accounts/{id}/balance", financialHandler.getBalance)
	mux.HandleFunc("POST /api/v1/transfers", financialHandler.createTransfer)
	mux.HandleFunc("POST /api/v1/obligations", futureHandler.createObligation)
	mux.HandleFunc("GET /api/v1/obligations", futureHandler.listObligations)
	mux.HandleFunc("GET /api/v1/obligations/{id}", futureHandler.getObligation)
	mux.HandleFunc("POST /api/v1/obligations/{id}/archive", futureHandler.archiveObligation)
	mux.HandleFunc("GET /api/v1/obligations/{id}/occurrences", futureHandler.expandObligation)
	mux.HandleFunc("POST /api/v1/scheduled-flows", futureHandler.createScheduledFlow)
	mux.HandleFunc("GET /api/v1/scheduled-flows", futureHandler.listScheduledFlows)
	mux.HandleFunc("GET /api/v1/scheduled-flows/{id}", futureHandler.getScheduledFlow)
	mux.HandleFunc("POST /api/v1/scheduled-flows/{id}/cancel", futureHandler.cancelScheduledFlow)
	mux.HandleFunc("POST /api/v1/receivables", futureHandler.createReceivable)
	mux.HandleFunc("GET /api/v1/receivables", futureHandler.listReceivables)
	mux.HandleFunc("GET /api/v1/receivables/{id}", futureHandler.getReceivable)
	mux.HandleFunc("POST /api/v1/receivables/{id}/collections", futureHandler.recordReceivableCollection)
	mux.HandleFunc("POST /api/v1/receivables/{id}/cancel", futureHandler.cancelReceivable)
	mux.HandleFunc("GET /api/v1/projection-policy", projectionHandler.getPolicy)
	mux.HandleFunc("PUT /api/v1/projection-policy", projectionHandler.replacePolicy)
	mux.HandleFunc("GET /api/v1/projection", projectionHandler.calculate)
	return mux
}
