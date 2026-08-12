package handler

import (
	"testing"
	"time"
)

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

func TestMobilePayableDocumentRowNormalizesSayadAndFields(t *testing.T) {
	now := time.Date(2026, 8, 12, 10, 30, 0, 0, time.UTC)
	row, err := mobilePayableDocumentRow(mobilePayableDocumentRequest{
		ExternalID: "check-42", Customer: "Supplier", Amount: 120000,
		CheckNo: "10", Sayad: "۱۲۳۴ ۵۶۷۸ ۹۰۱۲ ۳۴۵۶", DueJalali: "1405/06/07", Bank: "Saderat",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if stringValue(row["sayadId"]) != "1234567890123456" || stringValue(row["checkNo"]) != "10" {
		t.Fatalf("mobile payable identifiers were not normalized: %#v", row)
	}
	if stringValue(row["source_type"]) != "hesabyar_mobile" || stringValue(row["externalId"]) != "check-42" {
		t.Fatalf("mobile source identity was not preserved: %#v", row)
	}
}

func TestMobilePayableDocumentRejectsInvalidSayad(t *testing.T) {
	_, err := mobilePayableDocumentRow(mobilePayableDocumentRequest{
		ExternalID: "check-42", Customer: "Supplier", Amount: 120000,
		CheckNo: "10", Sayad: "123", DueJalali: "1405/06/07", Bank: "Saderat",
	}, time.Now())
	if err == nil {
		t.Fatal("short Sayad identifier was accepted")
	}
}

func TestMobilePayableDuplicateUsesExternalID(t *testing.T) {
	rows := []map[string]any{{"externalId": "check-42"}, {"source_type": "hesabyar_mobile", "sourceId": "check-43"}}
	if !hasMobilePayableDocument(rows, "check-42") || !hasMobilePayableDocument(rows, "check-43") {
		t.Fatal("mobile payable duplicate was not detected")
	}
	if hasMobilePayableDocument(rows, "check-44") {
		t.Fatal("unrelated mobile payable document was treated as duplicate")
	}
}
