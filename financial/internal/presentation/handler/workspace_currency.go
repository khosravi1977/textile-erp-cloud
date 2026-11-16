package handler

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/erpsystem/textile-erp/internal/platform/monetary"
)

var currencyCollections = []string{"invoices", "incomingInvoices", "yarnOutInvoices", "expenses", "receivableDocs", "payableDocs", "movements", "ownedInventory", "openingBalances", "mobileTransactions", "journalEntries"}

func legacyMobileMoney(key string, row map[string]any) bool {
	return key == "mobileTransactions" || stringValue(row["source_type"]) == "mobile_sms" || stringValue(row["source_type"]) == "hesabyar_mobile" || stringValue(row["sourceMobileTransaction"]) != ""
}

func workspaceNeedsRialMigration(state map[string]any) bool {
	for _, key := range currencyCollections {
		for _, row := range rowsFrom(state, key) {
			if legacyMobileMoney(key, row) && stringValue(row["currency"]) == "" {
				return true
			}
		}
	}
	for _, row := range rowsFrom(state, "accounts") {
		if row["lastReportedBalance"] != nil && stringValue(row["currency"]) == "" {
			return true
		}
	}
	return false
}

func rialInteger(value any) (int64, error) {
	if number, ok := value.(float64); ok {
		if math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number || math.Abs(number) > float64(monetary.MaxAmount) {
			return 0, fmt.Errorf("invalid exact rial amount")
		}
		return int64(number), nil
	}
	n := strings.TrimSpace(stringValue(value))
	if n == "" {
		return 0, nil
	}
	amount, err := strconv.ParseInt(n, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("مبلغ ریالی باید عدد صحیح و دقیق باشد: %s", n)
	}
	return monetary.ToRials(amount, monetary.Rial)
}

// prepareRialWorkspace is a pure, replay-safe migration. Only the proven
// Android sources (or an explicit IRT unit) are scaled; manual rial invoices
// and cheques retain their numbers. The original state is separately audited.
func prepareRialWorkspace(raw json.RawMessage) (map[string]any, int, error) {
	return prepareRialWorkspaceWithAnchors(raw, nil)
}

func prepareRialWorkspaceWithAnchors(raw json.RawMessage, anchors map[string]rialBankAnchor) (map[string]any, int, error) {
	var state map[string]any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&state); err != nil {
		return nil, 0, err
	}
	anchorNets := map[string]int64{}
	for _, account := range rowsFrom(state, "accounts") {
		if account["lastReportedBalance"] == nil || stringValue(account["currency"]) == monetary.Rial {
			continue
		}
		id := stringValue(account["id"])
		opening, err := rialInteger(account["opening"])
		if err != nil {
			return nil, 0, err
		}
		reported, err := rialInteger(account["lastReportedBalance"])
		if err != nil {
			return nil, 0, err
		}
		movements := rowsFrom(state, "movements")
		if opening+mobileAccountNet(movements, id) != reported {
			anchor, ok := anchors[id]
			if !ok {
				return nil, 0, fmt.Errorf("historical bank balance anchor required for %s", id)
			}
			movements = rowsFrom(anchor.State, "movements")
			if opening+mobileAccountNet(movements, id) != reported {
				return nil, 0, fmt.Errorf("invalid historical bank anchor for %s", id)
			}
			account["sourceBalanceAnchorRevision"] = anchor.Revision
		}
		anchorNets[id], err = rialMovementNet(movements, id)
		if err != nil {
			return nil, 0, err
		}
	}
	count := 0
	for _, key := range currencyCollections {
		for _, row := range rowsFrom(state, key) {
			fallback := monetary.Rial
			if legacyMobileMoney(key, row) {
				fallback = monetary.Toman
			}
			unit, err := monetary.Unit(stringValue(row["currency"]), fallback)
			if err != nil {
				return nil, 0, fmt.Errorf("%s: %w", key, err)
			}
			if unit == monetary.Toman {
				if row["amount"] != nil {
					row["sourceAmount"] = row["amount"]
				}
				row["sourceCurrency"] = monetary.Toman
				if err := scaleLegacyMoney(row); err != nil {
					return nil, 0, fmt.Errorf("%s: %w", key, err)
				}
				count++
			}
			row["currency"] = monetary.Rial
		}
	}
	for _, row := range rowsFrom(state, "accounts") {
		if stringValue(row["currency"]) == monetary.Rial {
			continue
		}
		unit, err := monetary.Unit(stringValue(row["currency"]), monetary.Rial)
		if err != nil {
			return nil, 0, err
		}
		if row["lastReportedBalance"] != nil || unit == monetary.Toman {
			// A reported SMS bank balance is proven IRT. Recompute opening
			// against the converted movements, including transfer counterlegs.
			original := row["lastReportedBalance"]
			if original == nil {
				original = row["opening"]
			}
			value, err := rialInteger(original)
			if err != nil {
				return nil, 0, err
			}
			balance, err := monetary.ToRials(value, monetary.Toman)
			if err != nil {
				return nil, 0, err
			}
			row["sourceOpening"] = row["opening"]
			if row["lastReportedBalance"] != nil {
				row["sourceReportedBalance"] = original
				row["lastReportedBalance"] = balance
				row["opening"] = balance - anchorNets[stringValue(row["id"])]
			} else {
				row["opening"] = balance
			}
			row["sourceCurrency"] = monetary.Toman
			count++
		}
		row["currency"] = monetary.Rial
	}
	state["currency"] = monetary.Rial
	state["currencyPolicyVersion"] = 1
	if err := monetary.ValidateDocument(state); err != nil {
		return nil, 0, err
	}
	return state, count, nil
}

func rialLedgerNamespace(key, source string) string {
	if source == "CurrencyNormalization" {
		return "CUR:" + strings.TrimPrefix(key, "WS:")
	}
	return key
}

func scaleLegacyMoney(row map[string]any) error {
	for _, key := range []string{"amount", "total", "subtotal", "taxAmount", "unitPrice", "costAmount", "costUnitPrice", "paid", "reportedBalance", "debit", "credit", "unitCost", "value", "price"} {
		if row[key] == nil || row[key] == "" {
			continue
		}
		value, err := rialInteger(row[key])
		if err != nil {
			return err
		}
		converted, err := monetary.ToRials(value, monetary.Toman)
		if err != nil {
			return err
		}
		row[key] = converted
	}
	for _, key := range []string{"payments", "lines"} {
		for _, nested := range rowsFrom(row, key) {
			if err := scaleLegacyMoney(nested); err != nil {
				return err
			}
		}
	}
	if _, exists := row["currency"]; exists {
		row["currency"] = monetary.Rial
	}
	return nil
}

func validateWorkspaceState(raw json.RawMessage) (json.RawMessage, string, error) {
	if len(raw) == 0 {
		return nil, "", errors.New("Workspace state is required")
	}
	var state map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&state); err != nil || state == nil {
		return nil, "", errors.New("Workspace state must be a JSON object")
	}
	for _, key := range []string{"invoices", "incomingInvoices", "manualCustomers", "yarnOutInvoices", "expenses", "receivableDocs", "payableDocs", "accounts", "movements", "ownedInventory", "openingBalances", "smsGroups", "smsBankSenders", "mobileTransactions", "journalEntries", "fiscalPeriods"} {
		if value, ok := state[key]; ok {
			if _, ok := value.([]any); !ok {
				return nil, "", fmt.Errorf("Workspace field %s must be an array", key)
			}
		}
	}
	if err := monetary.ValidateDocument(state); err != nil {
		return nil, "", err
	}
	canonical, err := json.Marshal(state)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(canonical)
	return canonical, hex.EncodeToString(sum[:]), nil
}
