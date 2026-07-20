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
	if location := rec.Header().Get("Location"); location != "https://textile.example.com/" {
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
	req.AddCookie(&http.Cookie{Name: accessCookieName, Value: "session-token"})
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

	app := &portalApp{accessFile: accessFile, allowOperationalCustomerAccess: true}
	req := httptest.NewRequest(http.MethodGet, "/api/portal/operational-session", nil)
	req.AddCookie(&http.Cookie{Name: accessCookieName, Value: "session-token"})
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

func TestAccessEntryAutoRedirectsAndSetsCookie(t *testing.T) {
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
	rec := httptest.NewRecorder()

	app.accessEntry(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "http://62.60.204.237/" {
		t.Fatalf("unexpected redirect target: %s", got)
	}
	cookies := rec.Result().Cookies()
	found := false
	for _, cookie := range cookies {
		if cookie.Name == accessCookieName && cookie.Value == "auto-token" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected access cookie to be set")
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
