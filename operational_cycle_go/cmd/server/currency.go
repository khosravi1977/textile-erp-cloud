package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
)

// The operational app accepts rials only. Source-unit conversion belongs to
// the HesabYar financial adapter, never to operational expense entry.
func withRialCurrency(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Money-Currency", "IRR")
		if r.Method == "POST" || r.Method == "PUT" || r.Method == "PATCH" {
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
				body, err := io.ReadAll(io.LimitReader(r.Body, (8<<20)+1))
				if err != nil || len(body) > 8<<20 {
					http.Error(w, "Invalid payload size", 413)
					return
				}
				r.Body = io.NopCloser(bytes.NewReader(body))
				var value any
				d := json.NewDecoder(bytes.NewReader(body))
				d.UseNumber()
				if d.Decode(&value) == nil {
					if err := validateOperationalMoney(value); err != nil {
						http.Error(w, err.Error(), 400)
						return
					}
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func validateOperationalMoney(value any) error {
	switch v := value.(type) {
	case map[string]any:
		for key, field := range v {
			lower := strings.ToLower(key)
			if lower == "currency" || lower == "money_unit" || lower == "amount_unit" || lower == "currency_code" || lower == "currencycode" || lower == "moneyunit" || lower == "amountunit" {
				unit := strings.ToUpper(strings.TrimSpace(fmt.Sprint(field)))
				if unit != "IRR" && unit != "RIAL" && unit != "ریال" {
					return fmt.Errorf("واحد پول باید ریال (IRR) باشد")
				}
			}
			if lower == "mablagh" || lower == "amount" || lower == "unit_price" || lower == "price" || lower == "cost" {
				if field != nil && field != "" {
					n, err := strconv.ParseFloat(fmt.Sprint(field), 64)
					if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || math.Abs(n) > 9007199254740991 {
						return fmt.Errorf("مبلغ ریالی نامعتبر است")
					}
				}
			}
			if err := validateOperationalMoney(field); err != nil {
				return err
			}
		}
	case []any:
		for _, field := range v {
			if err := validateOperationalMoney(field); err != nil {
				return err
			}
		}
	}
	return nil
}
