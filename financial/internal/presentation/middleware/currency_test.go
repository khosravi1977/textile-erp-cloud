package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRialGuardCannotBeBypassedByContentType(t *testing.T) {
	for _, contentType := range []string{"", "application/json", "text/plain"} {
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("invalid payload reached handler") })
		r := httptest.NewRequest("PUT", "/api/workspace", strings.NewReader(`{"state":{"currency":"IRT"}}`))
		r.Header.Set("Content-Type", contentType)
		w := httptest.NewRecorder()
		RialCurrency(next).ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatalf("%q: %d", contentType, w.Code)
		}
	}
	body := `{"amount":100,"currency":"IRR"}`
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if string(b) != body {
			t.Fatal("body consumed")
		}
		w.WriteHeader(201)
	})
	w := httptest.NewRecorder()
	RialCurrency(next).ServeHTTP(w, httptest.NewRequest("POST", "/api/transactions", strings.NewReader(body)))
	if w.Code != 201 || w.Header().Get("X-Money-Currency") != "IRR" {
		t.Fatal(w)
	}
}
