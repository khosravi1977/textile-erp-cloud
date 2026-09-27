package main

// Firewall contract smoke (FW-01) — the two login paths the whole product
// stands on, plus the admin-panel auth gate. A regression here (the
// "login broke / tabs vanished" class) turns CI red before a merge.

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func firewallTestApp() *portalApp {
	return &portalApp{
		adminUsername: "fw-admin",
		adminPassword: "fw-pass-12345",
		sessionSecret: "fw-test-session-secret-0123456789abcdef",
	}
}

func firewallAdminSession(t *testing.T, a *portalApp) *http.Cookie {
	t.Helper()
	rr := httptest.NewRecorder()
	a.setAdminSessionCookie(rr, httptest.NewRequest(http.MethodGet, "/admin", nil), time.Now().Add(time.Hour))
	for _, c := range rr.Result().Cookies() {
		if c.Name == adminCookieName && c.Value != "" {
			return c
		}
	}
	t.Fatal("could not mint an admin session cookie")
	return nil
}

func TestFirewallAdminLoginContract(t *testing.T) {
	a := firewallTestApp()

	// Wrong credentials: the login page re-renders and NO session cookie is set.
	rr := httptest.NewRecorder()
	form := url.Values{"username": {"fw-admin"}, "password": {"definitely-wrong"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	a.adminLogin(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("wrong creds: got status %d, want %d (login page re-render)", rr.Code, http.StatusOK)
	}
	for _, c := range rr.Result().Cookies() {
		if c.Name == adminCookieName && c.Value != "" {
			t.Fatal("wrong creds: admin session cookie must not be set")
		}
	}

	// Correct credentials: 303 redirect to /admin with the session cookie.
	rr = httptest.NewRecorder()
	form = url.Values{"username": {"fw-admin"}, "password": {"fw-pass-12345"}}
	req = httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	a.adminLogin(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("correct creds: got status %d, want %d", rr.Code, http.StatusSeeOther)
	}
	found := false
	for _, c := range rr.Result().Cookies() {
		if c.Name == adminCookieName && c.Value != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("correct creds: admin session cookie missing")
	}
}

func TestFirewallAdminPanelAuthGate(t *testing.T) {
	a := firewallTestApp()

	// No session: redirect to the login page.
	rr := httptest.NewRecorder()
	a.adminPanel(rr, httptest.NewRequest(http.MethodGet, "/admin", nil))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("admin without session: got %d, want redirect %d", rr.Code, http.StatusSeeOther)
	}
	if loc := rr.Header().Get("Location"); loc != "/admin/login" {
		t.Fatalf("admin without session: Location %q, want /admin/login", loc)
	}

	// Valid session: the panel renders.
	rr = httptest.NewRecorder()
	authed := httptest.NewRequest(http.MethodGet, "/admin", nil)
	authed.AddCookie(firewallAdminSession(t, a))
	a.adminPanel(rr, authed)
	if rr.Code != http.StatusOK {
		t.Fatalf("admin with session: got %d, want %d", rr.Code, http.StatusOK)
	}
	if !strings.Contains(rr.Body.String(), "پنل مدیریت") {
		t.Fatal("admin panel body is missing its title marker")
	}
}

func TestFirewallCustomerLoginAdminShortcut(t *testing.T) {
	// POST /login with admin credentials (non-local mode) must set the admin
	// session and redirect to /admin — the primary entry path.
	a := firewallTestApp()
	rr := httptest.NewRecorder()
	form := url.Values{"username": {"fw-admin"}, "password": {"fw-pass-12345"}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	a.customerLogin(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("customer login admin shortcut: got %d, want %d", rr.Code, http.StatusSeeOther)
	}
	found := false
	for _, c := range rr.Result().Cookies() {
		if c.Name == adminCookieName && c.Value != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("customer login admin shortcut: admin session cookie not set")
	}
}

func TestFirewallCustomerLoginRejectsBadPassword(t *testing.T) {
	a := firewallTestApp()
	rr := httptest.NewRecorder()
	form := url.Values{"username": {"fw-admin"}, "password": {"nope"}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	a.customerLogin(rr, req)
	for _, c := range rr.Result().Cookies() {
		if c.Name == adminCookieName && c.Value != "" {
			t.Fatal("bad password: admin session cookie must not be set")
		}
	}
}
