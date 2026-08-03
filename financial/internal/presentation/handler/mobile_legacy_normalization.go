package handler

import (
	"encoding/json"
	"strings"
)

// normalizeLegacyMobileExpenseState repairs the old mobile classification
// where a customer left in the form could turn an expense group into a
// supplier payment. The transformation is returned to the current tenant only;
// it is persisted through the normal revisioned workspace save path.
func normalizeLegacyMobileExpenseState(raw json.RawMessage) (json.RawMessage, string, bool, error) {
	state := decodeWorkspaceMap(raw)
	transactions := rowsFrom(state, "mobileTransactions")
	movements := rowsFrom(state, "movements")
	expenses := rowsFrom(state, "expenses")
	expenseSources := make(map[string]bool, len(expenses))
	for _, expense := range expenses {
		expenseSources[strings.TrimSpace(stringValue(expense["sourceId"]))] = true
	}

	changed := false
	for _, transaction := range transactions {
		if boolValue(transaction["transactionTypeExplicit"]) || !strings.EqualFold(strings.TrimSpace(stringValue(transaction["direction"])), "out") {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(stringValue(transaction["transactionType"])), "supplier_payment") {
			continue
		}
		group := strings.TrimSpace(stringValue(transaction["group"]))
		subgroup := strings.TrimSpace(stringValue(transaction["subgroup"]))
		if group == "" || group == "انتقال" || strings.TrimSpace(stringValue(transaction["counterAccount"])) != "" {
			continue
		}
		externalID := strings.TrimSpace(stringValue(transaction["externalId"]))
		if externalID == "" {
			continue
		}

		reportedCustomer := strings.TrimSpace(stringValue(transaction["reportedCustomer"]))
		if reportedCustomer == "" {
			reportedCustomer = strings.TrimSpace(stringValue(transaction["customer"]))
		}
		counterparty := group
		if subgroup != "" {
			counterparty += " / " + subgroup
		}
		transaction["transactionType"] = "expense"
		transaction["transactionTypeExplicit"] = false
		transaction["reportedCustomer"] = reportedCustomer
		transaction["customer"] = ""
		transaction["counterparty"] = counterparty

		expenseID := "exp-sms-" + externalID
		var sourceMovement map[string]any
		for _, movement := range movements {
			if strings.TrimSpace(stringValue(movement["sourceMobileTransaction"])) != externalID && strings.TrimSpace(stringValue(movement["sourceId"])) != externalID {
				continue
			}
			sourceMovement = movement
			movement["transactionType"] = "expense"
			movement["reportedCustomer"] = reportedCustomer
			movement["payer"] = ""
			movement["counterparty"] = counterparty
			movement["sourceExpense"] = expenseID
			break
		}

		if !expenseSources[externalID] {
			expense := map[string]any{
				"id": expenseID, "date": transaction["occurredAt"], "occurredJalali": transaction["occurredJalali"],
				"group": group, "subgroup": subgroup, "amount": transaction["amount"], "description": transaction["title"],
				"accountId": transaction["accountId"], "counterparty": counterparty, "reportedCustomer": reportedCustomer,
				"source_type": "mobile_sms", "sourceId": externalID, "bank": transaction["bank"],
			}
			if sourceMovement != nil {
				for _, key := range []string{"date", "occurredJalali", "amount", "description", "accountId", "bank"} {
					if value := sourceMovement[key]; strings.TrimSpace(stringValue(value)) != "" {
						expense[key] = value
					}
				}
			}
			expenses = append([]map[string]any{expense}, expenses...)
			expenseSources[externalID] = true
		}
		changed = true
	}
	if !changed {
		return raw, "", false, nil
	}
	state["mobileTransactions"] = mapsToAny(transactions)
	state["movements"] = mapsToAny(movements)
	state["expenses"] = mapsToAny(expenses)
	payload, err := json.Marshal(state)
	if err != nil {
		return nil, "", false, err
	}
	canonical, checksum, err := validateWorkspaceState(payload)
	return canonical, checksum, err == nil, err
}
