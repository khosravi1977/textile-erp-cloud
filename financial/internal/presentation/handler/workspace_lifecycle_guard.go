package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/erpsystem/textile-erp/internal/platform/requestctx"
)

// WorkspaceRootAudited adds cross-document lifecycle checks that require both
// the previous and proposed workspace states. The original WorkspaceRoot still
// performs payload validation, permission-scoped merging, optimistic revision
// control, accounting validation, persistence and ledger synchronization.
func (h *APIHandler) WorkspaceRootAudited(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		h.WorkspaceRoot(w, r)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxWorkspacePayload)
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid workspace payload")
		return
	}
	var request struct {
		State    json.RawMessage `json:"state"`
		Revision *int64          `json:"revision"`
	}
	if err := json.Unmarshal(payload, &request); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid workspace payload")
		return
	}
	proposed, _, err := validateWorkspaceState(request.State)
	if err != nil {
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}
	current, err := loadWorkspace(r, requestctx.CompanyID(r.Context()))
	if err != nil {
		RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	effective := proposed
	if requestctx.IsPortalAccess(r.Context()) {
		effective, _, err = mergeWorkspaceState(current.State, proposed, writableWorkspaceFields(r.Context()))
		if err != nil {
			if errors.Is(err, errWorkspaceWriteForbidden) {
				RespondError(w, http.StatusForbidden, err.Error())
			} else {
				RespondError(w, http.StatusBadRequest, err.Error())
			}
			return
		}
	}
	if err := validateWorkspaceLifecycleChanges(decodeWorkspaceMap(current.State), decodeWorkspaceMap(effective)); err != nil {
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Rewind exactly the original request so WorkspaceRoot remains the single
	// persistence implementation and still performs its own conflict checks.
	r.Body = io.NopCloser(bytes.NewReader(payload))
	h.WorkspaceRoot(w, r)
}

func validateWorkspaceLifecycleChanges(oldState, newState map[string]any) error {
	oldDocs := make(map[string]map[string]any)
	for _, doc := range rowsFrom(oldState, "receivableDocs") {
		if id := firstText(doc, "id", "checkNo"); id != "" {
			oldDocs[id] = doc
		}
	}
	newDocs := make(map[string]map[string]any)
	for _, doc := range rowsFrom(newState, "receivableDocs") {
		if id := firstText(doc, "id", "checkNo"); id != "" {
			newDocs[id] = doc
		}
	}

	assignmentReferences := make(map[string]int)
	for _, invoice := range rowsFrom(newState, "incomingInvoices") {
		invoiceID := firstText(invoice, "id", "sourceId")
		party := strings.TrimSpace(stringValue(invoice["customer"]))
		for _, payment := range rowsFrom(invoice, "payments") {
			if stringValue(payment["type"]) != "assign_receivable" {
				continue
			}
			docID := strings.TrimSpace(stringValue(payment["docId"]))
			if docID == "" {
				return errors.New("واگذاری چک دریافتی بدون شناسه سند مجاز نیست")
			}
			doc, ok := newDocs[docID]
			if !ok {
				return errors.New("چک دریافتی انتخاب‌شده برای واگذاری پیدا نشد")
			}
			if strings.ToLower(strings.TrimSpace(stringValue(doc["status"]))) != "assigned" {
				return errors.New("چک واگذار‌شده باید در وضعیت assigned ثبت شود")
			}
			if strings.TrimSpace(stringValue(doc["assignedIncomingInvoice"])) != invoiceID {
				return errors.New("ارتباط چک واگذار‌شده با فاکتور خرید یکسان نیست")
			}
			if !strings.EqualFold(strings.TrimSpace(stringValue(doc["assignedTo"])), party) {
				return errors.New("طرف حساب چک واگذار‌شده با فروشنده فاکتور یکسان نیست")
			}
			if !amountsEqual(number(payment["amount"]), number(doc["amount"])) {
				return errors.New("مبلغ واگذاری باید با مبلغ چک دریافتی یکسان باشد")
			}
			assignmentReferences[docID]++
			if assignmentReferences[docID] > 1 {
				return errors.New("یک چک دریافتی نمی‌تواند هم‌زمان به بیش از یک فاکتور واگذار شود")
			}
		}
	}

	for id, doc := range newDocs {
		status := strings.ToLower(strings.TrimSpace(stringValue(doc["status"])))
		if status != "assigned" {
			continue
		}
		previous, existed := oldDocs[id]
		previousStatus := strings.ToLower(strings.TrimSpace(stringValue(previous["status"])))
		if !existed {
			return errors.New("سند دریافتی جدید باید ابتدا با وضعیت باز ثبت شود و سپس واگذار شود")
		}
		if previousStatus != "open" && previousStatus != "assigned" {
			return errors.New("فقط چک دریافتی باز قابل واگذاری است")
		}
		assignedTo := strings.TrimSpace(stringValue(doc["assignedTo"]))
		if assignedTo == "" {
			return errors.New("طرف حساب واگذاری چک الزامی است")
		}
		linkedInvoice := strings.TrimSpace(stringValue(doc["assignedIncomingInvoice"]))
		if linkedInvoice != "" {
			if assignmentReferences[id] != 1 {
				return errors.New("چک واگذار‌شده باید دقیقاً به یک فاکتور خرید متصل باشد")
			}
		} else if !knownSupplierParty(newState, assignedTo) {
			return errors.New("واگذاری دستی چک فقط به طرف حساب تامین‌کننده/بستانکار ثبت‌شده مجاز است")
		}
	}
	return nil
}

func knownSupplierParty(state map[string]any, party string) bool {
	party = strings.TrimSpace(party)
	if party == "" {
		return false
	}
	for _, invoice := range rowsFrom(state, "incomingInvoices") {
		if strings.EqualFold(strings.TrimSpace(stringValue(invoice["customer"])), party) {
			return true
		}
	}
	for _, opening := range rowsFrom(state, "openingBalances") {
		if strings.EqualFold(strings.TrimSpace(stringValue(opening["type"])), "payable") && strings.EqualFold(strings.TrimSpace(stringValue(opening["customer"])), party) {
			return true
		}
	}
	return false
}
