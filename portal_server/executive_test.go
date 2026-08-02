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
		AllowWeaving:       true,
		WeavingTenantID:    "11111111-1111-4111-8111-111111111111",
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

func TestExecutiveAllowedRequiresManagerAndAnyPurchasedModule(t *testing.T) {
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
	record.AllowWeaving = false
	if !executiveAllowed(record) {
		t.Fatal("manager with a financial-only purchase should be allowed")
	}
	record.AllowFinancial = false
	if executiveAllowed(record) {
		t.Fatal("manager without any purchased module should not be allowed")
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
	for _, expected := range []string{"عملیات", "مالی", "راندمان سالن", "رتبه‌بندی بافنده‌ها", "manifest.webmanifest"} {
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
		if r.Header.Get("X-Viora-Tenant-Id") != "11111111-1111-4111-8111-111111111111" {
			http.Error(w, "missing tenant", http.StatusForbidden)
			return
		}
		respondJSON(w, http.StatusOK, map[string]any{
			"schemaVersion": 1,
			"module":        "textie-weaving-efficiency",
			"basis":         "latest-confirmed-complete-shift",
			"generatedAt":   "2026-08-01T08:30:00Z",
			"hall": map[string]any{
				"efficiency":         94.2,
				"activeMachineCount": 1,
				"totalStops":         3,
			},
			"machines": []map[string]any{{"number": 1, "weaverName": "بافنده نمونه", "efficiency": 94.2, "meters": nil, "stops": 3, "status": "watch"}},
			"weavers":  []map[string]any{{"name": "بافنده نمونه", "machineNumbers": []int{1}, "efficiency": 94.2, "performanceScore": 91, "rank": 1}},
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
	if !strings.Contains(rec.Body.String(), "specialized") || !strings.Contains(rec.Body.String(), `"hallEfficiency":94.2`) {
		t.Fatalf("unexpected hall response: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"machine":1`) || !strings.Contains(rec.Body.String(), `"weaver":"بافنده نمونه"`) {
		t.Fatalf("nested monitor payload was not normalized: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"meters":null`) {
		t.Fatalf("unconfirmed meter value must remain empty: %s", rec.Body.String())
	}
}

func TestNormalizeExecutiveHallPayloadSupportsLegacyFlatContract(t *testing.T) {
	t.Parallel()

	result, err := normalizeExecutiveHallPayload(map[string]any{
		"hallEfficiency": 88.5,
		"activeMachines": 2,
		"totalStops":     7,
		"machines": []any{
			map[string]any{"machine": "4", "weaver": "کاربر", "efficiency": 88.5, "meters": 1200, "stops": 7, "status": "good"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["hallEfficiency"] != 88.5 || result["activeMachines"] != 2 || result["totalStops"] != float64(7) {
		t.Fatalf("legacy summary was not preserved: %#v", result)
	}
}

func TestNormalizeExecutiveHallPayloadRejectsNonObject(t *testing.T) {
	t.Parallel()

	if _, err := normalizeExecutiveHallPayload([]any{"invalid"}); err == nil {
		t.Fatal("non-object hall payload must be rejected")
	}
	if _, err := normalizeExecutiveHallPayload(map[string]any{"error": "upstream failure"}); err == nil {
		t.Fatal("object without a supported hall summary must be rejected")
	}
}

func TestNormalizeExecutiveHallPayloadPreservesExplicitShutdown(t *testing.T) {
	t.Parallel()

	result, err := normalizeExecutiveHallPayload(map[string]any{
		"hall": map[string]any{"efficiency": 0, "activeMachineCount": 0, "totalStops": 0},
		"machines": []map[string]any{
			{"number": 1, "efficiency": 90, "status": "stopped"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["hallEfficiency"] != float64(0) || result["activeMachines"] != 0 {
		t.Fatalf("explicit shutdown summary was overwritten: %#v", result)
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
