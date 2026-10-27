package integration_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/erpsystem/textile-erp/internal/infrastructure/persistence/postgres"
	"github.com/erpsystem/textile-erp/internal/presentation/router"
)

// TestOperationalExpenseToBankTransactionE2E proves the full chain for an
// operational expense:
//  1. the operational cycle server records the expense (h_rozmare) through its
//     own HTTP API,
//  2. the financial backend reads it through the operational bridge,
//  3. posting it as a HesabYar standard transaction creates exactly one
//     bank_transactions row with an "OP-" external id plus a balanced Posted
//     voucher, and re-posting the same id is an idempotent duplicate,
//  4. the workspace state and summary reflect the expense,
//  5. cleanup removes the expense on the operational side and every
//     financial-side row belonging to the test company.
func TestOperationalExpenseToBankTransactionE2E(t *testing.T) {
	if os.Getenv("TEST_POSTGRES_DSN") == "" {
		t.Skip("TEST_POSTGRES_DSN is not configured")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain is not available to build the operational server")
	}
	operationalDir, err := filepath.Abs(filepath.Join("..", "..", "..", "operational_cycle_go"))
	if err != nil {
		t.Skipf("operational module path is not reachable: %v", err)
	}
	if _, err := os.Stat(filepath.Join(operationalDir, "cmd", "server")); err != nil {
		t.Skipf("operational module is not present: %v", err)
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
	var companyID int64
	if err := db.QueryRow(`INSERT INTO companies (code,name) VALUES ($1,$2) RETURNING id`,
		fmt.Sprintf("OPE2E-%d", stamp), "operational expense e2e").Scan(&companyID); err != nil {
		t.Fatal(err)
	}

	operationalBase, opClient, operationalDBPath := startOperationalServer(t, goBin, operationalDir)
	cookies := loginOperationalAdmin(t, opClient, operationalBase)

	expenseID, expenseAmount := createOperationalExpense(t, opClient, operationalBase, cookies)

	// Point the financial operational bridge at the same operational database.
	t.Setenv("OPERATIONAL_DB_DRIVER", "sqlite")
	t.Setenv("OPERATIONAL_SQLITE_PATH", operationalDBPath)

	server := httptest.NewServer(router.SetupRouter())
	defer server.Close()

	// The financial operational bridge must see the freshly created expense.
	bridgeRows := getJSON[struct {
		Success bool `json:"success"`
		Rows    []struct {
			ID     int64   `json:"id"`
			Title  string  `json:"title"`
			Amount float64 `json:"amount"`
			DocNo  string  `json:"doc_no"`
		} `json:"rows"`
	}](t, server.URL+"/api/operational/expenses?limit=50", companyID)
	if !bridgeRows.Success {
		t.Fatal("operational bridge expenses call did not succeed")
	}
	seenByBridge := false
	for _, row := range bridgeRows.Rows {
		if row.ID == expenseID {
			seenByBridge = true
			if row.Amount != expenseAmount {
				t.Fatalf("bridge expense amount mismatch: got %v want %v", row.Amount, expenseAmount)
			}
		}
	}
	if !seenByBridge {
		t.Fatalf("operational expense %d was not visible through the financial bridge", expenseID)
	}

	// Posting the expense as a HesabYar standard transaction creates the OP- row.
	externalID := fmt.Sprintf("OP-%d", expenseID)
	payload := map[string]any{
		"external_transaction_id": externalID,
		"bank_account_id":         "bank-e2e",
		"direction":               "OUT",
		"amount":                  expenseAmount,
		"transaction_type":        "DIRECT_EXPENSE",
		"description":             "هزینه عملیاتی E2E",
		"bank_reference":          fmt.Sprintf("h_rozmare:%d", expenseID),
		"occurred_at":             time.Now().UTC().Format(time.RFC3339),
	}
	var synced struct {
		Success          bool   `json:"success"`
		Status           string `json:"status"`
		TransactionID    int64  `json:"erp_transaction_id"`
		PostingVoucherID int64  `json:"voucher_id"`
	}
	postJSONStatus(t, server.URL, companyID, "/api/v1/hesabyar/transactions", payload, &synced, http.StatusCreated)
	if !synced.Success || synced.Status != "synced" || synced.TransactionID <= 0 || synced.PostingVoucherID <= 0 {
		t.Fatalf("unexpected sync response: %+v", synced)
	}

	ctx := context.Background()
	var direction, txnType, txnStatus, source string
	var amount float64
	var voucherID int64
	if _, err := postgres.WithCompanySession(ctx, db, companyID, func(q postgres.SessionQueryable) (bool, error) {
		err := q.QueryRowContext(ctx, `
			SELECT direction, amount, transaction_type, status, source, posting_voucher_id
			FROM bank_transactions
			WHERE company_id=$1 AND external_transaction_id=$2
		`, companyID, externalID).Scan(&direction, &amount, &txnType, &txnStatus, &source, &voucherID)
		return err == nil, err
	}); err != nil {
		t.Fatalf("bank_transactions OP- row was not stored: %v", err)
	}
	if direction != "OUT" || txnType != "DIRECT_EXPENSE" || txnStatus != "SYNCED" || source != "hesabyar" {
		t.Fatalf("unexpected OP- row content: direction=%s type=%s status=%s source=%s", direction, txnType, txnStatus, source)
	}
	if amount != expenseAmount || voucherID != synced.PostingVoucherID {
		t.Fatalf("OP- row amount/voucher mismatch: amount=%v voucher=%d", amount, voucherID)
	}
	var voucherStatus string
	var debit, credit float64
	if _, err := postgres.WithCompanySession(ctx, db, companyID, func(q postgres.SessionQueryable) (bool, error) {
		err := q.QueryRowContext(ctx, `
			SELECT v.status, COALESCE(SUM(l.debit),0), COALESCE(SUM(l.credit),0)
			FROM journal_vouchers v
			JOIN journal_voucher_lines l ON l.journal_voucher_id=v.id AND l.company_id=v.company_id
			WHERE v.company_id=$1 AND v.id=$2
			GROUP BY v.status
		`, companyID, voucherID).Scan(&voucherStatus, &debit, &credit)
		return err == nil, err
	}); err != nil {
		t.Fatalf("posting voucher was not found: %v", err)
	}
	if voucherStatus != "Posted" || debit <= 0 || debit != credit {
		t.Fatalf("voucher was not posted balanced: status=%s debit=%.2f credit=%.2f", voucherStatus, debit, credit)
	}

	// Re-posting the same external id must be an idempotent duplicate.
	var duplicate struct {
		Status        string `json:"status"`
		TransactionID int64  `json:"erp_transaction_id"`
	}
	postJSONStatus(t, server.URL, companyID, "/api/v1/hesabyar/transactions", payload, &duplicate, http.StatusCreated)
	if duplicate.Status != "duplicate" || duplicate.TransactionID != synced.TransactionID {
		t.Fatalf("expected idempotent duplicate, got %+v", duplicate)
	}

	// The workspace state and summary must reflect the expense.
	workspaceState := map[string]any{
		"expenses": []any{map[string]any{
			"id": externalID, "title": "هزینه عملیاتی E2E", "amount": expenseAmount,
			"date": time.Now().Format("2006-01-02"), "accountId": "bank-e2e",
		}},
	}
	putWorkspace(t, server.URL, companyID, 0, workspaceState)
	summary := getJSON[struct {
		TotalExpenses float64 `json:"total_expenses"`
		ExpenseCount  int     `json:"expense_count"`
	}](t, server.URL+"/api/workspace/summary", companyID)
	if summary.TotalExpenses != expenseAmount || summary.ExpenseCount != 1 {
		t.Fatalf("workspace summary did not reflect the expense: %+v", summary)
	}

	// Operational cleanup: deleting the expense removes it from the bridge too.
	opRequest(t, opClient, http.MethodDelete, fmt.Sprintf("%s/api/expenses/%d", operationalBase, expenseID), nil, cookies, nil)
	afterDelete := getJSON[struct {
		Rows []struct {
			ID int64 `json:"id"`
		} `json:"rows"`
	}](t, server.URL+"/api/operational/expenses?limit=50", companyID)
	for _, row := range afterDelete.Rows {
		if row.ID == expenseID {
			t.Fatalf("operational expense %d survived deletion", expenseID)
		}
	}

	// Financial cleanup: the synced OP- bank row and the workspace document are
	// removable. Posted vouchers are intentionally immutable (migration 016
	// trigger) and stay behind as the audit trail, so the test company is
	// deactivated instead of deleted.
	cleanupErr := postgres.WithCompanyTx(ctx, db, companyID, func(tx *sql.Tx) error {
		for _, stmt := range []string{
			`DELETE FROM bank_transactions WHERE company_id=$1`,
			`DELETE FROM financial_workspace_history WHERE company_id=$1`,
			`DELETE FROM financial_workspace_states WHERE company_id=$1`,
			`UPDATE companies SET is_active=false WHERE id=$1`,
		} {
			if _, err := tx.ExecContext(ctx, stmt, companyID); err != nil {
				return err
			}
		}
		return nil
	})
	if cleanupErr != nil {
		t.Fatalf("cleanup failed: %v", cleanupErr)
	}
	for _, table := range []string{"bank_transactions", "financial_workspace_states", "transaction_allocations"} {
		var remaining int
		if err := db.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE company_id=$1`, table), companyID).Scan(&remaining); err != nil {
			t.Fatalf("cleanup verification failed for %s: %v", table, err)
		}
		if remaining != 0 {
			t.Fatalf("cleanup left %d row(s) in %s", remaining, table)
		}
	}
	var voucherCount int
	var retainedStatus string
	if err := db.QueryRow(`SELECT COUNT(*), MIN(status) FROM journal_vouchers WHERE company_id=$1 AND external_key LIKE 'HY:%'`, companyID).Scan(&voucherCount, &retainedStatus); err != nil {
		t.Fatalf("cleanup verification failed for vouchers: %v", err)
	}
	if voucherCount != 1 || retainedStatus != "Posted" {
		t.Fatalf("immutable audit voucher was expected to remain posted: count=%d status=%s", voucherCount, retainedStatus)
	}
	var companyActive bool
	if err := db.QueryRow(`SELECT is_active FROM companies WHERE id=$1`, companyID).Scan(&companyActive); err != nil {
		t.Fatalf("cleanup verification failed for company: %v", err)
	}
	if companyActive {
		t.Fatal("test company was expected to be deactivated after cleanup")
	}
}

func startOperationalServer(t *testing.T, goBin, operationalDir string) (string, *http.Client, string) {
	t.Helper()
	workDir, err := os.MkdirTemp("", "operational-e2e-*")
	if err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(workDir, "operational-e2e.db")
	binPath := filepath.Join(workDir, "operational-e2e-server")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}
	build := exec.Command(goBin, "build", "-o", binPath, "./cmd/server")
	build.Dir = operationalDir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build operational server: %v\n%s", err, tailBytes(out))
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	cmd := exec.Command(binPath)
	cmd.Dir = operationalDir
	cmd.Env = append(os.Environ(),
		"OPERATIONAL_DB_DRIVER=sqlite",
		"OPERATIONAL_DB="+dbPath,
		"OPERATIONAL_ADMIN_PASSWORD=e2e-admin-pass",
		fmt.Sprintf("PORT=%d", port),
	)
	var serverLog bytes.Buffer
	cmd.Stdout = &serverLog
	cmd.Stderr = &serverLog
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
		// Best effort: on Windows the financial bridge in this test process
		// keeps the sqlite handle open, so the directory may survive removal.
		for attempt := 0; attempt < 3; attempt++ {
			if err := os.RemoveAll(workDir); err == nil {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
	})

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: 10 * time.Second}
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		resp, err := client.Post(base+"/api/login", "application/json", bytes.NewBufferString(`{"username":"admin","password":"e2e-admin-pass"}`))
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return base, client, dbPath
			}
		}
		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			t.Fatalf("operational server exited early:\n%s", tailBytes(serverLog.Bytes()))
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("operational server did not become ready:\n%s", tailBytes(serverLog.Bytes()))
	return base, client, dbPath
}

func loginOperationalAdmin(t *testing.T, client *http.Client, base string) []*http.Cookie {
	t.Helper()
	resp, err := client.Post(base+"/api/login", "application/json", bytes.NewBufferString(`{"username":"admin","password":"e2e-admin-pass"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("operational admin login failed: %s", resp.Status)
	}
	if len(resp.Cookies()) == 0 {
		t.Fatal("operational session cookie was not issued")
	}
	return resp.Cookies()
}

func createOperationalExpense(t *testing.T, client *http.Client, base string, cookies []*http.Cookie) (int64, float64) {
	t.Helper()
	const (
		expenseTitle    = "هزینه عملیاتی E2E"
		operatorName    = "اپراتور E2E"
		expenseAmount   = 250000.0
		expenseDescript = "خرید نخ اضافه E2E"
	)
	seedLookup(t, client, base, cookies, "costs", expenseTitle)
	seedLookup(t, client, base, cookies, "operators", operatorName)
	costID, operatorID := lookupIDs(t, client, base, cookies, expenseTitle, operatorName)
	opRequest(t, client, http.MethodPost, base+"/api/expenses", map[string]any{
		"hazine_id": costID, "operator_id": operatorID,
		"mablagh": expenseAmount, "description": expenseDescript, "sanad_no": "E2E-1",
	}, cookies, nil)

	var expenses []map[string]any
	opRequest(t, client, http.MethodGet, base+"/api/expenses", nil, cookies, &expenses)
	for _, row := range expenses {
		id, ok := row["id"].(float64)
		if !ok {
			continue
		}
		amount, _ := row["mablagh"].(float64)
		title, _ := row["onvan_hazine"].(string)
		if int64(id) > 0 && amount == expenseAmount && title == expenseTitle {
			return int64(id), amount
		}
	}
	t.Fatal("created operational expense was not returned by the operational API")
	return 0, 0
}

func seedLookup(t *testing.T, client *http.Client, base string, cookies []*http.Cookie, kind, name string) {
	t.Helper()
	opRequest(t, client, http.MethodPost, fmt.Sprintf("%s/api/basic/%s", base, kind), map[string]any{"name": name}, cookies, nil)
}

func lookupIDs(t *testing.T, client *http.Client, base string, cookies []*http.Cookie, costName, operatorName string) (int64, int64) {
	t.Helper()
	var costs, operators []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	opRequest(t, client, http.MethodGet, base+"/api/basic/costs", nil, cookies, &costs)
	opRequest(t, client, http.MethodGet, base+"/api/basic/operators", nil, cookies, &operators)
	find := func(rows []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}, name string) int64 {
		for _, row := range rows {
			if row.Name == name {
				return row.ID
			}
		}
		t.Fatalf("lookup %s was not found", name)
		return 0
	}
	return find(costs, costName), find(operators, operatorName)
}

func opRequest(t *testing.T, client *http.Client, method, url string, body any, cookies []*http.Cookie, out any) {
	t.Helper()
	requestOps(t, client, method, url, body, cookies, out, http.StatusOK)
}

func requestOps(t *testing.T, client *http.Client, method, url string, body any, cookies []*http.Cookie, out any, wantStatus int) {
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
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		var dump bytes.Buffer
		_, _ = dump.ReadFrom(resp.Body)
		t.Fatalf("%s %s returned %s: %s", method, url, resp.Status, dump.String())
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("%s %s returned invalid JSON: %v", method, url, err)
		}
	}
}

func postJSONStatus(t *testing.T, base string, companyID int64, path string, body map[string]any, out any, wantStatus int) {
	t.Helper()
	payload, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, base+path, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	applyDevTenantHeaders(req, companyID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		var dump bytes.Buffer
		_, _ = dump.ReadFrom(resp.Body)
		t.Fatalf("POST %s returned %s: %s", path, resp.Status, dump.String())
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("POST %s returned invalid JSON: %v", path, err)
	}
}

func tailBytes(data []byte) string {
	const max = 2000
	if len(data) > max {
		return string(data[len(data)-max:])
	}
	return string(data)
}
