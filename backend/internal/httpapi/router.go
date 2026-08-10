package httpapi

import (
	"context"
	"net/http"

	"runway/backend/internal/auth"
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

func NewHandler(authentication AuthenticationService, authConfig AuthConfig) http.Handler {
	handler := authHandler{authentication: authentication, config: authConfig}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", health)
	mux.HandleFunc("POST /api/v1/auth/login", handler.login)
	mux.HandleFunc("POST /api/v1/auth/logout", handler.logout)
	mux.HandleFunc("GET /api/v1/auth/me", handler.me)
	return mux
}
