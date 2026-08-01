package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func executiveTestAccess() projectAccess {
	return projectAccess{
		ID:                 41,
		ProjectKey:         "textile-erp",
		CompanyName:        "بافندگی نمونه",
		ContactName:        "مدیر نمونه",
		Username:           "executive_manager",
		FinancialCompanyID: 7,
		AccessRole:         "manager",
		AllowFinancial:     true,
		AllowOperational:   true,
		ExpiresAt:          time.Now().Add(24 * time.Hour),
		AccessToken:        "executive-access-token",
		PasswordHash:       "$2a$10$abcdefghijklmnopqrstuv1234567890123456789012345678901",
		IsActive:           true,
		CreatedAt:          time.Now(),
	}
}

func writeExecutiveTestAccess(t *testing.T, record projectAccess) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "portal-access.db")
	payload, err := json.Marshal([]projectAccess{record})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestExecutiveAllowedRequiresManagerAndBothModules(t *testing.T) {
	t.Parallel()

	record := executiveTestAccess()
	if !executiveAllowed(record) {
		t.Fatal("manager with both module permissions should be allowed")
	}
	record.AccessRole = "accountant"
	if executiveAllowed(record) {
		t.Fatal("accountant should not be allowed into the executive center")
	}
	record.AccessRole = "manager"
	record.AllowOperational = false
	if executiveAllowed(record) {
		t.Fatal("manager without operational access should not be allowed")
	}
}

func TestExecutiveAppRequiresPortalSession(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/executive/", nil)
	rec := httptest.NewRecorder()
	(&portalApp{}).executiveApp(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected login redirect, got %d", rec.Code)
	}
	if rec.Header().Get("Location") != "/login?next=%2Fexecutive%2F" {
		t.Fatalf("unexpected redirect: %s", rec.Header().Get("Location"))
	}
}

func TestSafePortalNextAllowsExecutiveAppOnlyOnLocalPath(t *testing.T) {
	t.Parallel()

	if got := safePortalNext("/executive/?tab=finance"); got != "/executive/?tab=finance" {
		t.Fatalf("expected executive path to be preserved, got %q", got)
	}
	for _, unsafe := range []string{
		"//attacker.example/executive/",
		"https://attacker.example/executive/",
		"/executive\\..\\admin",
	} {
		if got := safePortalNext(unsafe); got != "" {
			t.Fatalf("expected unsafe next path %q to be rejected, got %q", unsafe, got)
		}
	}
}

func TestExecutiveAppServesThreeTabPWA(t *testing.T) {
	t.Parallel()

	record := executiveTestAccess()
	app := &portalApp{accessFile: writeExecutiveTestAccess(t, record)}
	req := httptest.NewRequest(http.MethodGet, "/executive/", nil)
	req.AddCookie(&http.Cookie{Name: accessCookieName, Value: record.AccessToken})
	rec := httptest.NewRecorder()
	app.executiveApp(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, expected := range []string{"عملیات", "مالی", "راندمان سالن", "manifest.webmanifest"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("executive PWA is missing %q", expected)
		}
	}
	if rec.Header().Get("Cache-Control") != "no-store, max-age=0" {
		t.Fatalf("executive page must not be cached: %s", rec.Header().Get("Cache-Control"))
	}
}

func TestExecutiveSessionBootstrapsFinancialAndOperationalAccess(t *testing.T) {
	t.Parallel()

	operational := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/portal/session" || r.Header.Get("X-Operational-Portal-Secret") != "operational-test-secret" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "operational_session", Value: "executive-operational-session", Path: "/", HttpOnly: true})
		respondJSON(w, http.StatusOK, map[string]any{"success": true})
	}))
	defer operational.Close()

	record := executiveTestAccess()
	app := &portalApp{
		accessFile:               writeExecutiveTestAccess(t, record),
		financialJWTKey:          "financial-test-secret",
		operationalAPI:           operational.URL,
		operationalSessionSecret: "operational-test-secret",
	}
	req := httptest.NewRequest(http.MethodGet, "/api/portal/executive-session", nil)
	req.AddCookie(&http.Cookie{Name: accessCookieName, Value: record.AccessToken})
	rec := httptest.NewRecorder()
	app.executiveSession(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var response struct {
		FinancialReady   bool   `json:"financialReady"`
		OperationalReady bool   `json:"operationalReady"`
		FinancialToken   string `json:"financialToken"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.FinancialReady || !response.OperationalReady || response.FinancialToken == "" {
		t.Fatalf("unexpected bootstrap response: %#v", response)
	}
	claims, err := verifyJWTForTest(response.FinancialToken, "financial-test-secret")
	if err != nil || valueNumberForTest(claims["company_id"]) != 7 {
		t.Fatalf("invalid financial token: claims=%#v err=%v", claims, err)
	}
	foundOperationalCookie := false
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == "operational_session" && cookie.Value == "executive-operational-session" {
			foundOperationalCookie = true
		}
	}
	if !foundOperationalCookie {
		t.Fatal("expected operational session cookie")
	}
}

func TestExecutiveHallProxyKeepsServiceTokenPrivate(t *testing.T) {
	t.Parallel()

	const privateToken = "private-hall-service-token"
	monitor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+privateToken {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		respondJSON(w, http.StatusOK, map[string]any{
			"hallEfficiency": 94.2,
			"machines":       []map[string]any{{"machine": 1, "weaver": "بافنده نمونه"}},
		})
	}))
	defer monitor.Close()

	record := executiveTestAccess()
	app := &portalApp{
		accessFile:          writeExecutiveTestAccess(t, record),
		weavingMonitorURL:   monitor.URL,
		weavingMonitorToken: privateToken,
	}
	req := httptest.NewRequest(http.MethodGet, "/api/portal/executive-hall", nil)
	req.AddCookie(&http.Cookie{Name: accessCookieName, Value: record.AccessToken})
	rec := httptest.NewRecorder()
	app.executiveHallSummary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), privateToken) {
		t.Fatal("service token leaked to the browser response")
	}
	if !strings.Contains(rec.Body.String(), "specialized") {
		t.Fatalf("unexpected hall response: %s", rec.Body.String())
	}
}

func valueNumberForTest(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case json.Number:
		result, _ := typed.Int64()
		return result
	default:
		return 0
	}
}
