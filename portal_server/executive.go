package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

//go:embed executive/*
var executiveFiles embed.FS

var executivePublicAssets = map[string]bool{
	"app.js":               true,
	"styles.css":           true,
	"manifest.webmanifest": true,
	"sw.js":                true,
	"offline.html":         true,
	"icon-192.png":         true,
	"icon-512.png":         true,
}

func executiveAllowed(record projectAccess) bool {
	role := normalizeAccessRole(effectiveAccessRole(record))
	return record.ProjectKey == "textile-erp" &&
		(role == "owner" || role == "manager") &&
		effectiveAllowFinancial(record) &&
		effectiveAllowOperational(record) &&
		!record.MustChangePassword &&
		!accessRequiresSetup(record)
}

func setExecutiveSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
}

func (a *portalApp) executiveApp(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/executive" {
		http.Redirect(w, r, "/executive/", http.StatusPermanentRedirect)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/executive/") {
		http.NotFound(w, r)
		return
	}

	assetName := strings.TrimPrefix(r.URL.Path, "/executive/")
	if assetName != "" {
		a.serveExecutiveAsset(w, r, assetName)
		return
	}

	record, err := a.accessRecordFromRequest(r)
	if err != nil {
		http.Redirect(w, r, "/login?next=%2Fexecutive%2F", http.StatusSeeOther)
		return
	}
	if !executiveAllowed(record) {
		setExecutiveSecurityHeaders(w)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<!doctype html><html lang="fa" dir="rtl"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>عدم دسترسی</title><style>body{font-family:Tahoma;background:#071416;color:#f5f7f2;display:grid;place-items:center;min-height:100vh;margin:0;padding:24px}.box{max-width:620px;border:1px solid #31555a;border-radius:20px;background:#0d2529;padding:28px;line-height:2}a{color:#f4c96b}</style><main class="box"><h1>مرکز فرمان فقط برای مدیر فعال است</h1><p>این صفحه به دسترسی هم‌زمان مالی و عملیاتی و نقش مدیر یا مالک نیاز دارد.</p><a href="/">بازگشت به درگاه نساجی</a></main></html>`))
		return
	}

	setExecutiveSecurityHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'self'; form-action 'self'")
	data, err := executiveFiles.ReadFile("executive/index.html")
	if err != nil {
		http.Error(w, "executive app is unavailable", http.StatusInternalServerError)
		return
	}
	if r.Method == http.MethodHead {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_, _ = w.Write(data)
}

func (a *portalApp) serveExecutiveAsset(w http.ResponseWriter, r *http.Request, assetName string) {
	assetName = path.Clean(strings.TrimSpace(assetName))
	if assetName == "." || strings.Contains(assetName, "/") || !executivePublicAssets[assetName] {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	data, err := executiveFiles.ReadFile("executive/" + assetName)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	setExecutiveSecurityHeaders(w)
	contentType := mime.TypeByExtension(path.Ext(assetName))
	if assetName == "manifest.webmanifest" {
		contentType = "application/manifest+json; charset=utf-8"
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	if assetName == "sw.js" || assetName == "manifest.webmanifest" {
		w.Header().Set("Cache-Control", "no-cache")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=86400")
	}
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(data)
}

func (a *portalApp) executiveSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	setExecutiveSecurityHeaders(w)
	record, err := a.accessRecordFromRequest(r)
	if err != nil {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "نشست مدیر معتبر نیست؛ دوباره وارد شوید."})
		return
	}
	if !executiveAllowed(record) {
		respondJSON(w, http.StatusForbidden, map[string]string{"error": "دسترسی مرکز فرمان برای این کاربر فعال نیست."})
		return
	}

	financialToken, financialErr := a.signFinancialJWT(record)
	operationalData, operationalErr := a.createOperationalSessionForRecord(w, r, record)
	response := map[string]any{
		"company":            record.CompanyName,
		"displayName":        record.ContactName,
		"username":           record.Username,
		"financialReady":     financialErr == nil,
		"operationalReady":   operationalErr == nil,
		"hallMonitorReady":   strings.TrimSpace(a.weavingMonitorURL) != "",
		"financialToken":     financialToken,
		"financialExpiresAt": minTime(record.ExpiresAt, time.Now().Add(15*time.Minute)).UTC().Format(time.RFC3339),
		"refreshSeconds":     60,
	}
	if operationalErr == nil {
		response["operational"] = operationalData
	} else {
		response["operationalMessage"] = "ارتباط با داده‌های عملیاتی برقرار نشد."
	}
	if financialErr != nil {
		response["financialMessage"] = "ارتباط با داده‌های مالی برقرار نشد."
	}
	respondJSON(w, http.StatusOK, response)
}

func (a *portalApp) executiveHallSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	setExecutiveSecurityHeaders(w)
	record, err := a.accessRecordFromRequest(r)
	if err != nil {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "نشست مدیر معتبر نیست."})
		return
	}
	if !executiveAllowed(record) {
		respondJSON(w, http.StatusForbidden, map[string]string{"error": "دسترسی مرکز فرمان برای این کاربر فعال نیست."})
		return
	}
	endpoint := strings.TrimSpace(a.weavingMonitorURL)
	if endpoint == "" {
		respondJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error":  "نسخه تخصصی راندمان سالن هنوز به مرکز فرمان متصل نشده است.",
			"source": "operational-fallback",
		})
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, endpoint, nil)
	if err != nil {
		respondJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "نشانی سرویس راندمان معتبر نیست."})
		return
	}
	req.Header.Set("Accept", "application/json")
	if token := strings.TrimSpace(a.weavingMonitorToken); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 6 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		respondJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "سرویس تخصصی راندمان در دسترس نیست."})
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil || len(body) == 0 || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respondJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "پاسخ سرویس تخصصی راندمان قابل استفاده نیست."})
		return
	}
	var payload any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		respondJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ساختار داده راندمان معتبر نیست."})
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"source":     "specialized",
		"capturedAt": time.Now().UTC().Format(time.RFC3339),
		"data":       payload,
	})
}

func validateExecutiveMonitorConfig(endpoint, token string) error {
	if strings.TrimSpace(endpoint) == "" && strings.TrimSpace(token) != "" {
		return errors.New("monitor token is configured without a summary endpoint")
	}
	return nil
}
