package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/erpsystem/textile-erp/internal/platform/requestctx"
)

func testWorkspace(t *testing.T, raw string) map[string]any {
	t.Helper()
	var state map[string]any
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestValidateAndDeriveBalancedSale(t *testing.T) {
	state := testWorkspace(t, `{
		"accounts":[{"id":"bank-main","name":"بانک اصلی","type":"بانک","opening":0}],
		"invoices":[{
			"id":"sale-1","number":"1001","date":"2026-07-22","customer":"مشتری الف","item":"پارچه",
			"total":100000,"payments":[
				{"id":"p1","type":"cash","accountId":"bank-main","amount":40000},
				{"id":"p2","type":"credit","amount":60000}
			]
		}]
	}`)
	if err := validateWorkspaceAccounting(state); err != nil {
		t.Fatalf("valid sale rejected: %v", err)
	}
	entries, err := deriveWorkspaceLedger(state)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected invoice and receipt vouchers, got %d", len(entries))
	}
	for key, entry := range entries {
		debit, credit := 0.0, 0.0
		for _, line := range entry.Lines {
			debit += line.Debit
			credit += line.Credit
		}
		if !amountsEqual(debit, credit) {
			t.Fatalf("entry %s is not balanced: %.2f != %.2f", key, debit, credit)
		}
	}
}

func TestRejectsIncompleteSettlement(t *testing.T) {
	state := testWorkspace(t, `{
		"accounts":[{"id":"cash","name":"صندوق","type":"صندوق"}],
		"invoices":[{"id":"s1","number":"1","date":"2026-07-22","customer":"الف","item":"پارچه","total":100,"payments":[{"id":"p1","type":"cash","accountId":"cash","amount":90}]}]
	}`)
	if err := validateWorkspaceAccounting(state); err == nil {
		t.Fatal("incomplete settlement must be rejected")
	}
}

func TestPurchaseAssignedCheckHasCorrectAccountingSign(t *testing.T) {
	state := testWorkspace(t, `{
		"incomingInvoices":[{"id":"buy-1","date":"2026-07-22","customer":"فروشنده","itemName":"نخ","inventoryType":"yarn","amount":500,"payments":[{"id":"p1","type":"assign_receivable","docId":"c1","amount":500}]}]
	}`)
	if err := validateWorkspaceAccounting(state); err != nil {
		t.Fatal(err)
	}
	entries, err := deriveWorkspaceLedger(state)
	if err != nil {
		t.Fatal(err)
	}
	payment := entries["purchase-payment:buy-1:p1"]
	if len(payment.Lines) != 2 || payment.Lines[0].AccountCode != canonicalGL["payable"].Code || payment.Lines[0].Debit != 500 || payment.Lines[1].AccountCode != canonicalGL["checkReceivable"].Code || payment.Lines[1].Credit != 500 {
		t.Fatalf("assigned check accounting is wrong: %#v", payment.Lines)
	}
}

func TestReversalSwapsDebitAndCredit(t *testing.T) {
	entry := ledgerEntry{Key: "x", Lines: []ledgerLine{{Debit: 125}, {Credit: 125}}}
	reversed := reverseLedgerEntry(entry)
	if reversed.Lines[0].Credit != 125 || reversed.Lines[1].Debit != 125 {
		t.Fatalf("unexpected reversal: %#v", reversed.Lines)
	}
}

func TestPurchaseVATIsSeparatedFromInventory(t *testing.T) {
	state := testWorkspace(t, `{
		"incomingInvoices":[{"id":"buy-vat","date":"2026-07-22","customer":"فروشنده","itemName":"نخ","inventoryType":"yarn","subtotal":100,"taxable":true,"taxAmount":10,"amount":110,"payments":[{"id":"p1","type":"credit","amount":110}]}]
	}`)
	if err := validateWorkspaceAccounting(state); err != nil {
		t.Fatal(err)
	}
	entry := mustLedgerEntry(t, state, "purchase:buy-vat")
	if len(entry.Lines) != 3 || entry.Lines[0].Debit != 100 || entry.Lines[1].AccountCode != canonicalGL["inputVAT"].Code || entry.Lines[1].Debit != 10 || entry.Lines[2].Credit != 110 {
		t.Fatalf("purchase VAT accounting is wrong: %#v", entry.Lines)
	}
}

func TestOwnedYarnSaleRecognizesCost(t *testing.T) {
	state := testWorkspace(t, `{
		"yarnOutInvoices":[{"id":"y1","date":"2026-07-22","customer":"مشتری","itemName":"نخ","quantity":2,"unitPrice":100,"amount":200,"outMode":"sale","stockType":"owned","costUnitPrice":60,"costAmount":120}]
	}`)
	if err := validateWorkspaceAccounting(state); err != nil {
		t.Fatal(err)
	}
	entry := mustLedgerEntry(t, state, "yarn-out-cogs:y1")
	if len(entry.Lines) != 2 || entry.Lines[0].AccountCode != canonicalGL["cogs"].Code || entry.Lines[0].Debit != 120 || entry.Lines[1].AccountCode != canonicalGL["yarnInventory"].Code || entry.Lines[1].Credit != 120 {
		t.Fatalf("yarn COGS accounting is wrong: %#v", entry.Lines)
	}
}

func TestAccountingPeriodMutationRequiresAccountingPermission(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/accounting/periods", bytes.NewBufferString(`{}`))
	req = req.WithContext(requestctx.WithAccess(req.Context(), []string{"reports"}, true))
	response := httptest.NewRecorder()
	(&APIHandler{}).AccountingPeriods(response, req)
	if response.Code != http.StatusForbidden {
		t.Fatalf("reports-only access changed a fiscal period: %d %s", response.Code, response.Body.String())
	}
}

func mustLedgerEntry(t *testing.T, state map[string]any, key string) ledgerEntry {
	t.Helper()
	entries, err := deriveWorkspaceLedger(state)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := entries[key]
	if !ok {
		t.Fatalf("ledger entry %s was not derived", key)
	}
	return entry
}
