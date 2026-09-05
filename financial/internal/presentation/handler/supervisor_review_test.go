package handler

import (
	"testing"
	"time"
)

func TestSupervisorApprovalBindsActorTenantRevisionDraftAndSource(t *testing.T) {
	now := time.Now()
	base := supervisorApproval{Company: 2, User: 3, Revision: 4, Checksum: "draft", SourceStamp: "source", Expires: now.Add(time.Minute).Unix()}
	token := signSupervisorApproval(base)
	if !checkSupervisorApproval(token, base, now) {
		t.Fatal("own unexpired review rejected")
	}
	for name, change := range map[string]func(*supervisorApproval){
		"tenant": func(x *supervisorApproval) { x.Company++ }, "actor": func(x *supervisorApproval) { x.User++ }, "revision": func(x *supervisorApproval) { x.Revision++ }, "draft": func(x *supervisorApproval) { x.Checksum = "other" }, "source": func(x *supervisorApproval) { x.SourceStamp = "changed" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := base
			change(&changed)
			if checkSupervisorApproval(token, changed, now) {
				t.Fatal("changed review accepted")
			}
		})
	}
	if checkSupervisorApproval(token, base, now.Add(2*time.Minute)) {
		t.Fatal("expired review accepted")
	}
	if checkSupervisorApproval(token+"x", base, now) {
		t.Fatal("forged signature accepted")
	}
}

func supervisorExpenseState(amount float64) map[string]any {
	return map[string]any{
		"accounts":  []any{map[string]any{"id": "bank", "name": "Test bank", "type": "بانک", "opening": 1000}},
		"expenses":  []any{map[string]any{"id": "e", "date": "2026-09-01", "group": "expense", "subgroup": "test", "amount": amount, "accountId": "bank"}},
		"movements": []any{map[string]any{"id": "m", "date": "2026-09-01", "direction": "out", "transactionType": "expense", "amount": amount, "accountId": "bank", "sourceExpense": "e"}},
	}
}

func TestSupervisorEditAndDeletePreviewOnlyNetLedgerEffect(t *testing.T) {
	old := supervisorExpenseState(100)
	updated := supervisorExpenseState(140)
	lines, err := supervisorLedgerDelta(old, updated)
	if err != nil {
		t.Fatal(err)
	}
	var debit, credit float64
	for _, line := range lines {
		debit += line.Debit
		credit += line.Credit
	}
	if debit != 40 || credit != 40 {
		t.Fatalf("edit applied full invoice rather than delta: %#v", lines)
	}
	deleted := supervisorExpenseState(140)
	deleted["expenses"] = []any{}
	deleted["movements"] = []any{}
	if err := validateWorkspaceSupervisorChanges(updated, deleted); err != nil {
		t.Fatal(err)
	}
	lines, err = supervisorLedgerDelta(updated, deleted)
	if err != nil {
		t.Fatal(err)
	}
	debit, credit = 0, 0
	for _, line := range lines {
		debit += line.Debit
		credit += line.Credit
	}
	if debit != 140 || credit != 140 {
		t.Fatal("delete did not reverse effect")
	}
}

func TestSupervisorRejectsChildOnlyMutationsAndAllowsUnrelatedLegacyWork(t *testing.T) {
	for _, kind := range []string{"amount", "direction", "account", "delete-movement", "delete-parent", "duplicate"} {
		t.Run(kind, func(t *testing.T) {
			old := supervisorExpenseState(100)
			next := supervisorExpenseState(100)
			m := rowsFrom(next, "movements")[0]
			switch kind {
			case "amount":
				m["amount"] = 90
			case "direction":
				m["direction"] = "in"
			case "account":
				m["accountId"] = "missing"
			case "delete-movement":
				next["movements"] = []any{}
			case "delete-parent":
				next["expenses"] = []any{}
			case "duplicate":
				next["movements"] = []any{m, m}
			}
			if validateWorkspaceSupervisorChanges(old, next) == nil {
				t.Fatal("broken relation accepted")
			}
		})
	}
	old := supervisorExpenseState(100)
	old["movements"] = []any{}
	next := supervisorExpenseState(100)
	next["movements"] = []any{}
	next["manualCustomers"] = []any{"new customer"}
	if err := validateWorkspaceSupervisorChanges(old, next); err != nil {
		t.Fatalf("legacy issue blocked unrelated update: %v", err)
	}
}

func TestSupervisorPreventsReissuingPaidChequeDuringInvoiceEdit(t *testing.T) {
	old := testWorkspace(t, `{"payableDocs":[{"id":"paid","status":"paid","amount":100,"customer":"supplier","checkNo":"42","dueDate":"2026-09-01"}]}`)
	next := testWorkspace(t, `{"payableDocs":[{"id":"new","status":"open","amount":100,"customer":"supplier","checkNo":"42","dueDate":"2026-09-01"}]}`)
	if validateWorkspaceSupervisorChanges(old, next) == nil {
		t.Fatal("paid cheque was silently reissued")
	}
	if err := validateWorkspaceSupervisorChanges(old, old); err != nil {
		t.Fatal(err)
	}
}

func TestSupervisorMatchesEquivalentPersianAndGregorianExpenseDates(t *testing.T) {
	if !supervisorSameDate("1405/06/10", "2026-09-01") {
		t.Fatal("equivalent accounting dates did not match")
	}
	if supervisorSameDate("", "invalid") {
		t.Fatal("invalid dates falsely matched")
	}
}
