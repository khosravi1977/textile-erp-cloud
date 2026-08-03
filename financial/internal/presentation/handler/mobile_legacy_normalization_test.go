package handler

import (
	"encoding/json"
	"testing"
)

func TestNormalizeLegacyMobileExpenseStateRepairsStaleCustomer(t *testing.T) {
	raw := json.RawMessage(`{
		"mobileTransactions":[{"externalId":"42","direction":"out","transactionType":"supplier_payment","group":"پاکستانی","subgroup":"سوپری","customer":"حاج حسن","amount":500,"accountId":"cash","occurredAt":"2026-08-03","occurredJalali":"1405/05/12","title":"خرید روزانه"}],
		"movements":[{"sourceMobileTransaction":"42","sourceId":"42","transactionType":"supplier_payment","payer":"حاج حسن","amount":500,"accountId":"cash","date":"2026-08-03","description":"خرید روزانه"}],
		"expenses":[]
	}`)
	normalized, checksum, changed, err := normalizeLegacyMobileExpenseState(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || checksum == "" {
		t.Fatalf("expected a normalized state and checksum, changed=%v checksum=%q", changed, checksum)
	}
	state := decodeWorkspaceMap(normalized)
	transaction := rowsFrom(state, "mobileTransactions")[0]
	if got := stringValue(transaction["transactionType"]); got != "expense" {
		t.Fatalf("transaction type=%q", got)
	}
	if got := stringValue(transaction["customer"]); got != "" {
		t.Fatalf("stale customer was not cleared: %q", got)
	}
	if got := stringValue(transaction["reportedCustomer"]); got != "حاج حسن" {
		t.Fatalf("audit customer=%q", got)
	}
	movement := rowsFrom(state, "movements")[0]
	if stringValue(movement["transactionType"]) != "expense" || stringValue(movement["payer"]) != "" || stringValue(movement["sourceExpense"]) != "exp-sms-42" {
		t.Fatalf("movement was not repaired: %#v", movement)
	}
	expenses := rowsFrom(state, "expenses")
	if len(expenses) != 1 || stringValue(expenses[0]["group"]) != "پاکستانی" || stringValue(expenses[0]["subgroup"]) != "سوپری" {
		t.Fatalf("expense was not rebuilt: %#v", expenses)
	}
	if _, _, changedAgain, err := normalizeLegacyMobileExpenseState(normalized); err != nil || changedAgain {
		t.Fatalf("normalization must be idempotent, changed=%v err=%v", changedAgain, err)
	}
}

func TestNormalizeLegacyMobileExpenseStatePreservesExplicitSupplierPayment(t *testing.T) {
	raw := json.RawMessage(`{"mobileTransactions":[{"externalId":"7","direction":"out","transactionType":"supplier_payment","transactionTypeExplicit":true,"group":"خرید","customer":"فروشنده"}],"movements":[],"expenses":[]}`)
	_, _, changed, err := normalizeLegacyMobileExpenseState(raw)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("an explicit supplier payment must not be normalized as an expense")
	}
}
