package financecore

import "testing"

func TestNormalizeRialsPreservesSourceAndAllocations(t *testing.T) {
	original := TransactionRequest{Amount: 120, InterestAmount: 20, Currency: "IRT", Allocations: []AllocationInput{{AllocatedAmount: 90}}}
	converted, err := NormalizeRialRequest(original)
	if err != nil {
		t.Fatal(err)
	}
	if converted.Amount != 1200 || converted.InterestAmount != 200 || converted.Allocations[0].AllocatedAmount != 900 || converted.SourceAmount != 120 || converted.SourceCurrency != "IRT" || converted.Currency != "IRR" {
		t.Fatalf("%+v", converted)
	}
	if original.Allocations[0].AllocatedAmount != 90 {
		t.Fatal("mutated input")
	}
	again, err := NormalizeRialRequest(converted)
	if err != nil || again.Amount != 1200 || again.Allocations[0].AllocatedAmount != 900 || again.SourceAmount != 120 {
		t.Fatalf("double conversion %+v %v", again, err)
	}
	if _, err := NormalizeRialRequest(TransactionRequest{Amount: 10, Currency: "USD"}); err == nil {
		t.Fatal("unknown unit accepted")
	}
}
