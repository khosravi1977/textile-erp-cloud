package handler

import (
	"encoding/json"
	"testing"
)

func TestRialMigrationKeepsManualDocumentsAndReconcilesBank(t *testing.T) {
	raw := json.RawMessage(`{"invoices":[{"id":"i","total":1890000000,"lines":[{"unitPrice":25000}]}],"receivableDocs":[{"id":"c","amount":900000000}],"mobileTransactions":[{"amount":100,"reportedBalance":1000}],"movements":[{"accountId":"a","amount":100,"direction":"in","source_type":"mobile_sms"},{"accountId":"b","counterAccountId":"a","amount":50,"direction":"out","transactionType":"transfer","source_type":"mobile_sms"},{"accountId":"a","amount":7,"direction":"out"}],"expenses":[{"amount":100,"source_type":"mobile_sms"}],"accounts":[{"id":"a","opening":857,"lastReportedBalance":1000}]}`)
	state, count, err := prepareRialWorkspace(raw)
	if err != nil {
		t.Fatal(err)
	}
	if count != 5 {
		t.Fatalf("converted %d", count)
	}
	if mobileInt64(rowsFrom(state, "invoices")[0]["total"]) != 1890000000 || mobileInt64(rowsFrom(state, "receivableDocs")[0]["amount"]) != 900000000 {
		t.Fatal("manual rial values changed")
	}
	account := rowsFrom(state, "accounts")[0]
	if mobileInt64(account["opening"]) != 8507 || mobileInt64(account["opening"])+mobileAccountNet(rowsFrom(state, "movements"), "a") != 10000 {
		t.Fatalf("bank mismatch %+v", account)
	}
	if mobileInt64(rowsFrom(state, "mobileTransactions")[0]["sourceAmount"]) != 100 {
		t.Fatal("original source lost")
	}
	converted, _ := json.Marshal(state)
	again, n, err := prepareRialWorkspace(converted)
	if err != nil || n != 0 || mobileInt64(rowsFrom(again, "mobileTransactions")[0]["amount"]) != 1000 {
		t.Fatalf("not idempotent n=%d err=%v", n, err)
	}
	if string(raw) == string(converted) {
		t.Fatal("no change")
	}
	if _, _, err := prepareRialWorkspace(json.RawMessage(`{"expenses":[{"currency":"USD","amount":1}]}`)); err == nil {
		t.Fatal("unknown unit accepted")
	}
}

func TestMobileBoundaryAndBootstrapRoundTrip(t *testing.T) {
	balance := int64(1000)
	req := mobileTransactionRequest{Amount: 100, ReportedBalance: &balance}
	if err := normalizeMobileMoney(&req); err != nil {
		t.Fatal(err)
	}
	if req.Amount != 1000 || *req.ReportedBalance != 10000 || req.SourceAmount != 100 {
		t.Fatalf("%+v", req)
	}
	if err := normalizeMobileMoney(&req); err != nil || req.Amount != 1000 || *req.ReportedBalance != 10000 {
		t.Fatal("double conversion")
	}
	for _, currency := range []string{"", "IRR"} {
		payload := map[string]any{"accounts": []any{map[string]any{"balance": int64(10000), "opening": int64(9000)}}, "receivable_checks": []any{map[string]any{"amount": int64(1000)}}}
		if err := adaptMobileBootstrap(payload, currency); err != nil {
			t.Fatal(err)
		}
		want := int64(1000)
		if currency == "IRR" {
			want = 10000
		}
		if mobileInt64(rowsFrom(payload, "accounts")[0]["balance"]) != want {
			t.Fatal(payload)
		}
	}
	payload := map[string]any{"accounts": []any{map[string]any{"balance": int64(1)}}}
	if adaptMobileBootstrap(payload, "") == nil {
		t.Fatal("silently rounded one rial")
	}
	if !legacyTypedToman(map[string]any{"source": "IMPORT", "external_transaction_id": "HY-1"}) || legacyTypedToman(map[string]any{"source": "OPERATIONAL"}) {
		t.Fatal("unproven source conversion")
	}
}

func TestBankAnchorPreservesLaterManualRialPayment(t *testing.T) {
	anchor := decodeWorkspaceMap(json.RawMessage(`{"accounts":[{"id":"a","opening":900,"lastReportedBalance":1000}],"movements":[{"accountId":"a","amount":100,"direction":"in","source_type":"mobile_sms"}]}`))
	raw := json.RawMessage(`{"accounts":[{"id":"a","opening":900,"lastReportedBalance":1000}],"movements":[{"accountId":"a","amount":100,"direction":"in","source_type":"mobile_sms"},{"accountId":"a","amount":7,"direction":"out"}]}`)
	if _, _, err := prepareRialWorkspace(raw); err == nil {
		t.Fatal("ambiguous residual was silently erased")
	}
	state, _, err := prepareRialWorkspaceWithAnchors(raw, map[string]rialBankAnchor{"a": {State: anchor, Revision: 5}})
	if err != nil {
		t.Fatal(err)
	}
	account := rowsFrom(state, "accounts")[0]
	if mobileInt64(account["opening"])+mobileAccountNet(rowsFrom(state, "movements"), "a") != 9993 {
		t.Fatal("later seven-rial payment was lost or multiplied")
	}
	if account["sourceBalanceAnchorRevision"] != int64(5) {
		t.Fatal("anchor audit reference missing")
	}
	payload := map[string]any{"accounts": []map[string]any{{"balance": int64(10000), "opening": int64(9003)}}}
	if err := adaptMobileBootstrap(payload, ""); err != nil {
		t.Fatal(err)
	}
	row := payload["accounts"].([]map[string]any)[0]
	if row["balance"] != int64(1000) || row["opening"] != nil {
		t.Fatal("legacy bootstrap unit mismatch")
	}
}
