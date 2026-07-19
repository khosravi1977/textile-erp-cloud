package handler

import "testing"

func TestLegacyHesabYarTransactionCreatesExpenseAndMovement(t *testing.T) {
	state := map[string]any{
		"accounts":           []any{},
		"movements":          []any{},
		"expenses":           []any{},
		"mobileTransactions": []any{},
		"journalEntries":     []any{},
	}
	req := legacyHesabYarTransaction(map[string]any{
		"id":             "101",
		"title":          "خرید ملزومات",
		"amount":         "250000",
		"type":           "expense",
		"account":        "بانک ملت",
		"category":       "اداری / ملزومات",
		"customer":       "دفتر",
		"jalaliDateTime": "1405/04/28 10:30",
		"reviewed":       true,
	})
	if req.Direction != "out" {
		t.Fatalf("expected out direction, got %q", req.Direction)
	}
	appendMobileTransactionState(state, req, "2026-07-18T00:00:00Z")
	if got := len(rowsFrom(state, "movements")); got != 1 {
		t.Fatalf("expected one bank movement, got %d", got)
	}
	if got := len(rowsFrom(state, "expenses")); got != 1 {
		t.Fatalf("expected one expense, got %d", got)
	}
	if !mobileTransactionExists(state, "101") {
		t.Fatal("expected mobile transaction to be deduplicatable by external id")
	}
}

func TestLegacyHesabYarTransferDoesNotCreateExpense(t *testing.T) {
	state := map[string]any{
		"accounts":           []any{},
		"movements":          []any{},
		"expenses":           []any{},
		"mobileTransactions": []any{},
		"journalEntries":     []any{},
	}
	req := mobileTransactionRequest{
		ExternalID:     "tr-1",
		Title:          "انتقال وجه",
		Amount:         500000,
		Direction:      "out",
		AccountID:      "بانک ملی",
		Group:          "انتقال",
		Subgroup:       "بین حساب‌ها",
		CounterAccount: "بانک ملت",
	}
	appendMobileTransactionState(state, req, "2026-07-18T00:00:00Z")
	if got := len(rowsFrom(state, "movements")); got != 2 {
		t.Fatalf("expected two transfer movements, got %d", got)
	}
	if got := len(rowsFrom(state, "expenses")); got != 0 {
		t.Fatalf("expected no expense for transfer, got %d", got)
	}
}
