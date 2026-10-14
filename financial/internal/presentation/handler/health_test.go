package handler

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/erpsystem/textile-erp/internal/infrastructure/persistence/postgres"
)

func TestHealthRejectsUnavailableDatabase(t *testing.T) {
	previousDB := postgres.DB
	t.Cleanup(func() { postgres.DB = previousDB })
	closed, err := sql.Open("postgres", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	for _, database := range []*sql.DB{nil, closed} {
		postgres.DB = database
		request := httptest.NewRequest(http.MethodGet, "/health", nil)
		response := httptest.NewRecorder()
		(&APIHandler{}).HealthCheck(response, request)
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("unavailable database reported healthy: status=%d", response.Code)
		}
	}
}
