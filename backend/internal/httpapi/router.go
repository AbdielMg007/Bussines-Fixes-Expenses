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

func NewHandler(authentication AuthenticationService, financial LedgerService, authConfig AuthConfig) http.Handler {
	authenticationHandler := authHandler{authentication: authentication, config: authConfig}
	financialHandler := ledgerHandler{authentication: authentication, ledger: financial, config: authConfig}
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
	return mux
}
