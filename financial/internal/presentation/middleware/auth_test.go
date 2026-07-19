package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPortalAuthorizationEnforcesModulePermissions(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-financial-authorization-secret-32-bytes")

	viewer := portalToken(t, "viewer", []string{"dashboard"}, true)
	assertAuthStatus(t, http.MethodGet, "/api/workspace", viewer, http.StatusNoContent)
	assertAuthStatus(t, http.MethodPut, "/api/workspace", viewer, http.StatusForbidden)

	accountant := portalToken(t, "accountant", []string{"incomingInvoices"}, true)
	assertAuthStatus(t, http.MethodGet, "/api/operational/chelle-incoming", accountant, http.StatusNoContent)
	assertAuthStatus(t, http.MethodGet, "/api/operational/expenses", accountant, http.StatusForbidden)

	disabled := portalToken(t, "owner", []string{"dashboard"}, false)
	assertAuthStatus(t, http.MethodGet, "/api/workspace", disabled, http.StatusForbidden)
}

func TestFinancialAuthorizationRejectsMissingToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/workspace", nil)
	rec := httptest.NewRecorder()
	AdminAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func portalToken(t *testing.T, role string, permissions []string, allowFinancial bool) string {
	t.Helper()
	token, err := SignJWT(map[string]any{
		"user_id": 10, "company_id": 20, "role": "customer", "portal_role": role,
		"project_key": "textile-erp", "permissions": permissions, "allow_financial": allowFinancial,
	})
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func assertAuthStatus(t *testing.T, method, path, token string, expected int) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	AdminAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })).ServeHTTP(rec, req)
	if rec.Code != expected {
		t.Fatalf("%s %s: expected %d, got %d: %s", method, path, expected, rec.Code, rec.Body.String())
	}
}
