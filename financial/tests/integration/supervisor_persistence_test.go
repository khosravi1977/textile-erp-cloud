package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/erpsystem/textile-erp/internal/infrastructure/persistence/postgres"
	"github.com/erpsystem/textile-erp/internal/presentation/router"
)

func TestSupervisorPreviewCommitAndReplayAreAtomic(t *testing.T) {
	if os.Getenv("TEST_POSTGRES_DSN") == "" {
		t.Skip("TEST_POSTGRES_DSN is not configured; runs in cloud CI")
	}
	t.Setenv("ALLOW_DEV_AUTH", "true")
	t.Setenv("CACHE_DISABLED", "true")
	db, err := postgres.Connect(postgres.LoadConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := postgres.RunMigrations(db, filepath.Join("..", "..", "internal", "infrastructure", "persistence", "postgres", "migrations")); err != nil {
		t.Fatal(err)
	}
	var company int64
	if err := db.QueryRow(`INSERT INTO companies(code,name) VALUES($1,'supervisor test') RETURNING id`, fmt.Sprintf("SUP-%d", time.Now().UnixNano())).Scan(&company); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(router.SetupRouter())
	defer server.Close()
	state := validInvoiceWorkspace("REVIEW", 100)
	post := func(path string, body any, want int) map[string]any {
		t.Helper()
		resp := workspaceRequest(t, http.MethodPost, server.URL+path, company, body)
		defer resp.Body.Close()
		var result map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&result)
		if resp.StatusCode != want {
			t.Fatalf("%s status=%d want=%d response=%v", path, resp.StatusCode, want, result)
		}
		return result
	}
	review := post("/api/supervisor/preview", map[string]any{"state": state, "revision": 0}, 200)
	before := getWorkspace(t, server.URL, company)
	if before["revision"].(float64) != 0 {
		t.Fatal("preview changed data")
	}
	changed := validInvoiceWorkspace("REVIEW", 200)
	post("/api/supervisor/commit", map[string]any{"state": changed, "revision": 0, "approval": review["approval"]}, 409)
	post("/api/supervisor/commit", map[string]any{"state": state, "revision": 0, "approval": review["approval"]}, 200)
	post("/api/supervisor/commit", map[string]any{"state": state, "revision": 0, "approval": review["approval"]}, 409)
	loaded := getWorkspace(t, server.URL, company)
	if loaded["revision"].(float64) != 1 {
		t.Fatal("replay changed revision")
	}
	var approvals int
	if err := db.QueryRow(`SELECT count(*) FROM financial_supervisor_approvals WHERE company_id=$1`, company).Scan(&approvals); err != nil || approvals != 1 {
		t.Fatalf("approval evidence count=%d err=%v", approvals, err)
	}
	var debit, credit float64
	if err := db.QueryRow(`SELECT COALESCE(sum(l.debit),0),COALESCE(sum(l.credit),0) FROM journal_voucher_lines l JOIN journal_vouchers v ON v.id=l.journal_voucher_id AND v.company_id=l.company_id WHERE v.company_id=$1 AND v.external_key LIKE 'WS:%'`, company).Scan(&debit, &credit); err != nil || debit != 100 || credit != 100 {
		t.Fatalf("ledger not exactly once: debit=%v credit=%v err=%v", debit, credit, err)
	}
	// A stale approval after another writer must not overwrite their update.
	review = post("/api/supervisor/preview", map[string]any{"state": changed, "revision": 1}, 200)
	putWorkspace(t, server.URL, company, 1, validInvoiceWorkspace("REVIEW", 150))
	post("/api/supervisor/commit", map[string]any{"state": changed, "revision": 1, "approval": review["approval"]}, 409)
	if getWorkspace(t, server.URL, company)["revision"].(float64) != 2 {
		t.Fatal("stale preview wrote data")
	}
	// The shared save path applies expense + bank + journal changes atomically,
	// including replacement and deletion, not just a successful HTTP response.
	expenseState := func(amount float64) map[string]any {
		return map[string]any{
			"accounts":  []any{map[string]any{"id": "bank", "name": "test bank", "type": "بانک", "opening": 0}},
			"expenses":  []any{map[string]any{"id": "expense", "date": "2026-09-01", "group": "test", "subgroup": "test", "amount": amount, "accountId": "bank"}},
			"movements": []any{map[string]any{"id": "movement", "date": "2026-09-01", "direction": "out", "transactionType": "expense", "amount": amount, "accountId": "bank", "sourceExpense": "expense"}},
		}
	}
	putWorkspace(t, server.URL, company, 2, expenseState(100))
	putWorkspace(t, server.URL, company, 3, expenseState(150))
	var expenseNet float64
	net := func() {
		t.Helper()
		if err := db.QueryRow(`SELECT COALESCE(SUM(l.debit-l.credit),0) FROM journal_voucher_lines l JOIN accounts a ON a.id=l.account_id AND a.company_id=l.company_id WHERE l.company_id=$1 AND a.code='5900'`, company).Scan(&expenseNet); err != nil {
			t.Fatal(err)
		}
	}
	net()
	if expenseNet != 150 {
		t.Fatalf("expense edit doubled/missed journal: %v", expenseNet)
	}
	deleted := expenseState(150)
	deleted["expenses"] = []any{}
	deleted["movements"] = []any{}
	putWorkspace(t, server.URL, company, 4, deleted)
	net()
	if expenseNet != 0 {
		t.Fatalf("expense delete left ledger effect: %v", expenseNet)
	}
}
