package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestCustomerLoginDoesNotExposeDefaultCredentials(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	(&portalApp{}).renderCustomerLogin(recorder, "", "")
	body := recorder.Body.String()
	if strings.Contains(body, "admin123") || strings.Contains(body, "admin /") {
		t.Fatal("customer login page must not expose default credentials")
	}
	if !strings.Contains(body, "نام کاربری و رمز عبور اختصاصی") {
		t.Fatal("customer login page should direct users to their assigned credentials")
	}
}

func TestCreateAccessPersistsPasswordHash(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	accessFile := filepath.Join(tempDir, "portal-access.db")
	if err := os.WriteFile(accessFile, []byte("[]\n"), 0o644); err != nil {
		t.Fatalf("write access file: %v", err)
	}

	app := &portalApp{
		accessFile:    accessFile,
		sessionSecret: "test-secret",
	}

	_, err := app.createAccess("textile-erp", "شرکت نمونه", "تست", "trial_user", "Secret123!", 7, time.Now().Add(48*time.Hour), "")
	if err != nil {
		t.Fatalf("create access: %v", err)
	}

	items, err := readAccesses(accessFile)
	if err != nil {
		t.Fatalf("read accesses: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 access, got %d", len(items))
	}
	if items[0].PasswordHash == "" {
		t.Fatal("expected password_hash to be persisted")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(items[0].PasswordHash), []byte("Secret123!")); err != nil {
		t.Fatalf("persisted password hash does not match: %v", err)
	}
}

func TestAccessUsernameTakenScopesTextileByTenant(t *testing.T) {
	t.Parallel()

	items := []projectAccess{
		{
			ID:                 1,
			ProjectKey:         "textile-erp",
			CompanyName:        "Company A",
			FinancialCompanyID: 1,
			Username:           "shared_manager",
		},
	}

	if accessUsernameTaken(items, projectAccess{
		ProjectKey:         "textile-erp",
		CompanyName:        "Company B",
		FinancialCompanyID: 2,
	}, "shared_manager", 0) {
		t.Fatal("expected username reuse across different textile tenants to be allowed")
	}

	if !accessUsernameTaken(items, projectAccess{
		ProjectKey:         "textile-erp",
		CompanyName:        "Company A",
		FinancialCompanyID: 1,
	}, "shared_manager", 0) {
		t.Fatal("expected username collision inside the same textile tenant")
	}
}

func TestVerifyAccessPasswordBackfillsLegacyHash(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	accessFile := filepath.Join(tempDir, "portal-access.db")
	app := &portalApp{
		accessFile:    accessFile,
		sessionSecret: "legacy-secret",
	}

	enc, err := app.encryptPassword("Legacy123!")
	if err != nil {
		t.Fatalf("encrypt password: %v", err)
	}

	payload := `[
  {
    "id": 1,
    "project_key": "textile-erp",
    "company_name": "legacy",
    "contact_name": "legacy",
    "username": "legacy_user",
    "expires_at": "2030-01-01T00:00:00Z",
    "access_token": "legacy-token",
    "password_enc": "` + enc + `",
    "notes": "",
    "is_active": true,
    "created_at": "2026-07-05T00:00:00Z",
    "last_used_at": "0001-01-01T00:00:00Z"
  }
]
`
	if err := os.WriteFile(accessFile, []byte(payload), 0o644); err != nil {
		t.Fatalf("write legacy access file: %v", err)
	}

	if err := app.verifyAccessPassword("legacy-token", "Legacy123!"); err != nil {
		t.Fatalf("verify legacy password: %v", err)
	}

	items, err := readAccesses(accessFile)
	if err != nil {
		t.Fatalf("read accesses: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 access, got %d", len(items))
	}
	if items[0].PasswordHash == "" {
		t.Fatal("expected missing legacy hash to be backfilled")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(items[0].PasswordHash), []byte("Legacy123!")); err != nil {
		t.Fatalf("backfilled password hash does not match: %v", err)
	}
}

func TestCustomerLoginUsesStableUsernameAndPassword(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	accessFile := filepath.Join(tempDir, "portal-access.db")
	if err := os.WriteFile(accessFile, []byte("[]\n"), 0o644); err != nil {
		t.Fatalf("write access file: %v", err)
	}
	app := &portalApp{
		accessFile:    accessFile,
		publicBase:    "https://textile.example.com",
		sessionSecret: "login-secret",
	}
	access, err := app.createAccess(
		"textile-erp",
		"Customer",
		"Owner",
		"customer_user",
		"Customer123!",
		7,
		time.Now().Add(48*time.Hour),
		"",
	)
	if err != nil {
		t.Fatalf("create access: %v", err)
	}

	form := url.Values{"username": {"customer_user"}, "password": {"Customer123!"}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	app.customerLogin(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", rec.Code, rec.Body.String())
	}
	if location := rec.Header().Get("Location"); location != "/" {
		t.Fatalf("unexpected redirect: %s", location)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != accessCookieName || cookies[0].Value != access.AccessToken {
		t.Fatalf("expected portal access cookie, got %#v", cookies)
	}
}

func TestCustomerLoginRejectsWrongPassword(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	accessFile := filepath.Join(tempDir, "portal-access.db")
	if err := os.WriteFile(accessFile, []byte("[]\n"), 0o644); err != nil {
		t.Fatalf("write access file: %v", err)
	}
	app := &portalApp{accessFile: accessFile, sessionSecret: "login-secret"}
	if _, err := app.createAccess("textile-erp", "Customer", "Owner", "customer_user", "Customer123!", 7, time.Now().Add(48*time.Hour), ""); err != nil {
		t.Fatalf("create access: %v", err)
	}

	form := url.Values{"username": {"customer_user"}, "password": {"wrong-password"}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	app.customerLogin(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Fatal("wrong password must not create an access cookie")
	}
}

func TestModuleLoginCreatesOnlyRequestedModuleSession(t *testing.T) {
	t.Parallel()

	accessFile := filepath.Join(t.TempDir(), "portal-access.db")
	if err := os.WriteFile(accessFile, []byte("[]\n"), 0o600); err != nil {
		t.Fatalf("create access store: %v", err)
	}
	app := &portalApp{accessFile: accessFile, sessionSecret: "module-login-secret", publicBase: "http://127.0.0.1:28080"}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("Finance123!"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	access := projectAccess{ID: 1, ProjectKey: "textile-erp", CompanyName: "Test Company", ContactName: "Financial User", Username: "finance_user", FinancialCompanyID: 2, AccessRole: "accountant", AllowFinancial: true, AllowOperational: false, ExpiresAt: time.Now().Add(48 * time.Hour), AccessToken: "financial-user-token", PasswordHash: string(passwordHash), IsActive: true, CreatedAt: time.Now()}
	payload, _ := json.Marshal([]projectAccess{access})
	if err := os.WriteFile(accessFile, payload, 0o600); err != nil {
		t.Fatal(err)
	}

	form := url.Values{
		"module":   {"financial"},
		"next":     {"/financial/"},
		"username": {"finance_user"},
		"password": {"Finance123!"},
	}
	req := httptest.NewRequest(http.MethodPost, "/module-login?module=financial", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	app.moduleLogin(rec, req)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/financial/" {
		t.Fatalf("unexpected module login response: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	cookies := map[string]*http.Cookie{}
	for _, cookie := range rec.Result().Cookies() {
		cookies[cookie.Name] = cookie
	}
	if cookies[accessCookieName] == nil || cookies[accessCookieName].Value != access.AccessToken {
		t.Fatal("expected portal identity cookie")
	}
	if cookies[financialAccessCookieName] == nil || cookies[financialAccessCookieName].Value != access.AccessToken {
		t.Fatal("expected financial module cookie")
	}
	if cookies[operationalAccessCookieName] != nil {
		t.Fatal("financial login must not create an operational module cookie")
	}
}

func TestModuleLoginRejectsUserWithoutModulePermission(t *testing.T) {
	t.Parallel()

	accessFile := filepath.Join(t.TempDir(), "portal-access.db")
	if err := os.WriteFile(accessFile, []byte("[]\n"), 0o600); err != nil {
		t.Fatalf("create access store: %v", err)
	}
	app := &portalApp{accessFile: accessFile, sessionSecret: "module-denied-secret", publicBase: "http://127.0.0.1:28080"}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("Finance123!"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	access := projectAccess{ID: 1, ProjectKey: "textile-erp", CompanyName: "Test Company", ContactName: "Financial User", Username: "finance_only", FinancialCompanyID: 2, AccessRole: "accountant", AllowFinancial: true, AllowOperational: false, ExpiresAt: time.Now().Add(48 * time.Hour), AccessToken: "financial-only-token", PasswordHash: string(passwordHash), IsActive: true, CreatedAt: time.Now()}
	payload, _ := json.Marshal([]projectAccess{access})
	if err := os.WriteFile(accessFile, payload, 0o600); err != nil {
		t.Fatal(err)
	}

	form := url.Values{"module": {"operational"}, "username": {"finance_only"}, "password": {"Finance123!"}}
	req := httptest.NewRequest(http.MethodPost, "/module-login?module=operational", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	app.moduleLogin(rec, req)

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "اجازه ورود") {
		t.Fatalf("expected a Persian permission error, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Fatal("denied module login must not create cookies")
	}
}

func TestModuleRouteCannotUseGeneralPortalCookie(t *testing.T) {
	t.Parallel()

	accessFile := filepath.Join(t.TempDir(), "portal-access.db")
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal([]projectAccess{{ID: 1, ProjectKey: "textile-erp", Username: "user", AccessToken: "portal-only", PasswordHash: string(passwordHash), AllowFinancial: true, IsActive: true, ExpiresAt: time.Now().Add(time.Hour)}})
	if err := os.WriteFile(accessFile, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	app := &portalApp{accessFile: accessFile}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler := app.requireModuleAccess("financial", next)

	req := httptest.NewRequest(http.MethodGet, "/financial/", nil)
	req.AddCookie(&http.Cookie{Name: accessCookieName, Value: "portal-only"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/module-login?module=financial") {
		t.Fatalf("portal-only cookie bypassed module gate: %d %s", rec.Code, rec.Header().Get("Location"))
	}

	req = httptest.NewRequest(http.MethodGet, "/financial/", nil)
	req.AddCookie(&http.Cookie{Name: financialAccessCookieName, Value: "portal-only"})
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("valid financial module cookie was rejected: %d", rec.Code)
	}
}

func TestModuleLoginPromotesValidPortalSessionWithoutSecondPassword(t *testing.T) {
	t.Parallel()

	accessFile := filepath.Join(t.TempDir(), "portal-access.db")
	payload, _ := json.Marshal([]projectAccess{{
		ID:               1,
		ProjectKey:       "textile-erp",
		Username:         "portal-user",
		AccessToken:      "portal-sso-token",
		PasswordHash:     "stored-password-hash",
		AllowFinancial:   true,
		AllowOperational: false,
		IsActive:         true,
		ExpiresAt:        time.Now().Add(time.Hour),
	}})
	if err := os.WriteFile(accessFile, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	app := &portalApp{accessFile: accessFile}

	req := httptest.NewRequest(http.MethodGet, "/module-login?module=financial", nil)
	req.AddCookie(&http.Cookie{Name: accessCookieName, Value: "portal-sso-token"})
	rec := httptest.NewRecorder()
	app.moduleLogin(rec, req)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/financial/" {
		t.Fatalf("expected automatic module entry, got %d %s", rec.Code, rec.Header().Get("Location"))
	}
	cookies := map[string]*http.Cookie{}
	for _, cookie := range rec.Result().Cookies() {
		cookies[cookie.Name] = cookie
	}
	if cookies[financialAccessCookieName] == nil || cookies[financialAccessCookieName].Value != "portal-sso-token" {
		t.Fatalf("expected financial module cookie, got %#v", cookies)
	}
	if cookies[operationalAccessCookieName] != nil {
		t.Fatal("financial SSO must not grant the operational module")
	}
}

func TestModuleSwitchUserClearsCentralAndModuleSessions(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/module-logout?module=financial&login=1", nil)
	rec := httptest.NewRecorder()
	(&portalApp{}).moduleLogout(rec, req)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/module-login?module=financial" {
		t.Fatalf("unexpected switch-user response: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	cookies := map[string]*http.Cookie{}
	for _, cookie := range rec.Result().Cookies() {
		cookies[cookie.Name] = cookie
	}
	for _, name := range []string{accessCookieName, financialAccessCookieName, "operational_session"} {
		cookie := cookies[name]
		if cookie == nil || cookie.MaxAge != -1 || cookie.Value != "" {
			t.Fatalf("expected %s to be cleared, got %#v", name, cookie)
		}
	}
}

func TestOwnerAlwaysReceivesEveryFinancialPermission(t *testing.T) {
	t.Parallel()

	got := effectivePermissions(projectAccess{
		ProjectKey:  "textile-erp",
		AccessRole:  "owner",
		Permissions: []string{"dashboard"},
	})
	allowed := make(map[string]bool, len(got))
	for _, permission := range got {
		allowed[permission] = true
	}
	for _, permission := range financialPermissionCatalog {
		if !allowed[permission] {
			t.Fatalf("owner is missing financial permission %q: %#v", permission, got)
		}
	}
	if len(got) != len(financialPermissionCatalog) {
		t.Fatalf("owner permissions must match the catalog: got=%d want=%d", len(got), len(financialPermissionCatalog))
	}
}

func TestOperationalPortalMenusRespectCentralRoleAndExcludeUserManagement(t *testing.T) {
	t.Parallel()

	manager := projectAccess{ProjectKey: "textile-erp", AccessRole: "manager"}
	menus := operationalPortalMenusForKeys(operationalMenuKeys(manager))
	keys := map[string]bool{}
	for _, menu := range menus {
		key, _ := menu["menu_key"].(string)
		keys[key] = true
	}
	if !keys["expenses"] || !keys["machinery-services"] || !keys["spare-parts"] {
		t.Fatalf("manager operational menus are incomplete: %#v", keys)
	}
	if keys["database"] || keys["users"] {
		t.Fatalf("manager received a central-only or owner-only menu: %#v", keys)
	}

	for _, menu := range operationalPortalMenus() {
		if menu["menu_key"] == "users" {
			t.Fatal("portal users must be managed only from /team")
		}
	}
}

func TestOneTimeLaunchTicketCreatesPortalSessionAndCannotBeReused(t *testing.T) {
	t.Parallel()

	accessFile := filepath.Join(t.TempDir(), "portal-access.db")
	payload, _ := json.Marshal([]projectAccess{{
		ID:             1,
		ProjectKey:     "textile-erp",
		Username:       "launch-user",
		AccessToken:    "launch-access-token",
		PasswordHash:   "stored-password-hash",
		AllowFinancial: true,
		IsActive:       true,
		ExpiresAt:      time.Now().Add(time.Hour),
	}})
	if err := os.WriteFile(accessFile, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	app := &portalApp{
		accessFile:    accessFile,
		sessionSecret: "launch-session-secret",
		launchTickets: make(map[string]launchTicket),
	}

	body := strings.NewReader(`{"accessToken":"launch-access-token"}`)
	createReq := httptest.NewRequest(http.MethodPost, "/admin/api/launch-ticket", body)
	createReq.Header.Set("Content-Type", "application/json")
	createReq.AddCookie(&http.Cookie{
		Name:  adminCookieName,
		Value: app.signAdminSession(time.Now().Add(time.Hour)),
	})
	createRec := httptest.NewRecorder()
	app.adminLaunchTicket(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected ticket creation, got %d: %s", createRec.Code, createRec.Body.String())
	}
	var created struct {
		Ticket string `json:"ticket"`
	}
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(created.Ticket) == "" {
		t.Fatal("launch ticket was not returned")
	}

	launchReq := httptest.NewRequest(http.MethodGet, "/launch/"+created.Ticket, nil)
	launchRec := httptest.NewRecorder()
	app.launchEntry(launchRec, launchReq)
	if launchRec.Code != http.StatusSeeOther || launchRec.Header().Get("Location") != "/financial/" {
		t.Fatalf("expected portal redirect, got %d %s", launchRec.Code, launchRec.Header().Get("Location"))
	}
	var accessCookie *http.Cookie
	for _, cookie := range launchRec.Result().Cookies() {
		if cookie.Name == accessCookieName {
			accessCookie = cookie
		}
	}
	if accessCookie == nil || accessCookie.Value != "launch-access-token" {
		t.Fatalf("expected portal access cookie, got %#v", launchRec.Result().Cookies())
	}

	reuseReq := httptest.NewRequest(http.MethodGet, "/launch/"+created.Ticket, nil)
	reuseRec := httptest.NewRecorder()
	app.launchEntry(reuseRec, reuseReq)
	if reuseRec.Code != http.StatusSeeOther || reuseRec.Header().Get("Location") != "/login" {
		t.Fatalf("reused launch ticket was accepted: %d %s", reuseRec.Code, reuseRec.Header().Get("Location"))
	}
}

func TestLocalAdminLoginCreatesOwnerWorkspace(t *testing.T) {
	tempDir := t.TempDir()
	accessFile := filepath.Join(tempDir, "portal-access.db")
	if err := os.WriteFile(accessFile, []byte("[]\n"), 0o600); err != nil {
		t.Fatalf("create access store: %v", err)
	}
	provision := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/portal/provision" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("X-Operational-Portal-Secret") != "local-operational-secret" {
			http.Error(w, "bad secret", http.StatusUnauthorized)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode provision payload: %v", err)
		}
		if payload["username"] != "admin" || int64(payload["company_id"].(float64)) != 2 {
			t.Fatalf("unexpected local owner provision payload: %#v", payload)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "company_id": 2})
	}))
	defer provision.Close()

	app := &portalApp{
		accessFile:               accessFile,
		publicBase:               "http://127.0.0.1:28080",
		operationalAPI:           provision.URL,
		operationalSessionSecret: "local-operational-secret",
		adminUsername:            "admin",
		adminPassword:            "admin123",
		sessionSecret:            "local-session-secret-for-tests",
		localMode:                true,
		localCompanyID:           2,
		localCompanyName:         "پرگل",
	}
	form := url.Values{"username": {"admin"}, "password": {"admin123"}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	app.customerLogin(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected local admin redirect, got %d: %s", rec.Code, rec.Body.String())
	}
	if location := rec.Header().Get("Location"); location != "/" {
		t.Fatalf("unexpected local admin target: %s", location)
	}
	var accessCookie *http.Cookie
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == accessCookieName {
			accessCookie = cookie
		}
	}
	if accessCookie == nil {
		t.Fatal("local admin login must create an access cookie")
	}
	items, err := readAccesses(accessFile)
	if err != nil {
		t.Fatalf("read local owner access: %v", err)
	}
	if len(items) != 1 || items[0].Username != "admin" || items[0].FinancialCompanyID != 2 {
		t.Fatalf("unexpected local owner access: %#v", items)
	}
	if !items[0].AllowFinancial || !items[0].AllowOperational || !effectiveCanManageTeam(items[0]) || items[0].MustChangePassword {
		t.Fatalf("local owner permissions are incomplete: %#v", items[0])
	}
	if err := bcrypt.CompareHashAndPassword([]byte(items[0].PasswordHash), []byte("admin123")); err != nil {
		t.Fatalf("local owner password is not admin123: %v", err)
	}
}

func TestPortalFinancialSessionReturnsJWT(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	accessFile := filepath.Join(tempDir, "portal-access.db")
	payload := `[
  {
    "id": 4,
    "project_key": "textile-erp",
    "company_name": "ماهان بافت",
    "contact_name": "test",
    "username": "MAJID_1988",
    "financial_company_id": 7,
    "allow_financial": true,
    "allow_operational": true,
    "expires_at": "2030-08-04T20:29:00Z",
    "access_token": "session-token",
    "password_hash": "$2a$10$abcdefghijklmnopqrstuv",
    "notes": "",
    "is_active": true,
    "created_at": "2026-07-05T00:00:00Z",
    "last_used_at": "0001-01-01T00:00:00Z"
  }
]` + "\n"
	if err := os.WriteFile(accessFile, []byte(payload), 0o644); err != nil {
		t.Fatalf("write access file: %v", err)
	}

	app := &portalApp{
		accessFile:      accessFile,
		sessionSecret:   "portal-secret",
		financialJWTKey: "shared-jwt-secret",
	}

	req := httptest.NewRequest(http.MethodGet, "/api/portal/financial-session", nil)
	req.AddCookie(&http.Cookie{Name: financialAccessCookieName, Value: "session-token"})
	rec := httptest.NewRecorder()

	app.portalFinancialSession(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Token == "" {
		t.Fatal("expected financial token in response")
	}

	claims, err := verifyJWTForTest(body.Token, "shared-jwt-secret")
	if err != nil {
		t.Fatalf("verify returned jwt: %v", err)
	}
	if got := claims["username"]; got != "MAJID_1988" {
		t.Fatalf("expected username claim, got %#v", got)
	}
	if got := claims["project_key"]; got != "textile-erp" {
		t.Fatalf("expected project_key claim, got %#v", got)
	}
	if got := claims["company_name"]; got != "ماهان بافت" {
		t.Fatalf("expected company_name claim, got %#v", got)
	}
	if got := claims["company_id"]; got != float64(7) {
		t.Fatalf("expected company_id claim, got %#v", got)
	}
}

func TestPortalFinancialSessionRequiresAccessCookie(t *testing.T) {
	t.Parallel()

	app := &portalApp{financialJWTKey: "shared-jwt-secret"}
	req := httptest.NewRequest(http.MethodGet, "/api/portal/financial-session", nil)
	rec := httptest.NewRecorder()

	app.portalFinancialSession(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestPortalOperationalSessionReturnsPortalUser(t *testing.T) {
	t.Parallel()
	operationalServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/portal/session" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("X-Operational-Portal-Secret") != "operational-secret" {
			http.Error(w, "invalid portal secret", http.StatusUnauthorized)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "operational_session", Value: "test-session", Path: "/", HttpOnly: true})
		respondJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"menus":   operationalPortalMenus(),
		})
	}))
	defer operationalServer.Close()

	tempDir := t.TempDir()
	accessFile := filepath.Join(tempDir, "portal-access.db")
	payload := `[
  {
    "id": 4,
    "project_key": "textile-erp",
    "company_name": "ماهان بافت",
    "contact_name": "test",
    "username": "MAJID_1988",
    "financial_company_id": 7,
    "allow_financial": true,
    "allow_operational": true,
    "expires_at": "2030-08-04T20:29:00Z",
    "access_token": "session-token",
    "password_hash": "$2a$10$abcdefghijklmnopqrstuv",
    "notes": "",
    "is_active": true,
    "created_at": "2026-07-05T00:00:00Z",
    "last_used_at": "0001-01-01T00:00:00Z"
  }
]` + "\n"
	if err := os.WriteFile(accessFile, []byte(payload), 0o644); err != nil {
		t.Fatalf("write access file: %v", err)
	}

	app := &portalApp{
		accessFile:                     accessFile,
		allowOperationalCustomerAccess: true,
		operationalAPI:                 operationalServer.URL,
		operationalSessionSecret:       "operational-secret",
	}
	req := httptest.NewRequest(http.MethodGet, "/api/portal/operational-session", nil)
	req.AddCookie(&http.Cookie{Name: operationalAccessCookieName, Value: "session-token"})
	rec := httptest.NewRecorder()

	app.portalOperationalSession(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		User  map[string]any   `json:"user"`
		Menus []map[string]any `json:"menus"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.User["username"] != "MAJID_1988" {
		t.Fatalf("expected username, got %#v", body.User["username"])
	}
	if body.User["role"] != "customer" {
		t.Fatalf("expected customer role, got %#v", body.User["role"])
	}
	if len(body.Menus) == 0 {
		t.Fatal("expected operational menus in response")
	}
}

func TestPortalOperationalSessionDisabledByDefault(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	accessFile := filepath.Join(tempDir, "portal-access.db")
	payload := `[
  {
    "id": 5,
    "project_key": "textile-erp",
    "company_name": "demo",
    "contact_name": "test",
    "username": "demo_user",
    "financial_company_id": 2,
    "allow_financial": true,
    "allow_operational": true,
    "expires_at": "2030-08-04T20:29:00Z",
    "access_token": "disabled-token",
    "password_hash": "$2a$10$abcdefghijklmnopqrstuv",
    "notes": "",
    "is_active": true,
    "created_at": "2026-07-05T00:00:00Z",
    "last_used_at": "0001-01-01T00:00:00Z"
  }
]` + "\n"
	if err := os.WriteFile(accessFile, []byte(payload), 0o644); err != nil {
		t.Fatalf("write access file: %v", err)
	}

	app := &portalApp{accessFile: accessFile}
	req := httptest.NewRequest(http.MethodGet, "/api/portal/operational-session", nil)
	req.AddCookie(&http.Cookie{Name: accessCookieName, Value: "disabled-token"})
	rec := httptest.NewRecorder()

	app.portalOperationalSession(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAccessEntryWithValidCookieRedirects(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	accessFile := filepath.Join(tempDir, "portal-access.db")
	payload := `[
  {
    "id": 7,
    "project_key": "textile-erp",
    "company_name": "demo",
    "contact_name": "demo",
    "username": "demo_user",
    "financial_company_id": 9,
    "allow_financial": true,
    "allow_operational": true,
    "expires_at": "2030-08-04T20:29:00Z",
    "access_token": "auto-token",
    "password_hash": "$2a$10$abcdefghijklmnopqrstuv",
    "notes": "",
    "is_active": true,
    "created_at": "2026-07-05T00:00:00Z",
    "last_used_at": "0001-01-01T00:00:00Z"
  }
]` + "\n"
	if err := os.WriteFile(accessFile, []byte(payload), 0o644); err != nil {
		t.Fatalf("write access file: %v", err)
	}

	app := &portalApp{
		accessFile:    accessFile,
		publicBase:    "http://62.60.204.237",
		sessionSecret: "portal-secret",
	}

	req := httptest.NewRequest(http.MethodGet, "/access/auto-token", nil)
	req.AddCookie(&http.Cookie{Name: accessCookieName, Value: "auto-token"})
	rec := httptest.NewRecorder()

	app.accessEntry(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/" {
		t.Fatalf("unexpected redirect target: %s", got)
	}
}

func TestAccessTargetForCoolerStoreIncludesSignedLogin(t *testing.T) {
	t.Parallel()

	app := &portalApp{
		coolerStoreURL:    "http://62.60.204.237:8088",
		coolerStoreSecret: "cooler-secret",
	}
	record := projectAccess{
		ProjectKey:  "cooler-store",
		CompanyName: "کولر سجادی",
		Username:    "TEST_AC_100",
		ExpiresAt:   time.Now().Add(2 * time.Hour),
	}

	target := app.accessTarget(record)
	parsed, err := url.Parse(target)
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}
	if parsed.Path != "/api/portal-login" {
		t.Fatalf("expected cooler-store portal login path, got %s", parsed.Path)
	}
	token := parsed.Query().Get("token")
	if token == "" {
		t.Fatal("expected signed portal token in redirect url")
	}
	claims, err := verifyCoolerPortalTokenForTest(token, "cooler-secret")
	if err != nil {
		t.Fatalf("verify cooler-store token: %v", err)
	}
	if claims["username"] != "TEST_AC_100" {
		t.Fatalf("expected username claim, got %#v", claims["username"])
	}
	if claims["project_key"] != "cooler-store" {
		t.Fatalf("expected project_key claim, got %#v", claims["project_key"])
	}
}

func verifyJWTForTest(token, secret string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, os.ErrInvalid
	}
	unsigned := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(unsigned))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[2])) {
		return nil, os.ErrPermission
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	claims := map[string]any{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, err
	}
	return claims, nil
}

func verifyCoolerPortalTokenForTest(token, secret string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return nil, os.ErrInvalid
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(parts[0]))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[1])) {
		return nil, os.ErrPermission
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, err
	}
	claims := map[string]any{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, err
	}
	return claims, nil
}
