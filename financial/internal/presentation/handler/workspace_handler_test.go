package handler

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/erpsystem/textile-erp/internal/platform/requestctx"
)

func TestValidateWorkspaceState(t *testing.T) {
	state, checksum, err := validateWorkspaceState(json.RawMessage(`{"accounts":[],"invoices":[]}`))
	if err != nil {
		t.Fatalf("validate state: %v", err)
	}
	if len(state) == 0 || len(checksum) != 64 {
		t.Fatalf("unexpected canonical state or checksum: %s %s", state, checksum)
	}
	if _, _, err := validateWorkspaceState(json.RawMessage(`[]`)); err == nil {
		t.Fatal("expected non-object workspace to fail")
	}
	if _, _, err := validateWorkspaceState(json.RawMessage(`{"invoices":{}}`)); err == nil {
		t.Fatal("expected invalid invoices collection to fail")
	}
}

func TestRestrictedWorkspaceMergePreservesOtherModules(t *testing.T) {
	current := json.RawMessage(`{"invoices":[{"id":"original"}],"expenses":[{"id":"old"}]}`)
	proposed := json.RawMessage(`{"invoices":[{"id":"tampered"}],"expenses":[{"id":"new"}]}`)
	merged, _, err := mergeWorkspaceState(current, proposed, map[string]bool{"expenses": true})
	if err != nil {
		t.Fatal(err)
	}
	state := decodeWorkspaceMap(merged)
	if got := stringValue(rowsFrom(state, "invoices")[0]["id"]); got != "original" {
		t.Fatalf("unauthorized invoice update was applied: %s", got)
	}
	if got := stringValue(rowsFrom(state, "expenses")[0]["id"]); got != "new" {
		t.Fatalf("authorized expense update was not applied: %s", got)
	}
}

func TestAccountingValidationAllowsUnchangedLegacyRows(t *testing.T) {
	legacy := decodeWorkspaceMap(json.RawMessage(`{"invoices":[{"id":"legacy"}]}`))
	updated := decodeWorkspaceMap(json.RawMessage(`{"invoices":[{"id":"legacy"}],"expenses":[{"id":"cost-1","date":"2026-07-23","amount":100,"accountId":"cash"}]}`))
	if err := validateWorkspaceAccountingChanges(legacy, updated); err != nil {
		t.Fatalf("unchanged legacy row blocked an unrelated valid change: %v", err)
	}
}

func TestAccountingValidationRejectsChangedInvalidLegacyRow(t *testing.T) {
	legacy := decodeWorkspaceMap(json.RawMessage(`{"invoices":[{"id":"legacy"}]}`))
	updated := decodeWorkspaceMap(json.RawMessage(`{"invoices":[{"id":"legacy","total":100}]}`))
	if err := validateWorkspaceAccountingChanges(legacy, updated); err == nil {
		t.Fatal("modified invalid legacy row must be rejected")
	}
}

func TestAccountingValidationRejectsDuplicateChequeAcrossExistingRows(t *testing.T) {
	oldState := decodeWorkspaceMap(json.RawMessage(`{"receivableDocs":[{"id":"c1","checkNo":"42","amount":100,"dueDate":"2026-08-01"}]}`))
	newState := decodeWorkspaceMap(json.RawMessage(`{"receivableDocs":[{"id":"c1","checkNo":"42","amount":100,"dueDate":"2026-08-01"},{"id":"c2","checkNo":"42","amount":200,"dueDate":"2026-08-02"}]}`))
	if err := validateWorkspaceAccountingChanges(oldState, newState); err == nil {
		t.Fatal("duplicate cheque number must be rejected")
	}
}

func TestWorkspaceDocumentFiltering(t *testing.T) {
	ctx := requestctx.WithAccess(context.Background(), []string{"costs"}, true)
	doc := workspaceDocument{State: json.RawMessage(`{"invoices":[{"id":1}],"expenses":[{"id":2}],"movements":[]}`)}
	filtered := filterWorkspaceDocument(doc, ctx)
	state := decodeWorkspaceMap(filtered.State)
	if _, exists := state["invoices"]; exists {
		t.Fatal("invoice data leaked into costs-only workspace")
	}
	if _, exists := state["expenses"]; !exists {
		t.Fatal("cost data was removed from costs-only workspace")
	}
}

func TestWorkspaceSummaryAndAlerts(t *testing.T) {
	yesterday := time.Now().Add(-24 * time.Hour).Format("2006-01-02")
	state := map[string]any{
		"invoices":         []any{map[string]any{"number": "OUT-1", "customer": "مشتری", "total": 1000.0, "payments": []any{map[string]any{"amount": 600.0}}}},
		"incomingInvoices": []any{map[string]any{"amount": 200.0}},
		"expenses":         []any{map[string]any{"amount": 100.0}},
		"receivableDocs": []any{
			map[string]any{"checkNo": "10", "amount": 300.0, "dueDate": yesterday, "status": "open"},
			map[string]any{"checkNo": "10", "amount": 200.0, "dueDate": yesterday, "status": "open"},
		},
		"payableDocs": []any{map[string]any{"checkNo": "20", "amount": 150.0, "dueDate": yesterday, "status": "open"}},
		"accounts":    []any{map[string]any{"id": "cash", "name": "صندوق", "opening": 0.0}},
		"movements":   []any{map[string]any{"accountId": "cash", "direction": "out", "amount": 50.0}},
	}
	summary := buildWorkspaceSummary(state, 3, time.Now())
	if summary["total_sales"] != 1000.0 || summary["gross_margin"] != 700.0 || summary["cash_balance"] != -50.0 {
		t.Fatalf("unexpected workspace summary: %#v", summary)
	}
	alerts := buildWorkspaceAlerts(state)
	if len(alerts) < 5 {
		t.Fatalf("expected financial alerts, got %#v", alerts)
	}
}
