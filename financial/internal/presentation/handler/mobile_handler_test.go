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

func TestMobileAccountingDateHandlesClockSuffix(t *testing.T) {
	if got := mobileAccountingDate("۱۴۰۵/۰۶/۰۴ ۱۵:۴۹"); got != "2026-08-26" {
		t.Fatalf("stamped Jalali should convert to occurrence date, got %s", got)
	}
	if got := mobileAccountingDate("1405/06/04 15:49"); got != "2026-08-26" {
		t.Fatalf("ASCII stamped Jalali should convert to occurrence date, got %s", got)
	}
	if got := mobileAccountingDate("2026-08-26 15:49"); got != "2026-08-26" {
		t.Fatalf("stamped ISO should convert to occurrence date, got %s", got)
	}
}

func TestLegacyStateMappingForTypedTypes(t *testing.T) {
	cases := map[string]string{
		"CUSTOMER_RECEIPT":   "customer_receipt",
		"SUPPLIER_PAYMENT":   "supplier_payment",
		"INTERNAL_TRANSFER":  "transfer",
		"PETTY_CASH_FUNDING": "expense",
		"PAYROLL_PAYMENT":    "expense",
		"OWNER_DEPOSIT":      "other_income",
		"CHECK_RECEIPT":      "other_income",
	}
	for typed, legacy := range cases {
		if got := legacyStateTypeForTyped(typed); got != legacy {
			t.Fatalf("legacyStateTypeForTyped(%s)=%s want %s", typed, got, legacy)
		}
	}
	expenseLike := map[string]bool{
		"DIRECT_EXPENSE": true, "PAYROLL_PAYMENT": true, "BANK_FEE": true,
		"ASSET_PURCHASE": false, "PETTY_CASH_FUNDING": false, "OWNER_WITHDRAWAL": false,
		"LOAN_REPAYMENT": false, "CHECK_PAYMENT": false,
	}
	for typed, want := range expenseLike {
		if got := typedExpenseLike(typed); got != want {
			t.Fatalf("typedExpenseLike(%s)=%v want %v", typed, got, want)
		}
	}
}

func TestNormalizeMobileCategoryKeepsPeopleOutOfSubgroup(t *testing.T) {
	group, subgroup := normalizeMobileCategory("حقوق", "مهدی خسروی", "expense", &typedStateMeta{TypedType: "PAYROLL_PAYMENT", CandidateName: "مهدی خسروی", ExpenseLike: true})
	if group != "هزینه" || subgroup != "حقوق پرسنل" {
		t.Fatalf("payroll category=%q subgroup=%q", group, subgroup)
	}
}

func TestNormalizeMobileCategoryAllowsBankFeeWithoutParty(t *testing.T) {
	group, subgroup := normalizeMobileCategory("کارمزد", "", "expense", &typedStateMeta{TypedType: "BANK_FEE", ExpenseLike: true})
	if group != "هزینه" || subgroup != "کارمزد بانکی" {
		t.Fatalf("bank fee category=%q subgroup=%q", group, subgroup)
	}
}
