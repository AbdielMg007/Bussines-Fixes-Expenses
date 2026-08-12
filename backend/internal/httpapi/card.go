package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"runway/backend/internal/auth"
	app "runway/backend/internal/card"
	domain "runway/backend/internal/domain/card"
	"runway/backend/internal/domain/financialdate"
	"runway/backend/internal/domain/money"
	ledger "runway/backend/internal/ledger"
	"time"
)

type CardService interface {
	RegisterStatement(context.Context, string, app.StatementInput) (domain.Statement, error)
	ListStatements(context.Context, string, string) ([]domain.Statement, error)
	GetStatement(context.Context, string, string) (domain.Statement, error)
	GetIntent(context.Context, string, string) (domain.PaymentIntent, error)
	ReplaceIntent(context.Context, string, string, money.Money, financialdate.Date) (domain.PaymentIntent, error)
	CancelIntent(context.Context, string, string) (domain.PaymentIntent, error)
}
type cardHandler struct {
	authentication AuthenticationService
	cards          CardService
	config         AuthConfig
}
type statementRequest struct {
	CycleStart string `json:"cycle_start"`
	CycleEnd   string `json:"cycle_end"`
	Authority  string `json:"authority"`
	Balance    *int64 `json:"statement_balance_minor"`
	Minimum    *int64 `json:"minimum_payment_minor"`
	Avoid      *int64 `json:"payment_to_avoid_interest_minor"`
	Currency   string `json:"currency"`
	Due        string `json:"due_date"`
}
type intentRequest struct {
	Amount   int64  `json:"amount_minor"`
	Currency string `json:"currency"`
	Planned  string `json:"planned_date"`
}
type statementResponse struct {
	ID, AccountID, CycleID, Authority, Currency, DueDate, SupersededByID string     `json:"-"`
	Revision                                                             int64      `json:"-"`
	Balance                                                              int64      `json:"-"`
	Minimum, Avoid                                                       *int64     `json:"-"`
	CreatedAt                                                            time.Time  `json:"-"`
	SupersededAt                                                         *time.Time `json:"-"`
}

func (h cardHandler) register(w http.ResponseWriter, r *http.Request) {
	owner, ok := h.mutate(w, r)
	if !ok {
		return
	}
	key, ok := parseIdempotencyKey(w, r)
	if !ok {
		return
	}
	var q statementRequest
	if !decodeLedgerRequest(w, r, &q) {
		return
	}
	start, e := financialdate.Parse(q.CycleStart)
	if e != nil {
		writeError(w, 400, "invalid request")
		return
	}
	end, e := financialdate.Parse(q.CycleEnd)
	if e != nil {
		writeError(w, 400, "invalid request")
		return
	}
	due, e := financialdate.Parse(q.Due)
	if e != nil {
		writeError(w, 400, "invalid request")
		return
	}
	cur, e := money.ParseCurrency(q.Currency)
	if e != nil {
		writeError(w, 400, "invalid request")
		return
	}
	if q.Balance == nil {
		writeError(w, 400, "invalid request")
		return
	}
	bal, e := money.New(*q.Balance, cur)
	if e != nil {
		writeError(w, 400, "invalid request")
		return
	}
	var min, av *money.Money
	if q.Minimum != nil {
		x, e := money.New(*q.Minimum, cur)
		if e != nil {
			writeError(w, 400, "invalid request")
			return
		}
		min = &x
	}
	if q.Avoid != nil {
		x, e := money.New(*q.Avoid, cur)
		if e != nil {
			writeError(w, 400, "invalid request")
			return
		}
		av = &x
	}
	s, e := h.cards.RegisterStatement(r.Context(), owner, app.StatementInput{AccountID: r.PathValue("account_id"), Start: start, End: end, Due: due, Authority: domain.Authority(q.Authority), Balance: bal, Minimum: min, AvoidInterest: av, Mutation: ledger.MutationIdentity{Key: key, Fingerprint: ledger.CanonicalFingerprint("v1", "register_credit_card_statement", owner, r.PathValue("account_id"), q.CycleStart, q.CycleEnd, q.Authority, q.Due, q.Currency, itoa(*q.Balance), ptrInt(q.Minimum), ptrInt(q.Avoid))}})
	if e != nil {
		h.err(w, e)
		return
	}
	writeJSON(w, 201, mapStatement(s))
}
func (h cardHandler) list(w http.ResponseWriter, r *http.Request) {
	o, ok := h.auth(w, r)
	if !ok {
		return
	}
	x, e := h.cards.ListStatements(r.Context(), o, r.PathValue("account_id"))
	if e != nil {
		h.err(w, e)
		return
	}
	out := make([]any, 0, len(x))
	for _, v := range x {
		out = append(out, mapStatement(v))
	}
	writeJSON(w, 200, out)
}
func (h cardHandler) getStatement(w http.ResponseWriter, r *http.Request) {
	o, ok := h.auth(w, r)
	if !ok {
		return
	}
	x, e := h.cards.GetStatement(r.Context(), o, r.PathValue("statement_id"))
	if e != nil {
		h.err(w, e)
		return
	}
	writeJSON(w, 200, mapStatement(x))
}
func (h cardHandler) getIntent(w http.ResponseWriter, r *http.Request) {
	o, ok := h.auth(w, r)
	if !ok {
		return
	}
	x, e := h.cards.GetIntent(r.Context(), o, r.PathValue("cycle_id"))
	if e != nil {
		h.err(w, e)
		return
	}
	writeJSON(w, 200, mapIntent(x))
}
func (h cardHandler) putIntent(w http.ResponseWriter, r *http.Request) {
	o, ok := h.mutate(w, r)
	if !ok {
		return
	}
	var q intentRequest
	if !decodeLedgerRequest(w, r, &q) {
		return
	}
	c, e := money.ParseCurrency(q.Currency)
	if e != nil {
		writeError(w, 400, "invalid request")
		return
	}
	m, e := money.New(q.Amount, c)
	if e != nil {
		writeError(w, 400, "invalid request")
		return
	}
	d, e := financialdate.Parse(q.Planned)
	if e != nil {
		writeError(w, 400, "invalid request")
		return
	}
	x, e := h.cards.ReplaceIntent(r.Context(), o, r.PathValue("cycle_id"), m, d)
	if e != nil {
		h.err(w, e)
		return
	}
	writeJSON(w, 200, mapIntent(x))
}
func (h cardHandler) cancel(w http.ResponseWriter, r *http.Request) {
	o, ok := h.mutate(w, r)
	if !ok {
		return
	}
	x, e := h.cards.CancelIntent(r.Context(), o, r.PathValue("cycle_id"))
	if e != nil {
		h.err(w, e)
		return
	}
	writeJSON(w, 200, mapIntent(x))
}
func (h cardHandler) auth(w http.ResponseWriter, r *http.Request) (string, bool) {
	secureResponse(w)
	if h.cards == nil {
		writeError(w, 500, "service unavailable")
		return "", false
	}
	c, e := r.Cookie(sessionCookieName)
	if e != nil {
		writeError(w, 401, "authentication required")
		return "", false
	}
	o, e := h.authentication.Authenticate(r.Context(), c.Value)
	if errors.Is(e, auth.ErrUnauthenticated) {
		writeError(w, 401, "authentication required")
		return "", false
	}
	if e != nil {
		writeError(w, 500, "service unavailable")
		return "", false
	}
	return o.ID(), true
}
func (h cardHandler) mutate(w http.ResponseWriter, r *http.Request) (string, bool) {
	if r.Header.Get("Origin") != h.config.AllowedOrigin {
		writeError(w, 403, "request origin is not allowed")
		return "", false
	}
	return h.auth(w, r)
}
func (h cardHandler) err(w http.ResponseWriter, e error) {
	if errors.Is(e, app.ErrNotFound) {
		writeError(w, 404, "resource not found")
		return
	}
	if errors.Is(e, domain.ErrIssuedCannotBeReplacedByEstimate) || errors.Is(e, domain.ErrPaymentIntentCancelled) || errors.Is(e, app.ErrConflict) {
		writeError(w, 409, "request conflicts with current statement state")
		return
	}
	if errors.Is(e, ledger.ErrIdempotencyConflict) {
		writeError(w, 409, "request conflicts with current statement state")
		return
	}
	writeError(w, 400, "invalid request")
}
func mapStatement(s domain.Statement) map[string]any {
	m := map[string]any{"id": s.ID, "account_id": s.AccountID, "cycle_id": s.CycleID, "revision": s.Revision, "authority": s.Authority, "statement_balance_minor": s.Balance.MinorUnits(), "currency": s.Balance.Currency().Code(), "due_date": s.Due.String(), "created_at": s.CreatedAt}
	if s.Minimum != nil {
		m["minimum_payment_minor"] = s.Minimum.MinorUnits()
	}
	if s.AvoidInterest != nil {
		m["payment_to_avoid_interest_minor"] = s.AvoidInterest.MinorUnits()
	}
	if s.SupersededAt != nil {
		m["superseded_at"] = s.SupersededAt
		m["superseded_by_id"] = s.SupersededBy
	}
	return m
}
func mapIntent(x domain.PaymentIntent) map[string]any {
	return map[string]any{"id": x.ID, "account_id": x.AccountID, "cycle_id": x.CycleID, "amount_minor": x.Amount.MinorUnits(), "currency": x.Amount.Currency().Code(), "planned_date": x.Planned.String(), "status": x.Status, "version": x.Version, "created_at": x.CreatedAt, "updated_at": x.UpdatedAt}
}
func itoa(v int64) string { return fmt.Sprintf("%d", v) }
func ptrInt(v *int64) string {
	if v == nil {
		return ""
	}
	return itoa(*v)
}
