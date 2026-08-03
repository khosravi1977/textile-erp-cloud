package handler

import "testing"

func TestSetUnconfirmedCounterpartyKeepsOnlyCandidate(t *testing.T) {
	row := map[string]any{"payer": "old", "customer": "old", "counterpartyConfirmed": true}
	setUnconfirmedCounterparty(row, "  حاج حسن خسروی  ")
	if stringValue(row["payer"]) != "" || stringValue(row["customer"]) != "" {
		t.Fatalf("unconfirmed name must not be assigned as payer/customer: %#v", row)
	}
	if got := stringValue(row["counterpartyCandidate"]); got != "حاج حسن خسروی" {
		t.Fatalf("candidate=%q", got)
	}
	if boolValue(row["counterpartyConfirmed"]) {
		t.Fatal("mobile candidate must require explicit confirmation")
	}
}

func TestClassifyMobileTransactionPrefersExplicitType(t *testing.T) {
	typ, err := classifyMobileTransaction("expense", "out", "پاکستانی", "حاج حسن", "")
	if err != nil {
		t.Fatal(err)
	}
	if typ != "expense" {
		t.Fatalf("explicit expense was misclassified as %q", typ)
	}
}

func TestClassifyMobileTransactionExpenseGroupBeatsStaleCustomer(t *testing.T) {
	typ, err := classifyMobileTransaction("", "out", "پاکستانی", "حاج حسن", "")
	if err != nil {
		t.Fatal(err)
	}
	if typ != "expense" {
		t.Fatalf("expense with stale customer was misclassified as %q", typ)
	}
}

func TestClassifyMobileTransactionKeepsLegacyCustomerPayment(t *testing.T) {
	typ, err := classifyMobileTransaction("", "out", "", "فروشنده", "")
	if err != nil {
		t.Fatal(err)
	}
	if typ != "supplier_payment" {
		t.Fatalf("legacy supplier payment was misclassified as %q", typ)
	}
}

func TestClassifyMobileTransactionRejectsInvalidExplicitType(t *testing.T) {
	if _, err := classifyMobileTransaction("customer", "in", "", "مشتری", ""); err == nil {
		t.Fatal("invalid explicit transaction type was accepted")
	}
}
