package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/erpsystem/textile-erp/internal/platform/monetary"
)

// RialCurrency guards all ERP JSON entry paths. Mobile endpoints perform their
// explicit source conversion instead, including the legacy IRT contract.
func RialCurrency(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Money-Currency", monetary.Rial)
		if (r.Method == "POST" || r.Method == "PUT" || r.Method == "PATCH") && strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
			body, err := io.ReadAll(io.LimitReader(r.Body, (8<<20)+1))
			if err != nil || len(body) > 8<<20 {
				http.Error(w, "Invalid financial payload size", http.StatusRequestEntityTooLarge)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			var value any
			decoder := json.NewDecoder(bytes.NewReader(body))
			decoder.UseNumber()
			if err := decoder.Decode(&value); err == nil {
				validate := monetary.ValidateDocument
				if strings.HasPrefix(r.URL.Path, "/api/mobile/") {
					validate = monetary.ValidateMobileDocument
				}
				if err := validate(value); err != nil {
					w.Header().Set("Content-Type", "application/json; charset=utf-8")
					w.WriteHeader(http.StatusBadRequest)
					_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error(), "currency": monetary.Rial})
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}
