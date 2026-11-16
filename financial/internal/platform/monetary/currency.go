// Package monetary defines the single ERP money contract: amounts are rials.
package monetary

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

const Rial = "IRR"
const Toman = "IRT"

// The web client must be able to represent every persisted amount exactly.
const MaxAmount int64 = 9007199254740991

func Unit(value, fallback string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "":
		if fallback == Rial || fallback == Toman {
			return fallback, nil
		}
	case "IRR", "RIAL", "RIALS", "ریال":
		return Rial, nil
	case "IRT", "TOMAN", "TOMANS", "تومان":
		return Toman, nil
	}
	return "", fmt.Errorf("واحد پول نامعتبر است؛ فقط IRR (ریال) یا IRT (تومان) پذیرفته می‌شود")
}

func ToRials(value int64, unit string) (int64, error) {
	limit := MaxAmount
	if unit == Toman {
		limit /= 10
	} else if unit != Rial {
		return 0, fmt.Errorf("واحد پول مشخص نیست")
	}
	if value > limit || value < -limit {
		return 0, fmt.Errorf("مبلغ از محدوده امن ریالی بیشتر است")
	}
	if unit == Toman {
		return value * 10, nil
	}
	return value, nil
}

var moneyKeys = map[string]bool{}

func init() {
	for _, key := range strings.Fields("amount total totalAmount total_amount unitPrice unit_price unitCost unit_cost opening balance debit credit paid paidAmount taxAmount tax_amount subtotal costAmount costUnitPrice price creditLimit downtimeRate yarnPrice fabricPrice interest_amount allocated_amount lastReportedBalance reportedBalance reported_balance credit_limit downtime_rate") {
		moneyKeys[strings.ToLower(key)] = true
	}
}

// ValidateDocument rejects foreign/ambiguous units and invalid amounts before
// legacy handlers can silently coerce them to zero. It does not convert data.
func ValidateDocument(value any) error { return validateDocument(value, false) }

// Mobile sources may declare IRT for conversion, but must use the documented
// currency field. Ignoring a second unit field could silently multiply rials.
func ValidateMobileDocument(value any) error { return validateDocument(value, true) }

func validateDocument(value any, source bool) error {
	switch value := value.(type) {
	case map[string]any:
		for key, field := range value {
			lower := strings.ToLower(key)
			if lower == "currency" || lower == "money_unit" || lower == "amount_unit" || lower == "currency_code" || lower == "currencycode" || lower == "moneyunit" || lower == "amountunit" {
				unit, err := Unit(fmt.Sprint(field), Rial)
				if err != nil || (!source && unit != Rial) || (source && lower != "currency") {
					return fmt.Errorf("ثبت مستقیم فقط به ریال مجاز است؛ واحد %s را اصلاح کنید", key)
				}
			}
			if moneyKeys[lower] && field != nil && field != "" {
				var n float64
				switch v := field.(type) {
				case json.Number:
					n, _ = v.Float64()
				case float64:
					n = v
				case int64:
					n = float64(v)
				case int:
					n = float64(v)
				case string:
					var err error
					n, err = strconv.ParseFloat(v, 64)
					if err != nil {
						return fmt.Errorf("مبلغ %s باید عدد ریالی معتبر باشد", key)
					}
				default:
					return fmt.Errorf("مبلغ %s باید عدد باشد", key)
				}
				if math.IsNaN(n) || math.IsInf(n, 0) || math.Abs(n) > float64(MaxAmount) {
					return fmt.Errorf("مبلغ %s خارج از محدوده امن ریالی است", key)
				}
			}
			if err := validateDocument(field, source); err != nil {
				return err
			}
		}
	case []any:
		for _, row := range value {
			if err := validateDocument(row, source); err != nil {
				return err
			}
		}
	}
	return nil
}
