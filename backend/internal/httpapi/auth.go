package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"runway/backend/internal/auth"
)

const (
	sessionCookieName = "runway_session"
	maximumJSONBytes  = 8 * 1024
)

var (
	errRequestBodyTooLarge  = errors.New("request body too large")
	errUnsupportedMediaType = errors.New("unsupported media type")
)

type authHandler struct {
	authentication AuthenticationService
	config         AuthConfig
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type identityResponse struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

func (h authHandler) login(w http.ResponseWriter, r *http.Request) {
	secureResponse(w)
	if !h.validOrigin(r) {
		writeError(w, http.StatusForbidden, "request origin is not allowed")
		return
	}
	var input loginRequest
	if err := decodeJSON(w, r, &input); err != nil {
		if errors.Is(err, errRequestBodyTooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body is too large")
			return
		}
		if errors.Is(err, errUnsupportedMediaType) {
			writeError(w, http.StatusUnsupportedMediaType, "content type must be application/json")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid JSON request")
		return
	}
	result, err := h.authentication.Login(r.Context(), input.Email, input.Password)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "authentication service unavailable")
		return
	}
	h.setSessionCookie(w, result.Token, result.ExpiresAt)
	writeJSON(w, http.StatusOK, identityResponse{ID: result.Owner.ID(), Email: result.Owner.Email()})
}

func (h authHandler) logout(w http.ResponseWriter, r *http.Request) {
	secureResponse(w)
	if !h.validOrigin(r) {
		writeError(w, http.StatusForbidden, "request origin is not allowed")
		return
	}
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		if err := h.authentication.Logout(r.Context(), cookie.Value); err != nil {
			writeError(w, http.StatusInternalServerError, "authentication service unavailable")
			return
		}
	}
	h.expireSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h authHandler) me(w http.ResponseWriter, r *http.Request) {
	secureResponse(w)
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	owner, err := h.authentication.Authenticate(r.Context(), cookie.Value)
	if errors.Is(err, auth.ErrUnauthenticated) {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "authentication service unavailable")
		return
	}
	writeJSON(w, http.StatusOK, identityResponse{ID: owner.ID(), Email: owner.Email()})
}

func (h authHandler) validOrigin(r *http.Request) bool {
	return r.Header.Get("Origin") == h.config.AllowedOrigin
}

func (h authHandler) setSessionCookie(w http.ResponseWriter, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   h.config.SessionMaxAgeSeconds,
		HttpOnly: true,
		Secure:   h.config.CookieSecure,
		SameSite: http.SameSiteStrictMode,
	})
}

func (h authHandler) expireSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.config.CookieSecure,
		SameSite: http.SameSiteStrictMode,
	})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return errUnsupportedMediaType
	}
	r.Body = http.MaxBytesReader(w, r.Body, maximumJSONBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var maximumBytesError *http.MaxBytesError
		if errors.As(err, &maximumBytesError) {
			return errRequestBodyTooLarge
		}
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON value")
	}
	return nil
}

func secureResponse(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
