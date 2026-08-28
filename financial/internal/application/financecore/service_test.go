package financecore

import (
	"strings"
	"testing"
	"time"
)

func TestValidateEnforcesTypeRules(t *testing.T) {
	base := func() TransactionRequest {
		return TransactionRequest{
			ExternalTransactionID: "HY-1",
			Direction:             "IN",
			Amount:                1000,
			TransactionType:       TypeCustomerReceipt,
			PartyID:               5,
		}
	}
	if err := Validate(base()); err != nil {
		t.Fatalf("valid customer receipt rejected: %v", err)
	}

	// Party required types
	for _, typ := range []string{
		TypeCustomerReceipt, TypeSupplierPayment, TypePayrollPayment,
		TypePettyCashFunding, TypePettyCashReturn, TypeCheckReceipt, TypeCheckPayment,
	} {
		req := base()
		req.TransactionType = typ
		req.PartyID = 0
		switch typ {
		case TypeSupplierPayment, TypePayrollPayment, TypePettyCashFunding, TypeCheckPayment:
			req.Direction = "OUT"
		}
		if err := Validate(req); err == nil || !strings.Contains(err.Error(), "طرف حساب") {
			t.Fatalf("%s must require a party, got %v", typ, err)
		}
	}

	// Direction consistency
	req := base()
	req.Direction = "OUT"
	if err := Validate(req); err == nil || !strings.Contains(err.Error(), "فقط جهت") {
		t.Fatalf("customer receipt must reject OUT direction, got %v", err)
	}

	// Internal transfer needs a distinct counter account
	req = base()
	req.TransactionType = TypeInternalTransfer
	req.Direction = "OUT"
	req.PartyID = 0
	req.CounterAccountID = 0
	if err := Validate(req); err == nil || !strings.Contains(err.Error(), "حساب مقصد") {
		t.Fatalf("transfer without counter account must fail, got %v", err)
	}
	req.CounterAccountID = req.BankAccountID
	if err := Validate(req); err == nil {
		t.Fatal("transfer to the same account must fail")
	}

	// Amount and id guards
	req = base()
	req.Amount = 0
	if err := Validate(req); err == nil {
		t.Fatal("zero amount must fail")
	}
	req = base()
	req.ExternalTransactionID = "  "
	if err := Validate(req); err == nil {
		t.Fatal("missing external id must fail")
	}

	// Loan repayment split
	req = base()
	req.TransactionType = TypeLoanRepayment
	req.Direction = "OUT"
	req.PartyID = 0
	req.InterestAmount = req.Amount + 1
	if err := Validate(req); err == nil {
		t.Fatal("interest above amount must fail")
	}
}

func TestValidateAllocationRules(t *testing.T) {
	req := TransactionRequest{
		ExternalTransactionID: "HY-2",
		Direction:             "IN",
		Amount:                100,
		TransactionType:       TypeCustomerReceipt,
		PartyID:               9,
		Allocations: []AllocationInput{
			{DocumentType: "INVOICE", DocumentID: "125", AllocatedAmount: 60},
			{DocumentType: "INVOICE", DocumentID: "131", AllocatedAmount: 50},
		},
	}
	if err := Validate(req); err == nil || !strings.Contains(err.Error(), "بیشتر از مبلغ") {
		t.Fatalf("over-allocation must fail, got %v", err)
	}
	req.Allocations[1].AllocatedAmount = 40
	if err := Validate(req); err != nil {
		t.Fatalf("exact allocation must pass: %v", err)
	}
	req.TransactionType = TypeOtherReceipt
	if err := Validate(req); err == nil || !strings.Contains(err.Error(), "وصول مشتری") {
		t.Fatalf("allocation on non-receipt must fail, got %v", err)
	}
}

func TestPostingPlansAreBalanced(t *testing.T) {
	cases := []TransactionRequest{
		{TransactionType: TypeCustomerReceipt, Direction: "IN", Amount: 40_000_000, PartyID: 1},
		{TransactionType: TypeSupplierPayment, Direction: "OUT", Amount: 10, PartyID: 2},
		{TransactionType: TypeDirectExpense, Direction: "OUT", Amount: 10},
		{TransactionType: TypePayrollPayment, Direction: "OUT", Amount: 10, PartyID: 3},
		{TransactionType: TypeInternalTransfer, Direction: "OUT", Amount: 10, CounterAccountID: 7},
		{TransactionType: TypeInternalTransfer, Direction: "IN", Amount: 10, CounterAccountID: 7},
		{TransactionType: TypePettyCashFunding, Direction: "OUT", Amount: 50_000_000, PartyID: 4},
		{TransactionType: TypePettyCashReturn, Direction: "IN", Amount: 5, PartyID: 4},
		{TransactionType: TypeLoanReceipt, Direction: "IN", Amount: 10, PartyID: 5},
		{TransactionType: TypeLoanRepayment, Direction: "OUT", Amount: 100, InterestAmount: 20, PartyID: 5},
		{TransactionType: TypeOwnerDeposit, Direction: "IN", Amount: 10, PartyID: 6},
		{TransactionType: TypeOwnerWithdrawal, Direction: "OUT", Amount: 10, PartyID: 6},
		{TransactionType: TypeAssetPurchase, Direction: "OUT", Amount: 10},
		{TransactionType: TypeBankFee, Direction: "OUT", Amount: 10},
		{TransactionType: TypeCheckReceipt, Direction: "IN", Amount: 10, PartyID: 1},
		{TransactionType: TypeCheckPayment, Direction: "OUT", Amount: 10, PartyID: 2},
		{TransactionType: TypeRefund, Direction: "IN", Amount: 10},
		{TransactionType: TypeOtherReceipt, Direction: "IN", Amount: 10},
		{TransactionType: TypeOtherPayment, Direction: "OUT", Amount: 10},
	}
	for _, req := range cases {
		legs, err := postingPlan(req, typeRules[req.TransactionType])
		if err != nil {
			t.Fatalf("%s plan error: %v", req.TransactionType, err)
		}
		var debit, credit int64
		for _, leg := range legs {
			debit += leg.Debit
			credit += leg.Credit
		}
		if debit != credit || debit != req.Amount {
			t.Fatalf("%s plan not balanced: debit=%d credit=%d amount=%d", req.TransactionType, debit, credit, req.Amount)
		}
	}

	// Petty cash funding must never hit an expense account.
	req := cases[6]
	legs, _ := postingPlan(req, typeRules[req.TransactionType])
	if legs[0].Account.Code == canonicalAccounts["operatingExpense"].Code {
		t.Fatal("petty cash funding must not be an expense")
	}

	// Loan repayment splits principal and interest.
	req = cases[9]
	legs, _ = postingPlan(req, typeRules[req.TransactionType])
	if len(legs) != 3 {
		t.Fatalf("loan repayment with interest needs 3 legs, got %d", len(legs))
	}
	if legs[0].Debit != 80 || legs[1].Debit != 20 {
		t.Fatalf("loan split wrong: %#v", legs)
	}

	// Internal transfer must never touch P&L accounts.
	req = cases[4]
	legs, _ = postingPlan(req, typeRules[req.TransactionType])
	for _, leg := range legs {
		if leg.Account.Type == "Income" || leg.Account.Type == "Expense" {
			t.Fatalf("internal transfer touched P&L account %s", leg.Account.Code)
		}
	}
}

func TestPartyBalanceEffects(t *testing.T) {
	cases := map[string]struct {
		req    TransactionRequest
		effect int64
	}{
		"customer receipt reduces receivable": {
			TransactionRequest{TransactionType: TypeCustomerReceipt, Amount: 40, Direction: "IN"}, -40,
		},
		"petty cash funding increases holder balance": {
			TransactionRequest{TransactionType: TypePettyCashFunding, Amount: 50, Direction: "OUT"}, 50,
		},
		"petty cash return decreases holder balance": {
			TransactionRequest{TransactionType: TypePettyCashReturn, Amount: 10, Direction: "IN"}, -10,
		},
		"loan repayment reduces liability": {
			TransactionRequest{TransactionType: TypeLoanRepayment, Amount: 100, InterestAmount: 20, Direction: "OUT"}, 80,
		},
		"expense has no party effect": {
			TransactionRequest{TransactionType: TypeDirectExpense, Amount: 10, Direction: "OUT"}, 0,
		},
	}
	for name, test := range cases {
		if got := partyBalanceEffect(test.req); got != test.effect {
			t.Fatalf("%s: got %d want %d", name, got, test.effect)
		}
	}
}

func TestAccountingDateParsesISOAndJalali(t *testing.T) {
	iso, err := AccountingDate("2026-08-25")
	if err != nil || iso.Format("2006-01-02") != "2026-08-25" {
		t.Fatalf("ISO parse failed: %v %v", iso, err)
	}
	jalali, err := AccountingDate("1405/06/03")
	if err != nil || jalali.Format("2006-01-02") != "2026-08-25" {
		t.Fatalf("Jalali parse failed: %v %v", jalali, err)
	}
	persianDigits, err := AccountingDate("۱۴۰۵/۰۶/۰۳")
	if err != nil || persianDigits.Format("2006-01-02") != "2026-08-25" {
		t.Fatalf("Persian digit parse failed: %v %v", persianDigits, err)
	}
	if _, err := AccountingDate("invalid"); err == nil {
		t.Fatal("invalid date must fail")
	}
	if _, err := time.Parse("2006-01-02", ""); err == nil {
		t.Fatal("sanity")
	}
}

func TestAccountingDateHandlesTrailingClockTime(t *testing.T) {
	// The HesabYar Android app sends occurred_jalali with a clock suffix.
	stamped, err := AccountingDate("۱۴۰۵/۰۶/۰۴ ۱۵:۴۹")
	if err != nil || stamped.Format("2006-01-02") != "2026-08-26" {
		t.Fatalf("stamped Jalali parse failed: %v %v", stamped, err)
	}
	asciiStamped, err := AccountingDate("1405/06/04 15:49")
	if err != nil || asciiStamped.Format("2006-01-02") != "2026-08-26" {
		t.Fatalf("ASCII stamped parse failed: %v %v", asciiStamped, err)
	}
	isoStamped, err := AccountingDate("2026-08-26 15:49")
	if err != nil || isoStamped.Format("2006-01-02") != "2026-08-26" {
		t.Fatalf("ISO stamped parse failed: %v %v", isoStamped, err)
	}
	if clock := AccountingClock("۱۴۰۵/۰۶/۰۴ ۱۵:۴۹"); clock != "15:49" {
		t.Fatalf("clock extraction failed: %q", clock)
	}
	if clock := AccountingClock("1405/06/04"); clock != "" {
		t.Fatalf("clock extraction should be empty: %q", clock)
	}
	if tolerant, err := AccountingDate("1405/06/04 garbage"); err != nil || tolerant.Format("2006-01-02") != "2026-08-26" {
		t.Fatalf("unknown tail should be ignored, got %v %v", tolerant, err)
	}
	if _, err := AccountingDate("1405/13/01"); err == nil {
		t.Fatal("invalid Jalali month must fail")
	}
}

func TestAccountingDateParsesRfc3339(t *testing.T) {
	stamped, err := AccountingDate("2026-08-26T00:00:00Z")
	if err != nil || stamped.Format("2006-01-02") != "2026-08-26" {
		t.Fatalf("RFC3339 parse failed: %v %v", stamped, err)
	}
}
