package handler

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func sourceXML(amount, formula string) []byte {
	values := []string{"۱۴۰۵/۰۸/۲۶ ۱۰:۴۳", "بانک صادرات", "واریز", amount, "371279801", "", "", "", "", "", "", "ارسال‌شده"}
	header := []string{"تاریخ شمسی", "بانک", "نوع", "مبلغ", "مانده بانک", "شماره پیگیری", "گروه", "زیرگروه", "توضیحات", "مشتری / طرف حساب", "بانک مقابل", "وضعیت ERP"}
	var b strings.Builder
	b.WriteString(`<Workbook xmlns="urn:schemas-microsoft-com:office:spreadsheet" xmlns:ss="urn:schemas-microsoft-com:office:spreadsheet"><Worksheet><Table>`)
	for i, row := range [][]string{header, values} {
		b.WriteString("<Row>")
		for j, cell := range row {
			attr := ""
			if i == 1 && j == 3 {
				attr = formula
			}
			fmt.Fprintf(&b, "<Cell%s><Data>%s</Data></Cell>", attr, cell)
		}
		b.WriteString("</Row>")
	}
	b.WriteString("</Table></Worksheet></Workbook>")
	return []byte(b.String())
}

func TestVerifiedRialSourceParser(t *testing.T) {
	e, err := ParseRialSourceExcel(sourceXML("1890000000", ""), "IRR", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Rows) != 1 || e.Rows[0].Amount != 1890000000 || e.Rows[0].Date != "2026-11-17" || e.Rows[0].Time != "10:43" || e.Rows[0].Bank != "صادرات" || len(e.SHA256) != 64 {
		t.Fatalf("bad evidence %+v", e)
	}
	for _, unit := range []string{"", "IRT", "USD"} {
		if _, err := ParseRialSourceExcel(sourceXML("100", ""), unit, nil); err == nil {
			t.Fatalf("accepted unconfirmed unit %q", unit)
		}
	}
	for _, amount := range []string{"0", "-1", "1.2", "9007199254740992"} {
		if _, err := ParseRialSourceExcel(sourceXML(amount, ""), "IRR", nil); err == nil {
			t.Fatalf("accepted invalid amount %q", amount)
		}
	}
	if _, err := ParseRialSourceExcel(sourceXML("100", ` ss:Formula="=1+1"`), "IRR", nil); err == nil {
		t.Fatal("accepted formula")
	}
}

func TestSourceMatchRequiresEvidenceAndAuditApproval(t *testing.T) {
	balance := int64(100)
	e := RialSourceEvidence{Rows: []RialSourceRow{{Row: 2, Date: "2026-11-17", Time: "10:43", Bank: "پاسارگاد", Direction: "in", Amount: 10, Balance: &balance}}}
	row := map[string]any{"id": "sms-x", "externalId": "x", "amount": 100, "sourceAmount": 10, "sourceCurrency": "IRT", "occurredJalali": "2026-11-17T00:00:00Z", "bank": "B.Pasargad", "direction": "in", "reportedBalance": 1000}
	match, err := matchRialSource(row, map[string]any{"transaction_time": "10:43:00"}, e, nil)
	if err != nil || match.Amount != 10 || len(match.Rows) != 1 || *match.Balance != 100 {
		t.Fatalf("match %+v %v", match, err)
	}
	row["bank"] = "Unknown bank"
	if _, err := matchRialSource(row, nil, e, nil); err == nil {
		t.Fatal("guessed unmatched amount")
	}
	e.RestoreIDs = []string{"x"}
	if _, err := matchRialSource(row, nil, e, map[string]any{"amount": 11}); err == nil {
		t.Fatal("accepted changed original")
	}
	match, err = matchRialSource(row, nil, e, map[string]any{"amount": 10})
	if err != nil || match.Basis != "owner_approved_policy_1_restore" {
		t.Fatalf("approval %+v %v", match, err)
	}
	row["amount"] = 101
	if _, err := matchRialSource(row, nil, e, map[string]any{"amount": 10}); err == nil {
		t.Fatal("guessed conversion by magnitude")
	}
}

func TestSourceCorrectionPreservesManualMoneyAndResidual(t *testing.T) {
	prior := json.RawMessage(`{"mobileTransactions":[{"id":"sms-x","amount":10}],"accounts":[{"id":"a","lastReportedBalance":100}],"payableDocs":[{"id":"p","amount":20}]}`)
	raw := json.RawMessage(`{"mobileTransactions":[{"id":"sms-x","externalId":"x","amount":100,"sourceAmount":10,"sourceCurrency":"IRT","currency":"IRR","bank":"Bank","direction":"in","occurredJalali":"2026-11-17","reportedBalance":1000},{"id":"new","externalId":"new","amount":3,"sourceAmount":3,"sourceCurrency":"IRR","currency":"IRR"}],"accounts":[{"id":"a","name":"Bank","opening":900,"lastReportedBalance":1000,"sourceReportedBalance":100,"sourceCurrency":"IRT"}],"movements":[{"id":"m","accountId":"a","amount":100,"sourceAmount":10,"sourceCurrency":"IRT","currency":"IRR","source_type":"mobile_sms","sourceId":"x","direction":"in"},{"id":"manual","accountId":"a","amount":7,"direction":"out"}],"invoices":[{"id":"i","total":1890000000,"lines":[{"unitPrice":25000}]}],"receivableDocs":[{"id":"c","amount":900000000}],"payableDocs":[{"id":"p","amount":200,"sourceAmount":20,"sourceCurrency":"IRT","source_type":"hesabyar_mobile"}]}`)
	balance := int64(100)
	e := RialSourceEvidence{Currency: "IRR", SHA256: strings.Repeat("a", 64), Rows: []RialSourceRow{{Row: 2, Date: "2026-11-17", Bank: "bank", Direction: "in", Amount: 10, Balance: &balance}}}
	var result RialSourceCorrectionResult
	state, _, err := prepareSourceCorrection(raw, prior, nil, e, &result)
	if err != nil {
		t.Fatal(err)
	}
	a := rowsFrom(state, "accounts")[0]
	if number(a["opening"]) != 90 || number(a["opening"])+float64(mobileAccountNet(rowsFrom(state, "movements"), "a")) != 93 {
		t.Fatal("manual residual erased", a)
	}
	if number(rowsFrom(state, "invoices")[0]["total"]) != 1890000000 || number(rowsFrom(state, "receivableDocs")[0]["amount"]) != 900000000 || number(rowsFrom(state, "payableDocs")[0]["amount"]) != 20 || number(rowsFrom(state, "mobileTransactions")[1]["amount"]) != 3 {
		t.Fatal("unrelated rial amounts changed")
	}
	if result.WorkspaceCorrected != 3 || result.ExcelMatched != 1 || result.BankAccounts != 1 {
		t.Fatalf("counts %+v", result)
	}
}

func TestMobileUnitContractConvertsOnlyExplicitToman(t *testing.T) {
	for _, unit := range []string{"", "IRR", "IRT", "USD"} {
		t.Run(unit, func(t *testing.T) {
			balance := int64(100)
			req := mobileTransactionRequest{Amount: 1, ReportedBalance: &balance, Currency: unit}
			err := normalizeMobileMoney(&req)
			if unit == "USD" {
				if err == nil {
					t.Fatal("accepted unknown unit")
				}
				return
			}
			want := int64(1)
			if unit == "IRT" {
				want = 10
			}
			if err != nil || req.Amount != want || *req.ReportedBalance != 100*want || req.SourceAmount != 1 || req.Currency != "IRR" {
				t.Fatalf("%+v %v", req, err)
			}
			if err := normalizeMobileMoney(&req); err != nil || req.Amount != want {
				t.Fatal("double conversion", err)
			}
		})
	}
	payload := map[string]any{"accounts": []map[string]any{{"balance": int64(1)}}}
	if err := adaptMobileBootstrap(payload, ""); err != nil || payload["currency"] != "IRR" || payload["accounts"].([]map[string]any)[0]["balance"] != int64(1) {
		t.Fatal("lost one rial", err)
	}
}

func TestMobilePayableUsesVerifiedSourceUnit(t *testing.T) {
	for _, unit := range []string{"", "IRR", "IRT", "USD"} {
		row, err := mobilePayableDocumentRow(mobilePayableDocumentRequest{ExternalID: "fixture", Customer: "Customer", Amount: 100, DueJalali: "1405/09/01", Currency: unit}, time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC))
		if unit == "USD" {
			if err == nil {
				t.Fatal("accepted unknown payable unit")
			}
			continue
		}
		want := int64(100)
		if unit == "IRT" {
			want = 1000
		}
		if err != nil || mobileInt64(row["amount"]) != want || row["currency"] != "IRR" || mobileInt64(row["sourceAmount"]) != 100 {
			t.Fatalf("unit=%q row=%+v err=%v", unit, row, err)
		}
	}
}
