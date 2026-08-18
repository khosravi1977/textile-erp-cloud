package handler

import (
	"testing"
	"time"

	"github.com/erpsystem/textile-erp/internal/infrastructure/operationalbridge"
)

func TestOperationalExpenseAutoSyncCreatesExpenseAndMovementWithoutApproval(t *testing.T) {
	state := map[string]any{
		"accounts": []any{map[string]any{"id": "cash-main", "name": "صندوق", "type": "صندوق", "opening": 1000.0}},
		"expenses": []any{},
		"movements": []any{},
	}
	source := []operationalbridge.ExpenseRow{{
		ID: 42, Date: "1405/05/28", Title: "برق", Operator: "javad", Amount: 250, Description: "هزینه سالن", DocNo: "OP-42",
	}}
	if !mergeOperationalExpensesIntoState(state, source, "cash-main", time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("new operational expense was not synchronized")
	}
	expenses := rowsFrom(state, "expenses")
	movements := rowsFrom(state, "movements")
	if len(expenses) != 1 || len(movements) != 1 {
		t.Fatalf("expense/movement counts = %d/%d, want 1/1", len(expenses), len(movements))
	}
	expense := expenses[0]
	if stringValue(expense["source_type"]) != "operational_expense" || stringValue(expense["sourceId"]) != "42" {
		t.Fatalf("source identity lost: %#v", expense)
	}
	if !boolValue(expense["autoPosted"]) || boolValue(expense["approvalRequired"]) {
		t.Fatalf("operational expense still requires redundant approval: %#v", expense)
	}
	if stringValue(expense["accountId"]) != "cash-main" || number(expense["amount"]) != 250 {
		t.Fatalf("expense destination/amount wrong: %#v", expense)
	}
	movement := movements[0]
	if stringValue(movement["sourceExpense"]) != stringValue(expense["id"]) || stringValue(movement["transactionType"]) != "expense" || stringValue(movement["direction"]) != "out" {
		t.Fatalf("cash movement was not linked to auto-posted expense: %#v", movement)
	}
}

func TestOperationalExpenseAutoSyncIsIdempotent(t *testing.T) {
	state := map[string]any{
		"accounts": []any{map[string]any{"id": "cash-main"}},
		"expenses": []any{},
		"movements": []any{},
	}
	source := []operationalbridge.ExpenseRow{{ID: 7, Date: "2026-08-18", Title: "تعمیرات", Amount: 100}}
	now := time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)
	if !mergeOperationalExpensesIntoState(state, source, "cash-main", now) {
		t.Fatal("first sync did not change state")
	}
	if mergeOperationalExpensesIntoState(state, source, "cash-main", now.Add(time.Minute)) {
		t.Fatal("same source expense was duplicated or rewritten only because sync time changed")
	}
	if len(rowsFrom(state, "expenses")) != 1 || len(rowsFrom(state, "movements")) != 1 {
		t.Fatal("idempotent sync produced duplicate financial records")
	}
}

func TestOperationalExpenseEditUpdatesFinancialRecordAndPreservesAccountOverride(t *testing.T) {
	state := map[string]any{
		"accounts": []any{map[string]any{"id": "cash-main"}, map[string]any{"id": "bank-special"}},
		"expenses": []any{map[string]any{
			"id": "existing-exp", "source_type": "operational_expense", "sourceId": "9", "accountId": "bank-special", "date": "2026-08-18", "amount": 100.0,
		}},
		"movements": []any{map[string]any{
			"id": "existing-mov", "source_type": "operational_expense", "sourceId": "9", "accountId": "bank-special", "sourceExpense": "existing-exp", "direction": "out", "transactionType": "expense", "date": "2026-08-18", "amount": 100.0,
		}},
	}
	source := []operationalbridge.ExpenseRow{{ID: 9, Date: "2026-08-19", Title: "برق", Amount: 175, Description: "اصلاح مبلغ"}}
	if !mergeOperationalExpensesIntoState(state, source, "cash-main", time.Now()) {
		t.Fatal("source edit did not update financial record")
	}
	expense := rowsFrom(state, "expenses")[0]
	movement := rowsFrom(state, "movements")[0]
	if number(expense["amount"]) != 175 || number(movement["amount"]) != 175 {
		t.Fatalf("edited amount did not propagate: expense=%v movement=%v", expense["amount"], movement["amount"])
	}
	if stringValue(expense["accountId"]) != "bank-special" || stringValue(movement["accountId"]) != "bank-special" {
		t.Fatal("finance-side account override was overwritten by automatic sync")
	}
}

func TestOperationalExpenseAccountResolutionUsesConfiguredDefault(t *testing.T) {
	state := map[string]any{
		"accounts": []any{map[string]any{"id": "first"}, map[string]any{"id": "configured"}},
		"accountingSettings": map[string]any{"operationalExpenseAccountId": "configured"},
	}
	if got := resolveOperationalExpenseAccountID(state); got != "configured" {
		t.Fatalf("resolved account = %q, want configured", got)
	}
}
