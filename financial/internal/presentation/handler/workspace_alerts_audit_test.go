package handler

import (
	"testing"
	"time"
)

func alertTitles(rows []map[string]any) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, stringValue(row["title"]))
	}
	return out
}

func containsText(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func TestWorkspaceAlertsDoNotFlagTransferDestinationAsMissingCash(t *testing.T) {
	state := map[string]any{
		"accounts": []any{
			map[string]any{"id": "a", "name": "A", "opening": 1000.0},
			map[string]any{"id": "b", "name": "B", "opening": 0.0},
		},
		"movements": []any{
			map[string]any{"accountId": "a", "counterAccountId": "b", "transactionType": "transfer", "direction": "out", "amount": 500.0},
		},
	}
	rows := buildWorkspaceAlertsAccurate(state, time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC))
	if containsText(alertTitles(rows), "مانده منفی حساب") {
		t.Fatalf("valid internal transfer generated negative-balance alert: %+v", rows)
	}
}

func TestWorkspaceAlertsSkipAssignedReceivableCheckDueAlert(t *testing.T) {
	state := map[string]any{
		"receivableDocs": []any{
			map[string]any{"checkNo": "A1", "status": "assigned", "dueDate": "2026-01-01", "amount": 100.0},
		},
	}
	rows := buildWorkspaceAlertsAccurate(state, time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC))
	if containsText(alertTitles(rows), "سند سررسید گذشته") {
		t.Fatalf("assigned check generated overdue receivable alert: %+v", rows)
	}
}

func TestWorkspaceAlertsClassifyBouncedCheckExplicitly(t *testing.T) {
	state := map[string]any{
		"receivableDocs": []any{
			map[string]any{"checkNo": "B1", "status": "bounced", "dueDate": "2026-01-01", "amount": 250.0},
		},
	}
	rows := buildWorkspaceAlertsAccurate(state, time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC))
	titles := alertTitles(rows)
	if !containsText(titles, "سند برگشتی") {
		t.Fatalf("bounced check did not generate explicit returned alert: %+v", rows)
	}
	if containsText(titles, "سند سررسید گذشته") {
		t.Fatalf("bounced check should not also be classified as ordinary overdue: %+v", rows)
	}
}
