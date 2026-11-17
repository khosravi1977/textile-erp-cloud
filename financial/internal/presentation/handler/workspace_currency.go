package handler

import (
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

func rialLedgerNamespace(key, source string) string {
	if source == "CurrencyNormalization" {
		return "CUR:" + strings.TrimPrefix(key, "WS:")
	}
	return key
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
