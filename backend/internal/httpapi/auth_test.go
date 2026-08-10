package httpapi

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"runway/backend/internal/auth"
)

const testOrigin = "https://runway.example"

func TestLoginSetsSecureServerSessionCookie(t *testing.T) {
	owner := mustOwner(t)
	expiresAt := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	service := &fakeAuthentication{
		login: func(_, _ string) (auth.LoginResult, error) {
			return auth.LoginResult{Owner: owner, Token: "secret-session-token", ExpiresAt: expiresAt}, nil
		},
	}
	request := jsonRequest(http.MethodPost, "/api/v1/auth/login", `{"email":"owner@example.com","password":"password value"}`)
	response := httptest.NewRecorder()
	newTestHandler(service).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d, want 1", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != sessionCookieName || cookie.Value != "secret-session-token" || cookie.Path != "/" {
		t.Fatalf("cookie identity = %+v", cookie)
	}
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie security attributes = %+v", cookie)
	}
	if cookie.MaxAge != 3600 || !cookie.Expires.Equal(expiresAt) {
		t.Fatalf("cookie expiration = MaxAge %d, Expires %s", cookie.MaxAge, cookie.Expires)
	}
	if strings.Contains(response.Body.String(), "password") || strings.Contains(response.Body.String(), "hash") {
		t.Fatalf("response contains sensitive password data: %s", response.Body.String())
	}
}

func TestLoginFailureDoesNotRevealAccountExistence(t *testing.T) {
	service := &fakeAuthentication{
		login: func(_, _ string) (auth.LoginResult, error) {
			return auth.LoginResult{}, auth.ErrInvalidCredentials
		},
	}
	handler := newTestHandler(service)
	responses := make([]*httptest.ResponseRecorder, 0, 2)
	for _, body := range []string{
		`{"email":"owner@example.com","password":"wrong password"}`,
		`{"email":"unknown@example.com","password":"wrong password"}`,
	} {
		request := jsonRequest(http.MethodPost, "/api/v1/auth/login", body)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		responses = append(responses, response)
	}
	if responses[0].Code != http.StatusUnauthorized || responses[1].Code != http.StatusUnauthorized {
		t.Fatalf("statuses = %d and %d, want 401", responses[0].Code, responses[1].Code)
	}
	if responses[0].Body.String() != responses[1].Body.String() {
		t.Fatalf("credential failures differ: %q vs %q", responses[0].Body.String(), responses[1].Body.String())
	}
}

func TestMeRequiresAndReturnsAuthenticatedIdentity(t *testing.T) {
	owner := mustOwner(t)
	service := &fakeAuthentication{
		authenticate: func(token string) (auth.Owner, error) {
			if token != "valid-token" {
				return auth.Owner{}, auth.ErrUnauthenticated
			}
			return owner, nil
		},
	}
	handler := newTestHandler(service)

	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", unauthenticated.Code)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "valid-token"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("authenticated status = %d, want 200", response.Code)
	}
	want := `{"id":"owner-id","email":"owner@example.com"}` + "\n"
	if response.Body.String() != want {
		t.Fatalf("body = %q, want %q", response.Body.String(), want)
	}
}

func TestLogoutInvalidatesSessionAndExpiresCookie(t *testing.T) {
	var loggedOutToken string
	service := &fakeAuthentication{
		logout: func(token string) error {
			loggedOutToken = token
			return nil
		},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	request.Header.Set("Origin", testOrigin)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "active-token"})
	response := httptest.NewRecorder()
	newTestHandler(service).ServeHTTP(response, request)

	if response.Code != http.StatusNoContent || loggedOutToken != "active-token" {
		t.Fatalf("status = %d, logged-out token = %q", response.Code, loggedOutToken)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge >= 0 || cookies[0].Value != "" {
		t.Fatalf("expired cookie = %+v", cookies)
	}
	if !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].Path != "/" {
		t.Fatalf("expired cookie security attributes = %+v", cookies[0])
	}

	alreadyLoggedOut := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	request.Header.Set("Origin", testOrigin)
	newTestHandler(service).ServeHTTP(alreadyLoggedOut, request)
	if alreadyLoggedOut.Code != http.StatusNoContent {
		t.Fatalf("already-logged-out status = %d, want 204", alreadyLoggedOut.Code)
	}
}

func TestStateChangingRequestsRequireExactOrigin(t *testing.T) {
	service := &fakeAuthentication{}
	for _, origin := range []string{"", "null", "https://attacker.example", testOrigin + "/"} {
		t.Run(origin, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Origin", origin)
			response := httptest.NewRecorder()
			newTestHandler(service).ServeHTTP(response, request)
			if response.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", response.Code)
			}
		})
	}
}

func TestLoginRejectsInvalidRequestBodies(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		wantStatus  int
	}{
		{name: "wrong content type", contentType: "text/plain", body: `{}`, wantStatus: http.StatusUnsupportedMediaType},
		{name: "malformed JSON", contentType: "application/json", body: `{`, wantStatus: http.StatusBadRequest},
		{name: "unknown field", contentType: "application/json", body: `{"email":"a","password":"b","extra":true}`, wantStatus: http.StatusBadRequest},
		{name: "multiple values", contentType: "application/json", body: `{} {}`, wantStatus: http.StatusBadRequest},
		{name: "too large", contentType: "application/json", body: `{"email":"` + strings.Repeat("a", maximumJSONBytes) + `"}`, wantStatus: http.StatusRequestEntityTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(test.body))
			request.Header.Set("Content-Type", test.contentType)
			request.Header.Set("Origin", testOrigin)
			response := httptest.NewRecorder()
			newTestHandler(&fakeAuthentication{}).ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, test.wantStatus, response.Body.String())
			}
		})
	}
}

type fakeAuthentication struct {
	login        func(string, string) (auth.LoginResult, error)
	authenticate func(string) (auth.Owner, error)
	logout       func(string) error
}

func (f *fakeAuthentication) Login(_ context.Context, email, password string) (auth.LoginResult, error) {
	if f.login == nil {
		return auth.LoginResult{}, errors.New("unexpected login")
	}
	return f.login(email, password)
}

func (f *fakeAuthentication) Authenticate(_ context.Context, token string) (auth.Owner, error) {
	if f.authenticate == nil {
		return auth.Owner{}, auth.ErrUnauthenticated
	}
	return f.authenticate(token)
}

func (f *fakeAuthentication) Logout(_ context.Context, token string) error {
	if f.logout == nil {
		return nil
	}
	return f.logout(token)
}

func newTestHandler(service AuthenticationService) http.Handler {
	return NewHandler(service, AuthConfig{
		AllowedOrigin:        testOrigin,
		CookieSecure:         true,
		SessionMaxAgeSeconds: 3600,
	})
}

func jsonRequest(method, path, body string) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", testOrigin)
	return request
}

func mustOwner(t *testing.T) auth.Owner {
	t.Helper()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	owner, err := auth.NewOwner("owner-id", "owner@example.com", "encoded-password-hash", now)
	if err != nil {
		t.Fatalf("NewOwner() error = %v", err)
	}
	return owner
}
