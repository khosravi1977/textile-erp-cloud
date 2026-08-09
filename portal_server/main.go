package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	adminCookieName             = "erp_portal_admin"
	accessCookieName            = "erp_portal_access"
	financialAccessCookieName   = "erp_financial_access"
	operationalAccessCookieName = "erp_operational_access"
	timeLayout                  = "2006-01-02 15:04"
	fullTrialDays               = 30
)

type portalApp struct {
	accessFile                     string
	mu                             sync.Mutex
	ordersFile                     string
	ordersMu                       sync.Mutex
	launchTicketMu                 sync.Mutex
	launchTickets                  map[string]launchTicket
	weavingTicketMu                sync.Mutex
	weavingTickets                 map[string]weavingBridgeTicket
	publicBase                     string
	financialURL                   string
	operationalURL                 string
	financialAPIURL                string
	operationalAPI                 string
	coolerStoreURL                 string
	coolerStoreSecret              string
	allowOperationalCustomerAccess bool
	operationalAdminPassword       string
	operationalSessionSecret       string
	weavingMonitorURL              string
	weavingMonitorToken            string
	weavingAppURL                  string
	adminUsername                  string
	adminPassword                  string
	sessionSecret                  string
	financialJWTKey                string
	localMode                      bool
	localCompanyID                 int64
	localCompanyName               string
}

type launchTicket struct {
	AccessToken string
	Module      string
	ExpiresAt   time.Time
}

type weavingBridgeTicket struct {
	AccessID  int64
	ExpiresAt time.Time
}

type moduleSessionClaims struct {
	AccessToken string `json:"access_token"`
	Module      string `json:"module"`
	AuthMode    string `json:"auth_mode"`
	ExpiresAt   int64  `json:"exp"`
}

type projectAccess struct {
	ID                 int64     `json:"id"`
	ProjectKey         string    `json:"project_key"`
	CompanyName        string    `json:"company_name"`
	ContactName        string    `json:"contact_name"`
	Username           string    `json:"username"`
	FinancialCompanyID int64     `json:"financial_company_id,omitempty"`
	AccessRole         string    `json:"access_role,omitempty"`
	Permissions        []string  `json:"permissions,omitempty"`
	CanManageTeam      bool      `json:"can_manage_team,omitempty"`
	RequiresSetup      bool      `json:"requires_setup,omitempty"`
	MustChangePassword bool      `json:"must_change_password,omitempty"`
	ModuleAccessSet    bool      `json:"module_access_set,omitempty"`
	AllowFinancial     bool      `json:"allow_financial,omitempty"`
	AllowOperational   bool      `json:"allow_operational,omitempty"`
	AllowWeaving       bool      `json:"allow_weaving,omitempty"`
	WeavingTenantID    string    `json:"weaving_tenant_id,omitempty"`
	TrialEndsAt        time.Time `json:"trial_ends_at,omitempty"`
	ExpiresAt          time.Time `json:"expires_at"`
	AccessToken        string    `json:"access_token"`
	PasswordHash       string    `json:"password_hash,omitempty"`
	PasswordEnc        string    `json:"password_enc,omitempty"`
	Notes              string    `json:"notes"`
	IsActive           bool      `json:"is_active"`
	CreatedAt          time.Time `json:"created_at"`
	LastUsedAt         time.Time `json:"last_used_at"`
}

var financialPermissionCatalog = []string{
	"dashboard",
	"financialHealth",
	"initialData",
	"operational",
	"incomingInvoices",
	"yarnOutInvoices",
	"invoices",
	"inventory",
	"costs",
	"receivableDocs",
	"payableDocs",
	"bankCash",
	"accounting",
	"reports",
	"taxReports",
	"credit",
	"advisor",
	"mobileApp",
}

func normalizeAccessRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "", "owner", "customer":
		return "owner"
	case "manager":
		return "manager"
	case "accountant":
		return "accountant"
	case "viewer":
		return "viewer"
	default:
		return "owner"
	}
}

func defaultPermissionsForRole(role string) []string {
	switch normalizeAccessRole(role) {
	case "viewer":
		return []string{"dashboard", "financialHealth", "reports"}
	case "accountant":
		return []string{
			"dashboard", "financialHealth", "initialData", "incomingInvoices", "yarnOutInvoices",
			"invoices", "inventory", "costs", "receivableDocs", "payableDocs", "bankCash",
			"accounting", "reports", "taxReports", "credit", "advisor", "mobileApp",
		}
	case "manager":
		return []string{
			"dashboard", "financialHealth", "initialData", "operational", "incomingInvoices",
			"yarnOutInvoices", "invoices", "inventory", "costs", "receivableDocs",
			"payableDocs", "bankCash", "reports", "taxReports", "credit", "advisor",
			"accounting", "mobileApp",
		}
	default:
		return append([]string{}, financialPermissionCatalog...)
	}
}

func normalizePermissions(input []string, role string) []string {
	allowed := map[string]struct{}{}
	for _, key := range financialPermissionCatalog {
		allowed[key] = struct{}{}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(input))
	for _, raw := range input {
		key := strings.TrimSpace(raw)
		if _, ok := allowed[key]; !ok {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	if len(out) == 0 {
		return defaultPermissionsForRole(role)
	}
	normalizedRole := normalizeAccessRole(role)
	if normalizedRole != "viewer" {
		if _, ok := seen["mobileApp"]; !ok {
			out = append(out, "mobileApp")
		}
	}
	return out
}

func effectiveAccessRole(record projectAccess) string {
	if record.ProjectKey == "textile-erp" {
		return normalizeAccessRole(record.AccessRole)
	}
	return "customer"
}

func effectivePermissions(record projectAccess) []string {
	if record.ProjectKey != "textile-erp" {
		return nil
	}
	if effectiveAccessRole(record) == "owner" {
		return append([]string(nil), financialPermissionCatalog...)
	}
	return normalizePermissions(record.Permissions, effectiveAccessRole(record))
}

func effectiveAllowFinancial(record projectAccess) bool {
	if record.ProjectKey != "textile-erp" {
		return false
	}
	return record.AllowFinancial || fullTrialActive(record)
}

func effectiveAllowOperational(record projectAccess) bool {
	if record.ProjectKey != "textile-erp" {
		return false
	}
	return record.AllowOperational || fullTrialActive(record)
}

func effectiveAllowWeaving(record projectAccess) bool {
	return record.ProjectKey == "textile-erp" && (record.AllowWeaving || fullTrialActive(record))
}

func fullTrialActive(record projectAccess) bool {
	return fullTrialActiveAt(record, time.Now())
}

func fullTrialActiveAt(record projectAccess, now time.Time) bool {
	return record.ProjectKey == "textile-erp" && !record.TrialEndsAt.IsZero() && now.Before(record.TrialEndsAt)
}

func fullTrialDaysRemaining(record projectAccess, now time.Time) int {
	if !fullTrialActiveAt(record, now) {
		return 0
	}
	remaining := record.TrialEndsAt.Sub(now)
	days := int(remaining / (24 * time.Hour))
	if remaining%(24*time.Hour) != 0 {
		days++
	}
	if days < 1 {
		return 1
	}
	return days
}

func (a *portalApp) startFullTrialOnFirstUse(record projectAccess) (projectAccess, error) {
	if record.ProjectKey != "textile-erp" || effectiveAccessRole(record) != "owner" || !record.TrialEndsAt.IsZero() {
		return record, nil
	}
	started, _, err := a.grantFullTrial(record.ID, 30)
	if err != nil {
		return record, err
	}
	return started, nil
}

func (a *portalApp) startFullTrialsForExistingOwners() {
	const maxAttempts = 5
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		items, err := a.listAccesses()
		if err != nil {
			log.Printf("automatic existing owner trial scan failed (attempt %d/%d): %v", attempt, maxAttempts, err)
		} else {
			eligible := 0
			failed := 0
			for _, record := range items {
				if record.ProjectKey != "textile-erp" || effectiveAccessRole(record) != "owner" || !record.IsActive || accessRequiresSetup(record) || time.Now().After(record.ExpiresAt) || !record.TrialEndsAt.IsZero() {
					continue
				}
				eligible++
				if _, err := a.startFullTrialOnFirstUse(record); err != nil {
					failed++
					log.Printf("automatic existing owner trial failed for access %d (attempt %d/%d): %v", record.ID, attempt, maxAttempts, err)
				} else {
					log.Printf("automatic 30-day full trial started for existing access %d", record.ID)
				}
			}
			if eligible == 0 || failed == 0 {
				return
			}
		}
		if attempt < maxAttempts {
			time.Sleep(20 * time.Second)
		}
	}
}

func effectiveCanManageTeam(record projectAccess) bool {
	if record.ProjectKey != "textile-erp" {
		return false
	}
	if record.CanManageTeam {
		return true
	}
	return effectiveAccessRole(record) == "owner"
}

func accessRequiresSetup(record projectAccess) bool {
	return record.RequiresSetup ||
		strings.TrimSpace(record.Username) == "" ||
		strings.TrimSpace(record.PasswordHash) == ""
}

func normalizeModuleAccess(projectKey string, allowFinancial, allowOperational, allowWeaving bool) (bool, bool, bool) {
	if projectKey != "textile-erp" {
		return false, false, false
	}
	return allowFinancial, allowOperational, allowWeaving
}

func accessModuleLabel(record projectAccess) string {
	if record.ProjectKey != "textile-erp" {
		return "پروژه"
	}
	parts := make([]string, 0, 3)
	if effectiveAllowFinancial(record) {
		parts = append(parts, "مالی")
	}
	if effectiveAllowOperational(record) {
		parts = append(parts, "عملیاتی")
	}
	if effectiveAllowWeaving(record) {
		parts = append(parts, "راندمان سالن")
	}
	if len(parts) == 0 {
		return "بدون دسترسی ماژول"
	}
	return strings.Join(parts, " + ")
}

func accessRoleLabel(role string) string {
	switch normalizeAccessRole(role) {
	case "owner":
		return "مالک"
	case "manager":
		return "مدیر"
	case "accountant":
		return "حسابدار"
	case "viewer":
		return "مشاهده‌گر"
	default:
		return "کاربر"
	}
}

func boolPtrValue(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func sameTenantAccess(owner, item projectAccess) bool {
	if owner.ProjectKey != item.ProjectKey {
		return false
	}
	if owner.ProjectKey == "textile-erp" && owner.FinancialCompanyID > 0 && item.FinancialCompanyID > 0 {
		return owner.FinancialCompanyID == item.FinancialCompanyID
	}
	return strings.EqualFold(strings.TrimSpace(owner.CompanyName), strings.TrimSpace(item.CompanyName))
}

func accessUsernameTaken(items []projectAccess, scope projectAccess, username string, excludeID int64) bool {
	username = strings.TrimSpace(username)
	if username == "" {
		return false
	}
	for _, item := range items {
		if item.ID == excludeID {
			continue
		}
		if scope.ProjectKey == "textile-erp" {
			if item.ProjectKey != "textile-erp" {
				continue
			}
		} else if item.ProjectKey != scope.ProjectKey {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(item.Username), username) {
			return true
		}
	}
	return false
}

func slugTokenBase(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "user"
	}
	var builder strings.Builder
	prevDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash && builder.Len() > 0 {
			builder.WriteByte('-')
			prevDash = true
		}
	}
	out := strings.Trim(builder.String(), "-")
	if out == "" {
		return "user"
	}
	if len(out) > 18 {
		out = strings.Trim(out[:18], "-")
	}
	if out == "" {
		return "user"
	}
	return out
}

func main() {
	addr := flag.String("addr", ":8080", "portal listen address")
	financial := flag.String("financial", "http://127.0.0.1:5173", "financial frontend URL")
	operational := flag.String("operational", "http://127.0.0.1:8091", "operational frontend URL")
	financialAPI := flag.String("financial-api", "http://127.0.0.1:8081", "financial API URL")
	operationalAPI := flag.String("operational-api", "http://127.0.0.1:8091", "operational API URL")
	flag.Parse()

	publicBase := normalizeBaseURL(env("PORTAL_PUBLIC_BASE", env("APP_DOMAIN", "http://127.0.0.1"+*addr)), "http://127.0.0.1"+*addr)
	coolerStoreURL := normalizeBaseURL(env("PORTAL_COOLER_STORE_URL", "http://127.0.0.1:8088"), "http://127.0.0.1:8088")
	adminUsername := strings.TrimSpace(env("PORTAL_ADMIN_USERNAME", "admin"))
	adminPassword := strings.TrimSpace(env("PORTAL_ADMIN_PASSWORD", "change_this_portal_admin_password"))
	sessionSecret := strings.TrimSpace(env("PORTAL_SESSION_SECRET", "change_this_portal_session_secret"))
	coolerStoreSecret := strings.TrimSpace(env("PORTAL_COOLER_STORE_SECRET", sessionSecret))
	financialJWTKey := strings.TrimSpace(env("PORTAL_FINANCIAL_JWT_SECRET", env("JWT_SECRET", "")))
	allowOperationalCustomerAccess := envBool("PORTAL_ALLOW_OPERATIONAL_CUSTOMER_ACCESS", false)
	operationalAdminPassword := strings.TrimSpace(env("OPERATIONAL_ADMIN_PASSWORD", "admin123"))
	operationalSessionSecret := strings.TrimSpace(env("PORTAL_OPERATIONAL_SECRET", sessionSecret))
	weavingMonitorURL := strings.TrimSpace(env("PORTAL_WEAVING_MONITOR_SUMMARY_URL", ""))
	weavingMonitorToken := strings.TrimSpace(env("PORTAL_WEAVING_MONITOR_TOKEN", ""))
	weavingAppURL := normalizeBaseURL(env("PORTAL_WEAVING_APP_URL", "https://weaving.vioraapps.com"), "https://weaving.vioraapps.com")
	localMode := envBool("PORTAL_LOCAL_MODE", false)
	localCompanyID := envInt64("PORTAL_LOCAL_COMPANY_ID", 2)
	localCompanyName := strings.TrimSpace(env("PORTAL_LOCAL_COMPANY_NAME", "پرگل"))
	dbPath := strings.TrimSpace(env("ACCESS_DB_PATH", "/data/portal-access.db"))
	ordersPath := strings.TrimSpace(env("PORTAL_ORDERS_PATH", filepath.Join(filepath.Dir(dbPath), "portal-orders.json")))
	validatePortalProductionConfig(adminPassword, sessionSecret, financialJWTKey, operationalSessionSecret)
	if err := validateExecutiveMonitorConfig(weavingMonitorURL, weavingMonitorToken); err != nil {
		log.Printf("executive monitor configuration warning: %v", err)
	}

	if err := ensureAccessStore(dbPath); err != nil {
		log.Fatal(err)
	}
	if err := ensurePurchaseOrderStore(ordersPath); err != nil {
		log.Fatal(err)
	}

	app := &portalApp{
		accessFile:                     dbPath,
		ordersFile:                     ordersPath,
		launchTickets:                  make(map[string]launchTicket),
		weavingTickets:                 make(map[string]weavingBridgeTicket),
		publicBase:                     strings.TrimRight(publicBase, "/"),
		financialURL:                   *financial,
		operationalURL:                 *operational,
		financialAPIURL:                *financialAPI,
		operationalAPI:                 *operationalAPI,
		coolerStoreURL:                 strings.TrimRight(coolerStoreURL, "/"),
		coolerStoreSecret:              coolerStoreSecret,
		allowOperationalCustomerAccess: allowOperationalCustomerAccess,
		operationalAdminPassword:       operationalAdminPassword,
		operationalSessionSecret:       operationalSessionSecret,
		weavingMonitorURL:              weavingMonitorURL,
		weavingMonitorToken:            weavingMonitorToken,
		weavingAppURL:                  strings.TrimRight(weavingAppURL, "/"),
		adminUsername:                  adminUsername,
		adminPassword:                  adminPassword,
		sessionSecret:                  sessionSecret,
		financialJWTKey:                financialJWTKey,
		localMode:                      localMode,
		localCompanyID:                 localCompanyID,
		localCompanyName:               localCompanyName,
	}
	if err := app.repairAccessHashes(); err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", app.health)
	mux.HandleFunc("/downloads/HesabYar.apk", app.downloadMobileApp)
	mux.HandleFunc("/HesabYar.apk", app.downloadMobileApp)
	mux.HandleFunc("/login", app.customerLogin)
	mux.HandleFunc("/plans", app.purchasePlans)
	mux.HandleFunc("/api/public/purchase-orders", app.publicPurchaseOrders)
	mux.HandleFunc("/assets/plans-og.png", app.plansSocialImage)
	mux.HandleFunc("/logout", app.customerLogout)
	mux.HandleFunc("/module-login", app.moduleLogin)
	mux.HandleFunc("/module-logout", app.moduleLogout)
	mux.HandleFunc("/team", app.teamPage)
	mux.HandleFunc("/admin/login", app.adminLogin)
	mux.HandleFunc("/admin/logout", app.adminLogout)
	mux.HandleFunc("/admin/api/accesses", app.adminAccesses)
	mux.HandleFunc("/admin/api/accesses/", app.adminAccessByID)
	mux.HandleFunc("/admin/orders", app.adminOrdersPage)
	mux.HandleFunc("/admin/api/orders", app.adminOrdersAPI)
	mux.HandleFunc("/admin/api/orders/", app.adminOrderByID)
	mux.HandleFunc("/admin/api/launch-ticket", app.adminLaunchTicket)
	mux.HandleFunc("/admin/api/deprovision", app.adminDeprovision)
	mux.HandleFunc("/admin", app.adminPanel)
	mux.HandleFunc("/access/", app.accessEntry)
	mux.HandleFunc("/launch/", app.launchEntry)
	mux.HandleFunc("/api/weaving/sso-ticket/", app.weavingSSOTicket)
	mux.HandleFunc("/api/portal/financial-session", app.portalFinancialSession)
	mux.HandleFunc("/api/portal/operational-session", app.portalOperationalSession)
	mux.HandleFunc("/api/portal/team", app.portalTeam)
	mux.HandleFunc("/api/portal/team/", app.portalTeamByID)
	mux.HandleFunc("/api/portal/executive-session", app.executiveSession)
	mux.HandleFunc("/api/portal/executive-hall", app.executiveHallSummary)
	mux.HandleFunc("/executive", app.executiveApp)
	mux.HandleFunc("/executive/", app.executiveApp)
	mux.Handle("/financial/", app.requireModuleAccess("financial", stripAndProxy("/financial", *financial)))
	mux.Handle("/assets/", app.requireModuleAccess("financial", stripAndProxy("", *financial)))
	mux.Handle("/operational/", app.requireModuleAccess("operational", stripAndProxy("/operational", *operational)))
	mux.Handle("/@vite/", app.requireModuleAccess("financial", stripAndProxy("", *financial)))
	mux.Handle("/src/", app.requireModuleAccess("financial", stripAndProxy("", *financial)))
	mux.Handle("/node_modules/", app.requireModuleAccess("financial", stripAndProxy("", *financial)))
	mux.Handle("/api/financial/", stripAndProxy("/api/financial", *financialAPI))
	mux.Handle("/api/operational/", stripAndProxy("/api/operational", *operationalAPI))
	mux.HandleFunc("/", app.landing)

	log.Printf("ERP portal started on %s", *addr)
	log.Printf("portal_public_base=%s cooler_store=%s access_db=%s orders_db=%s", app.publicBase, app.coolerStoreURL, dbPath, ordersPath)
	log.Printf("financial=%s operational=%s financial_api=%s operational_api=%s", *financial, *operational, *financialAPI, *operationalAPI)
	go app.startFullTrialsForExistingOwners()
	log.Fatal(http.ListenAndServe(*addr, mux))
}

func (a *portalApp) downloadMobileApp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.android.package-archive")
	w.Header().Set("Content-Disposition", `attachment; filename="HesabYar.apk"`)
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, "/app/downloads/HesabYar.apk")
}

func stripAndProxy(prefix, target string) http.Handler {
	u, err := url.Parse(target)
	if err != nil {
		panic(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(u)
	originalDirector := proxy.Director
	proxy.Director = func(r *http.Request) {
		originalDirector(r)
		r.URL.Path = strings.TrimPrefix(r.URL.Path, prefix)
		if r.URL.Path == "" {
			r.URL.Path = "/"
		}
		r.Host = u.Host
		r.Header.Set("X-Forwarded-Prefix", prefix)
	}
	proxy.ModifyResponse = injectPortalConfig(prefix)
	return proxy
}

func (a *portalApp) requireModuleAccess(module string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record, err := a.moduleRecordFromRequest(r, module)
		if err != nil {
			nextPath := safePortalNext(r.URL.RequestURI())
			http.Redirect(w, r, "/module-login?module="+url.QueryEscape(module)+"&next="+url.QueryEscape(nextPath), http.StatusSeeOther)
			return
		}
		switch module {
		case "financial":
			if !effectiveAllowFinancial(record) {
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
		case "operational":
			if !effectiveAllowOperational(record) {
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
			if r.Method == http.MethodGet && (r.URL.Path == "/operational" || r.URL.Path == "/operational/" || strings.Contains(r.Header.Get("Accept"), "text/html")) {
				if _, err := a.createOperationalSessionForRecord(w, r, record); err != nil {
					log.Printf("operational single sign-on failed for access=%d: %v", record.ID, err)
					http.Error(w, "ورود خودکار به بخش عملیاتی برقرار نشد. دوباره از منوی اصلی تلاش کنید.", http.StatusServiceUnavailable)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func injectPortalConfig(prefix string) func(*http.Response) error {
	return func(resp *http.Response) error {
		if prefix == "/operational" || prefix == "/api/operational" {
			resp.Header.Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
			resp.Header.Set("Pragma", "no-cache")
			resp.Header.Set("Expires", "0")
		}
		if !strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
			return nil
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		_ = resp.Body.Close()
		body = rewriteAssetRefs(body, prefix)
		mobileBase := strings.TrimRight(strings.TrimSpace(os.Getenv("PORTAL_MOBILE_BASE")), "/")
		inject := []byte(`<script>window.ERP_PORTAL_PREFIX="` + prefix + `";window.ERP_MOBILE_ORIGIN=` + strconv.Quote(mobileBase) + `;window.ERP_FINANCIAL_API="/api/financial/api";window.ERP_OPERATIONAL_API="/api/operational/api";window.ERP_PORTAL_FINANCIAL_SESSION="/api/portal/financial-session";window.ERP_PORTAL_OPERATIONAL_SESSION="/api/portal/operational-session";</script>`)
		body = bytes.Replace(body, []byte("</head>"), append(inject, []byte("</head>")...), 1)
		resp.Body = io.NopCloser(bytes.NewReader(body))
		resp.ContentLength = int64(len(body))
		resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
		return nil
	}
}

func rewriteAssetRefs(body []byte, prefix string) []byte {
	if prefix == "" {
		return body
	}
	replacements := [][2]string{
		{`src="/assets/`, `src="` + prefix + `/assets/`},
		{`href="/assets/`, `href="` + prefix + `/assets/`},
		{`src="./assets/`, `src="` + prefix + `/assets/`},
		{`href="./assets/`, `href="` + prefix + `/assets/`},
	}
	for _, replacement := range replacements {
		body = bytes.ReplaceAll(body, []byte(replacement[0]), []byte(replacement[1]))
	}
	return body
}

func (a *portalApp) health(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]any{
		"ok":                 true,
		"financial":          a.financialURL,
		"operational":        a.operationalURL,
		"financialApi":       a.financialAPIURL,
		"operationalApi":     a.operationalAPI,
		"coolerStore":        a.coolerStoreURL,
		"weavingApp":         a.weavingAppURL,
		"weavingIntegration": a.weavingIntegrationStatus(r.Context()),
		"accessManager":      "ok",
		"purchaseOrders":     a.purchaseOrderStoreStatus(),
		"telegramReports":    a.telegramReportStatus(r.Context()),
	})
}

func (a *portalApp) weavingIntegrationStatus(parent context.Context) string {
	base := strings.TrimRight(strings.TrimSpace(a.weavingAppURL), "/")
	if base == "" {
		return "unavailable"
	}
	ctx, cancel := context.WithTimeout(parent, 4*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/auth/portal-sso/health", nil)
	if err != nil {
		return "unavailable"
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "unavailable"
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "unavailable"
	}
	return "ok"
}

func (a *portalApp) issueWeavingBridgeTicket(record projectAccess) (string, error) {
	if record.ProjectKey != "textile-erp" || !record.IsActive || time.Now().After(record.ExpiresAt) || !moduleAllowed(record, "weaving") {
		return "", errors.New("weaving access is not active")
	}
	if record.FinancialCompanyID <= 0 || strings.TrimSpace(record.Username) == "" {
		return "", errors.New("weaving account is incomplete")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	ticket := base64.RawURLEncoding.EncodeToString(raw)
	a.weavingTicketMu.Lock()
	defer a.weavingTicketMu.Unlock()
	if a.weavingTickets == nil {
		a.weavingTickets = make(map[string]weavingBridgeTicket)
	}
	for key, item := range a.weavingTickets {
		if time.Now().After(item.ExpiresAt) {
			delete(a.weavingTickets, key)
		}
	}
	a.weavingTickets[ticket] = weavingBridgeTicket{AccessID: record.ID, ExpiresAt: time.Now().Add(2 * time.Minute)}
	return ticket, nil
}

func (a *portalApp) weavingSSOTicket(w http.ResponseWriter, r *http.Request) {
	setPrivatePageHeaders(w)
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ticket := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/weaving/sso-ticket/"), "/")
	if ticket == "health" {
		respondJSON(w, http.StatusOK, map[string]any{"ok": true, "service": "textile-weaving-sso-bridge"})
		return
	}
	a.weavingTicketMu.Lock()
	item, ok := a.weavingTickets[ticket]
	if ok {
		delete(a.weavingTickets, ticket)
	}
	a.weavingTicketMu.Unlock()
	if !ok || time.Now().After(item.ExpiresAt) {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "ticket is invalid or expired"})
		return
	}
	items, err := a.listAccesses()
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "access store is unavailable"})
		return
	}
	var record projectAccess
	for _, candidate := range items {
		if candidate.ID == item.AccessID {
			record = candidate
			break
		}
	}
	if record.ID == 0 || !record.IsActive || time.Now().After(record.ExpiresAt) || !moduleAllowed(record, "weaving") {
		respondJSON(w, http.StatusForbidden, map[string]string{"error": "weaving access is not active"})
		return
	}
	role := "worker"
	if effectiveAccessRole(record) == "owner" || effectiveAccessRole(record) == "manager" {
		role = "manager"
	}
	tenantName := strings.TrimSpace(record.CompanyName)
	if tenantName == "" {
		tenantName = "سالن بافت"
	}
	displayName := strings.TrimSpace(record.ContactName)
	if displayName == "" {
		displayName = record.Username
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"tenantName":        tenantName,
		"externalTenantKey": fmt.Sprintf("textile-company:%d", record.FinancialCompanyID),
		"externalUserId":    fmt.Sprintf("textile-access:%d", record.ID),
		"username":          record.Username,
		"displayName":       displayName,
		"role":              role,
		"sessionExpiresAt":  minTime(record.ExpiresAt, time.Now().Add(12*time.Hour)).Unix(),
	})
}

func (a *portalApp) telegramReportStatus(parent context.Context) string {
	base := strings.TrimRight(strings.TrimSpace(a.financialAPIURL), "/")
	if base == "" {
		return "unavailable"
	}
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/health", nil)
	if err != nil {
		return "unavailable"
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "unavailable"
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "unavailable"
	}
	var payload struct {
		TelegramReports struct {
			Available bool `json:"available"`
		} `json:"telegramReports"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&payload); err != nil || !payload.TelegramReports.Available {
		return "unavailable"
	}
	return "ok"
}

func (a *portalApp) landing(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	setPrivatePageHeaders(w)
	record, err := a.accessRecordFromRequest(r)
	hasAccess := err == nil && record.ProjectKey == "textile-erp"
	if hasAccess {
		if started, startErr := a.startFullTrialOnFirstUse(record); startErr != nil {
			log.Printf("automatic full trial activation failed for access %d: %v", record.ID, startErr)
		} else {
			record = started
		}
	}
	cardParts := make([]string, 0, 6)
	statusParts := []string{
		`<div class="pill">یک نام کاربری و رمز برای همه بخش‌های مجاز</div>`,
		`<div class="pill">خرید مستقل هر بخش پس از تست رایگان</div>`,
	}
	title := "درگاه دسترسی ERP نساجی"
	hint := "یک‌بار وارد شوید؛ سپس بخش‌های مجاز بدون درخواست دوبارهٔ رمز باز می‌شوند."
	foot := "مدیر شرکت می‌تواند برای هر کارمند، از میان بخش‌های خریداری‌شده دسترسی مناسب تعیین کند."
	if hasAccess {
		if fullTrialActive(record) {
			days := fullTrialDaysRemaining(record, time.Now())
			statusParts = append(statusParts,
				`<div class="pill trial-pill">تست رایگان هر سه بخش · `+strconv.Itoa(days)+` روز باقی‌مانده</div>`,
				`<div class="pill">پایان تست: `+html.EscapeString(record.TrialEndsAt.Local().Format(timeLayout))+`</div>`,
			)
		}
		if effectiveAllowFinancial(record) {
			cardParts = append(cardParts, `<a class="card" href="/module-login?module=financial">ورود به بخش مالی</a>`, `<a class="card mobile" href="/HesabYar.apk?v=1.0.3-production-20260803">دانلود اپ حسابیار</a>`)
		}
		if effectiveAllowOperational(record) {
			cardParts = append(cardParts, `<a class="card secondary" href="/module-login?module=operational">ورود به بخش عملیاتی</a>`)
		}
		if effectiveAllowWeaving(record) {
			cardParts = append(cardParts, `<a class="card weaving" href="/module-login?module=weaving">ورود به راندمان سالن بافت</a>`)
		}
		role := effectiveAccessRole(record)
		if role == "owner" || role == "manager" {
			cardParts = append(cardParts, `<a class="card executive" href="/executive/">مرکز فرمان مدیر نساجی</a>`)
		}
		if effectiveCanManageTeam(record) {
			cardParts = append(cardParts, `<a class="card accent" href="/team">مدیریت کاربران و دسترسی‌ها</a>`)
		}
		cardParts = append(cardParts, `<a class="card plans" href="/plans">خرید یا افزودن محصول</a>`)
		statusParts = append(statusParts,
			`<div class="pill">شرکت: `+html.EscapeString(record.CompanyName)+`</div>`,
			`<div class="pill">کاربر فعلی: `+html.EscapeString(record.Username)+`</div>`,
		)
	} else {
		cardParts = append(cardParts,
			`<a class="card" href="/login">ورود یکپارچه به Textile ERP</a>`,
			`<a class="card plans" href="/plans">مشاهده محصولات و ثبت سفارش</a>`,
		)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`<!doctype html>
<html lang="fa" dir="rtl">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>` + html.EscapeString(title) + `</title>
  <style>
    *{box-sizing:border-box} body{margin:0;min-height:100vh;background:#f7f1e8;color:#2a1a14;font-family:Tahoma,Arial;display:flex;align-items:center;justify-content:center;padding:24px}
    .box{width:min(1040px,96vw);background:#fffaf4;border:1px solid #dbc7ae;border-radius:20px;padding:30px;box-shadow:0 24px 80px rgba(75,43,24,.12)}
    h1{margin:0 0 8px;font-size:32px}.hint{color:#6f574a;margin-bottom:24px;line-height:1.9}
    .grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:16px}
    a.card,.empty{display:flex;align-items:center;justify-content:center;text-decoration:none;color:white;background:#8b5e3c;border-radius:16px;padding:24px;border:1px solid #b5835a;font-weight:bold;text-align:center;font-size:18px;min-height:126px}
    a.secondary{background:#44665a;border-color:#7ca28d}
    a.accent{background:#5c4334;border-color:#8c6a51}
    a.mobile{background:#176b5b;border-color:#2c927c}
	    a.executive{background:#0f4c5c;border-color:#2b8398}
	    a.weaving{background:#176b5b;border-color:#42a78f}
	    a.plans{background:#7c3aed;border-color:#a78bfa}
    .empty{background:#f4e7d6;color:#5f4635;border-color:#ddc2a5}
    a:hover{filter:brightness(1.08)}
    .status{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:10px;margin-top:22px;color:#7a6355;font-size:12px}
    .pill{border:1px solid #ddc2a5;background:#f4e7d6;border-radius:999px;padding:9px 12px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
	.trial-pill{background:#ecfdf5;border-color:#6ee7b7;color:#065f46;font-weight:bold}
    .foot{margin-top:20px;color:#7a6355;font-size:13px;line-height:1.9}
    @media(max-width:860px){.grid,.status{grid-template-columns:1fr}h1{font-size:24px}}
  </style>
</head>
<body>
  <main class="box">
    <h1>` + html.EscapeString(title) + `</h1>
    <div class="hint">` + html.EscapeString(hint) + `</div>
    <div class="grid">` + strings.Join(cardParts, "") + `</div>
    <div class="status">` + strings.Join(statusParts, "") + `</div>
    <div class="foot">` + html.EscapeString(foot) + `</div>
  </main>
</body>
</html>`))
}

func (a *portalApp) customerLogin(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/login" {
		http.NotFound(w, r)
		return
	}
	nextValue := r.URL.Query().Get("next")
	if r.Method == http.MethodPost && strings.TrimSpace(r.Form.Get("next")) != "" {
		nextValue = r.Form.Get("next")
	}
	nextPath := safePortalNext(nextValue)
	if r.Method == http.MethodGet {
		if record, err := a.accessRecordFromRequest(r); err == nil && record.ProjectKey == "textile-erp" {
			if nextPath != "" {
				http.Redirect(w, r, nextPath, http.StatusSeeOther)
			} else {
				http.Redirect(w, r, a.accessTarget(record), http.StatusSeeOther)
			}
			return
		}
		a.renderCustomerLogin(w, "", nextPath)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		a.renderCustomerLogin(w, "درخواست ورود معتبر نیست.", nextPath)
		return
	}
	nextPath = safePortalNext(r.Form.Get("next"))
	username := strings.TrimSpace(r.Form.Get("username"))
	password := r.Form.Get("password")
	if !a.localMode && username == a.adminUsername && password == a.adminPassword {
		a.setAdminSessionCookie(w, r, time.Now().Add(12*time.Hour))
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	if a.localMode && strings.EqualFold(username, a.adminUsername) && password == a.adminPassword {
		record, err := a.ensureLocalOwnerAccess()
		if err != nil {
			log.Printf("local owner login failed: %v", err)
			a.renderCustomerLogin(w, "حساب مدیر محلی آماده نشد. چند لحظه بعد دوباره تلاش کنید.", nextPath)
			return
		}
		if record, err = a.startFullTrialOnFirstUse(record); err != nil {
			log.Printf("automatic local owner trial activation failed: %v", err)
		}
		a.setPortalAccessCookie(w, r, record.AccessToken, record.ExpiresAt)
		_ = a.markAccessUsed(record.ID)
		if nextPath != "" {
			http.Redirect(w, r, nextPath, http.StatusSeeOther)
		} else {
			http.Redirect(w, r, a.accessTarget(record), http.StatusSeeOther)
		}
		return
	}
	items, err := a.listAccesses()
	if err != nil {
		a.renderCustomerLogin(w, "سامانه ورود موقتاً در دسترس نیست.", nextPath)
		return
	}
	for _, record := range items {
		if record.ProjectKey != "textile-erp" || !record.IsActive || time.Now().After(record.ExpiresAt) {
			continue
		}
		if !strings.EqualFold(username, strings.TrimSpace(record.Username)) {
			continue
		}
		if a.verifyAccessPassword(record.AccessToken, password) != nil {
			continue
		}
		if accessRequiresSetup(record) {
			http.Redirect(w, r, "/access/"+url.PathEscape(record.AccessToken), http.StatusSeeOther)
			return
		}
		if record.MustChangePassword {
			record, _ = a.setAccessMustChangePassword(record.ID, false)
		}
		if record, err = a.startFullTrialOnFirstUse(record); err != nil {
			log.Printf("automatic full trial activation failed for access %d: %v", record.ID, err)
		}
		a.setPortalAccessCookie(w, r, record.AccessToken, record.ExpiresAt)
		_ = a.markAccessUsed(record.ID)
		if nextPath != "" {
			http.Redirect(w, r, nextPath, http.StatusSeeOther)
		} else {
			http.Redirect(w, r, a.accessTarget(record), http.StatusSeeOther)
		}
		return
	}
	a.renderCustomerLogin(w, "نام کاربری یا رمز عبور صحیح نیست.", nextPath)
}

func safePortalNext(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || strings.Contains(value, "\\") {
		return ""
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.IsAbs() || (parsed.Path != "/team" && !strings.HasPrefix(parsed.Path, "/executive/") && !strings.HasPrefix(parsed.Path, "/operational/") && !strings.HasPrefix(parsed.Path, "/financial/")) {
		return ""
	}
	return parsed.RequestURI()
}

func (a *portalApp) customerLogout(w http.ResponseWriter, r *http.Request) {
	record, recordErr := a.accessRecordFromRequest(r)
	for _, cookieName := range []string{accessCookieName, financialAccessCookieName, operationalAccessCookieName, "operational_session"} {
		http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: isSecureRequest(r), MaxAge: -1})
	}
	if recordErr == nil && effectiveAllowWeaving(record) {
		http.Redirect(w, r, strings.TrimRight(a.weavingAppURL, "/")+"/api/auth/portal-logout", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func moduleCookieName(module string) string {
	if module == "operational" {
		return operationalAccessCookieName
	}
	return financialAccessCookieName
}

func moduleTitle(module string) string {
	if module == "operational" {
		return "بخش عملیاتی"
	}
	if module == "weaving" {
		return "راندمان سالن بافت"
	}
	return "بخش مالی"
}

func moduleTarget(module string) string {
	if module == "operational" {
		return "/operational/"
	}
	if module == "weaving" {
		return "/module-login?module=weaving"
	}
	return "/financial/"
}

func moduleAllowed(record projectAccess, module string) bool {
	if module == "operational" {
		return effectiveAllowOperational(record)
	}
	if module == "weaving" {
		return effectiveAllowWeaving(record)
	}
	return module == "financial" && effectiveAllowFinancial(record)
}

func (a *portalApp) moduleRecordFromRequest(r *http.Request, module string) (projectAccess, error) {
	if module != "financial" && module != "operational" && module != "weaving" {
		return projectAccess{}, errors.New("invalid module")
	}
	cookie, err := r.Cookie(moduleCookieName(module))
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return projectAccess{}, errors.New("module login is required")
	}
	claims, err := a.verifyModuleSession(strings.TrimSpace(cookie.Value), module)
	if err != nil {
		return projectAccess{}, err
	}
	record, err := a.findAccessByToken(claims.AccessToken)
	if err != nil {
		return projectAccess{}, err
	}
	if record.ProjectKey != "textile-erp" || !record.IsActive || time.Now().After(record.ExpiresAt) || !moduleAllowed(record, module) {
		return projectAccess{}, errors.New("module access is not valid")
	}
	switch claims.AuthMode {
	case "password":
	case "single-user":
		if !a.canUseSingleUserModuleSSO(record) {
			return projectAccess{}, errors.New("password login is required in team mode")
		}
	case "portal":
	default:
		return projectAccess{}, errors.New("module session is not valid")
	}
	return record, nil
}

func (a *portalApp) authenticateTextileUser(username, password string) (projectAccess, error) {
	username = strings.TrimSpace(username)
	if a.localMode && strings.EqualFold(username, a.adminUsername) && password == a.adminPassword {
		return a.ensureLocalOwnerAccess()
	}
	items, err := a.listAccesses()
	if err != nil {
		return projectAccess{}, err
	}
	for _, record := range items {
		if record.ProjectKey != "textile-erp" || !record.IsActive || time.Now().After(record.ExpiresAt) || !strings.EqualFold(username, strings.TrimSpace(record.Username)) {
			continue
		}
		if a.verifyAccessPassword(record.AccessToken, password) == nil {
			return record, nil
		}
	}
	return projectAccess{}, errors.New("invalid username or password")
}

func (a *portalApp) canUseSingleUserModuleSSO(record projectAccess) bool {
	if effectiveAccessRole(record) != "owner" {
		return false
	}
	items, err := a.tenantAccesses(record)
	if err != nil {
		return false
	}
	for _, item := range items {
		if item.ID != record.ID {
			return false
		}
	}
	return true
}

func (a *portalApp) signModuleSession(module, authMode string, record projectAccess, expiresAt time.Time) (string, error) {
	if strings.TrimSpace(a.sessionSecret) == "" {
		return "", errors.New("portal session secret is not configured")
	}
	claims := moduleSessionClaims{
		AccessToken: record.AccessToken,
		Module:      module,
		AuthMode:    authMode,
		ExpiresAt:   expiresAt.Unix(),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(a.sessionSecret))
	_, _ = mac.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (a *portalApp) verifyModuleSession(value, module string) (moduleSessionClaims, error) {
	var claims moduleSessionClaims
	parts := strings.Split(value, ".")
	if len(parts) != 2 || strings.TrimSpace(a.sessionSecret) == "" {
		return claims, errors.New("module session is not valid")
	}
	providedSignature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return claims, errors.New("module session is not valid")
	}
	mac := hmac.New(sha256.New, []byte(a.sessionSecret))
	_, _ = mac.Write([]byte(parts[0]))
	if !hmac.Equal(providedSignature, mac.Sum(nil)) {
		return claims, errors.New("module session is not valid")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || json.Unmarshal(payload, &claims) != nil {
		return moduleSessionClaims{}, errors.New("module session is not valid")
	}
	if claims.Module != module || claims.ExpiresAt <= time.Now().Unix() || strings.TrimSpace(claims.AccessToken) == "" {
		return moduleSessionClaims{}, errors.New("module session is not valid")
	}
	return claims, nil
}

func (a *portalApp) setModuleAccessCookie(w http.ResponseWriter, r *http.Request, module, authMode string, record projectAccess) error {
	expiresAt := minTime(record.ExpiresAt, time.Now().Add(12*time.Hour))
	value, err := a.signModuleSession(module, authMode, record, expiresAt)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     moduleCookieName(module),
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isSecureRequest(r),
		Expires:  expiresAt,
	})
	return nil
}

func (a *portalApp) moduleLogin(w http.ResponseWriter, r *http.Request) {
	setPrivatePageHeaders(w)
	module := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("module")))
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err == nil {
			module = strings.ToLower(strings.TrimSpace(r.Form.Get("module")))
		}
	}
	if module != "financial" && module != "operational" && module != "weaving" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	nextValue := r.URL.Query().Get("next")
	if r.Method == http.MethodPost {
		nextValue = r.Form.Get("next")
	}
	nextPath := safePortalNext(nextValue)
	if nextPath == "" || !strings.HasPrefix(nextPath, moduleTarget(module)) {
		nextPath = moduleTarget(module)
	}
	if r.Method == http.MethodGet {
		if module == "weaving" {
			if record, err := a.accessRecordFromRequest(r); err == nil &&
				record.ProjectKey == "textile-erp" &&
				!record.MustChangePassword &&
				!accessRequiresSetup(record) &&
				moduleAllowed(record, module) {
				a.redirectToWeavingSSO(w, r, record)
				return
			}
			a.renderModuleLogin(w, module, nextPath, "")
			return
		}
		if _, err := a.moduleRecordFromRequest(r, module); err == nil {
			http.Redirect(w, r, nextPath, http.StatusSeeOther)
			return
		}
		if record, err := a.accessRecordFromRequest(r); err == nil &&
			record.ProjectKey == "textile-erp" &&
			!record.MustChangePassword &&
			!accessRequiresSetup(record) &&
			moduleAllowed(record, module) {
			if err := a.setModuleAccessCookie(w, r, module, "portal", record); err != nil {
				log.Printf("single-user module session failed for access=%d: %v", record.ID, err)
				a.renderModuleLogin(w, module, nextPath, "ورود خودکار برقرار نشد؛ با نام کاربری و رمز عبور وارد شوید.")
				return
			}
			_ = a.markAccessUsed(record.ID)
			http.Redirect(w, r, nextPath, http.StatusSeeOther)
			return
		}
		a.renderModuleLogin(w, module, nextPath, "")
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	username := strings.TrimSpace(r.Form.Get("username"))
	password := r.Form.Get("password")
	record, err := a.authenticateTextileUser(username, password)
	if err != nil {
		a.renderModuleLogin(w, module, nextPath, "نام کاربری یا رمز عبور صحیح نیست.")
		return
	}
	if accessRequiresSetup(record) {
		a.renderModuleLogin(w, module, nextPath, "حساب کاربر هنوز تکمیل نشده است. مدیر باید برای این کاربر نام کاربری و رمز عبور مشخص کند.")
		return
	}
	if record.MustChangePassword {
		record, _ = a.setAccessMustChangePassword(record.ID, false)
	}
	if !moduleAllowed(record, module) {
		a.renderModuleLogin(w, module, nextPath, "این کاربر اجازه ورود به "+moduleTitle(module)+" را ندارد.")
		return
	}
	a.setPortalAccessCookie(w, r, record.AccessToken, record.ExpiresAt)
	if module == "weaving" {
		_ = a.markAccessUsed(record.ID)
		a.redirectToWeavingSSO(w, r, record)
		return
	}
	if err := a.setModuleAccessCookie(w, r, module, "password", record); err != nil {
		log.Printf("password module session failed for access=%d: %v", record.ID, err)
		a.renderModuleLogin(w, module, nextPath, "نشست امن ورود ایجاد نشد؛ دوباره تلاش کنید.")
		return
	}
	_ = a.markAccessUsed(record.ID)
	http.Redirect(w, r, nextPath, http.StatusSeeOther)
}

func (a *portalApp) moduleLogout(w http.ResponseWriter, r *http.Request) {
	module := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("module")))
	if module != "financial" && module != "operational" {
		module = "financial"
	}
	cookieNames := []string{moduleCookieName(module), "operational_session"}
	if r.URL.Query().Get("login") == "1" {
		cookieNames = append(cookieNames, accessCookieName)
	}
	for _, name := range cookieNames {
		http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: isSecureRequest(r), MaxAge: -1})
	}
	if r.URL.Query().Get("login") == "1" {
		http.Redirect(w, r, "/module-login?module="+url.QueryEscape(module), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *portalApp) renderModuleLogin(w http.ResponseWriter, module, nextPath, errMsg string) {
	errorHTML := ""
	if errMsg != "" {
		errorHTML = `<div class="error">` + html.EscapeString(errMsg) + `</div>`
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html><html lang="fa" dir="rtl"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>ورود به ` + moduleTitle(module) + `</title><style>
*{box-sizing:border-box}body{margin:0;min-height:100vh;background:#0b1322;color:#eef2ff;font-family:Tahoma,Arial;display:flex;align-items:center;justify-content:center;padding:20px}.panel{width:min(450px,96vw);background:#111c2e;border:1px solid #334155;border-radius:18px;padding:28px;box-shadow:0 24px 80px #0006}h1{margin:0 0 10px}.lead{color:#a8bad3;line-height:1.9}.badge{display:inline-block;margin-bottom:16px;border-radius:999px;background:#1d4ed8;padding:7px 12px;font-size:12px;font-weight:bold}form{display:grid;gap:12px;margin-top:20px}input{width:100%;border:1px solid #475569;border-radius:11px;padding:13px;background:#07101f;color:white}button{border:0;border-radius:11px;padding:13px;background:#2563eb;color:white;font-weight:bold;cursor:pointer}.error{margin-top:14px;border:1px solid #b91c1c;border-radius:11px;background:#7f1d1d;padding:11px;color:#fee2e2}.links{display:flex;justify-content:space-between;margin-top:18px;font-size:13px}.links a{color:#93c5fd;text-decoration:none}
</style></head><body><main class="panel"><span class="badge">ورود مستقل و امن</span><h1>ورود به ` + moduleTitle(module) + `</h1><div class="lead">فقط کاربری که این بخش برای او فعال شده باشد می‌تواند وارد شود.</div>` + errorHTML + `<form method="post" action="/module-login?module=` + url.QueryEscape(module) + `"><input type="hidden" name="module" value="` + html.EscapeString(module) + `"><input type="hidden" name="next" value="` + html.EscapeString(nextPath) + `"><input name="username" placeholder="نام کاربری" autocomplete="username" required autofocus><input name="password" type="password" placeholder="رمز عبور" autocomplete="current-password" required><button type="submit">ورود به ` + moduleTitle(module) + `</button></form><div class="links"><a href="/">بازگشت به صفحه اصلی</a><a href="/team">مدیریت کاربران</a></div></main></body></html>`))
}

func (a *portalApp) renderCustomerLogin(w http.ResponseWriter, errMsg, nextPath string) {
	setPrivatePageHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	errorHTML := ""
	if errMsg != "" {
		errorHTML = `<div class="error">` + html.EscapeString(errMsg) + `</div>`
	}
	_, _ = w.Write([]byte(`<!doctype html>
<html lang="fa" dir="rtl">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>ورود ERP نساجی</title>
  <style>
    *{box-sizing:border-box}body{margin:0;min-height:100vh;background:#f7f1e8;color:#2a1a14;font-family:Tahoma,Arial;display:flex;align-items:center;justify-content:center;padding:24px}
    .panel{width:min(460px,96vw);background:#fffaf4;border:1px solid #dbc7ae;border-radius:20px;padding:28px;box-shadow:0 24px 80px rgba(75,43,24,.12)}
    h1{margin:0 0 10px;font-size:28px}.lead,.muted{color:#6f574a;line-height:1.9}.muted{font-size:13px;margin-top:14px}.plans-link{display:block;text-align:center;margin-top:16px;color:#6d28d9;font-weight:bold;text-decoration:none}
    form{display:grid;gap:12px;margin-top:20px}input{width:100%;border:1px solid #c8ab8b;border-radius:12px;padding:13px 14px;background:#fff;color:#2a1a14}
    button{border:1px solid #8b5e3c;background:#8b5e3c;color:#fff;border-radius:12px;padding:13px 16px;cursor:pointer;font-weight:bold}
    .error{margin-top:14px;background:#fff0f0;color:#9f2333;border:1px solid #efb0b0;border-radius:12px;padding:10px 12px}
  </style>
</head>
<body><main class="panel">
  <h1>ورود ERP نساجی</h1>
  <div class="lead">مدیر و کاربران مجموعه از همین صفحه وارد برنامه می‌شوند.</div>
  ` + errorHTML + `
  <form method="post" action="/login">
    <input type="hidden" name="next" value="` + html.EscapeString(nextPath) + `">
    <input name="username" placeholder="نام کاربری" autocomplete="username" required>
    <input name="password" type="password" placeholder="رمز عبور" autocomplete="current-password" required>
    <button type="submit">ورود به برنامه</button>
  </form>
  <div class="muted">نام کاربری و رمز عبور اختصاصی خود را از مدیر مجموعه دریافت کنید.</div>
  <a class="plans-link" href="/plans">هنوز حساب ندارید؟ محصولات را انتخاب و سفارش ثبت کنید</a>
</main></body></html>`))
}

func (a *portalApp) adminLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		a.renderAdminLogin(w, "")
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		a.renderAdminLogin(w, "درخواست معتبر نیست.")
		return
	}
	username := strings.TrimSpace(r.Form.Get("username"))
	password := r.Form.Get("password")
	if username != a.adminUsername || password != a.adminPassword {
		a.renderAdminLogin(w, "نام کاربری یا رمز عبور مدیریت اشتباه است.")
		return
	}
	a.setAdminSessionCookie(w, r, time.Now().Add(12*time.Hour))
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (a *portalApp) setAdminSessionCookie(w http.ResponseWriter, r *http.Request, exp time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     adminCookieName,
		Value:    a.signAdminSession(exp),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isSecureRequest(r),
		Expires:  exp,
	})
}

func (a *portalApp) adminLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     adminCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isSecureRequest(r),
		MaxAge:   -1,
	})
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

func (a *portalApp) adminPanel(w http.ResponseWriter, r *http.Request) {
	if !a.isAdminAuthenticated(r) {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`<!doctype html>
<html lang="fa" dir="rtl">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>پنل مدیریت دسترسی مشتریان</title>
  <style>
    *{box-sizing:border-box} body{margin:0;background:#f3f6fb;color:#17324d;font-family:Tahoma,Arial}
    .shell{max-width:1220px;margin:0 auto;padding:24px}
    .topbar{display:flex;justify-content:space-between;align-items:center;gap:12px;margin-bottom:20px}
    .title h1{margin:0;font-size:28px}.title p{margin:6px 0 0;color:#58708c;line-height:1.9}
    .actions{display:flex;gap:10px;flex-wrap:wrap}
    button,a.btn{border:1px solid #ced8e3;background:white;color:#16324d;border-radius:10px;padding:10px 14px;cursor:pointer;text-decoration:none}
    button.primary{background:#2563eb;border-color:#2563eb;color:white}
    button.warn{background:#fff7ed;border-color:#fdba74;color:#9a3412}
    button.danger{background:#fff1f2;border-color:#fda4af;color:#be123c}
    .grid{display:grid;grid-template-columns:380px 1fr;gap:18px}
    .panel{background:white;border:1px solid #dde5ef;border-radius:14px;padding:18px;box-shadow:0 10px 30px rgba(30,41,59,.05)}
    .panel h2{margin:0 0 12px;font-size:18px}
    .form-grid{display:grid;gap:12px}
    label{display:grid;gap:6px;font-size:14px}
    input,select,textarea{width:100%;border:1px solid #ced8e3;border-radius:10px;padding:11px 12px;font:inherit}
    textarea{min-height:92px;resize:vertical}
    .row{display:grid;grid-template-columns:1fr 1fr;gap:12px}
    .result{margin-top:14px;padding:14px;border-radius:12px;background:#eff6ff;border:1px solid #bfdbfe;display:none}
    .result.show{display:block}
    .result strong{display:block;margin-bottom:8px}
    .result code{display:block;direction:ltr;text-align:left;background:#0f172a;color:#e2e8f0;padding:10px;border-radius:10px;margin-top:8px;word-break:break-all}
    table{width:100%;border-collapse:collapse}
    th,td{padding:12px 10px;border-bottom:1px solid #edf2f7;text-align:right;vertical-align:top;font-size:14px}
    th{color:#58708c;font-weight:bold}
    .badge{display:inline-block;padding:5px 10px;border-radius:999px;font-size:12px}
    .active{background:#ecfdf5;color:#166534}
    .inactive{background:#fef2f2;color:#991b1b}
    .muted{color:#6b7c93}
	    .module-options{display:grid;gap:9px;padding:12px;border:1px solid #ced8e3;border-radius:12px;background:#f8fafc}.module-option{display:flex;align-items:center;gap:9px}.module-option input{width:auto}
    .table-actions{display:flex;gap:8px;flex-wrap:wrap}
    .empty{padding:24px;text-align:center;color:#6b7c93}
    @media(max-width:980px){.grid{grid-template-columns:1fr}.row{grid-template-columns:1fr}}
  </style>
</head>
<body>
  <div class="shell">
    <div class="topbar">
      <div class="title">
        <h1>پنل مدیریت دسترسی مشتریان</h1>
        <p>برای هر مشتری لینک تست بسازید، تاریخ انقضا تعیین کنید و او را به صفحه مناسب همان پروژه هدایت کنید.</p>
      </div>
      <div class="actions">
	    <a class="btn" href="/admin/orders">سفارش‌های خرید</a>
        <a class="btn" href="/" target="_blank" rel="noopener">صفحه اصلی</a>
        <a class="btn" href="/admin/logout">خروج</a>
      </div>
    </div>
    <div class="grid">
      <section class="panel">
        <h2 id="formTitle">ایجاد دسترسی جدید</h2>
        <form id="createForm" class="form-grid">
          <input type="hidden" name="access_id" value="">
          <label>پروژه
            <select name="project_key">
              <option value="textile-erp">Textile ERP</option>
              <option value="cooler-store">Cooler Store</option>
            </select>
          </label>
          <label id="financialCompanyField">شناسه شرکت مالی (Textile ERP)
            <input name="financial_company_id" type="number" min="1" placeholder="مثلا 1">
          </label>
	          <div id="moduleFields" class="module-options">
	            <strong>بخش‌های خریداری‌شده مشتری</strong>
	            <label class="module-option"><input name="allow_financial" type="checkbox" checked> بخش مالی</label>
	            <label class="module-option"><input name="allow_operational" type="checkbox"> بخش عملیاتی</label>
	            <label class="module-option"><input name="allow_weaving" type="checkbox"> راندمان سالن بافت</label>
	            <span class="muted">مرکز فرمان مدیر برای همه رایگان است و فقط همین بخش‌های فعال را نمایش می‌دهد.</span>
	          </div>
          <label>نام شرکت
            <input name="company_name" required placeholder="مثلا: شرکت بافت نوین">
          </label>
          <label>نام مخاطب
            <input name="contact_name" placeholder="مثلا: آقای احمدی">
          </label>
          <div class="row">
            <label>نام کاربری
              <input name="username" placeholder="manager-account">
            </label>
            <label>تعداد روز تست
              <input name="trial_days" type="number" min="1" value="30">
            </label>
          </div>
          <div class="row">
            <label>رمز عبور
              <input name="password" required placeholder="رمز عبور">
            </label>
            <label>تاریخ پایان
              <input name="expires_at" type="datetime-local">
            </label>
          </div>
          <div class="muted" id="passwordHelp">رمز عبور برای ارسال به مشتری و ورود از طریق لینک اختصاصی استفاده می‌شود.</div>
          <label>توضیحات
            <textarea name="notes" placeholder="یادداشت داخلی، توضیح فروش یا وضعیت مشتری"></textarea>
          </label>
          <div class="muted" id="tenantHelp">برای Textile ERP شناسه شرکت را مشخص کنید. داده‌های مالی و عملیاتی هر شرکت با جداسازی کامل فقط در اختیار کاربران همان شرکت قرار می‌گیرد.</div>
          <div class="actions">
            <button class="warn" type="button" id="suggestBtn">تولید یوزر و رمز</button>
            <button class="primary" type="submit" id="submitBtn">ایجاد دسترسی</button>
            <button type="button" id="cancelEditBtn" style="display:none">لغو ویرایش</button>
          </div>
        </form>
        <div id="resultBox" class="result">
          <strong>اطلاعات دسترسی ساخته شد</strong>
          <div id="resultText"></div>
          <input id="resultLinkInput" readonly dir="ltr" style="margin-top:10px">
          <a id="resultOpenLink" href="#" target="_blank" rel="noopener noreferrer" style="margin-top:10px;display:inline-flex;width:max-content">باز کردن لینک</a>
          <div class="actions" style="margin-top:10px">
            <button type="button" id="copyLinkBtn">کپی لینک</button>
            <button type="button" id="copyAllBtn">کپی همه اطلاعات</button>
          </div>
        </div>
      </section>
      <section class="panel">
        <div class="topbar" style="margin-bottom:12px">
          <div class="title">
            <h2>فهرست دسترسی‌ها</h2>
            <p class="muted">لینک‌های فعال، منقضی‌شده و وضعیت مشتریان تست</p>
          </div>
          <div class="actions">
            <button type="button" id="refreshBtn">به‌روزرسانی</button>
          </div>
        </div>
        <div style="overflow:auto">
          <table>
            <thead>
              <tr>
                <th>پروژه</th>
                <th>شرکت</th>
                <th>یوزر</th>
                <th>انقضا</th>
                <th>وضعیت</th>
                <th>لینک</th>
                <th>عملیات</th>
              </tr>
            </thead>
            <tbody id="rows">
              <tr><td class="empty" colspan="7">در حال بارگذاری...</td></tr>
            </tbody>
          </table>
        </div>
      </section>
    </div>
  </div>
  <script>
    const rows = document.getElementById('rows');
    const form = document.getElementById('createForm');
    const formTitle = document.getElementById('formTitle');
    const submitBtn = document.getElementById('submitBtn');
    const cancelEditBtn = document.getElementById('cancelEditBtn');
    const passwordHelp = document.getElementById('passwordHelp');
    const tenantHelp = document.getElementById('tenantHelp');
    const financialCompanyField = document.getElementById('financialCompanyField');
	    const moduleFields = document.getElementById('moduleFields');
    const projectKeyInput = form.querySelector('[name="project_key"]');
    const financialCompanyInput = form.querySelector('[name="financial_company_id"]');
    const resultBox = document.getElementById('resultBox');
    const resultText = document.getElementById('resultText');
    const resultLinkInput = document.getElementById('resultLinkInput');
    const resultOpenLink = document.getElementById('resultOpenLink');
    let accessCache = [];
    let latestResult = null;

    function token(len, alphabet) {
      let out = '';
      for (let i = 0; i < len; i++) out += alphabet[Math.floor(Math.random() * alphabet.length)];
      return out;
    }

    function suggestCredentials() {
      const company = form.company_name.value.trim().toLowerCase();
      const slug = company
        .normalize('NFKD')
        .replace(/[^\w\s-]/g, '')
        .replace(/[\s_]+/g, '-')
        .replace(/-+/g, '-')
        .replace(/^-|-$/g, '')
        .slice(0, 18) || 'trial';
      form.username.value = slug + '-' + token(5, 'abcdefghijkmnpqrstuvwxyz23456789');
      form.password.value = token(14, 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789!@#$%^&*');
    }

    function syncProjectFields() {
      const isTextile = projectKeyInput.value === 'textile-erp';
      financialCompanyField.style.display = isTextile ? 'grid' : 'none';
	      moduleFields.style.display = isTextile ? 'grid' : 'none';
      financialCompanyInput.required = isTextile;
      form.username.required = !isTextile;
      form.password.required = !isTextile;
      if (!isTextile) {
        financialCompanyInput.value = '';
      }
      tenantHelp.style.display = isTextile ? 'block' : 'none';
      passwordHelp.textContent = isTextile
        ? 'برای Textile ERP می‌توانید نام کاربری و رمز را خالی بگذارید تا مدیر شرکت در اولین ورود آن‌ها را ایجاد کند.'
        : 'Password is used for the direct private link sign-in.';
    }

    async function api(path, options = {}) {
      const response = await fetch(path, {
        headers: { 'Content-Type': 'application/json; charset=utf-8' },
        ...options,
      });
      const data = await response.json().catch(() => ({}));
      if (!response.ok) throw new Error(data.error || 'خطا در ارتباط با سرور');
      return data;
    }

    function badge(active, expired) {
      if (!active) return '<span class="badge inactive">غیرفعال</span>';
      if (expired) return '<span class="badge inactive">منقضی</span>';
      return '<span class="badge active">فعال</span>';
    }

    function formatDate(value) {
      if (!value) return '-';
      const date = new Date(value);
      return isNaN(date.getTime()) ? value : date.toLocaleString('fa-IR');
    }

    function normalizeAccess(row) {
      return {
        id: row.id,
        project_key: row.project_key || row.projectKey || '',
        project_label: row.project_label || row.projectLabel || '',
        company_name: row.company_name || row.companyName || '',
        contact_name: row.contact_name || row.contactName || '',
        username: row.username || '',
        password: row.password || '',
        financial_company_id: Number(row.financial_company_id || row.financialCompanyId || 0),
        access_role: row.access_role || row.accessRole || '',
        permissions: Array.isArray(row.permissions) ? row.permissions : [],
        can_manage_team: typeof row.can_manage_team === 'boolean' ? row.can_manage_team : !!row.canManageTeam,
	        allow_financial: typeof row.allow_financial === 'boolean' ? row.allow_financial : !!row.allowFinancial,
	        allow_operational: typeof row.allow_operational === 'boolean' ? row.allow_operational : !!row.allowOperational,
	        allow_weaving: typeof row.allow_weaving === 'boolean' ? row.allow_weaving : !!row.allowWeaving,
	        module_access_label: row.module_access_label || row.moduleAccessLabel || '',
        requires_setup: typeof row.requires_setup === 'boolean' ? row.requires_setup : !!row.requiresSetup,
		trial_active: typeof row.trial_active === 'boolean' ? row.trial_active : !!row.trialActive,
		trial_ends_at: row.trial_ends_at || row.trialEndsAt || '',
		trial_days_remaining: Number(row.trial_days_remaining || row.trialDaysRemaining || 0),
        expires_at: row.expires_at || row.expiresAt || '',
        access_link: row.access_link || row.accessLink || '',
        access_token: row.access_token || row.accessToken || '',
        notes: row.notes || '',
        is_active: typeof row.is_active === 'boolean' ? row.is_active : !!row.isActive,
        is_expired: typeof row.is_expired === 'boolean' ? row.is_expired : !!row.isExpired,
      };
    }

    function toLocalInputValue(value) {
      if (!value) return '';
      const date = new Date(value);
      if (isNaN(date.getTime())) return '';
      const pad = (n) => String(n).padStart(2, '0');
      return date.getFullYear() + '-' + pad(date.getMonth() + 1) + '-' + pad(date.getDate()) + 'T' + pad(date.getHours()) + ':' + pad(date.getMinutes());
    }

    function resetFormState() {
      form.reset();
      form.access_id.value = '';
      form.trial_days.value = '30';
      financialCompanyInput.value = '';
	      form.allow_financial.checked = true;
	      form.allow_operational.checked = false;
	      form.allow_weaving.checked = false;
      formTitle.textContent = 'ایجاد دسترسی جدید';
      submitBtn.textContent = 'ایجاد دسترسی';
      cancelEditBtn.style.display = 'none';
      form.password.value = '';
      form.password.placeholder = 'Password';
      form.username.placeholder = 'manager-account';
      resultBox.classList.remove('show');
      syncProjectFields();
    }

    function startEdit(row) {
      form.access_id.value = row.id;
      form.project_key.value = row.project_key;
      form.company_name.value = row.company_name;
      form.contact_name.value = row.contact_name || '';
      form.username.value = row.username;
      financialCompanyInput.value = row.financial_company_id > 0 ? String(row.financial_company_id) : '';
	      form.allow_financial.checked = row.allow_financial;
	      form.allow_operational.checked = row.allow_operational;
	      form.allow_weaving.checked = row.allow_weaving;
      form.trial_days.value = '30';
      form.expires_at.value = toLocalInputValue(row.expires_at);
      form.notes.value = row.notes || '';
      form.password.value = row.password || '';
      form.password.placeholder = row.requires_setup ? 'مدیر شرکت در اولین ورود تعیین می‌کند' : 'برای حفظ رمز فعلی خالی بگذارید';
      formTitle.textContent = 'ویرایش دسترسی';
      submitBtn.textContent = 'ذخیره تغییرات';
      cancelEditBtn.style.display = 'inline-flex';
      syncProjectFields();
      window.scrollTo({ top: 0, behavior: 'smooth' });
    }

    function buildCustomerInfo(row) {
      return [
        'پروژه: ' + row.project_label,
        'شرکت: ' + row.company_name,
        'نام کاربری: ' + row.username,
        'رمز عبور: ' + (row.password || 'برای این رکورد رمز ذخیره نشده است؛ یک ویرایش با رمز جدید انجام دهید.'),
	        'بخش‌های خریداری‌شده: ' + (row.module_access_label || '-'),
        'انقضا: ' + formatDate(row.expires_at),
        'لینک: ' + row.access_link,
      ].join('\n');
    }

    async function loadRows() {
      const data = await api('/admin/api/accesses');
      const list = (data.items || []).map(normalizeAccess);
      accessCache = list;
      if (!list.length) {
        rows.innerHTML = '<tr><td class="empty" colspan="7">هیچ دسترسی‌ای ثبت نشده است.</td></tr>';
        return;
      }
      rows.innerHTML = list.map(function(row) {
        return '<tr>'
          + '<td>' + row.project_label + '</td>'
          + '<td><strong>' + row.company_name + '</strong><div class="muted">' + (row.contact_name || '-') + '</div></td>'
	          + '<td>' + row.username + '<div class="muted">' + row.module_access_label + '</div>' + (row.trial_active ? '<span class="badge active">تست کامل · '+row.trial_days_remaining+' روز</span>' : '') + '</td>'
          + '<td>' + formatDate(row.expires_at) + '</td>'
          + '<td>' + badge(row.is_active, row.is_expired) + '</td>'
          + '<td><div class="table-actions">'
          + '<button type="button" data-copy="' + encodeURIComponent(row.access_link) + '">کپی لینک</button>'
          + '<button type="button" data-copy-info="' + row.id + '">کپی اطلاعات</button>'
          + '</div></td>'
          + '<td><div class="table-actions">'
          + '<button type="button" data-edit="' + row.id + '">ویرایش</button>'
		  + (row.project_key === 'textile-erp' && row.access_role === 'owner' ? '<button class="warn" type="button" data-trial="' + row.id + '">' + (row.trial_active ? 'تمدید تست ۳۰روزه' : 'فعال‌سازی تست ۳۰روزه') + '</button>' : '')
          + '<button type="button" data-toggle="' + row.id + '">' + (row.is_active ? 'غیرفعال‌کردن' : 'فعال‌کردن') + '</button>'
          + '<button class="danger" type="button" data-delete="' + row.id + '">حذف</button>'
          + '</div></td>'
          + '</tr>';
      }).join('');
    }

    function showResult(data) {
      latestResult = normalizeAccess(data);
      const passwordText = latestResult.requires_setup ? 'مدیر شرکت در اولین ورود نام کاربری و رمز عبور را ایجاد می‌کند.' : (latestResult.password || '-');
      resultText.innerHTML =
        '<div>پروژه: <strong>' + latestResult.project_label + '</strong></div>' +
        '<div>شرکت: <strong>' + latestResult.company_name + '</strong></div>' +
        '<div>نام کاربری: <strong>' + (latestResult.username || 'در انتظار ایجاد حساب') + '</strong></div>' +
        '<div>رمز عبور: <strong>' + passwordText + '</strong></div>' +
	    '<div>بخش‌های خریداری‌شده: <strong>' + (latestResult.module_access_label || '-') + '</strong></div>' +
        '<div>تاریخ انقضا: <strong>' + formatDate(latestResult.expires_at) + '</strong></div>';
      resultLinkInput.value = latestResult.access_link;
      resultOpenLink.href = latestResult.access_link;
      resultBox.classList.add('show');
    }

    async function copyText(text) {
      const value = String(text || '');
      if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(value);
        return;
      }
      const input = document.createElement('textarea');
      input.value = value;
      input.setAttribute('readonly', '');
      input.style.position = 'fixed';
      input.style.top = '-9999px';
      input.style.opacity = '0';
      document.body.appendChild(input);
      input.focus();
      input.select();
      const ok = document.execCommand('copy');
      document.body.removeChild(input);
      if (!ok) throw new Error('کپی خودکار در این مرورگر انجام نشد.');
    }

    document.getElementById('suggestBtn').addEventListener('click', suggestCredentials);
    document.getElementById('refreshBtn').addEventListener('click', () => loadRows().catch((err) => alert(err.message)));
    projectKeyInput.addEventListener('change', syncProjectFields);
    document.getElementById('copyLinkBtn').addEventListener('click', async () => {
      if (!latestResult) return;
      try {
        await copyText(latestResult.access_link);
        alert('لینک کپی شد.');
      } catch (error) {
        resultLinkInput.focus();
        resultLinkInput.select();
        alert(error.message + ' لینک انتخاب شد؛ اکنون کپی کنید.');
      }
    });
    document.getElementById('copyAllBtn').addEventListener('click', async () => {
      if (!latestResult) return;
      try {
        await copyText(buildCustomerInfo(latestResult));
        alert('همه اطلاعات کپی شد.');
      } catch (error) {
        resultLinkInput.focus();
        resultLinkInput.select();
        alert(error.message + ' ابتدا لینک انتخاب شد.');
      }
    });
    cancelEditBtn.addEventListener('click', () => resetFormState());

    form.addEventListener('submit', async (event) => {
      event.preventDefault();
      try {
	        if (projectKeyInput.value === 'textile-erp' && !form.access_id.value && (!form.username.value.trim() || !form.password.value.trim())) suggestCredentials();
        const formData = new FormData(form);
        const accessID = String(formData.get('access_id') || '').trim();
        const editingRow = accessCache.find((item) => String(item.id) === accessID);
        const projectKey = formData.get('project_key');
        const username = String(formData.get('username') || '').trim();
        const password = String(formData.get('password') || '').trim();
	        const requiresSetup = false;
	        const allowFinancial = form.allow_financial.checked;
	        const allowOperational = form.allow_operational.checked;
	        const allowWeaving = form.allow_weaving.checked;
	        if (projectKey === 'textile-erp' && !allowFinancial && !allowOperational && !allowWeaving) throw new Error('حداقل یکی از سه بخش را برای مشتری فعال کنید.');
        const payload = {
          projectKey,
          companyName: formData.get('company_name'),
          contactName: formData.get('contact_name'),
          username,
          password,
          financialCompanyId: Number(formData.get('financial_company_id') || 0),
          trialDays: Number(formData.get('trial_days') || 30),
          expiresAt: formData.get('expires_at'),
          notes: formData.get('notes'),
          requiresSetup,
          accessRole: editingRow ? editingRow.access_role : (projectKey === 'textile-erp' ? 'owner' : 'customer'),
          permissions: editingRow ? editingRow.permissions : [],
          canManageTeam: editingRow ? editingRow.can_manage_team : (projectKey === 'textile-erp'),
	          allowFinancial,
	          allowOperational,
	          allowWeaving,
        };
        const isEdit = accessID !== '';
        const data = await api(isEdit ? '/admin/api/accesses/' + accessID : '/admin/api/accesses', {
          method: isEdit ? 'PUT' : 'POST',
          body: JSON.stringify(payload),
        });
        showResult(data);
        await loadRows();
        resetFormState();
      } catch (error) {
        alert(error.message);
      }
    });

    rows.addEventListener('click', async (event) => {
      const copy = event.target.closest('[data-copy]');
      if (copy) {
        try {
          await copyText(decodeURIComponent(copy.dataset.copy));
          alert('لینک کپی شد.');
        } catch (error) {
          resultLinkInput.value = decodeURIComponent(copy.dataset.copy);
          resultLinkInput.focus();
          resultLinkInput.select();
          alert(error.message + ' لینک انتخاب شد.');
        }
        return;
      }
      const copyInfo = event.target.closest('[data-copy-info]');
      if (copyInfo) {
        const row = accessCache.find((item) => String(item.id) === copyInfo.dataset.copyInfo);
        if (!row) return;
        try {
          await copyText(buildCustomerInfo(row));
          alert('اطلاعات مشتری کپی شد.');
        } catch (error) {
          alert(error.message);
        }
        return;
      }
      const edit = event.target.closest('[data-edit]');
      if (edit) {
        const row = accessCache.find((item) => String(item.id) === edit.dataset.edit);
        if (row) startEdit(row);
        return;
      }
      const toggle = event.target.closest('[data-toggle]');
      if (toggle) {
        await api('/admin/api/accesses/' + toggle.dataset.toggle + '/toggle', { method: 'POST' });
        await loadRows();
        return;
      }
	  const trial = event.target.closest('[data-trial]');
	  if (trial) {
		if (!confirm('تست رایگان هر سه بخش برای ۳۰ روز کامل روی همین حساب فعال شود؟')) return;
		const data = await api('/admin/api/accesses/' + trial.dataset.trial + '/trial', { method: 'POST', body: JSON.stringify({ days: 30 }) });
		showResult(data);
		await loadRows();
		return;
	  }
      const del = event.target.closest('[data-delete]');
      if (del && confirm('این دسترسی حذف شود؟')) {
        await api('/admin/api/accesses/' + del.dataset.delete, { method: 'DELETE' });
        await loadRows();
      }
    });

    resetFormState();
    loadRows().catch((err) => {
      rows.innerHTML = '<tr><td class="empty" colspan="7">' + err.message + '</td></tr>';
    });
  </script>
</body>
</html>`))
}

type accessRequest struct {
	ProjectKey          string   `json:"projectKey"`
	ProjectKey2         string   `json:"project_key"`
	CompanyName         string   `json:"companyName"`
	CompanyName2        string   `json:"company_name"`
	ContactName         string   `json:"contactName"`
	ContactName2        string   `json:"contact_name"`
	Username            string   `json:"username"`
	Password            string   `json:"password"`
	FinancialCompanyID  int64    `json:"financialCompanyId"`
	FinancialCompanyID2 int64    `json:"financial_company_id"`
	TrialDays           int      `json:"trialDays"`
	TrialDays2          int      `json:"trial_days"`
	ExpiresAt           string   `json:"expiresAt"`
	ExpiresAt2          string   `json:"expires_at"`
	Notes               string   `json:"notes"`
	AccessRole          string   `json:"accessRole"`
	AccessRole2         string   `json:"access_role"`
	Permissions         []string `json:"permissions"`
	CanManageTeam       *bool    `json:"canManageTeam"`
	CanManageTeam2      *bool    `json:"can_manage_team"`
	RequiresSetup       *bool    `json:"requiresSetup"`
	RequiresSetup2      *bool    `json:"requires_setup"`
	MustChangePassword  *bool    `json:"mustChangePassword"`
	MustChangePassword2 *bool    `json:"must_change_password"`
	ForcePasswordChange *bool    `json:"forcePasswordChange"`
	AllowFinancial      *bool    `json:"allowFinancial"`
	AllowFinancial2     *bool    `json:"allow_financial"`
	AllowOperational    *bool    `json:"allowOperational"`
	AllowOperational2   *bool    `json:"allow_operational"`
	AllowWeaving        *bool    `json:"allowWeaving"`
	AllowWeaving2       *bool    `json:"allow_weaving"`
}

func decodeAccessRequest(r *http.Request) (accessRequest, error) {
	var req accessRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return accessRequest{}, err
	}
	if req.ProjectKey == "" {
		req.ProjectKey = req.ProjectKey2
	}
	if req.CompanyName == "" {
		req.CompanyName = req.CompanyName2
	}
	if req.ContactName == "" {
		req.ContactName = req.ContactName2
	}
	if req.FinancialCompanyID == 0 {
		req.FinancialCompanyID = req.FinancialCompanyID2
	}
	if req.TrialDays == 0 {
		req.TrialDays = req.TrialDays2
	}
	if req.ExpiresAt == "" {
		req.ExpiresAt = req.ExpiresAt2
	}
	if req.AccessRole == "" {
		req.AccessRole = req.AccessRole2
	}
	if req.CanManageTeam == nil {
		req.CanManageTeam = req.CanManageTeam2
	}
	if req.RequiresSetup == nil {
		req.RequiresSetup = req.RequiresSetup2
	}
	if req.MustChangePassword == nil {
		req.MustChangePassword = req.MustChangePassword2
	}
	if req.MustChangePassword == nil {
		req.MustChangePassword = req.ForcePasswordChange
	}
	if req.AllowFinancial == nil {
		req.AllowFinancial = req.AllowFinancial2
	}
	if req.AllowOperational == nil {
		req.AllowOperational = req.AllowOperational2
	}
	if req.AllowWeaving == nil {
		req.AllowWeaving = req.AllowWeaving2
	}
	req.ProjectKey = strings.TrimSpace(req.ProjectKey)
	req.CompanyName = strings.TrimSpace(req.CompanyName)
	req.ContactName = strings.TrimSpace(req.ContactName)
	req.Username = strings.TrimSpace(req.Username)
	req.Password = strings.TrimSpace(req.Password)
	req.Notes = strings.TrimSpace(req.Notes)
	req.AccessRole = strings.TrimSpace(req.AccessRole)
	return req, nil
}

func (a *portalApp) adminDeprovision(w http.ResponseWriter, r *http.Request) {
	if !a.isAdminAuthenticated(r) {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload struct {
		Username string `json:"username"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&payload); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	username := strings.TrimSpace(payload.Username)
	if username == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "username is required"})
		return
	}
	if err := a.deprovisionTextileTenant(username); err != nil {
		respondJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	items, err := a.listAccesses()
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	for _, item := range items {
		if item.ProjectKey == "textile-erp" && strings.EqualFold(strings.TrimSpace(item.Username), username) {
			if err := a.deleteAccess(item.ID); err != nil {
				respondJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
				return
			}
		}
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *portalApp) deprovisionTextileTenant(username string) error {
	payload, err := json.Marshal(map[string]string{"username": strings.TrimSpace(username)})
	if err != nil {
		return err
	}
	endpoint := strings.TrimRight(a.operationalAPI, "/") + "/api/portal/deprovision"
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Operational-Portal-Secret", a.operationalSessionSecret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var result struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !result.Success {
		if strings.TrimSpace(result.Error) == "" {
			result.Error = fmt.Sprintf("operational tenant deprovisioning failed with status %d", resp.StatusCode)
		}
		return errors.New(result.Error)
	}
	return nil
}

func (a *portalApp) issueLaunchTicket(accessToken, module string) (string, time.Time, error) {
	accessToken = strings.TrimSpace(accessToken)
	module = strings.ToLower(strings.TrimSpace(module))
	record, err := a.findAccessByToken(accessToken)
	if err != nil {
		return "", time.Time{}, err
	}
	if record.ProjectKey != "textile-erp" || !record.IsActive || time.Now().After(record.ExpiresAt) {
		return "", time.Time{}, errors.New("access is not active")
	}
	// The launch endpoint is admin-authenticated and is called only after the
	// customer has authenticated in the central Viora portal. Legacy setup and
	// password-change flags must not force that customer through a second login.
	if strings.TrimSpace(record.Username) == "" || strings.TrimSpace(record.PasswordHash) == "" {
		return "", time.Time{}, errors.New("access setup is incomplete")
	}
	if module != "" {
		if module != "financial" && module != "operational" && module != "weaving" {
			return "", time.Time{}, errors.New("launch module is invalid")
		}
		if !moduleAllowed(record, module) {
			return "", time.Time{}, errors.New("module access is not active")
		}
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	ticket := base64.RawURLEncoding.EncodeToString(raw)
	expiresAt := time.Now().Add(2 * time.Minute)

	a.launchTicketMu.Lock()
	defer a.launchTicketMu.Unlock()
	for key, item := range a.launchTickets {
		if time.Now().After(item.ExpiresAt) {
			delete(a.launchTickets, key)
		}
	}
	a.launchTickets[ticket] = launchTicket{AccessToken: accessToken, Module: module, ExpiresAt: expiresAt}
	return ticket, expiresAt, nil
}

func (a *portalApp) consumeLaunchTicket(ticket string) (projectAccess, string, error) {
	ticket = strings.TrimSpace(ticket)
	if ticket == "" {
		return projectAccess{}, "", errors.New("launch ticket is missing")
	}

	a.launchTicketMu.Lock()
	item, ok := a.launchTickets[ticket]
	if ok {
		delete(a.launchTickets, ticket)
	}
	a.launchTicketMu.Unlock()
	if !ok || time.Now().After(item.ExpiresAt) {
		return projectAccess{}, "", errors.New("launch ticket is invalid or expired")
	}

	record, err := a.findAccessByToken(item.AccessToken)
	if err != nil {
		return projectAccess{}, "", err
	}
	if record.ProjectKey != "textile-erp" || !record.IsActive || time.Now().After(record.ExpiresAt) {
		return projectAccess{}, "", errors.New("access is not active")
	}
	if strings.TrimSpace(record.Username) == "" || strings.TrimSpace(record.PasswordHash) == "" {
		return projectAccess{}, "", errors.New("access setup is incomplete")
	}
	if item.Module != "" && !moduleAllowed(record, item.Module) {
		return projectAccess{}, "", errors.New("module access is not active")
	}
	return record, item.Module, nil
}

func (a *portalApp) adminLaunchTicket(w http.ResponseWriter, r *http.Request) {
	if !a.isAdminAuthenticated(r) {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload struct {
		AccessToken string `json:"accessToken"`
		Module      string `json:"module"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&payload); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	ticket, expiresAt, err := a.issueLaunchTicket(payload.AccessToken, payload.Module)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	respondJSON(w, http.StatusCreated, map[string]any{
		"ticket":    ticket,
		"expiresAt": expiresAt.UTC().Format(time.RFC3339),
	})
}

func (a *portalApp) launchEntry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store, no-cache, max-age=0")
	w.Header().Set("Referrer-Policy", "no-referrer")
	ticket := strings.Trim(strings.TrimPrefix(r.URL.Path, "/launch/"), "/")
	record, module, err := a.consumeLaunchTicket(ticket)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	a.setPortalAccessCookie(w, r, record.AccessToken, record.ExpiresAt)
	_ = a.markAccessUsed(record.ID)
	if module == "weaving" {
		a.redirectToWeavingSSO(w, r, record)
		return
	}
	if module == "financial" || module == "operational" {
		http.Redirect(w, r, "/module-login?module="+url.QueryEscape(module), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, a.accessTarget(record), http.StatusSeeOther)
}

func (a *portalApp) adminAccesses(w http.ResponseWriter, r *http.Request) {
	if !a.isAdminAuthenticated(r) {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := a.listAccesses()
		if err != nil {
			respondJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		out := make([]map[string]any, 0, len(items))
		for _, item := range items {
			out = append(out, a.accessResponse(item, a.mustDecryptPassword(item)))
		}
		respondJSON(w, http.StatusOK, map[string]any{"items": out})
	case http.MethodPost:
		req, err := decodeAccessRequest(r)
		if err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		requiresSetup := boolPtrValue(req.RequiresSetup, req.ProjectKey == "textile-erp")
		canManageTeam := boolPtrValue(req.CanManageTeam, req.ProjectKey == "textile-erp")
		allowFinancial := boolPtrValue(req.AllowFinancial, req.ProjectKey == "textile-erp")
		allowOperational := boolPtrValue(req.AllowOperational, req.ProjectKey == "textile-erp")
		allowWeaving := boolPtrValue(req.AllowWeaving, false)
		accessRole := strings.TrimSpace(req.AccessRole)
		if accessRole == "" && req.ProjectKey == "textile-erp" {
			accessRole = "owner"
		}
		if req.ProjectKey == "" || req.CompanyName == "" {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "project and company name are required"})
			return
		}
		if !requiresSetup && (req.Username == "" || req.Password == "") {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "username and password are required when setup is disabled"})
			return
		}
		expiresAt, err := resolveExpiry(req.ExpiresAt, req.TrialDays)
		if err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		access, rawPassword, err := a.createManagedAccess(req.ProjectKey, req.CompanyName, req.ContactName, req.Username, req.Password, req.FinancialCompanyID, expiresAt, time.Time{}, req.Notes, accessRole, req.Permissions, canManageTeam, requiresSetup, allowFinancial, allowOperational, allowWeaving)
		if err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if req.MustChangePassword != nil && !requiresSetup && access.MustChangePassword != *req.MustChangePassword {
			access, err = a.setAccessMustChangePassword(access.ID, *req.MustChangePassword)
			if err != nil {
				respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
		}
		respondJSON(w, http.StatusCreated, a.accessResponse(access, rawPassword))
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
func (a *portalApp) adminAccessByID(w http.ResponseWriter, r *http.Request) {
	if !a.isAdminAuthenticated(r) {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/admin/api/accesses/"), "/")
	if path == "" {
		http.NotFound(w, r)
		return
	}
	parts := strings.Split(path, "/")
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid access id"})
		return
	}
	if len(parts) == 1 && r.Method == http.MethodDelete {
		if err := a.deleteAccess(id); err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		respondJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	if len(parts) == 1 && r.Method == http.MethodPut {
		req, err := decodeAccessRequest(r)
		if err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		requiresSetup := boolPtrValue(req.RequiresSetup, req.ProjectKey == "textile-erp")
		canManageTeam := boolPtrValue(req.CanManageTeam, req.ProjectKey == "textile-erp")
		allowFinancial := boolPtrValue(req.AllowFinancial, req.ProjectKey == "textile-erp")
		allowOperational := boolPtrValue(req.AllowOperational, req.ProjectKey == "textile-erp")
		allowWeaving := boolPtrValue(req.AllowWeaving, false)
		accessRole := strings.TrimSpace(req.AccessRole)
		if accessRole == "" && req.ProjectKey == "textile-erp" {
			accessRole = "owner"
		}
		if req.ProjectKey == "" || req.CompanyName == "" {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "project and company name are required"})
			return
		}
		if !requiresSetup && req.Username == "" {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "username is required when setup is disabled"})
			return
		}
		if req.ProjectKey == "textile-erp" && req.FinancialCompanyID <= 0 {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "financial company id is required for textile customers"})
			return
		}
		expiresAt, err := resolveExpiry(req.ExpiresAt, req.TrialDays)
		if err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		access, rawPassword, err := a.updateManagedAccess(id, req.ProjectKey, req.CompanyName, req.ContactName, req.Username, req.Password, req.FinancialCompanyID, expiresAt, time.Time{}, req.Notes, accessRole, req.Permissions, canManageTeam, requiresSetup, allowFinancial, allowOperational, allowWeaving)
		if err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if req.MustChangePassword != nil && !requiresSetup && access.MustChangePassword != *req.MustChangePassword {
			access, err = a.setAccessMustChangePassword(access.ID, *req.MustChangePassword)
			if err != nil {
				respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
		}
		respondJSON(w, http.StatusOK, a.accessResponse(access, rawPassword))
		return
	}
	if len(parts) == 2 && parts[1] == "toggle" && r.Method == http.MethodPost {
		if err := a.toggleAccess(id); err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		respondJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	if len(parts) == 2 && parts[1] == "trial" && r.Method == http.MethodPost {
		var body struct {
			Days int `json:"days"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&body)
		if body.Days == 0 {
			body.Days = fullTrialDays
		}
		access, rawPassword, err := a.grantFullTrial(id, body.Days)
		if err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		respondJSON(w, http.StatusOK, a.accessResponse(access, rawPassword))
		return
	}
	http.NotFound(w, r)
}
func (a *portalApp) accessEntry(w http.ResponseWriter, r *http.Request) {
	token := strings.Trim(strings.TrimPrefix(r.URL.Path, "/access/"), "/")
	if token == "" {
		http.NotFound(w, r)
		return
	}
	record, err := a.findAccessByToken(token)
	if err != nil {
		a.renderAccessPage(w, nil, "لینک دسترسی پیدا نشد.")
		return
	}
	if !record.IsActive {
		a.renderAccessPage(w, &record, "این لینک دسترسی در حال حاضر غیرفعال است.")
		return
	}
	if time.Now().After(record.ExpiresAt) {
		a.renderAccessPage(w, &record, "مهلت این لینک دسترسی به پایان رسیده است.")
		return
	}

	if accessRequiresSetup(record) {
		if r.Method == http.MethodPost {
			if err := r.ParseForm(); err != nil {
				a.renderAccessPage(w, &record, "درخواست ارسالی معتبر نیست.")
				return
			}
			password := strings.TrimSpace(r.Form.Get("password"))
			confirmPassword := strings.TrimSpace(r.Form.Get("confirm_password"))
			if password != confirmPassword {
				a.renderAccessPage(w, &record, "تکرار رمز عبور با رمز وارد شده یکسان نیست.")
				return
			}
			updated, err := a.finalizeAccessSetup(token, r.Form.Get("contact_name"), r.Form.Get("username"), password)
			if err != nil {
				a.renderAccessPage(w, &record, err.Error())
				return
			}
			record = updated
			a.setPortalAccessCookie(w, r, token, record.ExpiresAt)
			_ = a.markAccessUsed(record.ID)
			http.Redirect(w, r, a.accessTarget(record), http.StatusSeeOther)
			return
		}
		a.renderAccessPage(w, &record, "")
		return
	}
	if record.MustChangePassword {
		if r.Method != http.MethodPost {
			a.renderAccessPage(w, &record, "")
			return
		}
		if err := r.ParseForm(); err != nil {
			a.renderAccessPage(w, &record, "درخواست ارسالی معتبر نیست.")
			return
		}
		username := strings.TrimSpace(r.Form.Get("username"))
		currentPassword := r.Form.Get("current_password")
		newPassword := strings.TrimSpace(r.Form.Get("new_password"))
		confirmPassword := strings.TrimSpace(r.Form.Get("confirm_password"))
		if !strings.EqualFold(username, strings.TrimSpace(record.Username)) || a.verifyAccessPassword(token, currentPassword) != nil {
			a.renderAccessPage(w, &record, "نام کاربری یا رمز موقت صحیح نیست.")
			return
		}
		if newPassword != confirmPassword {
			a.renderAccessPage(w, &record, "تکرار رمز عبور جدید یکسان نیست.")
			return
		}
		if subtleConstantTimeCompare(currentPassword, newPassword) {
			a.renderAccessPage(w, &record, "رمز عبور جدید باید با رمز موقت متفاوت باشد.")
			return
		}
		updated, err := a.changeTemporaryPassword(token, newPassword)
		if err != nil {
			a.renderAccessPage(w, &record, err.Error())
			return
		}
		record = updated
		a.setPortalAccessCookie(w, r, token, record.ExpiresAt)
		_ = a.markAccessUsed(record.ID)
		http.Redirect(w, r, a.accessTarget(record), http.StatusSeeOther)
		return
	}

	if r.Method == http.MethodGet && a.accessCookieValid(r, token) {
		http.Redirect(w, r, a.accessTarget(record), http.StatusSeeOther)
		return
	}
	if r.Method != http.MethodPost {
		a.renderAccessPage(w, &record, "")
		return
	}
	if err := r.ParseForm(); err != nil {
		a.renderAccessPage(w, &record, "درخواست ارسالی معتبر نیست.")
		return
	}
	username := strings.TrimSpace(r.Form.Get("username"))
	if !strings.EqualFold(username, strings.TrimSpace(record.Username)) || a.verifyAccessPassword(token, r.Form.Get("password")) != nil {
		a.renderAccessPage(w, &record, "نام کاربری یا رمز عبور صحیح نیست.")
		return
	}
	a.setPortalAccessCookie(w, r, token, record.ExpiresAt)
	_ = a.markAccessUsed(record.ID)
	http.Redirect(w, r, a.accessTarget(record), http.StatusSeeOther)
}
func (a *portalApp) portalFinancialSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	record, err := a.moduleRecordFromRequest(r, "financial")
	if err != nil {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "access session is not valid"})
		return
	}
	if record.ProjectKey != "textile-erp" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "financial session is only available for textile-erp"})
		return
	}
	if !effectiveAllowFinancial(record) {
		respondJSON(w, http.StatusForbidden, map[string]string{"error": "financial access is not enabled for this user"})
		return
	}
	token, err := a.signFinancialJWT(record)
	if err != nil {
		respondJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"token":         token,
		"username":      record.Username,
		"displayName":   record.ContactName,
		"company":       record.CompanyName,
		"projectKey":    record.ProjectKey,
		"portalRole":    effectiveAccessRole(record),
		"permissions":   effectivePermissions(record),
		"canManageTeam": effectiveCanManageTeam(record),
		"expiresAt":     minTime(record.ExpiresAt, time.Now().Add(15*time.Minute)).UTC().Format(time.RFC3339),
	})
}
func (a *portalApp) portalOperationalSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !a.allowOperationalCustomerAccess {
		respondJSON(w, http.StatusForbidden, map[string]string{"error": "operational customer access is disabled"})
		return
	}
	record, err := a.moduleRecordFromRequest(r, "operational")
	if err != nil {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "access session is not valid"})
		return
	}
	if record.ProjectKey != "textile-erp" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "operational session is only available for textile-erp"})
		return
	}
	if !effectiveAllowOperational(record) {
		respondJSON(w, http.StatusForbidden, map[string]string{"error": "operational access is not enabled for this user"})
		return
	}
	loginData, err := a.createOperationalSessionForRecord(w, r, record)
	if err != nil {
		respondJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	menus := loginData["menus"]
	if menuItems, ok := menus.([]any); !ok || len(menuItems) == 0 {
		menus = operationalPortalMenusForKeys(operationalMenuKeys(record))
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"user": map[string]any{
			"id":       record.ID,
			"username": record.Username,
			"role":     "customer",
			"company":  record.CompanyName,
		},
		"menus": menus,
	})
}

func (a *portalApp) createOperationalSession(w http.ResponseWriter, r *http.Request) (map[string]any, error) {
	record, err := a.accessRecordFromRequest(r)
	if err != nil {
		return nil, err
	}
	return a.createOperationalSessionForRecord(w, r, record)
}

func (a *portalApp) createOperationalSessionForRecord(w http.ResponseWriter, r *http.Request, record projectAccess) (map[string]any, error) {
	if record.FinancialCompanyID <= 0 || record.ID <= 0 || strings.TrimSpace(record.Username) == "" {
		return nil, errors.New("operational access is not configured")
	}
	role := normalizeAccessRole(effectiveAccessRole(record))
	if role == "owner" {
		role = "admin"
	}
	payload, err := json.Marshal(map[string]any{
		"company_id": record.FinancialCompanyID,
		"access_id":  record.ID,
		"username":   record.Username,
		"role":       role,
		"menu_keys":  operationalMenuKeys(record),
	})
	if err != nil {
		return nil, err
	}
	loginURL := strings.TrimRight(a.operationalAPI, "/") + "/api/portal/session"
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, loginURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Operational-Portal-Secret", a.operationalSessionSecret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	data := map[string]any{}
	if len(body) > 0 {
		_ = json.Unmarshal(body, &data)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := "operational login failed"
		if errText, ok := data["error"].(string); ok && strings.TrimSpace(errText) != "" {
			msg = errText
		}
		return nil, errors.New(msg)
	}
	for _, cookie := range resp.Cookies() {
		if cookie.Name != "operational_session" {
			continue
		}
		http.SetCookie(w, &http.Cookie{
			Name:     cookie.Name,
			Value:    cookie.Value,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Secure:   isSecureRequest(r),
			MaxAge:   cookie.MaxAge,
			Expires:  cookie.Expires,
		})
	}
	return data, nil
}

func (a *portalApp) teamPage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/team" {
		http.NotFound(w, r)
		return
	}
	record, err := a.accessRecordFromRequest(r)
	if err != nil {
		http.Redirect(w, r, "/login?next=%2Fteam", http.StatusSeeOther)
		return
	}
	if record.ProjectKey != "textile-erp" || !effectiveCanManageTeam(record) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<!doctype html><html lang="fa" dir="rtl"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>عدم دسترسی</title><style>body{font-family:Tahoma;background:#0b1322;color:#fff;display:grid;place-items:center;min-height:100vh}.box{padding:28px;border:1px solid #475569;border-radius:16px;background:#111c2e}a{display:inline-block;color:#93c5fd;margin:8px;text-decoration:none}</style><div class="box"><h2>اجازه مدیریت کاربران را ندارید</h2><p>این بخش فقط برای مدیر سیستم یا کاربری با مجوز مدیریت کاربران قابل دسترسی است.</p><a href="/">بازگشت به صفحه اصلی</a><a href="/logout">خروج کاربر فعلی و ورود مدیر</a></div></html>`))
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html>
<html lang="fa" dir="rtl"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>مدیریت کاربران ERP</title>
<style>
*{box-sizing:border-box}body{margin:0;background:#08111f;color:#e5edf8;font-family:Tahoma,Arial;min-height:100vh}.top{position:sticky;top:0;z-index:3;background:#0d192b;border-bottom:1px solid #263850;padding:14px 22px;display:flex;align-items:center;justify-content:space-between;gap:12px}.top h1{font-size:21px;margin:0}.top a{color:#bfdbfe;text-decoration:none;border:1px solid #3b4f69;border-radius:10px;padding:9px 12px}.wrap{max-width:1250px;margin:auto;padding:24px}.intro{color:#9fb0c8;line-height:1.9;margin:5px 0 20px}.layout{display:grid;grid-template-columns:390px 1fr;gap:20px}.card{background:#111e31;border:1px solid #2d4059;border-radius:16px;padding:20px;box-shadow:0 16px 45px #0003}h2{margin:0 0 16px;font-size:18px}.grid{display:grid;grid-template-columns:1fr 1fr;gap:12px}.field{display:grid;gap:7px;margin-bottom:12px}.field.full{grid-column:1/-1}label{font-size:13px;color:#b8c5d6}input,select,textarea{width:100%;border:1px solid #40536d;border-radius:10px;padding:11px 12px;background:#081323;color:#f8fafc;font:inherit}textarea{resize:vertical;min-height:75px}.check{display:flex;align-items:center;gap:9px;border:1px solid #40536d;border-radius:10px;padding:11px}.check input{width:auto}.primary,.small{border:0;border-radius:10px;background:#2563eb;color:white;padding:11px 15px;font-weight:bold;cursor:pointer}.primary{width:100%}.mutedBtn{background:#334155}.danger{background:#b91c1c}.warn{background:#b45309}.msg{display:none;margin:14px 0;border-radius:10px;padding:11px;line-height:1.7}.msg.ok{display:block;background:#064e3b;color:#d1fae5}.msg.err{display:block;background:#7f1d1d;color:#fee2e2}.toolbar{display:flex;justify-content:space-between;align-items:center;gap:10px;margin-bottom:14px}.user{border:1px solid #30445d;background:#0a1627;border-radius:13px;padding:15px;margin-bottom:12px}.userHead{display:flex;justify-content:space-between;gap:12px;align-items:start}.name{font-weight:bold;font-size:16px}.sub{color:#94a3b8;font-size:12px;margin-top:7px;line-height:1.9}.tags{display:flex;gap:6px;flex-wrap:wrap;margin-top:10px}.tag{border-radius:999px;padding:5px 9px;font-size:11px;font-weight:bold;background:#1e3a5f;color:#bfdbfe}.tag.fin{background:#064e3b;color:#a7f3d0}.tag.op{background:#0c4a6e;color:#bae6fd}.tag.weave{background:#134e4a;color:#99f6e4}.tag.off{background:#7f1d1d;color:#fecaca}.credentials{display:grid;grid-template-columns:1fr 1fr;gap:8px;margin-top:12px}.cred{direction:ltr;text-align:left;border:1px dashed #415a77;border-radius:9px;padding:9px;color:#dbeafe;font-size:12px;overflow-wrap:anywhere}.cred button{float:right;margin-left:6px}.actions{display:flex;gap:7px;flex-wrap:wrap;margin-top:12px}.small{font-size:11px;padding:8px 10px}.empty{text-align:center;color:#94a3b8;padding:35px}.current{color:#fbbf24;font-size:11px}.help{font-size:12px;color:#93a4ba;line-height:1.8;border:1px solid #334155;background:#0a1627;border-radius:10px;padding:10px;margin-bottom:12px}@media(max-width:900px){.layout{grid-template-columns:1fr}.grid{grid-template-columns:1fr}.credentials{grid-template-columns:1fr}.top{align-items:flex-start}.wrap{padding:14px}}
</style></head><body>
<header class="top"><div><h1>مدیریت کاربران و سطح دسترسی</h1><div class="sub">مدیر فعلی: ` + html.EscapeString(record.ContactName) + ` (` + html.EscapeString(record.Username) + `)</div></div><div><a href="/">صفحه اصلی</a> <a href="/logout">خروج مدیر</a></div></header>
<main class="wrap"><p class="intro">هر کارمند یک نام کاربری و رمز دارد. فقط بخش‌هایی را می‌توانید به او بدهید که شرکت شما تهیه کرده است؛ ورود همه بخش‌ها از همین درگاه انجام می‌شود.</p><div id="message" class="msg"></div>
<div class="layout"><section class="card"><h2 id="formTitle">تعریف کاربر جدید</h2><div class="help">نام، نقش و بخش‌های مجاز را انتخاب کنید؛ سپس نام کاربری و رمز را با دکمه کپی برای کارمند بفرستید.</div>
<form id="userForm"><input type="hidden" id="editingId"><div class="field"><label>نام و نام خانوادگی</label><input id="contactName" required></div><div class="grid"><div class="field"><label>نام کاربری</label><input id="username" autocomplete="off" required></div><div class="field"><label>رمز عبور</label><input id="password" type="password" autocomplete="new-password" placeholder="در ویرایش برای حفظ رمز خالی بماند"></div><div class="field"><label>نقش کاربر</label><select id="accessRole"><option value="viewer">مشاهده‌گر</option><option value="accountant">حسابدار</option><option value="manager">مدیر اجرایی</option></select></div><div class="field"><label>مدت اعتبار (روز)</label><input id="trialDays" type="number" min="1" value="3650"></div></div><div class="field"><label>بخش‌های قابل ورود</label><div class="grid"><label class="check"><input id="allowFinancial" type="checkbox" ` + func() string {
		if effectiveAllowFinancial(record) {
			return ""
		}
		return "disabled"
	}() + `> مالی</label><label class="check"><input id="allowOperational" type="checkbox" ` + func() string {
		if effectiveAllowOperational(record) {
			return ""
		}
		return "disabled"
	}() + `> عملیاتی</label><label class="check"><input id="allowWeaving" type="checkbox" ` + func() string {
		if effectiveAllowWeaving(record) {
			return ""
		}
		return "disabled"
	}() + `> راندمان سالن</label></div><div class="help">گزینه غیرفعال یعنی این بخش در اشتراک شرکت خریداری نشده است.</div></div><label class="check"><input id="canManageTeam" type="checkbox"> اجازه مدیریت و ساخت کاربران دیگر</label><div class="field" style="margin-top:12px"><label>یادداشت</label><textarea id="notes" placeholder="مثلاً واحد حسابداری یا انبار"></textarea></div><button class="primary" id="saveBtn" type="submit">ایجاد کاربر</button><button class="primary mutedBtn" id="cancelBtn" type="button" style="display:none;margin-top:8px">انصراف از ویرایش</button></form></section>
<section class="card"><div class="toolbar"><div><h2 style="margin:0">کاربران تعریف‌شده</h2><div id="count" class="sub"></div></div><button class="small mutedBtn" id="refreshBtn">بازخوانی</button></div><div id="users"><div class="empty">در حال دریافت کاربران...</div></div></section></div></main>
<script>
const state={rows:[],editing:null};const $=id=>document.getElementById(id);const esc=v=>String(v??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const permissionCatalog=[['dashboard','داشبورد'],['financialHealth','سلامت مالی'],['initialData','اطلاعات اولیه'],['operational','داده‌های عملیاتی'],['incomingInvoices','فاکتور ورود'],['yarnOutInvoices','خروج نخ'],['invoices','فاکتور مالی'],['inventory','انبار'],['costs','هزینه‌ها'],['receivableDocs','اسناد دریافتی'],['payableDocs','اسناد پرداختی'],['bankCash','بانک و صندوق'],['accounting','دفاتر و تراز'],['reports','گزارش‌ها'],['taxReports','گزارش مالیاتی'],['credit','اعتبارسنجی'],['advisor','مشاور مالی'],['mobileApp','اتصال حسابیار']];
const rolePermissions={viewer:['dashboard','financialHealth','reports'],accountant:permissionCatalog.map(x=>x[0]).filter(x=>x!=='operational'),manager:permissionCatalog.map(x=>x[0])};
const permissionHost=document.createElement('div');permissionHost.className='field';permissionHost.innerHTML='<label>دسترسی تب‌های بخش مالی</label><div id="permissionGrid" class="grid"></div><div class="help">فقط تب‌های علامت‌خورده برای این کاربر نمایش داده می‌شود. اتصال حسابیار برای مدیر و حسابدار یک پل مالی اصلی است.</div>';$('canManageTeam').closest('label').before(permissionHost);$('permissionGrid').innerHTML=permissionCatalog.map(([key,label])=>'<label class="check"><input type="checkbox" data-permission="'+key+'"> '+label+'</label>').join('');
function selectedPermissions(){return [...document.querySelectorAll('[data-permission]:checked')].map(x=>x.dataset.permission);}
function setPermissionChecks(values){const allowed=new Set(values||[]);document.querySelectorAll('[data-permission]').forEach(x=>x.checked=allowed.has(x.dataset.permission));}
setPermissionChecks(rolePermissions.viewer);$('accessRole').addEventListener('change',()=>setPermissionChecks(rolePermissions[$('accessRole').value]||rolePermissions.viewer));
const portalFetch=window.fetch.bind(window);window.fetch=(path,options={})=>{if(/^\/api\/portal\/team(?:\/\d+)?$/.test(String(path))&&['POST','PUT'].includes(String(options.method||'').toUpperCase())&&options.body){try{const payload=JSON.parse(options.body);payload.permissions=selectedPermissions();options={...options,body:JSON.stringify(payload)};}catch{}}return portalFetch(path,options);};
const baseEditUser=editUser;editUser=id=>{baseEditUser(id);const row=state.rows.find(x=>Number(x.id)===Number(id));setPermissionChecks(row?.permissions?.length?row.permissions:(rolePermissions[row?.access_role||row?.accessRole]||rolePermissions.viewer));};
const baseResetForm=resetForm;resetForm=()=>{baseResetForm();setPermissionChecks(rolePermissions.viewer);};
function tell(text,type){const el=$('message');el.textContent=text;el.className='msg '+(type||'ok');window.scrollTo({top:0,behavior:'smooth'});}
async function api(path,options){const res=await fetch(path,{headers:{Accept:'application/json','Content-Type':'application/json'},...(options||{})});const data=await res.json().catch(()=>({}));if(!res.ok)throw new Error(data.error||('خطای '+res.status));return data;}
function roleLabel(v){return({owner:'مدیر اصلی',manager:'مدیر اجرایی',accountant:'حسابدار',viewer:'مشاهده‌گر'})[v]||v||'-';}
function resetForm(){$('userForm').reset();$('trialDays').value='3650';$('allowFinancial').checked=!$('allowFinancial').disabled;$('allowOperational').checked=false;$('allowWeaving').checked=false;$('editingId').value='';state.editing=null;$('formTitle').textContent='تعریف کاربر جدید';$('saveBtn').textContent='ایجاد کاربر';$('cancelBtn').style.display='none';}
function editUser(id){const r=state.rows.find(x=>Number(x.id)===Number(id));if(!r)return;state.editing=r;$('editingId').value=r.id;$('contactName').value=r.contact_name||r.contactName||'';$('username').value=r.username||'';$('password').value='';$('accessRole').value=r.access_role||r.accessRole||'viewer';$('allowFinancial').checked=Boolean(r.allow_financial??r.allowFinancial);$('allowOperational').checked=Boolean(r.allow_operational??r.allowOperational);$('allowWeaving').checked=Boolean(r.allow_weaving??r.allowWeaving);$('canManageTeam').checked=Boolean(r.can_manage_team??r.canManageTeam);$('notes').value=r.notes||'';$('formTitle').textContent='ویرایش کاربر';$('saveBtn').textContent='ذخیره تغییرات';$('cancelBtn').style.display='block';window.scrollTo({top:0,behavior:'smooth'});}
async function removeUser(id,name){if(!confirm('کاربر '+name+' حذف شود؟'))return;try{await api('/api/portal/team/'+id,{method:'DELETE'});tell('کاربر حذف شد.','ok');await load();}catch(e){tell(e.message,'err');}}
async function toggleUser(id,active){try{await api('/api/portal/team/'+id+'/toggle',{method:'POST'});tell(active?'دسترسی کاربر غیرفعال شد.':'دسترسی کاربر فعال شد.','ok');await load();}catch(e){tell(e.message,'err');}}
function copyValue(value,label){navigator.clipboard.writeText(String(value||'')).then(()=>tell(label+' کپی شد.','ok')).catch(()=>tell('کپی خودکار ممکن نشد؛ متن را انتخاب و کپی کنید.','err'));}
function render(){const box=$('users');$('count').textContent=state.rows.length+' کاربر';if(!state.rows.length){box.innerHTML='<div class="empty">هنوز کاربری تعریف نشده است.</div>';return;}box.innerHTML=state.rows.map(r=>{const current=Boolean(r.is_current??r.isCurrent),active=Boolean(r.is_active??r.isActive),f=Boolean(r.allow_financial??r.allowFinancial),o=Boolean(r.allow_operational??r.allowOperational),w=Boolean(r.allow_weaving??r.allowWeaving),name=r.contact_name||r.contactName||r.username||'بدون نام',role=r.access_role||r.accessRole,password=r.password||'ثبت شده و مخفی',username=r.username||'-';return '<article class="user"><div class="userHead"><div><div class="name">'+esc(name)+' '+(current?'<span class="current">(مدیر فعلی)</span>':'')+'</div><div class="sub">نقش: '+esc(roleLabel(role))+' | '+(active?'فعال':'غیرفعال')+'</div><div class="tags">'+(f?'<span class="tag fin">مالی</span>':'')+(o?'<span class="tag op">عملیاتی</span>':'')+(w?'<span class="tag weave">راندمان سالن</span>':'')+(!active?'<span class="tag off">غیرفعال</span>':'')+'</div></div></div><div class="credentials"><div class="cred"><button class="small" type="button" data-value="'+esc(username)+'" onclick="copyValue(this.dataset.value,\'نام کاربری\')">کپی</button>Username: '+esc(username)+'</div><div class="cred"><button class="small" type="button" data-value="'+esc(password)+'" onclick="copyValue(this.dataset.value,\'رمز عبور\')">کپی</button>Password: '+esc(password)+'</div></div>'+(!current?'<div class="actions"><button class="small" onclick="editUser('+r.id+')">ویرایش</button><button class="small warn" onclick="toggleUser('+r.id+','+active+')">'+(active?'غیرفعال‌کردن':'فعال‌کردن')+'</button><button class="small danger" onclick="removeUser('+r.id+',\''+esc(name).replace(/'/g,'&#39;')+'\')">حذف</button></div>':'')+'</article>';}).join('');}
async function load(){try{const data=await api('/api/portal/team');state.rows=data.items||[];render();}catch(e){tell(e.message,'err');$('users').innerHTML='<div class="empty">دریافت فهرست کاربران ممکن نشد.</div>';}}
$('userForm').addEventListener('submit',async e=>{e.preventDefault();const payload={contactName:$('contactName').value.trim(),username:$('username').value.trim(),password:$('password').value,accessRole:$('accessRole').value,canManageTeam:$('canManageTeam').checked,allowFinancial:$('allowFinancial').checked,allowOperational:$('allowOperational').checked,allowWeaving:$('allowWeaving').checked,trialDays:Number($('trialDays').value||3650),notes:$('notes').value.trim()};if(!payload.allowFinancial&&!payload.allowOperational&&!payload.allowWeaving){tell('حداقل یک بخش را برای کارمند انتخاب کنید.','err');return;}if(!state.editing&&!payload.password){tell('برای کاربر جدید رمز عبور مشخص کنید.','err');return;}try{$('saveBtn').disabled=true;await api(state.editing?'/api/portal/team/'+state.editing.id:'/api/portal/team',{method:state.editing?'PUT':'POST',body:JSON.stringify(payload)});tell(state.editing?'تغییرات کاربر ذخیره شد.':'کاربر جدید ساخته شد و اکنون می‌تواند با همین نام کاربری و رمز وارد بخش مجاز شود.','ok');resetForm();await load();}catch(err){tell(err.message,'err');}finally{$('saveBtn').disabled=false;}});
$('cancelBtn').addEventListener('click',resetForm);$('refreshBtn').addEventListener('click',load);load();
</script></body></html>`))
}

func (a *portalApp) portalTeam(w http.ResponseWriter, r *http.Request) {
	record, err := a.accessRecordFromRequest(r)
	if err != nil {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "access session is not valid"})
		return
	}
	if record.ProjectKey != "textile-erp" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "team management is only available for textile-erp"})
		return
	}
	if !effectiveCanManageTeam(record) {
		respondJSON(w, http.StatusForbidden, map[string]string{"error": "team management is not enabled for this access"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		items, err := a.tenantAccesses(record)
		if err != nil {
			respondJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		out := make([]map[string]any, 0, len(items))
		for _, item := range items {
			row := a.accessResponse(item, a.mustDecryptPassword(item))
			row["is_current"] = item.ID == record.ID
			row["isCurrent"] = item.ID == record.ID
			out = append(out, row)
		}
		respondJSON(w, http.StatusOK, map[string]any{"items": out})
	case http.MethodPost:
		req, err := decodeAccessRequest(r)
		if err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if strings.TrimSpace(req.ContactName) == "" {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "employee name is required"})
			return
		}
		role := normalizeAccessRole(req.AccessRole)
		if role == "owner" {
			role = "manager"
		}
		canManage := boolPtrValue(req.CanManageTeam, false)
		allowFinancial := boolPtrValue(req.AllowFinancial, true) && effectiveAllowFinancial(record)
		allowOperational := boolPtrValue(req.AllowOperational, false) && effectiveAllowOperational(record)
		allowWeaving := boolPtrValue(req.AllowWeaving, false) && effectiveAllowWeaving(record)
		if effectiveAccessRole(record) != "owner" && canManage {
			respondJSON(w, http.StatusForbidden, map[string]string{"error": "only the owner can grant team management"})
			return
		}
		expiresAt := record.ExpiresAt
		if strings.TrimSpace(req.ExpiresAt) != "" || req.TrialDays > 0 {
			customExpiry, err := resolveExpiry(req.ExpiresAt, req.TrialDays)
			if err != nil {
				respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			expiresAt = minTime(customExpiry, record.ExpiresAt)
		}
		if allowWeaving {
			if err := a.ensureCompanyWeavingReady(record); err != nil {
				respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
		}
		access, rawPassword, err := a.createManagedAccess(record.ProjectKey, record.CompanyName, req.ContactName, req.Username, req.Password, record.FinancialCompanyID, expiresAt, record.TrialEndsAt, req.Notes, role, req.Permissions, canManage, false, allowFinancial, allowOperational, allowWeaving)
		if err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		respondJSON(w, http.StatusCreated, a.accessResponse(access, rawPassword))
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *portalApp) portalTeamByID(w http.ResponseWriter, r *http.Request) {
	record, err := a.accessRecordFromRequest(r)
	if err != nil {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "access session is not valid"})
		return
	}
	if record.ProjectKey != "textile-erp" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "team management is only available for textile-erp"})
		return
	}
	if !effectiveCanManageTeam(record) {
		respondJSON(w, http.StatusForbidden, map[string]string{"error": "team management is not enabled for this access"})
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/portal/team/"), "/")
	if path == "" {
		http.NotFound(w, r)
		return
	}
	parts := strings.Split(path, "/")
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid team access id"})
		return
	}
	target, err := a.tenantAccessByID(record, id)
	if err != nil {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "team access not found"})
		return
	}

	if len(parts) == 1 && r.Method == http.MethodDelete {
		if target.ID == record.ID {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "current access cannot be deleted"})
			return
		}
		if err := a.deleteAccess(id); err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		respondJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}

	if len(parts) == 1 && r.Method == http.MethodPut {
		if target.ID == record.ID {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "current access cannot be edited from team management"})
			return
		}
		req, err := decodeAccessRequest(r)
		if err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if strings.TrimSpace(req.ContactName) == "" {
			req.ContactName = target.ContactName
		}
		role := normalizeAccessRole(req.AccessRole)
		if role == "owner" {
			role = "manager"
		}
		canManage := boolPtrValue(req.CanManageTeam, target.CanManageTeam)
		allowFinancial := boolPtrValue(req.AllowFinancial, effectiveAllowFinancial(target)) && effectiveAllowFinancial(record)
		allowOperational := boolPtrValue(req.AllowOperational, effectiveAllowOperational(target)) && effectiveAllowOperational(record)
		allowWeaving := boolPtrValue(req.AllowWeaving, effectiveAllowWeaving(target)) && effectiveAllowWeaving(record)
		if effectiveAccessRole(record) != "owner" && canManage {
			respondJSON(w, http.StatusForbidden, map[string]string{"error": "only the owner can grant team management"})
			return
		}
		expiresAt := target.ExpiresAt
		if strings.TrimSpace(req.ExpiresAt) != "" || req.TrialDays > 0 {
			customExpiry, err := resolveExpiry(req.ExpiresAt, req.TrialDays)
			if err != nil {
				respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			expiresAt = minTime(customExpiry, record.ExpiresAt)
		}
		if allowWeaving {
			if err := a.ensureCompanyWeavingReady(record); err != nil {
				respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
		}
		access, rawPassword, err := a.updateManagedAccess(id, target.ProjectKey, target.CompanyName, req.ContactName, req.Username, req.Password, target.FinancialCompanyID, expiresAt, target.TrialEndsAt, req.Notes, role, req.Permissions, canManage, false, allowFinancial, allowOperational, allowWeaving)
		if err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		respondJSON(w, http.StatusOK, a.accessResponse(access, rawPassword))
		return
	}

	if len(parts) == 2 && parts[1] == "toggle" && r.Method == http.MethodPost {
		if target.ID == record.ID {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "current access cannot be disabled"})
			return
		}
		if err := a.toggleAccess(id); err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		respondJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}

	if len(parts) == 2 && parts[1] == "rotate-link" && r.Method == http.MethodPost {
		if target.ID == record.ID {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "current access link cannot be replaced"})
			return
		}
		access, err := a.rotateAccessToken(id)
		if err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		respondJSON(w, http.StatusOK, a.accessResponse(access, ""))
		return
	}

	http.NotFound(w, r)
}

func (a *portalApp) renderAdminLogin(w http.ResponseWriter, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	errorHTML := ""
	if errMsg != "" {
		errorHTML = `<div class="error">` + html.EscapeString(errMsg) + `</div>`
	}
	_, _ = w.Write([]byte(`<!doctype html>
<html lang="fa" dir="rtl">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>ورود پنل مدیریت</title>
  <style>
    *{box-sizing:border-box} body{margin:0;min-height:100vh;background:#0f172a;color:#e2e8f0;font-family:Tahoma,Arial;display:flex;align-items:center;justify-content:center;padding:20px}
    .card{width:min(420px,100%);background:#111c2e;border:1px solid #334155;border-radius:16px;padding:24px}
    h1{margin:0 0 8px;font-size:24px} p{margin:0 0 18px;color:#a8bad3;line-height:1.9}
    form{display:grid;gap:12px} input{width:100%;border:1px solid #475569;border-radius:10px;padding:11px 12px;background:#0b1322;color:#fff}
    button{border:1px solid #2563eb;background:#2563eb;color:#fff;border-radius:10px;padding:11px 14px;cursor:pointer}
    .error{margin-bottom:12px;background:#7f1d1d;color:#fee2e2;border:1px solid #b91c1c;border-radius:10px;padding:10px 12px}
  </style>
</head>
<body>
  <main class="card">
    <h1>ورود پنل مدیریت</h1>
    <p>از این بخش می‌توانید برای مشتریان لینک تست، یوزر، رمز و تاریخ انقضا تعریف کنید.</p>
    ` + errorHTML + `
    <form method="post" action="/admin/login">
      <input name="username" placeholder="نام کاربری مدیریت" autocomplete="username" required>
      <input name="password" type="password" placeholder="رمز عبور مدیریت" autocomplete="current-password" required>
      <button type="submit">ورود</button>
    </form>
  </main>
</body>
</html>`))
}

func (a *portalApp) renderAccessPage(w http.ResponseWriter, record *projectAccess, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	status := http.StatusOK
	if record == nil {
		status = http.StatusNotFound
	}
	w.WriteHeader(status)
	setupMode := record != nil && accessRequiresSetup(*record)
	changePasswordMode := record != nil && record.MustChangePassword && !setupMode
	title := "دسترسی به پورتال"
	hint := "با استفاده از لینک اختصاصی خود وارد پروژه شوید."
	projectName := "پروژه"
	companyName := "-"
	expiry := "-"
	target := "-"
	contactName := ""
	if record != nil {
		projectName = projectLabel(record.ProjectKey)
		companyName = record.CompanyName
		expiry = record.ExpiresAt.Format(timeLayout)
		target = a.customerTargetHint(*record)
		contactName = record.ContactName
	}
	if setupMode {
		title = "ایجاد حساب مدیر"
		hint = "نام کاربری و رمز عبوری را انتخاب کنید که به عنوان حساب مدیر اصلی این مجموعه استفاده شود."
	} else if changePasswordMode {
		title = "تغییر رمز عبور موقت"
		hint = "برای اولین ورود، رمز موقت را با یک رمز اختصاصی و امن جایگزین کنید."
	}
	errorHTML := ""
	if errMsg != "" {
		errorHTML = `<div class="error">` + html.EscapeString(errMsg) + `</div>`
	}
	buttonText := "ورود به پروژه"
	formHTML := ""
	if record != nil {
		formHTML = `<form method="post">
        <input name="username" placeholder="نام کاربری" autocomplete="username" required>
        <input name="password" type="password" placeholder="رمز عبور" autocomplete="current-password" required>
        <button type="submit">` + html.EscapeString(buttonText) + `</button>
      </form>
      <div class="muted">برای ورودهای بعدی از همین لینک یا کد QR و نام کاربری و رمز عبور خود استفاده کنید.</div>`
	}
	if changePasswordMode {
		formHTML = `<form method="post">
        <input name="username" placeholder="نام کاربری" autocomplete="username" required>
        <input name="current_password" type="password" placeholder="رمز عبور موقت" autocomplete="current-password" required>
        <input name="new_password" type="password" placeholder="رمز عبور جدید؛ حداقل ۱۰ کاراکتر" autocomplete="new-password" required>
        <input name="confirm_password" type="password" placeholder="تکرار رمز عبور جدید" autocomplete="new-password" required>
        <button type="submit">ثبت رمز جدید و ورود</button>
      </form>
      <div class="muted">پس از ثبت رمز جدید، برای ورودهای بعدی از همین لینک یا کد QR و رمز جدید استفاده کنید.</div>`
	}
	if setupMode {
		buttonText = "ذخیره و ادامه"
		formHTML = `<form method="post">
        <input name="contact_name" placeholder="نام و نام خانوادگی مدیر" value="` + html.EscapeString(contactName) + `">
        <input name="username" placeholder="نام کاربری را انتخاب کنید" autocomplete="username" required>
        <input name="password" type="password" placeholder="رمز عبور را انتخاب کنید" autocomplete="new-password" required>
        <input name="confirm_password" type="password" placeholder="رمز عبور را دوباره وارد کنید" autocomplete="new-password" required>
        <button type="submit">` + html.EscapeString(buttonText) + `</button>
      </form>
      <div class="muted">بعد از این مرحله، شما مدیر اصلی این شرکت خواهید بود و می‌توانید لینک دسترسی کارمندان را داخل ERP ایجاد کنید.</div>`
	}
	_, _ = w.Write([]byte(`<!doctype html>
<html lang="fa" dir="rtl">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>` + html.EscapeString(title) + `</title>
  <style>
    *{box-sizing:border-box} body{margin:0;min-height:100vh;background:#f7f1e8;color:#2a1a14;font-family:Tahoma,Arial;display:flex;align-items:center;justify-content:center;padding:24px}
    .shell{width:min(980px,96vw);display:grid;grid-template-columns:1.2fr .95fr;gap:18px}
    .panel{background:#fffaf4;border:1px solid #dbc7ae;border-radius:20px;padding:26px;box-shadow:0 24px 80px rgba(75,43,24,.12)}
    h1{margin:0 0 10px;font-size:30px}.lead{color:#6f574a;line-height:1.9;margin-bottom:18px}
    .meta{display:grid;gap:10px}.item{padding:12px 14px;border-radius:14px;background:#f4e7d6;border:1px solid #ddc2a5}
    .item span{display:block;font-size:12px;color:#7a6355;margin-bottom:6px}
    form{display:grid;gap:12px;margin-top:14px} input{width:100%;border:1px solid #c8ab8b;border-radius:12px;padding:12px 14px;background:#fff;color:#2a1a14}
    button{border:1px solid #8b5e3c;background:#8b5e3c;color:#fff;border-radius:12px;padding:12px 16px;cursor:pointer}
    .error{margin-bottom:12px;background:#fff0f0;color:#9f2333;border:1px solid #efb0b0;border-radius:12px;padding:10px 12px}
    .muted{color:#7a6355;font-size:13px;line-height:1.9}
    @media(max-width:860px){.shell{grid-template-columns:1fr}}
  </style>
</head>
<body>
  <main class="shell">
    <section class="panel">
      <h1>` + html.EscapeString(title) + `</h1>
      <div class="lead">` + html.EscapeString(hint) + `</div>
      ` + errorHTML + `
      ` + formHTML + `
    </section>
    <aside class="panel">
      <h1 style="font-size:22px">جزئیات دسترسی</h1>
      <div class="meta">
        <div class="item"><span>پروژه</span>` + html.EscapeString(projectName) + `</div>
        <div class="item"><span>شرکت</span>` + html.EscapeString(companyName) + `</div>
        <div class="item"><span>معتبر تا</span>` + html.EscapeString(expiry) + `</div>
        <div class="item"><span>مسیر ورود</span>` + html.EscapeString(target) + `</div>
      </div>
    </aside>
  </main>
</body>
</html>`))
}
func (a *portalApp) listAccesses() ([]projectAccess, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	items, err := readAccesses(a.accessFile)
	if err != nil {
		return nil, err
	}
	sortAccesses(items)
	return items, nil
}

func (a *portalApp) createAccess(projectKey, companyName, contactName, username, password string, financialCompanyID int64, expiresAt time.Time, notes string) (projectAccess, error) {
	if !validProject(projectKey) {
		return projectAccess{}, fmt.Errorf("پروژه انتخاب‌شده معتبر نیست")
	}
	if projectKey == "textile-erp" && financialCompanyID <= 0 {
		return projectAccess{}, fmt.Errorf("financial company id is required for textile customer access")
	}
	if expiresAt.Before(time.Now()) {
		return projectAccess{}, fmt.Errorf("تاریخ انقضا باید در آینده باشد")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return projectAccess{}, err
	}
	enc, err := a.encryptPassword(password)
	if err != nil {
		return projectAccess{}, err
	}
	token, err := randomHex(24)
	if err != nil {
		return projectAccess{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	items, err := readAccesses(a.accessFile)
	if err != nil {
		return projectAccess{}, err
	}
	if accessUsernameTaken(items, projectAccess{
		ProjectKey:         projectKey,
		CompanyName:        companyName,
		FinancialCompanyID: financialCompanyID,
	}, username, 0) {
		return projectAccess{}, fmt.Errorf("این نام کاربری قبلا برای این شرکت ثبت شده است.")
	}
	now := time.Now().UTC()
	record := projectAccess{
		ID:                 nextAccessID(items),
		ProjectKey:         projectKey,
		CompanyName:        companyName,
		ContactName:        contactName,
		Username:           username,
		FinancialCompanyID: financialCompanyID,
		PasswordHash:       string(hash),
		PasswordEnc:        enc,
		ExpiresAt:          expiresAt.UTC(),
		AccessToken:        token,
		Notes:              notes,
		IsActive:           true,
		CreatedAt:          now,
	}
	items = append(items, record)
	if err := writeAccesses(a.accessFile, items); err != nil {
		return projectAccess{}, err
	}
	return record, nil
}

func (a *portalApp) updateAccess(id int64, projectKey, companyName, contactName, username, password string, financialCompanyID int64, expiresAt time.Time, notes string) (projectAccess, string, error) {
	if !validProject(projectKey) {
		return projectAccess{}, "", fmt.Errorf("پروژه انتخاب‌شده معتبر نیست")
	}
	if expiresAt.Before(time.Now()) {
		return projectAccess{}, "", fmt.Errorf("تاریخ انقضا باید در آینده باشد")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	items, err := readAccesses(a.accessFile)
	if err != nil {
		return projectAccess{}, "", err
	}
	index := -1
	for i, item := range items {
		if item.ID == id {
			index = i
			break
		}
	}
	if index == -1 {
		return projectAccess{}, "", fmt.Errorf("دسترسی موردنظر پیدا نشد")
	}
	if accessUsernameTaken(items, projectAccess{
		ProjectKey:         projectKey,
		CompanyName:        companyName,
		FinancialCompanyID: financialCompanyID,
	}, username, id) {
		return projectAccess{}, "", fmt.Errorf("این نام کاربری قبلا برای این شرکت ثبت شده است.")
	}
	record := items[index]
	record.ProjectKey = projectKey
	record.CompanyName = companyName
	record.ContactName = contactName
	record.Username = username
	record.FinancialCompanyID = financialCompanyID
	record.ExpiresAt = expiresAt.UTC()
	record.Notes = notes
	rawPassword := a.mustDecryptPassword(record)
	if strings.TrimSpace(password) != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return projectAccess{}, "", err
		}
		enc, err := a.encryptPassword(password)
		if err != nil {
			return projectAccess{}, "", err
		}
		record.PasswordHash = string(hash)
		record.PasswordEnc = enc
		rawPassword = password
	}
	items[index] = record
	if err := writeAccesses(a.accessFile, items); err != nil {
		return projectAccess{}, "", err
	}
	return record, rawPassword, nil
}

func uniqueGeneratedUsername(items []projectAccess, projectKey, contactName, companyName string, financialCompanyID int64, excludeID int64) string {
	base := slugTokenBase(contactName)
	if base == "user" {
		base = slugTokenBase(companyName)
	}
	if base == "user" {
		base = "staff"
	}
	for attempt := 0; attempt < 16; attempt++ {
		suffix, err := randomHex(2)
		if err != nil {
			suffix = fmt.Sprintf("%02d", time.Now().UnixNano()%97)
		}
		candidate := fmt.Sprintf("%s-%s", base, strings.ToLower(suffix))
		if !accessUsernameTaken(items, projectAccess{
			ProjectKey:         projectKey,
			CompanyName:        companyName,
			FinancialCompanyID: financialCompanyID,
		}, candidate, excludeID) {
			return candidate
		}
	}
	return fmt.Sprintf("%s-%d", base, time.Now().Unix()%100000)
}

func generatedAccessPassword(username string) string {
	suffix, err := randomHex(6)
	if err != nil {
		suffix = fmt.Sprintf("%d", time.Now().UnixNano()%1000000)
	}
	base := slugTokenBase(username)
	if base == "user" {
		base = "portal"
	}
	return "Portal@" + base + strings.ToUpper(suffix)
}

func validateAccessPassword(password string) error {
	if len([]rune(strings.TrimSpace(password))) < 8 {
		return fmt.Errorf("رمز عبور باید حداقل ۸ کاراکتر داشته باشد")
	}
	return nil
}

func (a *portalApp) ensureLocalOwnerAccess() (projectAccess, error) {
	if !a.localMode || a.localCompanyID <= 0 || strings.TrimSpace(a.localCompanyName) == "" {
		return projectAccess{}, errors.New("local owner mode is not configured")
	}
	items, err := a.listAccesses()
	if err != nil {
		return projectAccess{}, err
	}
	expiresAt := time.Now().AddDate(10, 0, 0)
	for _, item := range items {
		if item.ProjectKey != "textile-erp" || item.FinancialCompanyID != a.localCompanyID || !strings.EqualFold(strings.TrimSpace(item.Username), a.adminUsername) {
			continue
		}
		updated, _, err := a.updateManagedAccess(item.ID, "textile-erp", a.localCompanyName, "مدیر محلی", a.adminUsername, a.adminPassword, a.localCompanyID, expiresAt, time.Time{}, "حساب مدیر نسخه آفلاین", "owner", financialPermissionCatalog, true, false, true, true, false)
		if err != nil {
			return projectAccess{}, err
		}
		if _, err := a.provisionTextileTenant(updated.FinancialCompanyID, updated.ID, updated.CompanyName, updated.ContactName, updated.Username, a.adminPassword, "owner"); err != nil {
			return projectAccess{}, err
		}
		if !updated.IsActive {
			if err := a.toggleAccess(updated.ID); err != nil {
				return projectAccess{}, err
			}
		}
		return a.setAccessMustChangePassword(updated.ID, false)
	}
	created, _, err := a.createManagedAccess("textile-erp", a.localCompanyName, "مدیر محلی", a.adminUsername, a.adminPassword, a.localCompanyID, expiresAt, time.Time{}, "حساب مدیر نسخه آفلاین", "owner", financialPermissionCatalog, true, false, true, true, false)
	if err != nil {
		return projectAccess{}, err
	}
	return a.setAccessMustChangePassword(created.ID, false)
}

func existingFinancialCompanyID(items []projectAccess, companyName string) int64 {
	companyName = strings.TrimSpace(companyName)
	var companyID int64
	for _, item := range items {
		if item.ProjectKey != "textile-erp" || item.FinancialCompanyID <= 0 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(item.CompanyName), companyName) && item.FinancialCompanyID > companyID {
			companyID = item.FinancialCompanyID
		}
	}
	return companyID
}

func (a *portalApp) provisionTextileTenant(financialCompanyID, accessID int64, companyName, contactName, username, password, role string) (int64, error) {
	operationalRole := "viewer"
	if normalizeAccessRole(role) == "owner" {
		operationalRole = "admin"
	}
	payload, err := json.Marshal(map[string]any{
		"company_id":   financialCompanyID,
		"access_id":    accessID,
		"company_name": companyName,
		"contact_name": contactName,
		"username":     username,
		"password":     password,
		"role":         operationalRole,
	})
	if err != nil {
		return 0, err
	}
	endpoint := strings.TrimRight(a.operationalAPI, "/") + "/api/portal/provision"
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Operational-Portal-Secret", a.operationalSessionSecret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var result struct {
		Success   bool   `json:"success"`
		CompanyID int64  `json:"company_id"`
		Error     string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !result.Success || result.CompanyID <= 0 {
		if strings.TrimSpace(result.Error) == "" {
			result.Error = fmt.Sprintf("operational tenant provisioning failed with status %d", resp.StatusCode)
		}
		return 0, errors.New(result.Error)
	}
	return result.CompanyID, nil
}

func existingWeavingTenantID(items []projectAccess, financialCompanyID int64) string {
	for _, item := range items {
		if item.ProjectKey == "textile-erp" && item.FinancialCompanyID == financialCompanyID && strings.TrimSpace(item.WeavingTenantID) != "" {
			return strings.TrimSpace(item.WeavingTenantID)
		}
	}
	return ""
}

func (a *portalApp) provisionWeavingTenant(financialCompanyID, accessID int64, companyName, contactName, username, password string) (string, error) {
	if strings.TrimSpace(a.weavingAppURL) == "" || len(strings.TrimSpace(a.weavingMonitorToken)) < 32 {
		return "", errors.New("اتصال امن راندمان سالن روی سرور تنظیم نشده است")
	}
	payload, err := json.Marshal(map[string]any{
		"tenantName":        companyName,
		"displayName":       contactName,
		"username":          username,
		"password":          password,
		"externalTenantKey": fmt.Sprintf("textile-company:%d", financialCompanyID),
		"externalUserId":    fmt.Sprintf("textile-access:%d", accessID),
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(a.weavingAppURL, "/")+"/api/internal/provision", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(a.weavingMonitorToken))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: 12 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var result struct {
		TenantID string `json:"tenantId"`
		Error    string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || strings.TrimSpace(result.TenantID) == "" {
		if strings.TrimSpace(result.Error) == "" {
			result.Error = fmt.Sprintf("weaving tenant provisioning failed with status %d", resp.StatusCode)
		}
		return "", errors.New(result.Error)
	}
	return strings.TrimSpace(result.TenantID), nil
}

func (a *portalApp) syncWeavingUser(record projectAccess, password string, active bool) error {
	if strings.TrimSpace(record.WeavingTenantID) == "" {
		return errors.New("شناسه مشتری راندمان سالن آماده نیست")
	}
	role := "worker"
	if effectiveAccessRole(record) == "owner" || effectiveAccessRole(record) == "manager" {
		role = "manager"
	}
	payload, err := json.Marshal(map[string]any{
		"tenantId": record.WeavingTenantID, "externalUserId": fmt.Sprintf("textile-access:%d", record.ID),
		"username": record.Username, "password": password, "displayName": record.ContactName,
		"role": role, "active": active,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(a.weavingAppURL, "/")+"/api/internal/users/sync", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(a.weavingMonitorToken))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: 12 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	var result struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result)
	if strings.TrimSpace(result.Error) == "" {
		result.Error = fmt.Sprintf("weaving user sync failed with status %d", resp.StatusCode)
	}
	return errors.New(result.Error)
}

func (a *portalApp) persistWeavingTenantID(accessID int64, tenantID string) (projectAccess, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return projectAccess{}, errors.New("weaving tenant id is missing")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	items, err := readAccesses(a.accessFile)
	if err != nil {
		return projectAccess{}, err
	}
	for index := range items {
		if items[index].ID != accessID {
			continue
		}
		items[index].WeavingTenantID = tenantID
		if err := writeAccesses(a.accessFile, items); err != nil {
			return projectAccess{}, err
		}
		return items[index], nil
	}
	return projectAccess{}, errors.New("access not found")
}

func (a *portalApp) ensureWeavingReady(record projectAccess) (projectAccess, error) {
	if strings.TrimSpace(record.WeavingTenantID) != "" {
		return record, nil
	}
	password := strings.TrimSpace(a.mustDecryptPassword(record))
	if password == "" {
		generated, err := randomHex(24)
		if err != nil {
			return projectAccess{}, err
		}
		password = generated
	}
	tenantID, err := a.provisionWeavingTenant(
		record.FinancialCompanyID,
		record.ID,
		record.CompanyName,
		record.ContactName,
		record.Username,
		password,
	)
	if err != nil {
		return projectAccess{}, err
	}
	return a.persistWeavingTenantID(record.ID, tenantID)
}

// ensureCompanyWeavingReady repairs legacy/trial access records that allow the
// weaving module but were saved before a weaving tenant id was persisted. The
// tenant must be provisioned with the company owner, never with an employee,
// otherwise the first employee could accidentally become the tenant manager.
func (a *portalApp) ensureCompanyWeavingReady(record projectAccess) error {
	items, err := a.tenantAccesses(record)
	if err != nil {
		return err
	}
	if existingWeavingTenantID(items, record.FinancialCompanyID) != "" {
		return nil
	}
	var owner projectAccess
	for _, item := range items {
		if effectiveAccessRole(item) == "owner" {
			owner = item
			break
		}
	}
	if owner.ID == 0 {
		return errors.New("مدیر اصلی این مشتری برای فعال‌سازی راندمان سالن پیدا نشد")
	}
	_, err = a.ensureWeavingReady(owner)
	return err
}

func (a *portalApp) signWeavingSSO(record projectAccess) (string, error) {
	secret := strings.TrimSpace(a.weavingMonitorToken)
	if len(secret) < 32 || strings.TrimSpace(record.WeavingTenantID) == "" {
		return "", errors.New("ورود یکپارچه راندمان سالن آماده نیست")
	}
	claims := map[string]any{
		"tenantId":         record.WeavingTenantID,
		"externalUserId":   fmt.Sprintf("textile-access:%d", record.ID),
		"expiresAt":        time.Now().Add(2 * time.Minute).Unix(),
		"sessionExpiresAt": minTime(record.ExpiresAt, time.Now().Add(12*time.Hour)).Unix(),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (a *portalApp) redirectToWeavingSSO(w http.ResponseWriter, r *http.Request, record projectAccess) {
	ticket, err := a.issueWeavingBridgeTicket(record)
	if err != nil {
		log.Printf("weaving SSO failed for access=%d: %v", record.ID, err)
		a.renderModuleLogin(w, "weaving", moduleTarget("weaving"), "ورود یکپارچه راندمان سالن آماده نیست؛ با پشتیبانی تماس بگیرید.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, strings.TrimRight(a.weavingAppURL, "/")+"/api/auth/portal-sso?ticket="+url.QueryEscape(ticket), http.StatusSeeOther)
}

func (a *portalApp) createManagedAccess(projectKey, companyName, contactName, username, password string, financialCompanyID int64, expiresAt, trialEndsAt time.Time, notes, accessRole string, permissions []string, canManageTeam, requiresSetup, allowFinancial, allowOperational, allowWeaving bool) (projectAccess, string, error) {
	if !validProject(projectKey) {
		return projectAccess{}, "", fmt.Errorf("invalid project key")
	}
	projectKey = strings.TrimSpace(projectKey)
	companyName = strings.TrimSpace(companyName)
	contactName = strings.TrimSpace(contactName)
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	notes = strings.TrimSpace(notes)
	if projectKey == "textile-erp" && financialCompanyID <= 0 && requiresSetup {
		return projectAccess{}, "", fmt.Errorf("financial company id is required for textile customer access")
	}
	if expiresAt.Before(time.Now()) {
		return projectAccess{}, "", fmt.Errorf("expiry must be in the future")
	}

	role := normalizeAccessRole(accessRole)
	normalizedPermissions := normalizePermissions(permissions, role)
	if projectKey != "textile-erp" {
		role = "customer"
		normalizedPermissions = nil
		canManageTeam = false
		requiresSetup = false
		allowFinancial = false
		allowOperational = false
		allowWeaving = false
	} else {
		allowFinancial, allowOperational, allowWeaving = normalizeModuleAccess(projectKey, allowFinancial, allowOperational, allowWeaving)
		trialActive := !trialEndsAt.IsZero() && trialEndsAt.After(time.Now())
		if !allowFinancial && !allowOperational && !allowWeaving && !trialActive {
			return projectAccess{}, "", fmt.Errorf("حداقل یکی از بخش‌های مالی، عملیاتی یا راندمان سالن باید فعال باشد")
		}
		if requiresSetup && (allowWeaving || trialActive) {
			return projectAccess{}, "", fmt.Errorf("برای فعال‌سازی راندمان سالن، نام کاربری و رمز مدیر را همین حالا تعیین کنید")
		}
		if !allowFinancial {
			normalizedPermissions = nil
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	items, err := readAccesses(a.accessFile)
	if err != nil {
		return projectAccess{}, "", err
	}
	if projectKey == "textile-erp" && financialCompanyID <= 0 {
		financialCompanyID = existingFinancialCompanyID(items, companyName)
	}

	if requiresSetup {
		username = ""
		password = ""
	}
	if !requiresSetup && username == "" {
		username = uniqueGeneratedUsername(items, projectKey, contactName, companyName, financialCompanyID, 0)
	}
	if !requiresSetup && password == "" {
		password = generatedAccessPassword(username)
	}
	if !requiresSetup {
		if err := validateAccessPassword(password); err != nil {
			return projectAccess{}, "", err
		}
	}
	if accessUsernameTaken(items, projectAccess{
		ProjectKey:         projectKey,
		CompanyName:        companyName,
		FinancialCompanyID: financialCompanyID,
	}, username, 0) {
		return projectAccess{}, "", fmt.Errorf("این نام کاربری قبلا برای این شرکت ثبت شده است.")
	}

	hash := ""
	enc := ""
	rawPassword := ""
	if !requiresSetup {
		passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return projectAccess{}, "", err
		}
		passwordEnc, err := a.encryptPassword(password)
		if err != nil {
			return projectAccess{}, "", err
		}
		hash = string(passwordHash)
		enc = passwordEnc
		rawPassword = password
	}
	accessID := nextAccessID(items)
	if projectKey == "textile-erp" && !requiresSetup {
		financialCompanyID, err = a.provisionTextileTenant(financialCompanyID, accessID, companyName, contactName, username, password, role)
		if err != nil {
			return projectAccess{}, "", err
		}
	}
	if projectKey == "textile-erp" && financialCompanyID <= 0 {
		return projectAccess{}, "", fmt.Errorf("financial company id is required for textile customer access")
	}
	weavingTenantID := existingWeavingTenantID(items, financialCompanyID)
	trialActive := projectKey == "textile-erp" && !trialEndsAt.IsZero() && trialEndsAt.After(time.Now())
	if projectKey == "textile-erp" && (allowWeaving || trialActive) && weavingTenantID == "" {
		weavingTenantID, err = a.provisionWeavingTenant(financialCompanyID, accessID, companyName, contactName, username, password)
		if err != nil {
			return projectAccess{}, "", err
		}
	}
	token, err := randomHex(24)
	if err != nil {
		return projectAccess{}, "", err
	}
	record := projectAccess{
		ID:                 accessID,
		ProjectKey:         projectKey,
		CompanyName:        companyName,
		ContactName:        contactName,
		Username:           username,
		FinancialCompanyID: financialCompanyID,
		AccessRole:         role,
		Permissions:        normalizedPermissions,
		CanManageTeam:      canManageTeam,
		RequiresSetup:      requiresSetup,
		MustChangePassword: false,
		AllowFinancial:     allowFinancial,
		AllowOperational:   allowOperational,
		AllowWeaving:       allowWeaving,
		ModuleAccessSet:    projectKey == "textile-erp",
		WeavingTenantID:    weavingTenantID,
		TrialEndsAt:        trialEndsAt.UTC(),
		ExpiresAt:          expiresAt.UTC(),
		AccessToken:        token,
		PasswordHash:       hash,
		PasswordEnc:        enc,
		Notes:              notes,
		IsActive:           true,
		CreatedAt:          time.Now().UTC(),
	}
	if (allowWeaving || trialActive) && weavingTenantID != "" {
		if err := a.syncWeavingUser(record, password, true); err != nil {
			return projectAccess{}, "", err
		}
	}
	items = append(items, record)
	if err := writeAccesses(a.accessFile, items); err != nil {
		return projectAccess{}, "", err
	}
	return record, rawPassword, nil
}

func (a *portalApp) updateManagedAccess(id int64, projectKey, companyName, contactName, username, password string, financialCompanyID int64, expiresAt, trialEndsAt time.Time, notes, accessRole string, permissions []string, canManageTeam, requiresSetup, allowFinancial, allowOperational, allowWeaving bool) (projectAccess, string, error) {
	if !validProject(projectKey) {
		return projectAccess{}, "", fmt.Errorf("invalid project key")
	}
	projectKey = strings.TrimSpace(projectKey)
	companyName = strings.TrimSpace(companyName)
	contactName = strings.TrimSpace(contactName)
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	notes = strings.TrimSpace(notes)
	if projectKey == "textile-erp" && financialCompanyID <= 0 {
		return projectAccess{}, "", fmt.Errorf("financial company id is required for textile customer access")
	}
	if expiresAt.Before(time.Now()) {
		return projectAccess{}, "", fmt.Errorf("expiry must be in the future")
	}

	role := normalizeAccessRole(accessRole)
	normalizedPermissions := normalizePermissions(permissions, role)
	if projectKey != "textile-erp" {
		role = "customer"
		normalizedPermissions = nil
		canManageTeam = false
		requiresSetup = false
		allowFinancial = false
		allowOperational = false
		allowWeaving = false
	} else {
		allowFinancial, allowOperational, allowWeaving = normalizeModuleAccess(projectKey, allowFinancial, allowOperational, allowWeaving)
		if !allowFinancial {
			normalizedPermissions = nil
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	items, err := readAccesses(a.accessFile)
	if err != nil {
		return projectAccess{}, "", err
	}

	index := -1
	for i, item := range items {
		if item.ID == id {
			index = i
			break
		}
	}
	if index == -1 {
		return projectAccess{}, "", fmt.Errorf("access not found")
	}

	record := items[index]
	if trialEndsAt.IsZero() && projectKey == "textile-erp" {
		trialEndsAt = record.TrialEndsAt
	} else if projectKey != "textile-erp" {
		trialEndsAt = time.Time{}
	}
	trialActive := !trialEndsAt.IsZero() && trialEndsAt.After(time.Now())
	if projectKey == "textile-erp" && !allowFinancial && !allowOperational && !allowWeaving && !trialActive {
		return projectAccess{}, "", fmt.Errorf("حداقل یکی از بخش‌های مالی، عملیاتی یا راندمان سالن باید فعال باشد")
	}
	if projectKey == "textile-erp" && requiresSetup && (allowWeaving || trialActive) {
		return projectAccess{}, "", fmt.Errorf("برای فعال‌سازی راندمان سالن، نام کاربری و رمز مدیر را همین حالا تعیین کنید")
	}
	previousAllowWeaving := effectiveAllowWeaving(record)
	if requiresSetup {
		username = ""
		password = ""
	} else if username == "" {
		username = uniqueGeneratedUsername(items, projectKey, contactName, companyName, financialCompanyID, id)
	}
	if !requiresSetup && password != "" {
		if err := validateAccessPassword(password); err != nil {
			return projectAccess{}, "", err
		}
	}
	if accessUsernameTaken(items, projectAccess{
		ProjectKey:         projectKey,
		CompanyName:        companyName,
		FinancialCompanyID: financialCompanyID,
	}, username, id) {
		return projectAccess{}, "", fmt.Errorf("این نام کاربری قبلا برای این شرکت ثبت شده است.")
	}

	record.ProjectKey = projectKey
	record.CompanyName = companyName
	record.ContactName = contactName
	record.Username = username
	record.FinancialCompanyID = financialCompanyID
	record.AccessRole = role
	record.Permissions = normalizedPermissions
	record.CanManageTeam = canManageTeam
	record.RequiresSetup = requiresSetup
	record.AllowFinancial = allowFinancial
	record.AllowOperational = allowOperational
	record.AllowWeaving = allowWeaving
	record.ModuleAccessSet = projectKey == "textile-erp"
	record.TrialEndsAt = trialEndsAt.UTC()
	record.ExpiresAt = expiresAt.UTC()
	record.Notes = notes

	rawPassword := ""
	if requiresSetup {
		record.PasswordHash = ""
		record.PasswordEnc = ""
		record.MustChangePassword = false
	} else {
		record.MustChangePassword = false
		rawPassword = a.mustDecryptPassword(record)
		if strings.TrimSpace(password) != "" {
			passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
			if err != nil {
				return projectAccess{}, "", err
			}
			passwordEnc, err := a.encryptPassword(password)
			if err != nil {
				return projectAccess{}, "", err
			}
			record.PasswordHash = string(passwordHash)
			record.PasswordEnc = passwordEnc
			rawPassword = password
			record.MustChangePassword = false
		}
	}
	downstreamPassword := strings.TrimSpace(rawPassword)
	if downstreamPassword == "" && (allowOperational || allowWeaving || trialActive) {
		downstreamPassword, err = randomHex(24)
		if err != nil {
			return projectAccess{}, "", err
		}
	}
	if allowOperational || trialActive {
		if _, err := a.provisionTextileTenant(financialCompanyID, id, companyName, contactName, username, downstreamPassword, role); err != nil {
			return projectAccess{}, "", err
		}
	}
	weavingTenantID := strings.TrimSpace(record.WeavingTenantID)
	if weavingTenantID == "" {
		weavingTenantID = existingWeavingTenantID(items, financialCompanyID)
	}
	if (allowWeaving || trialActive) && weavingTenantID == "" {
		if role != "owner" {
			return projectAccess{}, "", fmt.Errorf("ابتدا باید راندمان سالن برای مدیر اصلی این مشتری فعال شود")
		}
		weavingTenantID, err = a.provisionWeavingTenant(financialCompanyID, id, companyName, contactName, username, downstreamPassword)
		if err != nil {
			return projectAccess{}, "", err
		}
	}
	record.WeavingTenantID = weavingTenantID
	if weavingTenantID != "" && (allowWeaving || trialActive || previousAllowWeaving) {
		if err := a.syncWeavingUser(record, downstreamPassword, (allowWeaving || trialActive) && record.IsActive); err != nil {
			return projectAccess{}, "", err
		}
	}

	items[index] = record
	if err := writeAccesses(a.accessFile, items); err != nil {
		return projectAccess{}, "", err
	}
	return record, rawPassword, nil
}

func (a *portalApp) grantFullTrial(id int64, days int) (projectAccess, string, error) {
	if days < 1 || days > 90 {
		return projectAccess{}, "", fmt.Errorf("مدت تست باید بین ۱ تا ۹۰ روز باشد")
	}
	items, err := a.listAccesses()
	if err != nil {
		return projectAccess{}, "", err
	}
	var target projectAccess
	for _, item := range items {
		if item.ID == id {
			target = item
			break
		}
	}
	if target.ID == 0 || target.ProjectKey != "textile-erp" {
		return projectAccess{}, "", fmt.Errorf("حساب مشتری Textile ERP پیدا نشد")
	}
	if effectiveAccessRole(target) != "owner" {
		return projectAccess{}, "", fmt.Errorf("تست کامل باید برای حساب مدیر اصلی شرکت فعال شود")
	}
	trialEndsAt := time.Now().Add(time.Duration(days) * 24 * time.Hour)
	expiresAt := target.ExpiresAt
	if expiresAt.Before(trialEndsAt) {
		expiresAt = trialEndsAt
	}
	notes := strings.TrimSpace(target.Notes + "\n" + fmt.Sprintf("تست رایگان کامل %d روزه تا %s", days, trialEndsAt.Local().Format(timeLayout)))

	// Persist the customer's trial entitlement before provisioning downstream
	// modules. A temporary outage in one module must not make the whole trial
	// disappear or cause the Viora customer portal to report a false failure.
	a.mu.Lock()
	items, readErr := readAccesses(a.accessFile)
	found := false
	if readErr == nil {
		for index := range items {
			if items[index].ID == target.ID {
				found = true
				items[index].TrialEndsAt = trialEndsAt.UTC()
				items[index].ExpiresAt = expiresAt.UTC()
				items[index].Notes = notes
				target = items[index]
				readErr = writeAccesses(a.accessFile, items)
				break
			}
		}
	}
	a.mu.Unlock()
	if readErr != nil {
		return projectAccess{}, "", readErr
	}
	if !found {
		return projectAccess{}, "", errors.New("access not found")
	}

	updated, rawPassword, updateErr := a.updateManagedAccess(
		target.ID,
		target.ProjectKey,
		target.CompanyName,
		target.ContactName,
		target.Username,
		"",
		target.FinancialCompanyID,
		expiresAt,
		trialEndsAt,
		notes,
		effectiveAccessRole(target),
		effectivePermissions(target),
		effectiveCanManageTeam(target),
		false,
		target.AllowFinancial,
		target.AllowOperational,
		target.AllowWeaving,
	)
	if updateErr != nil {
		log.Printf("trial entitlement active; downstream provisioning deferred for access %d: %v", target.ID, updateErr)
		return target, "", nil
	}
	return updated, rawPassword, nil
}

func (a *portalApp) setAccessMustChangePassword(id int64, mustChange bool) (projectAccess, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	items, err := readAccesses(a.accessFile)
	if err != nil {
		return projectAccess{}, err
	}
	for index := range items {
		if items[index].ID != id {
			continue
		}
		if items[index].RequiresSetup {
			mustChange = false
		}
		items[index].MustChangePassword = mustChange
		if err := writeAccesses(a.accessFile, items); err != nil {
			return projectAccess{}, err
		}
		return items[index], nil
	}
	return projectAccess{}, os.ErrNotExist
}

func (a *portalApp) finalizeAccessSetup(token, contactName, username, password string) (projectAccess, error) {
	contactName = strings.TrimSpace(contactName)
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	if username == "" || password == "" {
		return projectAccess{}, fmt.Errorf("نام کاربری و رمز عبور الزامی است.")
	}
	if err := validateAccessPassword(password); err != nil {
		return projectAccess{}, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return projectAccess{}, err
	}
	enc, err := a.encryptPassword(password)
	if err != nil {
		return projectAccess{}, err
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	items, err := readAccesses(a.accessFile)
	if err != nil {
		return projectAccess{}, err
	}
	index := -1
	for i, item := range items {
		if item.AccessToken == token {
			index = i
			break
		}
	}
	if index == -1 {
		return projectAccess{}, os.ErrNotExist
	}
	record := items[index]
	if accessUsernameTaken(items, record, username, record.ID) {
		return projectAccess{}, fmt.Errorf("این نام کاربری قبلا برای این شرکت ثبت شده است.")
	}
	if contactName != "" {
		record.ContactName = contactName
	}
	record.Username = username
	record.PasswordHash = string(hash)
	record.PasswordEnc = enc
	record.RequiresSetup = false
	record.MustChangePassword = false
	if record.ProjectKey == "textile-erp" && record.AccessRole == "" {
		record.AccessRole = "owner"
		record.Permissions = defaultPermissionsForRole("owner")
		record.CanManageTeam = true
	}
	record.AllowFinancial, record.AllowOperational, record.AllowWeaving = normalizeModuleAccess(record.ProjectKey, record.AllowFinancial, record.AllowOperational, record.AllowWeaving)
	items[index] = record
	if err := writeAccesses(a.accessFile, items); err != nil {
		return projectAccess{}, err
	}
	return record, nil
}

func (a *portalApp) changeTemporaryPassword(token, password string) (projectAccess, error) {
	password = strings.TrimSpace(password)
	if err := validateAccessPassword(password); err != nil {
		return projectAccess{}, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return projectAccess{}, err
	}
	enc, err := a.encryptPassword(password)
	if err != nil {
		return projectAccess{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	items, err := readAccesses(a.accessFile)
	if err != nil {
		return projectAccess{}, err
	}
	for index := range items {
		if items[index].AccessToken != token {
			continue
		}
		items[index].PasswordHash = string(hash)
		items[index].PasswordEnc = enc
		items[index].MustChangePassword = false
		if err := writeAccesses(a.accessFile, items); err != nil {
			return projectAccess{}, err
		}
		return items[index], nil
	}
	return projectAccess{}, os.ErrNotExist
}

func (a *portalApp) tenantAccesses(record projectAccess) ([]projectAccess, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	items, err := readAccesses(a.accessFile)
	if err != nil {
		return nil, err
	}
	out := make([]projectAccess, 0, len(items))
	for _, item := range items {
		if sameTenantAccess(record, item) {
			out = append(out, item)
		}
	}
	sortAccesses(out)
	return out, nil
}

func (a *portalApp) tenantAccessByID(owner projectAccess, id int64) (projectAccess, error) {
	items, err := a.tenantAccesses(owner)
	if err != nil {
		return projectAccess{}, err
	}
	for _, item := range items {
		if item.ID == id {
			return item, nil
		}
	}
	return projectAccess{}, os.ErrNotExist
}

func (a *portalApp) findAccessByToken(token string) (projectAccess, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	items, err := readAccesses(a.accessFile)
	if err != nil {
		return projectAccess{}, err
	}
	for _, item := range items {
		if item.AccessToken == token {
			return item, nil
		}
	}
	return projectAccess{}, os.ErrNotExist
}

func (a *portalApp) repairAccessHashes() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	items, err := readAccesses(a.accessFile)
	if err != nil {
		return err
	}
	changed := false
	for i := range items {
		if strings.TrimSpace(items[i].PasswordHash) != "" || strings.TrimSpace(items[i].PasswordEnc) == "" {
			continue
		}
		rawPassword, err := a.decryptPassword(items[i].PasswordEnc)
		if err != nil || strings.TrimSpace(rawPassword) == "" {
			continue
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(rawPassword), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		items[i].PasswordHash = string(hash)
		changed = true
	}
	if !changed {
		return nil
	}
	return writeAccesses(a.accessFile, items)
}

func (a *portalApp) deleteAccess(id int64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	items, err := readAccesses(a.accessFile)
	if err != nil {
		return err
	}
	next := make([]projectAccess, 0, len(items))
	removed := false
	for _, item := range items {
		if item.ID == id {
			if strings.TrimSpace(item.WeavingTenantID) != "" {
				if err := a.syncWeavingUser(item, a.mustDecryptPassword(item), false); err != nil {
					return fmt.Errorf("غیرفعال‌سازی حساب راندمان انجام نشد: %w", err)
				}
			}
			removed = true
			continue
		}
		next = append(next, item)
	}
	if !removed {
		return fmt.Errorf("دسترسی موردنظر پیدا نشد")
	}
	return writeAccesses(a.accessFile, next)
}

func (a *portalApp) rotateAccessToken(id int64) (projectAccess, error) {
	token, err := randomHex(24)
	if err != nil {
		return projectAccess{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	items, err := readAccesses(a.accessFile)
	if err != nil {
		return projectAccess{}, err
	}
	for i := range items {
		if items[i].ID != id {
			continue
		}
		items[i].AccessToken = token
		if err := writeAccesses(a.accessFile, items); err != nil {
			return projectAccess{}, err
		}
		return items[i], nil
	}
	return projectAccess{}, os.ErrNotExist
}

func (a *portalApp) toggleAccess(id int64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	items, err := readAccesses(a.accessFile)
	if err != nil {
		return err
	}
	found := false
	for i := range items {
		if items[i].ID == id {
			nextActive := !items[i].IsActive
			if strings.TrimSpace(items[i].WeavingTenantID) != "" {
				if err := a.syncWeavingUser(items[i], a.mustDecryptPassword(items[i]), nextActive && effectiveAllowWeaving(items[i])); err != nil {
					return fmt.Errorf("همگام‌سازی حساب راندمان انجام نشد: %w", err)
				}
			}
			items[i].IsActive = nextActive
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("دسترسی موردنظر پیدا نشد")
	}
	return writeAccesses(a.accessFile, items)
}

func (a *portalApp) markAccessUsed(id int64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	items, err := readAccesses(a.accessFile)
	if err != nil {
		return err
	}
	for i := range items {
		if items[i].ID == id {
			items[i].LastUsedAt = time.Now().UTC()
			return writeAccesses(a.accessFile, items)
		}
	}
	return nil
}

func (a *portalApp) verifyAccessPassword(token, password string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	items, err := readAccesses(a.accessFile)
	if err != nil {
		return err
	}
	for i, item := range items {
		if item.AccessToken == token {
			if strings.TrimSpace(item.PasswordHash) == "" && strings.TrimSpace(item.PasswordEnc) != "" {
				rawPassword, err := a.decryptPassword(item.PasswordEnc)
				if err != nil {
					return err
				}
				if subtleConstantTimeCompare(rawPassword, password) {
					hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
					if err != nil {
						return err
					}
					items[i].PasswordHash = string(hash)
					if err := writeAccesses(a.accessFile, items); err != nil {
						return err
					}
					return nil
				}
			}
			return bcrypt.CompareHashAndPassword([]byte(item.PasswordHash), []byte(password))
		}
	}
	return os.ErrNotExist
}

func subtleConstantTimeCompare(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	return hmac.Equal([]byte(left), []byte(right))
}

func (a *portalApp) accessResponse(record projectAccess, rawPassword string) map[string]any {
	if accessRequiresSetup(record) {
		rawPassword = ""
	} else if rawPassword == "" {
		rawPassword = a.portalAccessPassword(record)
	}
	accessLink := a.publicBase + "/access/" + record.AccessToken
	labelText := projectLabel(record.ProjectKey)
	expiresAt := record.ExpiresAt.Format(time.RFC3339)
	createdAt := record.CreatedAt.Format(time.RFC3339)
	lastUsedAt := emptyTime(record.LastUsedAt)
	isExpired := time.Now().After(record.ExpiresAt)
	trialActive := fullTrialActive(record)
	trialEndsAt := emptyTime(record.TrialEndsAt)
	trialDaysRemaining := fullTrialDaysRemaining(record, time.Now())
	role := effectiveAccessRole(record)
	permissions := effectivePermissions(record)
	return map[string]any{
		"id":                   record.ID,
		"project_key":          record.ProjectKey,
		"projectKey":           record.ProjectKey,
		"project_label":        labelText,
		"projectLabel":         labelText,
		"company_name":         record.CompanyName,
		"companyName":          record.CompanyName,
		"contact_name":         record.ContactName,
		"contactName":          record.ContactName,
		"username":             record.Username,
		"password":             rawPassword,
		"financial_company_id": record.FinancialCompanyID,
		"financialCompanyId":   record.FinancialCompanyID,
		"access_role":          role,
		"accessRole":           role,
		"permissions":          permissions,
		"can_manage_team":      effectiveCanManageTeam(record),
		"canManageTeam":        effectiveCanManageTeam(record),
		"requires_setup":       accessRequiresSetup(record),
		"requiresSetup":        accessRequiresSetup(record),
		"must_change_password": record.MustChangePassword,
		"mustChangePassword":   record.MustChangePassword,
		"allow_financial":      effectiveAllowFinancial(record),
		"allowFinancial":       effectiveAllowFinancial(record),
		"allow_operational":    effectiveAllowOperational(record),
		"allowOperational":     effectiveAllowOperational(record),
		"allow_weaving":        effectiveAllowWeaving(record),
		"allowWeaving":         effectiveAllowWeaving(record),
		"weaving_tenant_id":    record.WeavingTenantID,
		"weavingTenantId":      record.WeavingTenantID,
		"trial_active":         trialActive,
		"trialActive":          trialActive,
		"trial_ends_at":        trialEndsAt,
		"trialEndsAt":          trialEndsAt,
		"trial_days_remaining": trialDaysRemaining,
		"trialDaysRemaining":   trialDaysRemaining,
		"module_access_label":  accessModuleLabel(record),
		"moduleAccessLabel":    accessModuleLabel(record),
		"expires_at":           expiresAt,
		"expiresAt":            expiresAt,
		"access_token":         record.AccessToken,
		"accessToken":          record.AccessToken,
		"access_link":          accessLink,
		"accessLink":           accessLink,
		"target_url":           a.customerTargetHint(record),
		"targetUrl":            a.customerTargetHint(record),
		"notes":                record.Notes,
		"is_active":            record.IsActive,
		"isActive":             record.IsActive,
		"is_expired":           isExpired,
		"isExpired":            isExpired,
		"created_at":           createdAt,
		"createdAt":            createdAt,
		"last_used_at":         lastUsedAt,
		"lastUsedAt":           lastUsedAt,
	}
}
func (a *portalApp) encryptPassword(password string) (string, error) {
	key := sha256.Sum256([]byte(a.sessionSecret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(password), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

func (a *portalApp) decryptPassword(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("empty password")
	}
	key := sha256.Sum256([]byte(a.sessionSecret))
	payload, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(payload) < gcm.NonceSize() {
		return "", fmt.Errorf("invalid password payload")
	}
	nonce, ciphertext := payload[:gcm.NonceSize()], payload[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func (a *portalApp) mustDecryptPassword(record projectAccess) string {
	password, err := a.decryptPassword(record.PasswordEnc)
	if err != nil {
		return ""
	}
	return password
}

func (a *portalApp) accessRecordFromRequest(r *http.Request) (projectAccess, error) {
	cookie, err := r.Cookie(accessCookieName)
	if err != nil {
		return projectAccess{}, err
	}
	token := strings.TrimSpace(cookie.Value)
	if token == "" {
		return projectAccess{}, errors.New("missing access token")
	}
	record, err := a.findAccessByToken(token)
	if err != nil {
		return projectAccess{}, err
	}
	if !record.IsActive {
		return projectAccess{}, errors.New("access is inactive")
	}
	if time.Now().After(record.ExpiresAt) {
		return projectAccess{}, errors.New("access is expired")
	}
	return record, nil
}

func (a *portalApp) signFinancialJWT(record projectAccess) (string, error) {
	if strings.TrimSpace(a.financialJWTKey) == "" {
		return "", fmt.Errorf("financial jwt secret is not configured in portal")
	}
	if record.FinancialCompanyID <= 0 {
		return "", fmt.Errorf("financial company id is not configured for this customer access")
	}
	claims := map[string]any{
		"user_id":           record.ID,
		"company_id":        record.FinancialCompanyID,
		"role":              "customer",
		"portal_role":       effectiveAccessRole(record),
		"username":          record.Username,
		"display_name":      record.ContactName,
		"company_name":      record.CompanyName,
		"project_key":       record.ProjectKey,
		"access_token":      record.AccessToken,
		"permissions":       effectivePermissions(record),
		"can_manage_team":   effectiveCanManageTeam(record),
		"allow_financial":   effectiveAllowFinancial(record),
		"allow_operational": effectiveAllowOperational(record),
		"allow_weaving":     effectiveAllowWeaving(record),
		"iat":               time.Now().Unix(),
		"exp":               minTime(record.ExpiresAt, time.Now().Add(15*time.Minute)).Unix(),
	}
	headerBytes, err := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	if err != nil {
		return "", err
	}
	payloadBytes, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	head := base64.RawURLEncoding.EncodeToString(headerBytes)
	body := base64.RawURLEncoding.EncodeToString(payloadBytes)
	unsigned := head + "." + body
	mac := hmac.New(sha256.New, []byte(a.financialJWTKey))
	_, _ = mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}
func operationalPortalMenus() []map[string]any {
	return operationalPortalMenusForKeys([]string{"*"})
}

func operationalPortalMenusForKeys(keys []string) []map[string]any {
	catalog := []map[string]any{
		{"menu_key": "dashboard", "menu_name": "داشبورد", "path": "/", "icon": "", "is_restricted": 0, "has_access": 1},
		{"menu_key": "initial", "menu_name": "اطلاعات اولیه", "path": "/initial", "icon": "", "is_restricted": 0, "has_access": 1},
		{"menu_key": "nakh-vor", "menu_name": "ورود نخ", "path": "/nakh-vor", "icon": "", "is_restricted": 0, "has_access": 1},
		{"menu_key": "chelle", "menu_name": "ورود چله", "path": "/chelle", "icon": "", "is_restricted": 0, "has_access": 1},
		{"menu_key": "gere", "menu_name": "گره", "path": "/gere", "icon": "", "is_restricted": 0, "has_access": 1},
		{"menu_key": "nakh-salon", "menu_name": "ورود نخ سالن", "path": "/nakh-salon", "icon": "", "is_restricted": 0, "has_access": 1},
		{"menu_key": "formulas", "menu_name": "فرمول تولید ماشین‌ها", "path": "/formulas", "icon": "", "is_restricted": 0, "has_access": 1},
		{"menu_key": "salon", "menu_name": "سالن تولید", "path": "/salon", "icon": "", "is_restricted": 0, "has_access": 1},
		{"menu_key": "consumption", "menu_name": "مصرف تار و پود", "path": "/consumption", "icon": "", "is_restricted": 0, "has_access": 1},
		{"menu_key": "yarn-out", "menu_name": "خروج نخ", "path": "/yarn-out", "icon": "", "is_restricted": 0, "has_access": 1},
		{"menu_key": "empty-beam-out", "menu_name": "خروج نورد خالی", "path": "/empty-beam-out", "icon": "", "is_restricted": 0, "has_access": 1},
		{"menu_key": "out-invoice", "menu_name": "فاکتور خروج", "path": "/out-invoice", "icon": "", "is_restricted": 0, "has_access": 1},
		{"menu_key": "advisor", "menu_name": "تحلیل و مشاور هوشمند", "path": "/advisor", "icon": "", "is_restricted": 0, "has_access": 1},
		{"menu_key": "expenses", "menu_name": "هزینه‌ها", "path": "/expenses", "icon": "", "is_restricted": 0, "has_access": 1},
		{"menu_key": "reports", "menu_name": "گزارشات", "path": "/reports", "icon": "", "is_restricted": 0, "has_access": 1},
		{"menu_key": "database", "menu_name": "مدیریت دیتابیس", "path": "/database", "icon": "", "is_restricted": 1, "has_access": 1},
		{"menu_key": "machinery-services", "menu_name": "خدمات ماشین‌آلات", "path": "/machinery-services", "icon": "", "is_restricted": 1, "has_access": 1},
		{"menu_key": "spare-parts", "menu_name": "موجودی انبار قطعات", "path": "/spare-parts", "icon": "", "is_restricted": 1, "has_access": 1},
	}
	allowed := make(map[string]bool, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key != "" {
			allowed[key] = true
		}
	}
	out := make([]map[string]any, 0, len(catalog))
	for _, item := range catalog {
		key, _ := item["menu_key"].(string)
		if allowed["*"] || allowed[key] {
			out = append(out, item)
		}
	}
	return out
}

func validatePortalProductionConfig(adminPassword, sessionSecret, financialJWTKey, operationalSessionSecret string) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production") {
		return
	}
	if len(strings.TrimSpace(adminPassword)) < 16 || strings.Contains(strings.ToLower(adminPassword), "change_this") || adminPassword == "admin123" {
		log.Fatal("PORTAL_ADMIN_PASSWORD must contain at least 16 secure characters in production")
	}
	values := map[string]string{
		"PORTAL_SESSION_SECRET":       sessionSecret,
		"PORTAL_FINANCIAL_JWT_SECRET": financialJWTKey,
		"PORTAL_OPERATIONAL_SECRET":   operationalSessionSecret,
	}
	for key, value := range values {
		if len(strings.TrimSpace(value)) < 32 || strings.Contains(strings.ToLower(value), "change_this") || value == "admin123" {
			log.Fatalf("%s must contain at least 32 secure characters in production", key)
		}
	}
}

func (a *portalApp) projectTarget(projectKey string) string {
	switch projectKey {
	case "cooler-store":
		return a.coolerStoreURL + "/"
	default:
		return a.publicBase + "/"
	}
}

func (a *portalApp) customerTargetHint(record projectAccess) string {
	switch record.ProjectKey {
	case "cooler-store":
		return a.coolerStoreURL + "/"
	case "textile-erp":
		moduleCount := 0
		if effectiveAllowFinancial(record) {
			moduleCount++
		}
		if effectiveAllowOperational(record) {
			moduleCount++
		}
		if effectiveAllowWeaving(record) {
			moduleCount++
		}
		if moduleCount > 1 {
			return a.publicBase + "/"
		}
		if effectiveAllowWeaving(record) {
			return a.publicBase + "/module-login?module=weaving"
		}
		if effectiveAllowOperational(record) {
			return a.publicBase + "/operational/"
		}
		if effectiveAllowFinancial(record) {
			return a.publicBase + "/financial/"
		}
		return a.publicBase + "/"
	default:
		return a.projectTarget(record.ProjectKey)
	}
}

func (a *portalApp) accessTarget(record projectAccess) string {
	if record.ProjectKey == "cooler-store" {
		loginURL, err := a.coolerStorePortalLoginURL(record)
		if err == nil {
			return loginURL
		}
	}
	if record.ProjectKey == "textile-erp" {
		moduleCount := 0
		if effectiveAllowFinancial(record) {
			moduleCount++
		}
		if effectiveAllowOperational(record) {
			moduleCount++
		}
		if effectiveAllowWeaving(record) {
			moduleCount++
		}
		if moduleCount > 1 {
			return "/"
		}
		if effectiveAllowWeaving(record) {
			return "/module-login?module=weaving"
		}
		if effectiveAllowOperational(record) {
			return "/operational/"
		}
		if effectiveAllowFinancial(record) {
			return "/financial/"
		}
		return "/"
	}
	return a.projectTarget(record.ProjectKey)
}

func (a *portalApp) setPortalAccessCookie(w http.ResponseWriter, r *http.Request, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     accessCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isSecureRequest(r),
		Expires:  minTime(expiresAt, time.Now().Add(24*time.Hour)),
	})
}

func (a *portalApp) coolerStorePortalLoginURL(record projectAccess) (string, error) {
	token, err := a.signCoolerStorePortalToken(record)
	if err != nil {
		return "", err
	}
	return a.coolerStoreURL + "/api/portal-login?token=" + url.QueryEscape(token), nil
}

func (a *portalApp) signCoolerStorePortalToken(record projectAccess) (string, error) {
	if strings.TrimSpace(a.coolerStoreSecret) == "" {
		return "", fmt.Errorf("cooler-store portal secret is not configured")
	}
	payload := map[string]any{
		"iss":          "erp-portal",
		"project_key":  record.ProjectKey,
		"username":     record.Username,
		"company_name": record.CompanyName,
		"contact_name": record.ContactName,
		"password":     a.portalAccessPassword(record),
		"exp":          minTime(record.ExpiresAt, time.Now().Add(5*time.Minute)).Unix(),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, []byte(a.coolerStoreSecret))
	_, _ = mac.Write([]byte(encoded))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encoded + "." + signature, nil
}

func (a *portalApp) portalAccessPassword(record projectAccess) string {
	password := strings.TrimSpace(a.mustDecryptPassword(record))
	if password != "" {
		return password
	}
	tokenSuffix := record.AccessToken
	if len(tokenSuffix) > 8 {
		tokenSuffix = tokenSuffix[:8]
	}
	return "Portal@" + record.Username + "_" + tokenSuffix
}

func (a *portalApp) isAdminAuthenticated(r *http.Request) bool {
	cookie, err := r.Cookie(adminCookieName)
	if err != nil {
		return false
	}
	return a.verifyAdminSession(cookie.Value)
}

func (a *portalApp) signAdminSession(exp time.Time) string {
	payload := strconv.FormatInt(exp.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(a.sessionSecret))
	mac.Write([]byte(payload))
	return payload + "." + hex.EncodeToString(mac.Sum(nil))
}

func (a *portalApp) verifyAdminSession(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return false
	}
	expUnix, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || time.Now().Unix() > expUnix {
		return false
	}
	mac := hmac.New(sha256.New, []byte(a.sessionSecret))
	mac.Write([]byte(parts[0]))
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(parts[1]))
}

func (a *portalApp) accessCookieValid(r *http.Request, token string) bool {
	cookie, err := r.Cookie(accessCookieName)
	if err != nil {
		return false
	}
	return cookie.Value == token
}

func ensureAccessStore(path string) error {
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.WriteFile(path, []byte("[]\n"), 0o600)
}

func readAccesses(path string) ([]projectAccess, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 {
		return []projectAccess{}, nil
	}
	var items []projectAccess
	if err := json.Unmarshal(payload, &items); err != nil {
		return nil, err
	}
	var rawItems []map[string]json.RawMessage
	_ = json.Unmarshal(payload, &rawItems)
	for i := range items {
		if items[i].ProjectKey == "textile-erp" {
			if items[i].AccessRole == "" {
				items[i].AccessRole = "owner"
				items[i].Permissions = defaultPermissionsForRole("owner")
				items[i].CanManageTeam = true
			}
			if i < len(rawItems) {
				_, hasFinancialFlag := rawItems[i]["allow_financial"]
				_, hasOperationalFlag := rawItems[i]["allow_operational"]
				_, hasWeavingFlag := rawItems[i]["allow_weaving"]
				if !items[i].ModuleAccessSet && !hasFinancialFlag && !hasOperationalFlag && !hasWeavingFlag {
					items[i].AllowFinancial = true
					items[i].AllowOperational = true
				}
			}
			items[i].AllowFinancial, items[i].AllowOperational, items[i].AllowWeaving = normalizeModuleAccess(items[i].ProjectKey, items[i].AllowFinancial, items[i].AllowOperational, items[i].AllowWeaving)
			if items[i].AllowFinancial && len(items[i].Permissions) == 0 {
				items[i].Permissions = defaultPermissionsForRole(items[i].AccessRole)
			}
		}
	}
	return items, nil
}

func writeAccesses(path string, items []projectAccess) error {
	payload, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, payload, 0o600); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

func nextAccessID(items []projectAccess) int64 {
	var maxID int64
	for _, item := range items {
		if item.ID > maxID {
			maxID = item.ID
		}
	}
	return maxID + 1
}

func sortAccesses(items []projectAccess) {
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[j].CreatedAt.After(items[i].CreatedAt) || (items[j].CreatedAt.Equal(items[i].CreatedAt) && items[j].ID > items[i].ID) {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
}

func resolveExpiry(expiresAt string, trialDays int) (time.Time, error) {
	if strings.TrimSpace(expiresAt) != "" {
		value := strings.TrimSpace(expiresAt)
		for _, layout := range []string{"2006-01-02T15:04", "2006-01-02T15:04:05", time.RFC3339, "2006-01-02 15:04", "2006-01-02 15:04:05"} {
			parsed, err := time.Parse(layout, value)
			if err == nil {
				return parsed, nil
			}
		}
		return time.Time{}, fmt.Errorf("تاریخ انقضا معتبر نیست")
	}
	if trialDays <= 0 {
		trialDays = 30
	}
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, now.Location()).AddDate(0, 0, trialDays), nil
}

func parseStoredTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02 15:04"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.Local()
		}
	}
	return time.Time{}
}

func emptyTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func randomHex(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func projectLabel(key string) string {
	switch key {
	case "cooler-store":
		return "Cooler Store"
	default:
		return "Textile ERP"
	}
}

func validProject(key string) bool {
	return key == "textile-erp" || key == "cooler-store"
}

func normalizeBaseURL(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = fallback
	}
	if !strings.Contains(value, "://") {
		value = "http://" + value
	}
	return strings.TrimRight(value, "/")
}

func (a *portalApp) signOperationalSession(record projectAccess) (string, error) {
	if strings.TrimSpace(a.operationalSessionSecret) == "" {
		return "", errors.New("operational session secret is not configured")
	}
	claims := map[string]any{
		"user_id":           record.ID,
		"company_id":        record.FinancialCompanyID,
		"username":          record.Username,
		"role":              effectiveAccessRole(record),
		"menu_keys":         operationalMenuKeys(record),
		"can_manage_team":   effectiveCanManageTeam(record),
		"allow_operational": effectiveAllowOperational(record),
		"exp":               minTime(record.ExpiresAt, time.Now().Add(15*time.Minute)).Unix(),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(a.operationalSessionSecret))
	_, _ = mac.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func operationalMenuKeys(record projectAccess) []string {
	switch effectiveAccessRole(record) {
	case "owner":
		return []string{"*"}
	case "manager":
		return []string{
			"dashboard", "initial", "nakh-vor", "chelle", "gere", "nakh-salon", "formulas",
			"salon", "consumption", "yarn-out", "empty-beam-out", "out-invoice", "expenses",
			"advisor", "reports", "machinery-services", "spare-parts",
		}
	case "accountant":
		return []string{"dashboard", "advisor", "reports", "out-invoice", "expenses"}
	default:
		return []string{"dashboard", "advisor", "reports"}
	}
}

func isLocalPortalRequest(r *http.Request) bool {
	host := strings.TrimSpace(r.Host)
	host = strings.Split(host, ":")[0]
	switch strings.ToLower(host) {
	case "127.0.0.1", "localhost", "::1":
		return true
	default:
		return false
	}
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	switch strings.ToLower(value) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func envInt64(key string, fallback int64) int64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func isSecureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func setPrivatePageHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Header().Set("Vary", "Cookie")
}

func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
