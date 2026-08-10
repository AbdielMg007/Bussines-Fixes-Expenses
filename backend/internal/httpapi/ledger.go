package httpapi

import (
	"errors"
	"net/http"
	"time"

	"runway/backend/internal/auth"
	"runway/backend/internal/domain/account"
	"runway/backend/internal/domain/financialdate"
	domainledger "runway/backend/internal/domain/ledger"
	"runway/backend/internal/domain/money"
	applicationledger "runway/backend/internal/ledger"
)

type ledgerHandler struct {
	authentication AuthenticationService
	ledger         LedgerService
	config         AuthConfig
}

type createAccountRequest struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Currency string `json:"currency"`
}

type accountResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	Currency  string    `json:"currency"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type postTransactionRequest struct {
	Effect        string `json:"effect"`
	AmountMinor   int64  `json:"amount_minor"`
	Currency      string `json:"currency"`
	FinancialDate string `json:"financial_date"`
	Memo          string `json:"memo"`
}

type transactionResponse struct {
	ID             string    `json:"id"`
	AccountID      string    `json:"account_id"`
	Kind           string    `json:"kind"`
	Effect         string    `json:"effect"`
	AmountMinor    int64     `json:"amount_minor"`
	Currency       string    `json:"currency"`
	FinancialDate  string    `json:"financial_date"`
	LedgerSequence int64     `json:"ledger_sequence"`
	Memo           string    `json:"memo"`
	TransferID     string    `json:"transfer_id,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type createSnapshotRequest struct {
	BalanceMinor   *int64 `json:"balance_minor"`
	Currency       string `json:"currency"`
	EffectiveAt    string `json:"effective_at"`
	CutoffSequence *int64 `json:"cutoff_sequence"`
}

type snapshotResponse struct {
	ID             string    `json:"id"`
	AccountID      string    `json:"account_id"`
	BalanceMinor   int64     `json:"balance_minor"`
	Currency       string    `json:"currency"`
	EffectiveAt    time.Time `json:"effective_at"`
	CutoffSequence int64     `json:"cutoff_sequence"`
	CreatedAt      time.Time `json:"created_at"`
}

type balanceResponse struct {
	AccountID              string `json:"account_id"`
	BalanceMinor           int64  `json:"balance_minor"`
	Currency               string `json:"currency"`
	SnapshotID             string `json:"snapshot_id,omitempty"`
	SnapshotCutoffSequence int64  `json:"snapshot_cutoff_sequence"`
	LastAppliedSequence    int64  `json:"last_applied_sequence"`
	AppliedTransactions    int    `json:"applied_transactions"`
}

type createTransferRequest struct {
	SourceAccountID      string `json:"source_account_id"`
	DestinationAccountID string `json:"destination_account_id"`
	AmountMinor          int64  `json:"amount_minor"`
	Currency             string `json:"currency"`
	FinancialDate        string `json:"financial_date"`
	Memo                 string `json:"memo"`
}

type transferResponse struct {
	ID                     string              `json:"id"`
	SourceAccountID        string              `json:"source_account_id"`
	DestinationAccountID   string              `json:"destination_account_id"`
	AmountMinor            int64               `json:"amount_minor"`
	Currency               string              `json:"currency"`
	FinancialDate          string              `json:"financial_date"`
	Memo                   string              `json:"memo"`
	CreatedAt              time.Time           `json:"created_at"`
	SourceTransaction      transactionResponse `json:"source_transaction"`
	DestinationTransaction transactionResponse `json:"destination_transaction"`
}

func (h ledgerHandler) createAccount(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.authorizeMutation(w, r)
	if !ok {
		return
	}
	var input createAccountRequest
	if !decodeLedgerRequest(w, r, &input) {
		return
	}
	accountType, err := account.ParseType(input.Type)
	if err != nil {
		writeLedgerError(w, err)
		return
	}
	currency, err := money.ParseCurrency(input.Currency)
	if err != nil {
		writeLedgerError(w, err)
		return
	}
	created, err := h.ledger.CreateAccount(r.Context(), ownerID, input.Name, accountType, currency)
	if err != nil {
		writeLedgerError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, mapAccount(created))
}

func (h ledgerHandler) listAccounts(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.authorize(w, r)
	if !ok {
		return
	}
	accounts, err := h.ledger.ListAccounts(r.Context(), ownerID)
	if err != nil {
		writeLedgerError(w, err)
		return
	}
	response := make([]accountResponse, 0, len(accounts))
	for _, financialAccount := range accounts {
		response = append(response, mapAccount(financialAccount))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h ledgerHandler) getAccount(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.authorize(w, r)
	if !ok {
		return
	}
	financialAccount, err := h.ledger.GetAccount(r.Context(), ownerID, r.PathValue("id"))
	if err != nil {
		writeLedgerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, mapAccount(financialAccount))
}

func (h ledgerHandler) archiveAccount(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.authorizeMutation(w, r)
	if !ok {
		return
	}
	archived, err := h.ledger.ArchiveAccount(r.Context(), ownerID, r.PathValue("id"))
	if err != nil {
		writeLedgerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, mapAccount(archived))
}

func (h ledgerHandler) postTransaction(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.authorizeMutation(w, r)
	if !ok {
		return
	}
	idempotencyKey, ok := parseIdempotencyKey(w, r)
	if !ok {
		return
	}
	var input postTransactionRequest
	if !decodeLedgerRequest(w, r, &input) {
		return
	}
	effect, amount, date, err := parseTransactionValues(input.Effect, input.AmountMinor, input.Currency, input.FinancialDate)
	if err != nil {
		writeLedgerError(w, err)
		return
	}
	posted, err := h.ledger.PostTransaction(r.Context(), ownerID, r.PathValue("id"), effect, amount, date, input.Memo, idempotencyKey)
	if err != nil {
		writeLedgerError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, mapTransaction(posted))
}

func (h ledgerHandler) listTransactions(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.authorize(w, r)
	if !ok {
		return
	}
	transactions, err := h.ledger.ListTransactions(r.Context(), ownerID, r.PathValue("id"))
	if err != nil {
		writeLedgerError(w, err)
		return
	}
	response := make([]transactionResponse, 0, len(transactions))
	for _, transaction := range transactions {
		response = append(response, mapTransaction(transaction))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h ledgerHandler) createSnapshot(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.authorizeMutation(w, r)
	if !ok {
		return
	}
	idempotencyKey, ok := parseIdempotencyKey(w, r)
	if !ok {
		return
	}
	var input createSnapshotRequest
	if !decodeLedgerRequest(w, r, &input) {
		return
	}
	if input.BalanceMinor == nil || input.CutoffSequence == nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	currency, err := money.ParseCurrency(input.Currency)
	if err != nil {
		writeLedgerError(w, err)
		return
	}
	balance, err := money.NewBalance(*input.BalanceMinor, currency)
	if err != nil {
		writeLedgerError(w, err)
		return
	}
	effectiveAt, err := time.Parse(time.RFC3339, input.EffectiveAt)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	snapshot, err := h.ledger.CreateSnapshot(r.Context(), ownerID, r.PathValue("id"), balance, effectiveAt, *input.CutoffSequence, idempotencyKey)
	if err != nil {
		writeLedgerError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, mapSnapshot(snapshot))
}

func (h ledgerHandler) getBalance(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.authorize(w, r)
	if !ok {
		return
	}
	result, err := h.ledger.CurrentBalance(r.Context(), ownerID, r.PathValue("id"))
	if err != nil {
		writeLedgerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, balanceResponse{
		AccountID: result.AccountID, BalanceMinor: result.Balance.MinorUnits(), Currency: result.Balance.Currency().Code(),
		SnapshotID: result.SnapshotID, SnapshotCutoffSequence: result.SnapshotCutoffSequence,
		LastAppliedSequence: result.LastAppliedSequence, AppliedTransactions: result.AppliedTransactions,
	})
}

func (h ledgerHandler) createTransfer(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.authorizeMutation(w, r)
	if !ok {
		return
	}
	idempotencyKey, ok := parseIdempotencyKey(w, r)
	if !ok {
		return
	}
	var input createTransferRequest
	if !decodeLedgerRequest(w, r, &input) {
		return
	}
	_, amount, date, err := parseTransactionValues(domainledger.AssetOutflow().String(), input.AmountMinor, input.Currency, input.FinancialDate)
	if err != nil {
		writeLedgerError(w, err)
		return
	}
	result, err := h.ledger.CreateTransfer(r.Context(), ownerID, input.SourceAccountID, input.DestinationAccountID, amount, date, input.Memo, idempotencyKey)
	if err != nil {
		writeLedgerError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, transferResponse{
		ID: result.Transfer.ID(), SourceAccountID: result.Transfer.SourceAccountID(), DestinationAccountID: result.Transfer.DestinationAccountID(),
		AmountMinor: result.Transfer.Amount().MinorUnits(), Currency: result.Transfer.Amount().Currency().Code(),
		FinancialDate: result.Transfer.FinancialDate().String(), Memo: result.Transfer.Memo(), CreatedAt: result.Transfer.CreatedAt(),
		SourceTransaction: mapTransaction(result.SourceTransaction), DestinationTransaction: mapTransaction(result.DestinationTransaction),
	})
}

func (h ledgerHandler) authorizeMutation(w http.ResponseWriter, r *http.Request) (string, bool) {
	secureResponse(w)
	if r.Header.Get("Origin") != h.config.AllowedOrigin {
		writeError(w, http.StatusForbidden, "request origin is not allowed")
		return "", false
	}
	return h.authorize(w, r)
}

func (h ledgerHandler) authorize(w http.ResponseWriter, r *http.Request) (string, bool) {
	secureResponse(w)
	if h.authentication == nil || h.ledger == nil {
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

func decodeLedgerRequest(w http.ResponseWriter, r *http.Request, target any) bool {
	if err := decodeJSON(w, r, target); err != nil {
		switch {
		case errors.Is(err, errRequestBodyTooLarge):
			writeError(w, http.StatusRequestEntityTooLarge, "request body is too large")
		case errors.Is(err, errUnsupportedMediaType):
			writeError(w, http.StatusUnsupportedMediaType, "content type must be application/json")
		default:
			writeError(w, http.StatusBadRequest, "invalid JSON request")
		}
		return false
	}
	return true
}

func parseIdempotencyKey(w http.ResponseWriter, r *http.Request) (applicationledger.IdempotencyKey, bool) {
	values := r.Header.Values("Idempotency-Key")
	if len(values) != 1 {
		writeError(w, http.StatusBadRequest, "invalid Idempotency-Key")
		return applicationledger.IdempotencyKey{}, false
	}
	key, err := applicationledger.ParseIdempotencyKey(values[0])
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid Idempotency-Key")
		return applicationledger.IdempotencyKey{}, false
	}
	return key, true
}

func parseTransactionValues(effectValue string, amountMinor int64, currencyCode, dateValue string) (domainledger.Effect, money.Money, financialdate.Date, error) {
	effect, err := domainledger.ParseEffect(effectValue)
	if err != nil {
		return domainledger.Effect{}, money.Money{}, financialdate.Date{}, err
	}
	currency, err := money.ParseCurrency(currencyCode)
	if err != nil {
		return domainledger.Effect{}, money.Money{}, financialdate.Date{}, err
	}
	amount, err := money.New(amountMinor, currency)
	if err != nil {
		return domainledger.Effect{}, money.Money{}, financialdate.Date{}, err
	}
	if amount.MinorUnits() == 0 {
		return domainledger.Effect{}, money.Money{}, financialdate.Date{}, domainledger.ErrZeroAmount
	}
	date, err := financialdate.Parse(dateValue)
	if err != nil {
		return domainledger.Effect{}, money.Money{}, financialdate.Date{}, err
	}
	return effect, amount, date, nil
}

func mapAccount(financialAccount account.Account) accountResponse {
	return accountResponse{
		ID: financialAccount.ID(), Name: financialAccount.DisplayName(), Type: financialAccount.Type().String(),
		Currency: financialAccount.Currency().Code(), Status: financialAccount.Status().String(),
		CreatedAt: financialAccount.CreatedAt(), UpdatedAt: financialAccount.UpdatedAt(),
	}
}

func mapTransaction(transaction domainledger.Transaction) transactionResponse {
	return transactionResponse{
		ID: transaction.ID(), AccountID: transaction.AccountID(), Kind: transaction.Kind().String(), Effect: transaction.Effect().String(),
		AmountMinor: transaction.Amount().MinorUnits(), Currency: transaction.Amount().Currency().Code(), FinancialDate: transaction.FinancialDate().String(),
		LedgerSequence: transaction.LedgerSequence(), Memo: transaction.Memo(), TransferID: transaction.TransferID(), CreatedAt: transaction.CreatedAt(),
	}
}

func mapSnapshot(snapshot domainledger.BalanceSnapshot) snapshotResponse {
	return snapshotResponse{
		ID: snapshot.ID(), AccountID: snapshot.AccountID(), BalanceMinor: snapshot.Balance().MinorUnits(),
		Currency: snapshot.Balance().Currency().Code(), EffectiveAt: snapshot.EffectiveAt(), CutoffSequence: snapshot.CutoffSequence(), CreatedAt: snapshot.CreatedAt(),
	}
}

func writeLedgerError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, applicationledger.ErrNotFound):
		writeError(w, http.StatusNotFound, "resource not found")
	case errors.Is(err, applicationledger.ErrConflict),
		errors.Is(err, applicationledger.ErrIdempotencyConflict),
		errors.Is(err, applicationledger.ErrLiabilityOverpayment),
		errors.Is(err, account.ErrAccountArchived),
		errors.Is(err, applicationledger.ErrSnapshotCutoffUnknown),
		errors.Is(err, applicationledger.ErrSnapshotCutoffRegression):
		writeError(w, http.StatusConflict, "request conflicts with current ledger state")
	case errors.Is(err, account.ErrInvalidAccount),
		errors.Is(err, account.ErrInvalidDisplayName),
		errors.Is(err, account.ErrInvalidAccountType),
		errors.Is(err, account.ErrInvalidStatus),
		errors.Is(err, money.ErrUnsupportedCurrency),
		errors.Is(err, money.ErrCurrencyMismatch),
		errors.Is(err, money.ErrNegativeMagnitude),
		errors.Is(err, money.ErrMonetaryAmountOverflow),
		errors.Is(err, financialdate.ErrInvalidDate),
		errors.Is(err, domainledger.ErrInvalidTransaction),
		errors.Is(err, domainledger.ErrInvalidTransactionKind),
		errors.Is(err, domainledger.ErrInvalidTransactionEffect),
		errors.Is(err, domainledger.ErrIncompatibleEffect),
		errors.Is(err, domainledger.ErrZeroAmount),
		errors.Is(err, domainledger.ErrInvalidSnapshot),
		errors.Is(err, domainledger.ErrInvalidTransfer),
		errors.Is(err, domainledger.ErrSameTransferAccount),
		errors.Is(err, applicationledger.ErrInvalidIdempotencyKey),
		errors.Is(err, applicationledger.ErrNegativeLiabilitySnapshot):
		writeError(w, http.StatusBadRequest, "invalid request")
	default:
		writeError(w, http.StatusInternalServerError, "service unavailable")
	}
}
