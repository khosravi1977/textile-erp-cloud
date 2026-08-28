package handler

import "testing"

func TestReversalKeepsOriginalAccountingDate(t *testing.T) {
	original := ledgerEntry{
		Key:         "sale:AGENT_TEST_PERIOD",
		Date:        "2026-06-15",
		Description: "فاکتور فروش آزمایشی",
		Lines: []ledgerLine{
			{AccountCode: "1200", Debit: 1000},
			{AccountCode: "4200", Credit: 1000},
		},
	}
	reversed := reverseLedgerEntry(original)
	if reversed.Date != original.Date {
		t.Fatalf("reversal date = %s, want original date %s", reversed.Date, original.Date)
	}
	if reversed.Lines[0].Debit != 0 || reversed.Lines[0].Credit != 1000 {
		t.Fatalf("first reversal line did not swap debit/credit: %+v", reversed.Lines[0])
	}
	if reversed.Lines[1].Debit != 1000 || reversed.Lines[1].Credit != 0 {
		t.Fatalf("second reversal line did not swap debit/credit: %+v", reversed.Lines[1])
	}
}
