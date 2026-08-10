package httpapi

import (
	"errors"
	"net/http"
	"time"

	"runway/backend/internal/auth"
	"runway/backend/internal/domain/financialdate"
	"runway/backend/internal/domain/money"
	domainschedule "runway/backend/internal/domain/schedule"
	applicationledger "runway/backend/internal/ledger"
)

type scheduleHandler struct {
	authentication AuthenticationService
	schedule       ScheduleService
	config         AuthConfig
}

type createObligationRequest struct {
	Name        string  `json:"name"`
	AmountMinor int64   `json:"amount_minor"`
	Currency    string  `json:"currency"`
	Recurrence  string  `json:"recurrence"`
	StartDate   string  `json:"start_date"`
	EndDate     *string `json:"end_date"`
}

type obligationResponse struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	AmountMinor  int64     `json:"amount_minor"`
	Currency     string    `json:"currency"`
	Direction    string    `json:"direction"`
	Recurrence   string    `json:"recurrence"`
	StartDate    string    `json:"start_date"`
	EndDate      *string   `json:"end_date"`
	InactiveFrom *string   `json:"inactive_from"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type archiveObligationRequest struct {
	InactiveFrom string `json:"inactive_from"`
}

type createScheduledFlowRequest struct {
	AmountMinor          int64  `json:"amount_minor"`
	Currency             string `json:"currency"`
	Direction            string `json:"direction"`
	FinancialDate        string `json:"financial_date"`
	SourceKind           string `json:"source_kind"`
	AmountProvenance     string `json:"amount_provenance"`
	DateProvenance       string `json:"date_provenance"`
	InclusionEligibility string `json:"inclusion_eligibility"`
}

type scheduledFlowResponse struct {
	ID                      string    `json:"id"`
	SourceKind              string    `json:"source_kind"`
	SourceID                string    `json:"source_id"`
	AmountMinor             int64     `json:"amount_minor"`
	Currency                string    `json:"currency"`
	Direction               string    `json:"direction"`
	FinancialDate           string    `json:"financial_date"`
	Status                  string    `json:"status"`
	AmountProvenance        string    `json:"amount_provenance"`
	DateProvenance          string    `json:"date_provenance"`
	InclusionEligibility    string    `json:"inclusion_eligibility"`
	SettlementTransactionID string    `json:"settlement_transaction_id,omitempty"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}

type createReceivableRequest struct {
	Name                string  `json:"name"`
	OriginalAmountMinor int64   `json:"original_amount_minor"`
	Currency            string  `json:"currency"`
	ExpectedDate        *string `json:"expected_date"`
	Certainty           string  `json:"certainty"`
}

type receivableResponse struct {
	ID                   string    `json:"id"`
	Name                 string    `json:"name"`
	OriginalAmountMinor  int64     `json:"original_amount_minor"`
	CollectedAmountMinor int64     `json:"collected_amount_minor"`
	OutstandingMinor     int64     `json:"outstanding_amount_minor"`
	Currency             string    `json:"currency"`
	Status               string    `json:"status"`
	ExpectedDate         *string   `json:"expected_date"`
	Certainty            string    `json:"certainty"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type collectionRequest struct {
	AmountMinor         int64  `json:"amount_minor"`
	Currency            string `json:"currency"`
	LedgerTransactionID string `json:"ledger_transaction_id"`
}

type collectionResponse struct {
	ID                    string    `json:"id"`
	ReceivableID          string    `json:"receivable_id"`
	AmountMinor           int64     `json:"amount_minor"`
	Currency              string    `json:"currency"`
	CollectedBeforeMinor  int64     `json:"collected_before_minor"`
	CollectedAfterMinor   int64     `json:"collected_after_minor"`
	OutstandingAfterMinor int64     `json:"outstanding_after_minor"`
	ResultingStatus       string    `json:"resulting_status"`
	LedgerTransactionID   string    `json:"ledger_transaction_id,omitempty"`
	CreatedAt             time.Time `json:"created_at"`
}

func (h scheduleHandler) createObligation(w http.ResponseWriter, r *http.Request) {
	ownerID, key, ok := h.authorizeMutation(w, r)
	if !ok {
		return
	}
	var input createObligationRequest
	if !decodeLedgerRequest(w, r, &input) {
		return
	}
	amount, err := parsePositiveMoney(input.AmountMinor, input.Currency)
	if err != nil {
		writeScheduleError(w, err)
		return
	}
	recurrence, err := domainschedule.ParseRecurrence(input.Recurrence)
	if err != nil {
		writeScheduleError(w, err)
		return
	}
	start, err := financialdate.Parse(input.StartDate)
	if err != nil {
		writeScheduleError(w, err)
		return
	}
	end, err := parseOptionalDate(input.EndDate)
	if err != nil {
		writeScheduleError(w, err)
		return
	}
	created, err := h.schedule.CreateObligation(r.Context(), ownerID, input.Name, amount, recurrence, start, end, key)
	if err != nil {
		writeScheduleError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, mapObligation(created))
}

func (h scheduleHandler) listObligations(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.authorize(w, r)
	if !ok {
		return
	}
	values, err := h.schedule.ListObligations(r.Context(), ownerID)
	if err != nil {
		writeScheduleError(w, err)
		return
	}
	response := make([]obligationResponse, 0, len(values))
	for _, value := range values {
		response = append(response, mapObligation(value))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h scheduleHandler) getObligation(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.authorize(w, r)
	if !ok {
		return
	}
	value, err := h.schedule.GetObligation(r.Context(), ownerID, r.PathValue("id"))
	if err != nil {
		writeScheduleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, mapObligation(value))
}

func (h scheduleHandler) archiveObligation(w http.ResponseWriter, r *http.Request) {
	ownerID, key, ok := h.authorizeMutation(w, r)
	if !ok {
		return
	}
	var input archiveObligationRequest
	if !decodeLedgerRequest(w, r, &input) {
		return
	}
	inactiveFrom, err := financialdate.Parse(input.InactiveFrom)
	if err != nil {
		writeScheduleError(w, err)
		return
	}
	value, err := h.schedule.ArchiveObligation(r.Context(), ownerID, r.PathValue("id"), inactiveFrom, key)
	if err != nil {
		writeScheduleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, mapObligation(value))
}

func (h scheduleHandler) expandObligation(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.authorize(w, r)
	if !ok {
		return
	}
	from, err := financialdate.Parse(r.URL.Query().Get("from"))
	if err != nil {
		writeScheduleError(w, err)
		return
	}
	to, err := financialdate.Parse(r.URL.Query().Get("to"))
	if err != nil {
		writeScheduleError(w, err)
		return
	}
	values, err := h.schedule.ExpandObligation(r.Context(), ownerID, r.PathValue("id"), from, to)
	if err != nil {
		writeScheduleError(w, err)
		return
	}
	response := make([]scheduledFlowResponse, 0, len(values))
	for _, value := range values {
		response = append(response, mapScheduledFlow(value))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h scheduleHandler) createScheduledFlow(w http.ResponseWriter, r *http.Request) {
	ownerID, key, ok := h.authorizeMutation(w, r)
	if !ok {
		return
	}
	var input createScheduledFlowRequest
	if !decodeLedgerRequest(w, r, &input) {
		return
	}
	amount, err := parsePositiveMoney(input.AmountMinor, input.Currency)
	if err != nil {
		writeScheduleError(w, err)
		return
	}
	direction, err := domainschedule.ParseDirection(input.Direction)
	if err != nil {
		writeScheduleError(w, err)
		return
	}
	date, err := financialdate.Parse(input.FinancialDate)
	if err != nil {
		writeScheduleError(w, err)
		return
	}
	sourceKind, err := domainschedule.ParseSourceKind(input.SourceKind)
	if err != nil {
		writeScheduleError(w, err)
		return
	}
	amountProvenance, err := domainschedule.ParseAmountProvenance(input.AmountProvenance)
	if err != nil {
		writeScheduleError(w, err)
		return
	}
	dateProvenance, err := domainschedule.ParseDateProvenance(input.DateProvenance)
	if err != nil {
		writeScheduleError(w, err)
		return
	}
	if dateProvenance.IsScenarioAssumed() {
		writeScheduleError(w, domainschedule.ErrInvalidScheduledFlow)
		return
	}
	inclusion, err := domainschedule.ParseInclusionEligibility(input.InclusionEligibility)
	if err != nil {
		writeScheduleError(w, err)
		return
	}
	created, err := h.schedule.CreateManualScheduledFlow(r.Context(), ownerID, amount, direction, date, sourceKind, amountProvenance, dateProvenance, inclusion, key)
	if err != nil {
		writeScheduleError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, mapScheduledFlow(created))
}

func (h scheduleHandler) listScheduledFlows(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.authorize(w, r)
	if !ok {
		return
	}
	values, err := h.schedule.ListScheduledFlows(r.Context(), ownerID)
	if err != nil {
		writeScheduleError(w, err)
		return
	}
	response := make([]scheduledFlowResponse, 0, len(values))
	for _, value := range values {
		response = append(response, mapScheduledFlow(value))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h scheduleHandler) getScheduledFlow(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.authorize(w, r)
	if !ok {
		return
	}
	value, err := h.schedule.GetScheduledFlow(r.Context(), ownerID, r.PathValue("id"))
	if err != nil {
		writeScheduleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, mapScheduledFlow(value))
}

func (h scheduleHandler) cancelScheduledFlow(w http.ResponseWriter, r *http.Request) {
	ownerID, key, ok := h.authorizeMutation(w, r)
	if !ok {
		return
	}
	value, err := h.schedule.CancelScheduledFlow(r.Context(), ownerID, r.PathValue("id"), key)
	if err != nil {
		writeScheduleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, mapScheduledFlow(value))
}

func (h scheduleHandler) createReceivable(w http.ResponseWriter, r *http.Request) {
	ownerID, key, ok := h.authorizeMutation(w, r)
	if !ok {
		return
	}
	var input createReceivableRequest
	if !decodeLedgerRequest(w, r, &input) {
		return
	}
	amount, err := parsePositiveMoney(input.OriginalAmountMinor, input.Currency)
	if err != nil {
		writeScheduleError(w, err)
		return
	}
	expectedDate, err := parseOptionalDate(input.ExpectedDate)
	if err != nil {
		writeScheduleError(w, err)
		return
	}
	certainty, err := domainschedule.ParseCertainty(input.Certainty)
	if err != nil {
		writeScheduleError(w, err)
		return
	}
	created, err := h.schedule.CreateReceivable(r.Context(), ownerID, input.Name, amount, expectedDate, certainty, key)
	if err != nil {
		writeScheduleError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, mapReceivable(created))
}

func (h scheduleHandler) listReceivables(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.authorize(w, r)
	if !ok {
		return
	}
	values, err := h.schedule.ListReceivables(r.Context(), ownerID)
	if err != nil {
		writeScheduleError(w, err)
		return
	}
	response := make([]receivableResponse, 0, len(values))
	for _, value := range values {
		response = append(response, mapReceivable(value))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h scheduleHandler) getReceivable(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.authorize(w, r)
	if !ok {
		return
	}
	value, err := h.schedule.GetReceivable(r.Context(), ownerID, r.PathValue("id"))
	if err != nil {
		writeScheduleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, mapReceivable(value))
}

func (h scheduleHandler) recordReceivableCollection(w http.ResponseWriter, r *http.Request) {
	ownerID, key, ok := h.authorizeMutation(w, r)
	if !ok {
		return
	}
	var input collectionRequest
	if !decodeLedgerRequest(w, r, &input) {
		return
	}
	amount, err := parsePositiveMoney(input.AmountMinor, input.Currency)
	if err != nil {
		writeScheduleError(w, err)
		return
	}
	collection, err := h.schedule.RecordReceivableCollection(r.Context(), ownerID, r.PathValue("id"), amount, input.LedgerTransactionID, key)
	if err != nil {
		writeScheduleError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, mapCollection(collection))
}

func (h scheduleHandler) cancelReceivable(w http.ResponseWriter, r *http.Request) {
	ownerID, key, ok := h.authorizeMutation(w, r)
	if !ok {
		return
	}
	value, err := h.schedule.CancelReceivable(r.Context(), ownerID, r.PathValue("id"), key)
	if err != nil {
		writeScheduleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, mapReceivable(value))
}

func (h scheduleHandler) authorizeMutation(w http.ResponseWriter, r *http.Request) (string, applicationledger.IdempotencyKey, bool) {
	secureResponse(w)
	if r.Header.Get("Origin") != h.config.AllowedOrigin {
		writeError(w, http.StatusForbidden, "request origin is not allowed")
		return "", applicationledger.IdempotencyKey{}, false
	}
	ownerID, ok := h.authorize(w, r)
	if !ok {
		return "", applicationledger.IdempotencyKey{}, false
	}
	key, ok := parseIdempotencyKey(w, r)
	return ownerID, key, ok
}

func (h scheduleHandler) authorize(w http.ResponseWriter, r *http.Request) (string, bool) {
	secureResponse(w)
	if h.authentication == nil || h.schedule == nil {
		writeError(w, http.StatusInternalServerError, "service unavailable")
		return "", false
	}
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return "", false
	}
	owner, err := h.authentication.Authenticate(r.Context(), cookie.Value)
	if errors.Is(err, auth.ErrUnauthenticated) {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return "", false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "authentication service unavailable")
		return "", false
	}
	return owner.ID(), true
}

func parsePositiveMoney(minor int64, currencyCode string) (money.Money, error) {
	currency, err := money.ParseCurrency(currencyCode)
	if err != nil {
		return money.Money{}, err
	}
	amount, err := money.New(minor, currency)
	if err != nil {
		return money.Money{}, err
	}
	if amount.MinorUnits() == 0 {
		return money.Money{}, domainschedule.ErrInvalidScheduledFlow
	}
	return amount, nil
}

func parseOptionalDate(value *string) (*financialdate.Date, error) {
	if value == nil {
		return nil, nil
	}
	date, err := financialdate.Parse(*value)
	if err != nil {
		return nil, err
	}
	return &date, nil
}

func mapObligation(value domainschedule.Obligation) obligationResponse {
	var end *string
	if date, ok := value.EndDate(); ok {
		text := date.String()
		end = &text
	}
	var inactiveFrom *string
	if date, ok := value.InactiveFrom(); ok {
		text := date.String()
		inactiveFrom = &text
	}
	return obligationResponse{ID: value.ID(), Name: value.DisplayName(), AmountMinor: value.Amount().MinorUnits(), Currency: value.Amount().Currency().Code(), Direction: value.Direction().String(), Recurrence: value.Recurrence().String(), StartDate: value.StartDate().String(), EndDate: end, InactiveFrom: inactiveFrom, Status: value.Status().String(), CreatedAt: value.CreatedAt(), UpdatedAt: value.UpdatedAt()}
}

func mapScheduledFlow(value domainschedule.ScheduledCashFlow) scheduledFlowResponse {
	return scheduledFlowResponse{ID: value.ID(), SourceKind: value.SourceKind().String(), SourceID: value.SourceID(), AmountMinor: value.Amount().MinorUnits(), Currency: value.Amount().Currency().Code(), Direction: value.Direction().String(), FinancialDate: value.FinancialDate().String(), Status: value.Status().String(), AmountProvenance: value.AmountProvenance().String(), DateProvenance: value.DateProvenance().String(), InclusionEligibility: value.InclusionEligibility().String(), SettlementTransactionID: value.SettlementTransactionID(), CreatedAt: value.CreatedAt(), UpdatedAt: value.UpdatedAt()}
}

func mapReceivable(value domainschedule.Receivable) receivableResponse {
	var expected *string
	if date, ok := value.ExpectedDate(); ok {
		text := date.String()
		expected = &text
	}
	outstanding, _ := value.OutstandingAmount()
	return receivableResponse{ID: value.ID(), Name: value.DisplayName(), OriginalAmountMinor: value.OriginalAmount().MinorUnits(), CollectedAmountMinor: value.CollectedAmount().MinorUnits(), OutstandingMinor: outstanding.MinorUnits(), Currency: value.OriginalAmount().Currency().Code(), Status: value.Status().String(), ExpectedDate: expected, Certainty: value.Certainty().String(), CreatedAt: value.CreatedAt(), UpdatedAt: value.UpdatedAt()}
}

func mapCollection(value domainschedule.ReceivableCollection) collectionResponse {
	return collectionResponse{ID: value.ID(), ReceivableID: value.ReceivableID(), AmountMinor: value.Amount().MinorUnits(), Currency: value.Amount().Currency().Code(), CollectedBeforeMinor: value.CollectedBefore().MinorUnits(), CollectedAfterMinor: value.CollectedAfter().MinorUnits(), OutstandingAfterMinor: value.OutstandingAfter().MinorUnits(), ResultingStatus: value.ResultingStatus().String(), LedgerTransactionID: value.LedgerTransactionID(), CreatedAt: value.CreatedAt()}
}

func writeScheduleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, applicationledger.ErrNotFound):
		writeError(w, http.StatusNotFound, "resource not found")
	case errors.Is(err, applicationledger.ErrConflict), errors.Is(err, applicationledger.ErrIdempotencyConflict),
		errors.Is(err, domainschedule.ErrObligationArchived), errors.Is(err, domainschedule.ErrScheduledFlowFinal),
		errors.Is(err, domainschedule.ErrReceivableFinal), errors.Is(err, domainschedule.ErrCollectionExceedsAmount):
		writeError(w, http.StatusConflict, "request conflicts with current financial state")
	case errors.Is(err, money.ErrUnsupportedCurrency), errors.Is(err, money.ErrNegativeMagnitude), errors.Is(err, money.ErrCurrencyMismatch),
		errors.Is(err, financialdate.ErrInvalidDate), errors.Is(err, domainschedule.ErrInvalidObligation),
		errors.Is(err, domainschedule.ErrInvalidName), errors.Is(err, domainschedule.ErrInvalidRecurrence),
		errors.Is(err, domainschedule.ErrInvalidDateRange), errors.Is(err, domainschedule.ErrOccurrenceLimitExceeded),
		errors.Is(err, domainschedule.ErrInvalidDirection),
		errors.Is(err, domainschedule.ErrInvalidSourceKind), errors.Is(err, domainschedule.ErrInvalidFlowStatus),
		errors.Is(err, domainschedule.ErrInvalidProvenance), errors.Is(err, domainschedule.ErrInvalidInclusion),
		errors.Is(err, domainschedule.ErrInvalidScheduledFlow), errors.Is(err, domainschedule.ErrInvalidReceivable),
		errors.Is(err, domainschedule.ErrInvalidCertainty), errors.Is(err, domainschedule.ErrInvalidReceivableStatus),
		errors.Is(err, domainschedule.ErrInvalidCollection), errors.Is(err, applicationledger.ErrInvalidIdempotencyKey):
		writeError(w, http.StatusBadRequest, "invalid request")
	default:
		writeError(w, http.StatusInternalServerError, "service unavailable")
	}
}
