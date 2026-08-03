package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func newPurchaseOrderTestApp(t *testing.T) *portalApp {
	t.Helper()
	dir := t.TempDir()
	accessFile := filepath.Join(dir, "portal-access.db")
	ordersFile := filepath.Join(dir, "portal-orders.json")
	if err := ensureAccessStore(accessFile); err != nil {
		t.Fatal(err)
	}
	if err := ensurePurchaseOrderStore(ordersFile); err != nil {
		t.Fatal(err)
	}
	return &portalApp{
		accessFile:    accessFile,
		ordersFile:    ordersFile,
		publicBase:    "https://textile.example.test",
		sessionSecret: "purchase-order-session-secret",
	}
}

func TestPublicPurchaseOrderPersistsAllThreeProducts(t *testing.T) {
	t.Parallel()
	app := newPurchaseOrderTestApp(t)
	body := `{"companyName":"بافت نمونه","contactName":"مدیر نمونه","mobile":"09121234567","email":"manager@example.com","allowFinancial":true,"allowOperational":true,"allowWeaving":true,"employeeCount":8,"machineCount":24,"unitCount":2,"billingCycle":"annual","notes":"آزمایش سه محصول","website":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/public/purchase-orders", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	app.publicPurchaseOrders(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	retryReq := httptest.NewRequest(http.MethodPost, "/api/public/purchase-orders", strings.NewReader(body))
	retryRec := httptest.NewRecorder()
	app.publicPurchaseOrders(retryRec, retryReq)
	if retryRec.Code != http.StatusCreated {
		t.Fatalf("idempotent retry failed: %d %s", retryRec.Code, retryRec.Body.String())
	}
	items, err := readPurchaseOrders(app.ordersFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one order after a retry, got %d", len(items))
	}
	order := items[0]
	if !order.AllowFinancial || !order.AllowOperational || !order.AllowWeaving {
		t.Fatalf("three selected products were not persisted: %#v", order)
	}
	if order.Status != purchaseOrderPending || !strings.HasPrefix(order.ID, "TX-") {
		t.Fatalf("unexpected new order state: %#v", order)
	}
	if strings.Contains(rec.Body.String(), order.Mobile) || strings.Contains(rec.Body.String(), order.Email) {
		t.Fatal("public order response must not echo private contact details")
	}
}

func TestPublicPurchaseOrderRejectsMissingProductAndWeavingMachineCount(t *testing.T) {
	t.Parallel()
	app := newPurchaseOrderTestApp(t)
	cases := []string{
		`{"companyName":"بافت نمونه","contactName":"مدیر نمونه","mobile":"09121234567","employeeCount":2,"machineCount":0,"unitCount":1,"billingCycle":"annual"}`,
		`{"companyName":"بافت نمونه","contactName":"مدیر نمونه","mobile":"09121234567","allowWeaving":true,"employeeCount":2,"machineCount":0,"unitCount":1,"billingCycle":"annual"}`,
	}
	for _, body := range cases {
		req := httptest.NewRequest(http.MethodPost, "/api/public/purchase-orders", strings.NewReader(body))
		rec := httptest.NewRecorder()
		app.publicPurchaseOrders(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	}
	items, err := readPurchaseOrders(app.ordersFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("invalid orders were persisted: %#v", items)
	}
}

func TestApprovePurchaseOrderCreatesTenantAccessOnce(t *testing.T) {
	app := newPurchaseOrderTestApp(t)
	var mu sync.Mutex
	calls := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls[r.URL.Path]++
		mu.Unlock()
		switch r.URL.Path {
		case "/api/portal/provision":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "company_id": 77})
		case "/api/internal/provision":
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
				http.Error(w, "missing bearer", http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"tenantId": "11111111-1111-4111-8111-111111111111"})
		case "/api/internal/users/sync":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	app.operationalAPI = server.URL
	app.operationalSessionSecret = "operational-order-secret"
	app.weavingAppURL = server.URL
	app.weavingMonitorToken = "weaving-order-secret-that-is-long-enough"

	order, err := app.createPurchaseOrder(purchaseOrderRequest{
		CompanyName: "بافت کامل", ContactName: "مدیر کامل", Mobile: "09121234567",
		AllowFinancial: true, AllowOperational: true, AllowWeaving: true,
		EmployeeCount: 5, MachineCount: 40, UnitCount: 1, BillingCycle: "annual",
	})
	if err != nil {
		t.Fatal(err)
	}
	approved, access, password, err := app.approvePurchaseOrder(order.ID, "تأیید فروش")
	if err != nil {
		t.Fatal(err)
	}
	if approved.Status != purchaseOrderApproved || approved.AccessID != access.ID {
		t.Fatalf("order was not linked to access: order=%#v access=%#v", approved, access)
	}
	if !access.AllowFinancial || !access.AllowOperational || !access.AllowWeaving || access.FinancialCompanyID != 77 || access.WeavingTenantID == "" {
		t.Fatalf("approved access does not contain all products: %#v", access)
	}
	if strings.TrimSpace(access.Username) == "" || len(password) < 8 {
		t.Fatalf("manager credentials were not generated: username=%q password-length=%d", access.Username, len(password))
	}

	_, secondAccess, secondPassword, err := app.approvePurchaseOrder(order.ID, "تأیید مجدد")
	if err != nil {
		t.Fatal(err)
	}
	if secondAccess.ID != access.ID || secondPassword != password {
		t.Fatal("idempotent approval did not return the existing account")
	}
	items, err := app.listAccesses()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("approval created duplicate accounts: %#v", items)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls["/api/portal/provision"] != 1 || calls["/api/internal/provision"] != 1 || calls["/api/internal/users/sync"] != 1 {
		t.Fatalf("approval provisioning was repeated: %#v", calls)
	}
}

func TestApproveAddOnOrderUpgradesExistingCustomerWithoutDuplicate(t *testing.T) {
	app := newPurchaseOrderTestApp(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/portal/provision":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "company_id": 88})
		case "/api/internal/provision":
			_ = json.NewEncoder(w).Encode(map[string]any{"tenantId": "22222222-2222-4222-8222-222222222222"})
		case "/api/internal/users/sync":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	app.operationalAPI = server.URL
	app.operationalSessionSecret = "operational-add-on-secret"
	app.weavingAppURL = server.URL
	app.weavingMonitorToken = "weaving-add-on-secret-that-is-long-enough"

	existing, _, err := app.createManagedAccess(
		"textile-erp", "پرگل", "مدیر پرگل", "paregol-manager", "Secure123!", 0,
		time.Now().Add(60*24*time.Hour), time.Time{}, "مشتری موجود", "owner", financialPermissionCatalog,
		true, false, true, true, false,
	)
	if err != nil {
		t.Fatal(err)
	}
	order, err := app.createPurchaseOrder(purchaseOrderRequest{
		CompanyName: "پرگل", ContactName: "مدیر پرگل", Mobile: "09121234567",
		AllowWeaving: true, EmployeeCount: 4, MachineCount: 72, UnitCount: 1,
		BillingCycle: "annual", RequesterAccessID: existing.ID,
		FinancialCompanyID: existing.FinancialCompanyID, RequestedBy: existing.Username,
	})
	if err != nil {
		t.Fatal(err)
	}
	approved, upgraded, _, err := app.approvePurchaseOrder(order.ID, "افزودن راندمان سالن")
	if err != nil {
		t.Fatal(err)
	}
	if approved.AccessID != existing.ID || upgraded.ID != existing.ID {
		t.Fatalf("add-on order created a different account: order=%#v access=%#v", approved, upgraded)
	}
	if !upgraded.AllowFinancial || !upgraded.AllowOperational || !upgraded.AllowWeaving || upgraded.WeavingTenantID == "" {
		t.Fatalf("existing customer was not upgraded to all three products: %#v", upgraded)
	}
	items, err := app.listAccesses()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("add-on order duplicated the customer: %#v", items)
	}
}

func TestAdminOrdersRequireAdminSession(t *testing.T) {
	t.Parallel()
	app := newPurchaseOrderTestApp(t)
	req := httptest.NewRequest(http.MethodGet, "/admin/api/orders", nil)
	rec := httptest.NewRecorder()
	app.adminOrdersAPI(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestCentralLoginAcceptsProductionAdminCredentials(t *testing.T) {
	t.Parallel()
	app := &portalApp{
		adminUsername: "platform_owner",
		adminPassword: "correct-platform-password",
		sessionSecret: "central-admin-session-secret",
	}
	form := url.Values{"username": {"platform_owner"}, "password": {"correct-platform-password"}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	app.customerLogin(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin" {
		t.Fatalf("expected central admin redirect, got %d %s", rec.Code, rec.Header().Get("Location"))
	}
	var found bool
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == adminCookieName && app.verifyAdminSession(cookie.Value) {
			found = true
		}
	}
	if !found {
		t.Fatal("central admin login did not issue an admin session")
	}
}

func TestPurchasePlansExposeThreeIndependentProducts(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/plans", nil)
	rec := httptest.NewRecorder()
	(&portalApp{publicBase: "https://textile.example.test"}).purchasePlans(rec, req)
	body := rec.Body.String()
	for _, text := range []string{"بخش مالی", "بخش عملیاتی", "راندمان سالن بافت", "مرکز فرمان مدیر نساجی رایگان است", "۳۰ روز هر سه بخش را رایگان آزمایش کنید", "شروع تست رایگان ۳۰روزه هر سه بخش"} {
		if !strings.Contains(body, text) {
			t.Fatalf("plans page is missing %q", text)
		}
	}
	if strings.Contains(body, "ریال") || strings.Contains(body, "تومان در ماه") {
		t.Fatal("plans page must not invent prices before business pricing is configured")
	}
	if !strings.Contains(body, "https://textile.example.test/assets/plans-og.png") {
		t.Fatal("plans page is missing its absolute social preview image")
	}
	imageReq := httptest.NewRequest(http.MethodGet, "/assets/plans-og.png", nil)
	imageRec := httptest.NewRecorder()
	(&portalApp{}).plansSocialImage(imageRec, imageReq)
	if imageRec.Code != http.StatusOK || imageRec.Header().Get("Content-Type") != "image/png" || imageRec.Body.Len() < 100000 {
		t.Fatalf("social image was not served correctly: status=%d bytes=%d", imageRec.Code, imageRec.Body.Len())
	}
}

func TestFullTrialTemporarilyEnablesAllThreeProducts(t *testing.T) {
	now := time.Now()
	record := projectAccess{
		ProjectKey:  "textile-erp",
		TrialEndsAt: now.Add(30 * 24 * time.Hour),
	}
	if !effectiveAllowFinancial(record) || !effectiveAllowOperational(record) || !effectiveAllowWeaving(record) {
		t.Fatalf("active full trial did not enable all products: %#v", record)
	}
	if days := fullTrialDaysRemaining(record, now); days != 30 {
		t.Fatalf("expected 30 trial days remaining, got %d", days)
	}
	record.TrialEndsAt = now.Add(-time.Minute)
	if effectiveAllowFinancial(record) || effectiveAllowOperational(record) || effectiveAllowWeaving(record) {
		t.Fatal("expired trial still grants product access")
	}
}

func TestTrialThenPurchaseUpgradesSameAccountWithoutMakingTrialProductsPermanent(t *testing.T) {
	app := newPurchaseOrderTestApp(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/portal/provision":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "company_id": 99})
		case "/api/internal/provision":
			_ = json.NewEncoder(w).Encode(map[string]any{"tenantId": "33333333-3333-4333-8333-333333333333"})
		case "/api/internal/users/sync":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	app.operationalAPI = server.URL
	app.operationalSessionSecret = "operational-trial-secret"
	app.weavingAppURL = server.URL
	app.weavingMonitorToken = "weaving-trial-secret-that-is-long-enough"

	trialOrder, err := app.createPurchaseOrder(purchaseOrderRequest{
		CompanyName: "بافت آزمایشی", ContactName: "مدیر آزمایشی", Mobile: "09120000000",
		Email: "trial@example.com", IsTrial: true, EmployeeCount: 3, MachineCount: 12, UnitCount: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !trialOrder.IsTrial || trialOrder.BillingCycle != "trial" || !trialOrder.AllowFinancial || !trialOrder.AllowOperational || !trialOrder.AllowWeaving {
		t.Fatalf("trial request was not normalized to all products: %#v", trialOrder)
	}
	_, trialAccess, _, err := app.approvePurchaseOrder(trialOrder.ID, "فعال‌سازی تست")
	if err != nil {
		t.Fatal(err)
	}
	if trialAccess.AllowFinancial || trialAccess.AllowOperational || trialAccess.AllowWeaving {
		t.Fatalf("trial products were incorrectly stored as purchased: %#v", trialAccess)
	}
	if !fullTrialActive(trialAccess) || !effectiveAllowFinancial(trialAccess) || !effectiveAllowOperational(trialAccess) || !effectiveAllowWeaving(trialAccess) {
		t.Fatalf("approved trial does not effectively expose all products: %#v", trialAccess)
	}

	purchaseOrder, err := app.createPurchaseOrder(purchaseOrderRequest{
		CompanyName: "بافت آزمایشی", ContactName: "مدیر آزمایشی", Mobile: "09120000000",
		Email: "trial@example.com", AllowWeaving: true, EmployeeCount: 3, MachineCount: 12, UnitCount: 1,
		BillingCycle: "annual",
	})
	if err != nil {
		t.Fatal(err)
	}
	if purchaseOrder.RequesterAccessID != trialAccess.ID {
		t.Fatalf("purchase was not linked to the existing trial account: %#v", purchaseOrder)
	}
	_, upgraded, _, err := app.approvePurchaseOrder(purchaseOrder.ID, "خرید راندمان سالن")
	if err != nil {
		t.Fatal(err)
	}
	if upgraded.ID != trialAccess.ID || upgraded.AllowFinancial || upgraded.AllowOperational || !upgraded.AllowWeaving {
		t.Fatalf("purchase did not preserve one account and only the paid product: %#v", upgraded)
	}
	upgraded.TrialEndsAt = time.Now().Add(-time.Minute)
	if effectiveAllowFinancial(upgraded) || effectiveAllowOperational(upgraded) || !effectiveAllowWeaving(upgraded) {
		t.Fatalf("trial products remained permanent after trial expiry: %#v", upgraded)
	}
	items, err := app.listAccesses()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("trial-to-purchase flow created duplicate accounts: %#v", items)
	}
}

func TestTrialLandingShowsWeavingAndCountdown(t *testing.T) {
	app := newPurchaseOrderTestApp(t)
	record := projectAccess{
		ID: 1, ProjectKey: "textile-erp", CompanyName: "بافت آزمایشی", ContactName: "مدیر",
		Username: "trial-manager", AccessToken: "trial-landing-token", IsActive: true,
		TrialEndsAt: time.Now().Add(30 * 24 * time.Hour), ExpiresAt: time.Now().Add(30 * 24 * time.Hour),
	}
	if err := writeAccesses(app.accessFile, []projectAccess{record}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: accessCookieName, Value: record.AccessToken})
	rec := httptest.NewRecorder()
	app.landing(rec, req)
	body := rec.Body.String()
	for _, text := range []string{"ورود به بخش مالی", "ورود به بخش عملیاتی", "ورود به راندمان سالن بافت", "تست رایگان هر سه بخش", "روز باقی‌مانده"} {
		if !strings.Contains(body, text) {
			t.Fatalf("trial landing is missing %q", text)
		}
	}
}

func TestGrantFullTrialPreservesPurchasedModules(t *testing.T) {
	app := newPurchaseOrderTestApp(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/portal/provision":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "company_id": 101})
		case "/api/internal/provision":
			_ = json.NewEncoder(w).Encode(map[string]any{"tenantId": "44444444-4444-4444-8444-444444444444"})
		case "/api/internal/users/sync":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	app.operationalAPI = server.URL
	app.operationalSessionSecret = "operational-admin-trial-secret"
	app.weavingAppURL = server.URL
	app.weavingMonitorToken = "weaving-admin-trial-secret-long-enough"
	owner, _, err := app.createManagedAccess(
		"textile-erp", "بافت مدیر", "مدیر", "owner-trial", "Secure123!", 0,
		time.Now().Add(365*24*time.Hour), time.Time{}, "خرید مالی", "owner", financialPermissionCatalog,
		true, false, true, false, false,
	)
	if err != nil {
		t.Fatal(err)
	}
	granted, _, err := app.grantFullTrial(owner.ID, 30)
	if err != nil {
		t.Fatal(err)
	}
	if !granted.AllowFinancial || granted.AllowOperational || granted.AllowWeaving {
		t.Fatalf("granting a trial changed paid entitlements: %#v", granted)
	}
	if !fullTrialActive(granted) || !effectiveAllowOperational(granted) || !effectiveAllowWeaving(granted) || granted.WeavingTenantID == "" {
		t.Fatalf("granting a trial did not provision all temporary products: %#v", granted)
	}
}

func TestPurchaseOrderStoreFileMode(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose POSIX permission bits")
	}
	path := filepath.Join(t.TempDir(), "orders.json")
	if err := ensurePurchaseOrderStore(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("orders file is too permissive: %v", info.Mode().Perm())
	}
	if time.Since(info.ModTime()) > time.Minute {
		t.Fatal("orders file was not created during the test")
	}
}
