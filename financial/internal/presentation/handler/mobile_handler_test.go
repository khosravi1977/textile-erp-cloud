package handler

import "testing"

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
