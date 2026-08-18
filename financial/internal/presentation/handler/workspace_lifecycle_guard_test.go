package handler

import "testing"

func TestWorkspaceLifecycleAllowsSingleOpenCheckAssignmentToMatchingPurchase(t *testing.T) {
	oldState := map[string]any{
		"receivableDocs": []any{
			map[string]any{"id": "rch-1", "checkNo": "1001", "customer": "Buyer", "amount": 500.0, "status": "open"},
		},
	}
	newState := map[string]any{
		"receivableDocs": []any{
			map[string]any{"id": "rch-1", "checkNo": "1001", "customer": "Buyer", "amount": 500.0, "status": "assigned", "assignedTo": "Supplier", "assignedIncomingInvoice": "pinv-1"},
		},
		"incomingInvoices": []any{
			map[string]any{"id": "pinv-1", "customer": "Supplier", "payments": []any{
				map[string]any{"type": "assign_receivable", "docId": "rch-1", "amount": 500.0},
			}},
		},
	}
	if err := validateWorkspaceLifecycleChanges(oldState, newState); err != nil {
		t.Fatalf("valid assignment rejected: %v", err)
	}
}

func TestWorkspaceLifecycleRejectsDoubleAssignment(t *testing.T) {
	oldState := map[string]any{
		"receivableDocs": []any{
			map[string]any{"id": "rch-1", "checkNo": "1001", "amount": 500.0, "status": "open"},
		},
	}
	newState := map[string]any{
		"receivableDocs": []any{
			map[string]any{"id": "rch-1", "checkNo": "1001", "amount": 500.0, "status": "assigned", "assignedTo": "Supplier", "assignedIncomingInvoice": "pinv-1"},
		},
		"incomingInvoices": []any{
			map[string]any{"id": "pinv-1", "customer": "Supplier", "payments": []any{map[string]any{"type": "assign_receivable", "docId": "rch-1", "amount": 500.0}}},
			map[string]any{"id": "pinv-2", "customer": "Supplier", "payments": []any{map[string]any{"type": "assign_receivable", "docId": "rch-1", "amount": 500.0}}},
		},
	}
	if err := validateWorkspaceLifecycleChanges(oldState, newState); err == nil {
		t.Fatal("same receivable check was allowed to settle two purchases")
	}
}

func TestWorkspaceLifecycleRejectsAssignmentFromClearedState(t *testing.T) {
	oldState := map[string]any{
		"receivableDocs": []any{
			map[string]any{"id": "rch-1", "checkNo": "1001", "amount": 500.0, "status": "cleared"},
		},
	}
	newState := map[string]any{
		"receivableDocs": []any{
			map[string]any{"id": "rch-1", "checkNo": "1001", "amount": 500.0, "status": "assigned", "assignedTo": "Supplier", "assignedIncomingInvoice": "pinv-1"},
		},
		"incomingInvoices": []any{
			map[string]any{"id": "pinv-1", "customer": "Supplier", "payments": []any{map[string]any{"type": "assign_receivable", "docId": "rch-1", "amount": 500.0}}},
		},
	}
	if err := validateWorkspaceLifecycleChanges(oldState, newState); err == nil {
		t.Fatal("cleared receivable check was allowed to become assigned")
	}
}

func TestWorkspaceLifecycleRejectsNewDocumentCreatedAsAssigned(t *testing.T) {
	newState := map[string]any{
		"receivableDocs": []any{
			map[string]any{"id": "rch-new", "checkNo": "2001", "amount": 400.0, "status": "assigned", "assignedTo": "Supplier"},
		},
		"incomingInvoices": []any{
			map[string]any{"id": "purchase-known", "customer": "Supplier"},
		},
	}
	if err := validateWorkspaceLifecycleChanges(map[string]any{}, newState); err == nil {
		t.Fatal("new receivable check was allowed to bypass open state")
	}
}

func TestWorkspaceLifecycleRejectsManualAssignmentToUnknownParty(t *testing.T) {
	oldState := map[string]any{
		"receivableDocs": []any{
			map[string]any{"id": "rch-1", "checkNo": "1001", "amount": 500.0, "status": "open"},
		},
	}
	newState := map[string]any{
		"receivableDocs": []any{
			map[string]any{"id": "rch-1", "checkNo": "1001", "amount": 500.0, "status": "assigned", "assignedTo": "Unknown Party"},
		},
	}
	if err := validateWorkspaceLifecycleChanges(oldState, newState); err == nil {
		t.Fatal("manual assignment to unknown non-supplier party was allowed")
	}
}

func TestWorkspaceLifecycleAllowsManualAssignmentToKnownSupplier(t *testing.T) {
	oldState := map[string]any{
		"receivableDocs": []any{
			map[string]any{"id": "rch-1", "checkNo": "1001", "amount": 500.0, "status": "open"},
		},
	}
	newState := map[string]any{
		"receivableDocs": []any{
			map[string]any{"id": "rch-1", "checkNo": "1001", "amount": 500.0, "status": "assigned", "assignedTo": "Supplier"},
		},
		"incomingInvoices": []any{
			map[string]any{"id": "pinv-known", "customer": "Supplier", "amount": 1000.0},
		},
	}
	if err := validateWorkspaceLifecycleChanges(oldState, newState); err != nil {
		t.Fatalf("manual assignment to known supplier rejected: %v", err)
	}
}

func TestWorkspaceLifecycleRejectsAssignedAmountMismatch(t *testing.T) {
	oldState := map[string]any{
		"receivableDocs": []any{
			map[string]any{"id": "rch-1", "checkNo": "1001", "amount": 500.0, "status": "open"},
		},
	}
	newState := map[string]any{
		"receivableDocs": []any{
			map[string]any{"id": "rch-1", "checkNo": "1001", "amount": 500.0, "status": "assigned", "assignedTo": "Supplier", "assignedIncomingInvoice": "pinv-1"},
		},
		"incomingInvoices": []any{
			map[string]any{"id": "pinv-1", "customer": "Supplier", "payments": []any{map[string]any{"type": "assign_receivable", "docId": "rch-1", "amount": 450.0}}},
		},
	}
	if err := validateWorkspaceLifecycleChanges(oldState, newState); err == nil {
		t.Fatal("partial/mismatched check assignment was allowed")
	}
}
