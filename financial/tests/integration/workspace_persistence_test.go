package integration_test

import (
	"bytes"
	"context"
	"database/sql"
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

func TestWorkspacePersistsAndRejectsCrossTenantReads(t *testing.T) {
	if os.Getenv("TEST_POSTGRES_DSN") == "" {
		t.Skip("TEST_POSTGRES_DSN is not configured")
	}
	t.Setenv("ALLOW_DEV_AUTH", "true")
	t.Setenv("CACHE_DISABLED", "true")
	db, err := postgres.Connect(postgres.LoadConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	migrations := filepath.Join("..", "..", "internal", "infrastructure", "persistence", "postgres", "migrations")
	if err := postgres.RunMigrations(db, migrations); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	stamp := time.Now().UnixNano()
	var companyOne, companyTwo int64
	if err := db.QueryRow(`INSERT INTO companies (code,name) VALUES ($1,$2) RETURNING id`, fmt.Sprintf("W1-%d", stamp), "workspace one").Scan(&companyOne); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO companies (code,name) VALUES ($1,$2) RETURNING id`, fmt.Sprintf("W2-%d", stamp), "workspace two").Scan(&companyTwo); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(router.SetupRouter())
	defer server.Close()
	docOne := putWorkspace(t, server.URL, companyOne, 0, validInvoiceWorkspace("ONE", 100))
	putWorkspace(t, server.URL, companyTwo, 0, validInvoiceWorkspace("TWO", 200))

	stateOne := getWorkspace(t, server.URL, companyOne)
	encoded, _ := json.Marshal(stateOne["state"])
	if !bytes.Contains(encoded, []byte(`"ONE"`)) || bytes.Contains(encoded, []byte(`"TWO"`)) {
		t.Fatalf("workspace tenant leak: %s", encoded)
	}
	conflict := workspaceRequest(t, http.MethodPut, server.URL+"/api/workspace", companyOne, map[string]any{"revision": 0, "state": map[string]any{"invoices": []any{}, "accounts": []any{}}})
	if conflict.StatusCode != http.StatusConflict {
		t.Fatalf("expected revision conflict after revision %v, got %s", docOne["revision"], conflict.Status)
	}
	_ = conflict.Body.Close()

	var voucherCount int
	var debit, credit float64
	ctx := context.Background()
	if _, err := postgres.WithCompanySession(ctx, db, companyOne, func(q postgres.SessionQueryable) (bool, error) {
		err := q.QueryRowContext(ctx, `
			SELECT COUNT(DISTINCT v.id), COALESCE(SUM(l.debit),0), COALESCE(SUM(l.credit),0)
			FROM journal_vouchers v
			JOIN journal_voucher_lines l ON l.journal_voucher_id=v.id AND l.company_id=v.company_id
			WHERE v.company_id=$1 AND v.status='Posted' AND v.external_key LIKE 'WS:%'
		`, companyOne).Scan(&voucherCount, &debit, &credit)
		return err == nil, err
	}); err != nil {
		t.Fatal(err)
	}
	if voucherCount == 0 || debit <= 0 || debit != credit {
		t.Fatalf("workspace ledger was not posted and balanced: vouchers=%d debit=%.2f credit=%.2f", voucherCount, debit, credit)
	}

	err = postgres.WithCompanyTx(ctx, db, companyOne, func(tx *sql.Tx) error {
		var voucherID, accountID int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM journal_vouchers WHERE company_id=$1 AND status='Posted' AND external_key LIKE 'WS:%' LIMIT 1`, companyOne).Scan(&voucherID); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT account_id FROM journal_voucher_lines WHERE company_id=$1 AND journal_voucher_id=$2 LIMIT 1`, companyOne, voucherID).Scan(&accountID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO journal_voucher_lines(company_id,journal_voucher_id,account_id,debit,credit,line_no) VALUES($1,$2,$3,1,0,99)`, companyOne, voucherID, accountID)
		return err
	})
	if err == nil {
		t.Fatal("posted voucher accepted a new line")
	}
}

func validInvoiceWorkspace(number string, total float64) map[string]any {
	return map[string]any{
		"accounts": []any{},
		"invoices": []any{map[string]any{
			"id": number, "number": number, "date": "2026-07-23", "customer": "tenant " + number,
			"item": "fabric", "total": total,
			"payments": []any{map[string]any{"id": number + "-credit", "type": "credit", "amount": total}},
		}},
	}
}

func putWorkspace(t *testing.T, base string, companyID, revision int64, state map[string]any) map[string]any {
	t.Helper()
	response := workspaceRequest(t, http.MethodPut, base+"/api/workspace", companyID, map[string]any{"revision": revision, "state": state})
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("save workspace: %s", response.Status)
	}
	var doc map[string]any
	if err := json.NewDecoder(response.Body).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func getWorkspace(t *testing.T, base string, companyID int64) map[string]any {
	t.Helper()
	response := workspaceRequest(t, http.MethodGet, base+"/api/workspace", companyID, nil)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("get workspace: %s", response.Status)
	}
	var doc map[string]any
	if err := json.NewDecoder(response.Body).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func workspaceRequest(t *testing.T, method, url string, companyID int64, body any) *http.Response {
	t.Helper()
	var payload []byte
	if body != nil {
		payload, _ = json.Marshal(body)
	}
	req, err := http.NewRequest(method, url, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	applyDevTenantHeaders(req, companyID)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return response
}
