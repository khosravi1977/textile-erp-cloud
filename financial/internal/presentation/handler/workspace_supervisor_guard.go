package handler

import (
	"encoding/json"
	"fmt"
	"strings"
)

// validateWorkspaceSupervisorChanges is the server-side financial supervisor.
// It verifies that each new or changed source document has exactly the cash,
// inventory and trace effects that the UI promised before the state is stored.
func validateWorkspaceSupervisorChanges(oldState, newState map[string]any) error {
	previous := map[string]string{}
	for _, issue := range supervisorStateFindings(oldState) {
		previous[issue.ID] = issue.Evidence
	}
	for _, issue := range supervisorStateFindings(newState) {
		if issue.Severity == "critical" && previous[issue.ID] != issue.Evidence {
			return fmt.Errorf("ناظر مالی: %s [%s]", issue.Detail, issue.Reference)
		}
	}
	return nil
}

func validateSupervisorCashPayments(invoice map[string]any, movements []map[string]any, accounts map[string]bool, sourceField, id, label string) error {
	expected := make([]map[string]any, 0)
	for _, payment := range rowsFrom(invoice, "payments") {
		if stringValue(payment["type"]) != "cash" || number(payment["amount"]) <= 0 {
			continue
		}
		accountID := strings.TrimSpace(stringValue(payment["accountId"]))
		if !accounts[accountID] {
			return fmt.Errorf("ناظر مالی: حساب تسویه نقدی %s %s معتبر نیست", label, id)
		}
		expected = append(expected, payment)
	}
	actual := filterSupervisorMovements(movements, sourceField, id)
	used := make([]bool, len(actual))
	for _, payment := range expected {
		matched := false
		for index, movement := range actual {
			if used[index] || !amountsEqual(number(movement["amount"]), number(payment["amount"])) || strings.TrimSpace(stringValue(movement["accountId"])) != strings.TrimSpace(stringValue(payment["accountId"])) {
				continue
			}
			used[index] = true
			matched = true
			break
		}
		if !matched {
			return fmt.Errorf("ناظر مالی: پرداخت نقدی %s %s در بانک/صندوق اعمال نشده است", label, id)
		}
	}
	if len(actual) != len(expected) {
		return fmt.Errorf("ناظر مالی: تعداد گردش‌های نقدی %s %s با ردیف‌های تسویه یکسان نیست", label, id)
	}
	return nil
}

func validateSupervisorSourceUniqueness(oldState, newState map[string]any) error {
	counts := func(state map[string]any) map[string]int {
		result := map[string]int{}
		for _, field := range []string{"incomingInvoices", "yarnOutInvoices", "expenses"} {
			for _, row := range rowsFrom(state, field) {
				sourceType := strings.TrimSpace(stringValue(row["source_type"]))
				sourceID := strings.TrimSpace(stringValue(row["sourceId"]))
				if sourceType == "" || sourceType == "manual" || sourceID == "" {
					continue
				}
				result[sourceType+":"+sourceID]++
			}
		}
		return result
	}
	oldCounts, newCounts := counts(oldState), counts(newState)
	for key, count := range newCounts {
		if count > 1 && count > oldCounts[key] {
			return fmt.Errorf("ناظر مالی: منبع %s بیش از یک‌بار در مالی ثبت شده است", key)
		}
	}
	return nil
}

func indexSupervisorRows(rows []map[string]any, keys ...string) map[string]map[string]any {
	out := make(map[string]map[string]any, len(rows))
	for _, row := range rows {
		if id := firstText(row, keys...); id != "" {
			out[id] = row
		}
	}
	return out
}

func filterSupervisorMovements(rows []map[string]any, field, expected string) []map[string]any {
	out := make([]map[string]any, 0)
	for _, row := range rows {
		if strings.TrimSpace(stringValue(row[field])) == expected {
			out = append(out, row)
		}
	}
	return out
}

func supervisorRowChanged(oldRow, newRow map[string]any) bool {
	if newRow == nil {
		return false
	}
	if oldRow == nil {
		return true
	}
	oldPayload, _ := json.Marshal(oldRow)
	newPayload, _ := json.Marshal(newRow)
	return string(oldPayload) != string(newPayload)
}
