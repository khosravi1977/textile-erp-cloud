package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHesabyarPortalConsentAndCompanyIsolation(t *testing.T) {
	const origin = "https://erp.example.test"
	t.Setenv("HESABYAR_PWA_ORIGIN", origin)
	t.Setenv("HESABYAR_PWA_ACCOUNTS", "501:31,502:32,501:33")
	var mu sync.Mutex
	var queries []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunks := strings.Split(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), ".")
		if len(chunks) != 3 || r.Method != http.MethodGet || r.URL.RawQuery != "" || r.Header.Get("Cookie") != "" {
			t.Error("unexpected upstream request shape")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		decoded, err := base64.RawURLEncoding.DecodeString(chunks[1])
		var claims struct {
			Company int64  `json:"company_id"`
			Path    string `json:"hsbyr_path"`
			Role    string `json:"role"`
			Aud     string `json:"aud"`
		}
		if err != nil || json.Unmarshal(decoded, &claims) != nil || claims.Role != "hesabyar_lookup" || claims.Aud != "hesabyar-portal" || claims.Path != r.URL.Path {
			t.Error("unscoped financial credential")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		mu.Lock()
		queries = append(queries, claims.Path)
		mu.Unlock()
		name := map[int64]string{501: "شرکت آلفا", 502: "شرکت بتا"}[claims.Company]
		if name == "" {
			t.Error("unexpected company")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		row := map[string]any{"id": 1, "name": name, "status": "ACTIVE", "roles": []string{"CUSTOMER"}, "code": "sku-123", "unit": "متر", "qty": "12.25", "debit": "100.00", "credit": "20.00", "count": 2}
		respondJSON(w, http.StatusOK, map[string]any{"success": true, "total": 1, "rows": []any{row}})
	}))
	defer upstream.Close()
	accesses := []projectAccess{}
	for n, name := range []string{"alpha", "beta", "limited"} {
		accesses = append(accesses, projectAccess{ID: int64(n + 31), FinancialCompanyID: int64(501 + n%2), ProjectKey: "textile-erp",
			Username: name, PasswordHash: "fixture-placeholder", AccessToken: "fixture-" + name, AccessRole: "owner", IsActive: true,
			ExpiresAt: time.Now().Add(time.Hour), AllowFinancial: true, AllowOperational: true})
	}
	accesses[2].AccessRole, accesses[2].Permissions, accesses[2].AllowOperational = "accountant", []string{"initialData"}, false
	path := filepath.Join(t.TempDir(), "access.json")
	if err := writeAccesses(path, accesses); err != nil {
		t.Fatal(err)
	}
	app := &portalApp{accessFile: path, financialAPIURL: upstream.URL,
		financialJWTKey: strings.Repeat("j", 48), sessionSecret: strings.Repeat("s", 48)}
	do := func(method, path, originHeader, token, body string, consent *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, origin+hesabyarPath+path, strings.NewReader(body))
		if originHeader != "" {
			r.Header.Set("Origin", originHeader)
			r.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			r.AddCookie(&http.Cookie{Name: accessCookieName, Value: "fixture-" + token})
		}
		if consent != nil {
			r.AddCookie(consent)
		}
		w := httptest.NewRecorder()
		app.hesabyarPWA(w, r)
		if w.Code != http.StatusNotFound && w.Header().Get("Cache-Control") != "no-store" {
			t.Error("missing no-store response")
		}
		return w
	}
	check := func(w *httptest.ResponseRecorder, code int, body string) {
		t.Helper()
		if w.Code != code || (body != "" && !strings.Contains(w.Body.String(), body)) {
			t.Fatalf("status=%d want=%d expected response field %q", w.Code, code, body)
		}
	}
	grant := func(token string) *http.Cookie {
		t.Helper()
		w := do("POST", "api/grant", origin, token, `{}`, nil)
		check(w, 200, `"granted":true`)
		c := w.Result().Cookies()
		if len(c) != 1 || !c[0].Secure || !c[0].HttpOnly || c[0].SameSite != http.SameSiteStrictMode || c[0].Path != hesabyarPath || c[0].Domain != "" {
			t.Fatal("unexpected consent cookie attributes")
		}
		return c[0]
	}
	cmd := func(token, command string, cookie *http.Cookie) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(map[string]string{"text": command})
		return do("POST", "api/command", origin, token, string(raw), cookie)
	}
	check(do("GET", "", "", "", "", nil), 200, "حسابیار صوتی")
	check(do("GET", "api/config", "", "", "", nil), 401, "login_required")
	check(do("GET", "api/config", "", "alpha", "", nil), 200, `"granted":false`)
	check(do("POST", "api/grant", "https://unregistered.example", "alpha", `{}`, nil), 403, "")
	check(cmd("alpha", "جستجوی پارچه شرکت", nil), 403, "consent_required")
	alpha, beta, limited := grant("alpha"), grant("beta"), grant("limited")
	check(cmd("beta", "جستجوی پارچه شرکت", alpha), 403, "consent_required")
	check(cmd("alpha", "جست‌وجوی پارچه شرکت را بگو", alpha), 200, "شرکت آلفا")
	check(cmd("beta", "جستجوی پارچه آلفا", beta), 200, "not_found")
	check(cmd("beta", "جستجوی نخ شرکت", beta), 200, "شرکت بتا")
	check(cmd("alpha", "موجودی کد SKU-۱۲۳ را بگو", alpha), 200, "12.25")
	check(cmd("beta", "صورت‌حساب مشتری شرکت بتا", beta), 200, "100.00")
	check(cmd("limited", "موجودی کد SKU-123", limited), 200, `"code":"forbidden"`)
	check(cmd("limited", "جستجوی مشتری شرکت", limited), 200, "شرکت آلفا")
	check(cmd("alpha", "فاکتور ثبت کن", alpha), 200, "clarify")
	invoice := "پیش‌نمایش فاکتور برای مشتری شرکت آلفا، کد SKU-۱۲۳، تعداد ۲٫۵، قیمت ۳۰۰ ریال"
	check(cmd("limited", invoice, limited), 200, "forbidden")
	check(cmd("alpha", invoice, alpha), 200, "750 ریال")
	w := do("POST", "api/disconnect", origin, "alpha", `{}`, alpha)
	check(w, 200, `"granted":false`)
	if cookies := w.Result().Cookies(); len(cookies) != 1 || cookies[0].MaxAge != -1 {
		t.Fatal("disconnect did not clear consent cookie")
	}
	accesses[0].IsActive = false
	if err := writeAccesses(path, accesses); err != nil {
		t.Fatal(err)
	}
	check(cmd("alpha", "جستجوی پارچه شرکت", alpha), 401, "login_required")
	t.Setenv("HESABYAR_PWA_ACCOUNTS", "501:32")
	check(do("GET", "api/config", "", "beta", "", nil), 403, "permission_required")
	mu.Lock()
	defer mu.Unlock()
	if len(queries) != 8 {
		t.Fatalf("unexpected number of upstream queries: %d", len(queries))
	}
	t.Setenv("HESABYAR_PWA_ORIGIN", "")
	check(do("GET", "", "", "", "", nil), 404, "")
}
