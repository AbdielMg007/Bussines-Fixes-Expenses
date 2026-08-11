package httpapi

import (
	"errors"
	"net/http"
	"time"

	"runway/backend/internal/auth"
	"runway/backend/internal/domain/financialdate"
	"runway/backend/internal/domain/money"
	domainprojection "runway/backend/internal/domain/projection"
	applicationledger "runway/backend/internal/ledger"
	applicationprojection "runway/backend/internal/projection"
)

type projectionHandler struct {
	authentication AuthenticationService
	projection     ProjectionService
	config         AuthConfig
}

type accountSelectionRequest struct {
	Mode       string   `json:"mode"`
	AccountIDs []string `json:"account_ids"`
}

type replacePolicyRequest struct {
	Currency          string                   `json:"currency"`
	HorizonDays       *int                     `json:"horizon_days"`
	ReserveMinor      *int64                   `json:"reserve_minor"`
	FinancialTimezone string                   `json:"financial_timezone"`
	AccountSelection  *accountSelectionRequest `json:"account_selection"`
	InflowPolicy      string                   `json:"inflow_policy"`
	SameDayOrder      string                   `json:"same_day_order"`
}

type accountSelectionResponse struct {
	Mode       string   `json:"mode"`
	AccountIDs []string `json:"account_ids"`
}

type policyResponse struct {
	ID                string                   `json:"id"`
	Currency          string                   `json:"currency"`
	HorizonDays       int                      `json:"horizon_days"`
	ReserveMinor      int64                    `json:"reserve_minor"`
	FinancialTimezone string                   `json:"financial_timezone"`
	AccountSelection  accountSelectionResponse `json:"account_selection"`
	InflowPolicy      string                   `json:"inflow_policy"`
	SameDayOrder      string                   `json:"same_day_order"`
	Version           int64                    `json:"version"`
	CreatedAt         time.Time                `json:"created_at"`
	UpdatedAt         time.Time                `json:"updated_at"`
}

type projectionEventResponse struct {
	ID               string `json:"id"`
	SourceKind       string `json:"source_kind"`
	SourceID         string `json:"source_id"`
	FinancialDate    string `json:"financial_date"`
	AmountMinor      int64  `json:"amount_minor"`
	Direction        string `json:"direction"`
	AmountProvenance string `json:"amount_provenance"`
	DateProvenance   string `json:"date_provenance"`
	InclusionBasis   string `json:"inclusion_basis"`
	SourceCertainty  string `json:"source_certainty,omitempty"`
	Label            string `json:"label,omitempty"`
	BalanceBefore    int64  `json:"balance_before_minor"`
	BalanceAfter     int64  `json:"balance_after_minor"`
}

type projectionExclusionResponse struct {
	SourceKind string   `json:"source_kind"`
	SourceID   string   `json:"source_id"`
	Reasons    []string `json:"reasons"`
	Label      string   `json:"label,omitempty"`
}

type projectionResponse struct {
	AsOf                string                        `json:"as_of"`
	HorizonEnd          string                        `json:"horizon_end"`
	Currency            string                        `json:"currency"`
	PolicyID            string                        `json:"policy_id"`
	PolicyVersion       int64                         `json:"policy_version"`
	ReserveMinor        int64                         `json:"reserve_minor"`
	SelectedAccountIDs  []string                      `json:"selected_account_ids"`
	OpeningBalanceMinor int64                         `json:"opening_liquid_balance_minor"`
	Events              []projectionEventResponse     `json:"events"`
	ClosingBalanceMinor int64                         `json:"closing_projected_balance_minor"`
	MinimumBalanceMinor int64                         `json:"minimum_projected_balance_minor"`
	MinimumEventID      string                        `json:"minimum_event_id,omitempty"`
	MinimumDate         *string                       `json:"minimum_date,omitempty"`
	Exclusions          []projectionExclusionResponse `json:"exclusions"`
}

type safeToSpendResponse struct {
	AsOf                        string             `json:"as_of"`
	Currency                    string             `json:"currency"`
	FundingAccountID            string             `json:"funding_account_id"`
	FundingAccountBalanceMinor  int64              `json:"funding_account_balance_minor"`
	ReserveMinor                int64              `json:"reserve_minor"`
	OpeningLiquidBalanceMinor   int64              `json:"opening_liquid_balance_minor"`
	BaselineMinimumBalanceMinor int64              `json:"baseline_minimum_balance_minor"`
	SafeToSpendMinor            int64              `json:"safe_to_spend_minor"`
	Status                      string             `json:"status"`
	LimitingEventID             string             `json:"limiting_event_id,omitempty"`
	LimitingDate                *string            `json:"limiting_date,omitempty"`
	EarliestBreachEventID       string             `json:"earliest_breach_event_id,omitempty"`
	EarliestBreachDate          *string            `json:"earliest_breach_date,omitempty"`
	DeficitMinor                int64              `json:"deficit_minor"`
	PolicyID                    string             `json:"policy_id"`
	PolicyVersion               int64              `json:"policy_version"`
	BaselineProjection          projectionResponse `json:"baseline_projection"`
}

func (h projectionHandler) getPolicy(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.authorize(w, r)
	if !ok {
		return
	}
	policy, err := h.projection.GetPolicy(r.Context(), ownerID)
	if err != nil {
		writeProjectionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, mapPolicy(policy))
}

func (h projectionHandler) replacePolicy(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.authorizeMutation(w, r)
	if !ok {
		return
	}
	var input replacePolicyRequest
	if !decodeLedgerRequest(w, r, &input) {
		return
	}
	if input.HorizonDays == nil || input.ReserveMinor == nil || input.AccountSelection == nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	currency, err := money.ParseCurrency(input.Currency)
	if err != nil {
		writeProjectionError(w, err)
		return
	}
	reserve, err := money.New(*input.ReserveMinor, currency)
	if err != nil {
		writeProjectionError(w, err)
		return
	}
	selectionMode, err := domainprojection.ParseAccountSelectionMode(input.AccountSelection.Mode)
	if err != nil {
		writeProjectionError(w, err)
		return
	}
	selection, err := domainprojection.NewAccountSelection(selectionMode, input.AccountSelection.AccountIDs)
	if err != nil {
		writeProjectionError(w, err)
		return
	}
	inflowPolicy, err := domainprojection.ParseInflowPolicy(input.InflowPolicy)
	if err != nil {
		writeProjectionError(w, err)
		return
	}
	sameDayOrder, err := domainprojection.ParseSameDayOrder(input.SameDayOrder)
	if err != nil {
		writeProjectionError(w, err)
		return
	}
	policy, err := h.projection.ReplacePolicy(
		r.Context(), ownerID, currency, *input.HorizonDays, reserve, input.FinancialTimezone,
		selection, inflowPolicy, sameDayOrder,
	)
	if err != nil {
		writeProjectionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, mapPolicy(policy))
}

func (h projectionHandler) calculate(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.authorize(w, r)
	if !ok {
		return
	}
	result, err := h.projection.CalculateBaseline(r.Context(), ownerID)
	if err != nil {
		writeProjectionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, mapProjection(result))
}

func (h projectionHandler) safeToSpend(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.authorize(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	accountIDs, exists := query["funding_account_id"]
	if !exists || len(query) != 1 || len(accountIDs) != 1 || accountIDs[0] == "" {
		writeError(w, http.StatusBadRequest, "funding_account_id is required")
		return
	}
	result, err := h.projection.CalculateSafeToSpend(r.Context(), ownerID, accountIDs[0])
	if err != nil {
		writeProjectionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, mapSafeToSpend(result))
}

func (h projectionHandler) authorizeMutation(w http.ResponseWriter, r *http.Request) (string, bool) {
	secureResponse(w)
	if r.Header.Get("Origin") != h.config.AllowedOrigin {
		writeError(w, http.StatusForbidden, "request origin is not allowed")
		return "", false
	}
	return h.authorize(w, r)
}

func (h projectionHandler) authorize(w http.ResponseWriter, r *http.Request) (string, bool) {
	secureResponse(w)
	if h.authentication == nil || h.projection == nil {
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

func mapPolicy(policy domainprojection.Policy) policyResponse {
	selection := policy.AccountSelection()
	return policyResponse{
		ID: policy.ID(), Currency: policy.Currency().Code(), HorizonDays: policy.HorizonDays(),
		ReserveMinor: policy.Reserve().MinorUnits(), FinancialTimezone: policy.FinancialTimezone(),
		AccountSelection: accountSelectionResponse{Mode: selection.Mode().String(), AccountIDs: selection.AccountIDs()},
		InflowPolicy:     policy.InflowPolicy().String(), SameDayOrder: policy.SameDayOrder().String(),
		Version: policy.Version(), CreatedAt: policy.CreatedAt(), UpdatedAt: policy.UpdatedAt(),
	}
}

func mapProjection(result domainprojection.Result) projectionResponse {
	response := projectionResponse{
		AsOf: result.AsOf.String(), HorizonEnd: result.HorizonEnd.String(), Currency: result.Currency.Code(),
		PolicyID: result.PolicyID, PolicyVersion: result.PolicyVersion, ReserveMinor: result.Reserve.MinorUnits(),
		SelectedAccountIDs: append([]string(nil), result.SelectedAccountIDs...), OpeningBalanceMinor: result.OpeningBalance.MinorUnits(),
		Events: make([]projectionEventResponse, 0, len(result.Events)), ClosingBalanceMinor: result.ClosingBalance.MinorUnits(),
		MinimumBalanceMinor: result.MinimumBalance.MinorUnits(), MinimumEventID: result.MinimumEventID,
		Exclusions: make([]projectionExclusionResponse, 0, len(result.Exclusions)),
	}
	if result.MinimumDate != nil {
		value := result.MinimumDate.String()
		response.MinimumDate = &value
	}
	for _, applied := range result.Events {
		event := applied.Event
		var sourceCertainty string
		if certainty, ok := event.SourceCertainty(); ok {
			sourceCertainty = certainty.String()
		}
		response.Events = append(response.Events, projectionEventResponse{
			ID: event.ID(), SourceKind: event.SourceKind().String(), SourceID: event.SourceID(),
			FinancialDate: event.FinancialDate().String(), AmountMinor: event.Amount().MinorUnits(),
			Direction: event.Direction().String(), AmountProvenance: event.AmountProvenance().String(),
			DateProvenance: event.DateProvenance().String(), InclusionBasis: event.InclusionBasis().String(),
			SourceCertainty: sourceCertainty, Label: event.Label(),
			BalanceBefore: applied.BalanceBefore.MinorUnits(), BalanceAfter: applied.BalanceAfter.MinorUnits(),
		})
	}
	for _, exclusion := range result.Exclusions {
		reasons := make([]string, 0, len(exclusion.Reasons))
		for _, reason := range exclusion.Reasons {
			reasons = append(reasons, string(reason))
		}
		response.Exclusions = append(response.Exclusions, projectionExclusionResponse{
			SourceKind: exclusion.SourceKind.String(), SourceID: exclusion.SourceID, Reasons: reasons, Label: exclusion.Label,
		})
	}
	return response
}

func mapSafeToSpend(result domainprojection.SafeToSpendResult) safeToSpendResponse {
	response := safeToSpendResponse{
		AsOf: result.AsOf.String(), Currency: result.Currency.Code(), FundingAccountID: result.FundingAccountID,
		FundingAccountBalanceMinor: result.FundingAccountBalance.MinorUnits(), ReserveMinor: result.Reserve.MinorUnits(),
		OpeningLiquidBalanceMinor: result.OpeningLiquidBalance.MinorUnits(), BaselineMinimumBalanceMinor: result.BaselineMinimumBalance.MinorUnits(),
		SafeToSpendMinor: result.SafeToSpend.MinorUnits(), Status: result.Status.String(), LimitingEventID: result.LimitingEventID,
		EarliestBreachEventID: result.EarliestBreachEventID, DeficitMinor: result.Deficit.MinorUnits(),
		PolicyID: result.PolicyID, PolicyVersion: result.PolicyVersion, BaselineProjection: mapProjection(result.BaselineProjection),
	}
	if result.LimitingDate != nil {
		value := result.LimitingDate.String()
		response.LimitingDate = &value
	}
	if result.EarliestBreachDate != nil {
		value := result.EarliestBreachDate.String()
		response.EarliestBreachDate = &value
	}
	return response
}

func writeProjectionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, applicationprojection.ErrPolicyNotFound), errors.Is(err, applicationledger.ErrNotFound):
		writeError(w, http.StatusNotFound, "resource not found")
	case errors.Is(err, applicationprojection.ErrConfigurationRequired):
		writeError(w, http.StatusConflict, "projection policy is required")
	case errors.Is(err, applicationprojection.ErrConfigurationInvalid):
		writeError(w, http.StatusConflict, "projection configuration is invalid; update projection policy")
	case errors.Is(err, applicationprojection.ErrFundingAccountInvalid):
		writeError(w, http.StatusConflict, "funding account is inactive or incompatible with the projection policy")
	case errors.Is(err, applicationprojection.ErrFundingAccountNotSelected):
		writeError(w, http.StatusConflict, "funding account is not selected for projection liquidity")
	case errors.Is(err, money.ErrMonetaryAmountOverflow):
		writeError(w, http.StatusUnprocessableEntity, "projection arithmetic is outside the supported range")
	case errors.Is(err, domainprojection.ErrInvalidPolicy), errors.Is(err, domainprojection.ErrInvalidHorizon),
		errors.Is(err, domainprojection.ErrInvalidTimezone), errors.Is(err, domainprojection.ErrInvalidAccountSelection),
		errors.Is(err, domainprojection.ErrInvalidInflowPolicy), errors.Is(err, domainprojection.ErrInvalidSameDayOrder),
		errors.Is(err, domainprojection.ErrInvalidProjectionEvent), errors.Is(err, domainprojection.ErrEventOutsideHorizon),
		errors.Is(err, domainprojection.ErrInvalidSafeToSpendInput),
		errors.Is(err, money.ErrUnsupportedCurrency), errors.Is(err, money.ErrNegativeMagnitude),
		errors.Is(err, money.ErrCurrencyMismatch), errors.Is(err, financialdate.ErrInvalidDate):
		writeError(w, http.StatusBadRequest, "invalid request")
	default:
		writeError(w, http.StatusInternalServerError, "service unavailable")
	}
}
