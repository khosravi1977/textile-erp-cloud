package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOperationalRialGuard(t *testing.T) {
	for _, body := range []string{`{"mablagh":100,"currency":"IRT"}`, `{"mablagh":"NaN"}`, `{"mablagh":"bad"}`, `{"mablagh":9007199254740992}`, `{"money_unit":"USD"}`} {
		w := httptest.NewRecorder()
		withRialCurrency(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("invalid expense reached write handler") })).ServeHTTP(w, httptest.NewRequest("POST", "/api/expenses", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatalf("%s: %d", body, w.Code)
		}
	}
	w := httptest.NewRecorder()
	withRialCurrency(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(201) })).ServeHTTP(w, httptest.NewRequest("POST", "/api/expenses", strings.NewReader(`{"mablagh":100,"currency":"IRR"}`)))
	if w.Code != 201 || w.Header().Get("X-Money-Currency") != "IRR" {
		t.Fatal("valid rial expense rejected")
	}
}
