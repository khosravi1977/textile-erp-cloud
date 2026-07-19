package integration_test

import (
	"bytes"
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
	docOne := putWorkspace(t, server.URL, companyOne, 0, map[string]any{"invoices": []any{map[string]any{"number": "ONE", "total": 100.0}}, "accounts": []any{}})
	putWorkspace(t, server.URL, companyTwo, 0, map[string]any{"invoices": []any{map[string]any{"number": "TWO", "total": 200.0}}, "accounts": []any{}})

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
