package handler

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMobileDateOrderKeyComparesJalaliAgainstISO(t *testing.T) {
	jalali := mobileDateOrderKey("1405/08/01 11:21")
	isoSame := mobileDateOrderKey("2026-10-23")
	if jalali == "" || isoSame == "" {
		t.Fatalf("canonical keys empty: %q %q", jalali, isoSame)
	}
	if jalali != isoSame {
		t.Fatalf("1405/08/01 and its Gregorian date must canonicalize equal: %q vs %q", jalali, isoSame)
	}
	if mobileDateOrderKey("1405/07/29 04:07") >= mobileDateOrderKey("2026-10-23") {
		t.Fatal("older jalali report must not sort above a newer ISO report")
	}
	if mobileDateOrderKey("2026-10-24") <= mobileDateOrderKey("1405/08/01 23:59") {
		t.Fatal("newer ISO report must not sort below a jalali one")
	}
	if got := mobileDateOrderKey("۱۴۰۵/۰۸/۰۱"); got != jalali {
		t.Fatalf("persian digits must canonicalize the same: %q vs %q", got, jalali)
	}
	if got := mobileDateOrderKey(""); got != "" {
		t.Fatalf("empty stays empty: %q", got)
	}
}

func TestTypedTransactionRequestDecodesReportedBalance(t *testing.T) {
	var payload typedTransactionRequest
	body := `{"external_transaction_id":"555","transaction_type":"OTHER_RECEIPT","direction":"IN","amount":10,"transaction_date":"2026-10-23","reported_balance":123456}`
	if err := json.NewDecoder(strings.NewReader(body)).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.ReportedBalance == nil || *payload.ReportedBalance != 123456 {
		t.Fatalf("reported_balance lost on decode: %#v", payload.ReportedBalance)
	}
	without := `{"external_transaction_id":"556","transaction_type":"OTHER_RECEIPT","direction":"IN","amount":10}`
	var absent typedTransactionRequest
	if err := json.NewDecoder(strings.NewReader(without)).Decode(&absent); err != nil {
		t.Fatal(err)
	}
	if absent.ReportedBalance != nil {
		t.Fatalf("reported_balance must stay nil when absent: %#v", absent.ReportedBalance)
	}
}
