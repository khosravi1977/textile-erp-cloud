package integration_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"

	"github.com/erpsystem/textile-erp/internal/presentation/router"
)

func TestTenantScopedCostAPIRejectsCrossCompanyLeakage(t *testing.T) {
	t.Setenv("ALLOW_DEV_AUTH", "true")
	t.Setenv("CACHE_DISABLED", "true")

	server := httptest.NewServer(router.SetupRouter())
	defer server.Close()

	postJSON(t, server.URL+"/api/costs", 1, map[string]any{
		"category":    "Labor",
		"description": "company 1 private cost",
		"amount":      1000,
	})
	postJSON(t, server.URL+"/api/costs", 2, map[string]any{
		"category":    "Labor",
		"description": "company 2 private cost",
		"amount":      2000,
	})

	companyOne := getJSON[map[string]any](t, server.URL+"/api/costs/", 1)
	companyTwo := getJSON[map[string]any](t, server.URL+"/api/costs/", 2)

	if containsCostDescription(companyOne, "company 2 private cost") {
		t.Fatalf("company 2 cost leaked into company 1 response: %#v", companyOne)
	}
	if containsCostDescription(companyTwo, "company 1 private cost") {
		t.Fatalf("company 1 cost leaked into company 2 response: %#v", companyTwo)
	}
}

func TestTenantScopedInventoryAPIRejectsCrossCompanyLeakage(t *testing.T) {
	t.Setenv("ALLOW_DEV_AUTH", "true")
	t.Setenv("CACHE_DISABLED", "true")

	server := httptest.NewServer(router.SetupRouter())
	defer server.Close()

	postJSON(t, server.URL+"/api/inventory/stock-out", 1, map[string]any{
		"ItemID":      1,
		"Qty":         100,
		"ReferenceNo": "company-1-stock-out",
		"Description": "tenant one movement",
		"Warehouse":   "main",
		"CreatedBy":   "test",
	})
	postJSON(t, server.URL+"/api/inventory/stock-in", 2, map[string]any{
		"ItemID":      1,
		"Qty":         250,
		"UnitCost":    150000,
		"ReferenceNo": "company-2-stock-in",
		"Description": "tenant two movement",
		"Warehouse":   "main",
		"CreatedBy":   "test",
	})

	companyOne := getJSON[map[string]any](t, server.URL+"/api/inventory/transactions", 1)
	companyTwo := getJSON[map[string]any](t, server.URL+"/api/inventory/transactions", 2)

	if containsTransactionReference(companyOne, "company-2-stock-in") {
		t.Fatalf("company 2 inventory transaction leaked into company 1 response: %#v", companyOne)
	}
	if containsTransactionReference(companyTwo, "company-1-stock-out") {
		t.Fatalf("company 1 inventory transaction leaked into company 2 response: %#v", companyTwo)
	}
}

func postJSON(t *testing.T, url string, companyID int64, payload map[string]any) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
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
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("POST %s returned %s", url, resp.Status)
	}
}

func getJSON[T any](t *testing.T, url string, companyID int64) T {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	applyDevTenantHeaders(req, companyID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s returned %s", url, resp.Status)
	}
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func applyDevTenantHeaders(req *http.Request, companyID int64) {
	req.Header.Set("X-Dev-Mode", "true")
	req.Header.Set("X-Company-ID", jsonNumber(companyID))
	req.Header.Set("X-User-ID", "1")
	req.Header.Set("X-User-Role", "admin")
}

func jsonNumber(value int64) string {
	return string(strconvAppendInt(nil, value))
}

func strconvAppendInt(dst []byte, value int64) []byte {
	return strconv.AppendInt(dst, value, 10)
}

func containsCostDescription(response map[string]any, description string) bool {
	for _, row := range asRows(response["costs"]) {
		if row["description"] == description {
			return true
		}
	}
	return false
}

func containsTransactionReference(response map[string]any, reference string) bool {
	for _, row := range asRows(response["transactions"]) {
		if row["reference_no"] == reference || row["ReferenceNo"] == reference {
			return true
		}
	}
	return false
}

func asRows(value any) []map[string]any {
	rows, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if mapped, ok := row.(map[string]any); ok {
			out = append(out, mapped)
		}
	}
	return out
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
