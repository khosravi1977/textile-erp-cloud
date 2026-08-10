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
	"strconv"
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
	return record.ProjectKey == "textile-erp" &&
		(effectiveAllowFinancial(record) || effectiveAllowOperational(record) || effectiveAllowWeaving(record)) &&
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
		_, _ = w.Write([]byte(`<!doctype html><html lang="fa" dir="rtl"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>عدم دسترسی</title><style>body{font-family:Tahoma;background:#071416;color:#f5f7f2;display:grid;place-items:center;min-height:100vh;margin:0;padding:24px}.box{max-width:620px;border:1px solid #31555a;border-radius:20px;background:#0d2529;padding:28px;line-height:2}a{color:#f4c96b}</style><main class="box"><h1>دسترسی این داشبورد فعال نیست</h1><p>مدیر شرکت باید حداقل یکی از بخش‌های مالی، عملیاتی یا راندمان سالن را برای این کاربر فعال کند.</p><a href="/">بازگشت به درگاه نساجی</a></main></html>`))
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

	allowFinancial := effectiveAllowFinancial(record)
	allowOperational := effectiveAllowOperational(record)
	allowWeaving := effectiveAllowWeaving(record)
	var financialToken string
	var financialErr error
	if allowFinancial {
		financialToken, financialErr = a.signFinancialJWT(record)
	}
	var operationalData map[string]any
	var operationalErr error
	if allowOperational {
		operationalData, operationalErr = a.createOperationalSessionForRecord(w, r, record)
	}
	response := map[string]any{
		"company":            record.CompanyName,
		"displayName":        record.ContactName,
		"username":           record.Username,
		"allowFinancial":     allowFinancial,
		"allowOperational":   allowOperational,
		"allowWeaving":       allowWeaving,
		"financialReady":     allowFinancial && financialErr == nil,
		"operationalReady":   allowOperational && operationalErr == nil,
		"hallMonitorReady":   allowWeaving && strings.TrimSpace(record.WeavingTenantID) != "" && strings.TrimSpace(a.weavingMonitorURL) != "",
		"weavingEntryURL":    "/module-login?module=weaving",
		"financialToken":     financialToken,
		"financialExpiresAt": minTime(record.ExpiresAt, time.Now().Add(15*time.Minute)).UTC().Format(time.RFC3339),
		"refreshSeconds":     60,
	}
	if allowOperational && operationalErr == nil {
		response["operational"] = operationalData
	} else if allowOperational {
		response["operationalMessage"] = "ارتباط با داده‌های عملیاتی برقرار نشد."
	}
	if allowFinancial && financialErr != nil {
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
	if !effectiveAllowWeaving(record) {
		respondJSON(w, http.StatusForbidden, map[string]string{"error": "بخش راندمان سالن در اشتراک این مشتری فعال نیست."})
		return
	}
	if strings.TrimSpace(record.WeavingTenantID) == "" {
		respondJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "فضای اختصاصی راندمان این مشتری هنوز آماده نشده است."})
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
	req.Header.Set("X-Viora-Tenant-Id", strings.TrimSpace(record.WeavingTenantID))
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
	normalized, err := normalizeExecutiveHallPayload(payload)
	if err != nil {
		respondJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ساختار داده راندمان معتبر نیست."})
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"source":     "specialized",
		"capturedAt": time.Now().UTC().Format(time.RFC3339),
		"data":       normalized,
	})
}

func normalizeExecutiveHallPayload(payload any) (map[string]any, error) {
	root, ok := payload.(map[string]any)
	if !ok {
		return nil, errors.New("hall payload must be an object")
	}
	_, hasHall := root["hall"]
	_, hasMachines := root["machines"]
	_, hasFlatEfficiency := executiveLookup(root, "hallEfficiency", "hall_efficiency", "efficiency")
	if !hasHall && !hasMachines && !hasFlatEfficiency {
		return nil, errors.New("hall payload does not contain a supported summary")
	}
	hall, _ := root["hall"].(map[string]any)
	machineRows := executiveObjectRows(root["machines"])
	weaverRows := executiveObjectRows(root["weavers"])

	machines := make([]map[string]any, 0, len(machineRows))
	for _, row := range machineRows {
		machines = append(machines, map[string]any{
			"machine":    executiveFirst(row, "machine", "number", "id", "machineNumber", "machine_number"),
			"weaver":     executiveFirst(row, "weaver", "weaverName", "weaver_name"),
			"efficiency": executiveNumber(executiveFirst(row, "efficiency", "efficiencyPercent", "efficiency_percent")),
			"meters":     executiveNullableNumber(executiveFirst(row, "meters", "productionMeters", "production_meters")),
			"stops":      executiveNumber(executiveFirst(row, "stops", "stopCount", "stop_count")),
			"warpStops":  executiveNumber(executiveFirst(row, "warpStops", "warp_stops", "warpBreaks", "warp_breaks")),
			"weftStops":  executiveNumber(executiveFirst(row, "weftStops", "weft_stops", "weftBreaks", "weft_breaks")),
			"status":     strings.ToLower(strings.TrimSpace(executiveString(executiveFirst(row, "status")))),
		})
	}

	weavers := make([]map[string]any, 0, len(weaverRows))
	for _, row := range weaverRows {
		weavers = append(weavers, map[string]any{
			"name":                   executiveString(executiveFirst(row, "name", "weaver", "weaverName", "weaver_name")),
			"machineNumbers":         executiveFirst(row, "machineNumbers", "machine_numbers", "machines"),
			"efficiency":             executiveNumber(executiveFirst(row, "efficiency", "score")),
			"performanceScore":       executiveNumber(executiveFirst(row, "performanceScore", "performance_score")),
			"averageRecoveryMinutes": executiveNullableNumber(executiveFirst(row, "averageRecoveryMinutes", "average_recovery_minutes")),
			"rank":                   executiveNumber(executiveFirst(row, "rank")),
		})
	}

	efficiencyValue, hasEfficiency := executiveLookup(root, "hallEfficiency", "hall_efficiency", "efficiency")
	if !hasEfficiency {
		efficiencyValue, hasEfficiency = executiveLookup(hall, "efficiency", "hallEfficiency", "hall_efficiency")
	}
	efficiency := executiveNumber(efficiencyValue)
	if !hasEfficiency && len(machines) > 0 {
		for _, row := range machines {
			efficiency += executiveNumber(row["efficiency"])
		}
		efficiency /= float64(len(machines))
	}

	activeValue, hasActiveMachines := executiveLookup(root, "activeMachines", "active_machines")
	if !hasActiveMachines {
		activeValue, hasActiveMachines = executiveLookup(hall, "activeMachineCount", "active_machine_count", "activeMachines", "active_machines")
	}
	activeMachines := int(executiveNumber(activeValue))
	if !hasActiveMachines && len(machines) > 0 {
		for _, row := range machines {
			if status := executiveString(row["status"]); status != "stopped" && status != "offline" {
				activeMachines++
			}
		}
	}

	totalStops := executiveNumber(executiveFirst(root, "totalStops", "total_stops"))
	if totalStops == 0 {
		totalStops = executiveNumber(executiveFirst(hall, "totalStops", "total_stops"))
	}
	if totalStops == 0 {
		for _, row := range machines {
			totalStops += executiveNumber(row["stops"])
		}
	}

	dataStatus := strings.ToLower(strings.TrimSpace(executiveString(executiveFirst(root, "dataStatus", "data_status"))))
	isSample := executiveBool(executiveFirst(root, "sample", "isSample", "is_sample", "demo")) || dataStatus == "sample" || dataStatus == "demo"
	return map[string]any{
		"schemaVersion":  executiveFirst(root, "schemaVersion", "schema_version"),
		"module":         executiveString(executiveFirst(root, "module")),
		"basis":          executiveString(executiveFirst(root, "basis")),
		"generatedAt":    executiveString(executiveFirst(root, "generatedAt", "generated_at")),
		"sample":         isSample,
		"hallEfficiency": efficiency,
		"activeMachines": activeMachines,
		"totalStops":     totalStops,
		"machines":       machines,
		"weavers":        weavers,
	}, nil
}

func executiveObjectRows(value any) []map[string]any {
	if rows, ok := value.([]map[string]any); ok {
		return rows
	}
	values, ok := value.([]any)
	if !ok {
		return nil
	}
	rows := make([]map[string]any, 0, len(values))
	for _, value := range values {
		if row, ok := value.(map[string]any); ok {
			rows = append(rows, row)
		}
	}
	return rows
}

func executiveFirst(row map[string]any, keys ...string) any {
	value, _ := executiveLookup(row, keys...)
	return value
}

func executiveLookup(row map[string]any, keys ...string) (any, bool) {
	for _, key := range keys {
		if value, exists := row[key]; exists && value != nil {
			return value, true
		}
	}
	return nil, false
}

func executiveNumber(value any) float64 {
	switch typed := value.(type) {
	case json.Number:
		result, _ := typed.Float64()
		return result
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case string:
		result, _ := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(typed), ",", ""), 64)
		return result
	default:
		return 0
	}
}

func executiveNullableNumber(value any) any {
	if value == nil {
		return nil
	}
	if stringValue, ok := value.(string); ok && strings.TrimSpace(stringValue) == "" {
		return nil
	}
	return executiveNumber(value)
}

func executiveString(value any) string {
	if value == nil {
		return ""
	}
	if stringValue, ok := value.(string); ok {
		return stringValue
	}
	if numberValue, ok := value.(json.Number); ok {
		return numberValue.String()
	}
	return ""
}

func executiveBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true") || strings.TrimSpace(typed) == "1"
	default:
		return executiveNumber(value) == 1
	}
}

func validateExecutiveMonitorConfig(endpoint, token string) error {
	if strings.TrimSpace(endpoint) == "" && strings.TrimSpace(token) != "" {
		return errors.New("monitor token is configured without a summary endpoint")
	}
	return nil
}
