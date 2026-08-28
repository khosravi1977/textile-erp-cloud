package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLegacyProfitabilityRequiresExplicitRevenue(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/costs/profitability", nil)
	rec := httptest.NewRecorder()
	(&APIHandler{}).GetProfitabilityAudited(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestLegacyCreditReportDoesNotReturnFabricatedProfile(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/advisor/credit-report/1", nil)
	rec := httptest.NewRecorder()
	(&APIHandler{}).GetCreditReportAudited(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestLegacyAdviceDoesNotUseFabricatedProfile(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/advisor/advice", nil)
	rec := httptest.NewRecorder()
	(&APIHandler{}).GetAdviceAudited(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}
