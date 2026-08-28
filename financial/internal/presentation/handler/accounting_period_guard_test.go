package handler

import "testing"

func TestClosedFiscalPeriodCannotBeReopened(t *testing.T) {
	if err := validateFiscalPeriodTransition("Closed", "Open"); err == nil {
		t.Fatal("closed fiscal period was allowed to reopen")
	}
}

func TestOpenFiscalPeriodCanBeClosed(t *testing.T) {
	if err := validateFiscalPeriodTransition("Open", "Closed"); err != nil {
		t.Fatalf("open period could not be closed: %v", err)
	}
}

func TestClosedFiscalPeriodCanRemainClosed(t *testing.T) {
	if err := validateFiscalPeriodTransition("Closed", "Closed"); err != nil {
		t.Fatalf("idempotent closed status failed: %v", err)
	}
}
