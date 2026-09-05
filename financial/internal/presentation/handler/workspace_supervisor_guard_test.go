package handler

import (
	"strings"
	"testing"
)

func TestFinancialSupervisorAcceptsLinkedExpense(t *testing.T) {
	state := map[string]any{
		"accounts":  []any{map[string]any{"id": "bank"}},
		"expenses":  []any{map[string]any{"id": "e1", "date": "2026-09-01", "amount": 100.0, "accountId": "bank"}},
		"movements": []any{map[string]any{"id": "m1", "date": "2026-09-01", "amount": 100.0, "accountId": "bank", "direction": "out", "transactionType": "expense", "sourceExpense": "e1"}},
	}
	if err := validateWorkspaceSupervisorChanges(map[string]any{}, state); err != nil {
		t.Fatalf("valid expense was rejected: %v", err)
	}
}

func TestFinancialSupervisorRejectsExpenseWithoutCashEffect(t *testing.T) {
	state := map[string]any{
		"accounts":  []any{map[string]any{"id": "bank"}},
		"expenses":  []any{map[string]any{"id": "e1", "date": "2026-09-01", "amount": 100.0, "accountId": "bank"}},
		"movements": []any{},
	}
	err := validateWorkspaceSupervisorChanges(map[string]any{}, state)
	if err == nil || !strings.Contains(err.Error(), "دقیقاً یک گردش") {
		t.Fatalf("missing linked movement was not rejected: %v", err)
	}
}

func TestFinancialSupervisorRejectsIncomingCashMismatch(t *testing.T) {
	state := map[string]any{
		"accounts":         []any{map[string]any{"id": "bank"}},
		"incomingInvoices": []any{map[string]any{"id": "in1", "payments": []any{map[string]any{"type": "cash", "amount": 100.0, "accountId": "bank"}}}},
		"movements":        []any{map[string]any{"id": "m1", "amount": 90.0, "accountId": "bank", "sourceIncomingInvoice": "in1"}},
	}
	err := validateWorkspaceSupervisorChanges(map[string]any{}, state)
	if err == nil || !strings.Contains(err.Error(), "اعمال نشده") {
		t.Fatalf("cash mismatch was not rejected: %v", err)
	}
}

func TestFinancialSupervisorDoesNotBlockUnchangedLegacyExpense(t *testing.T) {
	legacy := map[string]any{
		"accounts":  []any{map[string]any{"id": "bank"}},
		"expenses":  []any{map[string]any{"id": "legacy", "date": "2026-01-01", "amount": 100.0, "accountId": "bank"}},
		"movements": []any{},
	}
	if err := validateWorkspaceSupervisorChanges(legacy, legacy); err != nil {
		t.Fatalf("unchanged legacy data must remain readable: %v", err)
	}
}
