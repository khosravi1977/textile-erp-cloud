package handler

import "testing"

func TestAuditTransferPostsFromSourceToDestination(t *testing.T) {
	state := testWorkspace(t, `{
		"accounts":[
			{"id":"source","name":"بانک مبدأ","type":"بانک","opening":0},
			{"id":"destination","name":"بانک مقصد","type":"بانک","opening":0}
		],
		"movements":[{
			"id":"transfer-1","date":"2026-08-18","transactionType":"transfer",
			"direction":"in","accountId":"source","counterAccountId":"destination","amount":1000
		}]
	}`)

	entry := mustLedgerEntry(t, state, "movement:transfer-1")
	accounts := workspaceCashAccounts(state)
	source := accounts["source"]
	destination := accounts["destination"]

	if len(entry.Lines) != 2 {
		t.Fatalf("expected two transfer lines, got %#v", entry.Lines)
	}
	if entry.Lines[0].AccountCode != destination.Code || entry.Lines[0].Debit != 1000 {
		t.Fatalf("destination must be debited on a source→destination transfer: %#v", entry.Lines)
	}
	if entry.Lines[1].AccountCode != source.Code || entry.Lines[1].Credit != 1000 {
		t.Fatalf("source must be credited on a source→destination transfer: %#v", entry.Lines)
	}
}

func TestAuditReversalKeepsOriginalAccountingDate(t *testing.T) {
	entry := ledgerEntry{
		Key:  "sale:old-period",
		Date: "2026-07-31",
		Lines: []ledgerLine{
			{AccountCode: "1200", Debit: 1000},
			{AccountCode: "4200", Credit: 1000},
		},
	}

	reversed := reverseLedgerEntry(entry)
	if reversed.Date != entry.Date {
		t.Fatalf("reversal date must remain in the original accounting period: got %s want %s", reversed.Date, entry.Date)
	}
}

func TestAuditSaleWithInventoryCostCannotOmitCOGS(t *testing.T) {
	state := testWorkspace(t, `{
		"invoices":[{
			"id":"sale-no-cost","number":"2001","date":"2026-08-18","customer":"مشتری تست",
			"item":"پارچه تست","pricingMode":"sale","quantity":10,"unitPrice":1000,"costUnitPrice":0,
			"costAmount":0,"total":10000,"payments":[{"id":"p1","type":"credit","amount":10000}]
		}]
	}`)

	if err := validateWorkspaceAccounting(state); err == nil {
		t.Fatal("owned inventory sale without COGS must be rejected")
	}
}
