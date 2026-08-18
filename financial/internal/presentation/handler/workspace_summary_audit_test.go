package handler

import (
	"testing"
	"time"
)

func TestWorkspaceSummaryInternalTransferDoesNotChangeTotalLiquidity(t *testing.T) {
	state := map[string]any{
		"accounts": []any{
			map[string]any{"id": "bank-a", "opening": 1000.0},
			map[string]any{"id": "bank-b", "opening": 500.0},
		},
		"movements": []any{
			map[string]any{
				"id": "transfer-1", "accountId": "bank-a", "counterAccountId": "bank-b",
				"transactionType": "transfer", "direction": "out", "amount": 300.0,
			},
		},
	}
	summary := buildWorkspaceSummaryAccurate(state, 1, time.Now())
	if got := summary["cash_balance"]; got != 1500.0 {
		t.Fatalf("internal transfer changed total liquidity: got %v want 1500", got)
	}
	if got := workspaceAccountBalance(rowsFrom(state, "accounts")[0], rowsFrom(state, "movements")); got != 700.0 {
		t.Fatalf("source account balance = %v, want 700", got)
	}
	if got := workspaceAccountBalance(rowsFrom(state, "accounts")[1], rowsFrom(state, "movements")); got != 800.0 {
		t.Fatalf("destination account balance = %v, want 800", got)
	}
}

func TestWorkspaceSummaryUsesCOGSNotPurchasesForGrossMargin(t *testing.T) {
	state := map[string]any{
		"invoices": []any{
			map[string]any{"id": "sale-1", "total": 1000.0, "costAmount": 400.0},
		},
		"incomingInvoices": []any{
			map[string]any{"id": "purchase-1", "amount": 900.0, "nonFinancial": false},
		},
		"expenses": []any{
			map[string]any{"id": "expense-1", "amount": 100.0},
		},
	}
	summary := buildWorkspaceSummaryAccurate(state, 2, time.Now())
	if got := summary["total_purchases"]; got != 900.0 {
		t.Fatalf("purchase total = %v, want 900", got)
	}
	if got := summary["total_cogs"]; got != 400.0 {
		t.Fatalf("COGS = %v, want 400", got)
	}
	if got := summary["gross_margin"]; got != 600.0 {
		t.Fatalf("gross margin = %v, want 600", got)
	}
	if got := summary["operating_profit"]; got != 500.0 {
		t.Fatalf("operating profit = %v, want 500", got)
	}
}

func TestWorkspaceSummaryExcludesNonFinancialIncomingFromPurchases(t *testing.T) {
	state := map[string]any{
		"incomingInvoices": []any{
			map[string]any{"amount": 300.0, "nonFinancial": false},
			map[string]any{"amount": 700.0, "nonFinancial": true},
		},
	}
	summary := buildWorkspaceSummaryAccurate(state, 3, time.Now())
	if got := summary["total_purchases"]; got != 300.0 {
		t.Fatalf("financial purchase total = %v, want 300", got)
	}
}
