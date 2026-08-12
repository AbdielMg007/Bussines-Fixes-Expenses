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
	GetIntentSummary(context.Context, string, string) (app.PaymentIntentSummary, error)
	ReplaceIntent(context.Context, string, string, money.Money, financialdate.Date) (domain.PaymentIntent, error)
	CancelIntent(context.Context, string, string) (domain.PaymentIntent, error)
	SettleIntent(context.Context, string, app.PaymentIntentSettlementInput) (app.PaymentIntentSettlementResult, error)
	CreateInstallmentPlan(context.Context, string, app.InstallmentPlanInput) (domain.InstallmentPlan, error)
	GetInstallmentPlan(context.Context, string, string) (domain.InstallmentPlan, error)
	ListInstallmentPlans(context.Context, string, string) ([]domain.InstallmentPlan, error)
	ListInstallmentAllocations(context.Context, string, string) ([]domain.InstallmentAllocation, error)
	GetInstallmentPlanSummary(context.Context, string, string) (app.InstallmentPlanSummary, error)
	RecordInstallmentPrincipalPayment(context.Context, string, string, money.Money, ledger.MutationIdentity) (app.InstallmentPrincipalPaymentResult, error)
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
type installmentPlanRequest struct {
	Description           string `json:"description"`
	PurchaseTransactionID string `json:"purchase_transaction_id"`
	Principal             *int64 `json:"original_principal_minor"`
	Currency              string `json:"currency"`
	InstallmentCount      int    `json:"installment_count"`
	FirstCycleStart       string `json:"first_cycle_start"`
	FirstCycleEnd         string `json:"first_cycle_end"`
}
type installmentPrincipalPaymentRequest struct {
	Amount   *int64 `json:"amount_minor"`
	Currency string `json:"currency"`
}
type intentSettlementRequest struct {
	TransferID string `json:"transfer_id"`
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
	x, e := h.cards.GetIntentSummary(r.Context(), o, r.PathValue("cycle_id"))
	if e != nil {
		h.err(w, e)
		return
	}
	writeJSON(w, 200, mapIntentSummary(x))
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
func (h cardHandler) settleIntent(w http.ResponseWriter, r *http.Request) {
	owner, ok := h.mutate(w, r)
	if !ok {
		return
	}
	key, ok := parseIdempotencyKey(w, r)
	if !ok {
		return
	}
	var request intentSettlementRequest
	if !decodeLedgerRequest(w, r, &request) || request.TransferID == "" {
		writeError(w, 400, "invalid request")
		return
	}
	intentID := r.PathValue("intent_id")
	result, err := h.cards.SettleIntent(r.Context(), owner, app.PaymentIntentSettlementInput{
		IntentID: intentID, TransferID: request.TransferID,
		Mutation: ledger.MutationIdentity{Key: key, Fingerprint: ledger.CanonicalFingerprint("v1", "settle_credit_card_payment_intent", owner, intentID, request.TransferID)},
	})
	if err != nil {
		h.err(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": result.Settlement.ID, "intent": mapIntent(result.Intent)})
}
func (h cardHandler) createInstallmentPlan(w http.ResponseWriter, r *http.Request) {
	owner, ok := h.mutate(w, r)
	if !ok {
		return
	}
	key, ok := parseIdempotencyKey(w, r)
	if !ok {
		return
	}
	var request installmentPlanRequest
	if !decodeLedgerRequest(w, r, &request) {
		return
	}
	if request.Principal == nil || request.PurchaseTransactionID == "" {
		writeError(w, 400, "invalid request")
		return
	}
	currency, err := money.ParseCurrency(request.Currency)
	if err != nil {
		writeError(w, 400, "invalid request")
		return
	}
	principal, err := money.New(*request.Principal, currency)
	if err != nil {
		writeError(w, 400, "invalid request")
		return
	}
	if principal.MinorUnits() <= 0 {
		writeError(w, 400, "invalid request")
		return
	}
	start, err := financialdate.Parse(request.FirstCycleStart)
	if err != nil {
		writeError(w, 400, "invalid request")
		return
	}
	end, err := financialdate.Parse(request.FirstCycleEnd)
	if err != nil {
		writeError(w, 400, "invalid request")
		return
	}
	accountID := r.PathValue("account_id")
	plan, err := h.cards.CreateInstallmentPlan(r.Context(), owner, app.InstallmentPlanInput{
		AccountID: accountID, Description: request.Description, PurchaseTransactionID: request.PurchaseTransactionID, Principal: principal, InstallmentCount: request.InstallmentCount, FirstCycleStart: start, FirstCycleEnd: end,
		Mutation: ledger.MutationIdentity{Key: key, Fingerprint: ledger.CanonicalFingerprint("v1", "create_installment_plan", owner, accountID, request.Description, request.PurchaseTransactionID, itoa(*request.Principal), request.Currency, itoa(int64(request.InstallmentCount)), request.FirstCycleStart, request.FirstCycleEnd)},
	})
	if err != nil {
		h.err(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, mapInstallmentPlan(plan))
}
func (h cardHandler) listInstallmentPlans(w http.ResponseWriter, r *http.Request) {
	owner, ok := h.auth(w, r)
	if !ok {
		return
	}
	plans, err := h.cards.ListInstallmentPlans(r.Context(), owner, r.PathValue("account_id"))
	if err != nil {
		h.err(w, err)
		return
	}
	response := make([]any, 0, len(plans))
	for _, plan := range plans {
		response = append(response, mapInstallmentPlan(plan))
	}
	writeJSON(w, http.StatusOK, response)
}
func (h cardHandler) getInstallmentPlan(w http.ResponseWriter, r *http.Request) {
	owner, ok := h.auth(w, r)
	if !ok {
		return
	}
	summary, err := h.cards.GetInstallmentPlanSummary(r.Context(), owner, r.PathValue("plan_id"))
	if err != nil {
		h.err(w, err)
		return
	}
	writeJSON(w, http.StatusOK, mapInstallmentPlanSummary(summary))
}
func (h cardHandler) listInstallmentAllocations(w http.ResponseWriter, r *http.Request) {
	owner, ok := h.auth(w, r)
	if !ok {
		return
	}
	allocations, err := h.cards.ListInstallmentAllocations(r.Context(), owner, r.PathValue("plan_id"))
	if err != nil {
		h.err(w, err)
		return
	}
	response := make([]any, 0, len(allocations))
	for _, allocation := range allocations {
		response = append(response, mapInstallmentAllocation(allocation))
	}
	writeJSON(w, http.StatusOK, response)
}
func (h cardHandler) recordInstallmentPrincipalPayment(w http.ResponseWriter, r *http.Request) {
	owner, ok := h.mutate(w, r)
	if !ok {
		return
	}
	key, ok := parseIdempotencyKey(w, r)
	if !ok {
		return
	}
	var request installmentPrincipalPaymentRequest
	if !decodeLedgerRequest(w, r, &request) {
		return
	}
	if request.Amount == nil {
		writeError(w, 400, "invalid request")
		return
	}
	currency, err := money.ParseCurrency(request.Currency)
	if err != nil {
		writeError(w, 400, "invalid request")
		return
	}
	amount, err := money.New(*request.Amount, currency)
	if err != nil {
		writeError(w, 400, "invalid request")
		return
	}
	if amount.MinorUnits() <= 0 {
		writeError(w, 400, "invalid request")
		return
	}
	allocationID := r.PathValue("allocation_id")
	result, err := h.cards.RecordInstallmentPrincipalPayment(r.Context(), owner, allocationID, amount, ledger.MutationIdentity{Key: key, Fingerprint: ledger.CanonicalFingerprint("v1", "record_installment_principal_payment", owner, allocationID, itoa(*request.Amount), request.Currency)})
	if err != nil {
		h.err(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, mapInstallmentPrincipalPaymentResult(result))
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
	if errors.Is(e, domain.ErrIssuedCannotBeReplacedByEstimate) || errors.Is(e, domain.ErrPaymentIntentCancelled) || errors.Is(e, domain.ErrPaymentIntentSettled) || errors.Is(e, domain.ErrInstallmentOverpayment) || errors.Is(e, domain.ErrInstallmentPlanCompleted) || errors.Is(e, app.ErrConflict) {
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
func mapIntentSummary(summary app.PaymentIntentSummary) map[string]any {
	response := mapIntent(summary.Intent)
	response["settled_amount_minor"] = summary.SettledAmount.MinorUnits()
	response["remaining_amount_minor"] = summary.RemainingAmount.MinorUnits()
	return response
}
func mapInstallmentPlan(plan domain.InstallmentPlan) map[string]any {
	return map[string]any{"id": plan.ID, "account_id": plan.AccountID, "description": plan.Description, "purchase_transaction_id": plan.PurchaseTransactionID, "original_principal_minor": plan.OriginalPrincipal.MinorUnits(), "currency": plan.OriginalPrincipal.Currency().Code(), "installment_count": plan.InstallmentCount, "first_cycle_id": plan.FirstCycleID, "status": plan.Status, "schedule_version": plan.ScheduleVersion, "created_at": plan.CreatedAt, "updated_at": plan.UpdatedAt}
}
func mapInstallmentPlanSummary(summary app.InstallmentPlanSummary) map[string]any {
	response := mapInstallmentPlan(summary.Plan)
	response["paid_principal_minor"] = summary.PaidPrincipal.MinorUnits()
	response["outstanding_principal_minor"] = summary.OutstandingPrincipal.MinorUnits()
	return response
}
func mapInstallmentAllocation(allocation domain.InstallmentAllocation) map[string]any {
	return map[string]any{"id": allocation.ID, "plan_id": allocation.PlanID, "cycle_id": allocation.CycleID, "installment_number": allocation.InstallmentNumber, "schedule_version": allocation.ScheduleVersion, "principal_minor": allocation.Principal.MinorUnits(), "currency": allocation.Principal.Currency().Code(), "status": allocation.Status, "created_at": allocation.CreatedAt}
}
func mapInstallmentPrincipalPaymentResult(result app.InstallmentPrincipalPaymentResult) map[string]any {
	return map[string]any{"id": result.Payment.ID, "allocation_id": result.Payment.AllocationID, "plan_id": result.Payment.PlanID, "amount_minor": result.Payment.Amount.MinorUnits(), "currency": result.Payment.Amount.Currency().Code(), "created_at": result.Payment.CreatedAt, "plan": mapInstallmentPlanSummary(result.Summary)}
}
func itoa(v int64) string { return fmt.Sprintf("%d", v) }
func ptrInt(v *int64) string {
	if v == nil {
		return ""
	}
	return itoa(*v)
}
