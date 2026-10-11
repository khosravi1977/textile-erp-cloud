package handler

import "testing"

func TestSupervisorPartyMatchesShortAndFullOperationalLabels(t *testing.T) {
	cases := []struct {
		left, right string
		want        bool
	}{
		{"کسا", "کسا", true},
		{"کسا", "چله پیچی کسا", true},
		{"چله پیچی کسا", "کسا", true},
		{" کسا ", "چله پیچی  کسا", true},
		{"کسا", "کسایانی", false},
		{"امین", "منسوجات امین", true},
		{"", "", true},
		{"", "کسا", false},
	}
	for _, c := range cases {
		if got := supervisorPartyMatches(c.left, c.right); got != c.want {
			t.Fatalf("supervisorPartyMatches(%q, %q) = %v, want %v", c.left, c.right, got, c.want)
		}
	}
}

func TestStockEffectDetailNamesOnlyTheDivergingField(t *testing.T) {
	base := func() (map[string]any, map[string]any) {
		invoice := map[string]any{"quantity": 451.2, "itemName": "کش75/40/48", "customer": "کسا"}
		stock := map[string]any{"quantity": 451.2, "itemName": "کش75/40/48", "customer": "چله پیچی کسا"}
		return invoice, stock
	}
	invoice, stock := base()
	if detail := stockEffectDetail(invoice, stock); detail != "" {
		t.Fatalf("label-only difference flagged as %q", detail)
	}
	invoice, stock = base()
	stock["quantity"] = 460.0
	if detail := stockEffectDetail(invoice, stock); detail != "مقدار ردیف موجودی با فاکتور ورود متفاوت است" {
		t.Fatalf("quantity divergence detail = %q", detail)
	}
}

func supervisorFindingCodes(state map[string]any) []string {
	var codes []string
	for _, f := range supervisorStateFindings(state) {
		codes = append(codes, f.ID)
	}
	return codes
}

func hasCode(codes []string, prefix string) bool {
	for _, c := range codes {
		if len(c) >= len(prefix) && c[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

func TestCompleteOperationalExpenseNeedsNoPaymentVerification(t *testing.T) {
	state := map[string]any{
		"accounts": []any{map[string]any{"id": "bank", "name": "Test bank", "type": "بانک", "opening": 1000}},
		"expenses": []any{
			map[string]any{
				"id": "exp-operational-1", "date": "2026-10-01", "group": "expense", "subgroup": "test",
				"amount": 500.0, "accountId": "bank", "source_type": "operational_expense",
			},
		},
		"movements": []any{map[string]any{
			"id": "m1", "date": "2026-10-01", "direction": "out", "transactionType": "expense",
			"amount": 500.0, "accountId": "bank", "sourceExpense": "exp-operational-1",
		}},
	}
	if codes := supervisorFindingCodes(state); hasCode(codes, "payment-verification:") {
		t.Fatalf("complete operational expense still flagged: %v", codes)
	}
}

func TestIncompleteOperationalExpenseStillRequiresFinanceReview(t *testing.T) {
	state := map[string]any{
		"accounts": []any{map[string]any{"id": "bank", "name": "Test bank", "type": "بانک", "opening": 1000}},
		"expenses": []any{
			map[string]any{
				"id": "exp-operational-2", "date": "2026-10-01", "group": "expense", "subgroup": "test",
				"amount": 500.0, "accountId": "", "source_type": "operational_expense",
			},
		},
	}
	if codes := supervisorFindingCodes(state); !hasCode(codes, "payment-verification:") {
		t.Fatalf("incomplete operational expense passed without review: %v", codes)
	}
}

func TestContradictoryVerifiedStampStillFlagged(t *testing.T) {
	state := map[string]any{
		"accounts": []any{map[string]any{"id": "bank", "name": "Test bank", "type": "بانک", "opening": 1000}},
		"expenses": []any{
			map[string]any{
				"id": "exp-operational-3", "date": "2026-10-01", "group": "expense", "subgroup": "test",
				"amount": 500.0, "accountId": "bank", "source_type": "operational_expense",
				"verifiedPayment": map[string]any{"accountId": "cash", "date": "2026-10-01", "amount": 500.0},
			},
		},
	}
	if codes := supervisorFindingCodes(state); !hasCode(codes, "payment-verification:") {
		t.Fatalf("contradictory verifiedPayment stamp accepted silently: %v", codes)
	}
}

func TestStockEffectToleratesShortVersusFullCustomerLabel(t *testing.T) {
	state := map[string]any{
		"accounts": []any{map[string]any{"id": "bank", "name": "Test bank", "type": "بانک", "opening": 1000}},
		"incomingInvoices": []any{map[string]any{
			"id": "IN-1", "date": "2026-10-05", "customer": "کسا", "itemName": "کش75/40/48",
			"quantity": 451.2, "amount": 9000000, "inventoryType": "yarn", "source_type": "operational_chelle_in",
		}},
		"ownedInventory": []any{map[string]any{
			"id": "s1", "sourceIncomingInvoice": "IN-1", "customer": "چله پیچی کسا",
			"itemName": "کش75/40/48", "quantity": 451.2,
		}},
	}
	if codes := supervisorFindingCodes(state); hasCode(codes, "stock-effect:") {
		t.Fatalf("short-vs-full customer label flagged as stock divergence: %v", codes)
	}
}
