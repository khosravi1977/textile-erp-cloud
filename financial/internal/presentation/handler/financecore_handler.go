package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/erpsystem/textile-erp/internal/application/financecore"
	"github.com/erpsystem/textile-erp/internal/infrastructure/persistence/postgres"
	"github.com/erpsystem/textile-erp/internal/platform/requestctx"
)

func (h *APIHandler) financeCore() *financecore.Service {
	if postgres.DB == nil {
		return nil
	}
	if h.financeCoreService == nil {
		h.financeCoreService = financecore.New(postgres.DB)
	}
	return h.financeCoreService
}

type typedTransactionRequest struct {
	financecore.TransactionRequest
	PartyName string `json:"party_name"`
	Group     string `json:"group"`
	Subgroup  string `json:"subgroup"`
}

// decodeTypedTransaction parses the posting payload and resolves the party:
// party_id wins; otherwise an exact party_name match is required — the
// engine never invents parties.
func (h *APIHandler) decodeTypedTransaction(w http.ResponseWriter, r *http.Request) (financecore.TransactionRequest, bool) {
	var payload typedTransactionRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid JSON payload")
		return financecore.TransactionRequest{}, false
	}
	req := payload.TransactionRequest
	service := h.financeCore()
	if service == nil {
		RespondError(w, http.StatusServiceUnavailable, "Database is not available")
		return financecore.TransactionRequest{}, false
	}
	if req.PartyID == 0 && strings.TrimSpace(payload.PartyName) != "" {
		partyID, found, err := service.ResolvePartyByName(r.Context(), requestctx.CompanyID(r.Context()), payload.PartyName)
		if err != nil {
			RespondError(w, http.StatusInternalServerError, err.Error())
			return financecore.TransactionRequest{}, false
		}
		if !found {
			// Keep posting possible for non-party types; party-required types
			// will surface NEEDS_REVIEW instead of guessing a counterparty.
			req.RawSourceReference = strings.TrimSpace(payload.PartyName)
		} else {
			req.PartyID = partyID
		}
	}
	return req, true
}

// TypedTransactionsRoot routes POST (create) and GET (list) on the v1 path.
func (h *APIHandler) TypedTransactionsRoot(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.CreateTypedTransaction(w, r)
	case http.MethodGet:
		h.ListTypedTransactions(w, r)
	default:
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// MobileTypedTransaction is the device-authenticated HesabYar posting path
// (v1 app). It posts the canonical typed record voucherless — the GL stays
// with the workspace ledger engine — and mirrors the event into the workspace
// state so the bank & cash and expenses tabs keep showing every sync.
func (h *APIHandler) MobileTypedTransaction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	companyID := requestctx.CompanyID(r.Context())
	if !h.mobileDeviceActive(r, companyID) {
		RespondError(w, http.StatusUnauthorized, "Device is not paired")
		return
	}
	var payload typedTransactionRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}
	req := payload.TransactionRequest
	externalID := strings.TrimSpace(req.ExternalTransactionID)
	typedType := strings.ToUpper(strings.TrimSpace(req.TransactionType))
	if externalID == "" || req.Amount <= 0 || typedType == "" {
		RespondError(w, http.StatusBadRequest, "Invalid transaction")
		return
	}
	partyName := strings.TrimSpace(payload.PartyName)
	partyID := req.PartyID
	service := h.financeCore()
	if service == nil {
		RespondError(w, http.StatusServiceUnavailable, "Database is not available")
		return
	}
	if partyID == 0 && partyName != "" {
		if resolved, found, err := service.ResolvePartyByName(r.Context(), companyID, partyName); err != nil {
			RespondError(w, http.StatusInternalServerError, err.Error())
			return
		} else if found {
			partyID = resolved
		}
	}
	reviewQueued := false
	if financecore.TypeRequiresParty(typedType) && partyID == 0 {
		// Never post a party-required event against a guessed counterparty.
		if err := service.QueueReviewEntry(r.Context(), companyID, "HY-"+externalID,
			"typed party unmatched: "+firstNonEmpty(partyName, "-"), map[string]any{
				"external_id": externalID, "transaction_type": typedType,
				"amount": req.Amount, "party_name": partyName,
				"direction": req.Direction, "transaction_date": req.TransactionDate,
				"bank": req.BankAccountName,
			}); err != nil {
			RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		reviewQueued = true
	}
	bankTransactionID := int64(0)
	if !reviewQueued {
		typed := financecore.TransactionRequest{
			ExternalTransactionID: "HY-" + externalID,
			IdempotencyKey:        strings.TrimSpace(req.IdempotencyKey),
			BankAccountID:         req.BankAccountID,
			BankAccountName:       strings.TrimSpace(req.BankAccountName),
			Direction:             strings.ToUpper(strings.TrimSpace(req.Direction)),
			Amount:                req.Amount,
			TransactionDate:       strings.TrimSpace(req.TransactionDate),
			TransactionTime:       strings.TrimSpace(req.TransactionTime),
			TransactionType:       typedType,
			PartyID:               partyID,
			CounterAccountID:      req.CounterAccountID,
			CounterAccountName:    strings.TrimSpace(req.CounterAccountName),
			InterestAmount:        req.InterestAmount,
			Description:           strings.TrimSpace(req.Description),
			BankReference:         strings.TrimSpace(req.BankReference),
			Source:                financecore.SourceHesabyar,
			RawSourceReference:    partyName,
		}
		result, err := service.PostTransaction(r.Context(), companyID, requestctx.UserID(r.Context()), typed, false)
		if err != nil {
			RespondError(w, http.StatusBadRequest, err.Error())
			return
		}
		bankTransactionID = result.BankTransactionID
	}
	direction := strings.ToLower(strings.TrimSpace(req.Direction))
	if direction != "in" && direction != "out" {
		direction = "out"
	}
	stateReq := mobileTransactionRequest{
		ExternalID:      externalID,
		Title:           strings.TrimSpace(req.Description),
		Direction:       direction,
		TransactionType: legacyStateTypeForTyped(typedType),
		Bank:            strings.TrimSpace(req.BankAccountName),
		Group:           strings.TrimSpace(payload.Group),
		Subgroup:        strings.TrimSpace(payload.Subgroup),
		Customer:        partyName,
		CounterAccount:  strings.TrimSpace(req.CounterAccountName),
		Description:     strings.TrimSpace(req.Description),
		OccurredJalali:  strings.TrimSpace(req.TransactionDate),
		Amount:          req.Amount,
		TrackingNo:      strings.TrimSpace(req.BankReference),
	}
	stateResult, detail, err := h.persistMobileTransactionState(r, companyID, stateReq, stateReq.TransactionType, &typedStateMeta{
		TypedType:   typedType,
		PartyName:   partyName,
		ExpenseLike: typedExpenseLike(typedType),
	})
	switch stateResult {
	case mobileStateSaved, mobileStateDuplicate:
		status := "synced"
		if stateResult == mobileStateDuplicate {
			status = "duplicate"
		}
		if reviewQueued {
			status = "needs_review"
		}
		RespondJSON(w, http.StatusCreated, map[string]any{
			"status": status, "erp_transaction_id": bankTransactionID,
		})
	case mobileStateInvalid:
		RespondError(w, http.StatusBadRequest, detail)
	case mobileStateConflict:
		RespondError(w, http.StatusConflict, "Workspace changed; retry sync")
	default:
		RespondError(w, http.StatusInternalServerError, err.Error())
	}
}

// MobileListParties is the device-authenticated party sync path.
func (h *APIHandler) MobileListParties(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if !h.mobileDeviceActive(r, requestctx.CompanyID(r.Context())) {
		RespondError(w, http.StatusUnauthorized, "Device is not paired")
		return
	}
	h.ListParties(w, r)
}

// CreateTypedTransaction posts a typed bank transaction (HesabYar v1 API).
func (h *APIHandler) CreateTypedTransaction(w http.ResponseWriter, r *http.Request) {
	h.handleTypedTransaction(w, r, true)
}

// CreateTypedTransactionNoGL posts a typed record whose ledger is maintained
// by another source (legacy workspace state) to avoid double-posting.
func (h *APIHandler) CreateTypedTransactionNoGL(w http.ResponseWriter, r *http.Request) {
	h.handleTypedTransaction(w, r, false)
}

func (h *APIHandler) handleTypedTransaction(w http.ResponseWriter, r *http.Request, withGL bool) {
	if r.Method != http.MethodPost {
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	service := h.financeCore()
	if service == nil {
		RespondError(w, http.StatusServiceUnavailable, "Database is not available")
		return
	}
	req, ok := h.decodeTypedTransaction(w, r)
	if !ok {
		return
	}
	result, err := service.PostTransaction(
		r.Context(),
		requestctx.CompanyID(r.Context()),
		requestctx.UserID(r.Context()),
		req,
		withGL,
	)
	if err != nil {
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}
	status := http.StatusCreated
	if result.Status == "duplicate" {
		status = http.StatusOK
	}
	RespondJSON(w, status, result)
}

// ListTypedTransactions returns the typed bank & cash ledger.
func (h *APIHandler) ListTypedTransactions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	service := h.financeCore()
	if service == nil {
		RespondError(w, http.StatusServiceUnavailable, "Database is not available")
		return
	}
	limit, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit")))
	transactions, err := service.ListTransactions(r.Context(), requestctx.CompanyID(r.Context()), limit)
	if err != nil {
		RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	RespondJSON(w, http.StatusOK, map[string]any{"transactions": transactions})
}

// ReverseTypedTransaction voids a posted transaction with a mirrored voucher.
func (h *APIHandler) ReverseTypedTransaction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	service := h.financeCore()
	if service == nil {
		RespondError(w, http.StatusServiceUnavailable, "Database is not available")
		return
	}
	var payload struct {
		BankTransactionID int64  `json:"bank_transaction_id"`
		Reason            string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.BankTransactionID <= 0 {
		RespondError(w, http.StatusBadRequest, "bank_transaction_id is required")
		return
	}
	if err := service.ReverseTransaction(
		r.Context(),
		requestctx.CompanyID(r.Context()),
		requestctx.UserID(r.Context()),
		payload.BankTransactionID,
		payload.Reason,
	); err != nil {
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}
	RespondJSON(w, http.StatusOK, map[string]any{"status": "voided"})
}

// ListParties returns the party sync payload (optionally by role).
func (h *APIHandler) ListParties(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	service := h.financeCore()
	if service == nil {
		RespondError(w, http.StatusServiceUnavailable, "Database is not available")
		return
	}
	parties, err := service.ListParties(r.Context(), requestctx.CompanyID(r.Context()), r.URL.Query().Get("role"))
	if err != nil {
		RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	RespondJSON(w, http.StatusOK, map[string]any{"parties": parties})
}

// PartyLedgerHandler returns the ledger of one party.
func (h *APIHandler) PartyLedgerHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	service := h.financeCore()
	if service == nil {
		RespondError(w, http.StatusServiceUnavailable, "Database is not available")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	partyID, err := strconv.ParseInt(parts[len(parts)-1], 10, 64)
	if err != nil || partyID <= 0 {
		RespondError(w, http.StatusBadRequest, "party id is required")
		return
	}
	entries, err := service.PartyLedger(r.Context(), requestctx.CompanyID(r.Context()), partyID)
	if err != nil {
		RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	RespondJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

// ListBankAccountsHandler returns bank/cash accounts for sync.
func (h *APIHandler) ListBankAccountsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	service := h.financeCore()
	if service == nil {
		RespondError(w, http.StatusServiceUnavailable, "Database is not available")
		return
	}
	accounts, err := service.ListBankAccounts(r.Context(), requestctx.CompanyID(r.Context()))
	if err != nil {
		RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	RespondJSON(w, http.StatusOK, map[string]any{"accounts": accounts})
}
