package handler

import (
	"testing"
	"time"

	"github.com/erpsystem/textile-erp/internal/infrastructure/operationalbridge"
)

func TestOperationalExpenseAutoSyncCreatesExpenseAndMovementWithoutApproval(t *testing.T) {
	state := map[string]any{
		"accounts":  []any{map[string]any{"id": "cash-main", "name": "صندوق", "type": "صندوق", "opening": 1000.0}},
		"expenses":  []any{},
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
	if stringValue(expense["source"]) != "عملیاتی" || stringValue(expense["operationalDate"]) != "1405/05/28" || stringValue(expense["group"]) != "برق" || stringValue(expense["subgroup"]) != "سایر" || stringValue(expense["doc_no"]) != "OP-42" || stringValue(expense["documentNo"]) != "OP-42" || stringValue(expense["enteredBy"]) != "javad" {
		t.Fatalf("operational expense field mapping is wrong: %#v", expense)
	}
	movement := movements[0]
	if stringValue(movement["sourceExpense"]) != stringValue(expense["id"]) || stringValue(movement["transactionType"]) != "expense" || stringValue(movement["direction"]) != "out" {
		t.Fatalf("cash movement was not linked to auto-posted expense: %#v", movement)
	}
	if stringValue(movement["sourceExpenseTraceId"]) != "OP-42" {
		t.Fatalf("cash movement did not keep expense trace id: %#v", movement)
	}
}

func TestOperationalExpenseAutoSyncMapsWeaverToFinancialSubgroup(t *testing.T) {
	state := map[string]any{
		"accounts":  []any{map[string]any{"id": "cash-main"}},
		"expenses":  []any{},
		"movements": []any{},
	}
	source := []operationalbridge.ExpenseRow{{
		ID: 43, Date: "1405/05/29", Title: "خرید پاکستانی", Operator: "کارت تنخواه", Weaver: "پاکستانیها", Amount: 400000, Description: "شرح", DocNo: "7528",
	}}
	if !mergeOperationalExpensesIntoState(state, source, "cash-main", time.Now()) {
		t.Fatal("operational expense was not synchronized")
	}
	expense := rowsFrom(state, "expenses")[0]
	if stringValue(expense["source"]) != "عملیاتی" || stringValue(expense["group"]) != "خرید پاکستانی" || stringValue(expense["subgroup"]) != "پاکستانیها" || stringValue(expense["amount"]) == "" || stringValue(expense["doc_no"]) != "7528" || stringValue(expense["description"]) != "شرح" {
		t.Fatalf("field mapping mismatch: %#v", expense)
	}
}

func TestOperationalExpenseAutoSyncIsIdempotent(t *testing.T) {
	state := map[string]any{
		"accounts":  []any{map[string]any{"id": "cash-main"}},
		"expenses":  []any{},
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

func TestOperationalExpenseAutoSyncAdoptsLegacyManualMovementInsteadOfDuplicating(t *testing.T) {
	state := map[string]any{
		"accounts": []any{map[string]any{"id": "cash-main"}},
		"expenses": []any{map[string]any{
			"id": "legacy-exp", "source_type": "operational_expense", "sourceId": "12", "accountId": "cash-main", "date": "2026-08-18", "amount": 90.0,
		}},
		// Old UI created the movement with sourceExpense, but without source_type/sourceId.
		"movements": []any{map[string]any{
			"id": "legacy-mov", "sourceExpense": "legacy-exp", "accountId": "cash-main", "date": "2026-08-18", "direction": "out", "transactionType": "expense", "amount": 90.0,
		}},
	}
	source := []operationalbridge.ExpenseRow{{ID: 12, Date: "2026-08-18", Title: "سرویس", Amount: 90}}
	if !mergeOperationalExpensesIntoState(state, source, "cash-main", time.Now()) {
		t.Fatal("legacy manual posting was not adopted/tagged")
	}
	movements := rowsFrom(state, "movements")
	if len(movements) != 1 {
		t.Fatalf("legacy migration duplicated cash movement: got %d movements", len(movements))
	}
	if stringValue(movements[0]["id"]) != "legacy-mov" || stringValue(movements[0]["source_type"]) != "operational_expense" || stringValue(movements[0]["sourceId"]) != "12" {
		t.Fatalf("legacy linked movement was not adopted correctly: %#v", movements[0])
	}
}

func TestOperationalExpenseAccountResolutionUsesConfiguredDefault(t *testing.T) {
	state := map[string]any{
		"accounts":           []any{map[string]any{"id": "first"}, map[string]any{"id": "configured"}},
		"accountingSettings": map[string]any{"operationalExpenseAccountId": "configured"},
	}
	if got := resolveOperationalExpenseAccountID(state); got != "configured" {
		t.Fatalf("resolved account = %q, want configured", got)
	}
}

func TestOperationalExpensePayAccountStampsVerifiedPayment(t *testing.T) {
	state := map[string]any{
		"accounts":  []any{map[string]any{"id": "cash-main"}, map[string]any{"id": "bank-melli"}},
		"expenses":  []any{},
		"movements": []any{},
	}
	source := []operationalbridge.ExpenseRow{{
		ID: 77, Date: "1405/06/10", Title: "کرایه", Amount: 300, PayAccountID: "bank-melli", PayAccountName: "بانک ملی",
	}}
	if !mergeOperationalExpensesIntoState(state, source, "cash-main", time.Now()) {
		t.Fatal("pay-account expense was not synchronized")
	}
	expense := rowsFrom(state, "expenses")[0]
	movement := rowsFrom(state, "movements")[0]
	if stringValue(expense["accountId"]) != "bank-melli" {
		t.Fatalf("source pay account ignored: %#v", expense["accountId"])
	}
	if stringValue(movement["accountId"]) != "bank-melli" {
		t.Fatalf("movement does not follow the paying account: %#v", movement["accountId"])
	}
	verified, _ := expense["verifiedPayment"].(map[string]any)
	if stringValue(verified["accountId"]) != "bank-melli" || stringValue(verified["date"]) != "2026-09-01" || number(verified["amount"]) != 300 {
		t.Fatalf("verifiedPayment not stamped from source: %#v", expense["verifiedPayment"])
	}
}

func TestOperationalExpenseFinanceConfirmationSurvivesSourcePayAccount(t *testing.T) {
	state := map[string]any{
		"accounts": []any{map[string]any{"id": "cash-main"}, map[string]any{"id": "bank-special"}},
		"expenses": []any{map[string]any{
			"id": "exp-operational-9", "source_type": "operational_expense", "sourceId": "9",
			"accountId": "bank-special", "date": "2026-08-18", "amount": 100.0,
			"operationalDate": "1405/05/27", "group": "برق", "subgroup": "سایر", "source": "عملیاتی",
			"description": "", "doc_no": "", "documentNo": "", "enteredBy": "",
			"autoPosted": true, "approvalRequired": false,
			"verifiedPayment": map[string]any{"accountId": "bank-special", "date": "2026-08-18", "amount": 100.0},
			"payAccountSource": "bank-special", "payAccountName": "بانک خاص",
		}},
		"movements": []any{map[string]any{
			"id": "mov-operational-expense-9", "source_type": "operational_expense", "sourceId": "9",
			"accountId": "bank-special", "date": "2026-08-18", "direction": "out", "transactionType": "expense",
			"amount": 100.0, "description": "", "sourceExpense": "exp-operational-9", "sourceExpenseTraceId": "",
			"autoPosted": true, "approvalRequired": false,
		}},
	}
	source := []operationalbridge.ExpenseRow{{ID: 9, Date: "1405/05/27", Title: "برق", Amount: 100, PayAccountID: "cash-main"}}
	if mergeOperationalExpensesIntoState(state, source, "cash-main", time.Now()) {
		t.Fatal("finance-confirmed expense was rewritten by the source pay account")
	}
	expense := rowsFrom(state, "expenses")[0]
	if stringValue(expense["accountId"]) != "bank-special" {
		t.Fatalf("finance-side confirmed account was overridden: %#v", expense["accountId"])
	}
}

func TestOperationalExpenseInvalidPayAccountFallsBackToDefault(t *testing.T) {
	state := map[string]any{
		"accounts":  []any{map[string]any{"id": "cash-main"}},
		"expenses":  []any{},
		"movements": []any{},
	}
	source := []operationalbridge.ExpenseRow{{ID: 31, Date: "2026-08-18", Title: "ناهار", Amount: 60, PayAccountID: "ghost-account"}}
	if !mergeOperationalExpensesIntoState(state, source, "cash-main", time.Now()) {
		t.Fatal("expense was not synchronized")
	}
	expense := rowsFrom(state, "expenses")[0]
	if stringValue(expense["accountId"]) != "cash-main" {
		t.Fatalf("unknown pay account was not rejected: %#v", expense["accountId"])
	}
	if _, exists := expense["verifiedPayment"]; exists {
		t.Fatalf("verifiedPayment stamped for an unvalidated account: %#v", expense["verifiedPayment"])
	}
}
