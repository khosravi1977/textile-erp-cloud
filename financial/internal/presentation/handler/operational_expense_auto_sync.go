package handler

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/erpsystem/textile-erp/internal/infrastructure/operationalbridge"
	"github.com/erpsystem/textile-erp/internal/platform/requestctx"
)

// WorkspaceRootAutomated synchronizes employee-entered operational expenses
// before returning the financial workspace. HesabYar expenses already use the
// same no-approval model: MobileTransaction writes expense + movement directly.
func (h *APIHandler) WorkspaceRootAutomated(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if err := h.syncOperationalExpenses(r); err != nil {
			// Never make the financial workspace unavailable because the operational
			// bridge is temporarily down. Serve the last good state and keep the error
			// in server logs.
			log.Printf("operational expense auto-sync skipped: %v", err)
		}
		if err := h.syncHesabyarTransactionsIntoWorkspace(r); err != nil {
			// HesabYar may have posted the typed core transaction while a workspace
			// revision conflict prevented the UI rows from being written. Keep the
			// UI recoverable on the next workspace read instead of losing the event.
			log.Printf("HesabYar workspace backfill skipped: %v", err)
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

	sourceRows, err := bridge.Expenses(10000)
	if err != nil || len(sourceRows) == 0 {
		return err
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
		if !mergeOperationalExpensesIntoState(state, sourceRows, accountID, time.Now().UTC()) {
			return nil
		}
		raw, err := json.Marshal(state)
		if err != nil {
			return err
		}
		payload, checksum, err := validateWorkspaceState(raw)
		if err != nil {
			return err
		}
		revision := doc.Revision
		_, err = saveWorkspace(
			r,
			companyID,
			0, // system-originated sync; no second accountant approval/actor
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

	// A manager can configure the default once. This avoids asking an accountant
	// to approve every single employee-entered expense.
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

	// Backward compatibility: the previous manual form preselected the first
	// account and only waited for a redundant Save click. Keep the same default
	// while removing that per-record approval step.
	return strings.TrimSpace(stringValue(accounts[0]["id"]))
}

// mergeOperationalExpensesIntoState performs an idempotent upsert. If an
// employee edits the source expense, financial amount/date/category/description
// are updated automatically. A finance-side account override is preserved.
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
			existingExpense = cloneOperationalSyncMap(expenses[index])
			if id := strings.TrimSpace(stringValue(existingExpense["id"])); id != "" {
				expenseID = id
			}
			if id := strings.TrimSpace(stringValue(existingExpense["accountId"])); id != "" {
				accountID = id
			}
		}
		desiredExpense := cloneOperationalSyncMap(existingExpense)
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
			if !operationalSyncMapsEqual(expenses[index], desiredExpense) {
				expenses[index] = desiredExpense
				changed = true
			}
		} else {
			expenseIndex[sourceID] = len(expenses)
			expenses = append(expenses, desiredExpense)
			changed = true
		}

		movementPos := -1
		if index, ok := movementIndex[sourceID]; ok {
			movementPos = index
		} else {
			// Older versions created the linked movement manually and did not copy
			// source_type/sourceId onto it. Adopt that movement instead of creating
			// a second cash withdrawal during migration to automatic posting.
			for index, movement := range movements {
				if strings.TrimSpace(stringValue(movement["sourceExpense"])) == expenseID {
					movementPos = index
					break
				}
			}
		}

		movementID := "mov-operational-expense-" + sourceID
		existingMovement := map[string]any{}
		if movementPos >= 0 {
			existingMovement = cloneOperationalSyncMap(movements[movementPos])
			if id := strings.TrimSpace(stringValue(existingMovement["id"])); id != "" {
				movementID = id
			}
		}
		desiredMovement := cloneOperationalSyncMap(existingMovement)
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

		if movementPos >= 0 {
			if !operationalSyncMapsEqual(movements[movementPos], desiredMovement) {
				movements[movementPos] = desiredMovement
				changed = true
			}
			movementIndex[sourceID] = movementPos
		} else {
			movementIndex[sourceID] = len(movements)
			movements = append(movements, desiredMovement)
			changed = true
		}
	}
	if changed {
		state["expenses"] = operationalSyncRowsToAny(expenses)
		state["movements"] = operationalSyncRowsToAny(movements)
	}
	return changed
}

func cloneOperationalSyncMap(input map[string]any) map[string]any {
	out := make(map[string]any, len(input)+8)
	for key, value := range input {
		out[key] = value
	}
	return out
}

func operationalSyncMapsEqual(left, right map[string]any) bool {
	a := cloneOperationalSyncMap(left)
	b := cloneOperationalSyncMap(right)
	delete(a, "syncedAt")
	delete(b, "syncedAt")
	return reflect.DeepEqual(a, b)
}

func operationalSyncRowsToAny(rows []map[string]any) []any {
	out := make([]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, row)
	}
	return out
}
