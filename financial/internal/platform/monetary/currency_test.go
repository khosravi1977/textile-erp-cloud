package monetary

import (
	"encoding/json"
	"testing"
)

func TestCurrencyConversionAndBounds(t *testing.T) {
	for _, c := range []struct {
		amount  int64
		unit    string
		want    int64
		invalid bool
	}{
		{123, Rial, 123, false}, {123, Toman, 1230, false}, {-123, Toman, -1230, false},
		{MaxAmount, Rial, MaxAmount, false}, {MaxAmount/10 + 1, Toman, 0, true}, {1, "USD", 0, true},
	} {
		got, err := ToRials(c.amount, c.unit)
		if (err != nil) != c.invalid || !c.invalid && got != c.want {
			t.Fatalf("%+v: %d %v", c, got, err)
		}
	}
	for _, unit := range []string{"USD", "euro", "IR", "unknown"} {
		if _, err := Unit(unit, Rial); err == nil {
			t.Fatalf("accepted %q", unit)
		}
	}
}

func TestDocumentGuard(t *testing.T) {
	if err := ValidateMobileDocument(map[string]any{"currency": "IRT", "amount": 100}); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"money_unit", "amount_unit"} {
		if err := ValidateMobileDocument(map[string]any{field: "IRR", "amount": 100}); err == nil {
			t.Fatal("ignored alternate mobile unit")
		}
	}
	for _, raw := range []string{`{"amount":100,"currency":"IRR"}`, `{"unitPrice":0.5}`, `{"sourceCurrency":"IRT","amount":1000}`} {
		var value any
		json.Unmarshal([]byte(raw), &value)
		if err := ValidateDocument(value); err != nil {
			t.Fatalf("valid: %s: %v", raw, err)
		}
	}
	for _, raw := range []string{`{"currency":"IRT"}`, `{"lines":[{"money_unit":"USD"}]}`, `{"amount":"NaN"}`, `{"unit_price":"abc"}`, `{"balance":9007199254740992}`, `{"amount":true}`} {
		var value any
		json.Unmarshal([]byte(raw), &value)
		if err := ValidateDocument(value); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
