package handler

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/erpsystem/textile-erp/internal/infrastructure/operationalbridge"
	"github.com/erpsystem/textile-erp/internal/platform/requestctx"
)

// WorkspaceRootAutomated keeps the financial workspace synchronized with
// employee-entered operational expenses before returning the workspace.
// HesabYar mobile expenses are already written directly by MobileTransaction.
func (h *APIHandler) WorkspaceRootAutomated(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if err := h.syncOperationalExpenses(r); err != nil {
			// Operational synchronization must never make the financial workspace
			// unavailable. Keep the error server-side and serve the last good state.
			log.Printf("operational expense auto-sync skipped: %v", err)
		}
	}
	h.WorkspaceRootAudited(w, r)
}

func (h *APIHandler) syncOperationalExpenses(r *http.Request) error {
	if h.operational == nil {
		return nil
	}
	companyID := requestctx.CompanyID(r.Context())
	if companyID <= 0 {
		return nil
	}
	bridge, cleanup, err := h.operational.ForCompany(r.Context(), companyID)
	if err != nil {
		return err
	}
	defer cleanup()

	rows, err := bridge.Expenses(10000)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}

	for attempt := 0; attempt < 3; attempt++ {
		doc, err := loadWorkspace(r, companyID)
		if err != nil {
			return err
		}
		state := decodeWorkspaceMap(doc.State)
		accountID := resolveOperationalExpenseAccountID(state)
		if accountID == "" {
			return errors.New("no financial cash/bank account is available for automatic operational expense posting")
		}
		if !mergeOperationalExpensesIntoState(state, rows, accountID, time.Now().UTC()) {
			return nil
		}
		payload, checksum, err := validateWorkspaceState(mustJSON(state))
		if err != nil {
			return err
		}
		revision := doc.Revision
		_, err = saveWorkspace(
			r,
			companyID,
			0, // system-originated sync, not a second accountant approval
			&revision,
			payload,
			checksum,
			map[string]bool{"expenses": true, "movements": true},
		)
		if err == nil {
			return nil
		}
		var conflict workspaceConflict
		if !errors.As(err, &conflict) {
			return err
		}
	}
	return errors.New("workspace changed repeatedly while operational expenses were being synchronized")
}

func mustJSON(state map[string]any) []byte {
	payload, _, err := validateWorkspaceStateFromMap(state)
	if err != nil {
		return []byte(`{}`)
	}
	return payload
}

func validateWorkspaceStateFromMap(state map[string]any) ([]byte, string, error) {
	// Reuse the canonical workspace validator without introducing a second JSON
	// normalization implementation.
	payload := []byte("{}")
	if state != nil {
		var err error
		payload, err = marshalWorkspaceState(state)
		if err != nil {
			return nil, "", err
		}
	}
	return validateWorkspaceState(payload)
}

func marshalWorkspaceState(state map[string]any) ([]byte, error) {
	return jsonMarshal(state)
}

// Indirection keeps tests deterministic and allows this file to avoid touching
// persistence internals merely to encode the workspace document.
var jsonMarshal = func(value any) ([]byte, error) {
	return json.Marshal(value)
}

func resolveOperationalExpenseAccountID(state map[string]any) string {
	accounts := rowsFrom(state, "accounts")
	if len(accounts) == 0 {
		return ""
	}
	valid := map[string]bool{}
	for _, account := range accounts {
		if id := strings.TrimSpace(stringValue(account["id"])); id != "" {
			valid[id] = true
		}
	}
	if settings, ok := state["accountingSettings"].(map[string]any); ok {
		for _, key := range []string{"operationalExpenseAccountId", "defaultExpenseAccountId"} {
			id := strings.TrimSpace(stringValue(settings[key]))
			if valid[id] {
				return id
			}
		}
	}
	for _, account := range accounts {
		if boolValue(account["defaultForOperationalExpenses"]) || boolValue(account["defaultOperationalExpense"]) {
			if id := strings.TrimSpace(stringValue(account["id"])); id != "" {
				return id
			}
		}
	}
	// Backward-compatible behavior: the old manual form preselected the first
	// financial account and required an accountant to click Save. Auto-posting
	// keeps that same default but removes the redundant per-expense approval.
	return strings.TrimSpace(stringValue(accounts[0]["id"]))
}

func mergeOperationalExpensesIntoState(state map[string]any, source []operationalbridge.ExpenseRow, defaultAccountID string, syncedAt time.Time) bool {
	if state == nil || strings.TrimSpace(defaultAccountID) == "" {
		return false
	}
	expenses := rowsFrom(state, "expenses")
	movements := rowsFrom(state, "movements")
	expenseIndex := map[string]int{}
	movementIndex := map[string]int{}
	for i, row := range expenses {
		if stringValue(row["source_type"]) == "operational_expense" {
			expenseIndex[strings.TrimSpace(stringValue(row["sourceId"]))] = i
		}
	}
	for i, row := range movements {
		if stringValue(row["source_type"]) == "operational_expense" {
			movementIndex[strings.TrimSpace(stringValue(row["sourceId"]))] = i
		}
	}

	changed := false
	for _, row := range source {
		if row.ID <= 0 || row.Amount <= 0 {
			continue
		}
		sourceID := strconv.FormatInt(row.ID, 10)
		date := mobileAccountingDate(row.Date)
		group := strings.TrimSpace(row.Title)
		if group == "" {
			group = "سایر"
		}
		subgroup := strings.TrimSpace(row.Weaver)
		if subgroup == "" {
			subgroup = strings.TrimSpace(row.Operator)
		}
		if subgroup == "" {
			subgroup = "سایر"
		}

		expenseID := "exp-operational-" + sourceID
		accountID := defaultAccountID
		existingExpense := map[string]any{}
		if index, ok := expenseIndex[sourceID]; ok {
			existingExpense = cloneMap(expenses[index])
			if id := strings.TrimSpace(stringValue(existingExpense["id"])); id != "" {
				expenseID = id
			}
			if id := strings.TrimSpace(stringValue(existingExpense["accountId"])); id != "" {
				accountID = id
			}
		}
		desiredExpense := cloneMap(existingExpense)
		desiredExpense["id"] = expenseID
		desiredExpense["date"] = date
		desiredExpense["operationalDate"] = row.Date
		desiredExpense["group"] = group
		desiredExpense["subgroup"] = subgroup
		desiredExpense["amount"] = row.Amount
		desiredExpense["description"] = row.Description
		desiredExpense["doc_no"] = row.DocNo
		desiredExpense["enteredBy"] = row.Operator
		desiredExpense["accountId"] = accountID
		desiredExpense["source_type"] = "operational_expense"
		desiredExpense["sourceId"] = sourceID
		desiredExpense["autoPosted"] = true
		desiredExpense["approvalRequired"] = false
		desiredExpense["syncedAt"] = syncedAt.Format(time.RFC3339Nano)

		if index, ok := expenseIndex[sourceID]; ok {
			if !mapsEqualIgnoringSyncTime(expenses[index], desiredExpense) {
				expenses[index] = desiredExpense
				changed = true
			}
		} else {
			expenses = append([]map[string]any{desiredExpense}, expenses...)
			expenseIndex[sourceID] = 0
			changed = true
		}

		movementID := "mov-operational-expense-" + sourceID
		existingMovement := map[string]any{}
		if index, ok := movementIndex[sourceID]; ok {
			existingMovement = cloneMap(movements[index])
			if id := strings.TrimSpace(stringValue(existingMovement["id"])); id != "" {
				movementID = id
			}
		}
		desiredMovement := cloneMap(existingMovement)
		desiredMovement["id"] = movementID
		desiredMovement["accountId"] = accountID
		desiredMovement["date"] = date
		desiredMovement["direction"] = "out"
		desiredMovement["transactionType"] = "expense"
		desiredMovement["amount"] = row.Amount
		desiredMovement["description"] = row.Description
		desiredMovement["sourceExpense"] = expenseID
		desiredMovement["source_type"] = "operational_expense"
		desiredMovement["sourceId"] = sourceID
		desiredMovement["autoPosted"] = true
		desiredMovement["approvalRequired"] = false
		desiredMovement["syncedAt"] = syncedAt.Format(time.RFC3339Nano)

		if index, ok := movementIndex[sourceID]; ok {
			if !mapsEqualIgnoringSyncTime(movements[index], desiredMovement) {
				movements[index] = desiredMovement
				changed = true
			}
		} else {
			movements = append([]map[string]any{desiredMovement}, movements...)
			movementIndex[sourceID] = 0
			changed = true
		}
	}
	if changed {
		state["expenses"] = mapRowsToAny(expenses)
		state["movements"] = mapRowsToAny(movements)
	}
	return changed
}

func cloneMap(input map[string]any) map[string]any {
	out := make(map[string]any, len(input)+8)
	for key, value := range input {
		out[key] = value
	}
	return out
}

func mapsEqualIgnoringSyncTime(left, right map[string]any) bool {
	a := cloneMap(left)
	b := cloneMap(right)
	delete(a, "syncedAt")
	delete(b, "syncedAt")
	return reflect.DeepEqual(a, b)
}

func mapRowsToAny(rows []map[string]any) []any {
	out := make([]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, row)
	}
	return out
}

func operationalExpenseSourceKey(row operationalbridge.ExpenseRow) string {
	return fmt.Sprintf("operational_expense:%d", row.ID)
}
